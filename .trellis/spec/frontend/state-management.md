# State Management

> How state is managed in this project.

---

## Overview

There is no frontend state-management layer because no frontend implementation
exists yet.

Be careful not to confuse backend runtime state with frontend state patterns.
The repo does contain mutable backend state in packages such as
`internal/balancer` and `internal/observability`, but those are server-side Go
structures, not precedent for browser or desktop UI state.

---

## State Categories

- Frontend local state: not implemented.
- Frontend global state: not implemented.
- Frontend server state cache: not implemented.
- Frontend URL/navigation state: not implemented.

Current stateful backend examples:

- `internal/balancer.Manager` tracks passive health and node selection state.
- `internal/observability.Recorder` tracks recent request metadata in memory.

---

## When to Use Global State

- No frontend rule exists yet because there is no frontend store.
- The first frontend task must define this from actual UI behavior, not from a
  generic template.

---

## Server State

- No client-side cache or synchronization layer exists today.
- The current server API surface is the local HTTP handler in
  `internal/server/server.go`, which exposes `/v1/models` and
  `/v1/chat/completions`.
- If future UI work consumes those endpoints, document the real cache/fetch
  strategy alongside the code that introduces it.

---

## Examples

- There are no frontend store modules, cache keys, or route-state helpers yet.
- `internal/balancer/balancer.go` and `internal/observability/recorder.go`
  remain backend-only examples of mutable state and should not be cited as
  frontend store precedent.

---

## Common Mistakes

- Treating backend in-memory state as evidence that Redux, Zustand, MobX, or
  another frontend store has been chosen.
- Adding a frontend global state library before proving a concrete UI need.
- Documenting server-state conventions before any frontend network client exists.
