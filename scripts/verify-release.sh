#!/usr/bin/env bash
# Check all supported platforms before preparing the bundle for publication.
# Keep checksums.txt compatible with the in-app updater (six kinopub-* binaries).
set -euo pipefail

: "${VERSION:?Set VERSION to the release or validation-build version}"
cd "${1:?Usage: verify-release.sh <artifact-directory>}"

files=(
  kinopub-gui-darwin-arm64
  kinopub-gui-darwin-amd64
  kinopub-gui-linux-amd64
  kinopub-gui-linux-arm64
  kinopub-gui-windows-amd64.exe
  kinopub-gui-android-arm64
  "KinoPub-${VERSION}-arm64.dmg"
  "KinoPub-${VERSION}-x86_64.dmg"
  "KinoPub-${VERSION}-x86_64.AppImage"
)

for file in "${files[@]}"; do
  if [[ ! -s "$file" ]]; then
    echo "Missing or empty release artifact: $file" >&2
    exit 1
  fi
done

sha256sum kinopub-* > checksums.txt
sha256sum --check --strict checksums.txt
ls -lh
