# API behavior — Virtual Mode

Status: submission, readiness, inspector, and preview APIs are implemented.
[`openapi.yaml`](openapi.yaml) describes the current API. Integration evidence
and remaining native release checks are recorded in the
[Virtual Mode sprint](../08-backlog/virtual-mode-sprint.md).

## Destination selection

The saved application setting chooses Physical or Virtual globally. Requests
cannot override it. Snapshot the active destination when accepting each job.
In Virtual mode, neither print submission nor readiness checks may contact a
physical printer, even if a request supplies its address.

## POST /print

Continue accepting `application/json` with `text`, optional `printerPort`, and
`printerHostname`. Physical mode still requires the hostname and preserves its
existing defaults, validation, status codes, and `{ "message": "OK" } response.

Virtual mode:

- Require non-empty `text`; do not trim, normalize, or transform it.
- Permit the hostname to be omitted. Supplied hostname/port are descriptive
  metadata only and never cause a network connection. Supplied fields retain
  their JSON types, but virtual capture does not require a reachable address.
- Accept the existing port convention (omitted/zero uses the configured
  default); reject explicitly out-of-range ports as malformed input.
- Store `[]byte(decodedText)`, exactly the bytes the existing physical path
  would send. JSON escape spelling and the HTTP envelope are not captured.
- Return HTTP 200 only after successful capture, with `message: "OK"`,
  `destination: "virtual"`, and `jobId`. This is not a rendering-success signal.
- Accept one JSON object; reject null known fields and trailing documents/data.
  Unknown fields are ignored and cannot override the saved destination.
- Return 400 for invalid JSON, field types, or missing/empty text; 413 when
  request/job size limits are exceeded; 503 if capture is unavailable. Use the
  existing `{ "message": "..." } error shape.
- Limits: 8 MiB HTTP body, 1 MiB decoded payload, and 1,024 bytes of normalized
  host:port metadata. An omitted host is stored as `:port` (for example `:9100`);
  it never uses the configured default printer address.
- Preview queue pressure does not reject an otherwise captured job. Show an
  actionable preview state and allow retry later.

Example virtual request:

```json
{ "text": "^XA^FO50,50^ADN,36,20^FDTest Label^FS^XZ" }
```

Example response:

```json
{ "message": "OK", "destination": "virtual", "jobId": "<opaque-id>" }
```

No PDF, base64, multipart, or arbitrary binary request format is introduced.

## GET /status

Physical mode keeps the current host requirement, TCP probe, and response.
Virtual mode accepts requests with or without `host`/`port` and performs no
probe. An omitted/blank query port uses the configured default. An explicit
query port must be 1–65535; zero remains invalid, preserving Physical `/status`
behavior (JSON `printerPort: 0` on `/print` still means the default).

Return HTTP 200 with `destination: "virtual"` and `reachable` describing the
capture sink's readiness, independent of Labelary/internet availability. Keep
`host` and `port` fields for compatibility: echo supplied host (empty string
when absent) and resolved port as metadata. They are not a physical-reachability
claim. Return `reachable: false` if capture is unavailable.

```json
{ "reachable": true, "destination": "virtual", "host": "", "port": 9100 }
```

Clients wanting the active destination can use the added discriminator; older
clients checking `reachable` can continue their preflight flow without hardware.

## GET /ping

Unchanged: process liveness only, with `{ "message": "pong" }.

## Inspector routes (VM-5)

The inspector API is served on the same loopback listener and is independent of
the integration CORS allow-list. Its canonical origin is
`http://127.0.0.1:<configured-port>`; the `Host` header must be exactly
`127.0.0.1:<configured-port>`. A supplied `Origin` must exactly match that
origin; `Origin: null` and foreign origins are rejected. For reads, a missing
Origin is allowed for top-level navigation and local non-browser clients, but
if `Sec-Fetch-Site` is supplied it must be `same-origin` or `none`. Mutations
require the exact Origin and, when present, `Sec-Fetch-Site: same-origin`.
Print-allowed web origins do not gain access to inspector data or actions.

| Method and path | Behavior |
|---|---|
| `GET /inspector/` | Serves the bundled offline dashboard. |
| `GET /api/inspector/jobs` | Newest-first metadata, bounded to the store's maximum of 100 jobs, plus the current `previewEnabled` flag. |
| `GET /api/inspector/jobs/{jobId}` | Job metadata and relative download/preview URLs; no payload bytes. |
| `GET /api/inspector/jobs/{jobId}/download` | Exact decoded-text bytes, `application/octet-stream` attachment. |
| `GET /api/inspector/jobs/{jobId}/preview` | Cached PNG only; does not start rendering. Returns 409 with the preview state if unavailable. |
| `POST /api/inspector/jobs/{jobId}/preview` | Explicitly queues/retries a render using JSON `labelIndex` (zero-based, 0–99; defaults to 0). New likely-ZPL captures are also queued automatically when `previewEnabled` is true. |
| `DELETE /api/inspector/jobs` | Clears session history and invalidates pending preview results. |

Job payloads are limited to 1 MiB and cached previews to 4 MiB. Missing,
cleared, or evicted IDs return 404. Inspector responses use `Cache-Control:
no-store`, `X-Content-Type-Options: nosniff`, framing protection, and
same-origin resource policy. Downloads are attachments and are never served
as HTML. Render/retry and clear require the canonical local Origin,
independently of the integration CORS allow-list. Rendering is disabled by
default and is only
available in Virtual mode when explicitly enabled. New likely-ZPL jobs are
rendered asynchronously; pre-existing jobs are sent only after the explicit
render action. Labelary settings are snapshotted for each attempt. The request
is a fixed HTTPS POST with raw ZPL in the body; payloads are never in URLs.
Workers, queue slots, provider duration, response bytes, image dimensions, and
retained preview bytes are bounded. Render failures leave the captured payload
available and do not change `/print` success.

The dashboard accepts a zero-based label index for multi-label jobs and shows
disabled, queued, rendering, ready, unsupported, and failed states. ZPL
detection is only a heuristic based on `^XA`/`^XZ`; it is not validation.

The dashboard and its assets are bundled and have no remote dependencies. It
refreshes metadata at a bounded interval and fetches payloads only when a job
is selected. These routes do not provide physical replay or modify routing mode.
