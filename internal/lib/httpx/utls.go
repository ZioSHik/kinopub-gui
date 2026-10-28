// Package httpx — utls.go provides an HTTP transport that uses uTLS to
// impersonate a real browser's TLS fingerprint. This bypasses Cloudflare and
// CDN fingerprint-based throttling/blocking that affects Go's default
// crypto/tls implementation.
package httpx

import (
	"bufio"
	"context"
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	utls "github.com/refraction-networking/utls"
	"golang.org/x/net/http2"
	"golang.org/x/net/proxy"
)

// NewBrowserClient creates an HTTP client that impersonates Chrome's TLS
// fingerprint using uTLS. This prevents CDN throttling based on JA3/JA4
// fingerprint detection. Supports both HTTP/1.1 and HTTP/2 (ALPN negotiated).
//
// proxyURL is optional — pass nil for a direct connection.
// The returned client has no global Timeout (caller should use context).
func NewBrowserClient(proxyURL *url.URL) *http.Client {
	return &http.Client{
		Transport: &browserTransport{proxyURL: proxyURL},
		Timeout:   0,
	}
}

// browserTransport implements http.RoundTripper using uTLS for TLS connections.
// It handles both HTTP/1.1 and HTTP/2 based on ALPN negotiation.
type browserTransport struct {
	proxyURL  *url.URL
	mu        sync.Mutex
	h1        *http.Transport
	plain     *http.Transport
	h1Hosts   map[string]bool
	h2Clients map[string]*http2.ClientConn // host → h2 conn
}

func (t *browserTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Scheme != "https" {
		// Plain HTTP uses a pooled transport with the same configured proxy.
		t.mu.Lock()
		if t.plain == nil {
			if defaults, ok := http.DefaultTransport.(*http.Transport); ok {
				t.plain = defaults.Clone()
			} else {
				t.plain = &http.Transport{IdleConnTimeout: 90 * time.Second}
			}
			t.plain.Proxy = http.ProxyURL(t.proxyURL)
		}
		plain := t.plain
		t.mu.Unlock()
		return plain.RoundTrip(req)
	}

	// Reuse an established protocol/connection before opening another TLS socket.
	t.mu.Lock()
	cc, h1Known := t.h2Clients[req.URL.Host], t.h1Hosts[req.URL.Host]
	t.mu.Unlock()
	if cc != nil {
		response, err := cc.RoundTrip(req)
		if err == nil {
			return response, nil
		}
		if req.Context().Err() != nil {
			return nil, err
		}
		t.mu.Lock()
		if t.h2Clients[req.URL.Host] == cc {
			delete(t.h2Clients, req.URL.Host)
		}
		t.mu.Unlock()
		cc.Close()
		if req.Method != http.MethodGet && req.Method != http.MethodHead {
			return nil, err
		}
	}
	if h1Known {
		return t.roundTripH1(req, nil)
	}
	// Establish TLS connection with Chrome fingerprint.
	addr := req.URL.Host
	if !hasPort(addr) {
		addr += ":443"
	}

	ctx := req.Context()
	tlsConn, alpn, err := t.dialTLS(ctx, addr)
	if err != nil {
		return nil, err
	}

	if alpn == "h2" {
		// HTTP/2
		return t.roundTripH2(req, tlsConn, addr)
	}

	// HTTP/1.1
	t.mu.Lock()
	if t.h1Hosts == nil {
		t.h1Hosts = make(map[string]bool)
	}
	t.h1Hosts[req.URL.Host] = true
	t.mu.Unlock()
	return t.roundTripH1(req, tlsConn)
}

// dialProxy opens a TCP connection to addr, routing through the configured
// proxy if one is set. Supports SOCKS5 and HTTP/HTTPS CONNECT proxies.
func (t *browserTransport) dialProxy(ctx context.Context, addr string) (net.Conn, error) {
	baseDialer := newDialer()

	if t.proxyURL == nil {
		return baseDialer.DialContext(ctx, "tcp", addr)
	}

	switch t.proxyURL.Scheme {
	case "socks5":
		var auth *proxy.Auth
		if t.proxyURL.User != nil {
			auth = &proxy.Auth{User: t.proxyURL.User.Username()}
			if pass, ok := t.proxyURL.User.Password(); ok {
				auth.Password = pass
			}
		}
		socksDialer, err := proxy.SOCKS5("tcp", t.proxyURL.Host, auth, baseDialer)
		if err != nil {
			return nil, fmt.Errorf("socks5 dialer: %w", err)
		}
		if cd, ok := socksDialer.(proxy.ContextDialer); ok {
			return cd.DialContext(ctx, "tcp", addr)
		}
		return socksDialer.Dial("tcp", addr)

	case "http", "https":
		proxyAddr := t.proxyURL.Host
		if t.proxyURL.Port() == "" {
			port := "80"
			if t.proxyURL.Scheme == "https" {
				port = "443"
			}
			proxyAddr = net.JoinHostPort(t.proxyURL.Hostname(), port)
		}
		conn, err := baseDialer.DialContext(ctx, "tcp", proxyAddr)
		if err != nil {
			return nil, fmt.Errorf("proxy dial: %w", err)
		}
		success := false
		defer func() {
			if !success {
				conn.Close()
			}
		}()
		rawConn := conn
		stop := context.AfterFunc(ctx, func() { rawConn.Close() })
		defer stop()
		deadline := time.Now().Add(30 * time.Second)
		if end, ok := ctx.Deadline(); ok && end.Before(deadline) {
			deadline = end
		}
		conn.SetDeadline(deadline)
		if t.proxyURL.Scheme == "https" {
			secure := tls.Client(conn, &tls.Config{ServerName: t.proxyURL.Hostname(), MinVersion: tls.VersionTLS12})
			if err := secure.HandshakeContext(ctx); err != nil {
				return nil, fmt.Errorf("proxy TLS: %w", err)
			}
			conn = secure
		}
		request, _ := http.NewRequestWithContext(ctx, http.MethodConnect, "http://"+addr, nil)
		request.Host = addr
		if t.proxyURL.User != nil {
			password, _ := t.proxyURL.User.Password()
			credentials := t.proxyURL.User.Username() + ":" + password
			request.Header.Set("Proxy-Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(credentials)))
		}
		if err := request.Write(conn); err != nil {
			return nil, fmt.Errorf("proxy CONNECT write: %w", err)
		}
		reader := bufio.NewReader(conn)
		response, err := http.ReadResponse(reader, request)
		if err != nil {
			return nil, fmt.Errorf("proxy CONNECT read: %w", err)
		}
		if response.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("proxy CONNECT failed: HTTP %d", response.StatusCode)
		}
		conn.SetDeadline(time.Time{})
		success = true
		return &bufferedConn{Conn: conn, reader: reader}, nil
	default:
		return nil, fmt.Errorf("unsupported proxy scheme: %s", t.proxyURL.Scheme)
	}
}

type bufferedConn struct {
	net.Conn
	reader *bufio.Reader
}

func (c *bufferedConn) Read(p []byte) (int, error) { return c.reader.Read(p) }

func (t *browserTransport) dialTLS(ctx context.Context, addr string, http1Only ...bool) (net.Conn, string, error) {
	conn, err := t.dialProxy(ctx, addr)
	if err != nil {
		return nil, "", err
	}

	host, _, _ := net.SplitHostPort(addr)

	tlsConn := utls.UClient(conn, &utls.Config{
		ServerName: host,
	}, utls.HelloChrome_120)

	if len(http1Only) > 0 && http1Only[0] {
		if err := tlsConn.BuildHandshakeState(); err != nil {
			conn.Close()
			return nil, "", err
		}
		for _, extension := range tlsConn.Extensions {
			if alpn, ok := extension.(*utls.ALPNExtension); ok {
				alpn.AlpnProtocols = []string{"http/1.1"}
			}
		}
	}
	if err := tlsConn.HandshakeContext(ctx); err != nil {
		conn.Close()
		return nil, "", err
	}

	alpn := tlsConn.ConnectionState().NegotiatedProtocol
	return tlsConn, alpn, nil
}

func (t *browserTransport) roundTripH2(req *http.Request, conn net.Conn, addr string) (*http.Response, error) {
	t.mu.Lock()
	if t.h2Clients == nil {
		t.h2Clients = make(map[string]*http2.ClientConn)
	}

	// Check for existing h2 connection to this host.
	host := req.URL.Host
	if cc, ok := t.h2Clients[host]; ok {
		t.mu.Unlock()
		// Try existing connection first.
		resp, err := cc.RoundTrip(req)
		if err == nil {
			conn.Close() // don't need the new connection
			return resp, nil
		}
		// Connection stale — remove and use new one.
		t.mu.Lock()
		if t.h2Clients[host] == cc {
			delete(t.h2Clients, host)
		}
		t.mu.Unlock()
		cc.Close()
		if req.Context().Err() != nil || (req.Method != http.MethodGet && req.Method != http.MethodHead) {
			conn.Close()
			return nil, err
		}
	} else {
		t.mu.Unlock()
	}

	// Create new HTTP/2 client connection.
	tr := &http2.Transport{IdleConnTimeout: 90 * time.Second}
	cc, err := tr.NewClientConn(conn)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("h2 client conn: %w", err)
	}

	t.mu.Lock()
	t.h2Clients[host] = cc
	t.mu.Unlock()

	return cc.RoundTrip(req)
}

func (t *browserTransport) roundTripH1(req *http.Request, conn net.Conn) (*http.Response, error) {
	// The pre-established conn can't be easily injected into http.Transport,
	// so we close it and re-dial via DialTLSContext — this preserves both
	// proxy routing and the uTLS Chrome fingerprint for H1 connections.
	if conn != nil {
		conn.Close()
	}

	t.mu.Lock()
	if t.h1 == nil {
		self := t // capture for closure
		t.h1 = &http.Transport{
			MaxIdleConns:        100,
			IdleConnTimeout:     90 * time.Second,
			MaxIdleConnsPerHost: 10,
			DialTLSContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				tlsConn, _, err := self.dialTLS(ctx, addr, true)
				return tlsConn, err
			},
		}
	}
	h1 := t.h1
	t.mu.Unlock()

	return h1.RoundTrip(req)
}

func hasPort(addr string) bool {
	_, _, err := net.SplitHostPort(addr)
	return err == nil
}

// WrapWithBrowserTLS wraps an existing client with browser TLS fingerprinting,
// preserving the proxy URL from the original transport if available.
func WrapWithBrowserTLS(client *http.Client) *http.Client {
	var proxyURL *url.URL
	if t, ok := client.Transport.(*http.Transport); ok && t != nil {
		if t.Proxy != nil {
			proxyURL, _ = t.Proxy(&http.Request{URL: &url.URL{Scheme: "https"}})
		}
	}
	wrapped := *client
	wrapped.Transport = &browserTransport{proxyURL: proxyURL}
	return &wrapped
}
