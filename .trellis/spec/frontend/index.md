# Frontend Development Guidelines

> Current frontend status for this repo: no frontend implementation exists yet.

---

## Overview

This repository has no frontend source tree today. There are no React,
TypeScript, CSS, desktop GUI, or other UI implementation files checked in.

These guides therefore document constraints, not a mature frontend style guide.
Their job is to keep future tasks honest: agents should not invent UI
conventions until real frontend code exists in the repo.

---

## Guidelines Index

| Guide | Description | Status |
|-------|-------------|--------|
| [Directory Structure](./directory-structure.md) | Current repo layout and the absence of a frontend tree | Documented |
| [Component Guidelines](./component-guidelines.md) | Guardrails until a real UI component system exists | Documented |
| [Hook Guidelines](./hook-guidelines.md) | Guardrails until a real hook/data-fetching layer exists | Documented |
| [State Management](./state-management.md) | Guardrails until a real frontend state layer exists | Documented |
| [Quality Guidelines](./quality-guidelines.md) | Bootstrap review rules for first frontend work | Documented |
| [Type Safety](./type-safety.md) | Guardrails until a real frontend type system exists | Documented |

---

## Pre-Development Checklist

- [ ] Read [Directory Structure](./directory-structure.md) before proposing any
  frontend folder layout.
- [ ] Read [Component Guidelines](./component-guidelines.md) before describing
  a component system, styling approach, or accessibility baseline.
- [ ] Read [Hook Guidelines](./hook-guidelines.md) before assuming React or any
  other hook-capable runtime.
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
2. If a task introduces the first real frontend code, update these files in the
   same task with concrete stack choices and code references.
3. Do not cite planning documents as if they were implementation conventions.

---

## Quality Check

- [ ] No section implies implemented frontend source files, components, hooks,
  stores, or type modules exist when they do not.
- [ ] Any first-frontend task updates these docs with concrete file paths,
  tooling commands, and actual stack choices in the same change.
- [ ] Example sections either cite real frontend files or explicitly state that
  none exist yet.
- [ ] Backend references are described as backend contracts, not as proof of a
  frontend architecture.
- [ ] Task manifests include the frontend guideline files this task depends on.

---

**Language**: All documentation should remain in **English**.
