package memory

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"zenhub/internal/core/runtimeconfig"
	communitystorage "zenhub/internal/server/community/storage"
)

type Store struct {
	mu       sync.Mutex
	version  int64
	snapshot *communitystorage.SnapshotRecord
	meta     communitystorage.SyncMeta
}

func NewStore() *Store {
	return &Store{}
}

func (s *Store) EnsureSchema(context.Context) error {
	return nil
}

func (s *Store) CurrentSnapshot(context.Context) (communitystorage.SnapshotRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.snapshot == nil {
		return communitystorage.SnapshotRecord{}, communitystorage.ErrSnapshotNotFound
	}
	return cloneRecord(*s.snapshot), nil
}

func (s *Store) SaveSnapshot(
	_ context.Context,
	snapshot runtimeconfig.Snapshot,
	hash string,
	updatedAt time.Time,
) (communitystorage.SnapshotRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.version++
	record := communitystorage.SnapshotRecord{
		Version:   s.version,
		UpdatedAt: updatedAt.UTC(),
		Hash:      hash,
		Snapshot:  cloneSnapshot(snapshot),
	}
	s.snapshot = &record
	return cloneRecord(record), nil
}

func (s *Store) SyncMeta(context.Context) (communitystorage.SyncMeta, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.meta, nil
}

func (s *Store) RecordPull(_ context.Context, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.meta.LastPullAt = at.UTC()
	return nil
}

func (s *Store) RecordPush(_ context.Context, at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.meta.LastPushAt = at.UTC()
	return nil
}

func (s *Store) Close() error {
	return nil
}

func cloneRecord(record communitystorage.SnapshotRecord) communitystorage.SnapshotRecord {
	record.Snapshot = cloneSnapshot(record.Snapshot)
	return record
}

func cloneSnapshot(snapshot runtimeconfig.Snapshot) runtimeconfig.Snapshot {
	raw, err := json.Marshal(snapshot)
	if err != nil {
		return runtimeconfig.Snapshot{}
	}

	var cloned runtimeconfig.Snapshot
	if err := json.Unmarshal(raw, &cloned); err != nil {
		return runtimeconfig.Snapshot{}
	}
	return cloned
}
