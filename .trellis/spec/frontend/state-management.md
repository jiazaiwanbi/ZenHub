# State Management

> How state is managed in this project.

---

## Overview

The native GUI keeps a small local view state in `internal/gui/window.go` and
reads server/runtime state from `internal/app.Runtime`.

Be careful not to confuse backend runtime state with frontend state patterns.
The repo does contain mutable backend state in packages such as
`internal/balancer` and `internal/observability`, but the GUI should reach them
through `internal/app` read models instead of directly.

---

## State Categories

- Local GUI state: current table rows and status labels in `internal/gui`.
- Global GUI store: not implemented.
- Server/runtime state access: `internal/app.Runtime` status, routes, and
  request snapshots.
- URL/navigation state: not applicable to the native Fyne MVP.

Current stateful backend examples:

- `internal/balancer.Manager` tracks passive health and node selection state.
- `internal/observability.Recorder` tracks recent request metadata in memory.

---

## When to Use Global State

- No global GUI store exists today.
- Add one only if multiple windows or complex editable workflows require it.

---

## Server State

- No client-side cache or synchronization layer exists today.
- The current GUI does not call HTTP admin endpoints; it polls in-process
  runtime state from `internal/app.Runtime`.
- If future UI work consumes HTTP endpoints, document the cache/fetch strategy
  alongside the code that introduces it.

---

## Examples

- `internal/gui/window.go` shows the current state pattern: widget-local state
  refreshed from `internal/app.Runtime`.

---

## Common Mistakes

- Treating backend in-memory state as evidence that Redux, Zustand, MobX, or
  another store has been chosen.
- Reading balancer or recorder internals directly from the GUI instead of using
  `internal/app`.
- Documenting server-state conventions that the current desktop shell does not use.
