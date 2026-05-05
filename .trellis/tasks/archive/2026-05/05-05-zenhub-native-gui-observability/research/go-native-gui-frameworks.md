# Research: go-native-gui-frameworks

- Query: Research Go native desktop GUI framework options for ZenHub Phase 2; compare 2-4 realistic Windows/macOS choices that do not use WebView or a web frontend; map them to the current Go proxy core and recommend an MVP path.
- Scope: mixed
- Date: 2026-05-05

## Findings

ZenHub today is a Go-only backend/CLI, not a desktop app yet. The current entrypoint loads config, constructs router/balancer/executor/observability, then serves an HTTP proxy in-process from `main` (`cmd/client/main.go:19-62`). Observability already exists, but only as an in-memory recorder attached directly to the proxy service (`internal/proxy/service.go:17-23`, `internal/proxy/service.go:70-72`, `internal/observability/recorder.go:20-60`). The HTTP layer currently exposes only `/v1/models` and `/v1/chat/completions` (`internal/server/server.go:22-40`), so a desktop GUI cannot rely on an existing admin/status API yet. For Phase 2, the cleanest MVP is a desktop shell that embeds the current Go core in-process and renders:

1. Service status: running/stopped, listen address, loaded config path, model list, provider-group/node health summary.
2. Observability: recent request records from the existing recorder.
3. Small config actions first: select config file, start/stop proxy, open logs/record view.

### Files Found

- `.trellis/workflow.md` - Trellis workflow requiring persisted research artifacts and repo-grounded planning.
- `.trellis/spec/backend/index.md` - backend architecture guidance for the current Go proxy implementation.
- `.trellis/spec/guides/index.md` - cross-layer/reuse thinking guidance relevant because a GUI will span app shell + backend core.
- `go.mod` - module is `zenhub`; repo remains Go-only today.
- `cmd/client/main.go` - current process bootstrap for config, routing, balancing, observability, and HTTP server startup.
- `internal/proxy/service.go` - core proxy orchestration and observability record creation.
- `internal/server/server.go` - current public HTTP API surface.
- `internal/observability/recorder.go` - in-memory bounded record store for recent requests.
- `internal/config/config.go` - runtime config model, observability limit, env-backed API keys, listen address defaults.

### Code Patterns

- Thin bootstrap, rich internal core: `main` wires dependencies and runs an `http.Server`, which is favorable for embedding the same core into a desktop app without a second process (`cmd/client/main.go:28-60`).
- Observability is already GUI-friendly in-process data: `Service.run` records request time, model, selected node, retry count, duration, final status, and error text into `Recorder` (`internal/proxy/service.go:74-97`).
- Admin/status API is missing: the server mux only exposes OpenAI-compatible endpoints, and `ChatService` does not include `Records()` (`internal/server/server.go:16-40`), even though `proxy.Service` does (`internal/proxy/service.go:70-72`).
- Current config maps naturally to forms/settings pages: listen address, routes, provider groups, retry policy, health cooldown, and observability max records are all plain Go structs (`internal/config/config.go:26-72`, `internal/config/config.go:111-161`).
- Secret handling is local-process oriented: API keys may come directly from config or env vars (`internal/config/config.go:180-191`), so a GUI does not need a browser auth flow for the first MVP.

### Candidate Frameworks

#### 1. Fyne

Fit:
- Strong MVP fit if "native GUI" means native desktop app, not strict native OS widgets.
- Best balance of Go-first ergonomics and batteries included for a status/observability console.

Why it fits ZenHub:
- Fully Go-oriented API and documentation; fast to wire around existing in-process services.
- Built-in system tray support on macOS/Windows/Linux since Fyne `v2.2.0`, which maps well to a background local proxy app.
- Built-in app preferences/storage, useful for remembering last config path, window state, and startup settings.
- Table/list/data-binding widgets are already available, which reduces effort for the first observability screen.

Tradeoffs:
- UI is custom-rendered, not native OS widgets. If strict native-widget fidelity is a hard requirement, this is the main drawback.
- Requires GCC/toolchain setup despite being Go-centric.

Repo mapping:
- Very good for an embedded single-process app: instantiate the current proxy core, keep a pointer to the running service, bind UI tables to `Records()`, and later add a tiny admin interface if needed.
- Lowest likely implementation friction for the "service status + observability pages first" scope.

External references:
- Fyne docs overview and install flow: https://docs.fyne.io/
- Built-in tray support on macOS/Windows/Linux since `v2.2.0`: https://docs.fyne.io/explore/systray/
- Preferences API via `app.NewWithID` / `Preferences()`: https://docs.fyne.io/explore/preferences/
- Table widget for large tabular data: https://docs.fyne.io/collection/table/
- Generic list binding in `v2.7`: https://docs.fyne.io/api/v2/data/binding/list/

#### 2. Gio

Fit:
- Best fit if the team wants a modern Go-native rendering stack with minimal desktop dependencies and accepts more custom UI work.

Why it fits ZenHub:
- Desktop builds work directly with the `go` tool; Gio positions itself as requiring very few dependencies and uses `gogio` only for some packaging/extra features.
- The event-loop model is compatible with a local long-running app that owns its own process and background state.

Tradeoffs:
- Immediate-mode UI is powerful but higher-effort for dashboards/forms than Fyne.
- Supporting widgets and extensions in `gioui.org/x` are explicitly pre-1.0 and marked unstable, including components, file dialogs, preferences, and notifications.
- I did not find first-party tray support in the official docs reviewed; that likely means extra platform work or third-party code for a tray-first UX.

Repo mapping:
- Technically solid for a single-process embedded controller around the proxy.
- Better if ZenHub expects a bespoke high-performance desktop UI later, weaker for the shortest MVP focused on tables/forms/tray behavior.

External references:
- Gio site and platform support: https://gioui.org/
- Install/build guidance: https://gioui.org/doc/install
- `gioui.org/app` event loop and `Main()` model; pkg page showed `v0.9.0` in the published docs snapshot: https://pkg.go.dev/gioui.org/app
- `gioui.org/x` status table marking many packages unstable/pre-1.0: https://pkg.go.dev/gioui.org/x
- Material widgets/list/progress/editor primitives: https://pkg.go.dev/gioui.org/widget/material

#### 3. IUP-Go

Fit:
- Best strict-native-controls option among lighter-weight Go bindings, if native widgets matter more than toolchain simplicity.

Why it fits ZenHub:
- Explicitly provides system native UI controls for Windows and macOS.
- Has menus, dialogs, tray support, timers, and tables/tree-style primitives in the Go bindings/examples, which covers the likely MVP UI needs.
- Still allows a single-process Go app that directly owns the proxy core.

Tradeoffs:
- Requires CGO/C toolchains; first build can take time because bundled C/C++/Obj-C sources compile with the app.
- Thread-safety constraints are explicit: UI updates must stay on the main thread / message back to it.
- pkg.go.dev metadata shown by search is dated and pre-1.0, so module/version polish is weaker than the repo activity suggests.
- macOS support has historically had more caveats than Windows/Linux in older docs; current README is better, but this area deserves a proof-of-concept before committing.

Repo mapping:
- Plausible if ZenHub decides "must use native controls" is non-negotiable.
- More operational complexity than Fyne or Gio for a Go-only repo because CGO/toolchain/packaging become part of the product surface immediately.

External references:
- Repo README and requirements: https://github.com/gen2brain/iup-go
- pkg.go.dev docs showing native controls, tray callback, timers, menus: https://pkg.go.dev/github.com/gen2brain/iup-go/iup
- pkg.go.dev module page showing tray/timer/menu examples: https://pkg.go.dev/github.com/gen2brain/iup-go

#### 4. MIQT (Qt bindings for Go)

Fit:
- Most capable option if ZenHub eventually wants a full-featured desktop product and is willing to pay the highest build and packaging complexity.

Why it fits ZenHub:
- Qt gives the richest mature desktop widget/tooling ecosystem of the compared options.
- MIQT is an actively developed clean-room Go binding and supports both Qt 5.15 and Qt 6.4+ APIs with Windows and macOS support documented.

Tradeoffs:
- Highest complexity by far: CGO, Qt toolchains, packaging, plugin/runtime deployment, and Qt licensing obligations all become product concerns.
- This is a big jump from a Go-only backend repo to a mixed Go+C++ desktop distribution pipeline.
- Overkill for Phase 2 if the first pages are only service status and recent observability records.

Repo mapping:
- Viable for a later "desktop product" phase, not the best first shell around the current proxy.
- Good choice only if the roadmap already includes advanced desktop features that justify Qt's cost.

External references:
- MIQT README: https://github.com/mappu/miqt
- README states Qt `5.15 / 6.4+` bindings via CGO and documents Windows/macOS build paths: https://github.com/mappu/miqt

### Recommendation

Recommendation for MVP: **Fyne**, unless ZenHub decides that strict native OS widgets are a hard requirement from day one.

Why:
- It maps best to the repo's current reality: a Go-only codebase with an in-process local proxy core and no existing admin/status API.
- The first Phase 2 UI is likely operational rather than design-heavy: tray icon, start/stop, config selection, status cards, and a recent-requests table. Fyne already has the tray, preferences, lists/tables, and straightforward app lifecycle primitives to deliver that quickly.
- It keeps the packaging/build story much closer to the current repo than IUP-Go or MIQT.
- Gio is attractive technically, but for this specific MVP it increases UI implementation cost without giving ZenHub something obviously more valuable on day one.

Practical MVP architecture:
- Keep one process.
- Extract the bootstrap from `cmd/client/main.go` into a reusable app-core package later, so both CLI and desktop shell can start/stop the same proxy service.
- Let the GUI read observability directly from the in-memory recorder first instead of inventing a separate local admin HTTP API immediately.
- Add a small desktop-facing state model around: config path, running/stopped, listen address, loaded routes/models, recent records.

If strict native controls are mandatory:
- Choose **IUP-Go** as the fallback recommendation.
- It is the best compromise between "actual native controls" and "still reasonably Go-shaped" for Windows/macOS, but it should be validated with a spike on macOS packaging before committing.

### Related Specs

- `.trellis/spec/backend/index.md`
- `.trellis/spec/guides/index.md`

## Caveats / Not Found

- No active Trellis current-task session was set when this research started; I used the explicit path provided in the request.
- I did not find an existing ZenHub admin/status endpoint. The recommendation assumes the Phase 2 GUI embeds the current Go core and reads state in-process rather than calling a pre-existing admin API.
- I did not find first-party Gio tray documentation in the official sources reviewed; treat tray support there as a likely extra integration task until proven otherwise.
- Fyne is the MVP recommendation only if "native GUI" means native desktop application without WebView. If the requirement instead means strict native OS widgets/controls, prefer IUP-Go.
