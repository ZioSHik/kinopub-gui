package httpx

import (
	"bufio"
	"context"
	"golang.org/x/net/http2"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestHasPort(t *testing.T) {
	tests := []struct {
		name string
		addr string
		want bool
	}{
		{"host with port", "example.com:443", true},
		{"host without port", "example.com", false},
		{"ipv4 with port", "1.2.3.4:80", true},
		{"ipv4 without port", "1.2.3.4", false},
		{"ipv6 bracketed with port", "[::1]:443", true},
		{"ipv6 bare no port", "::1", false},
		{"empty", "", false},
		{"trailing colon counts as port", "example.com:", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hasPort(tt.addr); got != tt.want {
				t.Errorf("hasPort(%q) = %v, want %v", tt.addr, got, tt.want)
			}
		})
	}
}

func TestNewBrowserClient_DirectConfig(t *testing.T) {
	c := NewBrowserClient(nil)
	if c == nil {
		t.Fatal("nil client")
	}
	if c.Timeout != 0 {
		t.Errorf("Timeout = %v, want 0", c.Timeout)
	}
	bt, ok := c.Transport.(*browserTransport)
	if !ok {
		t.Fatalf("Transport = %T, want *browserTransport", c.Transport)
	}
	if bt.proxyURL != nil {
		t.Errorf("proxyURL = %v, want nil", bt.proxyURL)
	}
}

func TestNewBrowserClient_WithProxy(t *testing.T) {
	pu, _ := url.Parse("socks5://user:pass@127.0.0.1:1080")
	c := NewBrowserClient(pu)
	bt := c.Transport.(*browserTransport)
	if bt.proxyURL != pu {
		t.Errorf("proxyURL = %v, want %v", bt.proxyURL, pu)
	}
}

func TestWrapWithBrowserTLS_ExtractsProxy(t *testing.T) {
	pu, _ := url.Parse("http://proxy.local:3128")
	orig := &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyURL(pu),
		},
		Timeout: 5,
	}
	wrapped := WrapWithBrowserTLS(orig)
	if wrapped == orig {
		t.Fatal("expected a copied client")
	}
	if wrapped.Timeout != orig.Timeout {
		t.Errorf("Timeout = %v, want preserved %v", wrapped.Timeout, orig.Timeout)
	}
	bt, ok := wrapped.Transport.(*browserTransport)
	if !ok {
		t.Fatalf("Transport = %T, want *browserTransport", wrapped.Transport)
	}
	if bt.proxyURL == nil || bt.proxyURL.Host != "proxy.local:3128" {
		t.Errorf("proxyURL = %v, want proxy.local:3128", bt.proxyURL)
	}
}

func TestWrapWithBrowserTLS_NoProxy(t *testing.T) {
	orig := &http.Client{Transport: &http.Transport{}}
	wrapped := WrapWithBrowserTLS(orig)
	bt := wrapped.Transport.(*browserTransport)
	if bt.proxyURL != nil {
		t.Errorf("proxyURL = %v, want nil when transport has no Proxy func", bt.proxyURL)
	}
}

func TestWrapWithBrowserTLS_NonHTTPTransport(t *testing.T) {
	// A non-*http.Transport base (e.g. our own authTransport) should not panic;
	// proxyURL stays nil.
	orig := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
		return nil, nil
	})}
	wrapped := WrapWithBrowserTLS(orig)
	bt := wrapped.Transport.(*browserTransport)
	if bt.proxyURL != nil {
		t.Errorf("proxyURL = %v, want nil for non-http.Transport base", bt.proxyURL)
	}
}

func TestWrapWithBrowserTLS_NilTransport(t *testing.T) {
	// client.Transport == nil: the type assertion fails gracefully.
	orig := &http.Client{}
	wrapped := WrapWithBrowserTLS(orig)
	bt := wrapped.Transport.(*browserTransport)
	if bt.proxyURL != nil {
		t.Errorf("proxyURL = %v, want nil", bt.proxyURL)
	}
}

// TestBrowserTransport_PlainHTTP verifies the http (non-https) fast path goes
// through the default transport unchanged — no uTLS involved.
func TestBrowserTransport_PlainHTTP(t *testing.T) {
	var hit bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
		w.Write([]byte("ok"))
	}))
	defer srv.Close()

	bt := &browserTransport{}
	req, _ := http.NewRequest(http.MethodGet, srv.URL, nil)
	resp, err := bt.RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip: %v", err)
	}
	resp.Body.Close()
	if !hit {
		t.Error("plain-HTTP request did not reach the server")
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

// TestDialProxy_Direct verifies dialProxy with no proxy connects straight to the
// target address.
func TestDialProxy_Direct(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		c, err := ln.Accept()
		if err == nil {
			c.Close()
		}
	}()

	bt := &browserTransport{}
	conn, err := bt.dialProxy(context.Background(), ln.Addr().String())
	if err != nil {
		t.Fatalf("dialProxy direct: %v", err)
	}
	conn.Close()
}

// fakeConnectProxy starts a TCP listener that speaks just enough HTTP CONNECT to
// exercise browserTransport.dialProxy. status is the raw status line to send.
// If tunnel is true, after the response it echoes a sentinel so the test can
// confirm the returned conn is the live tunnel.
func fakeConnectProxy(t *testing.T, status string, tunnel bool, capture *string) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		br := bufio.NewReader(conn)
		// Read request line + headers until blank line.
		var b strings.Builder
		for {
			line, err := br.ReadString('\n')
			if err != nil {
				return
			}
			b.WriteString(line)
			if line == "\r\n" || line == "\n" {
				break
			}
		}
		if capture != nil {
			*capture = b.String()
		}
		conn.Write([]byte(status))
		if tunnel {
			conn.Write([]byte("SENTINEL"))
			// keep open briefly
			io := make([]byte, 1)
			conn.Read(io)
		}
	}()
	return ln.Addr().String()
}

func TestDialProxy_HTTPConnectSuccess(t *testing.T) {
	var captured string
	proxyAddr := fakeConnectProxy(t, "HTTP/1.1 200 Connection established\r\n\r\n", true, &captured)
	pu, _ := url.Parse("http://" + proxyAddr)
	bt := &browserTransport{proxyURL: pu}

	conn, err := bt.dialProxy(context.Background(), "target.example.com:443")
	if err != nil {
		t.Fatalf("dialProxy CONNECT: %v", err)
	}
	defer conn.Close()

	if !strings.HasPrefix(captured, "CONNECT http://target.example.com:443") &&
		!strings.HasPrefix(captured, "CONNECT target.example.com:443") {
		t.Errorf("CONNECT request line unexpected: %q", captured)
	}
	if !strings.Contains(captured, "Host: target.example.com:443") {
		t.Errorf("expected Host header for target, got: %q", captured)
	}
}

func TestDialProxy_HTTPConnectWithAuth(t *testing.T) {
	var captured string
	proxyAddr := fakeConnectProxy(t, "HTTP/1.1 200 OK\r\n\r\n", true, &captured)
	pu, _ := url.Parse("http://user:secret@" + proxyAddr)
	bt := &browserTransport{proxyURL: pu}

	conn, err := bt.dialProxy(context.Background(), "host:443")
	if err != nil {
		t.Fatalf("dialProxy CONNECT auth: %v", err)
	}
	defer conn.Close()
	if !strings.Contains(captured, "Proxy-Authorization: Basic ") || strings.Contains(captured, "\r\nAuthorization:") {
		t.Errorf("expected basic auth header, got: %q", captured)
	}
}

func TestDialProxy_HTTPConnectFailure(t *testing.T) {
	proxyAddr := fakeConnectProxy(t, "HTTP/1.1 403 Forbidden\r\n\r\n", false, nil)
	pu, _ := url.Parse("http://" + proxyAddr)
	bt := &browserTransport{proxyURL: pu}

	conn, err := bt.dialProxy(context.Background(), "host:443")
	if err == nil {
		conn.Close()
		t.Fatal("expected error on non-200 CONNECT response")
	}
	if !strings.Contains(err.Error(), "CONNECT failed") {
		t.Errorf("error = %v, want CONNECT failed", err)
	}
}

func TestDialProxy_ProxyDialError(t *testing.T) {
	// Point at a closed port to force a dial failure.
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := ln.Addr().String()
	ln.Close() // now nothing is listening

	pu, _ := url.Parse("http://" + addr)
	bt := &browserTransport{proxyURL: pu}
	_, err := bt.dialProxy(context.Background(), "host:443")
	if err == nil {
		t.Fatal("expected dial error")
	}
	if !strings.Contains(err.Error(), "proxy dial") {
		t.Errorf("error = %v, want proxy dial wrap", err)
	}
}

func TestDialProxy_SOCKS5DialerConstruction(t *testing.T) {
	// We cannot run a full SOCKS5 handshake without a server, but we can verify
	// dialProxy attempts a connection (and fails to a dead port) rather than
	// erroring on dialer construction.
	pu, _ := url.Parse("socks5://127.0.0.1:1") // port 1: connection refused
	bt := &browserTransport{proxyURL: pu}
	_, err := bt.dialProxy(context.Background(), "host:443")
	if err == nil {
		t.Fatal("expected an error dialing through dead SOCKS5 proxy")
	}
	// The error should come from the dial attempt, not "socks5 dialer:".
	if strings.Contains(err.Error(), "socks5 dialer:") {
		t.Errorf("unexpected dialer construction error: %v", err)
	}
}

func TestBrowserTransportReusesExistingH2WithoutAnotherTLSHandshake(t *testing.T) {
	client, server := net.Pipe()
	go (&http2.Server{}).ServeConn(server, &http2.ServeConnOpts{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("reused")) })})
	cc, err := (&http2.Transport{}).NewClientConn(client)
	if err != nil {
		t.Fatal(err)
	}
	defer cc.Close()
	bt := &browserTransport{h2Clients: map[string]*http2.ClientConn{"cached.invalid": cc}}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", "https://cached.invalid/segment", nil)
	resp, err := bt.RoundTrip(req)
	if err != nil {
		t.Fatalf("cached H2 attempted a fresh network handshake: %v", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil || string(data) != "reused" {
		t.Fatalf("response=%q err=%v", data, err)
	}
}

func TestBrowserTransportReusesKnownHTTP1Pool(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "pooled")
	}))
	defer server.Close()
	u, _ := url.Parse(server.URL)
	pool := server.Client().Transport.(*http.Transport)
	defer pool.CloseIdleConnections()
	bt := &browserTransport{h1: pool, h1Hosts: map[string]bool{u.Host: true}}
	for range 2 {
		req, _ := http.NewRequest(http.MethodGet, server.URL, nil)
		response, err := bt.RoundTrip(req)
		if err != nil {
			t.Fatalf("known HTTP/1 host did not use its pool: %v", err)
		}
		data, err := io.ReadAll(response.Body)
		response.Body.Close()
		if err != nil || string(data) != "pooled" || response.ProtoMajor != 1 {
			t.Fatalf("response=%q protocol=%s error=%v", data, response.Proto, err)
		}
	}
}

func TestBrowserTransportPlainHTTPUsesConfiguredProxy(t *testing.T) {
	seen := make(chan string, 1)
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { seen <- r.URL.Host; w.Write([]byte("proxied")) }))
	defer proxy.Close()
	proxyURL, _ := url.Parse(proxy.URL)
	client := NewBrowserClient(proxyURL)
	resp, err := client.Get("http://target.invalid/segment.ts")
	if err != nil {
		t.Fatalf("HTTP request bypassed its proxy: %v", err)
	}
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
	if host := <-seen; host != "target.invalid" {
		t.Fatalf("proxy target=%q", host)
	}
}

func TestConnectPreservesBufferedTunnelBytes(t *testing.T) {
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, _, err := w.(http.Hijacker).Hijack()
		if err != nil {
			return
		}
		defer conn.Close()
		conn.Write([]byte("HTTP/1.1 200 Connection established\r\n\r\nSENTINEL"))
	}))
	defer proxy.Close()
	proxyURL, _ := url.Parse(proxy.URL)
	bt := &browserTransport{proxyURL: proxyURL}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	conn, err := bt.dialProxy(ctx, "origin.example:443")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	buf := make([]byte, 8)
	if _, err := io.ReadFull(conn, buf); err != nil || string(buf) != "SENTINEL" {
		t.Fatalf("tunnel bytes lost: %q %v", buf, err)
	}
}

func TestConnectReadHonorsCancellation(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	accepted := make(chan net.Conn, 1)
	go func() {
		conn, err := listener.Accept()
		if err == nil {
			accepted <- conn
		}
	}()
	proxyURL, _ := url.Parse("http://" + listener.Addr().String())
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err = (&browserTransport{proxyURL: proxyURL}).dialProxy(ctx, "origin.example:443")
	conn := <-accepted
	conn.Close()
	if err == nil || time.Since(start) > time.Second {
		t.Fatalf("CONNECT ignored cancellation: %v elapsed=%s", err, time.Since(start))
	}
}
