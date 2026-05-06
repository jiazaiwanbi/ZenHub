# Directory Structure

> How backend code is organized in this project.

---

## Overview

The backend follows an explicit `core` / `client` / `server` split.
Runnable shells stay thin, `internal/core/` owns transport-agnostic request
machinery and shared runtime-config models, `internal/client/` owns the local
desktop product, `internal/server/community/` owns the open-source server
product, HTTP handlers stay thin, and provider-specific transport details stay
below those product boundaries.

---

## Directory Layout

```
cmd/
├── client/
│   └── main.go
├── gui/
│   └── main.go
└── server-community/
    └── main.go

internal/
├── client/
│   ├── app/
│   │   ├── runtime.go
│   │   └── runtime_test.go
│   ├── config/
│   │   └── config.go
│   ├── gui/
│   │   └── window.go
│   └── localhostapi/
│       ├── server.go
│       └── server_test.go
├── core/
│   ├── balancer/
│   │   ├── balancer.go
│   │   └── balancer_test.go
│   ├── canonical/
│   │   └── models.go
│   ├── executor/
│   │   └── direct.go
│   ├── observability/
│   │   └── recorder.go
│   ├── protocol/
│   │   └── openai/
│   │       ├── openai.go
│   │       └── openai_test.go
│   ├── proxy/
│   │   ├── service.go
│   │   └── service_test.go
│   ├── router/
│   │   ├── router.go
│   │   └── router_test.go
│   ├── runtimeconfig/
│   │   └── config.go
│   └── transformer/
│       ├── openai.go
│       └── openai_test.go
└── server/
    └── community/
        ├── api/
        │   ├── server.go
        │   └── server_test.go
        ├── app/
        │   └── runtime.go
        ├── auth/
        │   ├── service.go
        │   └── service_test.go
        ├── config/
        │   └── config.go
        ├── relay/
        │   └── service.go
        ├── storage/
        │   ├── memory/
        │   │   └── store.go
        │   ├── mysql/
        │   │   ├── store.go
        │   │   └── store_test.go
        │   └── storage.go
        └── sync/
            ├── service.go
            └── service_test.go
```

---

## Module Organization

### Scenario: Client Local Proxy And Community Server

#### 1. Scope / Trigger
- Trigger: Adding or modifying a backend request path that crosses runtime
  bootstrap, HTTP ingress, canonical modeling, route selection, load
  balancing, upstream execution, sync, auth, or persistence.

#### 2. Signatures
- Entry points: `cmd/client/main.go`, `cmd/gui/main.go`,
  `cmd/server-community/main.go`
- Shared client bootstrap: `internal/client/app.NewRuntime(configPath string) (*Runtime, error)`
- Shared runtime-config loading: `internal/core/runtimeconfig.Load(path string) (Runtime, error)`
- Client localhost API boundary: `internal/client/localhostapi.New(service ChatService) http.Handler`
- Community server bootstrap: `internal/server/community/app.NewRuntime(config.Config) (*Runtime, error)`
- Community server API boundary: `internal/server/community/api.New(...) http.Handler`
- Community server sync contract: `internal/server/community/sync.Service`
- Community server MySQL storage boundary: `internal/server/community/storage/mysql.Store`
- Protocol ingress: `internal/core/protocol/openai.ParseChatCompletion(io.Reader) (canonical.ChatRequest, error)`
- Core orchestration: `internal/core/proxy.Service`
- Route policy: `internal/core/router.Router`
- Load balancing: `internal/core/balancer.Manager`
- Provider execution: `internal/core/executor.Direct`
- Provider serialization: `internal/core/transformer.OpenAIChatRequest(...)`

#### 3. Contracts
- `cmd/client` and `cmd/gui` stay thin. They parse flags, build the shared
  runtime, and hand off to headless or GUI lifecycle code.
- `cmd/server-community` stays thin. It reads env/flag config, builds the
  community-server runtime, logs the listen address, and handles process
  shutdown only.
- `internal/client/app` owns client config loading, dependency wiring, HTTP
  server startup, and read-only runtime views shared by the headless and GUI
  client shells.
- `internal/core/runtimeconfig` owns shared route/provider-group/snapshot
  models that both `client` and `server` products can consume without creating
  a product-boundary dependency.
- `internal/client/localhostapi` maps the client-local URLs and HTTP status
  codes. It should not choose nodes or build upstream payloads.
- `internal/server/community/api` maps authenticated `/api/v1/*` community
  server endpoints. It should not embed sync conflict rules, token signing, or
  MySQL access inline.
- `internal/core/protocol/openai` parses/writes OpenAI-compatible payloads and
  SSE framing.
- `internal/core/canonical` defines protocol-neutral request/response structs
  shared by router, executor, and transformer layers.
- `internal/core/proxy` is the only place where route selection, node retries,
  executor calls, and observation recording come together. Product-specific
  route-mode behavior must be enabled through constructor options rather than
  copied into handlers.
- `internal/core/router` decides `direct` vs `relay` and resolves provider
  groups. It must stay pure and config-driven.
- `internal/core/balancer` owns node strategy, retry iteration inputs, and
  passive health state.
- `internal/core/executor` performs upstream HTTP/SSE work. Provider transport
  code belongs here, not in handlers.
- `internal/client/gui` renders the native shell only. It must consume
  `internal/client/app` read models instead of re-implementing proxy logic.
- `internal/server/community/auth` owns single-user login and bearer-token
  verification.
- `internal/server/community/sync` owns config snapshot hashing and
  `last_sync_at + updated_at + hash` conflict logic.
- `internal/server/community/storage/mysql` owns community-server persistence,
  schema bootstrap, and relay-node projection tables.
- `internal/core/transformer` rebuilds provider payloads from canonical models
  and converts provider responses back into canonical structures.

#### 4. Validation & Error Matrix
- Need a new client-local API route -> add it in `internal/client/localhostapi`,
  then call protocol/core helpers rather than embedding logic inline.
- Need a new community-server endpoint -> add it in
  `internal/server/community/api`, then call `auth`, `sync`, `relay`, or
  storage-backed services rather than embedding SQL or token logic inline.
- Need shared route/provider-group snapshot schema -> place it in
  `internal/core/runtimeconfig`, not `internal/client/config` or
  `internal/server/community/config`.
- Need a new external protocol -> add a new parser/writer package under
  `internal/core/protocol`, then reuse `internal/core/proxy.Service`.
- Need a new provider type -> add executor/transformer support under
  `internal/core/executor` and `internal/core/transformer` without changing
  routing or handler logic.
- Need GUI/runtime read models -> place them in `internal/client/app`, not
  `internal/core/proxy`.

#### 5. Good / Base / Bad Cases
- Good: `internal/client/app` wires router/balancer/proxy/localhost API once ->
  handler parses request -> proxy service routes and balances -> executor sends
  upstream request -> protocol layer writes JSON/SSE response.
- Good: `internal/server/community/app` wires MySQL storage, auth, sync, relay,
  and API once -> authenticated handler delegates to sync/relay services ->
  relay reuses `internal/core/proxy` with server-specific options.
- Base: `/v1/models` reads router state and returns protocol-compatible model
  metadata without touching executors.
- Bad: handler directly builds provider JSON, chooses a node, retries upstream
  requests, signs tokens, and writes SQL in one function.

#### 6. Tests Required
- Unit tests for router, balancer, transformer, and protocol parsing when
  behavior changes inside those packages.
- Service tests whenever retries, passive health, or observation behavior
  changes across layers.
- Runtime tests whenever shared startup/shutdown or GUI-facing read models
  change.
- Handler tests whenever HTTP method gating, error mapping, or streaming
  framing changes.
- Storage tests whenever schema bootstrap or snapshot persistence changes.

#### 7. Wrong vs Correct
##### Wrong
```go
func handleChat(w http.ResponseWriter, r *http.Request) {
    // parse JSON, choose provider, retry nodes, write SSE, sign tokens, save SQL...
}
```

##### Correct
```go
func handleChatCompletions(w http.ResponseWriter, r *http.Request, service ChatService) {
    request, err := openaiprotocol.ParseChatCompletion(r.Body)
    // hand off to proxy/service for route + balance + execute
}
```

Why: the project’s main extensibility promise is protocol/provider growth
without rewriting the shared core dispatch path or duplicating product-specific
logic in handlers.

---

## Naming Conventions

- Use singular package names for responsibilities inside each namespace:
  `app`, `localhostapi`, `router`, `balancer`, `executor`, `transformer`,
  `gui`, `api`, `auth`, `relay`, `sync`, `storage`.
- Keep provider-specific code inside a shared responsibility package unless a
  second provider makes a subpackage split necessary.
- Use `cmd/<binary>` for runnable entry points and `internal/<responsibility>`
  for application code.

---

## Examples

- `internal/client/app/runtime.go` shows the shared bootstrap boundary for the
  headless and GUI client binaries.
- `internal/server/community/app/runtime.go` shows the community-server
  bootstrap boundary for env config, MySQL storage, auth, and API wiring.
- `internal/client/localhostapi/server.go` shows the thin-handler pattern for
  the client localhost API.
- `internal/server/community/api/server.go` shows the thin-handler pattern for
  the authenticated community-server API.
- `internal/core/proxy/service.go` shows the orchestration boundary where
  routing, balancing, execution, and observation meet.
- `internal/core/runtimeconfig/config.go` shows the shared config-snapshot
  model consumed by both product lines.
- `internal/core/transformer/openai.go` shows provider serialization built from
  canonical models rather than raw handler payloads.
