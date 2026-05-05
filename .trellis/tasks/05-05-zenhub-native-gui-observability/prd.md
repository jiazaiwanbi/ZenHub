# ZenHub Phase 2: Native GUI Shell And Observability

## Goal

Build the first native desktop GUI slice for ZenHub on top of the completed
local proxy core. This slice should prove the project can run as a single
desktop client process that both hosts the localhost proxy service and exposes
an operator-facing native GUI for service status and request observability.

## What I already know

* `requirements_CN.md` recommends Phase 2 after the local proxy core:
  native GUI shell, provider management, routing/balancing config pages, and
  request observability.
* Phase 1 local proxy core is already implemented and archived:
  localhost OpenAI-compatible API, canonical pipeline, routing, balancing,
  direct execution, SSE forwarding, and metadata-only observability.
* The repo currently has no GUI/frontend code.
* The frontend bootstrap spec explicitly says future UI tasks must not invent
  conventions unsupported by code.
* The requirements forbid a web admin UI or WebView wrapper for the client GUI;
  the GUI must be native Go.

## Assumptions (temporary)

* The next slice should stay intentionally smaller than the full Phase 2 list.
* The best first GUI demo is read-only status + observability, not editable
  provider/routing configuration yet.
* The first GUI task may target the current macOS development environment for
  smoke testing while preserving a structure intended for later Windows/macOS
  support.
* We will likely need to refactor some proxy startup/runtime code into a shared
  application package so both headless and GUI entry points can reuse it.

## Open Questions

* Should the first GUI binary replace `cmd/client`, or should it be introduced
  as a separate desktop binary that reuses the same core service package?

## Requirements (evolving)

* Add a native Go desktop GUI entry point for the ZenHub client.
* Use `Fyne` as the Phase 2 MVP GUI toolkit.
* Keep the application single-process: GUI and local proxy service run in one
  app instance.
* Start the local proxy service from the GUI application lifecycle.
* Show a service status view with at least:
  * whether the local service is running
  * current listen address
  * loaded models count or list summary
  * basic config file/source information if available
* Show a models/routes overview with at least:
  * one row per exposed model
  * route mode
  * provider group
  * upstream model alias if configured
  * balancing strategy, timeout, retry count, max node attempts, and node count
* Show a request observability view backed by the existing metadata-only
  recorder with at least:
  * request time
  * model
  * route mode
  * selected node
  * balancing strategy
  * retry count
  * duration
  * final status or error
* Do not display full prompt or response bodies by default.
* Prefer in-process runtime/state access for the GUI MVP instead of adding a
  separate admin HTTP API unless a read model boundary is clearly needed.
* Preserve the local proxy core boundaries from Phase 1; the GUI must consume
  runtime state instead of re-implementing routing/execution logic.
* Keep the structure extensible for later:
  * provider management page
  * route/balancer config page
  * sync page
  * auth/account pages

## Acceptance Criteria (evolving)

* [ ] A native GUI window can be launched from the repo.
* [ ] Launching the GUI also starts the local proxy service in-process.
* [ ] The GUI shows service status derived from the running app state.
* [ ] The GUI shows a read-only models/routes overview derived from the loaded
      config/runtime state.
* [ ] The GUI shows recent observation records from the existing recorder.
* [ ] Generating proxy traffic updates the observability view without exposing
      full prompt/response bodies.
* [ ] The implementation does not introduce a WebView or browser-based admin
      interface.
* [ ] Existing `go test ./...` still passes after the refactor.

## Definition Of Done

* Tests added or updated for any refactored runtime/service boundaries.
* `go test ./...` passes.
* GUI launch flow is smoke-tested in the current environment.
* The chosen GUI toolkit and resulting architecture trade-offs are recorded in
  task research/spec notes.

## Out Of Scope (explicit)

* Editable provider CRUD UI
* Editable route and load balancer configuration UI
* Config snapshot sync
* Login, token refresh, or hosted-service account features
* Relay implementation work beyond what Phase 1 already exposed
* Claude/Gemini/Codex protocol expansion

## Technical Notes

* Local proxy core references:
  * `cmd/client/main.go`
  * `internal/proxy/service.go`
  * `internal/server/server.go`
  * `internal/observability/recorder.go`
* Frontend/UI constraints:
  * `.trellis/spec/frontend/*.md`
* Backend architecture constraints:
  * `.trellis/spec/backend/directory-structure.md`
  * `.trellis/spec/backend/error-handling.md`
  * `.trellis/spec/backend/quality-guidelines.md`

## Research References

* `research/go-native-gui-frameworks.md`
  * Recommendation: use `Fyne` for the MVP because it fits the Go-only repo,
    supports a fast single-process desktop shell, and has enough widgets for
    status/observability views.
* `research/gui-mvp-scope.md`
  * Recommendation: keep the first GUI slice read-only and centered on service
    status, models/routes overview, and recent request observability.

## Decision (ADR-lite)

**Context**: Phase 2 requires a native Go GUI, but the repo has no existing UI
stack and no admin/status API beyond inference endpoints.

**Decision**: Implement a read-only MVP desktop shell using `Fyne`, backed by
shared in-process runtime state from the local proxy core. The first GUI scope
is limited to service status, routes/models overview, and recent request
observability.

**Consequences**: This keeps the product narrative moving without dragging in
config mutation, secrets UX, sync, or auth. It also likely requires a runtime
refactor so both the headless client and GUI entry points can reuse the same
application core.
