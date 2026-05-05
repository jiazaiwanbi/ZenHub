# Research: gui-mvp-scope

- Query: Lightweight MVP scope patterns for desktop AI proxy clients / native tooling dashboards, with emphasis on the most effective read-only first GUI slice when a backend/local proxy core already exists. Include concrete recommendations for what to include now vs defer for ZenHub native GUI observability.
- Scope: mixed
- Date: 2026-05-05

## Findings

### Repo reality

The current repo is a Go proxy with no checked-in frontend or desktop shell yet. The native GUI MVP should therefore optimize for screens that can be driven from existing runtime and config state, not for admin flows that would force new persistence, mutation APIs, or secrets UX on day one.

Files found:

- `cmd/client/main.go` - boots config, router, balancer, recorder, and HTTP server; startup status is currently process-local.
- `internal/config/config.go` - runtime config already contains listen address, routes, provider groups, passive health, node details, and observability limit.
- `internal/router/router.go` - routes are a static model-to-provider-group map with direct/relay mode.
- `internal/balancer/balancer.go` - provider groups expose strategy, timeout, retry count, max node attempts, nodes, and passive health state transitions.
- `internal/proxy/service.go` - runtime request path records model, route mode, selected node, retry count, duration, and final status.
- `internal/observability/recorder.go` - observability is an in-memory ring buffer, not durable analytics.
- `internal/server/server.go` - current HTTP server only exposes `/v1/models` and `/v1/chat/completions`.

Code patterns:

- Process status is implicit, not modeled as a first-class status object; the app logs startup and then serves HTTP on the configured listen address. See `cmd/client/main.go:53`, `cmd/client/main.go:59`.
- Routes are stable, read-only config objects keyed by model, and the router can already return a sorted model list. See `internal/router/router.go:21`, `internal/router/router.go:35`, `internal/router/router.go:86`.
- Provider groups are also read-only config objects at load time, but balancer state is runtime-derived and transient. Health is inferred from failure counts and cooldown windows, not from active probes. See `internal/balancer/balancer.go:35`, `internal/balancer/balancer.go:50`, `internal/balancer/balancer.go:167`, `internal/balancer/balancer.go:219`.
- The richest existing observability surface is the per-request record captured in proxy execution: request time, model, route mode, selected node, balancing strategy, retry count, duration, final status, and error string. See `internal/proxy/service.go:79`, `internal/proxy/service.go:85`, `internal/proxy/service.go:104`, `internal/proxy/service.go:173`.
- Observability retention is capped by a local ring buffer. There is no historical database, aggregation layer, or persisted event log. See `internal/observability/recorder.go:20`, `internal/observability/recorder.go:33`, `internal/observability/recorder.go:49`.
- The current public HTTP surface is inference-focused, not admin-focused. There is no status endpoint, config endpoint, records endpoint, or mutation endpoint. See `internal/server/server.go:22`, `internal/server/server.go:33`.
- Config already contains the exact read-only metadata a GUI needs for a routes/providers overview: listen address, route definitions, provider groups, node base URLs, timeouts, retries, and observability limit. See `internal/config/config.go:26`, `internal/config/config.go:67`, `internal/config/config.go:111`, `internal/config/config.go:164`.

### External product patterns

Across current desktop/local AI tools, the first operational UI slice consistently starts with service status, model visibility, and logs/observability before full provider administration.

External references:

- LM Studio docs show a narrow operational stack around server start/status, loaded models, and logs. `lms server status` reports whether the local server is running and on which port, `lms ps` lists loaded models in memory, and `lms log stream` exposes model and server logs. Sources:
  - https://lmstudio.ai/docs/cli/serve/server-status
  - https://lmstudio.ai/docs/cli/local-models/ps
  - https://lmstudio.ai/docs/cli/serve/log-stream
- Jan Desktop exposes a built-in local API server with server host/port, API prefix, API key, trusted hosts, CORS, and verbose server logs, while model/provider management lives in settings. This indicates that local-server products prioritize inspectability and basic server controls before complex orchestration. Sources:
  - https://www.jan.ai/docs/desktop/api-server
  - https://www.jan.ai/docs/desktop/settings
- Ollama’s docs separate operational read-only surfaces into running-model inspection (`/api/ps`) and logs (`~/.ollama/logs/app.log`, `server.log`). This is another strong signal that loaded/running state plus logs are core first-class screens. Sources:
  - https://docs.ollama.com/api/ps
  - https://docs.ollama.com/macos
  - https://docs.ollama.com/troubleshooting
- Open WebUI supports broad provider connection management, but its higher-value admin surfaces are analytics and connection visibility. It also explicitly depends on `/models` verification and allowlists when a backend is only partially discoverable. That reinforces the usefulness of a routes/models overview page even before editing. Sources:
  - https://docs.openwebui.com/getting-started/quick-start/connect-a-provider/starting-with-openai-compatible/
  - https://docs.openwebui.com/features/administration/
  - https://docs.openwebui.com/features/

What these products have in common:

- A small “is the local service up?” surface.
- A “what models or routes are available right now?” surface.
- A “what happened recently?” surface based on logs, request history, or loaded-model state.
- Configuration editing exists, but it is usually settings-heavy, validation-heavy, and coupled to secrets/network/security concerns.

### Recommendation: what to include now

Recommended first GUI slice:

1. Service status card.
2. Models/routes overview screen.
3. Observability list screen for recent requests.

Why these three:

- They map directly onto state that already exists or can be derived with minimal new backend surface.
- They are immediately useful for debugging “is my proxy working?” which is the main value of a native shell around an already-existing local proxy.
- They avoid inventing day-one workflows for secrets, config writes, migration, auth, or sync.

Include now:

- Service status
  - Show running/stopped, listen address, config path, observability limit, and basic startup/config errors.
  - If feasible, also show request freshness such as “last request at”.
  - This is effective because ZenHub currently has no explicit admin/status API, so a native shell should first make process state visible rather than editable.
- Models/routes overview
  - Show one row per exposed model with route mode, provider group, upstream model alias, balancing strategy, retry count, timeout, max node attempts, and node count.
  - Show provider groups as read-only detail panes with node names and base URLs.
  - This is effective because route and provider config are static and already validated at load time; users mainly need confidence that model aliases map where they expect.
- Observability list
  - Show newest-first request records with timestamp, model, route mode, selected node, strategy, retry count, duration, final status, and truncated error.
  - Add simple client-side filters for status, model, and node.
  - This is the strongest first-screen differentiator because ZenHub already records these fields in memory during every request.

Strong optional-now item, but only if cheap:

- Provider/group health summary derived from balancer state
  - Show per-group node count, inferred unhealthy nodes, and cooldown status if you expose that state to the GUI.
  - Keep it summary-only. Do not turn it into a management surface yet.

Suggested screen structure:

- `Overview`
  - Service status card
  - Quick counters: models, provider groups, total nodes, buffered request records
  - Recent failures snippet
- `Routes`
  - Models and route mappings
  - Provider group detail drawer/panel
- `Requests`
  - Recent request table/list
  - Lightweight filtering

This is a better MVP than starting with settings because it answers the first three operator questions:

- Is the local proxy running?
- What can it route to?
- What happened on the last requests?

### Recommendation: what to defer

Defer from the first GUI slice:

- Provider editing
  - Editing provider groups, node URLs, API keys, or headers requires validation, redaction rules, dirty-state handling, save/reload semantics, and a safe write path back to config.
  - It also risks coupling the first GUI to secrets management before observability basics are solid.
- Route editing
  - Same issue as provider editing, plus it invites breakage in model aliases and group references.
- Sync/import/export features
  - There is no existing persistence/sync abstraction in the repo. Adding sync early would create product surface area unrelated to debugging the proxy core.
- Auth and multi-user concepts
  - The current product is a local desktop proxy, not a hosted multi-user admin console.
  - A local GUI should avoid introducing auth UX unless remote exposure becomes a supported product path.
- Full analytics dashboards
  - Open WebUI-style aggregate analytics are useful later, but ZenHub only has an in-memory request ring buffer today, not a historical store.
  - Start with a request list, not charts, leaderboards, token accounting, or cost reporting.
- Model download/start/stop/provider marketplace UX
  - Those flows are valuable in products like Jan or LM Studio because they own model lifecycle locally.
  - ZenHub’s current core is a routing proxy, so the most honest first GUI is observability around that proxy, not model lifecycle orchestration.
- Logs console beyond request records
  - Full raw logs can come later if needed, but a request list has higher signal and lower noise for an MVP.
  - If added later, keep raw logs separate from request observability.

### Practical MVP boundary for ZenHub

A disciplined MVP boundary is:

- Read-only desktop shell.
- Backed by current config and runtime memory.
- No config writes.
- No secrets editing.
- No provider onboarding wizard.
- No historical analytics beyond the current recorder limit.

This boundary fits the repo and matches successful local-AI tool patterns: operational visibility first, mutable administration later.

Related specs:

- `.trellis/spec/frontend/index.md` - documents that no frontend implementation exists yet, so GUI recommendations must avoid assuming a stack.
- `.trellis/spec/backend/index.md` - current Go backend conventions and architecture overview.
- `.trellis/spec/guides/index.md` - relevant because the GUI will span runtime state, config state, and observability if implemented later.

## Caveats / Not Found

- No frontend, native shell, or desktop runtime exists in the repo today, so this research does not recommend a framework or folder structure.
- No current admin/status HTTP endpoints exist for the GUI beyond OpenAI-compatible inference endpoints; a native shell may need direct in-process access, IPC, or new read-only endpoints.
- No persisted analytics store exists. Any charts, trendlines, token accounting, or cross-session history would require new storage and collection design.
- Balancer health state is internal and transient; exposing it cleanly to a GUI would require a deliberate read model rather than direct struct leakage.
- There is no current evidence of auth, sync, cloud account linkage, or remote fleet management in the repo, so those features should be treated as separate product bets rather than natural MVP extensions.
