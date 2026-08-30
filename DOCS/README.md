# printer-bridge — Build Playbook

This folder is the complete specification set for **printer-bridge**, a free, standalone local print agent for macOS and Windows. It is meant to be read and built from by an agentic AI coding tool (Claude Code, Cursor, or equivalent). Every file here is self-contained context — no external conversation history is assumed.

## What this project is

A local desktop application that lets any web app send print jobs to a network-connected thermal/label printer (Zebra, Datamax, SATO, etc.) without going through the OS print dialog. It is a spiritual successor to Zebra Browser Print, which is approaching end of support. printer-bridge is **not affiliated with or endorsed by Zebra Technologies** — see `09-legal/disclaimer.md`.

The project started from a working Go backend prototype (HTTP → TCP passthrough to port 9100). This playbook defines the target v1: that backend evolved into a full desktop app with a GUI, wrapped for distribution on both platforms.

## Read/build order for the agent

Read documents in this order. Each builds on the ones before it — do not skip ahead.

1. `00-overview/project-brief.md` — problem, goal, and scope boundary. Read this first, always.
2. `01-business/brd.md` — business requirements and success criteria.
3. `01-business/constraints.md` — hard constraints (no paid certs, solo maintainer, free tool). Treat these as non-negotiable unless the human explicitly overrides them.
4. `02-process/*.md` — Mermaid diagrams of the print-job flow and the user workflow.
5. `03-architecture/architecture-overview.md` — components and tech stack, with rationale.
6. `03-architecture/security-model.md` — what security model is used and why, and what is explicitly out of scope.
7. `03-architecture/decisions.md` — ADR log. **Read before proposing any architectural change** — many "obvious improvements" (auto-start, auth tokens, paid signing) were deliberately cut for v1 and are logged here with reasoning.
8. `04-api/openapi.yaml` — the API contract. This is the primary source of truth for endpoint behavior; prose elsewhere is explanatory, not authoritative.
9. `05-ux/tray-app-spec.md` — UI states and behavior.
10. `06-config/config-schema.md` — settings fields and storage format.
11. `07-build-deploy/*.md` — build pipeline and installer instructions per platform.
12. `08-backlog/epics.md` then `08-backlog/stories.md` — the actual units of work, in priority order.

## Ground rules for the agent

- **v1 scope is fixed.** Do not add features found in `08-backlog/stories.md` marked as backlog/deferred, even if they seem like natural improvements. Flag them back to the human instead.
- **Do not silently reintroduce cut scope.** Auto-start on login, paid code signing/notarization, and token-based auth were deliberately excluded from v1 — see `decisions.md` before touching any of these areas.
- **The OpenAPI spec is authoritative** for request/response shapes. If a markdown doc and the spec disagree, the spec wins; flag the discrepancy.
- **Diagrams are Mermaid**, embedded directly in markdown files — no external BPMN tooling required.

## Status

This playbook describes the v1 target and should stay in sync with implementation decisions as the new repo evolves.

## Folder structure

```
DOCS/
├── README.md                      ← this file
├── 00-overview/
│   └── project-brief.md
├── 01-business/
│   ├── brd.md
│   └── constraints.md
├── 02-process/
│   ├── bpmn-print-job-flow.md
│   └── bpmn-user-workflow.md
├── 03-architecture/
│   ├── architecture-overview.md
│   ├── security-model.md
│   └── decisions.md
├── 04-api/
│   └── openapi.yaml
├── 05-ux/
│   └── tray-app-spec.md
├── 06-config/
│   └── config-schema.md
├── 07-build-deploy/
│   ├── build-pipeline.md
│   ├── installer-windows.md
│   └── installer-macos.md
├── 08-backlog/
│   ├── epics.md
│   └── stories.md
└── 09-legal/
    └── disclaimer.md
```
