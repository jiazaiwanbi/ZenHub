# Quality Guidelines

> Code quality standards for frontend development.

---

## Overview

There is no frontend quality toolchain yet because there is no frontend code.

That means there are currently no frontend lint, format, unit-test, or UI test
commands in the repository. The quality rule for now is to avoid pretending
those conventions exist.

---

## Forbidden Patterns

- Adding a UI framework, component tree, or frontend build config without also
  documenting the actual commands and folder layout introduced by that task.
- Writing frontend spec text that references nonexistent files or invented team
  conventions.
- Duplicating backend API contracts by hand without checking
  `internal/canonical/models.go`, `internal/protocol/openai/openai.go`, and
  `internal/server/server.go`.

---

## Required Patterns

- The first real frontend task must update these specs with concrete file
  references and actual tooling commands.
- Any frontend implementation should make its chosen stack explicit in code
  review and in this directory, not only in planning documents.
- If the frontend consumes the local API, validate behavior against the current
  Go backend endpoints and payload rules.

---

## Testing Requirements

- No frontend tests exist today because no frontend code exists.
- The first frontend implementation must define its own lint/test/format
  commands as part of the setup task.
- Until then, there is no frontend-specific CI or local verification command to
  run.

---

## Examples

- There are no frontend lint, format, unit-test, or browser-test commands to
  cite yet.
- Current repo verification remains backend-oriented Go checks such as
  `go vet ./...` and `go test ./...`; do not relabel them as frontend tooling.

---

## Code Review Checklist

- Does the task acknowledge that it is introducing the first real frontend code?
- Does it add the actual frontend toolchain commands instead of relying on
  unstated defaults?
- Does it update this spec directory with concrete source-file references?
- Does it avoid claiming established frontend conventions that the repo still
  does not prove?
