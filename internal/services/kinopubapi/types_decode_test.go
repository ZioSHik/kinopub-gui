package kinopubapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestPostersBest(t *testing.T) {
	tests := []struct {
		name string
		p    Posters
		want string
	}{
		{"big preferred", Posters{Small: "s", Medium: "m", Big: "b", Wide: "w"}, "b"},
		{"medium when no big", Posters{Small: "s", Medium: "m", Wide: "w"}, "m"},
		{"wide when no big/medium", Posters{Small: "s", Wide: "w"}, "w"},
		{"small fallback", Posters{Small: "s"}, "s"},
		{"all empty", Posters{}, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.p.Best(); got != tt.want {
				t.Errorf("Best() = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestItemNamedIDNumericOrStringID proves json.Number decodes whether the API
// returns the id as a number or as a string.
func TestItemNamedIDNumericOrStringID(t *testing.T) {
	cases := []string{
		`{"id": 38290, "title": "X"}`,
		`{"id": "38290", "title": "X"}`,
	}
	for _, raw := range cases {
		var it Item
		if err := json.Unmarshal([]byte(raw), &it); err != nil {
			t.Fatalf("unmarshal %s: %v", raw, err)
		}
		if it.ID.String() != "38290" {
			t.Errorf("id = %q, want 38290 (from %s)", it.ID.String(), raw)
		}
	}
}

// TestDurationFractional: Duration averages/totals can be fractional floats.
func TestDurationFractional(t *testing.T) {
	var d Duration
	if err := json.Unmarshal([]byte(`{"average": 2700.5, "total": 81015.25}`), &d); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if d.Average != 2700.5 || d.Total != 81015.25 {
		t.Errorf("duration = %+v", d)
	}
}

// TestWatchingFractionalTime: resume position may be fractional.
func TestWatchingFractionalTime(t *testing.T) {
	var w Watching
	if err := json.Unmarshal([]byte(`{"status": 0, "time": 123.4}`), &w); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if w.Status != 0 || w.Time != 123.4 {
		t.Errorf("watching = %+v", w)
	}
}

// TestAudioFullShape: воisesover track including nested type/author NamedID.
func TestAudioFullShape(t *testing.T) {
	raw := `{
		"id": 5, "index": 1, "codec": "aac", "lang": "rus", "channels": 6,
		"type": {"id": 1, "title": "дубляж"},
		"author": {"id": "20", "title": "LostFilm"}
	}`
	var a Audio
	if err := json.Unmarshal([]byte(raw), &a); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if a.Channels != 6 || a.Type.Title != "дубляж" || a.Author.Title != "LostFilm" {
		t.Errorf("audio = %+v", a)
	}
	if a.Author.ID.String() != "20" {
		t.Errorf("author id = %q", a.Author.ID.String())
	}
}

// TestItemUnicodeAndOptionalFieldsAbsent: missing optional fields stay zero;
// unicode round-trips.
func TestItemUnicodeAndOptionalFieldsAbsent(t *testing.T) {
	raw := `{"id": 1, "title": "Война и мир", "type": "serial"}`
	var it Item
	if err := json.Unmarshal([]byte(raw), &it); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if it.Title != "Война и мир" {
		t.Errorf("title = %q", it.Title)
	}
	if it.Year != 0 || it.Plot != "" || len(it.Videos) != 0 || len(it.Seasons) != 0 {
		t.Errorf("absent fields should be zero: %+v", it)
	}
	// Note/WatchedAt/etc. are json:"-" so should never be populated from JSON.
	raw2 := `{"id": 1, "Note": "x", "WatchedAt": 99}`
	var it2 Item
	if err := json.Unmarshal([]byte(raw2), &it2); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if it2.Note != "" || it2.WatchedAt != 0 {
		t.Errorf("json:\"-\" fields populated: Note=%q WatchedAt=%d", it2.Note, it2.WatchedAt)
	}
}

func TestTokensValidAndHasToken(t *testing.T) {
	if (Tokens{}).Valid() {
		t.Error("empty tokens should not be Valid")
	}
	if !(Tokens{AccessToken: "x"}).Valid() {
		t.Error("tokens with access should be Valid")
	}
	c := New(nil, Tokens{})
	if c.HasToken() {
		t.Error("HasToken false for empty")
	}
	c2 := New(nil, Tokens{RefreshToken: "r"})
	if !c2.HasToken() {
		t.Error("HasToken true when refresh present")
	}
}

func TestNeedsRefresh(t *testing.T) {
	tests := []struct {
		name string
		tk   Tokens
		want bool
	}{
		{"no access token", Tokens{}, true},
		{"zero expiry never expires", Tokens{AccessToken: "x"}, false},
		{"far future ok", Tokens{AccessToken: "x", Expiry: time.Now().Add(time.Hour)}, false},
		{"within 60s window", Tokens{AccessToken: "x", Expiry: time.Now().Add(30 * time.Second)}, true},
		{"already expired", Tokens{AccessToken: "x", Expiry: time.Now().Add(-time.Second)}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := needsRefresh(tt.tk); got != tt.want {
				t.Errorf("needsRefresh = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTokensFromSetsExpiry(t *testing.T) {
	before := time.Now()
	tk := tokensFrom(tokenResp{AccessToken: "a", RefreshToken: "r", ExpiresIn: 100})
	if tk.AccessToken != "a" || tk.RefreshToken != "r" {
		t.Fatalf("tokens = %+v", tk)
	}
	if tk.Expiry.Before(before.Add(99*time.Second)) || tk.Expiry.After(time.Now().Add(101*time.Second)) {
		t.Errorf("expiry = %v, want ~100s out", tk.Expiry)
	}
	// ExpiresIn<=0 leaves a zero expiry.
	tk2 := tokensFrom(tokenResp{AccessToken: "a"})
	if !tk2.Expiry.IsZero() {
		t.Errorf("expiry should be zero when ExpiresIn unset, got %v", tk2.Expiry)
	}
}

// ---- device-code flow ----

func TestRequestDeviceCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code":       "DEV123",
			"user_code":  "AB-CD",
			"expires_in": 600,
			"interval":   7,
		})
	}))
	defer srv.Close()
	c := New(srv.Client(), Tokens{})
	c.host = srv.URL

	dc, err := c.RequestDeviceCode(context.Background())
	if err != nil {
		t.Fatalf("RequestDeviceCode: %v", err)
	}
	if dc.Code != "DEV123" || dc.UserCode != "AB-CD" || dc.ExpiresIn != 600 || dc.Interval != 7 {
		t.Errorf("device code = %+v", dc)
	}
	// verification_uri omitted → default kino.pub/device.
	if dc.VerificationURI != "https://kino.pub/device" {
		t.Errorf("verification uri = %q", dc.VerificationURI)
	}
}

func TestRequestDeviceCodeIntervalDefault(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// interval 0 → defaults to 5; verification_uri honored when present.
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code":             "C",
			"user_code":        "U",
			"verification_uri": "https://x.test/dev",
		})
	}))
	defer srv.Close()
	c := New(srv.Client(), Tokens{})
	c.host = srv.URL

	dc, err := c.RequestDeviceCode(context.Background())
	if err != nil {
		t.Fatalf("RequestDeviceCode: %v", err)
	}
	if dc.Interval != 5 {
		t.Errorf("interval = %d, want default 5", dc.Interval)
	}
	if dc.VerificationURI != "https://x.test/dev" {
		t.Errorf("verification uri = %q", dc.VerificationURI)
	}
}

func TestRequestDeviceCodeErrorField(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "invalid_client", "error_description": "bad creds"})
	}))
	defer srv.Close()
	c := New(srv.Client(), Tokens{})
	c.host = srv.URL

	_, err := c.RequestDeviceCode(context.Background())
	if err == nil || !strings.Contains(err.Error(), "invalid_client") || !strings.Contains(err.Error(), "bad creds") {
		t.Fatalf("error = %v, want invalid_client + desc", err)
	}
}

func TestRequestDeviceCodeEmptyResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{})
	}))
	defer srv.Close()
	c := New(srv.Client(), Tokens{})
	c.host = srv.URL

	_, err := c.RequestDeviceCode(context.Background())
	if err == nil || !strings.Contains(err.Error(), "empty device code") {
		t.Fatalf("error = %v, want empty device code", err)
	}
}

func TestPollDeviceTokenPending(t *testing.T) {
	for _, e := range []string{"authorization_pending", "slow_down"} {
		errVal := e
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewEncoder(w).Encode(map[string]any{"error": errVal})
		}))
		c := New(srv.Client(), Tokens{})
		c.host = srv.URL
		_, err := c.PollDeviceToken(context.Background(), "dc")
		if !errors.Is(err, ErrAuthorizationPending) {
			t.Errorf("error for %q = %v, want ErrAuthorizationPending", errVal, err)
		}
		srv.Close()
	}
}

func TestPollDeviceTokenEmptyAccessIsPending(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{})
	}))
	defer srv.Close()
	c := New(srv.Client(), Tokens{})
	c.host = srv.URL
	_, err := c.PollDeviceToken(context.Background(), "dc")
	if !errors.Is(err, ErrAuthorizationPending) {
		t.Fatalf("error = %v, want ErrAuthorizationPending", err)
	}
}

func TestPollDeviceTokenTerminalError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"error": "expired_token", "error_description": "code expired"})
	}))
	defer srv.Close()
	c := New(srv.Client(), Tokens{})
	c.host = srv.URL
	_, err := c.PollDeviceToken(context.Background(), "dc")
	if !errors.Is(err, ErrDeviceAuthError) {
		t.Fatalf("error = %v, want ErrDeviceAuthError", err)
	}
	if !strings.Contains(err.Error(), "expired_token") {
		t.Errorf("error = %v, want detail", err)
	}
}

func TestPollDeviceTokenSuccessPersistsTokens(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token":  "AT",
			"refresh_token": "RT",
			"expires_in":    3600,
		})
	}))
	defer srv.Close()

	var persisted Tokens
	c := New(srv.Client(), Tokens{}, WithPersist(func(tk Tokens) { persisted = tk }))
	c.host = srv.URL

	tk, err := c.PollDeviceToken(context.Background(), "dc")
	if err != nil {
		t.Fatalf("PollDeviceToken: %v", err)
	}
	if tk.AccessToken != "AT" || tk.RefreshToken != "RT" {
		t.Errorf("tokens = %+v", tk)
	}
	if persisted.AccessToken != "AT" {
		t.Errorf("persist hook not called with new tokens: %+v", persisted)
	}
	if c.Tokens().AccessToken != "AT" {
		t.Errorf("client tokens not updated: %+v", c.Tokens())
	}
}

// TestOAuthNonJSONBody: a non-JSON OAuth body yields an error carrying the
// status and raw text.
func TestOAuthNonJSONBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("<html>cloudflare</html>"))
	}))
	defer srv.Close()
	c := New(srv.Client(), Tokens{})
	c.host = srv.URL

	_, err := c.RequestDeviceCode(context.Background())
	if err == nil || !strings.Contains(err.Error(), "HTTP 502") {
		t.Fatalf("error = %v, want HTTP 502", err)
	}
}

func TestWithCredentialsOverride(t *testing.T) {
	c := New(nil, Tokens{}, WithCredentials("myid", "mysecret"))
	if c.clientID != "myid" || c.clientSecret != "mysecret" {
		t.Errorf("creds = %q/%q", c.clientID, c.clientSecret)
	}
	// Empty values must NOT override the defaults.
	c2 := New(nil, Tokens{}, WithCredentials("", ""))
	if c2.clientID != DefaultClientID || c2.clientSecret != DefaultClientSecret {
		t.Errorf("empty override clobbered defaults: %q/%q", c2.clientID, c2.clientSecret)
	}
}

func TestWithDebugReceivesSummary(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"code": "C", "user_code": "U"})
	}))
	defer srv.Close()
	var logged string
	c := New(srv.Client(), Tokens{}, WithDebug(func(s string) { logged = s }))
	c.host = srv.URL

	_, _ = c.RequestDeviceCode(context.Background())
	if !strings.Contains(logged, "grant_type=device_code") || !strings.Contains(logged, "HTTP 200") {
		t.Errorf("debug log = %q", logged)
	}
	// Must not leak the client secret.
	if strings.Contains(logged, DefaultClientSecret) {
		t.Errorf("debug log leaked secret: %q", logged)
	}
}

func TestGrantOf(t *testing.T) {
	if got := grantOf(url.Values{"grant_type": {"refresh_token"}, "client_secret": {"s"}}); got != "grant_type=refresh_token" {
		t.Errorf("grantOf = %q", got)
	}
}

func TestSnippetTruncates(t *testing.T) {
	if got := snippet([]byte("  hello  ")); got != "hello" {
		t.Errorf("snippet trim = %q", got)
	}
	long := strings.Repeat("a", 300)
	got := snippet([]byte(long))
	if len([]rune(got)) != 201 || !strings.HasSuffix(got, "…") {
		t.Errorf("snippet truncation wrong: len=%d suffix ok=%v", len([]rune(got)), strings.HasSuffix(got, "…"))
	}
}

// TestRefreshFallbackKeepsUsableToken: when refresh fails but the current access
// token is still within its lifetime, ensureToken returns it instead of erroring.
func TestRefreshFallbackKeepsUsableToken(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/oauth2/token", func(w http.ResponseWriter, r *http.Request) {
		// transient 500 (not a 4xx rejection) so refresh errors but is retryable
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("oops"))
	})
	mux.HandleFunc("/v1/items", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{}})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// access token still valid for 30s (inside refresh window but not expired).
	c := New(srv.Client(), Tokens{AccessToken: "stillgood", RefreshToken: "r", Expiry: time.Now().Add(30 * time.Second)})
	c.host = srv.URL

	if _, err := c.Items(context.Background(), ItemsParams{}); err != nil {
		t.Fatalf("Items should succeed with still-usable token despite refresh failure: %v", err)
	}
}
