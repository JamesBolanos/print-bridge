# Backlog — Epics

Read all prior sections before starting build work from this file — each epic references the specific docs that define its detailed requirements. Stories under each epic are in `08-backlog/stories.md`.

## Foundation Epics (implemented)

### E1 — Core HTTP API
Implement `/ping`, `/print`, `/status` per `04-api/openapi.yaml`, with connect/write timeouts per `03-architecture/architecture-overview.md`.

### E2 — Configuration & persistence
Read/write `config.json` per `06-config/config-schema.md`. Wire config values (HTTP port, allowed origins, default printer port/address) into the HTTP server at startup and on settings change.

### E3 — Tray/GUI application
Build the Fyne-based main window, Settings screen, Logs viewer, and tray icon per `05-ux/tray-app-spec.md`. Wire the GUI to the config store (E2) and to a controllable start/stop of the HTTP server (E1) for the restart-on-settings-save behavior.

### E4 — Logging
Rotating local log file per `03-architecture/architecture-overview.md`, written for the integration API (`/ping`, `/status`, and `/print`) and readable from the Logs viewer (E3). Inspector requests and captured payloads are not logged.

### E5 — Build & packaging
Update CI to build native per-platform (per `07-build-deploy/build-pipeline.md`), produce `.msi` (Windows) and `.dmg` (macOS) installers per the platform-specific docs, with unsigned-build user messaging baked into install docs.

### E6 — Legal & disclaimer
Non-affiliation disclaimer surfaced in-app (E3's main window) and in repo/install docs, per `09-legal/disclaimer.md`.

## Approved Virtual Mode sprint

### E7 — Virtual Mode and Browser Inspector

Add a global Virtual destination that captures ZPL/raw text without contacting
printers, a local browser inspector, and optional online Labelary previews.
Keep physical printing format-agnostic. PDF/binary submission is deferred.

Status: VM-1 through VM-8 implementation and VM-9 automated integration
coverage are complete, including virtual API capture/readiness, protected
inspector routes, the offline dashboard, optional Labelary previews, desktop
controls, and live-listener end-to-end tests. VM-9 remains open pending
interactive macOS/Windows tray/browser and packaged-installer smoke checks.
The ordered tasks, dependencies, acceptance criteria, and verification record are maintained in
[virtual-mode-sprint.md](virtual-mode-sprint.md). Test coverage and verification
limits are in [testing.md](../07-build-deploy/testing.md).

## Historical foundation build order (E1–E6)

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
| PDF submission/viewing and binary-safe API input | E7 deferred scope | A follow-up sprint after ZPL/raw text inspection |
| Fully offline ZPL rendering | ADR-006 | Separately scoped local renderer; E7 only guarantees offline capture/inspection |
| Persistent virtual job history / physical replay | E7 deferred scope | Separate retention and user workflow requirements |
| Saved multi-printer profiles / address book | BRD out-of-scope | UX feedback that re-typing printer address per session is a real friction point |
| Auto-update mechanism | BRD out-of-scope | Once there are enough active users that manual re-download is a support burden |
| Broader DNS-rebinding hardening of existing integration routes | `03-architecture/security-model.md` | Inspector Host/origin protection is required in E7; broader existing-route changes remain separate |
| Rate limiting on API endpoints | `03-architecture/security-model.md` | Only if abuse is actually observed |
| Linux support | `01-business/constraints.md` | Backend is already Go/largely portable; mainly blocked by Fyne GUI testing effort, not architecture |
