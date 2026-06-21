VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
LDFLAGS := -s -w -X main.version=$(VERSION)
BUILD   := go build -ldflags "$(LDFLAGS)" -trimpath

.PHONY: all build gui cli web web-install run dev test vet clean release release-gui

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

# The original CLI.
cli:
	$(BUILD) -o kinopub ./cmd/kinopub

# The web GUI (embeds web/dist — run `make web` first for a fresh UI).
gui:
	$(BUILD) -o kinopub-gui ./cmd/kinopub-gui

build: cli gui

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

release: clean
	GOOS=darwin  GOARCH=arm64                $(BUILD) -o kinopub-darwin-arm64     ./cmd/kinopub
	GOOS=darwin  GOARCH=amd64                $(BUILD) -o kinopub-darwin-amd64     ./cmd/kinopub
	CGO_ENABLED=0 GOOS=linux   GOARCH=amd64  $(BUILD) -o kinopub-linux-amd64      ./cmd/kinopub
	CGO_ENABLED=0 GOOS=linux   GOARCH=arm64  $(BUILD) -o kinopub-linux-arm64      ./cmd/kinopub
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64  $(BUILD) -o kinopub-windows-amd64.exe ./cmd/kinopub
	CGO_ENABLED=0 GOOS=android GOARCH=arm64  $(BUILD) -o kinopub-android-arm64    ./cmd/kinopub
	@echo "Built CLI release binaries:"
	@ls -la kinopub-*

# Cross-compile the GUI for every platform (frontend built once, embedded into each).
# Cross-compiled targets are CGO-off; darwin built on a Mac keeps CGO for the
# optional Yandex keychain import.
release-gui: web
	GOOS=darwin  GOARCH=arm64                $(BUILD) -o kinopub-gui-darwin-arm64      ./cmd/kinopub-gui
	GOOS=darwin  GOARCH=amd64                $(BUILD) -o kinopub-gui-darwin-amd64      ./cmd/kinopub-gui
	CGO_ENABLED=0 GOOS=linux   GOARCH=amd64  $(BUILD) -o kinopub-gui-linux-amd64       ./cmd/kinopub-gui
	CGO_ENABLED=0 GOOS=linux   GOARCH=arm64  $(BUILD) -o kinopub-gui-linux-arm64       ./cmd/kinopub-gui
	CGO_ENABLED=0 GOOS=windows GOARCH=amd64  $(BUILD) -o kinopub-gui-windows-amd64.exe ./cmd/kinopub-gui
	@echo "Built GUI release binaries:"
	@ls -la kinopub-gui-*
