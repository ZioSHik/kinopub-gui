<p align="right"><a href="README.md">Русский</a> · <b>English</b></p>

# kino.pub downloader · GUI

A polished, self-contained **desktop-grade web interface** for [kino.pub](https://kino.pub) — **browse the whole catalog, preview titles in a built-in player, and download full-fidelity video** (every audio track, every subtitle, whole multi-season series) with live per-episode progress.

It talks to the **official kino.pub API** (the same one the Kodi/Android apps use): you sign in once with a short device code — no cookies, no Cloudflare wrangling, no browser scraping. Under the hood it drives a battle-tested Go download engine, so progress is real and structured (speed, ETA, per-track bars) instead of scraped terminal output. Ships as **one binary** — a Go server with the React UI embedded (`go:embed`) — run it and a browser tab opens. No Electron, no Node at runtime.

<p align="center">
  <img src="docs/screenshots/catalog.png" alt="kino.pub downloader" width="900">
</p>

<p align="center">
  <img alt="Go" src="https://img.shields.io/badge/Go-1.26-00ADD8?logo=go&logoColor=white">
  <img alt="React" src="https://img.shields.io/badge/React-18-61DAFB?logo=react&logoColor=black">
  <img alt="TypeScript" src="https://img.shields.io/badge/TypeScript-5-3178C6?logo=typescript&logoColor=white">
  <img alt="Tailwind CSS" src="https://img.shields.io/badge/Tailwind-3-38BDF8?logo=tailwindcss&logoColor=white">
  <img alt="License" src="https://img.shields.io/badge/License-MIT-f59e0b">
</p>

---

## Highlights

- 🎬 **Catalog browser** — search, tops, collections (подборки), genre/country filters, year and IMDb/Kinopoisk rating ranges, your watch history and "continue watching", and per-title detail with plot, cast, ratings and the full season/episode tree.
- ▶️ **Built-in player** — preview any title in-app (HLS) before you commit to a download, streamed through a same-origin proxy so it just works without CORS or VPN gymnastics in the browser.
- 🎬 **Full-fidelity downloads** — every audio track, every subtitle, whole multi-season series — picked from the catalog or pasted as a direct link.
- ⚡ **Real-time progress over SSE** — per-episode and per-audio-track percentage, download speed, ETA, byte/segment counts, and the engine's smart deferred-retry state, all live.
- 🔊 **Interactive audio picker** — choose which dubs/озвучки to keep, generalized across episodes, as a proper modal — or filter with a pattern.
- 🩺 **Doctor** — verify downloads against the state file and repair inconsistencies, with a readable report.
- 📚 **Library** — browse what you've already downloaded, with sizes, resolutions and missing-file detection; open a finished file or reveal its folder.
- 🔐 **Sign in once** — a short device-code login against the official kino.pub API; tokens are stored encrypted and machine-bound. Local features (Library, Doctor, Settings) work without signing in.
- 🌍 **Bilingual** — English & Russian, switchable in one click (remembered between sessions).
- 📦 **Single binary** — the UI is embedded; self-updates from GitHub releases.

## Screenshots

| Catalog browser | Live queue |
| --- | --- |
| ![Catalog](docs/screenshots/catalog.png) | ![Queue](docs/screenshots/queue.png) |

| Doctor | Settings |
| --- | --- |
| ![Doctor](docs/screenshots/doctor.png) | ![Settings](docs/screenshots/settings.png) |

---

## Requirements

- **ffmpeg** on your `PATH` (used to mux video + audio + subtitles). The UI shows a green/red indicator for `ffmpeg` and `ffprobe`.
  ```bash
  brew install ffmpeg          # macOS
  sudo apt install ffmpeg      # Debian/Ubuntu
  ```
  ```powershell
  winget install Gyan.FFmpeg   # Windows (or: choco install ffmpeg / scoop install ffmpeg)
  ```
  On Windows, make sure `ffmpeg.exe` and `ffprobe.exe` are on your `PATH` (the package managers above do this) — the Settings page confirms both are found.

  **Or just let the app install it:** if ffmpeg is missing, **Settings → System → Install ffmpeg** (and the button on the Download page) downloads a static ffmpeg/ffprobe build for your platform into the app's config dir and uses it automatically — no system install or admin rights needed.
- A modern browser (the app opens in your default one).
- A kino.pub account with an active subscription (for catalog, streaming and downloads).

## Install & run

**Prebuilt clients for every major platform** — grab one from the [releases page](https://github.com/ZioSHik/kinopub-gui/releases):

- 🍎 **macOS** — `.dmg` menu-bar app + standalone binaries, Apple Silicon (`arm64`) and Intel (`amd64`)
- 🪟 **Windows** — `x64` (`amd64`) executable, no console window and an embedded icon
- 🐧 **Linux** — `x64` (`amd64`, with a system-tray icon; also an `AppImage`) and `ARM64`
- 🤖 **Android** — `ARM64` (no native tray, web UI as usual; runs under Termux)

Same single binary everywhere — the React UI is embedded, so there is nothing else to install.

### Option A — download a release binary

Grab `kinopub-gui-*` for your platform from the [releases page](https://github.com/ZioSHik/kinopub-gui/releases), then run it:

```bash
chmod +x kinopub-gui-darwin-arm64
./kinopub-gui-darwin-arm64
# → opens http://127.0.0.1:8765 in your browser
```

On **macOS** you can instead grab the `.dmg` and drag **KinoPub** to Applications — it runs as a menu-bar app (no Dock icon; the status-bar item has *Open* and *Quit*).

On **Windows**, unzip `kinopub-gui-windows-amd64.zip` and run the executable (double-click or from a terminal):

```powershell
.\kinopub-gui-windows-amd64.exe
# → opens http://127.0.0.1:8765 in your browser
```

> The binary is unsigned, so SmartScreen / Gatekeeper may warn on first run — on Windows choose **More info → Run anyway**; on macOS right-click → **Open**. Windows Firewall may also prompt; the server only listens on loopback, so allowing private-network access is enough. Credentials are stored encrypted at `~/.config/kinopub/credentials.enc` (`%USERPROFILE%\.config\kinopub\credentials.enc` on Windows).

### Option B — build from source

You need Go 1.26+ and Node 20+ (only to build the UI; not at runtime).

```bash
git clone https://github.com/ZioSHik/kinopub-gui
cd kinopub-gui
make run          # builds the web UI, builds the GUI binary, and launches it
```

Or step by step:

```bash
make web          # build the React frontend into web/dist (embedded via go:embed)
make gui          # build the ./kinopub-gui binary
./kinopub-gui
```

> **Distribution:** grab the prebuilt release binaries above, use `make`, or install from source with `go install github.com/ZioSHik/kinopub-gui/cmd/kinopub-gui@latest` — the module path matches this repo and the embedded `web/dist` is committed, so the install produces a complete, runnable binary. A plain `go build ./cmd/kinopub-gui` also works; `web/dist` is committed, and `make web` regenerates it.

### Flags

```
kinopub-gui [flags]
  -addr      address to listen on (default 127.0.0.1:8765;
             falls back to an ephemeral port if taken)
  -no-open   do not open the browser automatically
  -version   print version and exit
```

The server binds to `127.0.0.1` only — it is a local control panel, not a public service. Every request is additionally checked for a loopback `Host` (defeating DNS-rebinding) and a same-origin `Origin` (defeating a malicious page's cross-site fetch).

### Updating

Release binaries self-update: **Settings → Software update** shows the current
version and, when a newer GitHub release exists, an **Update & restart** button.
It downloads the binary for your platform, verifies its SHA-256 against the
release `checksums.txt`, replaces the running executable in place, and restarts —
the open browser tab reconnects automatically. (Builds from source report as
`dev` and don't self-update; rebuild with `make`.)

---

## Using it

### 1. Sign in

Local features — **Library, Doctor, Settings, the folder picker** — work without signing in. The catalog, search, the in-app player and downloads need an account.

Click **Sign in** (top-right or in the sidebar) and:

1. The app shows a short **device code** and a link (`kino.pub/device`).
2. Open that link in any browser where you're logged into kino.pub and enter the code.
3. Confirm — the app detects it within a couple of seconds and you're in.

The device shows up in your kino.pub account's device list as `kinopub-gui (your-hostname)`. Tokens are encrypted with AES-256-GCM, bound to your machine, and stored at `~/.config/kinopub/credentials.enc`. Sign out any time from Settings.

> **kino.pub is often unavailable without a VPN.** If sign-in, the catalog or downloads hang or time out, enable a VPN or set a proxy (Settings → Proxy, or per-download in Advanced options). The UI shows a reminder and detects timeouts.

### 2. Find something

Open **Catalog** to search and browse. Filter by type, genre, country, year range and IMDb/Kinopoisk rating; browse tops and collections; or jump back into your **history** and **continue-watching** rows. Open a title to see its details, ratings, available озвучки and the full season/episode tree — and hit ▶ to **preview it in the built-in player** before downloading.

You can also paste a kino.pub link directly on the **Download** page if you already have one.

### 3. Download

From a title's detail view (or the Download page), tick the seasons/episodes you want, choose quality/container, and **Start download**. Live progress appears under **Queue** — overall, per-episode, and (for HLS sources) per audio/video track, with speed and ETA.

Download options mirror the engine's full feature set: quality, container, season/episode selection, audio filters, proxy (HTTP/HTTPS/SOCKS5), concurrency, retries, throttling, `--no-chunked`, and custom ffmpeg args.

### 4. Audio tracks

By default every audio track is kept. To keep only some:

- type a pattern in **Audio tracks** (e.g. `anilibria`, `!jpn`, `anilibria,!jpn` — `!`/`-` excludes), or
- enable **Interactive audio menu** and pick tracks in the modal when the download starts.

Matching is substring + language based and case-insensitive, so a dub labelled `01. Многоголосый. AniLibria (RUS)` in one episode and `02. AniLibria` in another both match `anilibria`. If a chosen dub is missing from some episode, the engine falls back to another track in the same language.

### 5. Doctor & Library

- **Doctor** verifies files against the state file (missing, truncated, size mismatch, incomplete record, orphan `.tmp`) and can repair them (`--fix`, `--clean-tmp`). It checks file presence and recorded size on disk — a fast, offline pass with no network round-trip.
- **Library** scans your output folders for `.kinopub-state.json` files and lists everything you've downloaded, flagging files that have gone missing on disk. Open or reveal any file straight from the list.

### 6. Settings

Defaults for new downloads (output folder, quality, container, concurrency, retries, throttle, proxy, no-chunked) plus extra folders to scan in the Library, the kino.pub sign-in, the ffmpeg installer and the software updater. Stored at `~/.config/kinopub/gui.json`.

---

## How it works

```
┌──────────────────────────────┐        SSE (live progress)        ┌───────────────────────┐
│  React + TS + Tailwind UI     │ ◀───────────────────────────────── │  Go HTTP server       │
│  (embedded via go:embed)      │ ──── REST (commands) ────────────▶ │  internal/gui         │
└──────────────────────────────┘                                    └─────┬───────────┬─────┘
                                                                          │ drives    │ official API
                                                          ┌───────────────▼──┐   ┌────▼──────────────┐
                                                          │ kinopub engine    │   │ kino.pub API      │
                                                          │ internal/app +    │   │ services/kinopubapi│
                                                          │ services (HLS,    │   │ (device login,    │
                                                          │ downloader, …)    │   │ discovery, stream)│
                                                          └───────────────────┘   └───────────────────┘
```

The GUI implements the engine's own seams instead of shelling out to anything:

- a `domain.ProgressReporter` (plus the optional `ByteProgressSink` / `SegmentProgressSink` / `HLSProgressSink` and the `EpisodeDeferred` hook) that turns engine callbacks into SSE events;
- a `domain.AudioChooser` that surfaces the interactive picker to the browser and blocks until you answer;
- a `logx.Handler` that streams engine log lines into each job's log view.

Discovery and streaming go through `internal/services/kinopubapi`, a small client for the official kino.pub JSON API: it manages the OAuth2 device-code login and transparently refreshes the (rotating) token set. The in-app player streams HLS through `/api/hls`, a same-origin proxy whose every URL is HMAC-signed by a per-process key, so it can never be used as an open proxy.

### Project layout

```
cmd/
  kinopub-gui/      GUI server entrypoint (embeds the UI, opens the browser, macOS/Windows tray)
internal/
  app/kinopub/      engine composition root (App.Run)
  domain/           ports & models
  services/
    kinopubapi/     official kino.pub API client (device login, discovery, stream resolution)
    downloader/     HLS + file download, ffmpeg muxing
    hlsdownloader/  HLS manifest parsing & segment download
    doctor/         verify & repair downloads
    statestore/     per-series .kinopub-state.json
    …               outputlayout, scheduler, progress, proxyprovider
  gui/              REST + SSE server, job manager, discovery, HLS player proxy, reporter/chooser
  lib/              credstore (encrypted creds), httpx (uTLS), logx, audiomenu, …
web/                React + Vite + Tailwind frontend
  dist/             built UI, embedded into the binary (go:embed)
```

## Development

```bash
# Terminal 1 — run the Go server (serves the embedded UI + API)
make gui && ./kinopub-gui

# Terminal 2 — hot-reloading frontend with API proxy to :8765
make dev            # → http://localhost:5173
```

`make vet` runs `go vet`, `make test` runs the test suite. CI builds the UI, vets, and runs the suite (including the race detector) on Linux, Windows and macOS.

## Credits

- The download engine and the hard parts it grew from (HLS, retries, encrypted creds, doctor): **[niazlv/kinopub-downloader](https://github.com/niazlv/kinopub-downloader)**.
- The web interface, the official-API catalog/player integration, and the packaging (`cmd/kinopub-gui`, `internal/gui`, `internal/services/kinopubapi`, `web/`): this project.

## License

MIT — see [LICENSE](LICENSE). The upstream engine is MIT-licensed; this repository preserves that license and adds the GUI under the same terms.
