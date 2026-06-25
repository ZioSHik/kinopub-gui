package gui

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHandleDiscoverMarkTime_Unauthenticated(t *testing.T) {
	s := newTestServer(t)
	body, _ := json.Marshal(map[string]any{"id": "1", "season": 1, "episode": 2, "time": 30})
	req := httptest.NewRequest("POST", "/api/discover/marktime", bytes.NewReader(body))
	w := httptest.NewRecorder()
	s.handleDiscoverMarkTime(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

func TestHandleDiscoverMarkTime_BadBody(t *testing.T) {
	s := newTestServer(t)
	req := httptest.NewRequest("POST", "/api/discover/marktime", nil)
	w := httptest.NewRecorder()
	s.handleDiscoverMarkTime(w, req)
	// Unauthenticated → 401 before body parsing (client resolved first).
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

func TestHandleDiscoverStream_Unauthenticated(t *testing.T) {
	s := newTestServer(t)
	req := httptest.NewRequest("GET", "/api/discover/stream?id=1", nil)
	w := httptest.NewRecorder()
	s.handleDiscoverStream(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", w.Code)
	}
}

func TestHandleUpdateCheck_DevBuild(t *testing.T) {
	s := newTestServer(t) // version "v1.2.3" is a real tag; force avoids network only when cached
	// Prime the updater cache so no network call is made.
	s.updater.cached = &UpdateStatus{Current: "v1.2.3", Note: "cached"}
	s.updater.at = time.Now()
	req := httptest.NewRequest("GET", "/api/update", nil)
	w := httptest.NewRecorder()
	s.handleUpdateCheck(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var st UpdateStatus
	json.Unmarshal(w.Body.Bytes(), &st)
	if st.Note != "cached" {
		t.Errorf("expected cached status, got %+v", st)
	}
}

func TestHandleFFmpegAndDeps(t *testing.T) {
	s := newTestServer(t)
	// /api/ffmpeg always returns 200 with a status object.
	req := httptest.NewRequest("GET", "/api/ffmpeg", nil)
	w := httptest.NewRecorder()
	s.handleFFmpeg(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("ffmpeg status = %d", w.Code)
	}
	var fs FFmpegStatus
	if err := json.Unmarshal(w.Body.Bytes(), &fs); err != nil {
		t.Errorf("ffmpeg body not a FFmpegStatus: %v", err)
	}

	req = httptest.NewRequest("GET", "/api/deps", nil)
	w = httptest.NewRecorder()
	s.handleDeps(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("deps status = %d", w.Code)
	}
}

func TestHandleKPStatus(t *testing.T) {
	s := newTestServer(t)
	req := httptest.NewRequest("GET", "/api/kp/status", nil)
	w := httptest.NewRecorder()
	s.handleKPStatus(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var st KPStatus
	json.Unmarshal(w.Body.Bytes(), &st)
	if st.LoggedIn {
		t.Error("a fresh server should not be logged in")
	}
}
