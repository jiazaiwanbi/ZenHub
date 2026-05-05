# Frontend Development Guidelines

> Current frontend status for this repo: a first native desktop GUI now exists.

---

## Overview

This repository now contains a first native desktop GUI slice implemented in Go
with `Fyne`. The UI entrypoint is `cmd/gui/main.go`, the Fyne view layer lives
in `internal/client/gui/window.go`, and the GUI reads in-process runtime state
from `internal/client/app/runtime.go`.

There is still no web frontend, React stack, or TypeScript tree. These guides
therefore document the actual native GUI conventions that now exist and keep
future tasks honest about what the repo still does not implement.

---

## Guidelines Index

| Guide | Description | Status |
|-------|-------------|--------|
| [Directory Structure](./directory-structure.md) | Current native GUI layout | Documented |
| [Component Guidelines](./component-guidelines.md) | Current Fyne view/component guardrails | Documented |
| [Hook Guidelines](./hook-guidelines.md) | Guardrails for a repo that still has no hook runtime | Documented |
| [State Management](./state-management.md) | Current runtime-to-GUI read model pattern | Documented |
| [Quality Guidelines](./quality-guidelines.md) | Review and verification rules for native GUI work | Documented |
| [Type Safety](./type-safety.md) | Current Go-native UI contracts and guardrails | Documented |

---

## Pre-Development Checklist

- [ ] Read [Directory Structure](./directory-structure.md) before proposing any
  frontend folder layout.
- [ ] Read [Component Guidelines](./component-guidelines.md) before describing
  a component system, styling approach, or accessibility baseline.
- [ ] Read [Hook Guidelines](./hook-guidelines.md) before assuming React or any
  other hook-capable runtime beyond the current Fyne desktop shell.
- [ ] Read [State Management](./state-management.md) before proposing a client
  store, cache, or navigation state layer.
- [ ] Read [Type Safety](./type-safety.md) before documenting TypeScript or
  runtime validation conventions.
- [ ] Read [Quality Guidelines](./quality-guidelines.md) before claiming lint,
  test, or build commands exist for frontend code.

---

## How to Use These Guidelines

1. Treat every frontend guide in this directory as a "do not assume more than
   the repo proves" contract.
2. Use the current native GUI files as the source of truth for structure,
   runtime access, and verification commands.
3. Do not cite planning documents as if they were implementation conventions.

---

## Quality Check

- [ ] No section implies implemented frontend source files, components, hooks,
  stores, or type modules exist when they do not.
- [ ] Native GUI sections reference the real files under `cmd/gui/`,
  `internal/client/gui/`, and `internal/client/app/`.
- [ ] Example sections cite real GUI files instead of abstract planning notes.
- [ ] Backend references are described as backend contracts, not as proof of a
  browser frontend architecture.
- [ ] Task manifests include the frontend guideline files this task depends on.

---

**Language**: All documentation should remain in **English**.
