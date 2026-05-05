# Directory Structure

> How frontend code is organized in this project.

---

## Overview

There is no frontend directory structure yet because the repository does not
contain frontend source code.

The only implemented application layout today is the Go backend under `cmd/`
and `internal/`. Do not infer a `src/`, `app/`, `web/`, or `frontend/`
directory from this spec.

---

## Directory Layout

```
.
├── cmd/
│   └── client/
├── internal/
│   ├── balancer/
│   ├── canonical/
│   ├── config/
│   ├── executor/
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

- No frontend modules, pages, components, hooks, or asset folders are
  implemented.
- The first frontend task must choose an actual stack and create a real
  directory tree before this guide can become prescriptive.
- Until then, frontend work should not be merged as loose files sprinkled into
  the Go backend packages.

---

## Naming Conventions

- No frontend naming convention has been validated by code yet.
- Do not assume React/Next.js/Vite naming such as `components/`, `hooks/`, or
  `pages/`.
- If the first frontend implementation establishes those names, update this
  file with real paths from that task.

---

## Examples

- There are no frontend example files in the repo today.
- Backend examples that show the current repo shape:
  `cmd/client/main.go`, `internal/server/server.go`, and
  `internal/proxy/service.go`.
