// Package httpx — utls.go provides an HTTP transport that uses uTLS to
// impersonate a real browser's TLS fingerprint. This bypasses Cloudflare and
// CDN fingerprint-based throttling/blocking that affects Go's default
// crypto/tls implementation.
package httpx

import (
	"context"
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
	h2Clients map[string]*http2.ClientConn // host → h2 conn
}

func (t *browserTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Scheme != "https" {
		// Plain HTTP — use default transport.
		return http.DefaultTransport.RoundTrip(req)
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
		socksDialer, err := proxy.SOCKS5("tcp", t.proxyURL.Host, auth, proxy.Direct)
		if err != nil {
			return nil, fmt.Errorf("socks5 dialer: %w", err)
		}
		if cd, ok := socksDialer.(proxy.ContextDialer); ok {
			return cd.DialContext(ctx, "tcp", addr)
		}
		return socksDialer.Dial("tcp", addr)

	default:
		// HTTP/HTTPS proxy: dial proxy, then send CONNECT.
		conn, err := baseDialer.DialContext(ctx, "tcp", t.proxyURL.Host)
		if err != nil {
			return nil, fmt.Errorf("proxy dial: %w", err)
		}
		req, _ := http.NewRequestWithContext(ctx, http.MethodConnect, "http://"+addr, nil)
		req.Host = addr
		if t.proxyURL.User != nil {
			req.SetBasicAuth(t.proxyURL.User.Username(), func() string {
				p, _ := t.proxyURL.User.Password()
				return p
			}())
		}
		if err := req.Write(conn); err != nil {
			conn.Close()
			return nil, fmt.Errorf("proxy CONNECT write: %w", err)
		}
		// Read response without buffering — we need the raw conn afterwards.
		buf := make([]byte, 4096)
		n, err := conn.Read(buf)
		if err != nil {
			conn.Close()
			return nil, fmt.Errorf("proxy CONNECT read: %w", err)
		}
		if n < 12 || string(buf[9:12]) != "200" {
			conn.Close()
			return nil, fmt.Errorf("proxy CONNECT failed: %s", string(buf[:n]))
		}
		return conn, nil
	}
}

func (t *browserTransport) dialTLS(ctx context.Context, addr string) (net.Conn, string, error) {
	conn, err := t.dialProxy(ctx, addr)
	if err != nil {
		return nil, "", err
	}

	host, _, _ := net.SplitHostPort(addr)

	tlsConn := utls.UClient(conn, &utls.Config{
		ServerName: host,
	}, utls.HelloChrome_120)

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
		delete(t.h2Clients, host)
		t.mu.Unlock()
	} else {
		t.mu.Unlock()
	}

	// Create new HTTP/2 client connection.
	tr := &http2.Transport{}
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
	conn.Close()

	t.mu.Lock()
	if t.h1 == nil {
		self := t // capture for closure
		t.h1 = &http.Transport{
			MaxIdleConns:        100,
			IdleConnTimeout:     90 * time.Second,
			MaxIdleConnsPerHost: 10,
			DialTLSContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
				tlsConn, _, err := self.dialTLS(ctx, addr)
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
