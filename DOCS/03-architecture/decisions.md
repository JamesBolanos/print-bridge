# Architecture Decision Log — print-bridge

Each entry records a decision, the alternatives considered, and why the
alternative was rejected for the current beta. **Read this before proposing any
change to the areas covered here** — these were deliberate scope cuts, not gaps
to fill in.

---

## ADR-001: Manual launch, not auto-start on login

**Decision:** print-bridge does not register as a startup item (no macOS LaunchAgent, no Windows Startup registry entry). The user opens it manually when needed.

**Alternatives considered:**
- Menu bar/tray app with auto-start — originally the default assumption, since "always available" is the natural expectation for a tray app.
- Headless background service (Windows Service / macOS LaunchDaemon) with no GUI.

**Why rejected:** Auto-start adds real install/uninstall complexity (registering and cleanly deregistering a startup entry across OS versions) and, on modern macOS, explicitly surfaces a "Background Items Added" notification to the user — which reads as suspicious for an unfamiliar free tool from an unsigned developer. Given the app is also unsigned/unnotarized (ADR-003), an auto-starting background process compounds trust concerns. Manual launch avoids this entirely and fits the actual usage pattern: users print in bursts, not continuously.

**Do not reintroduce without:** explicit project owner sign-off, and ideally paired with code signing/notarization (ADR-003) to reduce the trust-friction this would otherwise add.

---

## ADR-002: CORS-only access control, no token/API-key auth

**Decision:** No authentication layer beyond the browser's CORS enforcement of an allow-listed set of origins.

**Alternatives considered:**
- Shared token/API key that the calling web app includes in each request.

**Why rejected:** See `03-architecture/security-model.md` for full rationale. Summary: a client-side token is visible in browser dev tools and doesn't meaningfully raise the security bar for a localhost-only tool, while adding integration friction that works against BR-5 (low integration friction). This matches a common trust model for local browser-to-desktop bridge tools.

**Do not reintroduce without:** a specific, articulated threat this addresses that CORS doesn't already cover — not "more auth is generally better."

---

## ADR-003: Free ad-hoc signing only for the current beta

**Decision:** The current beta does not require macOS notarization (Apple Developer Program) or a Windows code-signing certificate. The macOS release workflow applies free ad-hoc signing to make the app bundle structurally valid, then documents the Gatekeeper/quarantine workaround for downloaded builds.

**Alternatives considered:**
- Pay for both certificates upfront as part of the current beta build cost.
- Pay for one platform only.

**Why rejected:** Explicit budget constraint (`01-business/constraints.md`) — this is a free showcase project with no committed revenue to justify ~$300–500/yr in certificate costs before it's proven to have an audience. Manual-launch-only design (ADR-001) reduces the downside of shipping unsigned, since the app isn't silently running in the background — the OS warning appears once, at a moment the user is actively choosing to open the app.

**Do not require paid signing without:** the project owner deciding to invest in signing, typically once there's evidence of real usage/demand justifying the cost. Keep the free ad-hoc signing and verification step in CI so the app bundle is not structurally invalid.

---

## ADR-004: Single-process architecture (GUI + HTTP server together)

**Decision:** The Fyne GUI and the Gin HTTP server run in the same process/binary, not as separate processes (e.g., a headless agent plus a separate GUI controller talking to it over IPC).

**Alternatives considered:**
- Separate background agent process + separate GUI process communicating over a local socket or IPC.

**Why rejected:** A split-process design is the natural architecture *if* auto-start were in scope (headless agent always running, GUI launched on demand to manage it) — but since auto-start was cut (ADR-001), there's no current-beta scenario where the agent needs to run without the GUI. A single process is simpler to build, install, uninstall, and reason about, consistent with the "few moving parts" architectural value.

**Do not reintroduce without:** auto-start (ADR-001) being reinstated first — a process split only earns its complexity if the agent needs to outlive the GUI window.

---

## ADR-005: Raw passthrough only, no format conversion

**Decision:** `/print` accepts raw printer-language text (ZPL/EPL/etc.) and writes it directly to the TCP socket. No PDF/image rendering or conversion.

**Alternatives considered:**
- Accept PDF/image payloads and convert to ZPL/raster on the bridge side.

**Why rejected:** Significant scope and complexity increase (rendering engine, per-printer-language conversion logic) for a free tool with one maintainer. The calling web app is expected to already generate raw printer-language data, which is the standard pattern for label-printing integrations.

**Do not reintroduce without:** clear evidence this is blocking real adoption, and likely as a future scoped addition, not a current-beta amendment.
