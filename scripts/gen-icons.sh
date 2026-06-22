#!/usr/bin/env bash
# Regenerate the app icons from web/public/favicon.svg into build/icons/:
#   AppIcon.icns   — macOS bundle icon
#   icon.ico       — Windows executable icon
#   icon-256.png   — Linux .desktop / AppImage icon
#   icon-512.png
#
# These are committed so CI doesn't need any image tooling. Re-run this script
# (needs librsvg: `brew install librsvg`, plus macOS iconutil) whenever the
# source SVG changes.
set -euo pipefail
cd "$(dirname "$0")/.."
ROOT="$(pwd)"
SVG="$ROOT/web/public/favicon.svg"
OUT="$ROOT/build/icons"
mkdir -p "$OUT"

command -v rsvg-convert >/dev/null || { echo "need rsvg-convert (brew install librsvg)" >&2; exit 1; }
render() { rsvg-convert -w "$1" -h "$1" "$SVG" -o "$2"; }

# --- Linux PNGs ---
render 256 "$OUT/icon-256.png"
render 512 "$OUT/icon-512.png"

# --- macOS .icns (iconutil is macOS-only) ---
if command -v iconutil >/dev/null; then
  ISET="$(mktemp -d)/AppIcon.iconset"; mkdir -p "$ISET"
  render 16   "$ISET/icon_16x16.png";      render 32   "$ISET/icon_16x16@2x.png"
  render 32   "$ISET/icon_32x32.png";      render 64   "$ISET/icon_32x32@2x.png"
  render 128  "$ISET/icon_128x128.png";    render 256  "$ISET/icon_128x128@2x.png"
  render 256  "$ISET/icon_256x256.png";    render 512  "$ISET/icon_256x256@2x.png"
  render 512  "$ISET/icon_512x512.png";    render 1024 "$ISET/icon_512x512@2x.png"
  iconutil -c icns "$ISET" -o "$OUT/AppIcon.icns"
else
  echo "note: iconutil not found (not macOS) — skipping AppIcon.icns" >&2
fi

# --- Windows .ico (PNG-in-ICO via the bundled Go packer) ---
TMP="$(mktemp -d)"
for s in 16 32 48 64 128 256; do render "$s" "$TMP/$s.png"; done
go run "$ROOT/scripts/icogen" -o "$OUT/icon.ico" \
  "$TMP/16.png" "$TMP/32.png" "$TMP/48.png" "$TMP/64.png" "$TMP/128.png" "$TMP/256.png"
rm -rf "$TMP"

# --- tray icons embedded into the GUI binary (go:embed needs them in-package) ---
cp "$OUT/icon-256.png" "$ROOT/cmd/kinopub-gui/trayicon.png"
cp "$OUT/icon.ico"     "$ROOT/cmd/kinopub-gui/trayicon.ico"

echo "Generated:"; ls -la "$OUT"; echo "Embedded: cmd/kinopub-gui/trayicon.{png,ico}"
