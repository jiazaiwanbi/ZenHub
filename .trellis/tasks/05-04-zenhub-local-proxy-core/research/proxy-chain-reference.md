# Proxy Chain Reference

Source: `G:\project\guest\CLIProxyAPI\docs\proxy-chain-architecture_CN.md`

## Takeaways For ZenHub Phase 1

* Keep protocol handlers thin. A handler should parse the external protocol, detect stream mode, and call the shared execution path.
* Convert external requests into a unified internal request before model routing, provider selection, or upstream execution.
* Treat model routing as a separate module. Avoid guessing providers directly inside handlers.
* Treat selection/load balancing as its own module. Round-robin, fill-first, cooldown, and retries should not be embedded in protocol or executor code.
* Treat upstream execution as a provider executor interface. Adding another provider should mean adding an executor and transformer, not rewriting handlers.
* Keep request/response translation separate from upstream HTTP transport.
* Keep stream chunks and protocol-specific SSE framing separate. The executor streams chunks; the protocol layer writes OpenAI-compatible frames.
* Avoid mixing client-facing access auth with upstream provider credentials. Phase 1 does not need full auth, but package boundaries should leave room for both.

## Minimum Architecture To Reuse

`compatible protocol ingress -> canonical request -> route decision -> load balancer -> provider executor -> transformer -> JSON/SSE writer`

## Phase 1 Scope Mapping

* OpenAI Chat Completions is the first ingress protocol.
* OpenAI-compatible upstreams are the first provider executor target.
* Dynamic model registry can be simplified to configured model routes for this phase.
* AuthManager can be simplified to provider group execution, because this task focuses on direct local node pools rather than multi-account auth.
* Config hot reload and session affinity are useful future work but not required for this task.

## Risks

* If the handler directly forwards raw JSON to upstreams, later Claude/Gemini/Codex support will require rewrites.
* If route mode and load balancing are collapsed together, direct/relay fallback rules will become ambiguous.
* If passive health is represented only as local variables inside request handling, tests and future GUI observability will be harder.
