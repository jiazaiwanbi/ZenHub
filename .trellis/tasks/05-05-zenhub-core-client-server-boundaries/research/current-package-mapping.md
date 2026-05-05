# Research: Current Package Mapping For Core / Client / Server Refactor

## Current implemented product scope

The repository currently implements client-side ZenHub capabilities only:

- headless local proxy binary: `cmd/client/main.go`
- native desktop GUI binary: `cmd/gui/main.go`
- shared local runtime bootstrap: `internal/client/app/runtime.go`
- localhost OpenAI-compatible API handler: `internal/client/localhostapi/server.go`

There is no implemented community server or hosted server product yet.

## Current packages by responsibility

### Shared logic candidates

- `internal/core/canonical`
- `internal/core/protocol/openai`
- `internal/core/router`
- `internal/core/balancer`
- `internal/core/transformer`
- `internal/core/proxy`
- `internal/core/observability`
- `internal/core/executor`

These packages define protocol, canonical, routing, balancing, execution
orchestration, and observability behavior that should remain reusable by both
client and future server implementations.

### Client-only logic candidates

- `internal/client/app`
  - currently shared only between the two client binaries
- `internal/client/gui`
  - native desktop GUI only
- `internal/client/config`
  - current runtime config is client-local
- `internal/client/localhostapi`
  - despite the name, this is the client localhost API surface

## Primary naming problem

`internal/server` was the most misleading package name in the repo before the
refactor.

It previously meant:
- the client's local OpenAI-compatible localhost HTTP surface

But future product language requires:
- `server/community`
- closed-source hosted server

So the name `server` must be reclaimed for actual server product code.

## Refactor target

### Core

- `internal/core/canonical`
- `internal/core/protocol/openai`
- `internal/core/router`
- `internal/core/balancer`
- `internal/core/transformer`
- `internal/core/proxy`
- `internal/core/observability`
- `internal/core/executor`

### Client

- `internal/client/app`
- `internal/client/gui`
- `internal/client/config`
- `internal/client/localhostapi`

### Future server

- `internal/server/community/...`
- proprietary hosted server in a separate closed-source layer/repo

## Why do this now

- It prevents future server work from being forced around client-local names.
- It makes the current repo honest about what is implemented today.
- It gives a clean dependency rule:
  - client -> core
  - server -> core
  - no client <-> server dependency
