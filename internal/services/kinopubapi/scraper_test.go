package kinopubapi

import (
	"encoding/json"
	"testing"
)

func TestItemIDFromURL(t *testing.T) {
	cases := map[string]string{
		"https://kino.pub/item/view/38290":      "38290",
		"https://kino.pub/item/view/38290/s1e2": "38290",
		"38290":                                 "38290",
		"http://kino.pub/item/view/7/season/2":  "7",
		"":                                      "",
		"https://kino.pub/movies":               "",
	}
	for in, want := range cases {
		if got := ItemIDFromURL(in); got != want {
			t.Errorf("ItemIDFromURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFileURLManifestPrefersHLS4(t *testing.T) {
	u := FileURL{HLS: "a", HLS2: "b", HLS4: "c"}
	if got := u.Manifest(); got != "c" {
		t.Fatalf("Manifest() = %q, want hls4 'c'", got)
	}
	if got := (FileURL{HLS: "a", HLS2: "b"}).Manifest(); got != "b" {
		t.Fatalf("Manifest() fallback = %q, want hls2 'b'", got)
	}
	if got := (FileURL{HLS: "a"}).Manifest(); got != "a" {
		t.Fatalf("Manifest() fallback = %q, want hls 'a'", got)
	}
}

func TestBuildPagePlaylistSerial(t *testing.T) {
	item := Item{
		ID:      json.Number("100"),
		Type:    "serial",
		Title:   "Test Serial",
		Posters: Posters{Big: "poster.jpg"},
		Seasons: []Season{
			{Number: 1, Episodes: []Episode{
				{Number: 1, Title: "Pilot", Duration: 2700, Files: []File{
					{Quality: "720p", H: 720, URL: FileURL{HLS4: "s1e1-720"}},
					{Quality: "1080p", H: 1080, URL: FileURL{HLS4: "s1e1-1080"}},
				}},
				{Number: 2, Duration: 2600, Files: []File{
					{Quality: "1080p", H: 1080, URL: FileURL{HLS4: "s1e2-1080"}},
				}},
			}},
			{Number: 2, Episodes: []Episode{
				{Number: 1, Title: "S2", Files: []File{
					{Quality: "1080p", H: 1080, URL: FileURL{HLS4: "s2e1-1080"}},
				}},
			}},
		},
	}
	pl, err := BuildPagePlaylist(item)
	if err != nil {
		t.Fatalf("BuildPagePlaylist: %v", err)
	}
	if pl.ItemID != 100 || pl.Title != "Test Serial" || pl.Poster != "poster.jpg" {
		t.Fatalf("playlist meta wrong: %+v", pl)
	}
	if len(pl.Episodes) != 3 {
		t.Fatalf("got %d episodes, want 3", len(pl.Episodes))
	}
	// Highest-resolution file's manifest should win.
	if pl.Episodes[0].ManifestURL != "s1e1-1080" {
		t.Errorf("ep1 manifest = %q, want s1e1-1080 (highest res)", pl.Episodes[0].ManifestURL)
	}
	if pl.Episodes[0].EpisodeTitle != "Pilot" || pl.Episodes[0].Season != 1 || pl.Episodes[0].Episode != 1 {
		t.Errorf("ep1 fields wrong: %+v", pl.Episodes[0])
	}
	// Missing title falls back to "Серия N".
	if pl.Episodes[1].EpisodeTitle != "Серия 2" {
		t.Errorf("ep2 title = %q, want 'Серия 2'", pl.Episodes[1].EpisodeTitle)
	}
	if len(pl.Seasons) != 2 {
		t.Errorf("got %d seasons, want 2", len(pl.Seasons))
	}
}

func TestBuildPagePlaylistMovie(t *testing.T) {
	item := Item{
		ID:    json.Number("7"),
		Type:  "movie",
		Title: "Test Movie",
		Videos: []Video{
			{Number: 1, Duration: 5400, Files: []File{
				{Quality: "1080p", H: 1080, URL: FileURL{HLS4: "movie-1080"}},
			}},
		},
	}
	pl, err := BuildPagePlaylist(item)
	if err != nil {
		t.Fatalf("BuildPagePlaylist: %v", err)
	}
	if len(pl.Episodes) != 1 {
		t.Fatalf("got %d episodes, want 1", len(pl.Episodes))
	}
	ep := pl.Episodes[0]
	if ep.Season != 1 || ep.Episode != 1 || ep.ManifestURL != "movie-1080" {
		t.Errorf("movie episode wrong: %+v", ep)
	}
	if ep.EpisodeTitle != "Test Movie" {
		t.Errorf("movie episode title = %q, want 'Test Movie'", ep.EpisodeTitle)
	}
}

func TestBuildPagePlaylistNoFiles(t *testing.T) {
	item := Item{ID: json.Number("9"), Title: "Locked", Videos: []Video{{Number: 1}}}
	if _, err := BuildPagePlaylist(item); err == nil {
		t.Fatal("expected error for item with no playable files")
	}
}
