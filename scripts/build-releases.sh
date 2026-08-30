#!/usr/bin/env bash
set -euo pipefail

# Build a local release binary for the current platform.
# Native GitHub Actions packaging creates the Windows MSI and macOS DMG.

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DIST_DIR="$ROOT_DIR/dist"
APP_NAME="printer-bridge"

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
