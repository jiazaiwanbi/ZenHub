# State Management

> How state is managed in this project.

---

## Overview

The native GUI keeps a small local view state in
`internal/client/gui/window.go` and reads server/runtime state from
`internal/client/app.Runtime`.

Be careful not to confuse backend runtime state with frontend state patterns.
The repo does contain mutable backend state in packages such as
`internal/core/balancer` and `internal/core/observability`, but the GUI should
reach them through `internal/client/app` read models instead of directly.

---

## State Categories

- Local GUI state: current table rows and status labels in `internal/client/gui`.
- Global GUI store: not implemented.
- Server/runtime state access: `internal/client/app.Runtime` status, routes, and
  request snapshots.
- URL/navigation state: not applicable to the native Fyne MVP.

Current stateful backend examples:

- `internal/core/balancer.Manager` tracks passive health and node selection state.
- `internal/core/observability.Recorder` tracks recent request metadata in memory.

---

## When to Use Global State

- No global GUI store exists today.
- Add one only if multiple windows or complex editable workflows require it.

---

## Server State

- No client-side cache or synchronization layer exists today.
- The current GUI does not call HTTP admin endpoints; it polls in-process
  runtime state from `internal/client/app.Runtime`.
- If future UI work consumes HTTP endpoints, document the cache/fetch strategy
  alongside the code that introduces it.

---

## Examples

- `internal/client/gui/window.go` shows the current state pattern: widget-local
  state refreshed from `internal/client/app.Runtime`.

---

## Common Mistakes

- Treating backend in-memory state as evidence that Redux, Zustand, MobX, or
  another store has been chosen.
- Reading balancer or recorder internals directly from the GUI instead of using
  `internal/client/app`.
- Documenting server-state conventions that the current desktop shell does not use.
