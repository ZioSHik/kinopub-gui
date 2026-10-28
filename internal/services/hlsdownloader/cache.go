package hlsdownloader

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ZioSHik/kinopub-gui/internal/lib/fsutil"
)

// Cache segments only while their complete playlist identity is unchanged.
// Index-based filenames alone cannot distinguish another quality or voiceover.
func prepareTrackCache(dir string, playlist *MediaPlaylist) error {
	data, err := json.Marshal(playlist)
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
