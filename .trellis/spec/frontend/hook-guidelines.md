# Hook Guidelines

> How hooks are used in this project.

---

## Overview

No hook layer exists because the repository uses a Fyne desktop shell, not a
React or equivalent hook-capable runtime.

This means there are no custom hooks, no client-side data-fetching utilities,
and no shared `use*` patterns to document today.

---

## Custom Hook Patterns

- None are established.
- Do not add `use*` utilities or hook folders unless a later task introduces
  the runtime that needs them.

---

## Data Fetching

- No frontend data-fetching library is implemented.
- The current GUI reads runtime state in-process from `internal/app.Runtime`.
- If a future frontend consumes the local API, document the actual fetching
  layer rather than assuming React Query, SWR, or fetch wrappers.

---

## Naming Conventions

- No hook naming convention exists yet.
- Do not assume `useSomething` names until there is a hook-capable frontend in
  the repo.

---

## Examples

- `internal/gui/window.go` is a useful counterexample: periodic refresh uses a
  Go `time.Ticker` plus `fyne.Do`, not hooks.

---

## Common Mistakes

- Assuming React and adding hooks before the project has chosen that runtime.
- Wrapping simple Fyne polling in fake hook-shaped helpers.
- Treating backend helper packages as precedent for hook structure.
