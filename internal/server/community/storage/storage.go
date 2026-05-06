package storage

import (
	"context"
	"errors"
	"time"

	"zenhub/internal/core/runtimeconfig"
)

var ErrSnapshotNotFound = errors.New("community server snapshot not found")

type SnapshotRecord struct {
	Version   int64
	UpdatedAt time.Time
	Hash      string
	Snapshot  runtimeconfig.Snapshot
}

type SyncMeta struct {
	LastPullAt time.Time
	LastPushAt time.Time
}

type Store interface {
	EnsureSchema(context.Context) error
	CurrentSnapshot(context.Context) (SnapshotRecord, error)
	SaveSnapshot(context.Context, runtimeconfig.Snapshot, string, time.Time) (SnapshotRecord, error)
	SyncMeta(context.Context) (SyncMeta, error)
	RecordPull(context.Context, time.Time) error
	RecordPush(context.Context, time.Time) error
	Close() error
}
