# Quality Guidelines

> Code quality standards for frontend development.

---

## Overview

The native GUI shares the repository's Go toolchain and verification flow.

There is still no separate web/frontend lint or browser-test stack. Quality for
the desktop shell means using the existing Go commands and adding runtime tests
where the GUI depends on shared state or startup behavior.

---

## Forbidden Patterns

- Adding another UI framework or build config without documenting the real
  commands and folder layout introduced by that task.
- Writing frontend spec text that references nonexistent files or invented team
  conventions.
- Duplicating runtime or API contracts by hand without checking
  `internal/app/runtime.go`, `internal/canonical/models.go`, and
  `internal/server/server.go`.

---

## Required Patterns

- Native GUI work should keep stack choices explicit: Fyne views in
  `internal/gui`, shared runtime in `internal/app`.
- Validate runtime-dependent GUI behavior with Go tests around the shared
  runtime boundary.
- If a future frontend consumes the local API, validate behavior against the
  current Go backend endpoints and payload rules.

---

## Testing Requirements

- Current verification commands:
  - `gofmt -w ...`
  - `go vet ./...`
  - `go test ./...`
- Add focused tests when GUI-facing runtime read models or startup/shutdown
  behavior changes.

---

## Examples

- `internal/app/runtime_test.go` is the current example of GUI-facing runtime
  verification.
- Repo verification remains Go-oriented; do not relabel it as a browser stack.

---

## Code Review Checklist

- Does the task keep GUI reads inside `internal/app` instead of reaching into
  proxy or balancer internals?
- Does it use real Go/Fyne commands and files instead of invented web tooling?
- Does it update this spec directory with concrete source-file references?
- Does it avoid claiming established frontend conventions that the repo still
  does not prove?
