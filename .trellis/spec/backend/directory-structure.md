# Directory Structure

> How backend code is organized in this project.

---

## Overview

The backend follows a transport-to-core-to-provider split. Runnable shells stay
thin, `internal/app` owns shared runtime bootstrap, HTTP handlers stay thin,
protocol parsing converts input into canonical models, the proxy service owns
route/balance/execute orchestration, and provider-specific transport details
stay below that boundary.

---

## Directory Layout

```
cmd/
├── client/
│   └── main.go
└── gui/
    └── main.go

internal/
├── app/
│   ├── runtime.go
│   └── runtime_test.go
├── balancer/
│   ├── balancer.go
│   └── balancer_test.go
├── canonical/
│   └── models.go
├── config/
│   └── config.go
├── executor/
│   └── direct.go
├── gui/
│   └── window.go
├── observability/
│   └── recorder.go
├── protocol/
│   └── openai/
│       ├── openai.go
│       └── openai_test.go
├── proxy/
│   ├── service.go
│   └── service_test.go
├── router/
│   ├── router.go
│   └── router_test.go
├── server/
│   ├── server.go
│   └── server_test.go
└── transformer/
    ├── openai.go
    └── openai_test.go
```

---

## Module Organization

### Scenario: Local Proxy Execution Chain

#### 1. Scope / Trigger
- Trigger: Adding or modifying a backend request path that crosses runtime
  bootstrap, HTTP ingress, canonical modeling, route selection, load
  balancing, upstream execution, and response writing.

#### 2. Signatures
- Entry points: `cmd/client/main.go`, `cmd/gui/main.go`
- Shared bootstrap: `internal/app.NewRuntime(configPath string) (*Runtime, error)`
- HTTP boundary: `internal/server.New(service ChatService) http.Handler`
- Protocol ingress: `internal/protocol/openai.ParseChatCompletion(io.Reader) (canonical.ChatRequest, error)`
- Core orchestration: `internal/proxy.Service`
- Route policy: `internal/router.Router`
- Load balancing: `internal/balancer.Manager`
- Provider execution: `internal/executor.Direct`
- Provider serialization: `internal/transformer.OpenAIChatRequest(...)`

#### 3. Contracts
- `cmd/client` and `cmd/gui` stay thin. They parse flags, build the shared
  runtime, and hand off to headless or GUI lifecycle code.
- `internal/app` owns config loading, dependency wiring, HTTP server startup,
  and read-only runtime views shared by the CLI and GUI shells.
- `internal/server` maps URLs and HTTP status codes. It should not choose nodes
  or build upstream payloads.
- `internal/protocol/openai` parses/writes OpenAI-compatible payloads and SSE framing.
- `internal/canonical` defines protocol-neutral request/response structs shared
  by router, executor, and transformer layers.
- `internal/proxy` is the only place where route selection, node retries,
  executor calls, and observation recording come together.
- `internal/router` decides `direct` vs `relay` and resolves provider groups.
  It must stay pure and config-driven.
- `internal/balancer` owns node strategy, retry iteration inputs, and passive
  health state.
- `internal/executor` performs upstream HTTP/SSE work. Provider transport code
  belongs here, not in handlers.
- `internal/gui` renders the native shell only. It must consume `internal/app`
  read models instead of re-implementing proxy logic.
- `internal/transformer` rebuilds provider payloads from canonical models and
  converts provider responses back into canonical structures.

#### 4. Validation & Error Matrix
- Need a new API route -> add it in `internal/server`, then call protocol/core
  helpers rather than embedding logic inline.
- Need a new external protocol -> add a new parser/writer package under
  `internal/protocol`, then reuse `internal/proxy.Service`.
- Need a new provider type -> add executor/transformer support under
  `internal/executor` and `internal/transformer` without changing routing or
  handler logic.
- Need GUI/runtime read models -> place them in `internal/app`, not
  `internal/proxy`.

#### 5. Good / Base / Bad Cases
- Good: `internal/app` wires router/balancer/proxy/server once -> handler
  parses request -> proxy service routes and balances -> executor sends
  upstream request -> protocol layer writes JSON/SSE response.
- Base: `/v1/models` reads router state and returns protocol-compatible model
  metadata without touching executors.
- Bad: handler directly builds provider JSON, chooses a node, retries upstream
  requests, and stores GUI view data in one function.

#### 6. Tests Required
- Unit tests for router, balancer, transformer, and protocol parsing when
  behavior changes inside those packages.
- Service tests whenever retries, passive health, or observation behavior
  changes across layers.
- Runtime tests whenever shared startup/shutdown or GUI-facing read models change.
- Handler tests whenever HTTP method gating, error mapping, or streaming
  framing changes.

#### 7. Wrong vs Correct
##### Wrong
```go
func handleChat(w http.ResponseWriter, r *http.Request) {
    // parse JSON, choose provider, retry nodes, write SSE...
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
without rewriting the core dispatch path or duplicating logic in the GUI shell.

---

## Naming Conventions

- Use singular package names for responsibilities: `app`, `router`, `balancer`,
  `executor`, `transformer`, `server`, `gui`.
- Keep provider-specific code inside a shared responsibility package unless a
  second provider makes a subpackage split necessary.
- Use `cmd/<binary>` for runnable entry points and `internal/<responsibility>`
  for application code.

---

## Examples

- `internal/app/runtime.go` shows the shared bootstrap boundary for the
  headless and GUI binaries.
- `internal/server/server.go` shows the thin-handler pattern.
- `internal/proxy/service.go` shows the orchestration boundary where routing,
  balancing, execution, and observation meet.
- `internal/transformer/openai.go` shows provider serialization built from
  canonical models rather than raw handler payloads.
