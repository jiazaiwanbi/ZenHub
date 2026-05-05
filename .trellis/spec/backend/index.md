# Backend Development Guidelines

> Current backend conventions for the Go proxy and native desktop runtime in this repo.

---

## Overview

The implemented product today is a Go backend with two runnable shells:
`cmd/client/main.go` for the headless proxy and `cmd/gui/main.go` for the
native desktop shell. Shared runtime wiring lives in `internal/app/runtime.go`,
application logic lives under `internal/`, and the request path is split into
protocol parsing, canonical models, routing, balancing, execution,
transformation, and observability packages.

These guides are intentionally grounded in the current codebase. They document
the architecture that exists now, including gaps such as the absence of a
database layer or shared structured logger.

---

## Guidelines Index

| Guide | Description | Status |
|-------|-------------|--------|
| [Directory Structure](./directory-structure.md) | Module organization and file layout | Documented |
| [Database Guidelines](./database-guidelines.md) | Current persistence reality and rules for adding a database later | Documented |
| [Error Handling](./error-handling.md) | Error types, handling strategies | Documented |
| [Quality Guidelines](./quality-guidelines.md) | Code standards, forbidden patterns | Documented |
| [Logging Guidelines](./logging-guidelines.md) | Current runtime logging and observability patterns | Documented |

---

## Pre-Development Checklist

- [ ] Read [Directory Structure](./directory-structure.md) before adding or
  moving Go packages.
- [ ] Read [Database Guidelines](./database-guidelines.md) if the task touches
  persistence, durable state, or config that might otherwise become storage.
- [ ] Read [Error Handling](./error-handling.md) if request validation, status
  mapping, retries, or streaming behavior changes.
- [ ] Read [Logging Guidelines](./logging-guidelines.md) if the task changes
  startup logging, observability records, or troubleshooting output.
- [ ] Read [Quality Guidelines](./quality-guidelines.md) before changing
  canonical models, transformers, routing, balancing, or tests.

---

## How to Use These Guidelines

1. Start with [Directory Structure](./directory-structure.md) when deciding
   where new Go code belongs.
2. Read the specific guide for the concern you are changing: error behavior,
   logging, persistence, or quality checks.
3. Update these docs when the implementation changes materially. Future AI
   tasks load them automatically, so stale docs are harmful.

---

## Quality Check

- [ ] The described package layout still matches `cmd/` and `internal/`.
- [ ] Database guidance still says no persistence layer exists unless code in
  the same task introduces one.
- [ ] Logging guidance still reflects standard-library `log` in
  `cmd/client/main.go` and `cmd/gui/main.go`, plus in-memory observability
  records, not a made-up structured logger.
- [ ] Code examples still point at real files and current function/package
  names.
- [ ] Task manifests include the backend guideline files this task depends on.

---

**Language**: All documentation should remain in **English**.
