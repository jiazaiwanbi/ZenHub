# Database Guidelines

> Database patterns and conventions for this project.

---

## Overview

The repository now has one explicit persistence layer: the open-source
community server stores sync snapshots in MySQL under
`internal/server/community/storage/mysql`.

- There is still no ORM in the repo; persistence uses `database/sql`.
- Client product code is not database-backed. The desktop client still loads
  config from JSON and keeps observability data in memory.
- The MySQL adapter bootstraps its own initial schema through
  `EnsureSchema(context.Context)`.
- Persistence is product-scoped. `internal/core/` remains free of SQL calls.

---

## Current Reality

- Client request routing is config-driven, not database-driven.
- Community-server routing still executes from a config snapshot, but that
  snapshot is loaded from MySQL rather than from a local JSON file.
- Observability data remains a bounded in-memory slice managed by
  `observability.Recorder`; records disappear on process restart.
- The current transactional boundary exists inside
  `internal/server/community/storage/mysql.SaveSnapshot(...)` when a config
  snapshot row and its relay-node projection rows are inserted together.

---

## Scenario: Community Server MySQL Persistence

### 1. Scope / Trigger
- Trigger: A task needs durable server-side state such as config snapshots,
  sync metadata, or future hosted-server records.

### 2. Signatures
- MySQL adapter constructor:
  `internal/server/community/storage/mysql.Open(dsn string) (*Store, error)`
- Schema bootstrap:
  `(*Store).EnsureSchema(context.Context) error`
- Snapshot persistence:
  `(*Store).SaveSnapshot(context.Context, runtimeconfig.Snapshot, string, time.Time) (storage.SnapshotRecord, error)`
- Snapshot read path:
  `(*Store).CurrentSnapshot(context.Context) (storage.SnapshotRecord, error)`
- Sync metadata path:
  `(*Store).SyncMeta(context.Context) (storage.SyncMeta, error)`

### 3. Contracts
- Do not hide database access inside `internal/client/localhostapi`,
  `internal/core/router`, `internal/core/balancer`, `internal/core/executor`,
  or `internal/core/transformer`.
- Community-server DB access belongs under `internal/server/community/storage/`
  and is wired from `internal/server/community/app`.
- Required env contract for the current server product:
  - `ZENHUB_SERVER_DATABASE_DSN`
  - `ZENHUB_SERVER_ADMIN_USERNAME`
  - `ZENHUB_SERVER_ADMIN_PASSWORD`
  - `ZENHUB_SERVER_TOKEN_SECRET`
  - optional: `ZENHUB_SERVER_LISTEN`, `ZENHUB_SERVER_TOKEN_TTL`, `ZENHUB_SERVER_BOOTSTRAP_CONFIG`
- Current schema ownership:
  - `config_snapshots`: versioned JSON snapshots plus hash and update time
  - `sync_meta`: singleton pull/push timestamps
  - `relay_nodes`: denormalized relay-node projection for each saved snapshot version
- The current migration story is intentionally simple:
  `EnsureSchema(...)` executes `CREATE TABLE IF NOT EXISTS` bootstrap
  statements. If schema evolution beyond initial bootstrap is needed, add
  explicit migration tooling in the same task.

### 4. Validation & Error Matrix
- Missing or blank MySQL DSN -> runtime construction error
- MySQL ping failure -> runtime construction error
- No saved snapshot yet -> `storage.ErrSnapshotNotFound`
- Malformed persisted snapshot JSON -> storage read error
- Need durable state outside the current community-server scope -> add a
  dedicated product-scoped storage package and update this spec in the same task
- Need quick local persistence and want to write directly from a handler ->
  reject; keep I/O out of HTTP handlers
- Need schema evolution beyond bootstrap DDL -> add migration tooling before
  shipping multiple schema generations

### 5. Good / Base / Bad Cases
- Good: `internal/server/community/app` opens MySQL once, calls
  `EnsureSchema(...)`, and the sync service uses the store interface to read or
  save snapshots.
- Base: state that does not need durability stays in memory, for example
  `internal/core/observability.Recorder`.
- Bad: add ad hoc SQL calls directly inside handlers, routers, or executors
  because "it was faster".

### 6. Tests Required
- MySQL adapter tests for schema bootstrap statements and snapshot save/load
  behavior.
- Sync-service tests for conflict rules on top of the storage interface.
- API tests proving push/pull/models/relay behavior against a store-backed
  server handler.

### 7. Wrong vs Correct
#### Wrong
```go
func handlePush(w http.ResponseWriter, r *http.Request) {
    db.Exec("INSERT INTO config_snapshots ...")
}
```

#### Correct
```go
store, err := mysqlstorage.Open(cfg.DatabaseDSN)
syncService := communitysync.NewService(store)
```

Why: the current architecture keeps transport, routing, execution, and
observability concerns separate. Persistence belongs to an explicit
product-scoped dependency, not to handlers or shared core packages.

---

## Naming Conventions

- Use singular package names for storage responsibilities: `storage`,
  `storage/mysql`, `storage/memory`.
- Use `*_at` for UTC millisecond timestamp columns stored as `BIGINT`.
- Use `*_json` for JSON blob columns and `*_hash` for canonical snapshot hashes.

---

## Examples

- `internal/server/community/storage/mysql/store.go` shows the explicit MySQL
  persistence boundary.
- `internal/server/community/storage/mysql/store_test.go` shows the expected
  schema bootstrap and snapshot save/load behavior.
- `internal/server/community/storage/memory/store.go` shows the in-memory test
  implementation used by sync and API tests.
- `internal/core/runtimeconfig/config.go` shows the shared config snapshot
  model that is serialized into MySQL.

---

## Common Mistakes

- Treating the community-server MySQL layer as if it were a generic shared
  persistence abstraction for all products.
- Adding durable-state requirements without updating this spec and the relevant
  Trellis task manifests.
- Coupling storage access to HTTP handlers or low-level transport packages.
- Reusing `internal/client/config` as if it were a server storage layer instead
  of going through `internal/core/runtimeconfig` plus
  `internal/server/community/storage`.
