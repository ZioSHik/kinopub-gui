VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -s -w -X main.version=$(VERSION)
BUILD   := go build -ldflags "$(LDFLAGS)" -trimpath

# Windows GUI builds link with -H windowsgui so double-clicking the .exe does not
# pop a console window. An icon is embedded automatically when a generated
# cmd/kinopub-gui/resource_windows_amd64.syso is present (see `make winsyso`).
BUILD_WINGUI := go build -ldflags "$(LDFLAGS) -H windowsgui" -trimpath

.PHONY: all build gui web web-install run dev test vet clean release-gui icons app dmg winsyso

# Default: build the web UI and the GUI binary (which embeds it).
all: web gui

# ---- Frontend ------------------------------------------------------------

web-install:
	cd web && npm install

# Build the React frontend into web/dist (embedded by the Go binary via go:embed).
web:
	cd web && npm run build

# Frontend dev server with API proxy to a locally running `kinopub-gui`.
dev:
	cd web && npm run dev

# ---- Go binaries ---------------------------------------------------------

# The web GUI (embeds web/dist — run `make web` first for a fresh UI).
gui:
	$(BUILD) -o kinopub-gui ./cmd/kinopub-gui

build: gui

# Build the UI then run the GUI (opens a browser tab).
run: web gui
	./kinopub-gui

test:
	go test ./... -count=1

vet:
	go vet ./...

clean:
	rm -f kinopub kinopub-* kinopub-gui kinopub-gui-*
	rm -rf web/dist

# ---- Release -------------------------------------------------------------

# Cross-compile the GUI for every platform (frontend built once, embedded into each).
# Cross-compiled targets are CGO-off; darwin built on a Mac keeps CGO for the
# native menu-bar (systray) / Cocoa app shell.
release-gui: web
	GOOS=darwin  GOARCH=arm64                $(BUILD) -o kinopub-gui-darwin-arm64      ./cmd/kinopub-gui
	GOOS=darwin  GOARCH=amd64                $(BUILD) -o kinopub-gui-darwin-amd64      ./cmd/kinopub-gui
	CGO_ENABLED=0 GOOS=linux   GOARCH=amd64  $(BUILD) -o kinopub-gui-linux-amd64       ./cmd/kinopub-gui
	CGO_ENABLED=0 GOOS=linux   GOARCH=arm64  $(BUILD) -o kinopub-gui-linux-arm64       ./cmd/kinopub-gui
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64  $(BUILD_WINGUI) -o kinopub-gui-windows-amd64.exe ./cmd/kinopub-gui
	@echo "Built GUI release binaries:"
	@ls -la kinopub-gui-*

# ---- Packaging -----------------------------------------------------------

# Regenerate app icons from web/public/favicon.svg into build/icons/ (committed;
# needs librsvg + macOS iconutil). Re-run only when the source SVG changes.
icons:
	./scripts/gen-icons.sh

# Build KinoPub.app and a drag-to-install .dmg for the host arch (macOS only).
# `app` is an alias; the script produces both. Set ARCH=x86_64 for Intel.
app dmg: web
	./scripts/package-macos.sh

# Embed the Windows icon by generating a .syso resource (needs goversioninfo and
# build/icons/icon.ico). Run before `make release-gui` for an icon'd .exe.
winsyso:
	go run github.com/josephspurrier/goversioninfo/cmd/goversioninfo@latest \
		-skip-versioninfo -icon build/icons/icon.ico -64 \
		-o cmd/kinopub-gui/resource_windows_amd64.syso
