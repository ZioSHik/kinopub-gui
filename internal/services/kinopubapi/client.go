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
// yet confirmed the device code on kino.watch/device.
var ErrAuthorizationPending = errors.New("authorization_pending")

// ErrDeviceAuthError is returned by PollDeviceToken when kino.watch reports a
// terminal authorization error (expired code, access denied, device limit,
// etc.) — as opposed to a transient transport failure, which is retryable.
var ErrDeviceAuthError = errors.New("kino.watch device authorization error")

// ErrNotAuthenticated indicates no usable access/refresh token is available.
var ErrNotAuthenticated = errors.New("kino.watch API: not authenticated")

// ErrRefreshRejected indicates the refresh token was rejected by kino.watch (the
// session is dead server-side and the user must sign in again).
var ErrRefreshRejected = errors.New("kino.watch API: refresh token rejected")

// Client talks to the kino.watch JSON API. It manages the OAuth token set,
// refreshing it transparently before expiry and persisting refreshed tokens via
// the optional persist hook.
//
// kino.watch rotates (and invalidates) the refresh token on every refresh, so
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
		return DeviceCode{}, fmt.Errorf("kino.watch auth: %s: %s", out.Error, out.ErrorDescription)
	}
	if out.UserCode == "" || out.Code == "" {
		return DeviceCode{}, errors.New("kino.watch auth: empty device code response")
	}
	vu := out.VerificationURI
	if vu == "" {
		vu = "https://kino.watch/device"
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

// refreshTimeout bounds a token refresh on its own detached budget (see
// refresh).
const refreshTimeout = 30 * time.Second

// refresh exchanges the refresh token for a new token set.
func (c *Client) refresh(ctx context.Context) error {
	c.mu.Lock()
	rt := c.tokens.RefreshToken
	c.mu.Unlock()
	if rt == "" {
		return ErrNotAuthenticated
	}
	// The exchange runs detached from the caller's context, on its own budget.
	// kino.watch rotates the refresh token on every exchange: a POST canceled
	// after send but before the response is read (a navigation abort reaching
	// r.Context(), or the tail of the calling request's deadline) would lose the
	// new pair for good — the old refresh token is already dead server-side, and
	// the next 401 turns into a forced logout.
	refreshCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), refreshTimeout)
	defer cancel()
	ctx = refreshCtx
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
		return errors.New("kino.watch refresh: empty token response")
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
		return 0, safeRequestError(err)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, fmt.Errorf("kino.watch auth request: %w", safeRequestError(err))
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if c.debug != nil {
		c.debug(fmt.Sprintf("POST %s?%s -> HTTP %d: %s", path, grantOf(vals), resp.StatusCode, snippet(body)))
	}
	if jerr := json.Unmarshal(body, out); jerr != nil {
		return resp.StatusCode, fmt.Errorf("kino.watch auth: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
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
// concurrent callers triggers a single refresh — critical because kino.watch
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

// apiRequestTimeout bounds a SINGLE API read attempt — not the whole call: the
// original GET, the token refresh a 401 triggers (which runs on its own
// detached budget, see refresh) and the retried GET each get their own window,
// so an expired token on a slow link cannot eat the retry's time and fail a
// request whose every leg would have succeeded on its own.
//
// The shared HTTP client caps requests at 30s. That is far too long here:
// kino.watch is routinely unreachable without a VPN, and every blocked call held
// a browser socket for the full half minute. Browsers allow only ~6 connections
// per origin, so a few such calls — three tabs opened in a row — starved the
// pool, and unrelated work queued behind them: a local library scan the server
// answers in 11ms appeared to "scan" for half a minute. An unreachable host
// fails at the dial (8s cap in httpx), so this budget mostly bounds slow
// responses — 20s leaves room for a long serial's multi-MB /v1/items on a slow
// VPN, which the earlier 10s cut off.
//
// Only API reads are affected. Downloads use their own HTTP client.
const apiRequestTimeout = 20 * time.Second

// get performs an authenticated GET against /v1/<path> and decodes into out. A
// 401 triggers a single token refresh + retry.
func (c *Client) get(ctx context.Context, path string, q url.Values, out any) error {
	return c.doGet(ctx, path, q, out, false)
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
	status, body, err := c.fetch(ctx, reqURL)
	if err != nil {
		return fmt.Errorf("kino.watch API %s: %w", path, err)
	}

	if status == http.StatusUnauthorized && !retried {
		if rerr := c.refreshIfCurrent(ctx, token); rerr != nil {
			return fmt.Errorf("kino.watch API %s: unauthorized: %w", path, rerr)
		}
		q.Del("access_token")
		return c.doGet(ctx, path, q, out, true)
	}
	if status >= 400 {
		return fmt.Errorf("kino.watch API %s: HTTP %d: %s", path, status, snippet(body))
	}
	if err := json.Unmarshal(body, out); err != nil {
		return fmt.Errorf("kino.watch API %s: decode: %w", path, err)
	}
	return nil
}

// fetch performs one bounded GET attempt and returns the status and body.
// apiRequestTimeout applies to this attempt alone; a caller that already set a
// tighter deadline still wins (WithTimeout keeps the earlier one).
func (c *Client) fetch(ctx context.Context, reqURL string) (int, []byte, error) {
	ctx, cancel := context.WithTimeout(ctx, apiRequestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return 0, nil, safeRequestError(err)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, nil, safeRequestError(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	return resp.StatusCode, body, nil
}

// net/http embeds the full request URL, including OAuth query parameters, in
// transport errors. Keep the underlying cause for retries without exposing it.
func safeRequestError(err error) error {
	var requestErr *url.Error
	if errors.As(err, &requestErr) {
		return fmt.Errorf("%s: %w", requestErr.Op, safeRequestError(requestErr.Err))
	}
	return err
}

func snippet(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	return s
}
