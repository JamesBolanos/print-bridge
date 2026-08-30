# Config Schema — printer-bridge

Read `05-ux/tray-app-spec.md` alongside this — that document defines how these fields are edited; this document defines what they are, where they live, and how they're validated.

## Storage location

A local JSON file, platform-appropriate path:

| Platform | Path |
|---|---|
| macOS | `~/Library/Application Support/PrinterBridge/config.json` |
| Windows | `%APPDATA%\PrinterBridge\config.json` |

Rationale: a plain file, not a database, per the "few moving parts" architectural value in `03-architecture/architecture-overview.md`. JSON chosen over YAML/TOML since Go's standard library handles it with no extra dependency, and there's no need for human hand-editing to be especially pleasant — the Settings UI is the intended edit path.

## Schema

```json
{
  "httpPort": 8080,
  "defaultPrinterPort": 9100,
  "defaultPrinterAddress": "",
  "allowedOrigins": []
}
```

| Field | Type | Default | Validation |
|---|---|---|---|
| `httpPort` | integer | `8080` | 1024–65535 (avoid requiring elevated privileges for ports below 1024); must not already be in use at listener start |
| `defaultPrinterPort` | integer | `9100` | 1–65535; pre-fills the `printerPort` field for convenience but does not restrict what a caller can send in a `/print` request — the API itself accepts any port per-request |
| `defaultPrinterAddress` | string | `""` (empty) | Optional. Free text (IP or hostname). Not validated as reachable at save time — reachability is checked live via `/status`, not enforced here |
| `allowedOrigins` | array of strings | `[]` | Each entry must be a valid origin (scheme + host + optional port, no path). Empty by default; no browser web app can successfully call the API until the user adds the required origin(s) |

## Default `allowedOrigins`

Ship with no allowed browser origins by default:

```
[]
```

The user adds the origin(s) they actually need via Settings. Examples include local development origins such as `http://localhost:3000` or production origins such as `https://app.example.com`. Document clearly (in install/onboarding docs, not this file) that **the calling web app's origin must be added here** — this is the single most common integration stumbling block for a CORS-only tool.

## Relationship to the API

- `defaultPrinterPort` and `defaultPrinterAddress` are **UI/settings conveniences only** — they pre-fill values for testing from within the app (see "Recent activity" / any built-in test-print feature) but do not constrain what a `/print` request can specify. The API contract in `04-api/openapi.yaml` is unaffected by these defaults.
- `allowedOrigins` directly maps to the CORS configuration used by the Gin server — this is the one config field with real runtime/security effect, not just UI convenience.
- `httpPort` is the actual bind port for the Gin server — changing it requires the listener restart described in `03-architecture/architecture-overview.md` and `05-ux/tray-app-spec.md`.

## Migration / versioning

Not required for v1 — this new repo creates `config.json` on first launch. If the schema changes in a future version, add a `configVersion` field at that time — do not add one now for a version that doesn't exist yet.

## What is NOT stored in config

- Print job history/logs — these live in the rotating log file (`03-architecture/architecture-overview.md`), not in `config.json`.
- Window position/size or other pure UI state — not required for v1; the app can open at a sensible default size each launch.
