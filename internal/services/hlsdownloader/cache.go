package hlsdownloader

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/ZioSHik/kinopub-gui/internal/lib/fsutil"
)

// Cache segments only while their complete playlist identity is unchanged.
// Index-based filenames alone cannot distinguish another quality or voiceover.
func prepareTrackCache(dir string, playlist *MediaPlaylist) error {
	identityPlaylist := *playlist
	identityPlaylist.InitURI = cacheResourceURL(playlist.InitURI)
	identityPlaylist.Segments = append([]Segment(nil), playlist.Segments...)
	for i := range identityPlaylist.Segments {
		identityPlaylist.Segments[i].URL = cacheResourceURL(identityPlaylist.Segments[i].URL)
	}
	data, err := json.Marshal(identityPlaylist)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(data)
	identity := hex.EncodeToString(sum[:])
	marker := filepath.Join(dir, ".playlist")
	old, _ := os.ReadFile(marker)
	if string(old) != identity {
		if err := os.RemoveAll(dir); err != nil {
			return fmt.Errorf("reset segment cache: %w", err)
		}
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	return fsutil.AtomicWrite(marker, []byte(identity), 0600)
}

// The signed /hls/<session>/ prefix authorizes a CDN request; the remaining
// immutable media path identifies its content. A new session must not discard
// valid partial segments for the same video, quality and audio rendition.
func cacheResourceURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || !strings.HasSuffix(strings.ToLower(u.Hostname()), ".cdntogo.net") {
		return raw
	}
	parts := strings.SplitN(u.Path, "/", 4)
	if len(parts) != 4 || parts[1] != "hls" || parts[2] == "" {
		return raw
	}
	u.Path = "/hls/<session>/" + parts[3]
	u.RawPath = ""
	query := u.Query()
	query.Del("loc")
	u.RawQuery = query.Encode()
	return u.String()
}
