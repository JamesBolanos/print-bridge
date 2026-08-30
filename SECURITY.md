# Security Policy

## Supported Versions

Public releases of `printer-bridge` are supported on a best-effort basis.
Pre-release builds may change quickly while the project is still stabilizing.

## Reporting a Vulnerability

Please do not open a public issue for a suspected security vulnerability.

Report security concerns privately by contacting Jaime Bolaños through the
contact options listed at <https://jbolanos.dev>.

Helpful reports include:

- A clear description of the issue
- Steps to reproduce or proof of concept
- Operating system and version
- printer-bridge version or commit
- Any relevant configuration details, with secrets removed

## Security Model

`printer-bridge` runs a local HTTP API bound to `127.0.0.1` and forwards raw
printer-language text to network printers over TCP.

Browser access is controlled through a CORS allow-list. CORS is browser-enforced
access control, not full authentication. Only add origins you trust.

Do not expose the local listener to a public network. The intended deployment is
a trusted desktop machine communicating with trusted browser apps and known
network printers.
