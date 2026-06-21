package gui

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/niazlv/kinopub-downloader/internal/lib/credstore"
)

// Server hosts the REST API, the SSE event stream and the embedded SPA.
type Server struct {
	version  string
	static   fs.FS
	hub      *Hub
	mgr      *JobManager
	settings *settingsStore
	updater  *updateChecker
	tools    *toolInstaller
	restart  func() // set by main to re-exec the freshly installed binary
	mux      *http.ServeMux
}

// NewServer builds the HTTP handler. static is the embedded frontend (rooted at
// the build output directory).
func NewServer(version string, static fs.FS) *Server {
	cleanupOldExecutable()   // remove a leftover binary from a previous self-update
	ensureManagedBinOnPath() // so a previously installed ffmpeg/ffprobe is found
	hub := newHub()
	s := &Server{
		version:  version,
		static:   static,
		hub:      hub,
		mgr:      newJobManager(hub),
		settings: newSettingsStore(),
		updater:  newUpdateChecker(version),
		tools:    &toolInstaller{},
	}
	s.routes()
	return s
}

// SetRestart registers a function the server calls to re-exec the process after
// an in-place update has been installed.
func (s *Server) SetRestart(fn func()) { s.restart = fn }

// Handler returns the root http.Handler. There is no auth gate: local features
// (Library, Doctor, Settings, the folder picker) work without signing in;
// kino.pub operations (preview/download) fail with a clear error when no
// credentials are available, which the UI surfaces and prompts to sign in.
//
// All routes sit behind guardLocalOnly, which protects this credential-holding
// localhost server from web pages: it rejects requests whose Host is not a
// loopback address (defeating DNS-rebinding) and cross-origin requests carrying
// a foreign Origin (defeating a malicious site's direct fetch to 127.0.0.1).
func (s *Server) Handler() http.Handler { return guardLocalOnly(s.mux) }

// guardLocalOnly wraps the mux with anti-rebinding / anti-CSRF checks suitable
// for a loopback-only control server.
func guardLocalOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isLoopbackHost(r.Host) {
			writeErr(w, http.StatusForbidden, "forbidden: this server only accepts loopback requests")
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" {
			if u, err := url.Parse(origin); err != nil || u.Host != r.Host {
				writeErr(w, http.StatusForbidden, "forbidden: cross-origin request")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// isLoopbackHost reports whether the Host header refers to a loopback address.
// An empty Host (HTTP/1.0 or some proxies) is accepted because the listener
// itself is bound to loopback.
func isLoopbackHost(host string) bool {
	if host == "" {
		return true
	}
	h, _, err := net.SplitHostPort(host)
	if err != nil {
		h = host
	}
	if strings.EqualFold(h, "localhost") {
		return true
	}
	if ip := net.ParseIP(h); ip != nil {
		return ip.IsLoopback()
	}
	return false
}

func (s *Server) routes() {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/health", s.handleHealth)
	mux.HandleFunc("GET /api/state", s.handleState)
	mux.HandleFunc("GET /api/events", s.handleEvents)

	mux.HandleFunc("GET /api/auth", s.handleAuthStatus)
	mux.HandleFunc("POST /api/auth/login", s.handleLogin)
	mux.HandleFunc("POST /api/auth/logout", s.handleLogout)

	mux.HandleFunc("GET /api/ffmpeg", s.handleFFmpeg)

	mux.HandleFunc("GET /api/deps", s.handleDeps)
	mux.HandleFunc("POST /api/deps/install", s.handleDepsInstall)

	mux.HandleFunc("GET /api/update", s.handleUpdateCheck)
	mux.HandleFunc("POST /api/update/apply", s.handleUpdateApply)

	mux.HandleFunc("GET /api/settings", s.handleGetSettings)
	mux.HandleFunc("PUT /api/settings", s.handlePutSettings)

	mux.HandleFunc("POST /api/preview", s.handlePreview)

	mux.HandleFunc("GET /api/jobs", s.handleListJobs)
	mux.HandleFunc("POST /api/jobs", s.handleCreateJob)
	mux.HandleFunc("POST /api/jobs/clear", s.handleClearJobs)
	mux.HandleFunc("GET /api/jobs/{id}", s.handleGetJob)
	mux.HandleFunc("DELETE /api/jobs/{id}", s.handleDeleteJob)
	mux.HandleFunc("POST /api/jobs/{id}/cancel", s.handleCancelJob)
	mux.HandleFunc("POST /api/jobs/{id}/audio", s.handleAudioAnswer)

	mux.HandleFunc("POST /api/doctor", s.handleDoctor)
	mux.HandleFunc("GET /api/library", s.handleLibrary)
	mux.HandleFunc("POST /api/library/delete", s.handleDeleteLibrary)
	mux.HandleFunc("POST /api/open", s.handleOpenPath)
	mux.HandleFunc("GET /api/fs", s.handleFS)
	mux.HandleFunc("GET /api/img", s.handleImage)

	// SPA / static assets.
	mux.HandleFunc("GET /", s.handleStatic)

	s.mux = mux
}

// ---------------------------------------------------------------------------
// Snapshot / events
// ---------------------------------------------------------------------------

func (s *Server) snapshot() map[string]any {
	return map[string]any{
		"version":  s.version,
		"jobs":     s.mgr.list(),
		"auth":     authStatus(),
		"ffmpeg":   ffmpegStatus(),
		"settings": s.settings.get(),
	}
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "version": s.version})
}

func (s *Server) handleState(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.snapshot())
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	if _, ok := w.(http.Flusher); !ok {
		writeErr(w, http.StatusInternalServerError, "streaming unsupported")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")

	rc := http.NewResponseController(w)
	ch := s.hub.subscribe()
	defer s.hub.unsubscribe(ch)

	// Initial full snapshot so a fresh client is immediately consistent. A write
	// error (slow/half-open client) ends the handler, releasing the goroutine
	// and the hub subscription instead of pinning them on a blocked Write.
	if err := s.writeSSE(rc, w, Event{Type: "snapshot", Data: s.snapshot()}); err != nil {
		return
	}

	heartbeat := time.NewTicker(25 * time.Second)
	defer heartbeat.Stop()

	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case <-heartbeat.C:
			if err := writeWithDeadline(rc, w, []byte(": ping\n\n")); err != nil {
				return
			}
		case ev, ok := <-ch:
			if !ok {
				return
			}
			if err := s.writeSSE(rc, w, ev); err != nil {
				return
			}
		}
	}
}

// writeSSE marshals ev and writes it as an SSE data frame under a write
// deadline, returning any error so the caller can tear the stream down.
func (s *Server) writeSSE(rc *http.ResponseController, w io.Writer, ev Event) error {
	data, err := json.Marshal(ev)
	if err != nil {
		return nil // a single unmarshalable event shouldn't kill the stream
	}
	return writeWithDeadline(rc, w, []byte(fmt.Sprintf("data: %s\n\n", data)))
}

// writeWithDeadline writes p with a bounded deadline and flushes, so a stuck
// client cannot pin the handler goroutine indefinitely.
func writeWithDeadline(rc *http.ResponseController, w io.Writer, p []byte) error {
	_ = rc.SetWriteDeadline(time.Now().Add(10 * time.Second))
	if _, err := w.Write(p); err != nil {
		return err
	}
	return rc.Flush()
}

// ---------------------------------------------------------------------------
// Auth
// ---------------------------------------------------------------------------

func (s *Server) handleAuthStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, authStatus())
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	st, err := doLogin(req)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	s.hub.broadcast(Event{Type: "auth", Data: st})
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if err := credstore.Clear(); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	st := authStatus()
	s.hub.broadcast(Event{Type: "auth", Data: st})
	writeJSON(w, http.StatusOK, st)
}

// ---------------------------------------------------------------------------
// FFmpeg / settings
// ---------------------------------------------------------------------------

func (s *Server) handleFFmpeg(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, ffmpegStatus())
}

func (s *Server) handleDeps(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, depsView())
}

func (s *Server) handleDepsInstall(w http.ResponseWriter, r *http.Request) {
	// Downloading a static ffmpeg can take a while; run on a background context
	// so it isn't aborted if the request context is cancelled.
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	err := s.tools.installFFmpeg(ctx)
	cancel()
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	// Let every connected client refresh its ffmpeg indicator.
	s.hub.broadcast(Event{Type: "ffmpeg", Data: ffmpegStatus()})
	writeJSON(w, http.StatusOK, depsView())
}

func (s *Server) handleUpdateCheck(w http.ResponseWriter, r *http.Request) {
	force := r.URL.Query().Get("force") == "1"
	writeJSON(w, http.StatusOK, s.updater.status(r.Context(), force))
}

func (s *Server) handleUpdateApply(w http.ResponseWriter, r *http.Request) {
	// Run on a background context with a generous timeout so a slow download
	// isn't aborted if the request context is cancelled mid-way.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	version, err := s.updater.apply(ctx)
	cancel()
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	canRestart := s.restart != nil
	writeJSON(w, http.StatusOK, map[string]any{"updated": true, "version": version, "restarting": canRestart})
	if canRestart {
		// Restart shortly after the response is flushed so the browser tab can
		// reconnect to the new process on the same port.
		go func() {
			time.Sleep(800 * time.Millisecond)
			s.restart()
		}()
	}
}

func (s *Server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.settings.get())
}

func (s *Server) handlePutSettings(w http.ResponseWriter, r *http.Request) {
	var in Settings
	if err := decodeJSON(w, r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	saved, err := s.settings.save(in)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.hub.broadcast(Event{Type: "settings", Data: saved})
	writeJSON(w, http.StatusOK, saved)
}

// ---------------------------------------------------------------------------
// Preview
// ---------------------------------------------------------------------------

func (s *Server) handlePreview(w http.ResponseWriter, r *http.Request) {
	var req RunRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	resp, err := s.preview(r.Context(), req)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// ---------------------------------------------------------------------------
// Jobs
// ---------------------------------------------------------------------------

// StartRequest is the create-job payload: a RunRequest plus optional metadata
// seeded from a prior preview so the live view shows titles without re-fetching.
type StartRequest struct {
	RunRequest
	SeedTitle  string            `json:"seedTitle"`
	SeedPoster string            `json:"seedPoster"`
	SeedTitles map[string]string `json:"seedTitles"`
}

func (s *Server) handleListJobs(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.mgr.list())
}

func (s *Server) handleCreateJob(w http.ResponseWriter, r *http.Request) {
	var req StartRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	cfg, err := buildRunConfig(req.RunRequest)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if cfg.InputURL == "" && cfg.FeedFile == "" {
		writeErr(w, http.StatusBadRequest, "a kino.pub URL or feed file is required")
		return
	}
	// ffmpeg is required for real downloads (not dry-run).
	if !cfg.DryRun {
		if _, lookErr := exec.LookPath(cfg.FFmpegPath); lookErr != nil {
			writeErr(w, http.StatusPreconditionFailed, "ffmpeg not found on PATH — install ffmpeg to download")
			return
		}
	}

	input := cfg.InputURL
	if input == "" {
		input = cfg.FeedFile
	}
	job := newJob(s.mgr.nextID(), input, cfg)
	if req.SeedTitle != "" {
		job.title = req.SeedTitle
	}
	if req.SeedPoster != "" {
		job.posterURL = req.SeedPoster
	}
	s.mgr.add(job)
	s.mgr.publishNow(job)

	go s.mgr.run(context.Background(), job, cfg, req.SeedTitles, req.SeedTitle, req.SeedPoster)

	writeJSON(w, http.StatusAccepted, job.snapshot())
}

func (s *Server) handleGetJob(w http.ResponseWriter, r *http.Request) {
	job, ok := s.mgr.get(r.PathValue("id"))
	if !ok {
		writeErr(w, http.StatusNotFound, "job not found")
		return
	}
	writeJSON(w, http.StatusOK, job.snapshot())
}

func (s *Server) handleDeleteJob(w http.ResponseWriter, r *http.Request) {
	removed, exists := s.mgr.remove(r.PathValue("id"))
	if !exists {
		writeErr(w, http.StatusNotFound, "job not found")
		return
	}
	if !removed {
		writeErr(w, http.StatusConflict, "job is still running — cancel it first")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"removed": true})
}

func (s *Server) handleClearJobs(w http.ResponseWriter, r *http.Request) {
	n := s.mgr.clearFinished()
	writeJSON(w, http.StatusOK, map[string]int{"removed": n})
}

func (s *Server) handleCancelJob(w http.ResponseWriter, r *http.Request) {
	if !s.mgr.cancelJob(r.PathValue("id")) {
		writeErr(w, http.StatusNotFound, "job not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"canceling": true})
}

func (s *Server) handleAudioAnswer(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Indices []int `json:"indices"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if !s.mgr.answerAudio(r.PathValue("id"), body.Indices) {
		writeErr(w, http.StatusConflict, "no pending audio selection for this job")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// ---------------------------------------------------------------------------
// Doctor / library / fs / image
// ---------------------------------------------------------------------------

func (s *Server) handleDoctor(w http.ResponseWriter, r *http.Request) {
	var req DoctorRequest
	if err := decodeJSON(w, r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.OutputDir == "" {
		req.OutputDir = s.settings.get().OutputPath
	}
	report, err := runDoctor(r.Context(), req)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, report)
}

func (s *Server) handleLibrary(w http.ResponseWriter, r *http.Request) {
	dirs := s.libraryDirs()
	writeJSON(w, http.StatusOK, scanLibrary(dirs))
}

func (s *Server) handleDeleteLibrary(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Dir string `json:"dir"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if body.Dir == "" {
		writeErr(w, http.StatusBadRequest, "dir is required")
		return
	}
	if err := deleteLibrarySeries(body.Dir, s.libraryDirs()); err != nil {
		writeErr(w, http.StatusForbidden, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"deleted": true})
}

func (s *Server) handleOpenPath(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Path   string `json:"path"`
		Reveal bool   `json:"reveal"`
	}
	if err := decodeJSON(w, r, &body); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if body.Path == "" {
		writeErr(w, http.StatusBadRequest, "path is required")
		return
	}
	if !s.openPathAllowed(body.Path) {
		writeErr(w, http.StatusForbidden, "path is outside the configured library/output folders")
		return
	}
	if _, err := os.Stat(body.Path); err != nil {
		writeErr(w, http.StatusNotFound, "file not found")
		return
	}
	if err := openInOS(body.Path, body.Reveal); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// openPathAllowed reports whether target lies inside one of the configured
// library/output folders or an active job's output path. This confines the
// /api/open endpoint to the directories the user actually downloads into, so it
// cannot be coaxed into opening or revealing arbitrary files on disk.
func (s *Server) openPathAllowed(target string) bool {
	abs, err := filepath.Abs(target)
	if err != nil {
		return false
	}
	abs = filepath.Clean(abs)

	roots := s.libraryDirs()
	roots = append(roots, s.mgr.outputPaths()...)
	for _, root := range roots {
		rabs, err := filepath.Abs(root)
		if err != nil {
			continue
		}
		rel, err := filepath.Rel(filepath.Clean(rabs), abs)
		if err != nil {
			continue
		}
		if rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))) {
			return true
		}
	}
	return false
}

// libraryDirs returns the set of directories to scan for downloads: the default
// output path plus any extra dirs the user configured.
func (s *Server) libraryDirs() []string {
	cfg := s.settings.get()
	seen := make(map[string]bool)
	var dirs []string
	add := func(d string) {
		if d == "" || seen[d] {
			return
		}
		seen[d] = true
		dirs = append(dirs, d)
	}
	add(cfg.OutputPath)
	for _, d := range cfg.LibraryDirs {
		add(d)
	}
	return dirs
}

func (s *Server) handleFS(w http.ResponseWriter, r *http.Request) {
	listing, err := listDir(r.URL.Query().Get("path"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, listing)
}

func (s *Server) handleImage(w http.ResponseWriter, r *http.Request) {
	proxyImage(w, r, r.URL.Query().Get("u"))
}

// ---------------------------------------------------------------------------
// Static SPA
// ---------------------------------------------------------------------------

func (s *Server) handleStatic(w http.ResponseWriter, r *http.Request) {
	if s.static == nil {
		writeErr(w, http.StatusNotFound, "frontend not built")
		return
	}
	upath := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
	if upath == "" {
		upath = "index.html"
	}

	// Try the requested asset; on miss, fall back to index.html (SPA routing).
	f, err := s.static.Open(upath)
	if err != nil {
		s.serveIndex(w, r)
		return
	}
	stat, statErr := f.Stat()
	if statErr != nil || stat.IsDir() {
		f.Close()
		s.serveIndex(w, r)
		return
	}
	defer f.Close()

	setContentType(w, upath)
	if strings.HasPrefix(upath, "assets/") {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	}
	if rs, ok := f.(io.ReadSeeker); ok {
		http.ServeContent(w, r, upath, stat.ModTime(), rs)
		return
	}
	_, _ = io.Copy(w, f)
}

func (s *Server) serveIndex(w http.ResponseWriter, r *http.Request) {
	f, err := s.static.Open("index.html")
	if err != nil {
		writeErr(w, http.StatusNotFound, "frontend not built — run `make web` (or build the web/ project)")
		return
	}
	defer f.Close()
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = io.Copy(w, f)
}

func setContentType(w http.ResponseWriter, name string) {
	switch {
	case strings.HasSuffix(name, ".html"):
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
	case strings.HasSuffix(name, ".js"):
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	case strings.HasSuffix(name, ".css"):
		w.Header().Set("Content-Type", "text/css; charset=utf-8")
	case strings.HasSuffix(name, ".svg"):
		w.Header().Set("Content-Type", "image/svg+xml")
	case strings.HasSuffix(name, ".json"):
		w.Header().Set("Content-Type", "application/json")
	case strings.HasSuffix(name, ".woff2"):
		w.Header().Set("Content-Type", "font/woff2")
	case strings.HasSuffix(name, ".png"):
		w.Header().Set("Content-Type", "image/png")
	case strings.HasSuffix(name, ".webp"):
		w.Header().Set("Content-Type", "image/webp")
	case strings.HasSuffix(name, ".ico"):
		w.Header().Set("Content-Type", "image/x-icon")
	}
}
