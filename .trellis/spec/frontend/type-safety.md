# Type Safety

> Type safety patterns in this project.

---

## Overview

The native GUI uses Go types, not TypeScript or another separate frontend type
system.

The closest thing to a current contract source is the Go backend:

- `internal/core/canonical/models.go` defines the canonical request/response structs.
- `internal/core/protocol/openai/openai.go` validates and parses incoming
  OpenAI-compatible payloads.
- `internal/client/app/runtime.go` defines the read-only GUI view models.

Those Go structs are the current UI contract source as well.

---

## Type Organization

- Keep GUI-facing view models in Go near the shared client runtime
  (`internal/client/app`).
- Do not create speculative shared UI type modules outside the current Go code.

---

## Validation

- No frontend runtime validation library is implemented.
- Current validation still happens at backend/protocol boundaries.
- Do not assume Zod, Yup, io-ts, or generated client types until the codebase
  actually introduces them.

---

## Common Patterns

- Use `internal/client/app.StatusView`, `RouteView`, and `RequestView` as the
  source of truth for the current desktop shell.
- Avoid copying route/request fields into ad hoc GUI-only structs when the
  shared runtime already defines them.

---

## Examples

- `internal/client/app/runtime.go` is the current GUI contract reference.
- There are still no frontend `.ts` or `.tsx` type definitions in the repo.

---

## Forbidden Patterns

- Claiming a TypeScript convention exists when there is no TypeScript in the repo.
- Hand-copying GUI route/request structs instead of checking
  `internal/client/app`.
- Introducing a frontend build pipeline solely to satisfy this spec; the spec
  must follow real code, not force speculative scaffolding.
