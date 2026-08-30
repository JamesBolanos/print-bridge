# printer-bridge

`printer-bridge` is a free, local desktop bridge that lets browser-based web apps send raw print jobs to network-connected thermal and label printers without using the operating system print dialog.

This repository is the v1 evolution of the original Go beta: the working HTTP-to-TCP print path is now wrapped in a desktop tray app with configuration, status checks, logging, and installer packaging metadata.

## Project Status

This repository currently contains:

- A Fyne desktop app for macOS and Windows.
- A local Gin HTTP API bound to `127.0.0.1`.
- JSON config persistence in the user's app config directory.
- Rotating local request logs.
- Native release workflow definitions for macOS DMG and Windows MSI artifacts.
- A full v1 planning and implementation playbook in [`DOCS/`](DOCS/README.md).

The `DOCS/` folder defines the product scope, API contract, architecture, security model, UX, configuration schema, packaging plan, and backlog stories.

## What v1 Provides

- A manually launched desktop app for macOS and Windows.
- System tray/menu bar behavior so the bridge can keep running while the main window is hidden.
- Local HTTP API for browser-based integrations.
- Raw passthrough printing to network printers, commonly on TCP port `9100`.
- Per-request printer host and port support.
- CORS allow-list based browser access control.
- Configurable local HTTP port and allowed origins.
- Optional default printer address and printer port for convenience.
- Printer reachability checks.
- Explicit TCP connect/write timeouts so failures return quickly.
- Local rotating logs, viewable from the app.
- Unsigned Windows and macOS installers with clear user-facing installation guidance.

## What v1 Is Not

- Not affiliated with, endorsed by, sponsored by, or connected to Zebra Technologies or any printer manufacturer.
- Not an official replacement for any vendor product.
- Not a background service that starts automatically on login.
- Not code-signed or notarized in v1.
- Not an authentication system beyond CORS allow-listing.
- Not a PDF/image-to-ZPL converter.
- Not a multi-printer profile manager.
- Not a commercial product or paid support offering.

## Run Locally

Requirements:

- Go `1.24+`
- macOS or Windows for the desktop app

Run tests:

```bash
go test ./...
```

Run the app:

```bash
go run .
```

The app opens a desktop window, starts the local listener on `127.0.0.1:8080` by default, and keeps running from the tray/menu bar when the main window is hidden.

Build a local binary:

```bash
./scripts/build-releases.sh
```

The binary is written to `dist/printer-bridge`.

## Local API

The API is documented in [`DOCS/04-api/openapi.yaml`](DOCS/04-api/openapi.yaml).

Endpoints:

- `GET /ping` - check that the local bridge is running.
- `GET /status?host=<printer_host>&port=<printer_port>` - check whether a printer is reachable.
- `POST /print` - send raw printer-language text to a printer.

Example print request:

```bash
curl -X POST http://localhost:8080/print \
  -H "Content-Type: application/json" \
  -d '{
    "printerHostname": "192.168.1.100",
    "printerPort": 9100,
    "text": "^XA^FO50,50^ADN,36,20^FDTest Label^FS^XZ"
  }'
```

`printer-bridge` passes the `text` payload through as-is. The calling web app is responsible for generating valid printer-language data such as ZPL, EPL, or CPCL.

## Security Model

v1 is designed as a localhost desktop bridge. Browser access is controlled through a configured CORS allow-list. This keeps integration simple for web developers while matching the trust model commonly used by local browser-to-printer bridge tools.

Important limits:

- CORS is browser-enforced access control, not general-purpose authentication.
- Only trusted web app origins should be added to the allow-list.
- The bridge should bind to localhost only.
- Any future token/API-key authentication would be a deliberate scope change, not a v1 requirement.

See [`DOCS/03-architecture/security-model.md`](DOCS/03-architecture/security-model.md) for the full model and accepted risks.

## Configuration

On first launch, `printer-bridge` creates a local `config.json` file:

- macOS: `~/Library/Application Support/PrinterBridge/config.json`
- Windows: `%APPDATA%\PrinterBridge\config.json`

Default allowed origins are localhost-only:

```text
http://localhost
http://localhost:3000
http://localhost:5173
```

Add production web app origins from the Settings screen.

## Development Roadmap

Completed v1 foundation:

- Core API: `/ping`, `/status`, `/print`
- Configurable printer port and localhost HTTP port
- Connect/write timeouts
- Config persistence
- Rotating logs and recent activity
- Fyne tray/menu bar UI
- App identity: `com.jbolanosdiaz.printerbridge`
- Initial app icon
- Native CI/release workflow definitions

Remaining validation before public release should focus on real Windows MSI execution, real macOS DMG installation flow, and cross-platform tray behavior. Detailed epics and stories live in [`DOCS/08-backlog/`](DOCS/08-backlog/).

## Documentation Map

- [`DOCS/00-overview/project-brief.md`](DOCS/00-overview/project-brief.md) - goal, problem, scope, and success criteria.
- [`DOCS/01-business/`](DOCS/01-business/) - business requirements and constraints.
- [`DOCS/02-process/`](DOCS/02-process/) - print-job and user-workflow diagrams.
- [`DOCS/03-architecture/`](DOCS/03-architecture/) - architecture, decisions, and security model.
- [`DOCS/04-api/openapi.yaml`](DOCS/04-api/openapi.yaml) - target v1 API contract.
- [`DOCS/05-ux/tray-app-spec.md`](DOCS/05-ux/tray-app-spec.md) - tray app screens and behavior.
- [`DOCS/06-config/config-schema.md`](DOCS/06-config/config-schema.md) - config file shape and validation.
- [`DOCS/07-build-deploy/`](DOCS/07-build-deploy/) - build pipeline and installer guidance.
- [`DOCS/08-backlog/`](DOCS/08-backlog/) - implementation epics and stories.
- [`DOCS/09-legal/disclaimer.md`](DOCS/09-legal/disclaimer.md) - non-affiliation disclaimer.

## Non-Affiliation

`printer-bridge` is an independent, open project created to provide a free bridge for browser-to-printer communication. It is not affiliated with, endorsed by, sponsored by, or in any way officially connected with Zebra Technologies Corporation, Datamax, SATO, or any other printer manufacturer. Product names and marks belong to their respective owners and are referenced only for descriptive compatibility context.

## License

This project is licensed under the BSD 3-Clause License. See [`LICENSE`](LICENSE).
