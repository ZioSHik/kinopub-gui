package gui

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"runtime"
	"time"

	"github.com/ZioSHik/kinopub-gui/internal/lib/credstore"
	"github.com/ZioSHik/kinopub-gui/internal/services/kinopubapi"
	"github.com/ZioSHik/kinopub-gui/internal/services/proxyprovider"
)

// kpDeviceInfo builds a friendly identity for the account's device list so the
// device shows up named instead of "unknown".
func (s *Server) kpDeviceInfo() kinopubapi.DeviceInfo {
	host, _ := os.Hostname()
	title := "kinopub-gui"
	if host != "" {
		title = "kinopub-gui (" + host + ")"
	}
	return kinopubapi.DeviceInfo{
		Title:    title,
		Hardware: runtime.GOOS,
		Software: "kinopub-gui " + s.version,
	}
}

// KPStatus is the official-API auth state surfaced to the UI.
type KPStatus struct {
	LoggedIn        bool   `json:"loggedIn"`
	Pending         bool   `json:"pending"`
	UserCode        string `json:"userCode,omitempty"`
	VerificationURI string `json:"verificationUri,omitempty"`
	ExpiresAt       int64  `json:"expiresAt,omitempty"` // unix seconds
	Error           string `json:"error,omitempty"`
}

// kpLoginSession tracks an in-flight device-code login that polls in the
// background until the user confirms (or it expires).
type kpLoginSession struct {
	userCode        string
	verificationURI string
	expiresAt       time.Time
	done            bool
	err             error
	cancel          context.CancelFunc
}

// storedAPITokens reads the persisted token set from the credential store.
func storedAPITokens() kinopubapi.Tokens {
	creds, _ := credstore.Load()
	var exp time.Time
	if creds.APIExpiry > 0 {
		exp = time.Unix(creds.APIExpiry, 0)
	}
	return kinopubapi.Tokens{
		AccessToken:  creds.APIAccessToken,
		RefreshToken: creds.APIRefreshToken,
		Expiry:       exp,
	}
}

// saveAPITokens merges the token set into the existing credentials (preserving
// any stored cookie) and persists them. The read-modify-write goes through
// credstore.Update so a concurrent logout/refresh can't clobber it.
func saveAPITokens(tk kinopubapi.Tokens) error {
	return credstore.Update(func(c *credstore.Credentials) {
		c.APIAccessToken = tk.AccessToken
		c.APIRefreshToken = tk.RefreshToken
		if tk.Expiry.IsZero() {
			c.APIExpiry = 0
		} else {
			c.APIExpiry = tk.Expiry.Unix()
		}
	})
}

// clearAPITokens removes only the API token fields, leaving any cookie intact.
func clearAPITokens() error {
	return credstore.Update(func(c *credstore.Credentials) {
		c.APIAccessToken = ""
		c.APIRefreshToken = ""
		c.APIExpiry = 0
	})
}

// persistAPITokens saves a (rotated) token set, logging loudly on failure.
// kino.pub invalidates the old refresh token on every refresh, so a dropped
// write means the on-disk token is dead and the session is lost on restart — a
// silent error would be undiagnosable, hence the explicit log.
func persistAPITokens(tk kinopubapi.Tokens) {
	if err := saveAPITokens(tk); err != nil {
		log.Printf("[kp] CRITICAL: failed to persist kino.pub tokens (%v); the session may be lost on restart and re-login required", err)
	}
}

// kpHTTPClient builds a proxy-aware HTTP client using the configured proxy.
func (s *Server) kpHTTPClient() (*http.Client, error) {
	proxyProv, err := proxyprovider.New(s.settings.get().Proxy)
	if err != nil {
		return nil, err
	}
	return proxyProv.HTTPClient(), nil
}

// kpClient returns a single cached, authenticated API client. Reusing one
// instance (rather than building a fresh client per request) is essential:
// kino.pub rotates the refresh token on every refresh, so all discovery
// requests must share one client whose internal mutex serializes refreshes.
// The cache is invalidated on login, logout and settings changes.
func (s *Server) kpClient() (*kinopubapi.Client, error) {
	s.kpClientMu.Lock()
	defer s.kpClientMu.Unlock()
	if s.kpClientCached != nil {
		return s.kpClientCached, nil
	}
	creds, _ := credstore.Load()
	if !creds.HasAPIToken() {
		return nil, errors.New("not signed in to kino.pub — open Settings and sign in to the API")
	}
	hc, err := s.kpHTTPClient()
	if err != nil {
		return nil, err
	}
	gen := s.kpLogoutGen.Load()
	client := kinopubapi.New(hc, storedAPITokens(), kinopubapi.WithPersist(func(tk kinopubapi.Tokens) {
		// A refresh that completes after a logout (gen bumped) must not write the
		// rotated tokens back — that would silently undo the logout. The token is
		// dead server-side anyway, so dropping the write is correct.
		if s.kpLogoutGen.Load() != gen {
			return
		}
		persistAPITokens(tk)
	}))
	s.kpClientCached = client
	return client, nil
}

// invalidateKPClient drops the cached discovery client so the next request
// rebuilds it from the freshly stored tokens / proxy.
func (s *Server) invalidateKPClient() {
	s.kpClientMu.Lock()
	s.kpClientCached = nil
	s.kpClientMu.Unlock()
}

// kpFail writes an error for a discovery handler. If the failure means the
// kino.pub session is dead server-side (refresh rejected / not authenticated),
// it clears the stale tokens and broadcasts so the UI prompts a clean re-login
// instead of silently failing every request.
func (s *Server) kpFail(w http.ResponseWriter, err error) {
	if errors.Is(err, kinopubapi.ErrRefreshRejected) || errors.Is(err, kinopubapi.ErrNotAuthenticated) {
		_ = clearAPITokens()
		s.invalidateKPClient()
		s.hub.broadcast(Event{Type: "kpauth", Data: s.kpStatus()})
		writeErr(w, http.StatusUnauthorized, "kino.pub session expired — please sign in again")
		return
	}
	writeErr(w, http.StatusBadGateway, err.Error())
}

// kpStatus reports the current official-API auth state.
func (s *Server) kpStatus() KPStatus {
	creds, _ := credstore.Load()
	st := KPStatus{LoggedIn: creds.HasAPIToken()}

	// Copy the mutable session fields while holding kpMu: the background poller
	// writes done/err under the same lock, so reading them after unlocking would
	// be a data race (and a torn read of the err interface could even fault).
	s.kpMu.Lock()
	sess := s.kpLogin
	var (
		done                     bool
		serr                     error
		userCode, verificationURI string
		expiresAt                time.Time
	)
	if sess != nil {
		done = sess.done
		serr = sess.err
		userCode = sess.userCode
		verificationURI = sess.verificationURI
		expiresAt = sess.expiresAt
	}
	s.kpMu.Unlock()
	if sess != nil {
		if serr != nil {
			st.Error = serr.Error()
		}
		if !done && time.Now().Before(expiresAt) {
			st.Pending = true
			st.UserCode = userCode
			st.VerificationURI = verificationURI
			st.ExpiresAt = expiresAt.Unix()
		}
	}
	return st
}

func (s *Server) handleKPStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.kpStatus())
}

// KPUser is the account profile shown in the sidebar (a stable, frontend-facing
// projection of the kino.pub /v1/user response).
type KPUser struct {
	Username           string `json:"username"`
	Avatar             string `json:"avatar,omitempty"`
	SubscriptionActive bool   `json:"subscriptionActive"`
	SubscriptionDays   int    `json:"subscriptionDays"`
	SubscriptionEnd    int64  `json:"subscriptionEnd,omitempty"`
}

// handleKPUser returns the authenticated account's profile and subscription so
// the sidebar can show the username and remaining days.
func (s *Server) handleKPUser(w http.ResponseWriter, r *http.Request) {
	client, ok := s.kpClientOrErr(w)
	if !ok {
		return
	}
	u, err := client.User(r.Context())
	if err != nil {
		s.kpFail(w, err)
		return
	}
	name := u.Username
	if u.Profile.Name != "" {
		name = u.Profile.Name
	}
	days := 0
	if d, derr := u.Subscription.Days.Float64(); derr == nil && d > 0 {
		days = int(d)
	}
	writeJSON(w, http.StatusOK, KPUser{
		Username:           name,
		Avatar:             u.Profile.Avatar,
		SubscriptionActive: u.Subscription.Active,
		SubscriptionDays:   days,
		SubscriptionEnd:    u.Subscription.EndTime,
	})
}

// handleKPLogin starts a device-code login and kicks off background polling.
func (s *Server) handleKPLogin(w http.ResponseWriter, r *http.Request) {
	hc, err := s.kpHTTPClient()
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	client := kinopubapi.New(hc, kinopubapi.Tokens{},
		kinopubapi.WithPersist(persistAPITokens),
		kinopubapi.WithDebug(func(m string) { log.Printf("[kp-login] %s", m) }),
	)

	dc, err := client.RequestDeviceCode(r.Context())
	if err != nil {
		log.Printf("[kp-login] device code request failed: %v", err)
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	log.Printf("[kp-login] device code issued: enter %q at %s (expires in %ds, interval %ds)",
		dc.UserCode, dc.VerificationURI, dc.ExpiresIn, dc.Interval)
	s.startKPPoller(client, dc)

	writeJSON(w, http.StatusOK, KPStatus{
		Pending:         true,
		UserCode:        dc.UserCode,
		VerificationURI: dc.VerificationURI,
		ExpiresAt:       time.Now().Add(time.Duration(maxInt(dc.ExpiresIn, 300)) * time.Second).Unix(),
	})
}

func (s *Server) handleKPLogout(w http.ResponseWriter, r *http.Request) {
	// Bump the logout generation BEFORE clearing tokens so any in-flight client's
	// refresh-persist hook (captured at an older generation) is neutralized and
	// cannot write the rotated tokens back after this logout.
	s.kpLogoutGen.Add(1)
	s.kpMu.Lock()
	if s.kpLogin != nil && s.kpLogin.cancel != nil {
		s.kpLogin.cancel()
	}
	s.kpLogin = nil
	s.kpMu.Unlock()
	if err := clearAPITokens(); err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.invalidateKPClient()
	st := s.kpStatus()
	s.hub.broadcast(Event{Type: "kpauth", Data: st})
	writeJSON(w, http.StatusOK, st)
}

// startKPPoller cancels any prior session and polls for token activation in the
// background, broadcasting a "kpauth" event on completion.
func (s *Server) startKPPoller(client *kinopubapi.Client, dc kinopubapi.DeviceCode) {
	expSecs := maxInt(dc.ExpiresIn, 300)
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(expSecs)*time.Second)

	sess := &kpLoginSession{
		userCode:        dc.UserCode,
		verificationURI: dc.VerificationURI,
		expiresAt:       time.Now().Add(time.Duration(expSecs) * time.Second),
		cancel:          cancel,
	}

	s.kpMu.Lock()
	if s.kpLogin != nil && s.kpLogin.cancel != nil {
		s.kpLogin.cancel()
	}
	s.kpLogin = sess
	s.kpMu.Unlock()

	interval := time.Duration(dc.Interval) * time.Second
	if interval < 3*time.Second {
		interval = 3 * time.Second
	}

	go func() {
		defer cancel()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		polls, transientFails := 0, 0
		for {
			select {
			case <-ctx.Done():
				log.Printf("[kp-login] device code expired/cancelled after %d polls (user_code=%s)", polls, dc.UserCode)
				s.finishKP(sess, ctx.Err())
				return
			case <-ticker.C:
				polls++
				_, err := client.PollDeviceToken(ctx, dc.Code)
				switch {
				case err == nil:
					log.Printf("[kp-login] confirmed after %d polls — registering device", polls)
					_ = client.Notify(ctx, s.kpDeviceInfo()) // name this device (best-effort)
					s.finishKP(sess, nil)
					return
				case errors.Is(err, kinopubapi.ErrAuthorizationPending):
					transientFails = 0
					log.Printf("[kp-login] poll %d: still pending — confirm code %s at %s", polls, dc.UserCode, dc.VerificationURI)
					continue
				case errors.Is(err, kinopubapi.ErrDeviceAuthError):
					// Terminal: kino.pub refused (expired code, access denied,
					// device limit, …) — surface it instead of spinning forever.
					log.Printf("[kp-login] kino.pub refused: %v", err)
					s.finishKP(sess, err)
					return
				default:
					// Transport blip (kino.pub often needs a VPN). Retry a few
					// times so a single timeout doesn't kill the whole login.
					transientFails++
					log.Printf("[kp-login] poll %d: network error (%d/5, retrying): %v", polls, transientFails, err)
					if transientFails >= 5 {
						s.finishKP(sess, err)
						return
					}
					continue
				}
			}
		}
	}()
}

// finishKP marks a login session done (success when err is nil) and broadcasts.
func (s *Server) finishKP(sess *kpLoginSession, err error) {
	s.kpMu.Lock()
	if s.kpLogin == sess {
		sess.done = true
		if err != nil && !errors.Is(err, context.Canceled) {
			sess.err = err
		}
	}
	s.kpMu.Unlock()
	if err == nil {
		// Fresh tokens are on disk now — drop any cached (unauthenticated) client
		// so discovery rebuilds with them.
		s.invalidateKPClient()
	}
	s.hub.broadcast(Event{Type: "kpauth", Data: s.kpStatus()})
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// kpClientOrErr is a small helper for discovery handlers: it resolves the client
// or writes an error response, returning ok=false when the caller should return.
func (s *Server) kpClientOrErr(w http.ResponseWriter) (*kinopubapi.Client, bool) {
	client, err := s.kpClient()
	if err != nil {
		writeErr(w, http.StatusUnauthorized, err.Error())
		return nil, false
	}
	return client, true
}
