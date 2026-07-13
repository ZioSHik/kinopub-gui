#!/usr/bin/env bash
# Build KinoPub.app and wrap it in a drag-to-install .dmg.
#
# Produces, for the host architecture (or $ARCH if set):
#   dist-app/KinoPub.app                      — the app bundle (ad-hoc signed)
#   dist-app/KinoPub-<version>-<arch>.dmg     — the disk image
#
# Requirements (all preinstalled on macOS, plus librsvg for the icon):
#   go, rsvg-convert (brew install librsvg), sips, iconutil, hdiutil, codesign
#
# This is the local/Mac counterpart to the cross-compiled CI artifacts; it runs
# only on macOS because .app signing and .dmg creation need Apple tooling.
set -euo pipefail

cd "$(dirname "$0")/.."
ROOT="$(pwd)"

APP_NAME="KinoPub"
BUNDLE_ID="com.zioshik.kinopub-gui"
EXE="kinopub-gui"
ARCH="${ARCH:-$(uname -m)}"          # arm64 | x86_64
case "$ARCH" in
  arm64)  GOARCH=arm64 ;;
  x86_64) GOARCH=amd64 ;;
  *) echo "unsupported arch: $ARCH" >&2; exit 1 ;;
esac

VERSION="${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo dev)}"
SHORT_VERSION="${VERSION#v}"; SHORT_VERSION="${SHORT_VERSION%%-*}"   # 0.1.5
[ -n "$SHORT_VERSION" ] || SHORT_VERSION="0.0.0"

OUT="$ROOT/dist-app"
APP="$OUT/$APP_NAME.app"
DMG="$OUT/$APP_NAME-$VERSION-$ARCH.dmg"

echo "==> building $APP_NAME.app ($ARCH, $VERSION)"
# Only clear this arch's bundle, so building both arches in sequence (CI) keeps
# the other arch's .dmg.
rm -rf "$APP"
mkdir -p "$OUT" "$APP/Contents/MacOS" "$APP/Contents/Resources"

# --- icon: prefer the committed build/icons/AppIcon.icns; regenerate if absent
ICNS="$ROOT/build/icons/AppIcon.icns"
if [ ! -f "$ICNS" ]; then
  echo "==> AppIcon.icns missing — regenerating (scripts/gen-icons.sh)"
  "$ROOT/scripts/gen-icons.sh"
fi
cp "$ICNS" "$APP/Contents/Resources/AppIcon.icns"

# --- binary (native build keeps CGO for the menu-bar / Cocoa app shell) ----
echo "==> go build ./cmd/$EXE ($GOARCH)"
# CGO on so the Cocoa app shell (Dock icon + ⌘Q, applife_darwin.go) is compiled
# in; GOARCH set so this also cross-builds the Intel slice on an arm64 runner.
#
# Without an explicit deployment target clang stamps the build host's macOS
# version into LC_BUILD_VERSION minos, and Launch Services then refuses to run
# the app on anything older ("this version of macOS is not supported"). 12.0 is
# the floor: Go 1.25+ itself requires macOS 12 Monterey. The -mmacosx-version-min
# flags go through CGO_CFLAGS/CGO_LDFLAGS because those are part of Go's build
# cache key — MACOSX_DEPLOYMENT_TARGET alone leaves stale cached objects behind.
MACOS_MIN="12.0"
export MACOSX_DEPLOYMENT_TARGET="$MACOS_MIN"
export CGO_CFLAGS="-mmacosx-version-min=$MACOS_MIN"
export CGO_LDFLAGS="-mmacosx-version-min=$MACOS_MIN"
GOOS=darwin GOARCH="$GOARCH" CGO_ENABLED=1 go build -trimpath \
  -ldflags "-s -w -X main.version=$VERSION" \
  -o "$APP/Contents/MacOS/$EXE" "./cmd/$EXE"

# --- Info.plist ------------------------------------------------------------
cat > "$APP/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleName</key>            <string>$APP_NAME</string>
  <key>CFBundleDisplayName</key>     <string>KinoPub Downloader</string>
  <key>CFBundleExecutable</key>      <string>$EXE</string>
  <key>CFBundleIdentifier</key>      <string>$BUNDLE_ID</string>
  <key>CFBundleIconFile</key>        <string>AppIcon</string>
  <key>CFBundlePackageType</key>     <string>APPL</string>
  <key>CFBundleVersion</key>         <string>$SHORT_VERSION</string>
  <key>CFBundleShortVersionString</key> <string>$SHORT_VERSION</string>
  <key>LSMinimumSystemVersion</key>  <string>$MACOS_MIN</string>
  <key>NSHighResolutionCapable</key> <true/>
  <!-- Menu-bar app: no Dock icon. The status-bar item (systray) provides Open
       and Quit, so there's nothing to lose by staying out of the Dock. -->
  <key>LSUIElement</key>             <true/>
</dict>
</plist>
PLIST
printf 'APPL????' > "$APP/Contents/PkgInfo"

# --- ad-hoc sign (free; stops Gatekeeper killing unsigned arm64 as "damaged")
echo "==> codesign (ad-hoc)"
codesign --force --deep --sign - "$APP"
codesign --verify --deep --strict "$APP" && echo "    signature OK"

# --- .dmg with a drag-to-Applications layout -------------------------------
echo "==> hdiutil create $DMG"
STAGE="$(mktemp -d)"
cp -R "$APP" "$STAGE/"
ln -s /Applications "$STAGE/Applications"
hdiutil create -volname "$APP_NAME" -srcfolder "$STAGE" \
  -ov -format UDZO "$DMG" >/dev/null
rm -rf "$STAGE"

echo
echo "Built:"
echo "  $APP"
echo "  $DMG"
du -h "$DMG" | awk '{print "  size: "$1}'
