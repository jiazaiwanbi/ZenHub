# Hook Guidelines

> How hooks are used in this project.

---

## Overview

No hook layer exists because there is no React or equivalent frontend runtime in
the repository today.

This means there are no custom hooks, no client-side data-fetching utilities,
and no shared UI stateful logic patterns to document yet.

---

## Custom Hook Patterns

- None are established.
- Do not add `use*` utilities or hook folders unless the task also introduces
  the actual frontend stack that needs them.

---

## Data Fetching

- No frontend data-fetching library is implemented.
- The only implemented API consumer path today is server-side Go code calling
  upstream providers through `internal/executor/direct.go`.
- If a future frontend consumes the local API, its first task must document the
  actual fetching layer rather than assuming React Query, SWR, or fetch wrappers.

---

## Naming Conventions

- No hook naming convention exists yet.
- Do not assume `useSomething` names until there is a hook-capable frontend in
  the repo.

---

## Examples

- There are no frontend hook files or hook usage sites in the repo today.
- `internal/executor/direct.go` is a useful counterexample: current network I/O
  happens in backend Go code, not in a client-side fetching hook.

---

## Common Mistakes

- Assuming React and adding hooks before the project has chosen a frontend
  runtime.
- Creating client-side fetching abstractions without first documenting which
  backend endpoints they consume.
- Treating backend helper packages as precedent for frontend hook structure.
