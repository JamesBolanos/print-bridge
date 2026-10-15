# Sprint — Virtual Mode and Browser Inspector

Approved scope: 2026-10-14. Status: VM-1 through VM-8 implementation and VM-9
automated integration are complete. VM-9 native/release verification remains
open: interactive macOS checks were not run, and Windows runtime/installer
execution was unavailable. No native tray/browser or packaged-installer smoke
check is reported as passed.

## Outcome

A developer can switch PrintBridge to Virtual mode, submit existing ZPL/raw
text print requests without hardware, and inspect captured jobs in a local
browser dashboard. Optional Labelary rendering supplies PNG previews. The
physical transport remains format-agnostic and sends the same payload bytes.

## Agreed scope

- A global Physical / Virtual destination setting. Physical remains the default
  for new installs and valid older configurations.
- Virtual mode captures all jobs, including requests containing a physical
  hostname/port. There is no request-level bypass and no physical fallback.
- Keep JSON `text` input. Preserve exactly the bytes produced from the decoded
  text, including whitespace, line endings, and control characters; this is
  not a promise to preserve the original JSON encoding or arbitrary binary data.
- A local browser dashboard, served by the existing Go process with bundled
  assets, live history, raw text/hex views, copy, download, and clear actions.
- Bounded, session-only history. Full payloads are not written to rotating logs.
- Optional online ZPL-to-PNG previews through Labelary, disabled by default and
  enabled explicitly in Settings with a disclosure that label contents leave
  the machine. Capture and inspection work offline.
- Label size and print density controls, multiple-label navigation, and clear
  preview failure states. Rendering is asynchronous and does not determine
  whether capture succeeded.
- Tests belong to each implementation task; final integration testing does not
  replace those tests.

## Deferred

PDF submission/viewing, binary/base64 API additions, offline ZPL rendering,
PDF/image-to-printer-language conversion, physical replay of captured jobs,
persistent job history, saved printer profiles, and a separate service/process.
Other existing scope exclusions remain in effect.

## Working sequence

Work in the order below. Complete a task's implementation, relevant tests, and
documentation together before moving to the next task. Record verification
and limitations in the completion log. Change a checkbox only when its
acceptance criteria are met; do not mark native verification complete when
only unit tests ran. A blocked check must be recorded explicitly.

The maintainer reviews each completed task before authorizing the next one.
Stop at that review boundary; do not start the following task automatically.

| Status | ID | Task | Depends on |
|---|---|---|---|
| [x] | VM-0 | Record approved scope, contracts, and test plan | — |
| [x] | VM-1 | Isolate destination routing; protect physical behavior | VM-0 |
| [x] | VM-2 | Implement bounded virtual capture storage | VM-1 |
| [x] | VM-3 | Add destination and preview configuration | VM-2 |
| [x] | VM-4 | Add virtual submission and readiness behavior | VM-3 |
| [x] | VM-5 | Add local inspector routes and access boundaries | VM-4 |
| [x] | VM-6 | Build the browser dashboard and offline inspection | VM-5 |
| [x] | VM-7 | Add optional asynchronous Labelary previews | VM-6 |
| [x] | VM-8 | Connect desktop settings, tray, and inspector launch | VM-7 |
| [ ] | VM-9 | Complete integration, native checks, and release docs | VM-8 |

### VM-1 — Destination routing

- Introduce a small destination abstraction around the existing TCP transport;
  keep routing independent of ZPL detection or rendering.
- Preserve Physical validation, response bodies, port defaults, CORS behavior,
  and explicit connect/write timeouts.
- Select the destination once per accepted request. Later settings changes
  cannot reroute that job, and no destination error triggers a fallback.
- Tests: physical responses and exact outgoing bytes, injected transport
  failures/timeouts, and routing without a renderer dependency. Use existing
  fake connections; no physical printer is required.

### VM-2 — Virtual capture storage

Implemented limits and lifecycle details:
[virtual-capture-store.md](../03-architecture/virtual-capture-store.md).
Storage is available internally. Physical configurations use TCP; a saved
Virtual configuration keeps the listener stopped until VM-4 wires capture.

- Store immutable payload bytes, unique job ID, timestamp, supplied target
  metadata, detected type, and separate capture/preview states.
- Use a concurrency-safe store owned by the application, surviving listener
  restarts but cleared on application exit. Do not reuse sanitized log text as
  the payload source.
- Bound request size before JSON decoding in Virtual mode, decoded job size,
  job count, total payload/preview bytes, and pending rendering work. Set and
  document numeric limits in this task. Reject an individually oversized job;
  evict oldest jobs when aggregate retention limits are reached.
- Clear/eviction cancels or invalidates associated rendering work. Late results
  must not restore deleted jobs or grow memory outside the bounds.
- Tests: byte preservation, concurrent capture/read/clear, size boundaries,
  eviction, listener restart retention, and stale-result handling.

### VM-3 — Configuration

- Add the fields and migration described in
  [the config specification](../06-config/config-schema.md#destination-and-preview-settings).
- Preserve existing ports, printer address, and allowed origins when loading a
  valid legacy configuration. Default optional rendering to off.
- Reject invalid destination/preview settings. A configuration recovery path
  must not silently change a saved Virtual destination into active Physical
  printing; define recovery behavior and test it before enabling routing.
- Tests: legacy migration, defaults, round trips, invalid values, and recovery.

### VM-4 — Public API

- Replace VM-3's temporary unavailable-Virtual guard with capture routing;
  preserve invalid-config rejection and the prohibition on physical fallback.
- Implement [the specified API behavior](../04-api/virtual-mode.md) and update
  `DOCS/04-api/openapi.yaml` in the same task.
- Virtual requests may omit a hostname; supplied destinations remain metadata.
  Existing well-formed requests must work unchanged in either mode.
- Virtual `/status` reports capture readiness without any TCP probe, including
  when the caller supplies a physical destination.
- Return capture success before rendering. Preview errors do not retroactively
  turn successful submissions into failed print requests.
- Tests: Physical compatibility; Virtual requests with/without host; invalid
  JSON/text; readiness; limits; and a dialer that fails the test if called in
  Virtual mode, for both `/print` and `/status`.

### VM-5 — Inspector routes and access

- Serve the dashboard and bounded history APIs on the existing loopback
  listener. Bundle assets; do not require a frontend dev server or CDN.
- Implement list/detail, original-payload download, clear, and preview access.
  Document concrete paths, schemas, and limits in OpenAPI with the code.
- Apply the [inspector access requirements](../03-architecture/security-model.md#inspector-access)
  separately from the existing integration CORS allow-list. Implement these
  before exposing stored payloads through browser routes.
- Tests: same-origin use, foreign origins including an allowed print origin,
  unexpected Host values, mutation protection, no-store responses, missing or
  evicted IDs, and attachment downloads preserving exact bytes.

### VM-6 — Browser dashboard

- Implement [the inspector UX](../05-ux/tray-app-spec.md#virtual-mode-and-browser-inspector).
- Display live jobs and independent capture/preview states. Updates must work
  offline without automatically opening a browser tab for every incoming job.
- Show line endings/control characters in an inspection view and byte offsets
  in hex view. Display transformations must not change the stored/downloaded
  bytes. Never interpret captured text as HTML.
- Handle empty history, cleared/evicted selections, disconnected listener, and
  unavailable clipboard operations with useful UI feedback.
- Tests: browser capture-to-display flow, hostile markup rendered as text,
  selection/update behavior, copy/download/clear, and offline asset loading.

### VM-7 — Optional Labelary previews

- Detect likely ZPL only in the inspector; unsupported/raw text remains fully
  inspectable. Detection is a hint, not a complete ZPL parser or validator.
- Render using a separate adapter over HTTPS POST, with configured dimensions
  and density. Preserve the source payload; never send it in a URL.
- Bound worker concurrency, queue length, response bytes, decoded image size,
  and request duration. Deduplicate repeated UI requests for the same result.
- Support selecting multiple label indices. Surface disabled, queued,
  rendering, ready, unsupported, and failed states, with manual retry.
- Turning rendering off prevents further external requests and cancels pending
  work. Old jobs are only submitted after an explicit render/retry action.
- Handle offline errors, bad ZPL, provider limits, invalid image responses, and
  oversized results without losing captured data or blocking `/print`.
- Tests: fake HTTP renderer, request bytes/settings, multiple labels, opt-in,
  cancellation, rate limits, timeouts, queue pressure, and oversized responses.
  Automated tests must not call the public Labelary service.
- Provider reference: [Labelary API documentation](https://labelary.com/service.html).
  Recheck supported settings and limits when implementing the adapter.

### VM-8 — Desktop integration

Implemented destination and preview settings, mode indicators, tray and main
window Inspector launch, and recoverable browser launch errors.

- Add destination and preview settings, current-mode indicators in main/tray
  views, and Open Inspector in the main window and tray.
- Preserve physical printer settings while Virtual mode is active. Keep Save,
  Cancel, validation errors, and listener restart behavior consistent.
- Open the local dashboard from a user action. Incoming jobs update it without
  triggering additional browser launches. Browser tab reuse is best effort;
  do not promise control over an arbitrary browser's existing tabs.
- Show a recoverable error when opening the browser fails. Changing the HTTP
  port updates the launcher URL; an old tab shows its disconnected state.
- Tests: settings conversion and application, mode visibility, launch URL,
  cancel behavior, and mode changes with jobs in flight. Manually check tray
  and browser launch on both supported operating systems.

### VM-9 — Integration and release readiness

- Execute [the testing plan](../07-build-deploy/testing.md), including a local
  fake printer and fake renderer, and inspect the physical/virtual boundaries.
- Run `make check` and `make lint` when available, and verify native CI/build
  definitions and embedded-asset coverage.
- Complete macOS/Windows browser and installer smoke checks; record platform
  limitations honestly if the required environment is unavailable.
- Reconcile OpenAPI, config, architecture, UX, README, Help, and examples with
  implemented behavior. Replace planned labels only for completed behavior.
- Explain that preview success is not proof of physical printer fidelity and
  that offline capture does not provide offline ZPL rendering.

## Completion log

| Task | Result | Verification / limitations |
|---|---|---|
| VM-0 | Scope, ordered tasks, planned behavior, and test strategy documented | Local links/anchors, JSON examples, Markdown fences, and whitespace checked; `git diff --check` passed. No application code changed or test suite run. |
| VM-1 | Extracted format-agnostic `Destination` interface and default TCP transport; HTTP behavior retained; seven regression test functions added. Maintainer authorized VM-2 after review. | `make check` passed (format, all tests, vet, race); `make lint` passed with zero issues on macOS arm64, Go 1.27.0. Initial sandbox runs could not bind localhost/access the build cache; authorized reruns passed. Fake connections and an in-memory pipe were used; no physical printer, Windows runtime, or GUI/installer verification was performed. |
| VM-2 | Added bounded session capture, immutable snapshots, oldest-first eviction, preview reservations/cancellation, streamed request limits for injected capture destinations, and application-exit cleanup. Maintainer authorized VM-3 after review. | `go test ./internal/capture`, `make check` (format, all tests, vet, race), and `make lint` passed; lint reported zero issues. Tests used simulated preview data and local listener restarts on macOS arm64. No public renderer, physical printer, Windows runtime, or interactive GUI/installer checks were used. |
| VM-3 | Added version-1 destination/preview persistence, in-memory legacy migration, strict validation, and non-destructive recovery with listener stopped. Existing Settings saves preserve new preferences; invalid-file recovery disables Save. A temporary guard prevents configured Virtual mode from reaching TCP before VM-4. The user then authorized VM-4. | `go test ./internal/config`, `make check` (format, all tests, vet, race), and `make lint` passed; lint reported zero issues on macOS arm64. The macOS linker emitted a non-failing duplicate `-lobjc` warning for desktop tests. No renderer, physical printer, Windows runtime, or interactive GUI/installer checks were used. |
| VM-4 | Added Virtual `/print` capture with opaque job IDs, Virtual `/status` capture readiness without TCP probes, strict request validation/limits, and OpenAPI coverage. Fixed a same-port listener restart race found by the mode-switch test. Maintainer authorized VM-5 after review. | `go test -v ./internal/server` and `make check` (format, all tests, vet, race) passed; `make lint` passed with zero issues on macOS arm64, Go 1.27.0. Tests use injected failing dialers, fake connections, in-memory capture, and local listeners; no physical printer, Windows runtime, or interactive GUI/browser checks were used. The macOS linker emitted a non-failing duplicate `-lobjc` warning for desktop tests. |
| VM-5 | Added a bundled offline inspector shell and bounded metadata/detail, exact-byte attachment download, cached PNG retrieval, and clear routes. Inspector Host/origin/Fetch Metadata checks are separate from integration CORS; responses are no-store and protected against sniffing/framing. The user authorized VM-6. | `go test -count=1 ./internal/server`, `make check` (format, all tests, vet, race), and `make lint` passed; lint reported zero issues on macOS arm64, Go 1.27.0. Tests use in-memory jobs/previews and httptest; no physical printer, Windows runtime, or interactive browser/UI checks were performed. The macOS linker emitted a non-failing duplicate `-lobjc` warning for desktop tests. |
| VM-6 | Implemented an offline bundled dashboard with newest-first polling, raw/visible/hex inspection, byte offsets, download, copy feedback, clear, preview status/display, responsive layout, and empty/disconnected/evicted states. Payload content is rendered as text, never HTML. The user authorized VM-7. | `go test -count=1 ./internal/server`, `make check` (format, all tests, vet, race), and `make lint` passed; lint reported zero issues on macOS arm64, Go 1.27.0. Server tests verify offline bundled assets, safe text rendering primitives, and inspector API flows. No interactive browser session or Windows runtime was available; no Labelary service or physical printer was used. The macOS linker emitted a non-failing duplicate `-lobjc` warning for desktop tests. |
| VM-7 | Added conservative ZPL hinting, opt-in auto-render for new likely-ZPL captures, explicit render/retry for retained jobs, multi-label index selection, a bounded worker queue, and a fixed HTTPS Labelary POST adapter. Labelary settings are snapshotted per attempt; disabling cancels pending work and settings changes invalidate cached previews. PNG type, response bytes, dimensions, duration, worker count, and queue work are bounded. Preview failures never discard captures or affect `/print` success. The user authorized VM-8. | `make check` passed (format, all tests, vet, race); `make lint` passed with zero issues; focused server/capture tests passed; Node syntax check, OpenAPI YAML parse, and `git diff --check` passed on macOS arm64. Tests use a fake renderer/HTTP transport and verify raw body bytes, fixed HTTPS path, selected label index, opt-in, queue bounds, cancellation, provider failures, PNG limits, and retained payloads. No public Labelary request, interactive browser/desktop UI, Windows runtime, or physical printer test was performed. The macOS linker emitted a non-failing duplicate `-lobjc` warning. |
| VM-8 | Added Physical/Virtual and Labelary settings with validation/disclosure, preserved physical printer settings across mode changes, main/tray destination indicators, and user-initiated Inspector launch at the active loopback port. Browser-open failures show an in-app message/notification; changed-port tabs direct users to relaunch from PrintBridge. The user authorized VM-9. | `make check` and `make lint` passed; focused desktop/server tests verify settings conversion, recovery rejection, mode changes without mutating active settings, loopback URL/port selection, and browser-open failures. VM-4 listener transition tests verify captures survive mode and port changes. Node syntax and `git diff --check` passed. Native interactive tray/browser checks on macOS and Windows were not performed; no physical printer or public Labelary request was used. The macOS linker emitted a non-failing duplicate `-lobjc` warning. |
| VM-9 | Automated integration and documentation reconciliation completed; native and packaged-release verification remains open. Added a live-listener flow covering Virtual capture, asynchronous fake rendering, Inspector listing and exact-byte download, plus Physical delivery to a local TCP fake printer. | `go test ./internal/server -run TestLiveVirtualCaptureAndPhysicalPrinterIntegration -count=1 -v`, `go build ./...`, `make check`, `make lint`, JavaScript syntax, OpenAPI YAML parsing, and `git diff --check` passed on macOS arm64. The integration test uses a local TCP fake printer and a gated fake renderer; no physical printer or public Labelary request was used. Native CI and release workflow definitions were reviewed, and embedded asset route coverage is exercised by tests. Interactive macOS/Windows tray/browser checks and DMG/MSI install smoke tests were not performed; Windows runtime and installer execution were unavailable here. |

## Definition of done

VM-1 through VM-8 and VM-9 automated integration are complete with evidence;
VM-9 remains open pending platform-specific release verification. Existing
physical integrations still work; Virtual mode performs no printer
connections; exact decoded-text bytes remain inspectable; capture works
without internet; external rendering only happens when enabled; and
memory/work queues are bounded. Native verification limitations are recorded
above and in the testing plan. Complete the remaining platform-specific
tray/browser and packaged-installer checks before marking VM-9 complete or
treating the release as fully smoke-tested. Preview success is not proof of physical printer fidelity,
and offline capture does not provide offline ZPL rendering. Deferred
PDF/binary work is not required to finish this sprint.
