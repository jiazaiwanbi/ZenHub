# ZenHub Phase 1: Local Proxy Core

## Goal

Implement the first ZenHub delivery slice from `requirements_CN.md`: a Go-based local AI proxy core that exposes OpenAI-compatible localhost APIs, keeps protocol parsing separate from routing and upstream execution, and demonstrates the architecture needed for later desktop GUI, sync, relay, and hosted-service work.

## Requirements

* Initialize the repository as a Go project named `zenhub`.
* Provide a runnable local proxy service under `cmd/client`.
* Expose `POST /v1/chat/completions` and `GET /v1/models`.
* Implement an OpenAI-compatible Chat Completions ingress handler.
* Convert incoming protocol payloads into an internal canonical request model before routing or execution.
* Keep canonical models, protocol parsers, transformers, router, balancer, executor, and observability in separate packages.
* Implement route selection where each model maps to a route mode: `direct` or `relay`.
* For this phase, implement direct execution against OpenAI-compatible upstream nodes.
* Preserve relay as a route mode in config/types, but return a clear not-implemented error if selected.
* Implement provider groups with node pools and the following configurable parameters:
  * `strategy`: `round_robin` or `fill_first`
  * `timeout`
  * `retry_count`
  * `max_node_attempts`
  * passive health failure threshold and cooldown
* Implement load balancing as a standalone module.
* Implement passive health handling:
  * failed attempts increment node failure counts
  * nodes become unhealthy after threshold failures
  * unhealthy nodes become selectable again after cooldown
* Enforce that request failure never switches between `direct` and `relay`; retries only happen inside the selected path's node pool.
* Implement non-streaming OpenAI-compatible upstream forwarding.
* Implement streaming forwarding for OpenAI-compatible SSE responses.
* Record request observations with metadata only:
  * request time
  * model
  * route mode
  * selected node
  * load balancing strategy
  * retry count
  * duration
  * final status or error
* Provide sensible sample configuration for local development.
* Add focused tests for canonical parsing, route selection, load balancing, passive health, and chat handler behavior.

## Acceptance Criteria

* [ ] `go test ./...` passes.
* [ ] Running `go run ./cmd/client -config <sample-config>` starts a localhost server.
* [ ] `GET /v1/models` returns models derived from configured route rules/provider groups.
* [ ] `POST /v1/chat/completions` accepts OpenAI-style JSON and routes through canonical request models.
* [ ] Non-streaming requests are forwarded to a configured OpenAI-compatible upstream and returned in OpenAI-compatible format.
* [ ] Streaming requests preserve SSE framing and terminate correctly.
* [ ] `round_robin` rotates across healthy nodes.
* [ ] `fill_first` keeps using the current healthy node until it fails or becomes unhealthy.
* [ ] `retry_count` applies per selected node and `max_node_attempts` caps node switching inside the selected group.
* [ ] A failing direct route does not fall back to relay.
* [ ] Request observation records never store full prompt or full response bodies by default.

## Definition Of Done

* Tests added or updated for core behavior.
* `go test ./...` passes.
* Code structure reflects the layered design from `requirements_CN.md`.
* Any implementation-specific architectural decisions are captured in Trellis specs during finish.

## Technical Approach

Use a conservative Go standard-library-first backend implementation:

* `cmd/client` owns CLI flags, config loading, service construction, and server startup.
* `internal/server` owns HTTP route registration and error responses.
* `internal/protocol/openai` parses OpenAI-compatible requests and writes OpenAI-compatible/SSE responses.
* `internal/canonical` defines internal request/response/stream chunk types.
* `internal/router` resolves model aliases to a stable route decision.
* `internal/balancer` owns node selection, retry iteration, and passive health state.
* `internal/executor` owns provider execution. Phase 1 includes an OpenAI-compatible executor and a relay placeholder.
* `internal/transformer` maps canonical requests to provider-specific HTTP payloads and maps responses back.
* `internal/observability` records metadata-only request observations.

The code should prefer explicit structs and small interfaces over framework-heavy abstractions because the repository currently has no existing runtime stack.

## Decision (ADR-lite)

**Context**: The full requirements document describes a larger product with native GUI, sync, open-source server, and closed-source center service. Implementing all of that at once would produce a broad but shallow skeleton.

**Decision**: This task implements Phase 1: local proxy core only. It intentionally builds the core execution path and module boundaries first, because later GUI/sync/server work depends on those boundaries being stable.

**Consequences**: The first deliverable is runnable and testable but does not include GUI, SQLite persistence, MySQL server storage, hosted billing, account login, or full multi-protocol conversion yet. Those later capabilities should attach through the preserved canonical/transformer/router/executor boundaries.

## Out Of Scope

* Native Go desktop GUI.
* SQLite persistence beyond in-memory runtime state.
* Open-source community server.
* Closed-source center service.
* MySQL schemas and migrations.
* Account login, access token, refresh token, billing, and official provider catalog.
* Full Claude/Gemini/Codex protocol coverage.
* Active health checks.
* Automatic direct/relay fallback.
* End-to-end encrypted config sync.

## Technical Notes

* Primary requirement source: `requirements_CN.md`.
* Architecture reference: `G:\project\guest\CLIProxyAPI\docs\proxy-chain-architecture_CN.md`.
* Research summary: `research/proxy-chain-reference.md`.
* Existing repository has no application code yet, so package layout should follow the requirement document's recommended structure while keeping Phase 1 scoped.
* Current directory is not a Git repository, so commit steps may need to be skipped unless Git is initialized later.
