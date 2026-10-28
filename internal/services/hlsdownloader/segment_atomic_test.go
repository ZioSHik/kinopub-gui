package hlsdownloader

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSegmentIsVisibleOnlyAfterFullDownload(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "partial")
		w.(http.Flusher).Flush()
		close(started)
		<-release
		fmt.Fprint(w, " complete")
	}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	path := filepath.Join(t.TempDir(), "segment.ts")
	d := newTestDownloader(t, srv.Client())
	done := make(chan error, 1)
	go func() { _, err := d.downloadSegment(ctx, Segment{URL: srv.URL}, path, nil); done <- err }()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("server did not start")
	}
	_, statErr := os.Stat(path)
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if !os.IsNotExist(statErr) {
		t.Fatalf("partial segment was visible as complete: %v", statErr)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "partial complete" {
		t.Fatalf("segment=%q %v", data, err)
	}
}
