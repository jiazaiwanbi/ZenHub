# Type Safety

> Type safety patterns in this project.

---

## Overview

No frontend type-safety stack exists yet because the repo has no TypeScript or
other frontend type-bearing code.

The closest thing to a current contract source is the Go backend:

- `internal/canonical/models.go` defines the canonical request/response structs.
- `internal/protocol/openai/openai.go` validates and parses incoming
  OpenAI-compatible payloads.

Those are backend contracts, not frontend typing conventions.

---

## Type Organization

- No frontend type directory or co-location convention exists yet.
- Do not create shared UI type modules based on guesswork.
- If frontend code is introduced, this section should reference real files that
  define request types, UI state types, and validation helpers.

---

## Validation

- No frontend runtime validation library is implemented.
- Current request validation happens on the backend in
  `openaiprotocol.ParseChatCompletion(...)`.
- Do not assume Zod, Yup, io-ts, or generated client types until the codebase
  actually introduces them.

---

## Common Patterns

- None are established for frontend code yet.
- For now, use backend contracts as the source of truth when documenting API
  payloads, not imagined TypeScript mirrors.

---

## Examples

- There are no frontend `.ts` or `.tsx` type definitions in the repository.
- `internal/canonical/models.go` and `internal/protocol/openai/openai.go` are
  the current contract references to cite when documenting payload shape.

---

## Forbidden Patterns

- Claiming a TypeScript convention exists when there is no TypeScript in the repo.
- Hand-copying frontend request/response types from memory instead of checking
  the Go backend contract files.
- Introducing a frontend build pipeline solely to satisfy this spec; the spec
  must follow real code, not force speculative scaffolding.
