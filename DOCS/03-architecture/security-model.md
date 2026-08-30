# Security Model — printer-bridge

## Model: CORS allow-list, no additional authentication

printer-bridge trusts requests based solely on the browser's CORS enforcement of an allow-listed set of origins, configured via the app (see `06-config/config-schema.md`). The allow-list is empty on first launch, so the user must add each trusted web app origin explicitly. There is no API key, token, or session layer in v1.

This deliberately uses a known, established trust model for this category of localhost bridge tool, not a novel security posture.

## Why this is the accepted model for v1

- **The bridge only listens on localhost.** It binds to `127.0.0.1`, so it is not exposed to other machines on the network. Same-machine processes can still reach it.
- **CORS is enforced by the browser, not by printer-bridge.** A browser request from a non-allow-listed origin is blocked by browser CORS handling. This is not general-purpose authentication and does not restrict non-browser local clients such as scripts or native apps.
- **Adding a token would require the calling web app to embed and transmit a secret in client-side JavaScript**, which is visible to anyone who opens browser dev tools. It would not meaningfully raise the security bar for this specific threat model, while adding real integration friction (BR-5) for every developer using the tool.

## Accepted risks (documented, not overlooked)

| Risk | Description | Why it's accepted for v1 |
|---|---|---|
| Any allow-listed origin can target any host:port | Once an origin is allow-listed, a malicious or compromised page on that origin could direct the bridge to open a TCP connection to any reachable host on the local network, not just intended printers | The user controls their own CORS allow-list; only origins they trust should be added. |
| Same-machine non-browser clients bypass CORS | CORS does not stop local scripts, command-line tools, or native apps from calling the localhost API | Accepted for v1 because this is a local desktop utility. The security boundary is the user's machine account and localhost binding, not a secret-bearing API. |
| No rate limiting | A misbehaving page could send many rapid print/status requests | Low-severity for a local, single-user tool; worst case is excessive printer output or log growth, not data exposure |
| DNS rebinding | In theory, a malicious page could attempt DNS rebinding to make `localhost`-restricted logic target the bridge from an origin that shouldn't be allow-listed | Not mitigated in v1. Flagged here as a known limitation; mitigation (e.g., validating the `Host` header) is a reasonable v2 backlog item, not a v1 blocker, given the overall low-severity threat model of a local label-printing tool |

## What must NOT be added in v1

Per `01-business/constraints.md` and `03-architecture/decisions.md`: no token/API-key authentication layer. If a future need arises (e.g., the tool is used in a higher-trust-sensitivity environment), that is a scope change requiring explicit sign-off from the project owner, not an autonomous addition by the build process.

## Non-affiliation as a trust signal

The non-affiliation disclaimer (`09-legal/disclaimer.md`) also serves a security-communication purpose: users should understand this is an independent implementation they're choosing to trust, not a printer-manufacturer product inheriting that manufacturer's support/security posture.
