# Security Model — print-bridge

## Model: CORS allow-list, no additional authentication

print-bridge trusts requests based solely on the browser's CORS enforcement of an allow-listed set of origins, configured via the app (see `06-config/config-schema.md`). New installations allow `https://sscc-labels.com` and `https://www.sscc-labels.com` for the SSCC label maker. Users can remove these or add other trusted origins in Settings. Existing installations retain their saved allow-list, including an empty list. There is no API key, token, or session layer in the current beta.

This deliberately uses a known, established trust model for this category of localhost bridge tool, not a novel security posture.

## Why this is the accepted model for the current beta

- **The bridge only listens on localhost.** It binds to `127.0.0.1`, so it is not exposed to other machines on the network. Same-machine processes can still reach it.
- **CORS is enforced by the browser, not by print-bridge.** A browser request from a non-allow-listed origin is blocked by browser CORS handling. This is not general-purpose authentication and does not restrict non-browser local clients such as scripts or native apps.
- **Adding a token would require the calling web app to embed and transmit a secret in client-side JavaScript**, which is visible to anyone who opens browser dev tools. It would not meaningfully raise the security bar for this specific threat model, while adding real integration friction (BR-5) for every developer using the tool.

## Accepted risks (documented, not overlooked)

| Risk | Description | Why it's accepted for the current beta |
|---|---|---|
| Any allow-listed origin can target any host:port | Once an origin is allow-listed, a malicious or compromised page on that origin could direct the bridge to open a TCP connection to any reachable host on the local network, not just intended printers | The user controls their own CORS allow-list; only origins they trust should be added. |
| Same-machine non-browser clients bypass CORS | CORS does not stop local scripts, command-line tools, or native apps from calling the localhost API | Accepted for the current beta because this is a local desktop utility. The security boundary is the user's machine account and localhost binding, not a secret-bearing API. |
| No rate limiting | A misbehaving page could send many rapid print/status requests | Low-severity for a local, single-user tool; worst case is excessive printer output or log growth, not data exposure |
| DNS rebinding | In theory, a malicious page could attempt DNS rebinding to make `localhost`-restricted logic target the bridge from an origin that shouldn't be allow-listed | Not mitigated in the current beta. Flagged here as a known limitation; mitigation (e.g., validating the `Host` header) is a reasonable future backlog item, not a beta blocker, given the overall low-severity threat model of a local label-printing tool |

## What must NOT be added in the current beta

Per `01-business/constraints.md` and `03-architecture/decisions.md`: no token/API-key authentication layer. If a future need arises (e.g., the tool is used in a higher-trust-sensitivity environment), that is a scope change requiring explicit maintainer sign-off.

## Non-affiliation as a trust signal

The non-affiliation disclaimer (`09-legal/disclaimer.md`) also serves a security-communication purpose: users should understand this is an independent implementation they're choosing to trust, not a printer-manufacturer product inheriting that manufacturer's support/security posture.

## Inspector access

Status: implemented in VM-5. Inspector routes expose retained print payloads,
so they use a narrower browser access policy than print submission. The
existing CORS risk table above still describes the integration API. See
[ADR-006](decisions.md) and the [sprint checklist](../08-backlog/virtual-mode-sprint.md).

- Keep the listener on `127.0.0.1` and use a canonical local dashboard origin.
  Inspector routes require `Host: 127.0.0.1:<active-port>`, including requests
  without an Origin header.
- Do not apply the integration origin allow-list to history, payload download,
  previews, clear, or render/retry routes. A website allowed to submit print
  jobs must not gain browser access to other jobs or inspector actions.
- Allow the local dashboard at `http://127.0.0.1:<active-port>` to read its
  data. A supplied Origin must match exactly; null or foreign origins are
  rejected. Missing Origin is permitted on reads for top-level navigation and
  local non-browser clients. If `Sec-Fetch-Site` is present, reads permit only
  `same-origin` or `none`; mutations require exact Origin and permit only
  `same-origin` Fetch Metadata when present.
- Use non-GET methods for clear and render/retry; validate browser origin on
  these actions so a cross-site form cannot clear history or trigger uploads.
- Render raw text using text nodes, not HTML. Serve downloads as attachments
  with a non-executable content type and `nosniff`. Use no-store for job data
  and previews, prevent foreign framing, and avoid remote scripts/assets.
- Keep full payloads out of URLs, normal logs, and persistent app storage.
  User-initiated copies/downloads are intentional exports; clearing the store
  cannot remove copies the user has already exported.
- Bound memory, response/image sizes, and pending work. Clear/eviction must
  prevent stale renderer callbacks from reintroducing payloads.
- Labelary is disabled by default. Explicit enablement discloses that label
  contents leave the machine. Use HTTPS POST to the configured implementation's
  fixed provider endpoint; do not introduce arbitrary renderer URLs this sprint.
  Network failures never cause a physical print or discard captured data.

VM-5 and VM-7 implement metadata listing/detail, attachment download, cached
preview retrieval, clear, and protected render/retry. The renderer only sends
likely-ZPL payloads to Labelary when explicitly enabled; newly captured jobs
are queued only in that mode and old jobs require a user action. The fixed
provider receives HTTPS POST bodies, with bounded worker, queue, duration,
response, and image limits. Provider failures never delete captured data.

This does not add caller API keys or general local-process authentication.
Same-machine non-browser clients remain within the existing trust boundary.
Inspector Host/origin protection is part of this sprint; broader hardening of
the existing print integration routes is separate work.
