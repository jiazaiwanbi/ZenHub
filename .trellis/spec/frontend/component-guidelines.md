# Component Guidelines

> How components are built in this project.

---

## Overview

The current UI layer is a native Fyne desktop shell.

- The UI is implemented in Go, not in `.tsx`, `.jsx`, `.vue`, or `.svelte`.
- `internal/client/gui/window.go` is the current source of truth for window
  composition and widget layout.
- There is still no design system beyond the Fyne widget set used there.

---

## Component Structure

- Keep view composition close to the window or screen that owns it.
- Extract helpers only when widget assembly or cell formatting starts repeating.
- Keep route/request formatting in the GUI layer, not in proxy/runtime packages.

---

## Props Conventions

- Fyne widgets should receive already-shaped read models from
  `internal/client/app`.
- Do not invent React-style prop conventions for the current Go-native GUI.

---

## Styling Patterns

- Prefer Fyne layout containers, forms, cards, and tables over custom drawing.
- Keep the MVP visually simple and operational; avoid custom theme work unless
  a later task needs it.

---

## Accessibility

- Prefer readable labels, selectable status text, and explicit table columns.
- If a later task adds keyboard-heavy workflows or custom rendering, document
  the accessibility trade-offs in this guide.

---

## Examples

- `internal/client/gui/window.go` shows the current pattern: status cards plus
  tables bound to read-only runtime views from
  `internal/client/app/runtime.go`.

---

## Common Mistakes

- Writing a speculative React component library when the repo has a Go-native
  GUI only.
- Reaching into proxy, router, or balancer internals from widgets instead of
  consuming `internal/client/app` read models.
- Treating Fyne widget trees as precedent for a future browser frontend.
