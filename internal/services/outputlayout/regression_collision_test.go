package outputlayout

import (
	"github.com/ZioSHik/kinopub-gui/internal/domain"
	"testing"
)

func TestDifferentTitlesCannotOverwriteEachOther(t *testing.T) {
	l := New(domain.ContainerMKV)
	root := t.TempDir()
	a, _ := l.EpisodePath(root, domain.Series{ID: "42", Title: "Same title"}, domain.Episode{Key: domain.EpisodeKey{Series: "42", Season: 1, Episode: 1}})
	b, _ := l.EpisodePath(root, domain.Series{ID: "43", Title: "Same title"}, domain.Episode{Key: domain.EpisodeKey{Series: "43", Season: 1, Episode: 1}})
	if a == b {
		t.Fatalf("different catalog IDs write the same file and state directory: %s", a)
	}
}
