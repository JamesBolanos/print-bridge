# print-bridge — Project Documentation

This folder contains the planning and technical documentation for
**print-bridge**, a free, standalone local print agent for macOS and Windows.
Every file is written to stand on its own, so the project can be reviewed
without private context.

## What this project is

A local desktop application that lets any web app send print jobs to a network-connected thermal/label printer without going through the OS print dialog. print-bridge is vendor-neutral and **not affiliated with or endorsed by any printer manufacturer** — see `09-legal/disclaimer.md`.

The current beta combines a localhost HTTP API, raw TCP printer passthrough,
desktop configuration UI, tray behavior, local logging, and release packaging
notes.

## Virtual Mode sprint status

[Virtual Mode and Browser Inspector](08-backlog/virtual-mode-sprint.md) is
implemented through VM-8, with VM-9 automated integration coverage using a
local fake printer and fake renderer. VM-9 remains open pending interactive
native tray/browser and packaged-installer smoke checks. PDF/binary input is
deferred.

See [implemented Virtual API and inspector behavior](04-api/virtual-mode.md),
the [test inventory and verification plan](07-build-deploy/testing.md), and
the sprint checklist and verification log.

## Suggested Read Order

Read documents in this order when trying to understand or change the project.
Each builds on the ones before it.

1. `00-overview/project-brief.md` — problem, goal, and scope boundary. Read this first, always.
2. `01-business/brd.md` — business requirements and success criteria.
3. `01-business/constraints.md` — hard constraints (no paid certs, solo maintainer, free tool). Treat these as non-negotiable unless the human explicitly overrides them.
4. `02-process/*.md` — Mermaid diagrams of the print-job flow and the user workflow.
5. `03-architecture/architecture-overview.md` — components and tech stack, with rationale.
6. `03-architecture/security-model.md` — what security model is used and why, and what is explicitly out of scope.
7. `03-architecture/decisions.md` — ADR log. **Read before proposing any architectural change** — many "obvious improvements" (auto-start, auth tokens, paid signing) were deliberately deferred and are logged here with reasoning.
8. `04-api/openapi.yaml` — the current API contract. `04-api/virtual-mode.md` records implemented Virtual behavior and inspector routes; update OpenAPI alongside implementation.
9. `05-ux/tray-app-spec.md` — UI states and behavior.
10. `06-config/config-schema.md` — settings fields and storage format.
11. `07-build-deploy/*.md` — build pipeline and installer instructions per platform.
12. `08-backlog/epics.md` and `08-backlog/stories.md` — foundation backlog.
    The completed Virtual Mode sprint's scope, evidence, and remaining native
    release checks are recorded in `08-backlog/virtual-mode-sprint.md`.

## Scope Notes

- **Keep the current beta focused.** Do not add features found in
  `08-backlog/stories.md` marked as backlog/deferred without deliberately
  changing the project scope.
- **Do not silently reintroduce deferred scope.** Auto-start on login, paid code
  signing/notarization, and token-based auth were deliberately excluded from
  the current release scope. See `decisions.md` before touching any of these
  areas.
- **The OpenAPI spec is authoritative** for request/response shapes. If a markdown doc and the spec disagree, the spec wins; flag the discrepancy.
- **Diagrams are Mermaid**, embedded directly in markdown files — no external BPMN tooling required.

## Status

These docs describe the current beta and the approved Virtual Mode sprint.
The sprint checklist distinguishes implemented behavior from outstanding
release verification; future behavior must not be presented as shipped until
its task is complete.

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
│   ├── virtual-capture-store.md
│   ├── security-model.md
│   └── decisions.md
├── 04-api/
│   ├── openapi.yaml
│   └── virtual-mode.md
├── 05-ux/
│   └── tray-app-spec.md
├── 06-config/
│   └── config-schema.md
├── 07-build-deploy/
│   ├── build-pipeline.md
│   ├── installer-windows.md
│   ├── installer-macos.md
│   └── testing.md
├── 08-backlog/
│   ├── epics.md
│   ├── stories.md
│   └── virtual-mode-sprint.md
└── 09-legal/
    └── disclaimer.md
```
