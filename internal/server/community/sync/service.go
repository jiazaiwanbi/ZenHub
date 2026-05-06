package sync

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"zenhub/internal/core/runtimeconfig"
	communitystorage "zenhub/internal/server/community/storage"
)

const (
	StatusEmpty         = "empty"
	StatusUpdated       = "updated"
	StatusUpToDate      = "up_to_date"
	StatusConflict      = "conflict"
	StatusClientNewer   = "client_newer"
	StatusApplied       = "applied"
	StatusAlreadySynced = "already_synced"
)

type Status struct {
	HasSnapshot    bool   `json:"has_snapshot"`
	Version        int64  `json:"version,omitempty"`
	CloudUpdatedAt int64  `json:"cloud_updated_at,omitempty"`
	CloudHash      string `json:"cloud_hash,omitempty"`
	LastPullAt     int64  `json:"last_pull_at,omitempty"`
	LastPushAt     int64  `json:"last_push_at,omitempty"`
}

type PullRequest struct {
	LastSyncAt      int64  `json:"last_sync_at"`
	LocalModifiedAt int64  `json:"local_modified_at"`
	LocalHash       string `json:"local_hash"`
}

type PullResponse struct {
	Status         string                  `json:"status"`
	Version        int64                   `json:"version,omitempty"`
	CloudUpdatedAt int64                   `json:"cloud_updated_at,omitempty"`
	CloudHash      string                  `json:"cloud_hash,omitempty"`
	Snapshot       *runtimeconfig.Snapshot `json:"snapshot,omitempty"`
}

type PushRequest struct {
	LastSyncAt      int64                  `json:"last_sync_at"`
	LocalModifiedAt int64                  `json:"local_modified_at"`
	LocalHash       string                 `json:"local_hash"`
	Snapshot        runtimeconfig.Snapshot `json:"snapshot"`
}

type PushResponse struct {
	Status         string                  `json:"status"`
	Version        int64                   `json:"version,omitempty"`
	CloudUpdatedAt int64                   `json:"cloud_updated_at,omitempty"`
	CloudHash      string                  `json:"cloud_hash,omitempty"`
	Snapshot       *runtimeconfig.Snapshot `json:"snapshot,omitempty"`
}

type Service struct {
	store communitystorage.Store
	now   func() time.Time
}

func NewService(store communitystorage.Store) *Service {
	return &Service{
		store: store,
		now:   time.Now,
	}
}

func (s *Service) Status(ctx context.Context) (Status, error) {
	meta, err := s.store.SyncMeta(ctx)
	if err != nil {
		return Status{}, err
	}

	current, err := s.store.CurrentSnapshot(ctx)
	if err != nil {
		if errors.Is(err, communitystorage.ErrSnapshotNotFound) {
			return Status{
				LastPullAt: timeToMillis(meta.LastPullAt),
				LastPushAt: timeToMillis(meta.LastPushAt),
			}, nil
		}
		return Status{}, err
	}

	return Status{
		HasSnapshot:    true,
		Version:        current.Version,
		CloudUpdatedAt: current.UpdatedAt.UnixMilli(),
		CloudHash:      current.Hash,
		LastPullAt:     timeToMillis(meta.LastPullAt),
		LastPushAt:     timeToMillis(meta.LastPushAt),
	}, nil
}

func (s *Service) Pull(ctx context.Context, req PullRequest) (PullResponse, error) {
	now := s.now().UTC()
	if err := s.store.RecordPull(ctx, now); err != nil {
		return PullResponse{}, err
	}

	current, err := s.store.CurrentSnapshot(ctx)
	if err != nil {
		if errors.Is(err, communitystorage.ErrSnapshotNotFound) {
			return PullResponse{Status: StatusEmpty}, nil
		}
		return PullResponse{}, err
	}

	localHash := strings.TrimSpace(req.LocalHash)
	if localHash != "" && localHash == current.Hash {
		return pullResponse(StatusUpToDate, current, nil), nil
	}

	cloudChanged := cloudChangedSince(current, req.LastSyncAt, localHash)
	localChanged := localChangedSince(req.LastSyncAt, req.LocalModifiedAt, localHash, current.Hash)

	switch {
	case localChanged && cloudChanged:
		return pullResponse(StatusConflict, current, &current.Snapshot), nil
	case cloudChanged || req.LastSyncAt <= 0 || localHash == "":
		return pullResponse(StatusUpdated, current, &current.Snapshot), nil
	case localChanged:
		return pullResponse(StatusClientNewer, current, nil), nil
	default:
		return pullResponse(StatusUpToDate, current, nil), nil
	}
}

func (s *Service) Push(ctx context.Context, req PushRequest) (PushResponse, error) {
	if err := req.Snapshot.Validate(); err != nil {
		return PushResponse{}, fmt.Errorf("validate pushed snapshot: %w", err)
	}

	computedHash, err := HashSnapshot(req.Snapshot)
	if err != nil {
		return PushResponse{}, err
	}

	localHash := strings.TrimSpace(req.LocalHash)
	if localHash == "" {
		localHash = computedHash
	}
	if localHash != computedHash {
		return PushResponse{}, errors.New("local_hash does not match snapshot content")
	}

	current, err := s.store.CurrentSnapshot(ctx)
	if err != nil {
		if !errors.Is(err, communitystorage.ErrSnapshotNotFound) {
			return PushResponse{}, err
		}

		record, saveErr := s.saveSnapshot(ctx, req.Snapshot, computedHash)
		if saveErr != nil {
			return PushResponse{}, saveErr
		}
		return pushResponse(StatusApplied, record, nil), nil
	}

	if current.Hash == computedHash {
		if err := s.store.RecordPush(ctx, s.now().UTC()); err != nil {
			return PushResponse{}, err
		}
		return pushResponse(StatusAlreadySynced, current, nil), nil
	}

	if req.LastSyncAt <= 0 || current.UpdatedAt.UnixMilli() > req.LastSyncAt && current.Hash != localHash {
		return pushResponse(StatusConflict, current, &current.Snapshot), nil
	}

	record, err := s.saveSnapshot(ctx, req.Snapshot, computedHash)
	if err != nil {
		return PushResponse{}, err
	}
	return pushResponse(StatusApplied, record, nil), nil
}

func (s *Service) Bootstrap(ctx context.Context, snapshot runtimeconfig.Snapshot) (communitystorage.SnapshotRecord, error) {
	if err := snapshot.Validate(); err != nil {
		return communitystorage.SnapshotRecord{}, fmt.Errorf("validate bootstrap snapshot: %w", err)
	}

	current, err := s.store.CurrentSnapshot(ctx)
	if err == nil {
		return current, nil
	}
	if !errors.Is(err, communitystorage.ErrSnapshotNotFound) {
		return communitystorage.SnapshotRecord{}, err
	}

	hash, err := HashSnapshot(snapshot)
	if err != nil {
		return communitystorage.SnapshotRecord{}, err
	}
	return s.saveSnapshot(ctx, snapshot, hash)
}

func (s *Service) saveSnapshot(
	ctx context.Context,
	snapshot runtimeconfig.Snapshot,
	hash string,
) (communitystorage.SnapshotRecord, error) {
	now := s.now().UTC()
	record, err := s.store.SaveSnapshot(ctx, snapshot, hash, now)
	if err != nil {
		return communitystorage.SnapshotRecord{}, err
	}
	if err := s.store.RecordPush(ctx, now); err != nil {
		return communitystorage.SnapshotRecord{}, err
	}
	return record, nil
}

func HashSnapshot(snapshot runtimeconfig.Snapshot) (string, error) {
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return "", fmt.Errorf("marshal snapshot for hashing: %w", err)
	}

	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:]), nil
}

func pullResponse(status string, record communitystorage.SnapshotRecord, snapshot *runtimeconfig.Snapshot) PullResponse {
	return PullResponse{
		Status:         status,
		Version:        record.Version,
		CloudUpdatedAt: record.UpdatedAt.UnixMilli(),
		CloudHash:      record.Hash,
		Snapshot:       snapshot,
	}
}

func pushResponse(status string, record communitystorage.SnapshotRecord, snapshot *runtimeconfig.Snapshot) PushResponse {
	return PushResponse{
		Status:         status,
		Version:        record.Version,
		CloudUpdatedAt: record.UpdatedAt.UnixMilli(),
		CloudHash:      record.Hash,
		Snapshot:       snapshot,
	}
}

func cloudChangedSince(record communitystorage.SnapshotRecord, lastSyncAt int64, localHash string) bool {
	if lastSyncAt <= 0 {
		return strings.TrimSpace(localHash) != record.Hash
	}
	return record.UpdatedAt.UnixMilli() > lastSyncAt && strings.TrimSpace(localHash) != record.Hash
}

func localChangedSince(lastSyncAt, localModifiedAt int64, localHash, cloudHash string) bool {
	if strings.TrimSpace(localHash) == "" {
		return false
	}
	if lastSyncAt <= 0 {
		return false
	}
	return localModifiedAt > lastSyncAt && strings.TrimSpace(localHash) != cloudHash
}

func timeToMillis(value time.Time) int64 {
	if value.IsZero() {
		return 0
	}
	return value.UTC().UnixMilli()
}
