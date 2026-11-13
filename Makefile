VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -s -w -X main.version=$(VERSION)
BUILD   := go build -ldflags "$(LDFLAGS)" -trimpath

# Port the backend listens on during `make dev`. Must match the Vite proxy
# target in web/vite.config.ts.
DEV_PORT ?= 8765

# Windows GUI builds link with -H windowsgui so double-clicking the .exe does not
# pop a console window. An icon is embedded automatically when a generated
# cmd/kinopub-gui/resource_windows_amd64.syso is present (see `make winsyso`).
BUILD_WINGUI := go build -ldflags "$(LDFLAGS) -H windowsgui" -trimpath

.PHONY: help all build gui web web-install run dev test test-integration vet clean release-gui icons app dmg winsyso

.DEFAULT_GOAL := all

# Self-documenting help: a target is listed when its rule line carries a `## text`
# comment, under the nearest `##@ Section` header above it.
help: ## List the available targets
	@awk 'BEGIN { FS = ":.*##"; print "Usage: make <target>" } \
		/^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5); next } \
		/^[a-zA-Z0-9_.\- ]+:.*##/ { printf "  \033[36m%-16s\033[0m %s\n", $$1, $$2 }' $(MAKEFILE_LIST)

##@ General

# Default: build the web UI and the GUI binary (which embeds it).
all: web gui ## Build the web UI and the GUI binary (default)

##@ Frontend

web-install: ## Install the frontend npm dependencies
	cd web && npm install

# Build the React frontend into web/dist (embedded by the Go binary via go:embed).
web: ## Build the React frontend into web/dist
	cd web && npm run build

# Frontend dev server. The Vite proxy sends /api to the Go backend, so this also
# builds and starts one — the frontend reloads from source, but the backend is a
# compiled binary, and a stale process left running from an earlier session will
# happily serve yesterday's API against today's UI.
#
# A backend already listening on the port is left alone: it may be one you
# started yourself. It is NOT rebuilt, so the warning below says so.
dev: gui ## Run the frontend dev server (starts the backend too, unless one is already up)
	@set -e; \
	if lsof -nP -iTCP:$(DEV_PORT) -sTCP:LISTEN >/dev/null 2>&1; then \
		echo "==> :$(DEV_PORT) is already taken by PID $$(lsof -t -nP -iTCP:$(DEV_PORT) -sTCP:LISTEN | head -1) — leaving it alone."; \
		echo "    NOTE: that process was NOT rebuilt. Restart it to pick up Go changes."; \
	else \
		echo "==> starting backend on 127.0.0.1:$(DEV_PORT)"; \
		./kinopub-gui -addr 127.0.0.1:$(DEV_PORT) -no-open & \
		backend=$$!; \
		trap "kill $$backend 2>/dev/null || true" EXIT INT TERM; \
	fi; \
	cd web && npm run dev

##@ Go binaries

# The web GUI (embeds web/dist — run `make web` first for a fresh UI).
gui: ## Build the kinopub-gui binary (embeds web/dist)
	$(BUILD) -o kinopub-gui ./cmd/kinopub-gui

build: gui ## Alias for `gui`

# Build the UI then run the GUI (opens a browser tab).
run: web gui ## Build the UI and run the GUI (opens a browser tab)
	./kinopub-gui

##@ Checks

test: ## Run the unit tests
	go test ./... -count=1

# Network integration tests (real uTLS handshakes against live hosts). Excluded
# from `make test` so the default suite stays deterministic and passes offline.
# Requires network; individual tests self-skip when no host is reachable.
test-integration: ## Run the network integration tests (needs network)
	go test -tags netintegration ./... -count=1

vet: ## Run go vet
	go vet ./...

clean: ## Remove the built binaries and web/dist
	rm -f kinopub kinopub-* kinopub-gui kinopub-gui-*
	rm -rf web/dist

##@ Release

# Cross-compile the GUI for every platform (frontend built once, embedded into each).
# Cross-compiled targets are CGO-off; darwin built on a Mac keeps CGO for the
# native menu-bar (systray) / Cocoa app shell.
release-gui: web ## Cross-compile the GUI for macOS, Linux and Windows
	GOOS=darwin  GOARCH=arm64                $(BUILD) -o kinopub-gui-darwin-arm64      ./cmd/kinopub-gui
	GOOS=darwin  GOARCH=amd64                $(BUILD) -o kinopub-gui-darwin-amd64      ./cmd/kinopub-gui
	CGO_ENABLED=0 GOOS=linux   GOARCH=amd64  $(BUILD) -o kinopub-gui-linux-amd64       ./cmd/kinopub-gui
	CGO_ENABLED=0 GOOS=linux   GOARCH=arm64  $(BUILD) -o kinopub-gui-linux-arm64       ./cmd/kinopub-gui
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64  $(BUILD_WINGUI) -o kinopub-gui-windows-amd64.exe ./cmd/kinopub-gui
	@echo "Built GUI release binaries:"
	@ls -la kinopub-gui-*

##@ Packaging

# Regenerate app icons from web/public/favicon.svg into build/icons/ (committed;
# needs librsvg + macOS iconutil). Re-run only when the source SVG changes.
icons: ## Regenerate the app icons from web/public/favicon.svg
	./scripts/gen-icons.sh

# Build KinoPub.app and a drag-to-install .dmg for the host arch (macOS only).
# `app` is an alias; the script produces both. Set ARCH=x86_64 for Intel.
app dmg: web ## Build KinoPub.app and a .dmg (macOS; ARCH=x86_64 for Intel)
	./scripts/package-macos.sh

# Embed the Windows icon by generating a .syso resource (needs goversioninfo and
# build/icons/icon.ico). Run before `make release-gui` for an icon'd .exe.
winsyso: ## Generate the Windows icon resource (.syso) for release-gui
	go run github.com/josephspurrier/goversioninfo/cmd/goversioninfo@v1.7.0 \
		-skip-versioninfo -icon build/icons/icon.ico -64 \
		-o cmd/kinopub-gui/resource_windows_amd64.syso
