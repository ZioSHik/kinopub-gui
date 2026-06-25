package gui

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSetContentType(t *testing.T) {
	cases := map[string]string{
		"index.html":  "text/html; charset=utf-8",
		"app.js":      "text/javascript; charset=utf-8",
		"app.css":     "text/css; charset=utf-8",
		"icon.svg":    "image/svg+xml",
		"data.json":   "application/json",
		"font.woff2":  "font/woff2",
		"pic.png":     "image/png",
		"pic.webp":    "image/webp",
		"favicon.ico": "image/x-icon",
		"noext":       "", // unknown → not set
	}
	for name, want := range cases {
		w := httptest.NewRecorder()
		setContentType(w, name)
		if got := w.Header().Get("Content-Type"); got != want {
			t.Errorf("setContentType(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestReporterStopNoop(t *testing.T) {
	_, _, r := newReporterJob()
	r.Stop() // must not panic
}

func TestKPDeviceInfo(t *testing.T) {
	s := &Server{version: "v9.9.9"}
	di := s.kpDeviceInfo()
	if di.Title == "" {
		t.Error("device title should not be empty")
	}
	if !strings.HasPrefix(di.Title, "kinopub-gui") {
		t.Errorf("device title = %q, want kinopub-gui prefix", di.Title)
	}
	if !strings.Contains(di.Software, "v9.9.9") {
		t.Errorf("software = %q, want it to embed the version", di.Software)
	}
	if di.Hardware == "" {
		t.Error("hardware (GOOS) should be set")
	}
}

func TestHandlePreview_Unauthenticated(t *testing.T) {
	s := newTestServer(t)
	body, _ := json.Marshal(RunRequest{URL: "https://kino.pub/item/view/1"})
	req := httptest.NewRequest("POST", "/api/preview", bytes.NewReader(body))
	w := httptest.NewRecorder()
	s.handlePreview(w, req)
	// preview() resolves the client → not signed in → error surfaced as 502.
	if w.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", w.Code)
	}
}

func TestHandlePreview_BadJSON(t *testing.T) {
	s := newTestServer(t)
	req := httptest.NewRequest("POST", "/api/preview", strings.NewReader("{bad"))
	w := httptest.NewRecorder()
	s.handlePreview(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", w.Code)
	}
}

func TestHandleRetryJob_NotFound(t *testing.T) {
	s := newTestServer(t)
	req := httptest.NewRequest("POST", "/api/jobs/x/retry", nil)
	req.SetPathValue("id", "x")
	w := httptest.NewRecorder()
	s.handleRetryJob(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestHandleRetryJob_RunningConflict(t *testing.T) {
	s := newTestServer(t)
	s.mgr.add(newJobWith("j", statusRunning))
	req := httptest.NewRequest("POST", "/api/jobs/j/retry", nil)
	req.SetPathValue("id", "j")
	w := httptest.NewRecorder()
	s.handleRetryJob(w, req)
	if w.Code != http.StatusConflict {
		t.Errorf("running retry: status = %d, want 409", w.Code)
	}
}

func TestEpisodeHandlers_BadBody(t *testing.T) {
	s := newTestServer(t)
	handlers := map[string]http.HandlerFunc{
		"prioritize-episode": s.handlePrioritizeEpisode,
		"pause-episode":      s.handlePauseEpisode,
		"resume-episode":     s.handleResumeEpisode,
		"retry-episode":      s.handleRetryEpisode,
	}
	for name, h := range handlers {
		req := httptest.NewRequest("POST", "/api/jobs/j/"+name, strings.NewReader("{bad"))
		req.SetPathValue("id", "j")
		w := httptest.NewRecorder()
		h(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s with bad body: status = %d, want 400", name, w.Code)
		}
	}
}

func TestEpisodeHandlers_NotRunningConflict(t *testing.T) {
	s := newTestServer(t)
	s.mgr.add(newJobWith("j", statusCompleted))
	body, _ := json.Marshal(map[string]int{"season": 1, "episode": 1})

	req := httptest.NewRequest("POST", "/api/jobs/j/prioritize-episode", bytes.NewReader(body))
	req.SetPathValue("id", "j")
	w := httptest.NewRecorder()
	s.handlePrioritizeEpisode(w, req)
	if w.Code != http.StatusConflict {
		t.Errorf("prioritize-episode on finished job: status = %d, want 409", w.Code)
	}

	req = httptest.NewRequest("POST", "/api/jobs/j/pause-episode", bytes.NewReader(body))
	req.SetPathValue("id", "j")
	w = httptest.NewRecorder()
	s.handlePauseEpisode(w, req)
	if w.Code != http.StatusConflict {
		t.Errorf("pause-episode on finished job: status = %d, want 409", w.Code)
	}
}

func TestHandleDeleteLibraryEpisode_Validation(t *testing.T) {
	s := newTestServer(t)
	// Missing dir/key.
	body, _ := json.Marshal(map[string]string{"dir": ""})
	req := httptest.NewRequest("POST", "/api/library/delete-episode", bytes.NewReader(body))
	w := httptest.NewRecorder()
	s.handleDeleteLibraryEpisode(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("missing dir/key: status = %d, want 400", w.Code)
	}
	// Dir outside roots → forbidden.
	body, _ = json.Marshal(map[string]string{"dir": t.TempDir(), "key": "S1E1"})
	req = httptest.NewRequest("POST", "/api/library/delete-episode", bytes.NewReader(body))
	w = httptest.NewRecorder()
	s.handleDeleteLibraryEpisode(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("outside-root: status = %d, want 403", w.Code)
	}
}

func TestHandleOpenPath_Validation(t *testing.T) {
	s := newTestServer(t)
	// Missing path.
	body, _ := json.Marshal(map[string]any{})
	req := httptest.NewRequest("POST", "/api/open", bytes.NewReader(body))
	w := httptest.NewRecorder()
	s.handleOpenPath(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("missing path: status = %d, want 400", w.Code)
	}
}
