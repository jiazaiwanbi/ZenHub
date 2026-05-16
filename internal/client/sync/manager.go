package sync

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"

	clientconfig "zenhub/internal/client/config"
	"zenhub/internal/controlplane"
	"zenhub/internal/core/runtimeconfig"
	controlv1 "zenhub/internal/gen/controlv1"
)

const (
	StatusDisabled      = "disabled"
	StatusEmpty         = "empty"
	StatusUpdated       = "updated"
	StatusUpToDate      = "up_to_date"
	StatusClientNewer   = "client_newer"
	StatusConflict      = "conflict"
	StatusApplied       = "applied"
	StatusAlreadySynced = "already_synced"
	StatusError         = "error"
)

const (
	controlDialTimeout      = 3 * time.Second
	ConflictReasonFirstSync = "first_sync_mismatch"
	ConflictReasonPull      = "pull_conflict"
	ConflictReasonPush      = "push_conflict"
)

type ConflictChoice string

const (
	ConflictChoiceLocal ConflictChoice = "local"
	ConflictChoiceCloud ConflictChoice = "cloud"
)

type Manager struct {
	configPath string
	client     *http.Client
	now        func() time.Time
}

type controlClients struct {
	conn    *grpc.ClientConn
	auth    controlv1.AuthServiceClient
	control controlv1.CommunityControlServiceClient
}

type State struct {
	LastSyncAt     int64          `json:"last_sync_at,omitempty"`
	CloudHash      string         `json:"cloud_hash,omitempty"`
	LastSyncStatus string         `json:"last_sync_status,omitempty"`
	LastError      string         `json:"last_error,omitempty"`
	Conflict       *ConflictState `json:"conflict,omitempty"`
}

type Report struct {
	Enabled     bool
	LastSyncAt  int64
	CloudHash   string
	Status      string
	Error       string
	HasConflict bool
}

type StatusSummary struct {
	Enabled         bool
	Status          string
	Error           string
	HasConflict     bool
	LastSyncAt      int64
	LocalHash       string
	LocalModifiedAt int64
	CloudHash       string
	CloudUpdatedAt  int64
}

type Conflict struct {
	HasConflict bool
	Reason      string
	Local       ConflictSnapshot
	Cloud       ConflictSnapshot
	Error       string
}

type ConflictSnapshot struct {
	Hash      string
	Timestamp int64
	Snapshot  runtimeconfig.Snapshot
}

type ConflictState struct {
	Reason string            `json:"reason,omitempty"`
	Local  ConflictStateSide `json:"local"`
	Cloud  ConflictStateSide `json:"cloud"`
}

type ConflictStateSide struct {
	Hash      string                  `json:"hash,omitempty"`
	Timestamp int64                   `json:"timestamp,omitempty"`
	Snapshot  *runtimeconfig.Snapshot `json:"snapshot,omitempty"`
}

type statusResponse struct {
	HasSnapshot    bool   `json:"has_snapshot"`
	CloudUpdatedAt int64  `json:"cloud_updated_at,omitempty"`
	CloudHash      string `json:"cloud_hash,omitempty"`
}

type pullRequest struct {
	LastSyncAt      int64  `json:"last_sync_at"`
	LocalModifiedAt int64  `json:"local_modified_at"`
	LocalHash       string `json:"local_hash"`
}

type pullResponse struct {
	Status         string                  `json:"status"`
	CloudUpdatedAt int64                   `json:"cloud_updated_at,omitempty"`
	CloudHash      string                  `json:"cloud_hash,omitempty"`
	Snapshot       *runtimeconfig.Snapshot `json:"snapshot,omitempty"`
}

type pushRequest struct {
	LastSyncAt      int64                  `json:"last_sync_at"`
	LocalModifiedAt int64                  `json:"local_modified_at"`
	LocalHash       string                 `json:"local_hash"`
	Snapshot        runtimeconfig.Snapshot `json:"snapshot"`
}

type pushResponse struct {
	Status         string                  `json:"status"`
	CloudUpdatedAt int64                   `json:"cloud_updated_at,omitempty"`
	CloudHash      string                  `json:"cloud_hash,omitempty"`
	Snapshot       *runtimeconfig.Snapshot `json:"snapshot,omitempty"`
}

type providersResponse struct {
	Version        int64                         `json:"version,omitempty"`
	CloudUpdatedAt int64                         `json:"cloud_updated_at,omitempty"`
	CloudHash      string                        `json:"cloud_hash,omitempty"`
	ProviderGroups []runtimeconfig.ProviderGroup `json:"provider_groups"`
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type loginResponse struct {
	AccessToken string `json:"access_token"`
}

func NewManager(configPath string, client *http.Client) *Manager {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &Manager{
		configPath: configPath,
		client:     client,
		now:        time.Now,
	}
}

func (m *Manager) SyncBeforeStart(ctx context.Context) (clientconfig.File, Report, error) {
	file, localModifiedAt, err := m.loadConfig()
	if err != nil {
		return clientconfig.File{}, Report{}, err
	}

	resolved, err := clientconfig.ResolveSync(file.Sync)
	if err != nil {
		report := Report{Enabled: true, Status: StatusError, Error: err.Error()}
		return file, report, m.persistState(State{LastSyncStatus: StatusError, LastError: err.Error()}, err)
	}
	if !resolved.Enabled {
		return file, Report{Status: StatusDisabled}, nil
	}

	state, err := m.loadState()
	if err != nil {
		report := Report{Enabled: true, Status: StatusError, Error: err.Error()}
		return file, report, err
	}

	clients, err := m.dialControl(ctx, resolved.ServerURL)
	if err != nil {
		report := Report{Enabled: true, Status: StatusError, Error: err.Error()}
		return file, report, m.persistState(withError(state, err.Error(), StatusError), fmt.Errorf("sync login: dial control plane: %w", err))
	}
	defer clients.conn.Close()

	localHash, err := runtimeconfig.HashSnapshot(file.Snapshot())
	if err != nil {
		report := Report{Enabled: true, Status: StatusError, Error: err.Error()}
		return file, report, m.persistState(withError(state, err.Error(), StatusError), err)
	}

	token, err := m.login(ctx, clients.auth, resolved)
	if err != nil {
		report := Report{Enabled: true, Status: StatusError, Error: err.Error()}
		return file, report, m.persistState(withError(state, err.Error(), StatusError), fmt.Errorf("sync login: %w", err))
	}

	cloudStatus, err := m.fetchStatus(ctx, clients.control, token)
	if err != nil {
		report := Report{Enabled: true, Status: StatusError, Error: err.Error()}
		return file, report, m.persistState(withError(state, err.Error(), StatusError), fmt.Errorf("sync status: %w", err))
	}

	if state.LastSyncAt == 0 && cloudStatus.HasSnapshot && localHash != cloudStatus.CloudHash {
		cloudPull, err := m.pull(ctx, clients.control, token, pullRequest{
			LastSyncAt:      0,
			LocalModifiedAt: localModifiedAt,
			LocalHash:       localHash,
		})
		if err != nil {
			report := Report{Enabled: true, Status: StatusError, Error: err.Error()}
			return file, report, m.persistState(withError(state, err.Error(), StatusError), fmt.Errorf("sync pull for first conflict: %w", err))
		}
		if cloudPull.Snapshot == nil {
			err := errors.New("sync pull for first conflict returned no snapshot")
			report := Report{Enabled: true, Status: StatusError, Error: err.Error()}
			return file, report, m.persistState(withError(state, err.Error(), StatusError), err)
		}

		conflictErr := "sync conflict: local config differs from cloud before first sync"
		nextState := State{
			CloudHash:      chooseString(cloudPull.CloudHash, cloudStatus.CloudHash),
			LastSyncStatus: StatusConflict,
			LastError:      conflictErr,
			Conflict: buildConflictState(
				ConflictReasonFirstSync,
				file.Snapshot(),
				localHash,
				localModifiedAt,
				chooseString(cloudPull.CloudHash, cloudStatus.CloudHash),
				chooseInt64(cloudPull.CloudUpdatedAt, cloudStatus.CloudUpdatedAt),
				cloudPull.Snapshot,
			),
		}
		report := Report{
			Enabled:     true,
			Status:      StatusConflict,
			LastSyncAt:  chooseInt64(cloudPull.CloudUpdatedAt, cloudStatus.CloudUpdatedAt),
			CloudHash:   chooseString(cloudPull.CloudHash, cloudStatus.CloudHash),
			Error:       conflictErr,
			HasConflict: true,
		}
		return file, report, m.persistState(nextState, nil)
	}

	pullResp, err := m.pull(ctx, clients.control, token, pullRequest{
		LastSyncAt:      state.LastSyncAt,
		LocalModifiedAt: localModifiedAt,
		LocalHash:       localHash,
	})
	if err != nil {
		report := Report{Enabled: true, Status: StatusError, Error: err.Error()}
		return file, report, m.persistState(withError(state, err.Error(), StatusError), fmt.Errorf("sync pull: %w", err))
	}

	switch pullResp.Status {
	case StatusEmpty:
		nextState := State{
			LastSyncAt:     state.LastSyncAt,
			LastSyncStatus: StatusEmpty,
		}
		report := reportFromState(nextState, true)
		return file, report, m.persistState(nextState, nil)
	case StatusUpdated:
		if pullResp.Snapshot == nil {
			err := errors.New("sync pull returned updated without snapshot")
			report := Report{Enabled: true, Status: StatusError, Error: err.Error()}
			return file, report, m.persistState(withError(state, err.Error(), StatusError), err)
		}

		file = clientconfig.ApplySnapshot(file, *pullResp.Snapshot)
		if err := clientconfig.SaveFile(m.configPath, file); err != nil {
			report := Report{Enabled: true, Status: StatusError, Error: err.Error()}
			return file, report, m.persistState(withError(state, err.Error(), StatusError), err)
		}

		nextState := State{
			LastSyncAt:     pullResp.CloudUpdatedAt,
			CloudHash:      pullResp.CloudHash,
			LastSyncStatus: StatusUpdated,
		}
		report := reportFromState(nextState, true)
		return file, report, m.persistState(nextState, nil)
	case StatusUpToDate:
		nextState := State{
			LastSyncAt:     chooseInt64(pullResp.CloudUpdatedAt, state.LastSyncAt),
			CloudHash:      chooseString(pullResp.CloudHash, state.CloudHash),
			LastSyncStatus: StatusUpToDate,
		}
		report := reportFromState(nextState, true)
		return file, report, m.persistState(nextState, nil)
	case StatusClientNewer:
		nextState := State{
			LastSyncAt:     state.LastSyncAt,
			CloudHash:      chooseString(pullResp.CloudHash, state.CloudHash),
			LastSyncStatus: StatusClientNewer,
		}
		report := reportFromState(nextState, true)
		return file, report, m.persistState(nextState, nil)
	case StatusConflict:
		conflictErr := "sync conflict: local and cloud snapshots both changed"
		nextState := State{
			LastSyncAt:     state.LastSyncAt,
			CloudHash:      pullResp.CloudHash,
			LastSyncStatus: StatusConflict,
			LastError:      conflictErr,
			Conflict: buildConflictState(
				ConflictReasonPull,
				file.Snapshot(),
				localHash,
				localModifiedAt,
				pullResp.CloudHash,
				pullResp.CloudUpdatedAt,
				pullResp.Snapshot,
			),
		}
		report := reportFromState(nextState, true)
		report.Error = conflictErr
		report.LastSyncAt = chooseInt64(pullResp.CloudUpdatedAt, report.LastSyncAt)
		return file, report, m.persistState(nextState, nil)
	default:
		err := fmt.Errorf("unexpected sync pull status %q", pullResp.Status)
		report := Report{Enabled: true, Status: StatusError, Error: err.Error()}
		return file, report, m.persistState(withError(state, err.Error(), StatusError), err)
	}
}

func (m *Manager) SyncOnShutdown(ctx context.Context) (Report, error) {
	file, localModifiedAt, err := m.loadConfig()
	if err != nil {
		return Report{}, err
	}

	resolved, err := clientconfig.ResolveSync(file.Sync)
	if err != nil {
		report := Report{Enabled: true, Status: StatusError, Error: err.Error()}
		return report, m.persistState(State{LastSyncStatus: StatusError, LastError: err.Error()}, err)
	}
	if !resolved.Enabled {
		return Report{Status: StatusDisabled}, nil
	}

	state, err := m.loadState()
	if err != nil {
		return Report{Enabled: true, Status: StatusError, Error: err.Error()}, err
	}

	clients, err := m.dialControl(ctx, resolved.ServerURL)
	if err != nil {
		report := Report{Enabled: true, Status: StatusError, Error: err.Error()}
		return report, m.persistState(withError(state, err.Error(), StatusError), fmt.Errorf("sync login: dial control plane: %w", err))
	}
	defer clients.conn.Close()

	token, err := m.login(ctx, clients.auth, resolved)
	if err != nil {
		report := Report{Enabled: true, Status: StatusError, Error: err.Error()}
		return report, m.persistState(withError(state, err.Error(), StatusError), fmt.Errorf("sync login: %w", err))
	}

	snapshot := file.Snapshot()
	localHash, err := runtimeconfig.HashSnapshot(snapshot)
	if err != nil {
		report := Report{Enabled: true, Status: StatusError, Error: err.Error()}
		return report, m.persistState(withError(state, err.Error(), StatusError), err)
	}

	pushResp, err := m.push(ctx, clients.control, token, pushRequest{
		LastSyncAt:      state.LastSyncAt,
		LocalModifiedAt: localModifiedAt,
		LocalHash:       localHash,
		Snapshot:        snapshot,
	})
	if err != nil {
		report := Report{Enabled: true, Status: StatusError, Error: err.Error()}
		return report, m.persistState(withError(state, err.Error(), StatusError), fmt.Errorf("sync push: %w", err))
	}

	switch pushResp.Status {
	case StatusApplied, StatusAlreadySynced:
		nextState := State{
			LastSyncAt:     pushResp.CloudUpdatedAt,
			CloudHash:      pushResp.CloudHash,
			LastSyncStatus: pushResp.Status,
		}
		report := reportFromState(nextState, true)
		return report, m.persistState(nextState, nil)
	case StatusConflict:
		conflictErr := "sync conflict: cloud snapshot changed before exit push"
		nextState := State{
			LastSyncAt:     state.LastSyncAt,
			CloudHash:      pushResp.CloudHash,
			LastSyncStatus: StatusConflict,
			LastError:      conflictErr,
			Conflict: buildConflictState(
				ConflictReasonPush,
				snapshot,
				localHash,
				localModifiedAt,
				pushResp.CloudHash,
				pushResp.CloudUpdatedAt,
				pushResp.Snapshot,
			),
		}
		report := reportFromState(nextState, true)
		report.Error = conflictErr
		return report, m.persistState(nextState, nil)
	default:
		err := fmt.Errorf("unexpected sync push status %q", pushResp.Status)
		report := Report{Enabled: true, Status: StatusError, Error: err.Error()}
		return report, m.persistState(withError(state, err.Error(), StatusError), err)
	}
}

func (m *Manager) CurrentConflict() (Conflict, error) {
	state, err := m.loadState()
	if err != nil {
		return Conflict{}, err
	}
	if state.Conflict == nil {
		return Conflict{}, nil
	}

	return conflictFromState(state.Conflict, state.LastError), nil
}

func (m *Manager) CurrentStatus() (StatusSummary, error) {
	file, localModifiedAt, err := m.loadConfig()
	if err != nil {
		return StatusSummary{}, err
	}

	localHash, err := runtimeconfig.HashSnapshot(file.Snapshot())
	if err != nil {
		return StatusSummary{}, err
	}

	resolved, err := clientconfig.ResolveSync(file.Sync)
	if err != nil {
		return StatusSummary{
			Enabled:         true,
			Status:          StatusError,
			Error:           err.Error(),
			LocalHash:       localHash,
			LocalModifiedAt: localModifiedAt,
		}, nil
	}
	if !resolved.Enabled {
		return StatusSummary{
			Status:          StatusDisabled,
			LocalHash:       localHash,
			LocalModifiedAt: localModifiedAt,
		}, nil
	}

	state, err := m.loadState()
	if err != nil {
		return StatusSummary{}, err
	}

	summary := StatusSummary{
		Enabled:         true,
		Status:          state.LastSyncStatus,
		Error:           state.LastError,
		HasConflict:     state.Conflict != nil,
		LastSyncAt:      state.LastSyncAt,
		LocalHash:       localHash,
		LocalModifiedAt: localModifiedAt,
		CloudHash:       state.CloudHash,
		CloudUpdatedAt:  state.LastSyncAt,
	}
	if state.Conflict != nil {
		if strings.TrimSpace(state.Conflict.Cloud.Hash) != "" {
			summary.CloudHash = strings.TrimSpace(state.Conflict.Cloud.Hash)
		}
		if state.Conflict.Cloud.Timestamp != 0 {
			summary.CloudUpdatedAt = state.Conflict.Cloud.Timestamp
		}
	}
	return summary, nil
}

func (m *Manager) ResolveConflict(ctx context.Context, choice ConflictChoice) (clientconfig.File, Report, error) {
	file, _, err := m.loadConfig()
	if err != nil {
		return clientconfig.File{}, Report{}, err
	}

	state, err := m.loadState()
	if err != nil {
		return clientconfig.File{}, Report{}, err
	}
	if state.Conflict == nil {
		return file, reportFromState(state, false), errors.New("no sync conflict to resolve")
	}

	switch choice {
	case ConflictChoiceCloud:
		return m.resolveWithCloud(file, state)
	case ConflictChoiceLocal:
		return m.resolveWithLocal(ctx, file, state)
	default:
		return file, reportFromState(state, true), fmt.Errorf("unsupported conflict choice %q", choice)
	}
}

func (m *Manager) SyncNow(ctx context.Context) (clientconfig.File, Report, error) {
	file, report, err := m.SyncBeforeStart(ctx)
	if err != nil {
		return file, report, err
	}

	switch report.Status {
	case StatusDisabled, StatusUpdated, StatusUpToDate, StatusConflict, StatusError:
		return file, report, nil
	case StatusEmpty, StatusClientNewer:
		pushReport, pushErr := m.SyncOnShutdown(ctx)
		latestFile, _, loadErr := m.loadConfig()
		if loadErr != nil {
			if pushErr != nil {
				return latestFile, pushReport, fmt.Errorf("%v; %w", pushErr, loadErr)
			}
			return latestFile, pushReport, loadErr
		}
		return latestFile, pushReport, pushErr
	default:
		return file, report, nil
	}
}

func (m *Manager) SyncProviders(ctx context.Context) (clientconfig.File, Report, error) {
	file, _, err := m.loadConfig()
	if err != nil {
		return clientconfig.File{}, Report{}, err
	}

	resolved, err := clientconfig.ResolveSync(file.Sync)
	if err != nil {
		report := Report{Enabled: true, Status: StatusError, Error: err.Error()}
		return file, report, m.persistState(State{LastSyncStatus: StatusError, LastError: err.Error()}, err)
	}
	if !resolved.Enabled {
		return file, Report{Status: StatusDisabled}, nil
	}

	state, err := m.loadState()
	if err != nil {
		return file, Report{Enabled: true, Status: StatusError, Error: err.Error()}, err
	}

	clients, err := m.dialControl(ctx, resolved.ServerURL)
	if err != nil {
		report := Report{Enabled: true, Status: StatusError, Error: err.Error()}
		return file, report, m.persistState(withError(state, err.Error(), StatusError), fmt.Errorf("provider sync login: dial control plane: %w", err))
	}
	defer clients.conn.Close()

	token, err := m.login(ctx, clients.auth, resolved)
	if err != nil {
		report := Report{Enabled: true, Status: StatusError, Error: err.Error()}
		return file, report, m.persistState(withError(state, err.Error(), StatusError), fmt.Errorf("provider sync login: %w", err))
	}

	catalog, err := m.fetchProviders(ctx, clients.control, token)
	if err != nil {
		report := Report{Enabled: true, Status: StatusError, Error: err.Error()}
		return file, report, m.persistState(withError(state, err.Error(), StatusError), fmt.Errorf("provider sync fetch: %w", err))
	}

	file.ProviderGroups = cloneProviderGroups(catalog.ProviderGroups)
	if _, err := file.Runtime(); err != nil {
		report := Report{Enabled: true, Status: StatusError, Error: err.Error()}
		return file, report, m.persistState(withError(state, err.Error(), StatusError), fmt.Errorf("validate provider sync config: %w", err))
	}
	if err := clientconfig.SaveFile(m.configPath, file); err != nil {
		report := Report{Enabled: true, Status: StatusError, Error: err.Error()}
		return file, report, m.persistState(withError(state, err.Error(), StatusError), err)
	}

	return file, reportFromState(state, true), nil
}

func (m *Manager) resolveWithCloud(file clientconfig.File, state State) (clientconfig.File, Report, error) {
	conflict := state.Conflict
	if conflict == nil || conflict.Cloud.Snapshot == nil {
		return file, reportFromState(state, true), errors.New("cloud snapshot is not available for conflict resolution")
	}

	file = clientconfig.ApplySnapshot(file, *conflict.Cloud.Snapshot)
	if err := clientconfig.SaveFile(m.configPath, file); err != nil {
		return file, Report{Enabled: true, Status: StatusError, Error: err.Error()}, err
	}

	nextState := State{
		LastSyncAt:     conflict.Cloud.Timestamp,
		CloudHash:      conflict.Cloud.Hash,
		LastSyncStatus: StatusUpdated,
	}
	report := reportFromState(nextState, true)
	return file, report, m.persistState(nextState, nil)
}

func (m *Manager) resolveWithLocal(ctx context.Context, file clientconfig.File, state State) (clientconfig.File, Report, error) {
	conflict := state.Conflict
	if conflict == nil || conflict.Local.Snapshot == nil {
		return file, reportFromState(state, true), errors.New("local snapshot is not available for conflict resolution")
	}

	file = clientconfig.ApplySnapshot(file, *conflict.Local.Snapshot)
	if err := clientconfig.SaveFile(m.configPath, file); err != nil {
		return file, Report{Enabled: true, Status: StatusError, Error: err.Error()}, err
	}

	resolved, err := clientconfig.ResolveSync(file.Sync)
	if err != nil {
		report := Report{Enabled: true, Status: StatusError, Error: err.Error()}
		return file, report, m.persistState(State{LastSyncStatus: StatusError, LastError: err.Error()}, err)
	}
	if !resolved.Enabled {
		nextState := State{LastSyncStatus: StatusDisabled}
		report := reportFromState(nextState, false)
		return file, report, m.persistState(nextState, nil)
	}

	clients, err := m.dialControl(ctx, resolved.ServerURL)
	if err != nil {
		report := Report{Enabled: true, Status: StatusError, Error: err.Error()}
		return file, report, m.persistState(withError(state, err.Error(), StatusError), fmt.Errorf("dial control plane: %w", err))
	}
	defer clients.conn.Close()

	token, err := m.login(ctx, clients.auth, resolved)
	if err != nil {
		report := Report{Enabled: true, Status: StatusError, Error: err.Error()}
		return file, report, m.persistState(withError(state, err.Error(), StatusError), fmt.Errorf("sync login: %w", err))
	}

	localHash, err := runtimeconfig.HashSnapshot(*conflict.Local.Snapshot)
	if err != nil {
		report := Report{Enabled: true, Status: StatusError, Error: err.Error()}
		return file, report, m.persistState(withError(state, err.Error(), StatusError), err)
	}

	pushResp, err := m.push(ctx, clients.control, token, pushRequest{
		LastSyncAt:      conflict.Cloud.Timestamp,
		LocalModifiedAt: m.now().UTC().UnixMilli(),
		LocalHash:       localHash,
		Snapshot:        *conflict.Local.Snapshot,
	})
	if err != nil {
		report := Report{Enabled: true, Status: StatusError, Error: err.Error()}
		return file, report, m.persistState(withError(state, err.Error(), StatusError), fmt.Errorf("sync push: %w", err))
	}

	switch pushResp.Status {
	case StatusApplied, StatusAlreadySynced:
		nextState := State{
			LastSyncAt:     pushResp.CloudUpdatedAt,
			CloudHash:      pushResp.CloudHash,
			LastSyncStatus: pushResp.Status,
		}
		report := reportFromState(nextState, true)
		return file, report, m.persistState(nextState, nil)
	case StatusConflict:
		conflictState := buildConflictState(
			ConflictReasonPush,
			*conflict.Local.Snapshot,
			localHash,
			m.now().UTC().UnixMilli(),
			pushResp.CloudHash,
			pushResp.CloudUpdatedAt,
			pushResp.Snapshot,
		)
		nextState := State{
			LastSyncAt:     state.LastSyncAt,
			CloudHash:      pushResp.CloudHash,
			LastSyncStatus: StatusConflict,
			LastError:      "sync conflict: cloud snapshot changed before local override could be applied",
			Conflict:       conflictState,
		}
		report := reportFromState(nextState, true)
		return file, report, m.persistState(nextState, nil)
	default:
		err := fmt.Errorf("unexpected sync push status %q", pushResp.Status)
		report := Report{Enabled: true, Status: StatusError, Error: err.Error()}
		return file, report, m.persistState(withError(state, err.Error(), StatusError), err)
	}
}

func (m *Manager) loadConfig() (clientconfig.File, int64, error) {
	file, err := clientconfig.LoadFile(m.configPath)
	if err != nil {
		return clientconfig.File{}, 0, err
	}

	info, err := os.Stat(m.configPath)
	if err != nil {
		return clientconfig.File{}, 0, fmt.Errorf("stat config: %w", err)
	}
	return file, info.ModTime().UTC().UnixMilli(), nil
}

func (m *Manager) loadState() (State, error) {
	statePath := clientconfig.SyncStatePath(m.configPath)
	raw, err := os.ReadFile(statePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return State{}, nil
		}
		return State{}, fmt.Errorf("read sync state: %w", err)
	}

	var state State
	if err := json.Unmarshal(raw, &state); err != nil {
		backupPath, backupErr := m.quarantineCorruptedState(statePath)
		if backupErr != nil {
			return State{}, fmt.Errorf("decode sync state: %v; quarantine corrupted state: %w", err, backupErr)
		}
		return State{}, fmt.Errorf("decode sync state: %v; moved corrupted state to %s", err, backupPath)
	}
	return state, nil
}

func (m *Manager) quarantineCorruptedState(statePath string) (string, error) {
	backupPath := fmt.Sprintf("%s.corrupt-%s", statePath, m.now().UTC().Format("20060102T150405.000000000Z"))
	if err := os.Rename(statePath, backupPath); err != nil {
		return "", err
	}
	return backupPath, nil
}

func (m *Manager) saveState(state State) error {
	raw, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal sync state: %w", err)
	}
	raw = append(raw, '\n')

	if err := clientconfig.WriteFileAtomic(clientconfig.SyncStatePath(m.configPath), raw, 0o600); err != nil {
		return fmt.Errorf("write sync state: %w", err)
	}
	return nil
}

func (m *Manager) persistState(state State, cause error) error {
	if err := m.saveState(state); err != nil {
		if cause != nil {
			return fmt.Errorf("%v; %w", cause, err)
		}
		return err
	}
	return cause
}

func (m *Manager) login(ctx context.Context, client controlv1.AuthServiceClient, cfg clientconfig.ResolvedSyncConfig) (string, error) {
	response, err := client.Login(ctx, &controlv1.LoginRequest{
		Username: cfg.Username,
		Password: cfg.Password,
	})
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(response.GetAccessToken()) == "" {
		return "", errors.New("login returned empty access_token")
	}
	return strings.TrimSpace(response.GetAccessToken()), nil
}

func (m *Manager) fetchStatus(ctx context.Context, client controlv1.CommunityControlServiceClient, token string) (statusResponse, error) {
	response, err := client.Status(withBearerToken(ctx, token), &controlv1.StatusRequest{})
	if err != nil {
		return statusResponse{}, err
	}
	return statusResponse{
		HasSnapshot:    response.GetHasSnapshot(),
		CloudUpdatedAt: response.GetCloudUpdatedAt(),
		CloudHash:      response.GetCloudHash(),
	}, nil
}

func (m *Manager) pull(ctx context.Context, client controlv1.CommunityControlServiceClient, token string, request pullRequest) (pullResponse, error) {
	response, err := client.Pull(withBearerToken(ctx, token), &controlv1.PullRequest{
		LastSyncAt:      request.LastSyncAt,
		LocalModifiedAt: request.LocalModifiedAt,
		LocalHash:       request.LocalHash,
	})
	if err != nil {
		return pullResponse{}, err
	}
	snapshot, err := controlplane.FromProtoSnapshot(response.GetSnapshot())
	if err != nil {
		return pullResponse{}, err
	}
	var snapshotPtr *runtimeconfig.Snapshot
	if response.Snapshot != nil {
		snapshotPtr = &snapshot
	}
	return pullResponse{
		Status:         response.GetStatus(),
		CloudUpdatedAt: response.GetCloudUpdatedAt(),
		CloudHash:      response.GetCloudHash(),
		Snapshot:       snapshotPtr,
	}, nil
}

func (m *Manager) push(ctx context.Context, client controlv1.CommunityControlServiceClient, token string, request pushRequest) (pushResponse, error) {
	response, err := client.Push(withBearerToken(ctx, token), &controlv1.PushRequest{
		LastSyncAt:      request.LastSyncAt,
		LocalModifiedAt: request.LocalModifiedAt,
		LocalHash:       request.LocalHash,
		Snapshot:        controlplane.ToProtoSnapshot(request.Snapshot),
	})
	if err != nil {
		return pushResponse{}, err
	}
	snapshot, err := controlplane.FromProtoSnapshot(response.GetSnapshot())
	if err != nil {
		return pushResponse{}, err
	}
	var snapshotPtr *runtimeconfig.Snapshot
	if response.Snapshot != nil {
		snapshotPtr = &snapshot
	}
	return pushResponse{
		Status:         response.GetStatus(),
		CloudUpdatedAt: response.GetCloudUpdatedAt(),
		CloudHash:      response.GetCloudHash(),
		Snapshot:       snapshotPtr,
	}, nil
}

func (m *Manager) fetchProviders(ctx context.Context, client controlv1.CommunityControlServiceClient, token string) (providersResponse, error) {
	response, err := client.GetProviders(withBearerToken(ctx, token), &controlv1.GetProvidersRequest{})
	if err != nil {
		return providersResponse{}, err
	}
	providerGroups, err := controlplane.FromProtoProviderGroups(response.GetProviderGroups())
	if err != nil {
		return providersResponse{}, err
	}
	return providersResponse{
		Version:        response.GetVersion(),
		CloudUpdatedAt: response.GetCloudUpdatedAt(),
		CloudHash:      response.GetCloudHash(),
		ProviderGroups: providerGroups,
	}, nil
}

func (m *Manager) dialControl(ctx context.Context, serverURL string) (controlClients, error) {
	target, err := controlTarget(serverURL)
	if err != nil {
		return controlClients{}, err
	}

	dialCtx, cancel := context.WithTimeout(ctx, controlDialTimeout)
	defer cancel()

	conn, err := grpc.DialContext(
		dialCtx,
		target,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithBlock(),
	)
	if err != nil {
		return controlClients{}, err
	}

	return controlClients{
		conn:    conn,
		auth:    controlv1.NewAuthServiceClient(conn),
		control: controlv1.NewCommunityControlServiceClient(conn),
	}, nil
}

func controlTarget(serverURL string) (string, error) {
	value := strings.TrimSpace(serverURL)
	if value == "" {
		return "", errors.New("sync server_url is required")
	}

	parsed, err := url.Parse(value)
	if err != nil {
		return "", fmt.Errorf("parse sync server_url: %w", err)
	}
	if parsed.Host != "" {
		return parsed.Host, nil
	}
	if parsed.Scheme == "" && parsed.Path != "" {
		return parsed.Path, nil
	}
	return "", fmt.Errorf("sync server_url %q does not include a dialable host", serverURL)
}

func withBearerToken(ctx context.Context, token string) context.Context {
	return metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+strings.TrimSpace(token))
}

func (m *Manager) doJSON(
	ctx context.Context,
	baseURL string,
	method string,
	path string,
	token string,
	request any,
	allowedStatus []int,
	response any,
) error {
	var body io.Reader
	if request != nil {
		raw, err := json.Marshal(request)
		if err != nil {
			return fmt.Errorf("marshal request: %w", err)
		}
		body = bytes.NewReader(raw)
	}

	httpReq, err := http.NewRequestWithContext(ctx, method, joinURL(baseURL, path), body)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	httpReq.Header.Set("Accept", "application/json")
	if request != nil {
		httpReq.Header.Set("Content-Type", "application/json")
	}
	if strings.TrimSpace(token) != "" {
		httpReq.Header.Set("Authorization", "Bearer "+strings.TrimSpace(token))
	}

	httpResp, err := m.client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer httpResp.Body.Close()

	if !statusAllowed(httpResp.StatusCode, allowedStatus) {
		return decodeAPIError(httpResp)
	}
	if response == nil {
		return nil
	}

	decoder := json.NewDecoder(httpResp.Body)
	if err := decoder.Decode(response); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func reportFromState(state State, enabled bool) Report {
	return Report{
		Enabled:     enabled,
		LastSyncAt:  state.LastSyncAt,
		CloudHash:   state.CloudHash,
		Status:      state.LastSyncStatus,
		Error:       state.LastError,
		HasConflict: state.Conflict != nil,
	}
}

func withError(state State, message string, status string) State {
	state.LastSyncStatus = status
	state.LastError = message
	return state
}

func buildConflictState(
	reason string,
	localSnapshot runtimeconfig.Snapshot,
	localHash string,
	localTimestamp int64,
	cloudHash string,
	cloudTimestamp int64,
	cloudSnapshot *runtimeconfig.Snapshot,
) *ConflictState {
	state := &ConflictState{
		Reason: reason,
		Local: ConflictStateSide{
			Hash:      strings.TrimSpace(localHash),
			Timestamp: localTimestamp,
			Snapshot:  cloneSnapshot(localSnapshot),
		},
		Cloud: ConflictStateSide{
			Hash:      strings.TrimSpace(cloudHash),
			Timestamp: cloudTimestamp,
		},
	}
	if cloudSnapshot != nil {
		state.Cloud.Snapshot = cloneSnapshot(*cloudSnapshot)
	}
	return state
}

func conflictFromState(state *ConflictState, lastError string) Conflict {
	if state == nil {
		return Conflict{}
	}
	return Conflict{
		HasConflict: true,
		Reason:      state.Reason,
		Local: ConflictSnapshot{
			Hash:      state.Local.Hash,
			Timestamp: state.Local.Timestamp,
			Snapshot:  derefSnapshot(state.Local.Snapshot),
		},
		Cloud: ConflictSnapshot{
			Hash:      state.Cloud.Hash,
			Timestamp: state.Cloud.Timestamp,
			Snapshot:  derefSnapshot(state.Cloud.Snapshot),
		},
		Error: lastError,
	}
}

func chooseString(primary, fallback string) string {
	if strings.TrimSpace(primary) != "" {
		return strings.TrimSpace(primary)
	}
	return strings.TrimSpace(fallback)
}

func chooseInt64(primary, fallback int64) int64 {
	if primary != 0 {
		return primary
	}
	return fallback
}

func statusAllowed(status int, allowed []int) bool {
	for _, candidate := range allowed {
		if status == candidate {
			return true
		}
	}
	return false
}

func decodeAPIError(resp *http.Response) error {
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read error response: %w", err)
	}

	var payload struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &payload); err == nil && strings.TrimSpace(payload.Error.Message) != "" {
		return fmt.Errorf("api status %d: %s", resp.StatusCode, strings.TrimSpace(payload.Error.Message))
	}
	if len(bytes.TrimSpace(raw)) == 0 {
		return fmt.Errorf("api status %d", resp.StatusCode)
	}
	return fmt.Errorf("api status %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
}

func joinURL(baseURL, path string) string {
	return strings.TrimRight(strings.TrimSpace(baseURL), "/") + path
}

func cloneSnapshot(snapshot runtimeconfig.Snapshot) *runtimeconfig.Snapshot {
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return nil
	}

	var cloned runtimeconfig.Snapshot
	if err := json.Unmarshal(raw, &cloned); err != nil {
		return nil
	}
	return &cloned
}

func cloneProviderGroups(groups []runtimeconfig.ProviderGroup) []runtimeconfig.ProviderGroup {
	cloned := cloneSnapshot(runtimeconfig.Snapshot{ProviderGroups: groups})
	if cloned == nil {
		return nil
	}
	return cloned.ProviderGroups
}

func derefSnapshot(snapshot *runtimeconfig.Snapshot) runtimeconfig.Snapshot {
	if snapshot == nil {
		return runtimeconfig.Snapshot{}
	}
	cloned := cloneSnapshot(*snapshot)
	if cloned == nil {
		return runtimeconfig.Snapshot{}
	}
	return *cloned
}
