# Process — Print Job Flow

Read `04-api/openapi.yaml` alongside this document for exact request/response shapes; this file describes the *flow*, the spec is authoritative for the *contract*.

## Actors

- **Web App** — any browser-based application, running on an origin present in print-bridge's CORS allow-list.
- **print-bridge** — the local app, listening on `http://localhost:<http_port>`.
- **Printer** — a network device accepting raw data on a TCP port (typically 9100).

## Happy path

```mermaid
sequenceDiagram
    participant W as Web App (browser)
    participant B as print-bridge (local)
    participant P as Printer (network)

    W->>B: GET /status?host=<printer_ip>&port=<printer_port>
    B->>P: TCP connect (probe)
    P-->>B: connection accepted
    B-->>W: 200 OK { reachable: true }

    W->>B: POST /print { printerHostname, printerPort, text }
    B->>B: validate request body
    B->>P: TCP connect
    P-->>B: connection accepted
    B->>P: write raw data (ZPL/EPL/etc.)
    B->>P: close connection
    B-->>W: 200 OK { message: "OK" }
    P->>P: prints label
```

The `/status` pre-flight call is optional but recommended for integrators who want to show the end user a live "printer ready" indicator before attempting a print.

## Failure paths

```mermaid
flowchart TD
    A[Web App sends POST /print] --> B{Origin allowed by CORS?}
    B -- No --> B1[Browser blocks request client-side\nfor normal browser fetch calls]
    B -- Yes --> C{Request body valid JSON\nwith required fields?}
    C -- No --> C1[400 Bad Request\nerror: malformed request]
    C -- Yes --> D{TCP connect to\nprinterHostname:printerPort\nwithin timeout window?}
    D -- No / timeout --> D1[502/504-style error\nerror: printer unreachable]
    D -- Yes --> E{Write completes\nwithin timeout window?}
    E -- No --> E1[500-style error\nerror: write failed or timed out]
    E -- Yes --> F[200 OK]
```

## Notes for implementation

- The **CORS check happens in the browser**, not something print-bridge can control beyond setting the correct response headers. For normal browser `fetch()` calls, the web app sees a browser-level CORS error rather than a print-bridge error response if the origin is not allow-listed. CORS does not block same-machine non-browser clients.
- Both the connect step and the write step must be bounded by a timeout (see `03-architecture/architecture-overview.md` for the specific value) so BR-6 (predictable failure behavior) holds even when a printer is powered on but unresponsive, not just when it's fully offline.
- Every request (success or failure) should be written to the local rotating log, including the target host/port, so a user can self-diagnose repeated failures without contacting support.
