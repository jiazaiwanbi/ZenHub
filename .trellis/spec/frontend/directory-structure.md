# Directory Structure

> How frontend code is organized in this project.

---

## Overview

The repository now contains a native desktop GUI implemented in Go. There is
still no `src/`, `app/`, `web/`, or browser-oriented frontend tree.

---

## Directory Layout

```
.
├── cmd/
│   ├── client/
│   └── gui/
├── internal/
│   ├── app/
│   ├── balancer/
│   ├── canonical/
│   ├── config/
│   ├── executor/
│   ├── gui/
│   ├── observability/
│   ├── protocol/
│   ├── proxy/
│   ├── router/
│   ├── server/
│   └── transformer/
├── .trellis/
├── requirements_CN.md
└── sample-config.json
```

---

## Module Organization

- `cmd/gui/` owns the desktop binary entrypoint only.
- `internal/gui/` owns Fyne-specific view/layout code.
- `internal/app/` owns shared runtime bootstrap and read models consumed by the
  GUI and the headless client.
- New GUI files should extend these locations instead of mixing view code into
  routing, balancing, or protocol packages.

---

## Naming Conventions

- Keep the native shell entrypoint under `cmd/gui`.
- Keep Fyne-specific widgets and window composition under `internal/gui`.
- Do not assume React/Next.js/Vite naming such as `components/`, `hooks/`, or
  `pages/` unless a later task actually introduces them.

---

## Examples

- Native GUI examples:
  `cmd/gui/main.go`, `internal/gui/window.go`, and `internal/app/runtime.go`.
