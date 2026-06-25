package gui

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ZioSHik/kinopub-gui/internal/domain"
)

func newTestServer(t *testing.T) *Server {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	return NewServer("v1.2.3", nil)
}

func TestHandleHealth(t *testing.T) {
	s := newTestServer(t)
	req := httptest.NewRequest("GET", "/api/health", nil)
	w := httptest.NewRecorder()
	s.handleHealth(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var body map[string]string
	json.Unmarshal(w.Body.Bytes(), &body)
	if body["status"] != "ok" || body["version"] != "v1.2.3" {
		t.Errorf("health body = %v", body)
	}
}

func TestHandleState(t *testing.T) {
	s := newTestServer(t)
	req := httptest.NewRequest("GET", "/api/state", nil)
	w := httptest.NewRecorder()
	s.handleState(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var snap map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &snap); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, k := range []string{"version", "jobs", "kpauth", "ffmpeg", "settings"} {
		if _, ok := snap[k]; !ok {
			t.Errorf("snapshot missing %q", k)
		}
	}
}

func TestHandleGetPutSettings(t *testing.T) {
	s := newTestServer(t)

	// PUT with out-of-range values that should be clamped.
	in := Settings{Concurrency: 99, Retries: -1, Container: "avi", MaxActiveJobs: 50}
	body, _ := json.Marshal(in)
	req := httptest.NewRequest("PUT", "/api/settings", bytes.NewReader(body))
	w := httptest.NewRecorder()
	s.handlePutSettings(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, body=%s", w.Code, w.Body.String())
	}
	var saved Settings
	json.Unmarshal(w.Body.Bytes(), &saved)
	if saved.Concurrency != 16 || saved.Retries != 0 || saved.Container != "mkv" || saved.MaxActiveJobs != 16 {
		t.Errorf("settings not clamped: %+v", saved)
	}

	// GET returns the persisted settings.
	req = httptest.NewRequest("GET", "/api/settings", nil)
	w = httptest.NewRecorder()
	s.handleGetSettings(w, req)
	var got Settings
	json.Unmarshal(w.Body.Bytes(), &got)
	if got.Concurrency != 16 {
		t.Errorf("GET settings = %+v", got)
	}
}

func TestHandlePutSettings_BadJSON(t *testing.T) {
	s := newTestServer(t)
	req := httptest.NewRequest("PUT", "/api/settings", strings.NewReader("{bad"))
	w := httptest.NewRecorder()
	s.handlePutSettings(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("bad JSON status = %d, want 400", w.Code)
	}
}

func TestHandleListJobsEmpty(t *testing.T) {
	s := newTestServer(t)
	req := httptest.NewRequest("GET", "/api/jobs", nil)
	w := httptest.NewRecorder()
	s.handleListJobs(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	if strings.TrimSpace(w.Body.String()) != "[]" {
		t.Errorf("empty jobs should serialize to [], got %s", w.Body.String())
	}
}

func TestHandleGetJob_NotFound(t *testing.T) {
	s := newTestServer(t)
	req := httptest.NewRequest("GET", "/api/jobs/missing", nil)
	req.SetPathValue("id", "missing")
	w := httptest.NewRecorder()
	s.handleGetJob(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestHandleDeleteJob_RunningConflict(t *testing.T) {
	s := newTestServer(t)
	j := newJobWith("run", statusRunning)
	s.mgr.add(j)
	req := httptest.NewRequest("DELETE", "/api/jobs/run", nil)
	req.SetPathValue("id", "run")
	w := httptest.NewRecorder()
	s.handleDeleteJob(w, req)
	if w.Code != http.StatusConflict {
		t.Errorf("deleting a running job: status = %d, want 409", w.Code)
	}
}

func TestHandleDeleteJob_NotFound(t *testing.T) {
	s := newTestServer(t)
	req := httptest.NewRequest("DELETE", "/api/jobs/x", nil)
	req.SetPathValue("id", "x")
	w := httptest.NewRecorder()
	s.handleDeleteJob(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestHandleClearJobs(t *testing.T) {
	s := newTestServer(t)
	s.mgr.add(newJobWith("a", statusCompleted))
	s.mgr.add(newJobWith("b", statusRunning))
	req := httptest.NewRequest("POST", "/api/jobs/clear", nil)
	w := httptest.NewRecorder()
	s.handleClearJobs(w, req)
	var body map[string]int
	json.Unmarshal(w.Body.Bytes(), &body)
	if body["removed"] != 1 {
		t.Errorf("removed = %d, want 1 (only the finished job)", body["removed"])
	}
}

func TestHandleCancelJob_NotFound(t *testing.T) {
	s := newTestServer(t)
	req := httptest.NewRequest("POST", "/api/jobs/x/cancel", nil)
	req.SetPathValue("id", "x")
	w := httptest.NewRecorder()
	s.handleCancelJob(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestHandleAudioAnswer_NoPending(t *testing.T) {
	s := newTestServer(t)
	s.mgr.add(newJobWith("j", statusRunning))
	body, _ := json.Marshal(map[string][]int{"indices": {0, 1}})
	req := httptest.NewRequest("POST", "/api/jobs/j/audio", bytes.NewReader(body))
	req.SetPathValue("id", "j")
	w := httptest.NewRecorder()
	s.handleAudioAnswer(w, req)
	if w.Code != http.StatusConflict {
		t.Errorf("no pending audio: status = %d, want 409", w.Code)
	}
}

func TestHandleRetryEpisode_JobNotFound(t *testing.T) {
	s := newTestServer(t)
	body, _ := json.Marshal(map[string]int{"season": 1, "episode": 1})
	req := httptest.NewRequest("POST", "/api/jobs/x/retry-episode", bytes.NewReader(body))
	req.SetPathValue("id", "x")
	w := httptest.NewRecorder()
	s.handleRetryEpisode(w, req)
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestHandlePrioritizeJob_NotWaiting(t *testing.T) {
	s := newTestServer(t)
	s.mgr.add(newJobWith("j", statusRunning))
	req := httptest.NewRequest("POST", "/api/jobs/j/prioritize", nil)
	req.SetPathValue("id", "j")
	w := httptest.NewRecorder()
	s.handlePrioritizeJob(w, req)
	if w.Code != http.StatusConflict {
		t.Errorf("status = %d, want 409", w.Code)
	}
}

func TestHandlePauseResumeJob_Conflict(t *testing.T) {
	s := newTestServer(t)
	s.mgr.add(newJobWith("done", statusCompleted))

	req := httptest.NewRequest("POST", "/api/jobs/done/pause", nil)
	req.SetPathValue("id", "done")
	w := httptest.NewRecorder()
	s.handlePauseJob(w, req)
	if w.Code != http.StatusConflict {
		t.Errorf("pause finished: status = %d, want 409", w.Code)
	}

	req = httptest.NewRequest("POST", "/api/jobs/done/resume", nil)
	req.SetPathValue("id", "done")
	w = httptest.NewRecorder()
	s.handleResumeJob(w, req)
	if w.Code != http.StatusConflict {
		t.Errorf("resume finished: status = %d, want 409", w.Code)
	}
}

func TestHandleCreateJob_RequiresURL(t *testing.T) {
	s := newTestServer(t)
	body, _ := json.Marshal(StartRequest{})
	req := httptest.NewRequest("POST", "/api/jobs", bytes.NewReader(body))
	w := httptest.NewRecorder()
	s.handleCreateJob(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("missing URL: status = %d, want 400", w.Code)
	}
}

func TestHandleLibrary(t *testing.T) {
	s := newTestServer(t)
	lib := t.TempDir()
	s.settings.save(Settings{OutputPath: lib, Container: "mkv", Concurrency: 2})
	writeStateFile(t, filepath.Join(lib, "Show"), domain.DownloadState{
		Series:    "1",
		Completed: map[string]domain.CompletedRec{"S1E1": {Season: 1, Episode: 1}},
	})
	req := httptest.NewRequest("GET", "/api/library", nil)
	w := httptest.NewRecorder()
	s.handleLibrary(w, req)
	var resp LibraryResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp.Series) != 1 {
		t.Errorf("expected 1 series, got %d", len(resp.Series))
	}
}

func TestHandleLibraryDownloaded_RequiresID(t *testing.T) {
	s := newTestServer(t)
	req := httptest.NewRequest("GET", "/api/library/downloaded", nil)
	w := httptest.NewRecorder()
	s.handleLibraryDownloaded(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("missing id: status = %d, want 400", w.Code)
	}
}

func TestHandleDeleteLibrary_Validation(t *testing.T) {
	s := newTestServer(t)
	// Missing dir.
	body, _ := json.Marshal(map[string]string{})
	req := httptest.NewRequest("POST", "/api/library/delete", bytes.NewReader(body))
	w := httptest.NewRecorder()
	s.handleDeleteLibrary(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("missing dir: status = %d, want 400", w.Code)
	}

	// Dir outside configured roots → forbidden.
	body, _ = json.Marshal(map[string]string{"dir": t.TempDir()})
	req = httptest.NewRequest("POST", "/api/library/delete", bytes.NewReader(body))
	w = httptest.NewRecorder()
	s.handleDeleteLibrary(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("outside-root dir: status = %d, want 403", w.Code)
	}
}

func TestHandleOpenPath_Forbidden(t *testing.T) {
	s := newTestServer(t)
	outside := filepath.Join(t.TempDir(), "secret.txt")
	_ = os.WriteFile(outside, []byte("x"), 0o644)
	body, _ := json.Marshal(map[string]any{"path": outside})
	req := httptest.NewRequest("POST", "/api/open", bytes.NewReader(body))
	w := httptest.NewRecorder()
	s.handleOpenPath(w, req)
	if w.Code != http.StatusForbidden {
		t.Errorf("outside path: status = %d, want 403", w.Code)
	}
}

func TestHandleFS(t *testing.T) {
	s := newTestServer(t)
	dir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(dir, "sub"), 0o755)
	_ = os.MkdirAll(filepath.Join(dir, ".hidden"), 0o755)
	req := httptest.NewRequest("GET", "/api/fs?path="+dir, nil)
	w := httptest.NewRecorder()
	s.handleFS(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var listing FSListing
	json.Unmarshal(w.Body.Bytes(), &listing)
	if len(listing.Dirs) != 1 || listing.Dirs[0].Name != "sub" {
		t.Errorf("hidden dirs should be filtered: %+v", listing.Dirs)
	}
}

func TestHandleDoctor_EmptyDir(t *testing.T) {
	s := newTestServer(t)
	dir := t.TempDir() // no state files
	body, _ := json.Marshal(DoctorRequest{OutputDir: dir})
	req := httptest.NewRequest("POST", "/api/doctor", bytes.NewReader(body))
	w := httptest.NewRecorder()
	s.handleDoctor(w, req)
	// An empty folder is not an error; the doctor reports nothing to check.
	if w.Code != http.StatusOK && w.Code != http.StatusBadRequest {
		t.Errorf("unexpected status %d: %s", w.Code, w.Body.String())
	}
}

func TestServerLibraryDirsDedup(t *testing.T) {
	s := newTestServer(t)
	s.settings.save(Settings{
		OutputPath:  "/out",
		LibraryDirs: []string{"/out", "/extra", ""}, // "/out" dup, "" skipped
		Container:   "mkv",
		Concurrency: 2,
	})
	dirs := s.libraryDirs()
	if len(dirs) != 2 {
		t.Errorf("libraryDirs = %v, want 2 deduped entries", dirs)
	}
}
