# Component Guidelines

> How components are built in this project.

---

## Overview

No UI component system exists in the repository today.

- There are no `.tsx`, `.jsx`, `.vue`, `.svelte`, or desktop view files.
- There is no established design system, prop convention, or styling approach.
- Any component guidance beyond "none exists yet" would be invented.

---

## Component Structure

- No standard component file structure exists yet.
- The first frontend implementation must define the real structure through code
  and then update this guide with concrete examples.

---

## Props Conventions

- No props convention exists because no component code exists.
- Do not assume TypeScript interfaces, runtime prop validation, or composition
  helpers until the chosen UI stack proves them.

---

## Styling Patterns

- No styling system is implemented.
- Do not assume Tailwind, CSS modules, inline styles, styled-components, or a
  native desktop theming solution.
- Styling conventions must be documented only after real frontend files land.

---

## Accessibility

- There is no implemented UI to audit for accessibility yet.
- The first user-facing frontend task should state its accessibility baseline in
  code review and update this file with the actual patterns used.

---

## Examples

- There are no frontend component files to cite yet.
- If a task needs API contract examples before a UI exists, use the backend
  request/response files such as `internal/canonical/models.go` and
  `internal/server/server.go` rather than inventing component props.

---

## Common Mistakes

- Writing a speculative React component library when the repo does not yet have
  a frontend runtime.
- Claiming component conventions exist without referencing actual source files.
- Mixing UI experiments into backend packages under `internal/` because there
  is no dedicated frontend tree yet.
