# Business Requirements Document — printer-bridge

Read `00-overview/project-brief.md` first. This document expands the brief into concrete business-level requirements. It does not define technical implementation — see `03-architecture/` for that.

## 1. Background

Zebra Browser Print is a widely used local agent that lets browser-based web apps print to thermal/label printers by bypassing the OS print dialog. It is approaching end of support. Businesses and developers with web apps built against it need a replacement path. printer-bridge addresses the underlying capability — local, browser-callable, raw printer access — independent of any one vendor's tooling.

## 2. Business goal

Ship a free, working, publicly showcasable local print agent for macOS and Windows that demonstrates a credible, unaffiliated alternative to Zebra Browser Print for the core use case: a web app sending raw print data to a network printer.

This is a portfolio/showcase deliverable, not a funded commercial product. Scope, budget, and support commitments should reflect that throughout.

## 3. Stakeholders and users

| Stakeholder | Interest |
|---|---|
| Project owner (you) | A working, demonstrable v1 that can be shown publicly as evidence of capability; minimal ongoing support burden |
| Web developers / integrators | A stable local HTTP API they can call from their own app to print labels, with clear docs and predictable error behavior |
| End users (non-technical) | Able to install and run the app with minimal friction, despite it being unsigned |

## 4. Business requirements

**BR-1 — Cross-platform availability.** The app must run on both macOS and Windows, since target integrators cannot be assumed to use only one platform.

**BR-2 — No cost to the user.** The app must be free to download and use. No license key, account, or payment step.

**BR-3 — No cost to the maintainer beyond hosting/distribution.** v1 must not require paid code-signing certificates or notarization. See `01-business/constraints.md`.

**BR-4 — Generic printer support.** The bridge must not be hardcoded to Zebra devices. Any printer accepting raw data over a TCP port (Zebra/ZPL, Datamax, SATO, etc.) must be supported, since the target host/port is supplied per request.

**BR-5 — Low integration friction.** A web developer must be able to start sending print jobs with a small number of lines of client-side code (a `fetch()`/HTTP call), without installing an SDK or registering the app.

**BR-6 — Predictable failure behavior.** If a printer is unreachable or a print job fails, the caller must receive a clear error response within a few seconds — not an indefinite hang. This is a business requirement, not just a technical one: integrators building user-facing print flows need to show their own users a timely error.

**BR-7 — Transparent non-affiliation.** Because the project is explicitly positioned as an alternative to a Zebra product, it must be unambiguous — in the app, the repository, and install docs — that printer-bridge is independent and not affiliated with, endorsed by, or supported by Zebra Technologies or any printer manufacturer.

**BR-8 — Low support burden.** Given this is maintained by a single person alongside other work, v1 must favor features that reduce support requests (clear errors, visible logs, install-workaround docs) over features that expand scope (multi-printer profiles, format conversion, auto-update).

## 5. Out of scope for v1

- Commercial licensing, paid tiers, or support SLAs
- Auto-start on login (see `constraints.md`)
- Code signing / notarization (see `constraints.md`)
- Token/API-key authentication beyond CORS
- PDF/image-to-ZPL conversion
- Saved multi-printer profiles or an address book
- Auto-update mechanism

Items in this list may become v2 backlog candidates — see `08-backlog/epics.md` — but must not be built as part of v1 without an explicit scope change from the project owner.

## 6. Assumptions

- Target printers are already on the same local network as the machine running printer-bridge and accept raw data on a TCP port (commonly 9100).
- Integrators are comfortable making a same-machine HTTP call from their web app's JavaScript (i.e., they control the calling web app's origin and can add it to the CORS allow-list).
- End users are willing to follow a short, documented workaround (a few clicks) to run an unsigned app — this is treated as acceptable friction for a free tool, not a blocker.

## 7. Risks

| Risk | Impact | Mitigation |
|---|---|---|
| Unsigned installer scares away non-technical users | Lower adoption of the showcase | Clear, screenshot-based install docs; frame it honestly as a free/open tool |
| CORS-only security model is misunderstood as "insecure" by a technical reviewer | Reputational, for a portfolio piece | Document the security model and its rationale explicitly (`03-architecture/security-model.md`), matching how Browser Print itself works |
| Scope creep during build (agent or owner adding "obvious" features) | Delays v1, burns solo-maintainer time | `03-architecture/decisions.md` explicitly logs and blocks reintroducing cut features |
| Trademark/branding confusion with Zebra | Legal/reputational | Explicit non-affiliation disclaimer everywhere the product is named or shown (`09-legal/disclaimer.md`) |
