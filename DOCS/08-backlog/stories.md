# Backlog — Stories

INVEST-format stories under each epic from `08-backlog/epics.md`. Acceptance
criteria are written to be specific and testable, not vague goals.

---

## E1 — Core HTTP API

**S1.1 — Implement `/print` with configurable printer port**
As a web app developer, I want to send `printerHostname`, optional `printerPort`, and `text` to `/print`, so that I can print to any raw-printing device on any port, not just 9100.
- AC: Request without `printerPort` defaults to 9100.
- AC: Request with `printerPort` uses that value instead.
- AC: Response matches `PrintJobResponse` schema in `04-api/openapi.yaml` on success.
- AC: Missing `printerHostname` or `text` returns 400 with `ErrorResponse` schema.

**S1.2 — Add connect and write timeouts**
As a web app developer, I want `/print` to fail within a few seconds when a printer is unreachable, so that my UI doesn't hang indefinitely.
- AC: TCP connect attempt times out at 3 seconds (per `03-architecture/architecture-overview.md`) and returns 502 with a message indicating connection failure.
- AC: TCP write times out at 5 seconds and returns 500 with a message indicating write failure.
- AC: A successful connect + write within both windows returns 200 as before.

**S1.3 — Implement `/status`**
As a web app developer, I want to check if a printer is reachable before attempting a print, so that I can show the end user a live status indicator.
- AC: `GET /status?host=X&port=Y` performs a connect-only probe (no data written) with the same 3-second timeout as S1.2.
- AC: Returns 200 with `{ reachable: true, host, port }` on successful connect.
- AC: Returns 200 with `{ reachable: false, host, port }` on timeout/refusal — not a 4xx/5xx, per `04-api/openapi.yaml`.
- AC: Missing `host` query param returns 400.
- AC: Omitted `port` defaults to 9100.

**S1.4 — Wire CORS to config instead of env var**
As the app owner, I want allowed origins to come from `config.json`, so that they're editable via the Settings UI instead of requiring an environment variable and restart.
- AC: Server reads `allowedOrigins` from config at startup.
- AC: If `config.json` doesn't exist yet (first run), server falls back to the existing default origin list currently hardcoded in the prototype.
- AC: Changing `allowedOrigins` and triggering a listener restart (S3.4) applies the new list without a full app restart.

---

## E2 — Configuration & persistence

**S2.1 — Read/write `config.json`**
As the app, I need to load settings on startup and persist changes, so that user preferences survive restarts.
- AC: On startup, read config from the platform path in `06-config/config-schema.md`; if absent, create it with defaults from the schema.
- AC: Writes are atomic (write to temp file, rename) to avoid a corrupted config from an interrupted write.
- AC: Malformed existing `config.json` (e.g. corrupted JSON) falls back to defaults rather than crashing the app, and logs a warning (E4).

**S2.2 — Validate settings before persisting**
As a user, I want invalid settings (e.g. out-of-range port) rejected before they're saved, so that I don't lock myself out of a working configuration.
- AC: `httpPort` outside 1024–65535 is rejected with a clear error, config not written.
- AC: `defaultPrinterPort` outside 1–65535 is rejected similarly.
- AC: Malformed origin strings in `allowedOrigins` (not a valid scheme+host) are rejected similarly.

---

## E3 — Tray/GUI application

**S3.1 — Main window with status and disclaimer**
As a user, I want to see at a glance that printer-bridge is running and on what port, so that I can confirm it's ready before printing.
- AC: Main window shows listener status (running/stopped) and current HTTP port.
- AC: Non-affiliation disclaimer text (from `09-legal/disclaimer.md`) is visible on this screen without requiring a click.
- AC: Buttons for Settings, Details, Help, View Logs, Quit are present and functional.

**S3.2 — Close-to-tray behavior**
As a user, I want closing the main window to keep the app running in the tray, so that web apps can keep printing while I'm not looking at the window.
- AC: Clicking the window close control hides the window but does not terminate the process or stop the HTTP listener.
- AC: A one-time notice (first close only, per session or per install — pick one, document the choice in the story's implementation) informs the user the app is still running in the tray.
- AC: Clicking the tray icon reopens/focuses the main window.

**S3.3 — Quit terminates cleanly**
As a user, I want Quit to fully stop printer-bridge, so that it's not silently running when I don't want it to be.
- AC: Quit from the main window and Quit from the tray menu both stop the HTTP listener and exit the process.
- AC: No orphaned processes remain after Quit (verify via OS process list in testing).

**S3.4 — Settings screen with restart-on-save**
As a user, I want to change ports and allowed origins and have them take effect without relaunching the app, so that configuration is low-friction.
- AC: Settings screen fields match `06-config/config-schema.md`.
- AC: Save triggers validation (S2.2); on failure, shows the error inline and preserves entered values (does not clear the form).
- AC: Save triggers a clean stop/restart of the HTTP listener with new config values, and shows a confirmation message once restarted.
- AC: Cancel discards changes and does not affect the running listener.

**S3.5 — Logs viewer**
As a user, I want to view recent activity from within the app, so that I can self-diagnose a failed print without hunting for a file on disk.
- AC: Logs screen displays the current rotating log file's contents, most recent entries first.
- AC: Logs screen updates automatically while open.
- AC: Logs screen has no in-window action buttons.

---

## E4 — Logging

**S4.1 — Rotating log writer**
As the app, I need to log every API request, so that users have a record to self-diagnose issues.
- AC: Every `/print`, `/ping`, `/status` request logs timestamp, endpoint, relevant target host:port (for print/status), and outcome (success/error + detail).
- AC: Log file rotates by size (per policy in `03-architecture/architecture-overview.md`) rather than growing unbounded.
- AC: Log writing failures (e.g. disk full) do not crash the app or block the API response — log and continue.

---

## E5 — Build & packaging

**S5.1 — Native per-platform CI builds**
As the maintainer, I need CI to build the Fyne app natively per platform, so that CGO-linked GUI code compiles correctly.
- AC: Windows build runs on a Windows runner; macOS build runs on a macOS runner (per `07-build-deploy/build-pipeline.md`).
- AC: Both builds are triggered on the existing `v*.*.*` tag pattern, matching current release behavior.
- AC: `go test ./...` still runs on every push/PR, independent of the release build.

**S5.2 — Windows installer**
As a user, I want a standard Windows installer, so that installing feels normal despite the app being unsigned.
- AC: WiX Toolset produces a `.msi` installer that places the app in Program Files and creates Start Menu/Desktop shortcuts.
- AC: No Startup/auto-run registry entry is created (verify against ADR-001).
- AC: Installer is attached to the GitHub Release alongside existing artifacts.

**S5.3 — macOS installer**
As a user, I want a standard drag-to-Applications experience, so that installing feels normal despite the app being unsigned.
- AC: `.dmg` contains the `.app` bundle and an `/Applications` symlink.
- AC: `.app` bundle has a valid `Info.plist` with a real bundle identifier.
- AC: `.dmg` is attached to the GitHub Release alongside existing artifacts.

**S5.4 — Unsigned-build user documentation**
As a new user, I want clear instructions for getting past the OS security warning, so that I don't abandon installation thinking something is broken.
- AC: Repo README (or a linked install doc) includes the SmartScreen workaround steps from `07-build-deploy/installer-windows.md`.
- AC: Same doc includes the Gatekeeper workaround steps from `07-build-deploy/installer-macos.md`.
- AC: Both are written in plain, non-alarming language per the tone guidance in those files.

---

## E6 — Legal & disclaimer

**S6.1 — In-app and repo disclaimer**
As the maintainer, I need the non-affiliation disclaimer present everywhere the product is shown, so that there's no trademark/endorsement confusion.
- AC: Disclaimer text from `09-legal/disclaimer.md` appears on the main window (S3.1).
- AC: Same text (or the repo-appropriate variant) appears in the repository README.
- AC: Same appears on any install/download landing page or doc referenced in S5.4.
