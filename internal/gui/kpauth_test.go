package gui

import (
	"errors"
	"net/http/httptest"
	"testing"
	"time"
)

func TestMaxInt(t *testing.T) {
	cases := []struct{ a, b, want int }{
		{1, 2, 2},
		{5, 3, 5},
		{0, 0, 0},
		{-1, -2, -1},
	}
	for _, c := range cases {
		if got := maxInt(c.a, c.b); got != c.want {
			t.Errorf("maxInt(%d,%d) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

// kpStatus reads credstore (from disk) — point HOME/XDG at a temp dir so no real
// credentials leak in, then it reports LoggedIn=false and reflects the in-memory
// login session.
func TestKPStatus_PendingSession(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	s := &Server{}
	s.kpLogin = &kpLoginSession{
		userCode:        "ABCD-1234",
		verificationURI: "https://kino.pub/device",
		expiresAt:       time.Now().Add(5 * time.Minute),
	}
	st := s.kpStatus()
	if !st.Pending {
		t.Fatal("expected Pending=true for a live session")
	}
	if st.UserCode != "ABCD-1234" || st.VerificationURI != "https://kino.pub/device" {
		t.Errorf("session fields not surfaced: %+v", st)
	}
	if st.ExpiresAt == 0 {
		t.Error("ExpiresAt should be set")
	}
}

func TestKPStatus_ExpiredSessionNotPending(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	s := &Server{}
	s.kpLogin = &kpLoginSession{
		userCode:  "X",
		expiresAt: time.Now().Add(-1 * time.Minute), // already expired
	}
	st := s.kpStatus()
	if st.Pending {
		t.Error("an expired session must not report Pending")
	}
}

func TestKPStatus_DoneSessionWithError(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	s := &Server{}
	s.kpLogin = &kpLoginSession{
		done:      true,
		err:       errors.New("device limit reached"),
		expiresAt: time.Now().Add(5 * time.Minute),
	}
	st := s.kpStatus()
	if st.Pending {
		t.Error("a done session must not be pending")
	}
	if st.Error != "device limit reached" {
		t.Errorf("Error = %q, want surfaced session error", st.Error)
	}
}

func TestKPStatus_NoSession(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	s := &Server{}
	st := s.kpStatus()
	if st.LoggedIn || st.Pending || st.Error != "" {
		t.Errorf("no session / no creds should be all-zero, got %+v", st)
	}
}

func TestFinishKP_SuccessClearsAndBroadcasts(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	s := &Server{hub: newHub()}
	sess := &kpLoginSession{expiresAt: time.Now().Add(5 * time.Minute)}
	s.kpLogin = sess
	sub := s.hub.subscribe()
	defer s.hub.unsubscribe(sub)

	s.finishKP(sess, nil)

	if !sess.done {
		t.Error("session should be marked done")
	}
	if sess.err != nil {
		t.Errorf("successful finish should have nil err, got %v", sess.err)
	}
	select {
	case ev := <-sub:
		if ev.Type != "kpauth" {
			t.Errorf("broadcast type = %q, want kpauth", ev.Type)
		}
	default:
		t.Error("finishKP should broadcast a kpauth event")
	}
}

func TestKPClientOrErr_NotSignedIn(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	s := &Server{}
	w := httptest.NewRecorder()
	_, ok := s.kpClientOrErr(w)
	if ok {
		t.Fatal("expected ok=false when not signed in")
	}
	if w.Code != 401 {
		t.Errorf("status = %d, want 401", w.Code)
	}
}
