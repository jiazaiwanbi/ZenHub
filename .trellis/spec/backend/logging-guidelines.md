# Logging Guidelines

> How logging is done in this project.

---

## Overview

The current runtime uses the Go standard library `log` package only at process
startup and fatal failure boundaries in `cmd/client/main.go`,
`cmd/gui/main.go`, and `cmd/server-community/main.go`.

Request-level telemetry is not emitted through a shared logger. Instead, the
proxy service records bounded request metadata in memory through
`internal/core/observability/recorder.go`.

There is no project-wide structured logging package yet. Do not document one
or assume one exists.

---

## Log Levels

- `log.Printf`: use for coarse process lifecycle messages at the entry point.
  Current examples: announcing the listen address in `cmd/client/main.go` and
  `cmd/server-community/main.go`.
- `log.Fatal` / `log.Fatalf`: use only for unrecoverable startup and server
  failures in `main()`, such as missing config, invalid wiring, runtime start
  failure, GUI bootstrap failure, or MySQL bootstrap failure.
- Package code under `internal/` should usually return errors upward instead of
  logging them locally. Error classification and HTTP mapping already happen in
  `internal/core/proxy` and `internal/client/localhostapi`.

---

## Current Logging Pattern

```go
runtime, err := appcore.NewRuntime(configPath)
if err != nil {
    log.Fatalf("build runtime: %v", err)
}
```

- Keep logging centralized at the process boundary until a real shared logging
  abstraction exists.
- For per-request metadata, prefer `observability.Record` fields such as
  `Model`, `RouteMode`, `SelectedNode`, `RetryCount`, and `FinalStatus`.
- If a future task introduces structured logs, update this guide with the real
  package, schema, and call sites in the same change.

---

## What to Log

- Process startup with the listen address.
- Fatal configuration or dependency wiring failures in `main()`.
- Fatal runtime startup failures that prevent the proxy from serving.
- Request metadata via `internal/core/observability/Recorder`, not raw log lines,
  when the goal is troubleshooting route, node, or retry behavior.

---

## What NOT to Log

- Provider API keys resolved from `api_key` or `api_key_env`.
- Full request bodies, model prompts, or raw upstream responses.
- Duplicate error logs from lower-level packages when the error is already
  returned to `internal/client/localhostapi` or captured in
  `observability.Record.Error`.
- Invented debug-level logging conventions; there is no debug logger today.

---

## Examples

- `cmd/client/main.go`, `cmd/gui/main.go`, and `cmd/server-community/main.go`
  are the only places currently using `log`.
- `internal/core/proxy/service.go` records request outcome metadata instead of
  printing request-scoped logs.
- `internal/core/observability/recorder.go` shows the in-memory record schema.

---

## Common Mistakes

- Adding ad hoc `log.Printf` calls throughout `internal/` packages instead of
  returning typed errors.
- Logging raw prompts or upstream payloads just because they are available in
  canonical or protocol structs.
- Treating the in-memory observability recorder as if it were a permanent log
  sink.
