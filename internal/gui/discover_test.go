package gui

import (
	"encoding/json"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/ZioSHik/kinopub-gui/internal/services/kinopubapi"
)

func TestSplitTitle(t *testing.T) {
	cases := []struct {
		in, title, orig string
	}{
		{"Игра престолов / Game of Thrones", "Игра престолов", "Game of Thrones"},
		{"No separator here", "No separator here", ""},
		{" / OnlyOriginal", " / OnlyOriginal", ""}, // i==0 → not split (leading sep)
		{"A / B / C", "A", "B / C"},                // splits on first " / "
		{"", "", ""},
	}
	for _, c := range cases {
		title, orig := splitTitle(c.in)
		if title != c.title || orig != c.orig {
			t.Errorf("splitTitle(%q) = (%q, %q), want (%q, %q)", c.in, title, orig, c.title, c.orig)
		}
	}
}

func TestTitleNames(t *testing.T) {
	in := []kinopubapi.NamedID{
		{Title: "Драма"},
		{Title: ""}, // skipped
		{Title: "Комедия"},
	}
	got := titleNames(in)
	want := []string{"Драма", "Комедия"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("titleNames = %v, want %v", got, want)
	}
	// Empty input yields a non-nil empty slice.
	if got := titleNames(nil); got == nil || len(got) != 0 {
		t.Errorf("titleNames(nil) = %v, want empty non-nil", got)
	}
}

func TestToDiscoverItem(t *testing.T) {
	it := kinopubapi.Item{
		ID:            json.Number("123"),
		Type:          "serial",
		Title:         "Рик и Морти / Rick and Morty",
		Year:          2013,
		Posters:       kinopubapi.Posters{Big: "big.jpg", Medium: "med.jpg"},
		Director:      "Justin Roiland",
		RatingPercent: 95, // → 9.5
		IMDBRating:    9.1,
		KinopoiskRate: 9.0,
		Genres:        []kinopubapi.NamedID{{Title: "Анимация"}},
		Seasons:       []kinopubapi.Season{{Number: 1}},
		Note:          "S1E2",
		WatchedAt:     1700000000,
		HistSeason:    1,
		HistEpisode:   2,
	}
	got := toDiscoverItem(it)
	if got.ID != "123" {
		t.Errorf("ID = %q", got.ID)
	}
	if got.Title != "Рик и Морти" || got.OriginalTitle != "Rick and Morty" {
		t.Errorf("title split wrong: %q / %q", got.Title, got.OriginalTitle)
	}
	if got.Rating != 9.5 {
		t.Errorf("Rating = %v, want 9.5 (95/10)", got.Rating)
	}
	if got.Poster != "big.jpg" {
		t.Errorf("Poster = %q, want big.jpg", got.Poster)
	}
	if !got.IsSerial {
		t.Error("expected IsSerial true (has seasons)")
	}
	if got.Subtitle != "S1E2" || got.WatchedAt != 1700000000 || got.Season != 1 || got.Episode != 2 {
		t.Errorf("history fields wrong: %+v", got)
	}
}

func TestToDiscoverItem_SubnameOverridesOriginal(t *testing.T) {
	it := kinopubapi.Item{
		ID:      json.Number("1"),
		Title:   "Фильм / FilmOriginal",
		Subname: "Explicit Subname",
		Type:    "movie",
	}
	got := toDiscoverItem(it)
	if got.OriginalTitle != "Explicit Subname" {
		t.Errorf("Subname should override split original: %q", got.OriginalTitle)
	}
	if got.IsSerial {
		t.Error("movie should not be a serial")
	}
}

func TestToDiscoverItem_IsSerialByType(t *testing.T) {
	for _, typ := range []string{"serial", "docuserial", "tvshow"} {
		it := kinopubapi.Item{ID: json.Number("1"), Type: typ}
		if !toDiscoverItem(it).IsSerial {
			t.Errorf("type %q should be a serial", typ)
		}
	}
	for _, typ := range []string{"movie", "documovie", "4k", "concert"} {
		it := kinopubapi.Item{ID: json.Number("1"), Type: typ}
		if toDiscoverItem(it).IsSerial {
			t.Errorf("type %q should NOT be a serial", typ)
		}
	}
}

func TestAudioIsSurround(t *testing.T) {
	cases := []struct {
		a    kinopubapi.Audio
		want bool
	}{
		{kinopubapi.Audio{Codec: "ac3"}, true},
		{kinopubapi.Audio{Codec: "AC3"}, true}, // case-insensitive
		{kinopubapi.Audio{Codec: " eac3 "}, true},
		{kinopubapi.Audio{Codec: "dts-hd"}, true},
		{kinopubapi.Audio{Codec: "aac", Channels: 6}, true}, // >=6 channels → surround
		{kinopubapi.Audio{Codec: "aac", Channels: 8}, true},
		{kinopubapi.Audio{Codec: "aac", Channels: 2}, false},
		{kinopubapi.Audio{Codec: "aac"}, false},
		{kinopubapi.Audio{}, false},
	}
	for _, c := range cases {
		if got := audioIsSurround(c.a); got != c.want {
			t.Errorf("audioIsSurround(%+v) = %v, want %v", c.a, got, c.want)
		}
	}
}

func TestAudioCodecSuffix(t *testing.T) {
	cases := []struct {
		a    kinopubapi.Audio
		want string
	}{
		{kinopubapi.Audio{Codec: "aac", Channels: 2}, ""}, // not surround
		{kinopubapi.Audio{Codec: "ac3", Channels: 6}, "AC3 5.1"},
		{kinopubapi.Audio{Codec: "dts", Channels: 8}, "DTS 7.1"},
		{kinopubapi.Audio{Codec: "ac3", Channels: 0}, "AC3"},       // surround codec, unknown channels
		{kinopubapi.Audio{Codec: "", Channels: 6}, "Surround 5.1"}, // surround by channels, no codec
	}
	for _, c := range cases {
		if got := audioCodecSuffix(c.a); got != c.want {
			t.Errorf("audioCodecSuffix(%+v) = %q, want %q", c.a, got, c.want)
		}
	}
}

func TestAudioLabel(t *testing.T) {
	t.Run("type and author", func(t *testing.T) {
		a := kinopubapi.Audio{
			Type:   kinopubapi.NamedID{Title: "Дубляж"},
			Author: kinopubapi.NamedID{Title: "LostFilm"},
		}
		label, filter := audioLabel(a)
		if label != "Дубляж · LostFilm" {
			t.Errorf("label = %q", label)
		}
		if filter != "LostFilm" { // author preferred for filter
			t.Errorf("filter = %q, want LostFilm", filter)
		}
	})
	t.Run("type only filters by type", func(t *testing.T) {
		a := kinopubapi.Audio{Type: kinopubapi.NamedID{Title: "Многоголосый"}}
		label, filter := audioLabel(a)
		if label != "Многоголосый" || filter != "Многоголосый" {
			t.Errorf("label=%q filter=%q", label, filter)
		}
	})
	t.Run("only lang falls back", func(t *testing.T) {
		a := kinopubapi.Audio{Lang: "rus"}
		label, filter := audioLabel(a)
		if label != "rus" || filter != "rus" {
			t.Errorf("label=%q filter=%q, want rus/rus", label, filter)
		}
	})
	t.Run("nothing → Дорожка index", func(t *testing.T) {
		a := kinopubapi.Audio{Index: 3}
		label, _ := audioLabel(a)
		if label != "Дорожка 3" {
			t.Errorf("label = %q, want Дорожка 3", label)
		}
	})
	t.Run("surround appends codec suffix", func(t *testing.T) {
		a := kinopubapi.Audio{
			Type:     kinopubapi.NamedID{Title: "Дубляж"},
			Codec:    "ac3",
			Channels: 6,
		}
		label, _ := audioLabel(a)
		if label != "Дубляж · AC3 5.1" {
			t.Errorf("label = %q, want surround suffix", label)
		}
	})
}

func TestCollectAudios_DedupAndIndex(t *testing.T) {
	// Serial: source from the first episode that has audios. The plain dub and its
	// AC3 5.1 sibling produce distinct labels and are both kept; an exact duplicate
	// label is collapsed.
	it := kinopubapi.Item{
		Seasons: []kinopubapi.Season{
			{Episodes: []kinopubapi.Episode{
				{}, // no audios → skipped
				{Audios: []kinopubapi.Audio{
					{Index: 0, Type: kinopubapi.NamedID{Title: "Дубляж"}, Author: kinopubapi.NamedID{Title: "LostFilm"}},
					{Index: 1, Type: kinopubapi.NamedID{Title: "Дубляж"}, Author: kinopubapi.NamedID{Title: "LostFilm"}, Codec: "ac3", Channels: 6},
					{Index: 2, Type: kinopubapi.NamedID{Title: "Дубляж"}, Author: kinopubapi.NamedID{Title: "LostFilm"}}, // dup label → dropped
				}},
			}},
		},
	}
	got := collectAudios(it)
	if len(got) != 2 {
		t.Fatalf("expected 2 distinct audios, got %d: %+v", len(got), got)
	}
	if got[0].Label != "Дубляж · LostFilm" || got[0].Surround {
		t.Errorf("first audio wrong: %+v", got[0])
	}
	if got[1].Label != "Дубляж · LostFilm · AC3 5.1" || !got[1].Surround {
		t.Errorf("second audio wrong: %+v", got[1])
	}
}

func TestCollectAudios_IndexFallback(t *testing.T) {
	// When Index is 0 (unset for non-first entries), it falls back to the slice
	// position.
	it := kinopubapi.Item{
		Videos: []kinopubapi.Video{
			{Audios: []kinopubapi.Audio{
				{Index: 0, Type: kinopubapi.NamedID{Title: "A"}},
				{Index: 0, Type: kinopubapi.NamedID{Title: "B"}}, // idx 0 → fallback to i=1
			}},
		},
	}
	got := collectAudios(it)
	if len(got) != 2 {
		t.Fatalf("got %d audios", len(got))
	}
	if got[1].Index != 1 {
		t.Errorf("second audio Index = %d, want fallback 1", got[1].Index)
	}
}

func TestCollectSeasons(t *testing.T) {
	it := kinopubapi.Item{
		Seasons: []kinopubapi.Season{
			{Number: 2, Episodes: []kinopubapi.Episode{
				{Number: 1, Title: "", Watched: 0},
			}},
			{Number: 1, Episodes: []kinopubapi.Episode{
				{Number: 1, Title: "Pilot", Watched: 1},
				{Number: 2, Title: ""},
			}},
		},
	}
	seasons, count := collectSeasons(it)
	if count != 3 {
		t.Errorf("episode count = %d, want 3", count)
	}
	// Seasons sorted ascending.
	if len(seasons) != 2 || seasons[0].Number != 1 || seasons[1].Number != 2 {
		t.Fatalf("seasons not sorted: %+v", seasons)
	}
	// Empty title defaults to "Серия N".
	if seasons[1].Episodes[0].Title != "Серия 1" {
		t.Errorf("default episode title = %q", seasons[1].Episodes[0].Title)
	}
	if !seasons[0].Episodes[0].Watched {
		t.Error("watched episode should be marked watched")
	}
}

func TestCollectSeasons_MovieCountsVideos(t *testing.T) {
	it := kinopubapi.Item{Videos: []kinopubapi.Video{{}, {}}}
	seasons, count := collectSeasons(it)
	if seasons != nil {
		t.Errorf("movie should have no seasons, got %+v", seasons)
	}
	if count != 2 {
		t.Errorf("movie episode count should equal video count (2), got %d", count)
	}
}

func TestCollectQualities_DedupSortDescending(t *testing.T) {
	it := kinopubapi.Item{
		Videos: []kinopubapi.Video{
			{Files: []kinopubapi.File{
				{Quality: "1080p", H: 1080},
				{Quality: "1080p", H: 1080}, // HEVC dup → collapsed
				{Quality: "720p", H: 720},
				{Quality: "2160p", H: 2160},
				{Quality: "", H: 480}, // blank quality skipped
			}},
		},
	}
	got := collectQualities(it)
	want := []string{"2160p", "1080p", "720p"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("collectQualities = %v, want %v", got, want)
	}
}

func TestCollectQualities_SerialSamplesFirstWithFiles(t *testing.T) {
	it := kinopubapi.Item{
		Seasons: []kinopubapi.Season{
			{Episodes: []kinopubapi.Episode{
				{}, // no files
				{Files: []kinopubapi.File{{Quality: "720p", H: 720}}},
			}},
		},
	}
	got := collectQualities(it)
	if !reflect.DeepEqual(got, []string{"720p"}) {
		t.Errorf("collectQualities = %v, want [720p]", got)
	}
}

func TestPageOf(t *testing.T) {
	p := kinopubapi.ItemsPage{
		Items: []kinopubapi.Item{{ID: json.Number("1"), Title: "A"}},
		Pagination: kinopubapi.Pagination{
			Current:    2,
			Total:      5,
			TotalItems: 100,
		},
	}
	got := pageOf(p)
	if got.Page != 2 || !got.HasMore || got.Total != 100 {
		t.Errorf("pageOf = %+v", got)
	}
	if len(got.Items) != 1 || got.Items[0].Title != "A" {
		t.Errorf("items not mapped: %+v", got.Items)
	}
}

func TestPageOf_DefaultsCurrentToOne(t *testing.T) {
	p := kinopubapi.ItemsPage{Pagination: kinopubapi.Pagination{Current: 0, Total: 1}}
	got := pageOf(p)
	if got.Page != 1 {
		t.Errorf("Page = %d, want default 1", got.Page)
	}
	if got.HasMore { // total(1) <= current(1)
		t.Error("HasMore should be false on the last page")
	}
}

func TestQueryInt(t *testing.T) {
	r := httptest.NewRequest("GET", "/x?page=3&bad=abc&empty=", nil)
	if got := queryInt(r, "page", 1); got != 3 {
		t.Errorf("page = %d, want 3", got)
	}
	if got := queryInt(r, "bad", 7); got != 7 {
		t.Errorf("bad (unparseable) = %d, want default 7", got)
	}
	if got := queryInt(r, "empty", 9); got != 9 {
		t.Errorf("empty = %d, want default 9", got)
	}
	if got := queryInt(r, "missing", 5); got != 5 {
		t.Errorf("missing = %d, want default 5", got)
	}
}

func TestQueryFloat(t *testing.T) {
	r := httptest.NewRequest("GET", "/x?imdb=7.5&bad=xx", nil)
	if got := queryFloat(r, "imdb", 0); got != 7.5 {
		t.Errorf("imdb = %v, want 7.5", got)
	}
	if got := queryFloat(r, "bad", 1.2); got != 1.2 {
		t.Errorf("bad = %v, want default 1.2", got)
	}
	if got := queryFloat(r, "missing", 3.3); got != 3.3 {
		t.Errorf("missing = %v, want 3.3", got)
	}
}

func TestWatchProgress(t *testing.T) {
	serial := func(status int, time, dur float64) kinopubapi.Item {
		return kinopubapi.Item{Seasons: []kinopubapi.Season{
			{Number: 1, Episodes: []kinopubapi.Episode{
				{Number: 1, Watching: kinopubapi.Watching{Status: status, Time: time}, Duration: dur},
			}},
		}}
	}
	t.Run("in-progress resumes", func(t *testing.T) {
		resume, dur := watchProgress(serial(0, 300, 3600), 1, 1)
		if resume != 300 || dur != 3600 {
			t.Errorf("got resume=%d dur=%d, want 300/3600", resume, dur)
		}
	})
	t.Run("fully watched → no resume", func(t *testing.T) {
		resume, dur := watchProgress(serial(1, 300, 3600), 1, 1)
		if resume != 0 || dur != 3600 {
			t.Errorf("watched: got resume=%d dur=%d, want 0/3600", resume, dur)
		}
	})
	t.Run("only a few seconds in → no resume", func(t *testing.T) {
		resume, _ := watchProgress(serial(0, 8, 3600), 1, 1)
		if resume != 0 {
			t.Errorf("≤10s should not resume, got %d", resume)
		}
	})
	t.Run("basically finished → no resume", func(t *testing.T) {
		resume, _ := watchProgress(serial(0, 3580, 3600), 1, 1) // within last 60s
		if resume != 0 {
			t.Errorf("near-end should not resume, got %d", resume)
		}
	})
	t.Run("movie via videos", func(t *testing.T) {
		it := kinopubapi.Item{Videos: []kinopubapi.Video{
			{Number: 1, Watching: kinopubapi.Watching{Status: 0, Time: 500}, Duration: 7200},
		}}
		resume, dur := watchProgress(it, 0, 0)
		if resume != 500 || dur != 7200 {
			t.Errorf("movie: got resume=%d dur=%d", resume, dur)
		}
	})
	t.Run("movie specific part", func(t *testing.T) {
		it := kinopubapi.Item{Videos: []kinopubapi.Video{
			{Number: 1, Watching: kinopubapi.Watching{Status: 0, Time: 100}, Duration: 1000},
			{Number: 2, Watching: kinopubapi.Watching{Status: 0, Time: 600}, Duration: 2000},
		}}
		resume, dur := watchProgress(it, 0, 2)
		if resume != 600 || dur != 2000 {
			t.Errorf("part 2: got resume=%d dur=%d, want 600/2000", resume, dur)
		}
	})
}

func TestToDiscoverDetail(t *testing.T) {
	it := kinopubapi.Item{
		ID:        json.Number("77"),
		Type:      "serial",
		Title:     "Сериал / Series",
		Plot:      "plot text",
		Cast:      "actor1, actor2",
		Countries: []kinopubapi.NamedID{{Title: "США"}},
		Duration:  kinopubapi.Duration{Average: 2700}, // 45 min
		Seasons: []kinopubapi.Season{
			{Number: 1, Episodes: []kinopubapi.Episode{
				{Number: 1, Title: "Ep1", Files: []kinopubapi.File{{Quality: "1080p", H: 1080}}},
			}},
		},
	}
	d := toDiscoverDetail(it)
	if d.Plot != "plot text" || d.Cast != "actor1, actor2" {
		t.Errorf("plot/cast wrong: %+v", d)
	}
	if len(d.Countries) != 1 || d.Countries[0] != "США" {
		t.Errorf("countries = %v", d.Countries)
	}
	if d.DurationMin != 45 {
		t.Errorf("DurationMin = %d, want 45", d.DurationMin)
	}
	if d.EpisodeCount != 1 {
		t.Errorf("EpisodeCount = %d, want 1", d.EpisodeCount)
	}
	if d.ItemURL != "https://kino.pub/item/view/77" {
		t.Errorf("ItemURL = %q", d.ItemURL)
	}
	if !reflect.DeepEqual(d.Qualities, []string{"1080p"}) {
		t.Errorf("Qualities = %v", d.Qualities)
	}
}
