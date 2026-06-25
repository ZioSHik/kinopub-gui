package domain

import (
	"reflect"
	"testing"
)

func TestNormLang(t *testing.T) {
	tests := []struct{ in, want string }{
		{"ru", "rus"},
		{"RUS", "rus"},
		{"Russian", "rus"},
		{"русский", "rus"},
		{" (RUS) ", "rus"},
		{"[jpn]", "jpn"},
		{"japanese", "jpn"},
		{"en", "eng"},
		{"de", "ger"},
		{"deu", "ger"},
		{"zho", "chi"},
		{"", ""},
		{"   ", ""},
		{"klingon", "klingon"}, // unknown → lowercased/trimmed
		{"  KLINGON  ", "klingon"},
		{"(unknown)", "unknown"},
	}
	for _, tt := range tests {
		if got := normLang(tt.in); got != tt.want {
			t.Errorf("normLang(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestParseTrailingLang(t *testing.T) {
	tests := []struct{ in, want string }{
		{"Оригинал (JPN)", "jpn"},
		{"01. Многоголосый. AniLibria (RUS)", "rus"},
		{"no parens here", ""},
		{"", ""},
		{"(eng)", "eng"},
		{"unbalanced (open", ""}, // open with no close after it
		{"closed) only", ""},     // close before open → close <= open
		{"a) (jpn)", "jpn"},      // last pair wins
		{"empty ()", ""},         // empty content → normLang("") == ""
		{"trailing (rus) tail", "rus"},
	}
	for _, tt := range tests {
		if got := parseTrailingLang(tt.in); got != tt.want {
			t.Errorf("parseTrailingLang(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestAudioMatches(t *testing.T) {
	track := AudioTrackInfo{Name: "01. Многоголосый. AniLibria (RUS)", Language: "rus"}
	tests := []struct {
		name    string
		track   AudioTrackInfo
		pattern string
		want    bool
	}{
		{"empty pattern never matches", track, "", false},
		{"whitespace-only pattern never matches", track, "   ", false},
		{"name substring case-insensitive", track, "anilibria", true},
		{"name substring exact case", track, "AniLibria", true},
		{"language canonical match via alias", track, "ru", true},
		{"language canonical match russian word", track, "russian", true},
		{"no match", track, "netflix", false},
		{"match against language field jpn", AudioTrackInfo{Name: "Оригинал", Language: "jpn"}, "japanese", true},
		{"match against language field via substring of hay", AudioTrackInfo{Name: "Оригинал", Language: "jpn"}, "jpn", true},
		{"empty language, name carries lang word", AudioTrackInfo{Name: "Original English dub"}, "english", true},
		{"empty track empty pattern", AudioTrackInfo{}, "", false},
		{"pattern with surrounding space trimmed", track, "  anilibria  ", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := audioMatches(tt.track, tt.pattern); got != tt.want {
				t.Errorf("audioMatches(%+v, %q) = %v, want %v", tt.track, tt.pattern, got, tt.want)
			}
		})
	}
}

func TestPreferRank(t *testing.T) {
	track := AudioTrackInfo{Name: "AniLibria", Language: "rus"}
	if got := preferRank(track, []string{"jpn", "rus", "eng"}); got != 1 {
		t.Errorf("preferRank matched index = %d, want 1", got)
	}
	if got := preferRank(track, []string{"jpn", "eng"}); got != 2 {
		t.Errorf("preferRank no match = %d, want len(prefer)=2", got)
	}
	if got := preferRank(track, nil); got != 0 {
		t.Errorf("preferRank empty prefer = %d, want 0", got)
	}
	// First matching hint wins even if a later one also matches.
	if got := preferRank(track, []string{"anilibria", "rus"}); got != 0 {
		t.Errorf("preferRank first-match = %d, want 0", got)
	}
}

func TestExtractAudioKeywords_Edge(t *testing.T) {
	tests := []struct {
		name  string
		track AudioTrackInfo
		want  []string
	}{
		{"empty name empty lang", AudioTrackInfo{}, nil},
		{"only ordinal and descriptors falls back to lang", AudioTrackInfo{Name: "01. Дубляж", Language: "rus"}, []string{"rus"}},
		{"only ordinal no lang at all", AudioTrackInfo{Name: "01."}, nil},
		{"single-char tokens dropped, fall back lang", AudioTrackInfo{Name: "01. A B", Language: "eng"}, []string{"eng"}},
		{"dedups repeated studio", AudioTrackInfo{Name: "Netflix Netflix", Language: "eng"}, []string{"Netflix"}},
		{"keeps multiple distinct studios", AudioTrackInfo{Name: "Studio Kubik Lostfilm", Language: "rus"}, []string{"Studio", "Kubik", "Lostfilm"}},
		{"pure number tokens dropped", AudioTrackInfo{Name: "12 34 Lostfilm", Language: "rus"}, []string{"Lostfilm"}},
		{"lang word dropped then fall back via lang field", AudioTrackInfo{Name: "Original", Language: "jpn"}, []string{"jpn"}},
		{"no lang field falls back to trailing paren", AudioTrackInfo{Name: "Оригинал (JPN)"}, []string{"jpn"}},
		{"preserves original casing of kept token", AudioTrackInfo{Name: "AniLibria", Language: "rus"}, []string{"AniLibria"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractAudioKeywords(tt.track)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ExtractAudioKeywords(%+v) = %v, want %v", tt.track, got, tt.want)
			}
		})
	}
}

func TestDeriveAudioPrefer_Edge(t *testing.T) {
	// No include match → nil.
	if got := DeriveAudioPrefer(tracksA, []string{"netflix"}); got != nil {
		t.Errorf("no match = %v, want nil", got)
	}
	// Empty include patterns match nothing.
	if got := DeriveAudioPrefer(tracksA, nil); got != nil {
		t.Errorf("nil include = %v, want nil", got)
	}
	// Matching the JPN original derives jpn (from language field).
	if got := DeriveAudioPrefer(tracksA, []string{"оригинал"}); !reflect.DeepEqual(got, []string{"jpn"}) {
		t.Errorf("оригинал = %v, want [jpn]", got)
	}
	// Multiple matches across languages dedup and order by first appearance.
	tracks := []AudioTrackInfo{
		{Index: 0, Name: "Dub A", Language: "rus"},
		{Index: 1, Name: "Dub B", Language: "rus"},
		{Index: 2, Name: "Dub C", Language: "eng"},
	}
	if got := DeriveAudioPrefer(tracks, []string{"dub"}); !reflect.DeepEqual(got, []string{"rus", "eng"}) {
		t.Errorf("multi = %v, want [rus eng]", got)
	}
	// Track with empty language but trailing paren lang in name.
	tracks2 := []AudioTrackInfo{{Index: 0, Name: "Keep (JPN)"}}
	if got := DeriveAudioPrefer(tracks2, []string{"keep"}); !reflect.DeepEqual(got, []string{"jpn"}) {
		t.Errorf("trailing-paren lang = %v, want [jpn]", got)
	}
}

func TestAudioSpec_Matches(t *testing.T) {
	track := AudioTrackInfo{Name: "06. Многоголосый. TVShows (RUS) AC3", Language: "rus"}
	tests := []struct {
		name string
		spec AudioSpec
		want bool
	}{
		{"empty require matches nothing", AudioSpec{}, false},
		{"empty require with forbid still nothing", AudioSpec{Forbid: []string{"xyz"}}, false},
		{"single require present", AudioSpec{Require: []string{"tvshows"}}, true},
		{"require absent", AudioSpec{Require: []string{"netflix"}}, false},
		{"all requires present", AudioSpec{Require: []string{"tvshows", "ac3"}}, true},
		{"one require absent fails AND", AudioSpec{Require: []string{"tvshows", "dts"}}, false},
		{"forbid present blocks", AudioSpec{Require: []string{"tvshows"}, Forbid: []string{"ac3"}}, false},
		{"forbid absent allows", AudioSpec{Require: []string{"tvshows"}, Forbid: []string{"dts"}}, true},
		{"require via language alias", AudioSpec{Require: []string{"russian"}}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.spec.matches(track); got != tt.want {
				t.Errorf("spec %+v matches = %v, want %v", tt.spec, got, tt.want)
			}
		})
	}
}

func TestSelectAudio_SpecsFallbackPrefersHint(t *testing.T) {
	// Spec matches nothing → fall back to single best by Prefer rank.
	tracks := []AudioTrackInfo{
		{Index: 0, Name: "01. Original (JPN)", Language: "jpn"},
		{Index: 1, Name: "02. Dub (RUS)", Language: "rus"},
	}
	pref := AudioPreference{
		Specs:  []AudioSpec{{Require: []string{"netflix"}}},
		Prefer: []string{"rus"},
	}
	if got := SelectAudio(tracks, pref); !reflect.DeepEqual(got, []int{1}) {
		t.Errorf("specs fallback = %v, want [1] (preferred rus)", got)
	}
	// No prefer hint → tie breaks to first (highest in source list).
	pref2 := AudioPreference{Specs: []AudioSpec{{Require: []string{"netflix"}}}}
	if got := SelectAudio(tracks, pref2); !reflect.DeepEqual(got, []int{0}) {
		t.Errorf("specs fallback no-prefer = %v, want [0]", got)
	}
}

func TestSelectAudio_IncludeFallbackNoPreferUsesFirst(t *testing.T) {
	// Include matches nothing, no Prefer → first remaining track.
	pref := AudioPreference{Include: []string{"netflix"}}
	if got := SelectAudio(tracksA, pref); !reflect.DeepEqual(got, []int{0}) {
		t.Errorf("include miss no-prefer = %v, want [0]", got)
	}
}

func TestSelectAudio_ExcludeThenIncludeInteraction(t *testing.T) {
	// Exclude jpn, include "оригинал" (which only matches the excluded jpn track)
	// → include matches nothing among remaining → fallback to a remaining track.
	pref := AudioPreference{Exclude: []string{"jpn"}, Include: []string{"оригинал"}, Prefer: []string{"rus"}}
	got := SelectAudio(tracksA, pref)
	if len(got) != 1 {
		t.Fatalf("got %v, want exactly one fallback track", got)
	}
	// The fallback must be among the non-jpn (remaining) tracks: index 0 or 1.
	if got[0] != 0 && got[0] != 1 {
		t.Errorf("fallback chose excluded track index %d", got[0])
	}
}

func TestSelectAudio_SingleTrack(t *testing.T) {
	tracks := []AudioTrackInfo{{Index: 0, Name: "Only (RUS)", Language: "rus"}}
	if got := SelectAudio(tracks, AudioPreference{}); !reflect.DeepEqual(got, []int{0}) {
		t.Errorf("single keep-all = %v, want [0]", got)
	}
	// Exclude the only track → kept anyway (never empties output).
	if got := SelectAudio(tracks, AudioPreference{Exclude: []string{"rus"}}); !reflect.DeepEqual(got, []int{0}) {
		t.Errorf("single exclude-all = %v, want [0]", got)
	}
}

func TestBuildAudioPreference_ChooseNoneKeepsAll(t *testing.T) {
	// Empty chosen → no Include/Exclude → IsAll preference (keep everything).
	pref := BuildAudioPreference(tracksA, nil)
	if !pref.IsAll() {
		t.Errorf("choosing none should yield IsAll preference, got %+v", pref)
	}
	if got := SelectAudio(tracksA, pref); !reflect.DeepEqual(got, []int{0, 1, 2}) {
		t.Errorf("keep all = %v, want [0 1 2]", got)
	}
}

func TestBuildAudioPreference_ChooseAllNoExclude(t *testing.T) {
	// Choosing every track must not add Exclude patterns (guarded by len<len).
	pref := BuildAudioPreference(tracksA, []int{0, 1, 2})
	if len(pref.Exclude) != 0 {
		t.Errorf("choosing all should not produce excludes, got %v", pref.Exclude)
	}
}

func TestBuildAudioPreference_IgnoresOutOfRangeIndices(t *testing.T) {
	// Negative and too-large indices are ignored; a valid one still works.
	pref := BuildAudioPreference(tracksA, []int{-1, 99, 0})
	if !reflect.DeepEqual(pref.Include, []string{"AniLibria"}) {
		t.Errorf("Include = %v, want [AniLibria]", pref.Include)
	}
	// All-out-of-range → empty chosenSet → IsAll.
	pref2 := BuildAudioPreference(tracksA, []int{-5, 100})
	if !pref2.IsAll() {
		t.Errorf("all-out-of-range should be IsAll, got %+v", pref2)
	}
}
