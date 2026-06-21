// Command kinopub-gui serves a self-contained web interface for the kinopub
// downloader. It reuses the same Go engine as the CLI and streams real progress
// to the browser. Run it and a browser tab opens automatically.
package main

import (
	"context"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"syscall"
	"time"

	"github.com/niazlv/kinopub-downloader/internal/gui"
	"github.com/niazlv/kinopub-downloader/internal/lib/termx"
	"github.com/niazlv/kinopub-downloader/web"
)

var version = "dev"

func main() {
	os.Exit(run())
}

func run() int {
	var (
		addr        string
		noOpen      bool
		showVersion bool
	)
	flag.StringVar(&addr, "addr", "127.0.0.1:8765", "address to listen on (host:port)")
	flag.BoolVar(&noOpen, "no-open", false, "do not open the browser automatically")
	flag.BoolVar(&showVersion, "version", false, "print version and exit")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "kinopub-gui %s — web interface for the kinopub downloader\n\n", version)
		fmt.Fprintf(os.Stderr, "Usage:\n  kinopub-gui [flags]\n\nFlags:\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	if showVersion {
		fmt.Printf("kinopub-gui %s\n", version)
		return 0
	}

	srv := gui.NewServer(version, web.Dist())

	// Bind the listener up front so we know the final address (and can fall
	// back to an ephemeral port if the preferred one is taken).
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		host, _, _ := net.SplitHostPort(addr)
		if host == "" {
			host = "127.0.0.1"
		}
		fmt.Fprintf(os.Stderr, "kinopub-gui: %s is unavailable (%v); using an ephemeral port\n", addr, err)
		ln, err = net.Listen("tcp", net.JoinHostPort(host, "0"))
		if err != nil {
			fmt.Fprintf(os.Stderr, "kinopub-gui: cannot listen: %v\n", err)
			return 1
		}
	}

	url := "http://" + ln.Addr().String()
	httpSrv := &http.Server{
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	banner(url)

	errCh := make(chan error, 1)
	go func() { errCh <- httpSrv.Serve(ln) }()

	if !noOpen {
		go func() {
			time.Sleep(300 * time.Millisecond)
			openBrowser(url)
		}()
	}

	// Graceful shutdown on Ctrl-C.
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	select {
	case err := <-errCh:
		if err != nil && err != http.ErrServerClosed {
			fmt.Fprintf(os.Stderr, "kinopub-gui: server error: %v\n", err)
			return 1
		}
	case <-sigCh:
		fmt.Fprintln(os.Stderr, "\nkinopub-gui: shutting down…")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(ctx)
	}
	return 0
}

func banner(url string) {
	reset, bold, cyan, dim := "", "", "", ""
	if useColor(os.Stdout) {
		reset, bold, cyan, dim = "\033[0m", "\033[1m", "\033[36m", "\033[2m"
	}
	fmt.Printf("\n  %s%skinopub%s %sweb interface%s\n", bold, cyan, reset, dim, reset)
	fmt.Printf("  %s▸%s  %s%s%s\n", cyan, reset, bold, url, reset)
	fmt.Printf("  %sPress Ctrl-C to stop%s\n\n", dim, reset)
}

// useColor reports whether ANSI colors should be emitted to f: only when it is a
// real terminal and NO_COLOR is unset. On Windows it best-effort enables virtual
// terminal processing so modern consoles render the escapes instead of garbage.
func useColor(f *os.File) bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	if !termx.IsTTY(f) {
		return false
	}
	return enableVT(f)
}

// openBrowser opens the given URL in the default browser, best-effort.
func openBrowser(url string) {
	var cmd string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		cmd = "open"
		args = []string{url}
	case "windows":
		cmd = "rundll32"
		args = []string{"url.dll,FileProtocolHandler", url}
	default: // linux, bsd, …
		cmd = "xdg-open"
		args = []string{url}
	}
	_ = exec.Command(cmd, args...).Start()
}
