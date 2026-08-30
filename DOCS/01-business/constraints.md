# Constraints — print-bridge current beta

These are hard boundaries for the current beta. Unlike requirements, which
describe what to build, constraints describe limits the project must operate
within. Treat every item here as fixed unless the maintainer explicitly changes
it.

## Budget

- **No paid Apple Developer Program enrollment ($99/yr)** for the current release path. The macOS workflow uses free ad-hoc signing so the `.app` bundle is structurally valid, but the downloaded app can still trigger Gatekeeper warnings or require the quarantine workaround. This is accepted, not a defect. See `07-build-deploy/installer-macos.md`.
- **No paid Windows code-signing certificate (~$200–400/yr)** for the current beta. The Windows build will be unsigned and will trigger a SmartScreen warning on first run. Accepted, not a defect. See `07-build-deploy/installer-windows.md`.
- No paid infrastructure — GitHub (repo, Actions, Releases) is the full distribution channel. No hosted update server, license server, or backend.

## Team

- Solo maintainer (project owner). No dedicated QA, support, or design function.
- Documentation and error messages must be clear enough to be self-service — there is no support team to lean on.

## Time / maintenance

- This is a side/portfolio project, not a funded initiative with a hard deadline — but scope must stay tight enough that the current beta is realistically finishable and maintainable by one person.
- Favor low-maintenance choices: fewer moving parts, fewer background processes, fewer things that can silently break between OS updates.

## Platform

- Must run on macOS and Windows. Linux is not a current-beta target (may be considered later given the backend is already Go and largely portable).
- No dependency on a specific browser or browser extension — the bridge must work with any browser calling its local HTTP API.

## Explicitly excluded mechanisms (see `03-architecture/decisions.md` for full rationale)

| Excluded | Why it's excluded from the current beta |
|---|---|
| Auto-start on login (LaunchAgent / Windows Startup entry) | Adds install/uninstall complexity and, on macOS, triggers explicit "Background Items" user notifications that read as suspicious for an unfamiliar free tool. Manual launch avoids this entirely and matches the "run when needed" usage pattern. |
| Paid Developer ID signing / notarization | Budget constraint above. The workflow uses free ad-hoc signing only. Manual-launch-only design reduces the downside of unsigned distribution, since the app isn't silently running in the background. |
| Token/API-key authentication | Matches a common localhost bridge trust model (CORS-only for browser callers). Adding auth would require the caller's web app to manage and transmit a secret client-side, which doesn't meaningfully improve security for a same-machine, localhost-only tool and adds integration friction that works against BR-5 (low integration friction). |
| Format conversion (PDF/image → ZPL) | Out of scope — this is a passthrough bridge, not a rendering engine. Significant scope/complexity increase for a free tool. |

## What is NOT constrained

To avoid the opposite failure mode — treating everything as fixed — these are open and should be decided in the relevant architecture/design docs, not assumed from this file:

- Choice of GUI toolkit for the tray app (decide in `03-architecture/architecture-overview.md`)
- Exact config file format/location (decide in `06-config/config-schema.md`)
- Log rotation policy specifics (decide in `03-architecture/architecture-overview.md`)
