# Constraints — printer-bridge v1

These are hard boundaries for v1. Unlike requirements (which describe what to build), constraints describe limits the build must operate within. An agentic tool building from this playbook should treat every item here as fixed unless the project owner explicitly changes it — do not "improve past" a constraint without flagging it first.

## Budget

- **No paid Apple Developer Program enrollment ($99/yr)** for v1. This means no macOS notarization. The macOS build will be unsigned and will trigger a Gatekeeper warning on first launch; this is accepted, not a defect. See `07-build-deploy/installer-macos.md` for the documented user workaround.
- **No paid Windows code-signing certificate (~$200–400/yr)** for v1. The Windows build will be unsigned and will trigger a SmartScreen warning on first run. Accepted, not a defect. See `07-build-deploy/installer-windows.md`.
- No paid infrastructure — GitHub (repo, Actions, Releases) is the full distribution channel. No hosted update server, license server, or backend.

## Team

- Solo maintainer (project owner). No dedicated QA, support, or design function.
- Documentation and error messages must be clear enough to be self-service — there is no support team to lean on.

## Time / maintenance

- This is a side/portfolio project, not a funded initiative with a hard deadline — but scope must stay tight enough that v1 is realistically finishable and maintainable by one person.
- Favor low-maintenance choices: fewer moving parts, fewer background processes, fewer things that can silently break between OS updates.

## Platform

- Must run on macOS and Windows. Linux is not a v1 target (may be considered later given the backend is already Go and largely portable).
- No dependency on a specific browser or browser extension — the bridge must work with any browser calling its local HTTP API.

## Explicitly excluded mechanisms (see `03-architecture/decisions.md` for full rationale)

| Excluded | Why it's excluded for v1 |
|---|---|
| Auto-start on login (LaunchAgent / Windows Startup entry) | Adds install/uninstall complexity and, on macOS, triggers explicit "Background Items" user notifications that read as suspicious for an unfamiliar free tool. Manual launch avoids this entirely and matches the "run when needed" usage pattern. |
| Code signing / notarization | Budget constraint above. Manual-launch-only design reduces the downside of being unsigned, since the app isn't silently running in the background. |
| Token/API-key authentication | Matches Zebra Browser Print's own trust model (CORS-only). Adding auth would require the caller's web app to manage and transmit a secret client-side, which doesn't meaningfully improve security for a same-machine, localhost-only tool and adds integration friction that works against BR-5 (low integration friction). |
| Format conversion (PDF/image → ZPL) | Out of scope — this is a passthrough bridge, not a rendering engine. Significant scope/complexity increase for a free tool. |

## What is NOT constrained

To avoid the opposite failure mode — treating everything as fixed — these are open and should be decided in the relevant architecture/design docs, not assumed from this file:

- Choice of GUI toolkit for the tray app (decide in `03-architecture/architecture-overview.md`)
- Exact config file format/location (decide in `06-config/config-schema.md`)
- Log rotation policy specifics (decide in `03-architecture/architecture-overview.md`)
