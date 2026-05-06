package mysql

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"

	"zenhub/internal/core/runtimeconfig"
	communitystorage "zenhub/internal/server/community/storage"
)

var schemaStatements = []string{
	`CREATE TABLE IF NOT EXISTS config_snapshots (
		version_id BIGINT NOT NULL AUTO_INCREMENT PRIMARY KEY,
		snapshot_json JSON NOT NULL,
		snapshot_hash VARCHAR(64) NOT NULL,
		updated_at BIGINT NOT NULL
	)`,
	`CREATE TABLE IF NOT EXISTS sync_meta (
		singleton_id TINYINT NOT NULL PRIMARY KEY,
		last_pull_at BIGINT NOT NULL DEFAULT 0,
		last_push_at BIGINT NOT NULL DEFAULT 0
	)`,
	`CREATE TABLE IF NOT EXISTS relay_nodes (
		version_id BIGINT NOT NULL,
		provider_group VARCHAR(255) NOT NULL,
		node_name VARCHAR(255) NOT NULL,
		base_url TEXT NOT NULL,
		headers_json JSON NULL,
		PRIMARY KEY (version_id, provider_group, node_name),
		CONSTRAINT fk_relay_nodes_snapshot FOREIGN KEY (version_id)
			REFERENCES config_snapshots(version_id)
			ON DELETE CASCADE
	)`,
}

type Store struct {
	db *sql.DB
}

func Open(dsn string) (*Store, error) {
	if strings.TrimSpace(dsn) == "" {
		return nil, errors.New("mysql DSN is required")
	}

	db, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, fmt.Errorf("open mysql: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping mysql: %w", err)
	}

	return &Store{db: db}, nil
}

func newWithDB(db *sql.DB) *Store {
	return &Store{db: db}
}

func (s *Store) EnsureSchema(ctx context.Context) error {
	for _, statement := range schemaStatements {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("ensure mysql schema: %w", err)
		}
	}
	return nil
}

func (s *Store) CurrentSnapshot(ctx context.Context) (communitystorage.SnapshotRecord, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT version_id, snapshot_json, snapshot_hash, updated_at
		FROM config_snapshots
		ORDER BY version_id DESC
		LIMIT 1
	`)

	var (
		version     int64
		snapshotRaw []byte
		hash        string
		updatedAtMS int64
	)
	if err := row.Scan(&version, &snapshotRaw, &hash, &updatedAtMS); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return communitystorage.SnapshotRecord{}, communitystorage.ErrSnapshotNotFound
		}
		return communitystorage.SnapshotRecord{}, fmt.Errorf("query current snapshot: %w", err)
	}

	var snapshot runtimeconfig.Snapshot
	if err := json.Unmarshal(snapshotRaw, &snapshot); err != nil {
		return communitystorage.SnapshotRecord{}, fmt.Errorf("decode snapshot JSON: %w", err)
	}

	return communitystorage.SnapshotRecord{
		Version:   version,
		UpdatedAt: time.UnixMilli(updatedAtMS).UTC(),
		Hash:      hash,
		Snapshot:  snapshot,
	}, nil
}

func (s *Store) SaveSnapshot(
	ctx context.Context,
	snapshot runtimeconfig.Snapshot,
	hash string,
	updatedAt time.Time,
) (communitystorage.SnapshotRecord, error) {
	snapshotJSON, err := json.Marshal(snapshot)
	if err != nil {
		return communitystorage.SnapshotRecord{}, fmt.Errorf("encode snapshot JSON: %w", err)
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return communitystorage.SnapshotRecord{}, fmt.Errorf("begin snapshot transaction: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	result, err := tx.ExecContext(ctx, `
		INSERT INTO config_snapshots (snapshot_json, snapshot_hash, updated_at)
		VALUES (?, ?, ?)
	`, snapshotJSON, hash, updatedAt.UTC().UnixMilli())
	if err != nil {
		return communitystorage.SnapshotRecord{}, fmt.Errorf("insert snapshot: %w", err)
	}

	versionID, err := result.LastInsertId()
	if err != nil {
		return communitystorage.SnapshotRecord{}, fmt.Errorf("read inserted snapshot version: %w", err)
	}

	if err := insertRelayNodes(ctx, tx, versionID, snapshot); err != nil {
		return communitystorage.SnapshotRecord{}, err
	}

	if err := tx.Commit(); err != nil {
		return communitystorage.SnapshotRecord{}, fmt.Errorf("commit snapshot transaction: %w", err)
	}

	return communitystorage.SnapshotRecord{
		Version:   versionID,
		UpdatedAt: updatedAt.UTC(),
		Hash:      hash,
		Snapshot:  snapshot,
	}, nil
}

func insertRelayNodes(
	ctx context.Context,
	tx *sql.Tx,
	versionID int64,
	snapshot runtimeconfig.Snapshot,
) error {
	for _, group := range snapshot.ProviderGroups {
		for _, node := range group.Nodes {
			var headersJSON []byte
			if node.Headers != nil {
				raw, err := json.Marshal(node.Headers)
				if err != nil {
					return fmt.Errorf("encode relay node headers: %w", err)
				}
				headersJSON = raw
			}

			if _, err := tx.ExecContext(ctx, `
				INSERT INTO relay_nodes (version_id, provider_group, node_name, base_url, headers_json)
				VALUES (?, ?, ?, ?, ?)
			`, versionID, strings.TrimSpace(group.Name), strings.TrimSpace(node.Name), strings.TrimSpace(node.BaseURL), headersJSON); err != nil {
				return fmt.Errorf("insert relay node projection: %w", err)
			}
		}
	}
	return nil
}

func (s *Store) SyncMeta(ctx context.Context) (communitystorage.SyncMeta, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT last_pull_at, last_push_at
		FROM sync_meta
		WHERE singleton_id = 1
	`)

	var lastPullAt, lastPushAt int64
	if err := row.Scan(&lastPullAt, &lastPushAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return communitystorage.SyncMeta{}, nil
		}
		return communitystorage.SyncMeta{}, fmt.Errorf("query sync meta: %w", err)
	}

	return communitystorage.SyncMeta{
		LastPullAt: millisToTime(lastPullAt),
		LastPushAt: millisToTime(lastPushAt),
	}, nil
}

func (s *Store) RecordPull(ctx context.Context, at time.Time) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO sync_meta (singleton_id, last_pull_at, last_push_at)
		VALUES (1, ?, 0)
		ON DUPLICATE KEY UPDATE last_pull_at = VALUES(last_pull_at)
	`, at.UTC().UnixMilli())
	if err != nil {
		return fmt.Errorf("record sync pull: %w", err)
	}
	return nil
}

func (s *Store) RecordPush(ctx context.Context, at time.Time) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO sync_meta (singleton_id, last_pull_at, last_push_at)
		VALUES (1, 0, ?)
		ON DUPLICATE KEY UPDATE last_push_at = VALUES(last_push_at)
	`, at.UTC().UnixMilli())
	if err != nil {
		return fmt.Errorf("record sync push: %w", err)
	}
	return nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func millisToTime(value int64) time.Time {
	if value <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(value).UTC()
}
