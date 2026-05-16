package codex

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"

	clientconfig "zenhub/internal/client/config"
	"zenhub/internal/core/runtimeconfig"
)

const (
	defaultStableModelProviderID = "zenhub"
	originalAuthBackupName       = "original-auth.json"
	originalConfigBackupName     = "original-config.toml"
)

var reservedModelProviderIDs = []string{
	"amazon-bedrock",
	"openai",
	"ollama",
	"lmstudio",
	"oss",
	"ollama-chat",
}

type ProviderView struct {
	Name    string
	Current bool
}

type paths struct {
	configDir        string
	authPath         string
	configPath       string
	backupDir        string
	backupAuthPath   string
	backupConfigPath string
}

type liveFiles struct {
	Auth   json.RawMessage
	Config string
}

func ProviderViews(file runtimeconfig.File) []ProviderView {
	current := strings.TrimSpace(file.Codex.CurrentProviderGroup)
	views := make([]ProviderView, 0, len(file.ProviderGroups))
	for _, group := range file.ProviderGroups {
		if group.Codex == nil {
			continue
		}
		views = append(views, ProviderView{
			Name:    group.Name,
			Current: strings.TrimSpace(group.Name) == current,
		})
	}
	return views
}

func CurrentProviderName(file runtimeconfig.File) string {
	return strings.TrimSpace(file.Codex.CurrentProviderGroup)
}

func SwitchProvider(clientConfigPath string, file *runtimeconfig.File, providerGroupName string) error {
	if file == nil {
		return errors.New("codex switch requires a config file")
	}

	targetIndex, err := findProviderGroupIndex(*file, providerGroupName)
	if err != nil {
		return err
	}

	resolvedPaths, err := resolvePaths(clientConfigPath, file.Codex.ConfigDir)
	if err != nil {
		return err
	}
	if err := ensureOriginalBackup(resolvedPaths); err != nil {
		return err
	}

	currentName := strings.TrimSpace(file.Codex.CurrentProviderGroup)
	if currentName != "" && currentName != strings.TrimSpace(providerGroupName) {
		if currentIndex, currentErr := findProviderGroupIndex(*file, currentName); currentErr == nil {
			live, liveErr := readLive(resolvedPaths)
			if liveErr == nil {
				backfilled := &runtimeconfig.CodexProviderConfig{
					Auth: cloneRawMessage(live.Auth),
				}
				restoredConfig, restoreErr := restoreTemplateModelProviderID(
					live.Config,
					file.ProviderGroups[currentIndex].Codex.Config,
				)
				if restoreErr == nil {
					backfilled.Config = restoredConfig
				} else {
					backfilled.Config = live.Config
				}
				file.ProviderGroups[currentIndex].Codex = backfilled
			}
		}
	}

	if err := writeLiveAtomicWithStableProvider(resolvedPaths, file.ProviderGroups[targetIndex].Codex); err != nil {
		return err
	}

	file.Codex.CurrentProviderGroup = strings.TrimSpace(file.ProviderGroups[targetIndex].Name)
	return nil
}

func resolvePaths(clientConfigPath string, overrideDir string) (paths, error) {
	configDir, err := resolveCodexConfigDir(overrideDir)
	if err != nil {
		return paths{}, err
	}

	configPathInfo, err := clientconfig.PathsForConfig(clientConfigPath)
	if err != nil {
		return paths{}, err
	}

	backupDir := filepath.Join(configPathInfo.ConfigDir, "codex-live-backup")
	return paths{
		configDir:        configDir,
		authPath:         filepath.Join(configDir, "auth.json"),
		configPath:       filepath.Join(configDir, "config.toml"),
		backupDir:        backupDir,
		backupAuthPath:   filepath.Join(backupDir, originalAuthBackupName),
		backupConfigPath: filepath.Join(backupDir, originalConfigBackupName),
	}, nil
}

func resolveCodexConfigDir(overrideDir string) (string, error) {
	if strings.TrimSpace(overrideDir) != "" {
		return filepath.Abs(strings.TrimSpace(overrideDir))
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve codex home dir: %w", err)
	}
	return filepath.Join(homeDir, ".codex"), nil
}

func ensureOriginalBackup(paths paths) error {
	if err := os.MkdirAll(paths.backupDir, 0o700); err != nil {
		return fmt.Errorf("create codex backup dir: %w", err)
	}

	if err := backupFileIfNeeded(paths.authPath, paths.backupAuthPath); err != nil {
		return err
	}
	if err := backupFileIfNeeded(paths.configPath, paths.backupConfigPath); err != nil {
		return err
	}
	return nil
}

func backupFileIfNeeded(sourcePath, backupPath string) error {
	if _, err := os.Stat(backupPath); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("stat codex backup: %w", err)
	}

	raw, err := os.ReadFile(sourcePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return fmt.Errorf("read codex live file for backup: %w", err)
	}

	return clientconfig.WriteFileAtomic(backupPath, raw, 0o600)
}

func readLive(paths paths) (liveFiles, error) {
	authRaw, err := os.ReadFile(paths.authPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return liveFiles{}, fmt.Errorf("read codex auth.json: %w", err)
	}

	configRaw, err := os.ReadFile(paths.configPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return liveFiles{}, fmt.Errorf("read codex config.toml: %w", err)
	}

	return liveFiles{
		Auth:   cloneRawMessage(authRaw),
		Config: string(configRaw),
	}, nil
}

func writeLiveAtomicWithStableProvider(paths paths, provider *runtimeconfig.CodexProviderConfig) error {
	if provider == nil {
		return errors.New("selected provider does not define codex settings")
	}

	authRaw, err := normalizeAuth(provider.Auth)
	if err != nil {
		return err
	}

	currentLiveConfig := ""
	if live, liveErr := readLive(paths); liveErr == nil {
		currentLiveConfig = live.Config
	}

	configText, err := normalizeStableModelProvider(provider.Config, currentLiveConfig)
	if err != nil {
		return err
	}

	return writeLiveAtomic(paths, authRaw, configText)
}

func writeLiveAtomic(paths paths, authRaw []byte, configText string) error {
	if err := os.MkdirAll(paths.configDir, 0o700); err != nil {
		return fmt.Errorf("create codex config dir: %w", err)
	}
	if err := validateConfigTOML(configText); err != nil {
		return err
	}

	oldAuth, authExisted, err := readExistingFile(paths.authPath)
	if err != nil {
		return err
	}
	oldConfig, configExisted, err := readExistingFile(paths.configPath)
	if err != nil {
		return err
	}

	if err := clientconfig.WriteFileAtomic(paths.authPath, authRaw, 0o600); err != nil {
		return fmt.Errorf("write codex auth.json: %w", err)
	}

	configRaw := []byte(configText)
	if err := clientconfig.WriteFileAtomic(paths.configPath, configRaw, 0o600); err != nil {
		if rollbackErr := rollbackFile(paths.authPath, oldAuth, authExisted); rollbackErr != nil {
			return fmt.Errorf("write codex config.toml: %v; rollback auth.json: %w", err, rollbackErr)
		}
		if configExisted {
			_ = clientconfig.WriteFileAtomic(paths.configPath, oldConfig, 0o600)
		} else {
			_ = os.Remove(paths.configPath)
		}
		return fmt.Errorf("write codex config.toml: %w", err)
	}

	return nil
}

func normalizeAuth(raw json.RawMessage) ([]byte, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		trimmed = []byte("{}")
	}

	var value any
	if err := json.Unmarshal(trimmed, &value); err != nil {
		return nil, fmt.Errorf("decode codex auth.json: %w", err)
	}

	formatted, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal codex auth.json: %w", err)
	}
	return append(formatted, '\n'), nil
}

func validateConfigTOML(configText string) error {
	if strings.TrimSpace(configText) == "" {
		return nil
	}

	var document map[string]any
	if _, err := toml.Decode(configText, &document); err != nil {
		return fmt.Errorf("decode codex config.toml: %w", err)
	}
	return nil
}

func readExistingFile(path string) ([]byte, bool, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("read existing file: %w", err)
	}
	return raw, true, nil
}

func rollbackFile(path string, raw []byte, existed bool) error {
	if existed {
		return clientconfig.WriteFileAtomic(path, raw, 0o600)
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

func findProviderGroupIndex(file runtimeconfig.File, providerGroupName string) (int, error) {
	target := strings.TrimSpace(providerGroupName)
	if target == "" {
		return -1, errors.New("codex provider name is required")
	}

	for index, group := range file.ProviderGroups {
		if strings.TrimSpace(group.Name) == target && group.Codex != nil {
			return index, nil
		}
	}
	return -1, fmt.Errorf("codex provider group %q was not found", target)
}

func normalizeStableModelProvider(configText string, anchorConfigText string) (string, error) {
	if strings.TrimSpace(configText) == "" {
		return "", nil
	}

	document, err := parseTOMLConfig(configText)
	if err != nil {
		return "", fmt.Errorf("decode codex config.toml: %w", err)
	}

	sourceProviderID := activeModelProviderID(document)
	if sourceProviderID == "" || !hasModelProviderTable(document, sourceProviderID) {
		return configText, nil
	}

	stableProviderID := stableModelProviderID(anchorConfigText)
	if stableProviderID == "" {
		if isCustomModelProviderID(sourceProviderID) {
			stableProviderID = sourceProviderID
		} else {
			stableProviderID = defaultStableModelProviderID
		}
	}
	if stableProviderID == sourceProviderID {
		return configText, nil
	}

	renameModelProvider(document, sourceProviderID, stableProviderID)
	return encodeTOMLConfig(document)
}

func restoreTemplateModelProviderID(configText, templateConfigText string) (string, error) {
	templateProviderID := activeModelProviderIDWithTable(templateConfigText)
	if templateProviderID == "" || strings.TrimSpace(configText) == "" {
		return configText, nil
	}

	document, err := parseTOMLConfig(configText)
	if err != nil {
		return "", fmt.Errorf("decode codex live config.toml: %w", err)
	}

	liveProviderID := activeModelProviderID(document)
	if liveProviderID == "" || liveProviderID == templateProviderID || !hasModelProviderTable(document, liveProviderID) {
		return configText, nil
	}

	renameModelProvider(document, liveProviderID, templateProviderID)
	return encodeTOMLConfig(document)
}

func stableModelProviderID(anchorConfigText string) string {
	document, err := parseTOMLConfig(anchorConfigText)
	if err != nil {
		return ""
	}

	providerID := activeModelProviderID(document)
	if providerID == "" || !hasModelProviderTable(document, providerID) {
		return ""
	}
	if !isCustomModelProviderID(providerID) {
		return ""
	}
	return providerID
}

func activeModelProviderIDWithTable(configText string) string {
	document, err := parseTOMLConfig(configText)
	if err != nil {
		return ""
	}

	providerID := activeModelProviderID(document)
	if providerID == "" || !hasModelProviderTable(document, providerID) {
		return ""
	}
	return providerID
}

func parseTOMLConfig(configText string) (map[string]any, error) {
	document := map[string]any{}
	if strings.TrimSpace(configText) == "" {
		return document, nil
	}

	if _, err := toml.Decode(configText, &document); err != nil {
		return nil, err
	}
	return document, nil
}

func encodeTOMLConfig(document map[string]any) (string, error) {
	var buffer bytes.Buffer
	if err := toml.NewEncoder(&buffer).Encode(document); err != nil {
		return "", err
	}
	return buffer.String(), nil
}

func activeModelProviderID(document map[string]any) string {
	value, _ := document["model_provider"].(string)
	return strings.TrimSpace(value)
}

func hasModelProviderTable(document map[string]any, providerID string) bool {
	modelProviders, ok := document["model_providers"].(map[string]any)
	if !ok {
		return false
	}
	_, ok = modelProviders[providerID]
	return ok
}

func renameModelProvider(document map[string]any, sourceProviderID string, targetProviderID string) {
	modelProviders, ok := document["model_providers"].(map[string]any)
	if !ok {
		return
	}

	value, exists := modelProviders[sourceProviderID]
	if !exists {
		return
	}
	delete(modelProviders, sourceProviderID)
	modelProviders[targetProviderID] = value

	rewriteProfileModelProviderRefs(document, sourceProviderID, targetProviderID)
	document["model_provider"] = targetProviderID
}

func rewriteProfileModelProviderRefs(document map[string]any, sourceProviderID string, targetProviderID string) {
	profiles, ok := document["profiles"].(map[string]any)
	if !ok {
		return
	}

	for _, rawProfile := range profiles {
		profile, ok := rawProfile.(map[string]any)
		if !ok {
			continue
		}
		currentValue, _ := profile["model_provider"].(string)
		if strings.TrimSpace(currentValue) == sourceProviderID {
			profile["model_provider"] = targetProviderID
		}
	}
}

func isCustomModelProviderID(providerID string) bool {
	providerID = strings.TrimSpace(providerID)
	if providerID == "" {
		return false
	}
	for _, reserved := range reservedModelProviderIDs {
		if strings.EqualFold(providerID, reserved) {
			return false
		}
	}
	return true
}

func cloneRawMessage(raw []byte) json.RawMessage {
	if len(raw) == 0 {
		return nil
	}
	cloned := make([]byte, len(raw))
	copy(cloned, raw)
	return cloned
}
