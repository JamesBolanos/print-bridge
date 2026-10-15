# Backlog — Epics

Read all prior sections before starting build work from this file — each epic references the specific docs that define its detailed requirements. Stories under each epic are in `08-backlog/stories.md`.

## Current Beta Epics

### E1 — Core HTTP API
Implement `/ping`, `/print`, `/status` per `04-api/openapi.yaml`, with connect/write timeouts per `03-architecture/architecture-overview.md`.

### E2 — Configuration & persistence
Read/write `config.json` per `06-config/config-schema.md`. Wire config values (HTTP port, allowed origins, default printer port/address) into the HTTP server at startup and on settings change.

### E3 — Tray/GUI application
Build the Fyne-based main window, Settings screen, Logs viewer, and tray icon per `05-ux/tray-app-spec.md`. Wire the GUI to the config store (E2) and to a controllable start/stop of the HTTP server (E1) for the restart-on-settings-save behavior.

### E4 — Logging
Rotating local log file per `03-architecture/architecture-overview.md`, written on every API request, readable from the Logs viewer (E3).

### E5 — Build & packaging
Update CI to build native per-platform (per `07-build-deploy/build-pipeline.md`), produce `.msi` (Windows) and `.dmg` (macOS) installers per the platform-specific docs, with unsigned-build user messaging baked into install docs.

### E6 — Legal & disclaimer
Non-affiliation disclaimer surfaced in-app (E3's main window) and in repo/install docs, per `09-legal/disclaimer.md`.

## Suggested build order

E1 and E2 first (they're the testable core — can be verified with `curl` before any GUI exists, same as the current prototype). E3 next, since it depends on both. E4 can be built in parallel with E3 once E1's request handling exists to log against. E5 and E6 last, once there's a working app to package and disclaim.

---

## Future / Deferred backlog

Not part of the current beta. Listed here so they're captured, not forgotten,
and recognized as intentionally out of scope rather than missing requirements.
Each maps to a specific ADR in `03-architecture/decisions.md`.

| Item | Related ADR | Trigger for reconsidering |
|---|---|---|
| Auto-start on login | ADR-001 | User feedback indicates manual launch is a real adoption blocker |
| Code signing (Windows) / notarization (macOS) | ADR-003 | Evidence of real usage justifying ~$300–500/yr cost |
| Token/API-key authentication | ADR-002 | A specific, articulated threat model CORS doesn't cover |
| PDF/image-to-ZPL conversion | ADR-005 | Clear integrator demand; would need its own scoping pass, not a current-beta amendment |
| Saved multi-printer profiles / address book | BRD out-of-scope | UX feedback that re-typing printer address per session is a real friction point |
| Auto-update mechanism | BRD out-of-scope | Once there are enough active users that manual re-download is a support burden |
| DNS-rebinding hardening (Host header validation) | `03-architecture/security-model.md` | Low priority given threat model, but cheap to add — reasonable future candidate |
| Rate limiting on API endpoints | `03-architecture/security-model.md` | Only if abuse is actually observed |
| Linux support | `01-business/constraints.md` | Backend is already Go/largely portable; mainly blocked by Fyne GUI testing effort, not architecture |
