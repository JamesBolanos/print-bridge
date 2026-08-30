# Print Agent Go

# This project was originally forked from the LabelZoom Print Agent and has since evolved for a standalone workflow.


[![Go Version](https://img.shields.io/badge/Go-1.24-00ADD8?logo=go)](https://go.dev/)
[![License](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE)

A lightweight local print agent that enables browser-based printing to direct thermal printers. This agent acts as a bridge between embedded authenticated web pages and RAW printers on your local network, allowing fast and accurate reproduction of barcode labels in the printer's native language (such as Zebra ZPL).

## 🎯 What It Does

The Print Agent:
- **Receives print jobs** from embedded authenticated web pages via HTTP
- **Forwards print data** to thermal printers via TCP (port 9100)
- **Supports RAW printing** for direct thermal label printers
- **Runs locally** to communicate with printers on your network

## 🚀 Quick Start

### Build From Source

If you prefer to build and run the agent locally:

```bash
# Clone the repository
git clone https://github.com/DemosGS1NI/print-agent-go.git
cd print-agent-go

# Build the binary
go build -o lz-print-agent-local .

# Run the agent
./lz-print-agent-local
```

The agent will start on port 8080 by default.

## 📋 Requirements

- **Go 1.24+** (for building from source)
- **Network access** to your thermal printers (typically port 9100)
- **Web browser** with access to your embedded auth web pages

## 🔧 Configuration

The print agent uses the following default settings:

| Setting | Default | Description |
|---------|---------|-------------|
| HTTP Port | `8080` | Port the agent listens on for print jobs |
| Printer Port | `9100` | Standard RAW printing port for thermal printers |
| CORS Origins | Multiple | Allowed origins for web requests |

### Allowed CORS Origins

The agent accepts requests only from allowed origins. You can configure them with the `ALLOW_ORIGINS` environment variable (comma-separated list).

Custom example:

```bash
export ALLOW_ORIGINS="https://www.browser-print.vercel.app,http://localhost:3000"
./lz-print-agent-local
```

If `ALLOW_ORIGINS` is not set (or contains only empty values), the built-in defaults are used.
 
- `https://www.browser-print.vercel.app`
- `https://browser-print.vercel.app`
- `http://localhost`
- `http://localhost:3000`
- `http://localhost:5173`

## 🖨️ Supported Printers

The agent works with any thermal printer that supports RAW printing over TCP/IP, including:
- **Zebra** printers (ZPL language)
- **Datamax** printers
- **SATO** printers
- Other direct thermal/thermal transfer printers with network connectivity

## 🧪 Testing

### Run Tests

```bash
go test -v ./...
```

### Test the Agent

Once running, test the `/ping` endpoint:

```bash
curl http://localhost:52045/ping
```

Expected response:
```json
{"message":"pong"}
```

### Send a Test Print Job

```bash
curl -X POST http://localhost:52045/print \
  -H "Content-Type: application/json" \
  -d '{
    "printerHostname": "192.168.1.100",
    "text": "^XA^FO50,50^ADN,36,20^FDTest Label^FS^XZ"
  }'
```

Replace `192.168.1.100` with your printer's IP address.

## 🏗️ Development

### Project Structure

```
lz-print-agent-go/
├── main.go              # Main application code
├── main_test.go         # Unit tests
├── resources/           # Embedded resources (logo)
├── scripts/             # Build/release helper scripts
├── go.mod               # Go module definition
└── .github/
    └── workflows/       # CI/CD pipeline
```

### CI/CD

This project uses GitHub Actions for:
- ✅ Automated testing on push/PR
- ✅ Multi-platform builds (amd64, arm64)
- ✅ Automatic binary releases on semver tags (`v*.*.*`)

## 📥 Binary Releases (No GitHub Account Required)

Windows and macOS binaries are published on each semantic version tag (`v*.*.*`).

- Latest release page: `https://github.com/DemosGS1NI/print-agent-go/releases/latest`
- Direct Windows ZIP (latest): `https://github.com/DemosGS1NI/print-agent-go/releases/latest/download/print-agent-windows-amd64.zip`

If this repository is public, end users can download these files without creating a GitHub account.

## 🛣️ Roadmap

- Improve embedded auth web pages (UX, token/session handling, and configuration flow)
- Expand configurable CORS origin management
- Improve onboarding and troubleshooting docs for end users

### Windows Code Signing (Optional but Recommended)

The release workflow supports automatic signing of the Windows `.exe` when these repository secrets are configured:

- `WINDOWS_SIGN_CERT_BASE64`: Base64-encoded `.pfx` certificate
- `WINDOWS_SIGN_CERT_PASSWORD`: Password for the `.pfx` certificate

If the secrets are not set, releases are still generated, but the Windows binary is unsigned.

## 🤝 Contributing

Contributions are welcome! Please:

1. Fork the repository
2. Create a feature branch (`git checkout -b feature/amazing-feature`)
3. Commit your changes (`git commit -m 'Add amazing feature'`)
4. Push to the branch (`git push origin feature/amazing-feature`)
5. Open a Pull Request

## 📄 License

This project is licensed under the BSD 3-Clause License - see the [LICENSE](LICENSE) file for details.

## 🔗 Links

- **Releases**: [github.com/DemosGS1NI/print-agent-go/releases](https://github.com/DemosGS1NI/print-agent-go/releases)
- **Issues**: [GitHub Issues](https://github.com/DemosGS1NI/print-agent-go/issues)

## 💡 How It Works

```
┌─────────────┐      HTTP POST       ┌──────────────────┐      TCP (9100)      ┌─────────┐
│   Browser   │ ───────────────────> │  Print Agent     │ ───────────────────> │ Printer │
│ (Embedded)  │   (localhost:52045)  │  (Go Service)    │   (RAW ZPL data)     │ (Zebra) │
└─────────────┘                      └──────────────────┘                      └─────────┘
```

1. User creates a label from an embedded authenticated web page
2. Browser sends print job to local agent via HTTP
3. Agent forwards ZPL/RAW data to printer via TCP
4. Printer prints the label


