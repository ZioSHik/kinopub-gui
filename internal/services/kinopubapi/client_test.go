package kinopubapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// TestEnsureTokenSerializesRefresh proves the fix for the device-lockout bug:
// a burst of concurrent requests whose access token is expired must trigger
// exactly ONE refresh (kino.watch invalidates the old refresh token on each
// refresh, so concurrent refreshes would kill the session).
func TestEnsureTokenSerializesRefresh(t *testing.T) {
	var refreshCount int32
	mux := http.NewServeMux()
	mux.HandleFunc("/oauth2/token", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&refreshCount, 1)
		time.Sleep(20 * time.Millisecond) // widen the race window
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "new-access",
			"refresh_token": "new-refresh",
			"expires_in":    3600,
		})
	})
	mux.HandleFunc("/v1/items", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{}})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := New(srv.Client(), Tokens{
		AccessToken:  "old-access",
		RefreshToken: "old-refresh",
		Expiry:       time.Now().Add(-time.Minute), // expired → needs refresh
	})
	c.host = srv.URL

	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := c.Items(context.Background(), ItemsParams{}); err != nil {
				t.Errorf("Items: %v", err)
			}
		}()
	}
	wg.Wait()

	if got := atomic.LoadInt32(&refreshCount); got != 1 {
		t.Fatalf("refresh called %d times under concurrency, want exactly 1", got)
	}
	if tk := c.Tokens(); tk.AccessToken != "new-access" || tk.RefreshToken != "new-refresh" {
		t.Fatalf("tokens not rotated after refresh: %+v", tk)
	}
}

// TestEnsureTokenSkipsRefreshWhenValid: a still-valid token triggers no refresh.
func TestEnsureTokenSkipsRefreshWhenValid(t *testing.T) {
	var refreshCount int32
	mux := http.NewServeMux()
	mux.HandleFunc("/oauth2/token", func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&refreshCount, 1)
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "x", "expires_in": 3600})
	})
	mux.HandleFunc("/v1/items", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{}})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := New(srv.Client(), Tokens{AccessToken: "good", Expiry: time.Now().Add(time.Hour)})
	c.host = srv.URL
	if _, err := c.Items(context.Background(), ItemsParams{}); err != nil {
		t.Fatalf("Items: %v", err)
	}
	if got := atomic.LoadInt32(&refreshCount); got != 0 {
		t.Fatalf("refresh called %d times for a valid token, want 0", got)
	}
}

// TestRefreshRejectedIsSentinel: a refused refresh surfaces ErrRefreshRejected
// so the GUI can clear the dead session and prompt re-login.
func TestRefreshRejectedIsSentinel(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/oauth2/token", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "invalid_grant", "error_description": "dead"})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := New(srv.Client(), Tokens{RefreshToken: "dead", Expiry: time.Now().Add(-time.Hour)})
	c.host = srv.URL
	_, err := c.Items(context.Background(), ItemsParams{})
	if err == nil {
		t.Fatal("expected an error from a rejected refresh")
	}
	if !errorsIs(err, ErrRefreshRejected) {
		t.Fatalf("error %v is not ErrRefreshRejected", err)
	}
}

// TestRefreshRejectedOnHTTP4xxNoErrorField verifies the robustness fix: when a
// kino.watch intermediary (proxy/CDN) returns a 4xx whose JSON body has no
// "error" field, refresh() must still surface ErrRefreshRejected so the GUI
// clears the dead session instead of retrying forever.
func TestRefreshRejectedOnHTTP4xxNoErrorField(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/oauth2/token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized) // 401, no "error" field
		_ = json.NewEncoder(w).Encode(map[string]any{"message": "nope"})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := New(srv.Client(), Tokens{RefreshToken: "dead", Expiry: time.Now().Add(-time.Hour)})
	c.host = srv.URL
	_, err := c.Items(context.Background(), ItemsParams{})
	if err == nil {
		t.Fatal("expected an error from a 401 with no error field")
	}
	if !errorsIs(err, ErrRefreshRejected) {
		t.Fatalf("error %v does not wrap ErrRefreshRejected", err)
	}
}

// errorsIs avoids importing errors twice in this small test file.
func errorsIs(err, target error) bool {
	for err != nil {
		if err == target {
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

func TestTransportErrorsDoNotExposeOAuthQuery(t *testing.T) {
	cause := context.DeadlineExceeded
	err := &url.Error{Op: "Get", URL: "https://api.example/items?access_token=private-access&refresh_token=private-refresh", Err: cause}
	safe := safeRequestError(err)
	if strings.Contains(safe.Error(), "private-") || strings.Contains(safe.Error(), "access_token") {
		t.Fatalf("secret in transport error: %s", safe)
	}
	if !errors.Is(safe, cause) {
		t.Fatal("transport cause lost; cancellation/retry matching would break")
	}
}

func TestOAuthDebugNeverLogsResponseCredentials(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"access_token":"fake-private-access","refresh_token":"fake-private-refresh","code":"fake-device-code"}`)
	}))
	defer srv.Close()
	var logs []string
	c := New(srv.Client(), Tokens{}, WithDebug(func(line string) { logs = append(logs, line) }))
	c.host = srv.URL
	var result map[string]any
	if _, err := c.oauth(context.Background(), "/oauth2/token", url.Values{"grant_type": {"device_token"}}, &result); err != nil {
		t.Fatal(err)
	}
	if len(logs) == 0 || !strings.Contains(logs[0], "HTTP 200") {
		t.Fatal("OAuth response status missing")
	}
	if strings.Contains(strings.Join(logs, "\n"), "fake-") {
		t.Fatal("OAuth debug exposed response credentials")
	}
}
