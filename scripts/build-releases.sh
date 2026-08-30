#!/usr/bin/env bash
set -euo pipefail

# Build a local release binary for the current platform.
# Native GitHub Actions packaging creates the Windows MSI and macOS DMG.

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DIST_DIR="$ROOT_DIR/dist"
APP_NAME="printer-bridge"

if [[ "$(uname -s)" == "Linux" ]]; then
  missing_linux_deps=()

  if ! command -v pkg-config >/dev/null 2>&1; then
    missing_linux_deps+=("pkg-config")
  else
    for pkg in gl wayland-client xkbcommon; do
      if ! pkg-config --exists "$pkg"; then
        missing_linux_deps+=("$pkg")
      fi
    done
  fi

  if ((${#missing_linux_deps[@]} > 0)); then
    cat <<'EOF'
Missing native Linux build dependencies for Fyne/GLFW.

On Ubuntu, Debian, or GitHub Codespaces, install them with:

  sudo apt-get update
  sudo apt-get install -y gcc pkg-config libgl1-mesa-dev xorg-dev libwayland-dev libxkbcommon-dev wayland-protocols

Then rerun:

  ./scripts/build-releases.sh

EOF
    exit 1
  fi
fi

mkdir -p "$DIST_DIR"
rm -f "$DIST_DIR"/*

echo "Building release binaries into: $DIST_DIR"

go build -trimpath -ldflags="-s -w" -o "$DIST_DIR/$APP_NAME" .

if command -v sha256sum >/dev/null 2>&1; then
  (
    cd "$DIST_DIR"
    sha256sum * > SHA256SUMS
  )
  echo "   checksum: SHA256SUMS"
elif command -v shasum >/dev/null 2>&1; then
  (
    cd "$DIST_DIR"
    shasum -a 256 * > SHA256SUMS
  )
  echo "   checksum: SHA256SUMS"
fi

echo "Done. Artifacts:"
ls -lh "$DIST_DIR"
