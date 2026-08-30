# Contributing

Thanks for taking a look at `print-bridge`.

This is a small Go desktop app, so contributions are easiest to review when
they stay focused and include tests for behavior changes.

## Development

Requirements:

- Go `1.24+`
- `golangci-lint` for lint checks
- macOS or Windows to run the desktop UI locally

On Linux or GitHub Codespaces, install Fyne/GLFW native build dependencies:

```bash
sudo apt-get update
sudo apt-get install -y gcc pkg-config libgl1-mesa-dev xorg-dev libwayland-dev libxkbcommon-dev wayland-protocols
```

Useful commands:

```bash
make fmt
make test
make vet
make race
make lint
make check
```

Run `make fmt`, `make test`, and `make lint` before opening a pull request.
Use `make race` when changing server lifecycle, logging, goroutines, or shared
state.

## Pull Requests

- Keep PRs small and describe the user-visible behavior being changed.
- Add or update tests when changing API, config, logging, or server behavior.
- Keep generated release artifacts out of commits.
- Preserve the BSD 3-Clause license and fork attribution.

## Issues

Bug reports are most useful when they include:

- Operating system and version
- print-bridge version or commit
- Printer model and connection details, if relevant
- Steps to reproduce
- Any relevant log lines from the app's View Logs window
