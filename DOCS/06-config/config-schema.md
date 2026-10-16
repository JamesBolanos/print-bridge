# Config Schema — print-bridge

Read `05-ux/tray-app-spec.md` alongside this document for settings UI behavior.

Version 1 is implemented in VM-3 and virtual API routing in VM-4.
Destination/preview controls are implemented in VM-8; optional external
rendering is implemented in VM-7.
A saved Virtual destination captures jobs locally without physical fallback.

## Storage location

| Platform | Path |
|---|---|
| macOS | `~/Library/Application Support/PrintBridge/config.json` |
| Windows | `%APPDATA%\\PrintBridge\\config.json` |

Settings are stored as a local JSON file and saved atomically. No database or
external configuration service is required.

## Schema

New installations create:

```json
{
  "configVersion": 1,
  "destinationType": "physical",
  "preview": {
    "labelaryEnabled": false,
    "labelWidthInches": 4,
    "labelHeightInches": 6,
    "printDensityDpmm": 8
  },
  "httpPort": 8080,
  "defaultPrinterPort": 9100,
  "defaultPrinterAddress": "",
  "allowedOrigins": ["https://sscc-labels.com", "https://www.sscc-labels.com"]
}
```

| Field | Type | Default | Validation / behavior |
|---|---|---|---|
| `configVersion` | integer | `1` | Current schema; valid unversioned/version-0 files migrate in memory |
| `destinationType` | string | `physical` for new/legacy files | Version 1 requires exactly `physical` or `virtual` |
| `preview` | object | As above | Optional; missing preferences get defaults, explicit invalid/null values are rejected |
| `httpPort` | integer | `8080` | 1024–65535; port must be available at startup |
| `defaultPrinterPort` | integer | `9100` | 1–65535; supplies omitted/zero request ports |
| `defaultPrinterAddress` | string | Empty | Optional display/settings convenience; does not replace the required Physical request hostname |
| `allowedOrigins` | string array | Both HTTPS SSCC label-maker origins above | HTTP(S) origins with scheme, host, optional port; no query, fragment, credentials, or page path |

These origins are defaults for new installations starting with v1.0.0-beta.4.
Existing configuration files retain their saved origins, including an empty list.
Users can add or remove origins in Settings.

## Destination and preview settings

- `destinationType` is global. It routes both
  `/print` and `/status`; a request cannot override it. Switching destination
  preserves the configured physical address and port.
- `labelaryEnabled` is a boolean, false by default. The Settings control
  discloses that rendering sends label contents to an external service. VM-7
  prevents new submissions and cancels pending work when disabled.
- Width and height are finite numbers greater than zero and at most 15 inches.
  Density must be 6, 8, 12, or 24 dots/mm. Defaults are 4-by-6 inches at 8 dpmm,
  not values detected from a physical printer. VM-7 uses Labelary's fixed HTTPS
  endpoint and snapshots preferences per render attempt.
- Preview preferences never alter source bytes or physical output. Changing
  dimensions or density cancels in-flight attempts and invalidates cached
  images while retaining captured payloads. Disabling rendering cancels pending
  work but retains successful previews.
- The desktop Settings form exposes destination and preview preferences.
  Switching modes preserves the physical printer address and port; changing
  HTTP port restarts the listener and updates the Inspector launcher URL.

## Browser origins and API defaults

New installations start with an empty browser allow-list. Add only the required
web app origins, such as `http://localhost:3000` or `https://app.example.com`.
The current Gin CORS configuration uses this list; non-browser local clients
remain within the existing machine trust boundary.

`defaultPrinterPort` supplies omitted/zero `/print` ports and omitted `/status`
ports. Explicit valid request ports remain supported.
`defaultPrinterAddress` does not replace the required Physical `/print`
hostname. Virtual `/print` requests may omit a hostname. Changing `httpPort`
requires a listener restart.
See [the current API contract](../04-api/openapi.yaml) and
[virtual behavior](../04-api/virtual-mode.md).

## Migration and versioning

A valid file without `configVersion`, or with version 0, is legacy only if it
contains the original fields and no destination/preview fields. Existing ports,
printer address, and origins are preserved, subject to the existing whitespace
normalization. Physical and rendering-off defaults are added in memory.

Loading a legacy file does not rewrite it. Its next explicit Settings save
writes version 1 using the existing atomic replacement operation. New installs
create version 1 directly. Runtime validation requires the migrated version-1
representation; it never guesses a missing destination.

Versioned files must specify a destination. Missing preview objects/preferences
use safe defaults. Explicit zero, null, wrong types, or unsupported values are
rejected. Keys are exact and case-sensitive; unknown or duplicate fields,
unsupported versions, multiple JSON documents, and new fields in an unversioned
file are rejected rather than silently ignored or merged.

## Invalid-file recovery

Malformed or invalid existing files are left untouched. The loader returns a
repair warning and display-only defaults with an empty, invalid destination.
Normalization, Save, and listener startup reject that recovery configuration.
This replaces the earlier behavior of overwriting invalid files with defaults.

The desktop remains open, shows the affected path and repair instructions, and
disables Settings Save during recovery. Repair the file and restart the app.
There is no implicit reset to Physical or destructive repair button. File read
errors are returned as errors, not mistaken for a missing first-run config.

A valid Virtual configuration starts the loopback listener and routes `/print`
to session capture. `/status` reports capture readiness without probing a
printer. The Settings destination selector changes the mode and restarts the
listener while preserving the configured physical printer address and port.

## Data outside configuration

Request metadata lives in the rotating log. Captured payloads, preview images,
and pending render work stay in bounded session memory, not in config or logs.
Initial limits are documented in
[the capture-store design](../03-architecture/virtual-capture-store.md);
request, storage, and renderer bounds are implementation defaults, not
additional user settings in this sprint.

Window position/size is not persisted. See the
[sprint checklist](../08-backlog/virtual-mode-sprint.md) for implementation status.
