# Project Brief — printer-bridge

## Problem

Web applications that need to print to thermal/label printers (barcode labels, shipping labels, receipts) cannot do so directly — browsers don't expose raw printer access, and OS print dialogs don't support the raw printer languages (ZPL, EPL, CPCL) these devices use. Zebra Browser Print has historically filled this gap by running a local background agent that browser JavaScript can call. That tool is approaching end of support, leaving any web app built against it without a supported path forward.

## Goal

Build **printer-bridge**: a small, free, locally-run desktop application for macOS and Windows that any web app can call (via a local HTTP API) to send raw print data to a network-connected printer. It is a generic bridge, not tied to a specific web app or printer brand — the target printer's IP and port are configurable per print job.

This is released as a free showcase project — not a commercial product, not a paid support offering.

## Non-affiliation

printer-bridge is an independent, unofficial project. It is **not affiliated with, endorsed by, or connected to Zebra Technologies** or any printer manufacturer. This must be stated in the application UI, the repository, and installation instructions. See `09-legal/disclaimer.md` for the exact text to use.

## What v1 is

- A GUI desktop app (Mac + Windows) the user launches manually when they need to print.
- Minimizes to the system tray/menu bar while running; fully quits when the user quits it.
- Settings UI to configure: local HTTP port, target printer port, default printer address.
- Local HTTP API: `POST /print` (raw passthrough), `GET /ping` (liveness), `GET /status` (printer reachability check).
- CORS-based access control, matching the trust model used by Zebra Browser Print.
- Socket-level write/read timeout so an unreachable printer fails fast instead of hanging.
- Local rotating log file, viewable from within the app.
- Unsigned installers for both platforms (`.msi` via WiX Toolset on Windows, `.dmg` on macOS), with documented user-facing workarounds for the resulting OS security warnings.

## What v1 explicitly is not

- **Not auto-starting on login.** The user opens it when needed; it does not register as a background/startup item.
- **Not code-signed or notarized.** No paid Apple Developer or Windows signing certificate in v1 — this is a documented, accepted trade-off for a free tool, not an oversight.
- **Not authenticated beyond CORS.** No API key/token layer in v1.
- **Not a format converter.** It passes raw printer-language text through as-is; it does not convert PDFs or images to ZPL/EPL.
- **Not multi-printer-profile management.** The caller supplies the target IP/port per request; there's no saved address book in v1.

These exclusions are deliberate scope cuts, not gaps — see `03-architecture/decisions.md` for the reasoning behind each, so they aren't silently reintroduced during the build.

## Primary users

- **Web developers / integrators**: call the local API from their own web app to print labels.
- **End users**: run the tray app on their machine so their browser-based tool can print; interact with it only to configure ports/printer address and to see logs/status.

## Success criteria for v1

- A web page can successfully print a ZPL label to a network Zebra (or compatible) printer via the local API on both macOS and Windows.
- An unreachable printer produces a clear error within a few seconds, not a hang.
- A non-technical user can install and run the app on an unsigned macOS/Windows build by following the documented workaround steps.
- The project can be shown publicly (GitHub, portfolio) as a working, free, unaffiliated alternative to Zebra Browser Print for this specific use case.
