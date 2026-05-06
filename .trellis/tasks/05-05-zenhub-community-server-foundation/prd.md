# ZenHub Phase 3: Community Server Foundation

## Goal

Build the first open-source community server slice as a separate server product
line that depends on shared `core`, not on `client`. This slice should
establish the real `server/community` application boundary and implement the
minimum useful community-server API surface: single-user auth, config snapshot
sync, models listing, and relay foundations.

## What I already know

* The repo now has explicit `internal/core/...` and `internal/client/...`
  boundaries.
* The user explicitly wants the architecture understood as:
  * `core`
  * `client`
  * `server`
* Both `client` and `server` must depend on `core`.
* Current implemented product code is still client-only:
  * headless localhost proxy
  * native desktop GUI shell
* `requirements_CN.md` recommends Phase 3 next:
  * single-user auth
  * config snapshot storage and distribution
  * sync pull/push/status
  * relay API
* The API draft for the open-source server includes:
  * `POST /api/v1/auth/login`
  * `POST /api/v1/sync/pull`
  * `POST /api/v1/sync/push`
  * `GET /api/v1/sync/status`
  * `POST /api/v1/relay/chat/completions`
  * `GET /api/v1/models`
* The requirements say server-side storage should ultimately use MySQL.

## Assumptions (temporary)

* This first community-server slice should establish the server runtime,
  HTTP/API layout, and storage/auth abstractions in a way that future MySQL,
  hosted-server, and billing work can extend cleanly.
* Single-user auth should take the simplest product-valid form for the OSS
  server: username/password login that issues a bearer token, without refresh
  tokens or multi-user complexity.
* Config snapshot sync can store a single user-owned snapshot object plus sync
  metadata rather than introducing a large relational model immediately.
* This task may use a storage interface plus a simple development/test-backed
  implementation first if a full MySQL adapter would make the slice too broad,
  but the package boundaries must clearly reserve `storage/mysql` as the real
  long-term direction.

## Open Questions

* Whether this first slice should ship with a concrete MySQL-backed storage
  adapter immediately, or establish the storage contract and test-backed store
  first while keeping MySQL as the next task.

## Requirements (evolving)

* Add a dedicated community-server binary, separate from `cmd/client` and
  `cmd/gui`.
* Add explicit server package namespaces that do not depend on `client`.
* Reuse `core` for any protocol/canonical/router/balancer/proxy logic that
  applies to relay handling and models listing.
* Implement single-user auth for the community server:
  * login endpoint
  * authenticated bearer token flow for protected endpoints
* Implement sync endpoints:
  * `POST /api/v1/sync/pull`
  * `POST /api/v1/sync/push`
  * `GET /api/v1/sync/status`
* Implement `GET /api/v1/models` for the server-side model view.
* Implement `POST /api/v1/relay/chat/completions` as the first relay-facing
  API surface.
* Keep relay and sync data structures scoped to the community server rather
  than leaking them into `client`.
* Reserve clear extension points for:
  * hosted multi-user auth/refresh
  * official provider catalog
  * balances/billing
  * server-side MySQL persistence

## Acceptance Criteria (evolving)

* [ ] A dedicated community-server binary can be launched from the repo.
* [ ] Community-server code lives under explicit server namespaces and depends
      on `core`, not `client`.
* [ ] `POST /api/v1/auth/login` works for the single-user flow selected by this
      task.
* [ ] `sync/pull`, `sync/push`, and `sync/status` endpoints exist and share a
      coherent snapshot/sync model.
* [ ] `GET /api/v1/models` returns the server-side model view.
* [ ] `POST /api/v1/relay/chat/completions` exists and routes through server
      product code rather than the client localhost API layer.
* [ ] `go test ./...` passes after the server/community code is added.

## Definition Of Done

* Server package boundaries are explicit and documented.
* Core/client/server dependency directions remain clean.
* Tests cover auth, sync contracts, and at least the first relay/models path.
* The chosen storage approach for this slice is documented clearly in code/task
  notes.

## Out Of Scope (explicit)

* Closed-source hosted server features
* Multi-user auth
* Refresh tokens
* Official provider catalog
* Billing and balances
* Client sync UI

## Technical Notes

* Current reusable shared logic:
  * `internal/core/canonical`
  * `internal/core/protocol/openai`
  * `internal/core/router`
  * `internal/core/balancer`
  * `internal/core/transformer`
  * `internal/core/proxy`
* Current client-only logic that must not be reused as server code:
  * `internal/client/app`
  * `internal/client/config`
  * `internal/client/gui`
  * `internal/client/localhostapi`
* Requirements references:
  * Phase 3 in `requirements_CN.md`
  * sections 16, 17, and 19

## Proposed Package Direction

* `cmd/server-community`
* `internal/server/community/app`
* `internal/server/community/api`
* `internal/server/community/auth`
* `internal/server/community/storage`
* `internal/server/community/relay`
* `internal/server/community/sync`

## Decision (ADR-lite)

**Context**: The repo now has clean `core` and `client` boundaries, but no real
server product line yet. The next architectural milestone is to establish
`server/community` as a first-class product that consumes `core` rather than
sharing client-local packages.

**Decision**: Start Phase 3 with a community-server foundation slice that
establishes the server runtime and the smallest meaningful OSS server API
surface: single-user auth, sync endpoints, models listing, and relay
foundations.

**Consequences**: This gives the repo a real client/server split and prepares a
clean base for later hosted-server extensions without mixing those concerns into
the current client runtime.
