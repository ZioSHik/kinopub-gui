package kinopubapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// Tokens is the OAuth2 token set persisted between runs.
type Tokens struct {
	AccessToken  string
	RefreshToken string
	Expiry       time.Time
}

// Valid reports whether an access token is present.
func (t Tokens) Valid() bool { return t.AccessToken != "" }

// ErrAuthorizationPending is returned by PollDeviceToken while the user has not
// yet confirmed the device code on kino.pub/device.
var ErrAuthorizationPending = errors.New("authorization_pending")

// ErrDeviceAuthError is returned by PollDeviceToken when kino.pub reports a
// terminal authorization error (expired code, access denied, device limit,
// etc.) — as opposed to a transient transport failure, which is retryable.
var ErrDeviceAuthError = errors.New("kino.pub device authorization error")

// ErrNotAuthenticated indicates no usable access/refresh token is available.
var ErrNotAuthenticated = errors.New("kino.pub API: not authenticated")

// ErrRefreshRejected indicates the refresh token was rejected by kino.pub (the
// session is dead server-side and the user must sign in again).
var ErrRefreshRejected = errors.New("kino.pub API: refresh token rejected")

// Client talks to the kino.pub JSON API. It manages the OAuth token set,
// refreshing it transparently before expiry and persisting refreshed tokens via
// the optional persist hook.
//
// kino.pub rotates (and invalidates) the refresh token on every refresh, so
// concurrent refreshes would lock the account out. All refreshes are therefore
// serialized through refreshMu with a re-check, collapsing a burst of expiring
// requests into a single refresh.
type Client struct {
	http         *http.Client
	clientID     string
	clientSecret string
	host         string // API base; defaults to apiHost (overridable in tests)

	mu        sync.Mutex
	tokens    Tokens
	persist   func(Tokens)
	refreshMu sync.Mutex   // serializes token refreshes (see type doc)
	debug     func(string) // optional raw-response logger for diagnostics
}

// Option customizes a Client.
type Option func(*Client)

// WithCredentials overrides the default client_id/client_secret.
func WithCredentials(id, secret string) Option {
	return func(c *Client) {
		if id != "" {
			c.clientID = id
		}
		if secret != "" {
			c.clientSecret = secret
		}
	}
}

// WithPersist registers a hook invoked whenever the token set changes (initial
// device login and every refresh), so the caller can persist it.
func WithPersist(fn func(Tokens)) Option {
	return func(c *Client) { c.persist = fn }
}

// WithDebug registers a logger that receives a one-line summary of every OAuth
// response (status + truncated body), for diagnosing the device-login flow.
func WithDebug(fn func(string)) Option {
	return func(c *Client) { c.debug = fn }
}

// New builds a Client. hc should be a proxy-aware client; if nil a default one
// with a sane timeout is used. tokens may be the zero value for a fresh login.
func New(hc *http.Client, tokens Tokens, opts ...Option) *Client {
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	c := &Client{
		http:         hc,
		clientID:     DefaultClientID,
		clientSecret: DefaultClientSecret,
		host:         apiHost,
		tokens:       tokens,
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// Tokens returns the current token set.
func (c *Client) Tokens() Tokens {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.tokens
}

// HasToken reports whether the client holds an access or refresh token.
func (c *Client) HasToken() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.tokens.AccessToken != "" || c.tokens.RefreshToken != ""
}

// ---------------------------------------------------------------------------
// Device-code OAuth flow
// ---------------------------------------------------------------------------

// DeviceCode is the user-facing activation challenge.
type DeviceCode struct {
	Code            string `json:"code"`
	UserCode        string `json:"userCode"`
	VerificationURI string `json:"verificationUri"`
	ExpiresIn       int    `json:"expiresIn"`
	Interval        int    `json:"interval"`
}

// RequestDeviceCode starts a device-code login: returns the code to display and
// the device_code to poll with. No token is required for this call.
func (c *Client) RequestDeviceCode(ctx context.Context) (DeviceCode, error) {
	var out deviceCodeResp
	if _, err := c.oauth(ctx, "/oauth2/device", url.Values{
		"grant_type":    {"device_code"},
		"client_id":     {c.clientID},
		"client_secret": {c.clientSecret},
	}, &out); err != nil {
		return DeviceCode{}, err
	}
	if out.Error != "" {
		return DeviceCode{}, fmt.Errorf("kino.pub auth: %s: %s", out.Error, out.ErrorDescription)
	}
	if out.UserCode == "" || out.Code == "" {
		return DeviceCode{}, errors.New("kino.pub auth: empty device code response")
	}
	vu := out.VerificationURI
	if vu == "" {
		vu = "https://kino.pub/device"
	}
	itv := out.Interval
	if itv <= 0 {
		itv = 5
	}
	return DeviceCode{
		Code:            out.Code,
		UserCode:        out.UserCode,
		VerificationURI: vu,
		ExpiresIn:       out.ExpiresIn,
		Interval:        itv,
	}, nil
}

// PollDeviceToken exchanges a confirmed device code for tokens. It returns
// ErrAuthorizationPending until the user confirms; any other error is terminal.
// On success the new tokens are stored (and persisted via the persist hook).
func (c *Client) PollDeviceToken(ctx context.Context, deviceCode string) (Tokens, error) {
	var out tokenResp
	if _, err := c.oauth(ctx, "/oauth2/device", url.Values{
		"grant_type":    {"device_token"},
		"client_id":     {c.clientID},
		"client_secret": {c.clientSecret},
		"code":          {deviceCode},
	}, &out); err != nil {
		return Tokens{}, err
	}
	switch {
	case out.Error == "authorization_pending", out.Error == "slow_down":
		return Tokens{}, ErrAuthorizationPending
	case out.Error != "":
		return Tokens{}, fmt.Errorf("%w: %s: %s", ErrDeviceAuthError, out.Error, out.ErrorDescription)
	case out.AccessToken == "":
		return Tokens{}, ErrAuthorizationPending
	}
	tk := tokensFrom(out)
	c.setTokens(tk)
	return tk, nil
}

// refresh exchanges the refresh token for a new token set.
func (c *Client) refresh(ctx context.Context) error {
	c.mu.Lock()
	rt := c.tokens.RefreshToken
	c.mu.Unlock()
	if rt == "" {
		return ErrNotAuthenticated
	}
	var out tokenResp
	status, err := c.oauth(ctx, "/oauth2/token", url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {c.clientID},
		"client_secret": {c.clientSecret},
		"refresh_token": {rt},
	}, &out)
	if err != nil {
		// A non-JSON body (e.g. a Cloudflare/CDN HTML error page) on a 4xx
		// means the server definitively rejected the request — treat it as a
		// rejected refresh so the GUI can clear the dead session.
		if status >= 400 && status < 500 {
			return fmt.Errorf("%w (HTTP %d): %v", ErrRefreshRejected, status, err)
		}
		return err
	}
	if out.Error != "" {
		return fmt.Errorf("%w (%s: %s)", ErrRefreshRejected, out.Error, out.ErrorDescription)
	}
	if out.AccessToken == "" {
		if status >= 400 && status < 500 {
			return fmt.Errorf("%w (HTTP %d)", ErrRefreshRejected, status)
		}
		return errors.New("kino.pub refresh: empty token response")
	}
	c.setTokens(tokensFrom(out))
	return nil
}

func tokensFrom(out tokenResp) Tokens {
	var exp time.Time
	if out.ExpiresIn > 0 {
		exp = time.Now().Add(time.Duration(out.ExpiresIn) * time.Second)
	}
	return Tokens{
		AccessToken:  out.AccessToken,
		RefreshToken: out.RefreshToken,
		Expiry:       exp,
	}
}

func (c *Client) setTokens(tk Tokens) {
	c.mu.Lock()
	c.tokens = tk
	persist := c.persist
	c.mu.Unlock()
	if persist != nil {
		persist(tk)
	}
}

// oauth performs an OAuth POST. The device/token endpoints return JSON (with
// error fields) even on 4xx, so we unmarshal the body regardless of status.
// It returns the HTTP status code alongside any error (0 when no response was
// received, e.g. on request-build or transport failure).
func (c *Client) oauth(ctx context.Context, path string, vals url.Values, out any) (int, error) {
	reqURL := c.host + path + "?" + vals.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, fmt.Errorf("kino.pub auth request: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if c.debug != nil {
		c.debug(fmt.Sprintf("POST %s?%s -> HTTP %d: %s", path, grantOf(vals), resp.StatusCode, snippet(body)))
	}
	if jerr := json.Unmarshal(body, out); jerr != nil {
		return resp.StatusCode, fmt.Errorf("kino.pub auth: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return resp.StatusCode, nil
}

// grantOf extracts the grant_type for debug logs (avoids logging the secret).
func grantOf(vals url.Values) string { return "grant_type=" + vals.Get("grant_type") }

// ---------------------------------------------------------------------------
// Authenticated API GET
// ---------------------------------------------------------------------------

// needsRefresh reports whether the token set should be refreshed (no access
// token, or within 60s of expiry).
func needsRefresh(tk Tokens) bool {
	return tk.AccessToken == "" || (!tk.Expiry.IsZero() && time.Until(tk.Expiry) < 60*time.Second)
}

// ensureToken returns a valid access token, refreshing it if it is within 60s
// of expiry. Refreshes are serialized (refreshMu) and re-checked so a burst of
// concurrent callers triggers a single refresh — critical because kino.pub
// invalidates the old refresh token on every refresh.
func (c *Client) ensureToken(ctx context.Context) (string, error) {
	c.mu.Lock()
	tk := c.tokens
	c.mu.Unlock()
	if tk.AccessToken == "" && tk.RefreshToken == "" {
		return "", ErrNotAuthenticated
	}
	if !needsRefresh(tk) {
		return tk.AccessToken, nil
	}

	c.refreshMu.Lock()
	defer c.refreshMu.Unlock()
	// Re-read after acquiring the lock: another goroutine may have just
	// refreshed while we waited.
	c.mu.Lock()
	tk = c.tokens
	c.mu.Unlock()
	if !needsRefresh(tk) {
		return tk.AccessToken, nil
	}
	if err := c.refresh(ctx); err != nil {
		if tk.AccessToken != "" && (tk.Expiry.IsZero() || tk.Expiry.After(time.Now())) {
			return tk.AccessToken, nil // refresh failed but current token still usable
		}
		return "", err
	}
	c.mu.Lock()
	tk = c.tokens
	c.mu.Unlock()
	return tk.AccessToken, nil
}

// refreshIfCurrent refreshes only if the in-memory access token still equals
// usedToken (the one a request just got a 401 for). If another goroutine has
// already rotated the token, it returns nil so the caller retries with the new
// one — avoiding a double refresh that would invalidate the fresh token.
func (c *Client) refreshIfCurrent(ctx context.Context, usedToken string) error {
	c.refreshMu.Lock()
	defer c.refreshMu.Unlock()
	c.mu.Lock()
	cur := c.tokens.AccessToken
	c.mu.Unlock()
	if cur != usedToken {
		return nil // already refreshed by another goroutine
	}
	return c.refresh(ctx)
}

// get performs an authenticated GET against /v1/<path> and decodes into out. A
// 401 triggers a single token refresh + retry.
func (c *Client) get(ctx context.Context, path string, q url.Values, out any) error {
	if err := c.doGet(ctx, path, q, out, false); err != nil {
		return err
	}
	return nil
}

func (c *Client) doGet(ctx context.Context, path string, q url.Values, out any, retried bool) error {
	token, err := c.ensureToken(ctx)
	if err != nil {
		return err
	}
	if q == nil {
		q = url.Values{}
	}
	q.Set("access_token", token)
	reqURL := c.host + "/v1/" + strings.TrimPrefix(path, "/")
	if enc := q.Encode(); enc != "" {
		reqURL += "?" + enc
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("kino.pub API %s: %w", path, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<20))

	if resp.StatusCode == http.StatusUnauthorized && !retried {
		if rerr := c.refreshIfCurrent(ctx, token); rerr != nil {
			return fmt.Errorf("kino.pub API %s: unauthorized: %w", path, rerr)
		}
		q.Del("access_token")
		return c.doGet(ctx, path, q, out, true)
	}
	if resp.StatusCode >= 400 {
		return fmt.Errorf("kino.pub API %s: HTTP %d: %s", path, resp.StatusCode, snippet(body))
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("kino.pub API %s: decode: %w", path, err)
	}
	return nil
}

func snippet(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	return s
}
