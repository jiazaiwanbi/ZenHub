package mysql

import (
	"context"
	"encoding/json"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"

	"zenhub/internal/core/runtimeconfig"
)

func TestStoreEnsureSchema(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	store := newWithDB(db)
	for _, statement := range schemaStatements {
		mock.ExpectExec(regexp.QuoteMeta(statement)).WillReturnResult(sqlmock.NewResult(0, 0))
	}

	if err := store.EnsureSchema(context.Background()); err != nil {
		t.Fatalf("EnsureSchema() error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet() error = %v", err)
	}
}

func TestStoreSaveSnapshotAndLoadCurrentSnapshot(t *testing.T) {
	db, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("sqlmock.New() error = %v", err)
	}
	defer db.Close()

	store := newWithDB(db)
	snapshot := runtimeconfig.Snapshot{
		Routes: []runtimeconfig.Route{
			{
				Model:         "relay-model",
				Mode:          "relay",
				ProviderGroup: "relay",
				UpstreamModel: "upstream-model",
			},
		},
		ProviderGroups: []runtimeconfig.ProviderGroup{
			{
				Name:            "relay",
				Strategy:        "round_robin",
				Timeout:         runtimeconfig.Duration{Duration: 2 * time.Second},
				RetryCount:      0,
				MaxNodeAttempts: 1,
				PassiveHealth: runtimeconfig.PassiveHealthConfig{
					FailureThreshold: 1,
					Cooldown:         runtimeconfig.Duration{Duration: time.Second},
				},
				Nodes: []runtimeconfig.Node{
					{
						Name:    "primary",
						BaseURL: "https://provider.example.com",
						Headers: map[string]string{"X-Test": "1"},
					},
				},
			},
		},
	}

	hash := "abc123"
	updatedAt := time.Unix(1_700_000_000, 0).UTC()
	snapshotJSON, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("json.Marshal(snapshot) error = %v", err)
	}
	headersJSON, err := json.Marshal(map[string]string{"X-Test": "1"})
	if err != nil {
		t.Fatalf("json.Marshal(headers) error = %v", err)
	}

	mock.ExpectBegin()
	mock.ExpectExec(regexp.QuoteMeta(`
		INSERT INTO config_snapshots (snapshot_json, snapshot_hash, updated_at)
		VALUES (?, ?, ?)
	`)).
		WithArgs(snapshotJSON, hash, updatedAt.UnixMilli()).
		WillReturnResult(sqlmock.NewResult(7, 1))
	mock.ExpectExec(regexp.QuoteMeta(`
				INSERT INTO relay_nodes (version_id, provider_group, node_name, base_url, headers_json)
				VALUES (?, ?, ?, ?, ?)
			`)).
		WithArgs(int64(7), "relay", "primary", "https://provider.example.com", headersJSON).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	record, err := store.SaveSnapshot(context.Background(), snapshot, hash, updatedAt)
	if err != nil {
		t.Fatalf("SaveSnapshot() error = %v", err)
	}
	if record.Version != 7 || record.Hash != hash {
		t.Fatalf("record = %#v", record)
	}

	mock.ExpectQuery(regexp.QuoteMeta(`
		SELECT version_id, snapshot_json, snapshot_hash, updated_at
		FROM config_snapshots
		ORDER BY version_id DESC
		LIMIT 1
	`)).
		WillReturnRows(sqlmock.NewRows([]string{"version_id", "snapshot_json", "snapshot_hash", "updated_at"}).
			AddRow(int64(7), snapshotJSON, hash, updatedAt.UnixMilli()))

	current, err := store.CurrentSnapshot(context.Background())
	if err != nil {
		t.Fatalf("CurrentSnapshot() error = %v", err)
	}
	if current.Version != 7 || current.Hash != hash || len(current.Snapshot.ProviderGroups) != 1 {
		t.Fatalf("current = %#v", current)
	}

	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("ExpectationsWereMet() error = %v", err)
	}
}
