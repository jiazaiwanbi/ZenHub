# ZenHub Architecture Refactor: Core / Client / Server Boundaries

## Goal

Refactor the repository so ZenHub’s codebase clearly separates shared `core`
logic from `client` logic and future `server` logic. The immediate objective is
to stop the current local client runtime and localhost API from occupying names
and package boundaries that will later be needed by the community server and
the closed-source server.

## What I already know

* The requirements define three product shapes:
  * open-source client
  * open-source community/basic server
  * closed-source hosted server
* The user explicitly wants the architecture split as:
  * `core`
  * `client`
  * `server`
* Both `client` and `server` must depend on `core`.
* The current repository only implements client-side capabilities so far:
  * local proxy core
  * native GUI shell
* Before this refactor, package names blurred product boundaries:
  * `internal/server` meant the client localhost API handler
  * `internal/app` meant client runtime/bootstrap
  * shared packages were not grouped under an explicit `core` namespace

## Assumptions (temporary)

* This task should refactor names and package layout first, not implement the
  community server product itself.
* The refactor should preserve current behavior and tests.
* We should prefer clear product boundaries over minimizing path churn.

## Open Questions

* None blocking at the moment. The desired architectural split is explicit.

## Requirements (evolving)

* Introduce an explicit shared `core` package namespace for code used by both
  client and future server implementations.
* Introduce an explicit `client` package namespace for:
  * local runtime/bootstrap
  * localhost-compatible API surface
  * direct-path execution
  * GUI shell
* Reserve an explicit `server` namespace for future server work instead of
  using the current client localhost API package name.
* Refactor current imports and package paths so the current code reflects:
  * client depends on core
  * future server will depend on core
  * client does not depend on server
* Keep runnable entry points thin.
* Preserve current behavior:
  * headless client still starts the local proxy
  * GUI client still starts the same local proxy in-process
  * tests still pass

## Acceptance Criteria (evolving)

* [ ] Shared logic is grouped under an explicit `core` namespace.
* [ ] Client-specific logic is grouped under an explicit `client` namespace.
* [ ] No current client-local package uses the ambiguous top-level name
      `server` for the localhost API layer.
* [ ] `go test ./...` passes after the refactor.
* [ ] Existing client and GUI entry points still build.

## Definition Of Done

* Imports updated consistently across the repo.
* Architecture/spec docs updated where the package boundaries changed.
* `go test ./...` passes.
* `go build ./cmd/client ./cmd/gui` passes.

## Out Of Scope (explicit)

* Implementing the actual community server feature set
* Implementing the closed-source hosted server feature set
* Config sync, auth, relay, billing, or catalog features themselves

## Technical Notes

* Current package inventory after the refactor:
  * `cmd/client`, `cmd/gui`
  * `internal/client/app`, `internal/client/gui`
  * `internal/client/config`
  * `internal/client/localhostapi`
  * `internal/core/canonical`
  * `internal/core/protocol/openai`
  * `internal/core/router`
  * `internal/core/balancer`
  * `internal/core/transformer`
  * `internal/core/proxy`
  * `internal/core/observability`
  * `internal/core/executor`
* The main naming fix in this task is reclaiming `server` for future actual
  server product code by moving the client-local API to
  `internal/client/localhostapi`.

## Proposed Mapping

* Shared `core`
  * `internal/core/canonical`
  * `internal/core/protocol/openai`
  * `internal/core/router`
  * `internal/core/balancer`
  * `internal/core/transformer`
  * `internal/core/proxy`
  * `internal/core/observability`
  * `internal/core/executor`
* Client
  * `internal/client/app`
  * `internal/client/gui`
  * `internal/client/config`
  * `internal/client/localhostapi`
* Future server
  * `internal/server/community/...`
  * closed-source server stays out of this OSS repo or sits in a clearly
    separate proprietary layer

## Decision (ADR-lite)

**Context**: ZenHub will eventually have both client and server products, but
the current repo only implements client functionality. Leaving shared and
client-local code in flat `internal/*` paths will make future community-server
work confusing and increase the risk of mixing product responsibilities.

**Decision**: Refactor now to explicit `core` and `client` namespaces, and
reserve `server` for actual server product code.

**Consequences**: The repository will pay some short-term path churn now, but
future community-server and hosted-server work will slot into a clearer product
architecture with less ambiguity.
