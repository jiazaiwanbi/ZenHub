# Bootstrap Research: Current Repo State

## Existing convention sources

- `AGENTS.md`
  - The repo is managed by Trellis.
  - AI assistants should prefer Trellis workflow/tasks/specs.
  - Subagents should be spawned for parallelizable or long-running work.

## Backend reality in the repository

- The backend is a Go project (`go.mod`, module `zenhub`).
- The runnable entry point is `cmd/client/main.go`.
- Current backend packages are:
  - `internal/server`
  - `internal/protocol/openai`
  - `internal/canonical`
  - `internal/router`
  - `internal/balancer`
  - `internal/executor`
  - `internal/transformer`
  - `internal/proxy`
  - `internal/config`
  - `internal/observability`
- The architecture already follows a thin-handler -> canonical model -> router -> balancer -> executor -> transformer split.
- Tests currently exist for:
  - `internal/protocol/openai`
  - `internal/router`
  - `internal/balancer`
  - `internal/proxy`
  - `internal/server`
  - `internal/transformer`

## Backend examples worth referencing in spec

- Entry point wiring: `cmd/client/main.go`
- HTTP routing and error mapping: `internal/server/server.go`
- Canonical request/response models: `internal/canonical/models.go`
- Route policy: `internal/router/router.go`
- Load balancing and passive health: `internal/balancer/balancer.go`
- Upstream execution and SSE handling: `internal/executor/direct.go`
- Core orchestration and observability: `internal/proxy/service.go`
- Provider payload rebuilding from canonical models: `internal/transformer/openai.go`

## Database reality

- No database layer exists yet.
- No ORM, migrations, repositories, or SQL files exist in the repo today.
- The requirements document mentions future SQLite/MySQL usage, but that is not implemented yet.
- Database spec should document the current absence of a database layer and warn future contributors not to invent one ad hoc.

## Logging and observability reality

- There is no shared logging package yet.
- The runtime currently uses the Go standard library `log` package in `cmd/client/main.go`.
- Request metadata observation is stored via `internal/observability/recorder.go`.
- Sensitive request/response bodies are intentionally not stored by default; only metadata is recorded.

## Frontend reality

- There is no frontend source tree yet.
- No React, TypeScript, CSS, GUI component, or web frontend files exist in the repo today.
- `requirements_CN.md` describes a future native Go desktop GUI, but that frontend layer has not started.
- Frontend spec should explicitly say the frontend stack is not implemented yet, and should avoid pretending there are conventions already proven by code.

## Implications for bootstrap spec writing

- Backend spec files should be written from actual Go code already in the repository.
- Frontend spec files should document the current state honestly:
  - no frontend implementation yet
  - no validated component/hook/state/type patterns yet
  - future frontend work must establish real conventions before these docs can become detailed
- Add concrete backend code references where possible.
- Do not write speculative database, frontend, or logging architecture as if it already exists.
