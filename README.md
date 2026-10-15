# print-bridge

`print-bridge` is a local desktop bridge that lets browser-based web apps send raw print jobs to network-connected thermal and label printers without using the operating system print dialog.

It runs as a small desktop tray app, exposes a localhost HTTP API, and forwards raw printer-language text such as ZPL, EPL, or CPCL to a network printer over TCP.

## Project Status

Current status: **beta**. The core app, local API, config, logging, and release
workflow are in place; public release validation should still include real
macOS and Windows installer testing.

This repository currently contains:

- A Fyne desktop app for macOS and Windows.
- A local Gin HTTP API bound to `127.0.0.1`.
- JSON config persistence in the user's app config directory.
- Rotating local request logs.
- Native release workflow definitions for macOS DMG and Windows MSI artifacts.
- Product, API, architecture, security, and packaging notes in [`DOCS/`](DOCS/README.md).

The `DOCS/` folder defines the product scope, API contract, architecture, security model, UX, configuration schema, packaging plan, and backlog stories.

## What It Provides

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
- Windows and macOS installers with free macOS ad-hoc signing and clear user-facing installation guidance for unsigned downloads.

## What It Does Not Do

- Not affiliated with, endorsed by, sponsored by, or connected to any printer manufacturer.
- Not an official replacement for any vendor product.
- Not a background service that starts automatically on login.
- Not Developer ID signed or notarized.
- Not an authentication system beyond CORS allow-listing.
- Not a PDF/image-to-ZPL converter.
- Not a multi-printer profile manager.
- Not a commercial product or paid support offering.

## Run Locally

Requirements:

- Go `1.24+`
- macOS or Windows for the desktop app

Linux and GitHub Codespaces can build the app, but Fyne/GLFW needs native GUI
headers:

```bash
sudo apt-get update
sudo apt-get install -y gcc pkg-config libgl1-mesa-dev xorg-dev libwayland-dev libxkbcommon-dev wayland-protocols
```

Run tests:

```bash
go test ./...
```

Common development checks:

```bash
make fmt    # format Go code with gofmt
make test   # run all tests
make vet    # run Go's standard static checks
make race   # run tests with the race detector
make lint   # run golangci-lint, if installed
make check  # run fmt, test, vet, and race
```

Run the app:

```bash
go run .
```

The app opens a desktop window, starts the local listener on `127.0.0.1:8080` by default, and keeps running from the tray/menu bar when the main window is hidden. The main window shows the app logo, listener status, default printer address/port, and allowed origins. **Help** gives users setup and troubleshooting guidance, while **Details** shows runtime diagnostics such as config/log paths, endpoints, timeout values, and other technical information.

Build a local binary:

```bash
./scripts/build-releases.sh
```

The binary is written to `dist/print-bridge`.

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

`print-bridge` passes the `text` payload through as-is. The calling web app is responsible for generating valid printer-language data such as ZPL, EPL, or CPCL.

## Security Model

`print-bridge` is designed as a localhost desktop bridge. Browser access is controlled through a configured CORS allow-list. This keeps integration simple for web developers while matching the trust model commonly used by local browser printing bridge tools.

Important limits:

- CORS is browser-enforced access control, not general-purpose authentication.
- Only trusted web app origins should be added to the allow-list.
- The bridge should bind to localhost only.
- Any future token/API-key authentication would be a deliberate scope change.

See [`DOCS/03-architecture/security-model.md`](DOCS/03-architecture/security-model.md) for the full model and accepted risks.

## Configuration

On first launch, `print-bridge` creates a local `config.json` file:

- macOS: `~/Library/Application Support/PrintBridge/config.json`
- Windows: `%APPDATA%\PrintBridge\config.json`

New installs start with an empty CORS allow-list:

```text
[]
```

Add only the web app origins this installation needs from the Settings screen. For example, a local development app might add `http://localhost:3000`, while a deployed app might add `https://app.example.com`. With an empty list, browser-based callers cannot use the API until an origin is added; non-browser local tools such as `curl` are not governed by CORS.

## Support And Feedback

`print-bridge` is built by [Jaime Bolaños](https://jbolanos.dev) as a practical tool for teams that need browser apps to reach local printers. If you need help setting it up, find something confusing, or have an idea that would make it better, I'll be glad to help.

If `print-bridge` saves you time, a GitHub star on the project or a follow is appreciated: [github.com/JamesBolanos](https://github.com/JamesBolanos).

## Contributing

Issues and pull requests are welcome. See [`CONTRIBUTING.md`](CONTRIBUTING.md)
for the development workflow and [`SECURITY.md`](SECURITY.md) for responsible
security reporting.

## Development Roadmap

Completed beta foundation:

- Core API: `/ping`, `/status`, `/print`
- Configurable printer port and localhost HTTP port
- Connect/write timeouts
- Config persistence
- Live log viewer for recent activity
- Fyne tray/menu bar UI
- App identity: `com.jbolanosdiaz.printbridge`
- Initial app icon
- Native CI/release workflow definitions

Remaining validation before public release should focus on real Windows MSI execution, real macOS DMG installation flow, and cross-platform tray behavior. Detailed epics and stories live in [`DOCS/08-backlog/`](DOCS/08-backlog/).

## Documentation Map

- [`DOCS/00-overview/project-brief.md`](DOCS/00-overview/project-brief.md) - goal, problem, scope, and success criteria.
- [`DOCS/01-business/`](DOCS/01-business/) - business requirements and constraints.
- [`DOCS/02-process/`](DOCS/02-process/) - print-job and user-workflow diagrams.
- [`DOCS/03-architecture/`](DOCS/03-architecture/) - architecture, decisions, and security model.
- [`DOCS/04-api/openapi.yaml`](DOCS/04-api/openapi.yaml) - API contract.
- [`DOCS/05-ux/tray-app-spec.md`](DOCS/05-ux/tray-app-spec.md) - tray app screens and behavior.
- [`DOCS/06-config/config-schema.md`](DOCS/06-config/config-schema.md) - config file shape and validation.
- [`DOCS/07-build-deploy/`](DOCS/07-build-deploy/) - build pipeline and installer guidance.
- [`DOCS/08-backlog/`](DOCS/08-backlog/) - implementation epics and stories.
- [`DOCS/09-legal/disclaimer.md`](DOCS/09-legal/disclaimer.md) - non-affiliation disclaimer.

## Non-Affiliation

`print-bridge` is an independent, open project created to provide a free bridge for browser-to-printer communication. It is not affiliated with, endorsed by, sponsored by, or in any way officially connected with any printer manufacturer. Product names and marks belong to their respective owners and are referenced only for descriptive compatibility context.

## Fork Attribution

`print-bridge` is fork-derived from earlier BSD 3-Clause licensed work by
LabelZoom and has since been substantially modified. See [`NOTICE.md`](NOTICE.md)
for attribution.

## License

This project is licensed under the BSD 3-Clause License. See [`LICENSE`](LICENSE).
