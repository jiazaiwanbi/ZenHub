# Database Guidelines

> Database patterns and conventions for this project.

---

## Overview

No database layer exists in the repository today.

- There is no ORM or SQL query package under `internal/`.
- There are no migrations, schema files, repository packages, or database tests.
- Runtime state that might otherwise tempt people toward persistence currently
  stays in memory, for example `internal/observability/recorder.go`.
- Configuration is loaded from JSON via `internal/config/config.go` and
  `sample-config.json`; it does not establish a database connection.

This file exists to stop contributors and agents from inventing a persistence
architecture that is not present in the codebase.

---

## Current Reality

- Request routing is config-driven, not database-driven.
- Provider groups, routes, and API keys are resolved from JSON config at
  process start in `config.Load(...)`.
- Observability data is a bounded in-memory slice managed by
  `observability.Recorder`; records disappear on process restart.
- There is no transactional boundary anywhere in the current request path.

---

## Scenario: Introducing Persistence To This Repo

### 1. Scope / Trigger
- Trigger: A task needs durable storage for configuration, request records,
  history, quotas, or any other data that must survive process restarts.

### 2. Signatures
- Current config entry point: `internal/config.Load(path string) (Runtime, error)`
- Current in-memory state holder: `internal/observability.NewRecorder(limit int) *Recorder`
- Current request path has no repository or transaction interface.

### 3. Contracts
- Do not hide database access inside `internal/server`, `internal/router`,
  `internal/balancer`, `internal/executor`, or `internal/transformer`.
- Any future persistence layer must be introduced as an explicit package with a
  clear call site from higher-level orchestration code such as `internal/proxy`.
- Migration tooling, schema ownership, and connection configuration are
  currently undefined and must be documented at the same time the first real
  database code lands.
- Environment-based secret resolution should follow the existing config pattern
  used for provider API keys (`APIKeyEnv` in `internal/config/config.go`).

### 4. Validation & Error Matrix
- Need durable state, but no storage package exists -> create a dedicated
  package and extend this spec in the same task.
- Need quick local persistence and want to write directly from a handler ->
  reject; keep I/O out of `internal/server`.
- Need schema evolution support -> add migration tooling before shipping schema
  changes to multiple environments.
- Need retry or routing state to survive restarts -> document exactly which
  package owns that persistence boundary instead of extending unrelated structs.

### 5. Good / Base / Bad Cases
- Good: a future task introduces an explicit persistence package, wiring,
  tests, and spec updates in one change.
- Base: state stays in memory because the feature does not require durability.
- Bad: add ad hoc SQLite or SQL calls directly inside handlers, routers, or
  executors because "it was faster".

### 6. Tests Required
- When a database is added, include config parsing tests, integration tests for
  the persistence package, and migration coverage where applicable.
- Until then, there are no database-specific tests to run because the layer
  does not exist.

### 7. Wrong vs Correct
#### Wrong
```go
func handleChatCompletions(w http.ResponseWriter, r *http.Request, service ChatService) {
    db.Exec("INSERT INTO requests ...")
}
```

#### Correct
```go
service, err := proxy.New(routerInstance, balancerInstance, directExecutor, observer)
```

Why: the current architecture keeps transport, routing, execution, and
observability concerns separate. Persistence should arrive as a new explicit
dependency, not as hidden side effects inside existing packages.

---

## Naming Conventions

No database naming conventions are established yet because there are no tables,
columns, indexes, or migrations in the repo.

---

## Examples

- `internal/config/config.go` shows that runtime configuration is loaded from a
  JSON file rather than persistent storage.
- `internal/observability/recorder.go` shows the only implemented state store:
  a bounded in-memory recorder.
- `sample-config.json` is the concrete example of how routes and provider
  groups are configured today.

---

## Common Mistakes

- Treating future database ideas in planning docs as if a persistence layer
  already exists in this repo.
- Adding durable-state requirements without updating this spec and the relevant
  Trellis task manifests.
- Coupling storage access to HTTP handlers or low-level transport packages.
