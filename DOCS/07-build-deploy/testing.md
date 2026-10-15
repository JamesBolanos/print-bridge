# Testing — baseline and Virtual Mode sprint

## Existing tests

The repository already includes Go tests:

| File | Existing coverage |
|---|---|
| `internal/server/server_test.go` | Liveness, CORS, request validation, printer port defaults, injected connection/write failures, printer status, loopback binding, listener restart |
| `internal/server/destination_test.go` | Exact decoded-text bytes, default/custom deadlines, physical response/log compatibility, connection cleanup, actual pipe write timeout, status probe behavior, destination dispatch without fallback, and destination retention for in-flight requests |
| `internal/server/capture_test.go` | Injected capture destination, exact decoded JSON bytes, streaming request limits, escaped payload boundaries, closed-store rejection, and listener restart retention |
| `internal/capture/store_test.go` | Immutable snapshots, metadata, count/byte eviction, preview budgets, cancellation/stale results, session shutdown, and concurrent capture/read/clear |
| `internal/config/config_test.go` | Empty default origins, origin validation, first-run configuration, invalid saves, malformed/invalid configuration recovery |
| `internal/config/migration_test.go` | Legacy migration without writes, version-1 round trips, preview defaults/validation, invalid-file preservation, strict field decoding, and failed-save preservation |
| `internal/server/configuration_test.go` | Invalid/recovery settings cannot start or fall back to physical routing; switching to Virtual replaces the old physical listener |
| `internal/server/virtual_test.go` | Configured capture and job IDs, no TCP or transport bypass, input/size validation, readiness, preview pressure/failure independence, CORS, and capture retention across mode/port changes |
| `internal/server/inspector_test.go` | Bundled offline HTML/JS/CSS, bounded metadata, safe text rendering, exact-byte attachment, preview route, empty/evicted states, and Host/origin/Fetch Metadata/mutation boundaries |
| `internal/server/preview_test.go` | Fixed Labelary HTTPS POST path/body, fake renderer snapshots, label-index selection, automatic opt-in behavior, queue bounds, cancellation, provider failures, capture independence, and PNG validation |
| `internal/server/integration_test.go` | Live HTTP listener Virtual capture/Inspector download with a gated fake renderer, Virtual-to-fake-printer isolation, and Physical delivery to a local TCP fake printer |
| `internal/desktopapp/settings_test.go` | Destination/preview settings conversion, preserving printer settings on mode changes, recovery validation, Inspector URL construction, and browser launch errors |
| `internal/logging/logger_test.go` | Log/activity recording, newest-first reads, file rotation |

Server tests use an injected dialer and fake connections. Configuration and
logging tests use local test fixtures. They do not require physical printers.
This inventory does not imply that all edge cases are covered. Test execution
results and environment limitations are recorded in the sprint verification
record.

`.github/workflows/go.yml` defines builds/tests on Linux, macOS, and Windows,
plus formatting, vet, race, and lint checks on Linux. Native CI builds do not
replace interactive tray, browser, physical printing, or installer checks.

## Local commands

```sh
go test ./internal/capture ./internal/server ./internal/config ./internal/logging
go test ./internal/server -run TestLiveVirtualCaptureAndPhysicalPrinterIntegration -count=1 -v
make check
make lint
```

The first command targets the existing backend packages. `make check` runs
formatting, all Go tests, vet, and the race detector. Formatting modifies files;
inspect the diff. Lint requires `golangci-lint`. Full application checks require
the Fyne native build dependencies described in `CONTRIBUTING.md`.

For the sprint, run relevant tests during each task and the broader required
checks before marking code work complete. Add new packages to targeted runs as
they are introduced. Record failed or unavailable checks with their reason.

## Virtual Mode test matrix

The automated matrix covers the whole sprint. VM-1 routing, VM-2
storage/request-limit, VM-3 configuration, VM-4 public API, VM-5 inspector
API/access, VM-6 dashboard asset/flow tests, VM-7 preview tests, VM-8 desktop
helper tests, and VM-9 live-listener integration tests are implemented.
Interactive browser/tray and packaged-installer checks remain manual release
follow-up; see the [sprint checklist](../08-backlog/virtual-mode-sprint.md)
for evidence and limitations.

| Area | Evidence required | Task |
|---|---|---|
| Physical compatibility | Existing requests/responses, exact outgoing bytes, timeouts, failures, configured/default ports | VM-1, VM-4 |
| Capture | Text containing CR/LF, tabs, Unicode, NUL, and markup survives storage/download unchanged; limits and eviction are deterministic | VM-2 |
| Concurrent lifecycle | Race-tested capture/list/clear/eviction; listener restart retains history; late preview completion cannot restore cleared jobs | VM-2, VM-7 |
| Configuration | Legacy migration, defaults, atomic persistence, invalid destination/settings, no silent Virtual-to-Physical recovery | VM-3 |
| Routing/readiness | Virtual `/print` and `/status` never call a printer dialer, with or without supplied target; mode switches do not reroute accepted jobs | VM-4 |
| Inspector access | Print-allowed external origin cannot read history or invoke clear/render; Host/origin checks, no-store responses, attachment bytes | VM-5 |
| Browser UX | Submit, select, inspect, copy, download, clear, empty/evicted states, disconnect/reconnect, hostile text, offline assets | VM-6 |
| Rendering | Fake HTTP transport validates fixed HTTPS URL, raw request body, settings snapshot, and zero-based labels; disabled rendering makes no calls; failures, multiple labels, cancellation, response/image bounds, and capture independence | VM-7 |
| Desktop | Save/cancel, mode indicators, browser launch URL/failure, HTTP port change | VM-8 |
| Integrated flow | Live HTTP Virtual request captured, listed and downloaded while a gated fake renderer runs; Virtual makes no fake-printer connection; Physical delivers exact bytes to a local TCP fake printer | VM-9 (`internal/server/integration_test.go`) |

Use fake render responses and a local TCP capture server for automated
integration tests. Test renderer errors and rate limits deterministically; do
not call Labelary or require credentials/network access in the automated suite.
The VM-6 browser and VM-8 desktop/tray review steps are manual; server tests
cover the capture-to-download flow and bundled asset policy without executing
a browser.

## VM-8 desktop review

On macOS and Windows, verify Settings can switch between Physical and Virtual,
edit Labelary enablement/dimensions/density, preserve the saved printer
address/port while switching modes, and Cancel without changing active
settings. Save changes and confirm the listener restarts and the main window
and tray show the selected mode. With Virtual mode active, use Open Inspector
from both the main window and tray; check the URL uses the current HTTP port.
Change the HTTP port and verify the new launch URL follows it. Trigger an open
failure (for example, by stopping the listener or using a test opener) and
confirm the app presents a recoverable error. Verify that no browser launch
occurs when jobs arrive. These native UI checks were not performed by automated
tests.

## VM-7 preview review

Automated preview tests inject fake HTTP/rendering behavior and never contact
Labelary. To manually review the external integration, use only a non-sensitive
test label, explicitly set `destinationType` to `virtual` and
`preview.labelaryEnabled` to `true`, then restart PrintBridge. Submit a likely
ZPL job and verify capture returns before rendering completes. In the
inspector, select the job, choose a zero-based label index, and use Render /
retry; verify the preview and state update. Submit a raw-text job and confirm
that no rendering is attempted. Disable the preference and restart; new jobs
must not be sent and pending work must be canceled. Do not test with private
customer label data because enabled rendering shares payload bytes with the
external provider. Public-service review is optional and is not an automated
acceptance check.

## VM-4 local review

1. Run `go test -v ./internal/server` to exercise Virtual and Physical API tests.
   The tests use simulated printers and temporary listeners, not real hardware.
2. Quit PrintBridge (closing its window only hides it). Back up the config file
   shown in Details, then set `"destinationType": "virtual"` in the version-1
   file. On macOS this is `~/Library/Application Support/PrintBridge/config.json`.
   If still using a legacy file, save Settings once before quitting to upgrade it.
3. Start `go run .`. With the default HTTP port, run:

```sh
curl -sS http://127.0.0.1:8080/status
curl -sS http://127.0.0.1:8080/print \
  -H 'Content-Type: application/json' \
  -d '{"text":"^XA^FDVirtual test^FS^XZ"}'
curl -sS http://127.0.0.1:8080/print \
  -H 'Content-Type: application/json' \
  -d '{"printerHostname":"printer.invalid","printerPort":9100,"text":"raw text"}'
```

Status should report `destination: "virtual"`, `reachable: true`, empty `host`,
and the configured default printer port. Both submissions return `message: "OK"`,
`destination: "virtual"`, and distinct `jobId` values, without printer connections.
An empty `text` returns 400. Adapt the URL if your HTTP port differs.

There is no history/dashboard route yet; VM-4 tests verify stored bytes through
internal snapshots. View Logs still shows only request metadata. Preview settings
have no external effect until VM-7. Quit and restore the backed-up configuration
when finished. Switching destinations or HTTP ports retains history during the
same app session; quitting clears it. These steps are a review guide, not a claim
that interactive desktop checks have already been performed.

## VM-6 browser review

With PrintBridge running in Virtual mode, open `http://127.0.0.1:<port>/inspector/`
in a browser. Submit a job containing CR/LF, tabs, NUL, Unicode, and markup
using the documented `/print` request. Verify it appears without a page reload;
select it and inspect Raw text, Visible, and Hex views. Check byte offsets,
copy feedback, original download bytes, preview-unavailable state, clear
confirmation, and the cleared-selection message. Stop the listener while the
page is open and verify the disconnected message; restart it and verify
automatic recovery. Inspector assets are embedded and loaded from the local
listener, with no CDN or frontend server. Automated tests validate the
capture-to-download route, bundled assets, safe text-rendering APIs, and
security policy; they do not execute a full browser engine.

## Native smoke checklist (VM-8 / VM-9)

These interactive and packaged-install checks were not performed in this
session. Windows runtime/installer execution was unavailable here; interactive
macOS desktop/browser verification was not run. The checked-in native CI
matrix builds and tests Go code on Linux, macOS, and Windows; the release
workflow builds the macOS DMG and Windows MSI on native runners. Workflow
definitions and embedded dashboard asset tests were reviewed, but that is not
equivalent to running the tray app or installing the packages.

- [ ] macOS: mode selection, tray indicator, Open Inspector, offline capture,
  copy/download/clear, port change, quit/relaunch with empty history.
- [ ] Windows: the same workflow, including the default browser launch.
- [ ] Packaged DMG/MSI: dashboard assets are included and no development server
  is needed; installation and tray behavior remain functional.
- [ ] Optional live Labelary check: explicitly enable rendering and submit only
  a synthetic label; verify dimensions and multiple labels, then disable it.
- [ ] Record any physical-printer validation separately from fake TCP tests;
  rendering a preview is not proof of printer fidelity.

These checkboxes remain open until the checks are performed. Record
platform/browser, commands, outcomes, and unavailable environments in the
sprint verification record. No preview, whether generated locally or by Labelary,
proves physical printer fidelity; offline capture and inspection do not
provide offline ZPL rendering.
