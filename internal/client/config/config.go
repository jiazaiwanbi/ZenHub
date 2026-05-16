package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	coreconfig "zenhub/internal/core/runtimeconfig"
)

type Duration = coreconfig.Duration
type File = coreconfig.File
type Snapshot = coreconfig.Snapshot
type Route = coreconfig.Route
type ProviderGroup = coreconfig.ProviderGroup
type PassiveHealthConfig = coreconfig.PassiveHealthConfig
type Node = coreconfig.Node
type ObservabilityConfig = coreconfig.ObservabilityConfig
type Runtime = coreconfig.Runtime
type SyncConfig = coreconfig.SyncConfig

type ResolvedSyncConfig struct {
	Enabled   bool
	ServerURL string
	Username  string
	Password  string
}

const (
	defaultConfigTimeout  = 30 * time.Second
	defaultConfigCooldown = 30 * time.Second
	relayConfigTimeout    = 10 * time.Second
	relayConfigCooldown   = 10 * time.Second
)

type ClientPaths struct {
	ConfigDir     string
	ConfigPath    string
	SyncStatePath string
	LogDir        string
	LogPath       string
}

func Load(path string) (Runtime, error) {
	return coreconfig.Load(path)
}

func LoadFile(path string) (File, error) {
	return coreconfig.LoadFile(path)
}

func SaveFile(path string, file File) error {
	raw, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	raw = append(raw, '\n')

	if err := WriteFileAtomic(path, raw, 0o600); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	return nil
}

func WriteFileAtomic(path string, raw []byte, defaultPerm os.FileMode) error {
	perm, err := existingPerm(path, defaultPerm)
	if err != nil {
		return err
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create parent dir: %w", err)
	}

	tempFile, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create temp file: %w", err)
	}

	tempPath := tempFile.Name()
	committed := false
	defer func() {
		if !committed {
			_ = os.Remove(tempPath)
		}
	}()

	if err := tempFile.Chmod(perm); err != nil {
		_ = tempFile.Close()
		return fmt.Errorf("set temp file permissions: %w", err)
	}
	if _, err := tempFile.Write(raw); err != nil {
		_ = tempFile.Close()
		return fmt.Errorf("write temp file: %w", err)
	}
	if err := tempFile.Sync(); err != nil {
		_ = tempFile.Close()
		return fmt.Errorf("sync temp file: %w", err)
	}
	if err := tempFile.Close(); err != nil {
		return fmt.Errorf("close temp file: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("replace file: %w", err)
	}

	committed = true
	return nil
}

func existingPerm(path string, defaultPerm os.FileMode) (os.FileMode, error) {
	info, err := os.Stat(path)
	if err == nil {
		return info.Mode().Perm(), nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return defaultPerm, nil
	}
	return 0, fmt.Errorf("stat existing file: %w", err)
}

func ApplySnapshot(file File, snapshot Snapshot) File {
	file.Routes = append([]Route(nil), snapshot.Routes...)
	file.ProviderGroups = append([]ProviderGroup(nil), snapshot.ProviderGroups...)
	return file
}

func ResolveSync(raw SyncConfig) (ResolvedSyncConfig, error) {
	if !raw.Enabled {
		return ResolvedSyncConfig{}, nil
	}

	serverURL := strings.TrimSpace(raw.ServerURL)
	username := strings.TrimSpace(raw.Username)
	if username == "" && strings.TrimSpace(raw.UsernameEnv) != "" {
		username = strings.TrimSpace(os.Getenv(strings.TrimSpace(raw.UsernameEnv)))
	}

	password := raw.Password
	if password == "" && strings.TrimSpace(raw.PasswordEnv) != "" {
		password = os.Getenv(strings.TrimSpace(raw.PasswordEnv))
	}

	switch {
	case serverURL == "":
		return ResolvedSyncConfig{}, errors.New("sync.server_url is required when sync is enabled")
	case strings.TrimSpace(username) == "":
		return ResolvedSyncConfig{}, errors.New("sync username is required when sync is enabled")
	case password == "":
		return ResolvedSyncConfig{}, errors.New("sync password is required when sync is enabled")
	}

	return ResolvedSyncConfig{
		Enabled:   true,
		ServerURL: serverURL,
		Username:  strings.TrimSpace(username),
		Password:  password,
	}, nil
}

func SyncStatePath(configPath string) string {
	return configPath + ".sync-state.json"
}

func DefaultFile() File {
	return File{
		Listen: "127.0.0.1:8080",
		Observability: ObservabilityConfig{
			MaxRecords: 200,
		},
		Sync: SyncConfig{
			Enabled:     false,
			ServerURL:   "http://127.0.0.1:8081",
			Username:    "admin",
			PasswordEnv: "ZENHUB_COMMUNITY_PASSWORD",
		},
		Routes: []Route{
			{
				Model:         "gpt-4o-mini",
				Mode:          "direct",
				ProviderGroup: "openai-direct",
				UpstreamModel: "gpt-4o-mini",
			},
			{
				Model:         "relay-model",
				Mode:          "relay",
				ProviderGroup: "community-relay",
			},
		},
		ProviderGroups: []ProviderGroup{
			{
				Name:            "openai-direct",
				Strategy:        "round_robin",
				Timeout:         Duration{Duration: defaultConfigTimeout},
				RetryCount:      1,
				MaxNodeAttempts: 1,
				PassiveHealth: PassiveHealthConfig{
					FailureThreshold: 2,
					Cooldown:         Duration{Duration: defaultConfigCooldown},
				},
				Nodes: []Node{
					{
						Name:      "openai-primary",
						BaseURL:   "https://api.openai.com",
						APIKeyEnv: "OPENAI_API_KEY",
					},
				},
			},
			{
				Name:            "community-relay",
				Strategy:        "fill_first",
				Timeout:         Duration{Duration: relayConfigTimeout},
				RetryCount:      0,
				MaxNodeAttempts: 1,
				PassiveHealth: PassiveHealthConfig{
					FailureThreshold: 1,
					Cooldown:         Duration{Duration: relayConfigCooldown},
				},
				Nodes: []Node{
					{
						Name:      "community-primary",
						BaseURL:   "http://127.0.0.1:8081",
						APIKeyEnv: "ZENHUB_COMMUNITY_TOKEN",
					},
				},
			},
		},
	}
}

func DefaultPaths() (ClientPaths, error) {
	root, err := os.UserConfigDir()
	if err != nil {
		return ClientPaths{}, fmt.Errorf("resolve user config dir: %w", err)
	}
	return clientPathsFromRoot(root), nil
}

func PathsForConfig(configPath string) (ClientPaths, error) {
	configPath = strings.TrimSpace(configPath)
	if configPath == "" {
		return ClientPaths{}, errors.New("config path is required")
	}

	absPath, err := filepath.Abs(configPath)
	if err != nil {
		return ClientPaths{}, fmt.Errorf("resolve config path: %w", err)
	}

	configDir := filepath.Dir(absPath)
	logDir := filepath.Join(configDir, "logs")
	return ClientPaths{
		ConfigDir:     configDir,
		ConfigPath:    absPath,
		SyncStatePath: SyncStatePath(absPath),
		LogDir:        logDir,
		LogPath:       filepath.Join(logDir, "client.log"),
	}, nil
}

func ResolveOrCreateClientConfig(configPath string) (ClientPaths, bool, error) {
	configPath = strings.TrimSpace(configPath)
	if configPath != "" {
		paths, err := PathsForConfig(configPath)
		return paths, false, err
	}

	paths, err := DefaultPaths()
	if err != nil {
		return ClientPaths{}, false, err
	}

	created, err := ensureDefaultConfig(paths)
	if err != nil {
		return ClientPaths{}, false, err
	}
	return paths, created, nil
}

func clientPathsFromRoot(root string) ClientPaths {
	configDir := filepath.Join(root, "ZenHub")
	configPath := filepath.Join(configDir, "config.json")
	logDir := filepath.Join(configDir, "logs")
	return ClientPaths{
		ConfigDir:     configDir,
		ConfigPath:    configPath,
		SyncStatePath: SyncStatePath(configPath),
		LogDir:        logDir,
		LogPath:       filepath.Join(logDir, "client.log"),
	}
}

func ensureDefaultConfig(paths ClientPaths) (bool, error) {
	if err := os.MkdirAll(paths.ConfigDir, 0o700); err != nil {
		return false, fmt.Errorf("create config dir: %w", err)
	}
	if err := os.MkdirAll(paths.LogDir, 0o700); err != nil {
		return false, fmt.Errorf("create log dir: %w", err)
	}
	if _, err := os.Stat(paths.ConfigPath); err == nil {
		return false, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("stat default config: %w", err)
	}

	if err := SaveFile(paths.ConfigPath, DefaultFile()); err != nil {
		return false, err
	}
	return true, nil
}
