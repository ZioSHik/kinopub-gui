package domain

import (
	"reflect"
	"testing"
)

// realistic track sets seen across episodes of the same series.
var (
	// Episode where AniLibria is "01. Многоголосый. AniLibria (RUS)".
	tracksA = []AudioTrackInfo{
		{Index: 0, Name: "01. Многоголосый. AniLibria (RUS)", Language: "rus"},
		{Index: 1, Name: "02. Двухголосый (RUS)", Language: "rus"},
		{Index: 2, Name: "03. Оригинал (JPN)", Language: "jpn"},
	}
	// Episode where AniLibria moved to index 02 and lost its descriptor.
	tracksB = []AudioTrackInfo{
		{Index: 0, Name: "01. Двухголосый (RUS)", Language: "rus"},
		{Index: 1, Name: "02. AniLibria", Language: "rus"},
		{Index: 2, Name: "03. Оригинал (JPN)", Language: "jpn"},
	}
	// Episode missing AniLibria entirely.
	tracksC = []AudioTrackInfo{
		{Index: 0, Name: "01. Двухголосый (RUS)", Language: "rus"},
		{Index: 1, Name: "02. Оригинал (JPN)", Language: "jpn"},
	}
)

func TestSelectAudio_KeepAll(t *testing.T) {
	got := SelectAudio(tracksA, AudioPreference{})
	want := []int{0, 1, 2}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("SelectAudio(all) = %v, want %v", got, want)
	}
}

func TestSelectAudio_IncludeMatchesAcrossNamingDrift(t *testing.T) {
	pref := AudioPreference{Include: []string{"anilibria"}, Prefer: []string{"rus"}}

	if got := SelectAudio(tracksA, pref); !reflect.DeepEqual(got, []int{0}) {
		t.Errorf("tracksA: got %v, want [0]", got)
	}
	if got := SelectAudio(tracksB, pref); !reflect.DeepEqual(got, []int{1}) {
		t.Errorf("tracksB: got %v, want [1]", got)
	}
}

func TestSelectAudio_FallbackPrefersDesiredLanguage(t *testing.T) {
	// AniLibria (a RUS dub) is gone in tracksC. Fallback must pick a RUS track,
	// not the JPN original.
	pref := AudioPreference{Include: []string{"anilibria"}, Prefer: []string{"rus"}}
	got := SelectAudio(tracksC, pref)
	if !reflect.DeepEqual(got, []int{0}) {
		t.Fatalf("fallback = %v, want [0] (the RUS track), tracks=%+v", got, tracksC)
	}
}

func TestSelectAudio_ExcludeJapanese(t *testing.T) {
	pref := AudioPreference{Exclude: []string{"jpn"}}
	if got := SelectAudio(tracksA, pref); !reflect.DeepEqual(got, []int{0, 1}) {
		t.Errorf("exclude jpn (lang): got %v, want [0 1]", got)
	}
	// Exclude by name fragment too.
	pref2 := AudioPreference{Exclude: []string{"оригинал"}}
	if got := SelectAudio(tracksA, pref2); !reflect.DeepEqual(got, []int{0, 1}) {
		t.Errorf("exclude оригинал: got %v, want [0 1]", got)
	}
}

func TestSelectAudio_ExcludeNeverEmptiesOutput(t *testing.T) {
	// Excluding every present language must be ignored to keep some audio.
	pref := AudioPreference{Exclude: []string{"rus", "jpn"}}
	got := SelectAudio(tracksA, pref)
	if len(got) != len(tracksA) {
		t.Fatalf("exclude-all should keep all: got %v", got)
	}
}

func TestSelectAudio_IncludePlusExclude(t *testing.T) {
	// Keep AniLibria, never JPN. On the episode that has AniLibria, only it.
	pref := AudioPreference{Include: []string{"anilibria"}, Exclude: []string{"jpn"}, Prefer: []string{"rus"}}
	if got := SelectAudio(tracksB, pref); !reflect.DeepEqual(got, []int{1}) {
		t.Errorf("got %v, want [1]", got)
	}
}

// The screenshot bug: one studio ("TVShows") ships a plain stereo dub AND an
// AC3 5.1 sibling. Selecting just the plain one must keep exactly it, not both.
func TestSelectAudio_SpecsSeparatePlainFromAC3(t *testing.T) {
	tracks := []AudioTrackInfo{
		{Index: 0, Name: "01. Многоголосый. TVShows (RUS)", Language: "rus"},
		{Index: 1, Name: "06. Многоголосый. TVShows (RUS) AC3", Language: "rus"},
		{Index: 2, Name: "03. Оригинал (JPN)", Language: "jpn"},
	}

	// Plain only: REQUIRE the studio, FORBID the codec → keeps index 0 alone.
	plain := AudioPreference{Specs: []AudioSpec{{Require: []string{"TVShows"}, Forbid: []string{"ac3"}}}}
	if got := SelectAudio(tracks, plain); !reflect.DeepEqual(got, []int{0}) {
		t.Errorf("plain-only spec = %v, want [0]", got)
	}

	// AC3 only: REQUIRE studio + codec → keeps index 1 alone.
	ac3 := AudioPreference{Specs: []AudioSpec{{Require: []string{"TVShows", "ac3"}}}}
	if got := SelectAudio(tracks, ac3); !reflect.DeepEqual(got, []int{1}) {
		t.Errorf("ac3-only spec = %v, want [1]", got)
	}

	// Both variants explicitly selected → both kept.
	both := AudioPreference{Specs: []AudioSpec{
		{Require: []string{"TVShows"}, Forbid: []string{"ac3"}},
		{Require: []string{"TVShows", "ac3"}},
	}}
	if got := SelectAudio(tracks, both); !reflect.DeepEqual(got, []int{0, 1}) {
		t.Errorf("both-variants specs = %v, want [0 1]", got)
	}

	// A spec that matches nothing this episode falls back to a single track (not
	// empty, never all).
	miss := AudioPreference{Specs: []AudioSpec{{Require: []string{"NoSuchStudio"}}}}
	if got := SelectAudio(tracks, miss); len(got) != 1 {
		t.Errorf("missing spec fallback = %v, want exactly one track", got)
	}

	// Specs make the preference non-"all".
	if plain.IsAll() {
		t.Error("a preference with Specs must not report IsAll")
	}
}

func TestSelectAudio_Empty(t *testing.T) {
	if got := SelectAudio(nil, AudioPreference{Include: []string{"x"}}); got != nil {
		t.Fatalf("nil tracks should yield nil, got %v", got)
	}
}

func TestExtractAudioKeywords(t *testing.T) {
	tests := []struct {
		name  string
		track AudioTrackInfo
		want  []string
	}{
		{
			name:  "studio with descriptor and lang",
			track: AudioTrackInfo{Name: "01. Многоголосый. AniLibria (RUS)", Language: "rus"},
			want:  []string{"AniLibria"},
		},
		{
			name:  "studio bare",
			track: AudioTrackInfo{Name: "02. AniLibria", Language: "rus"},
			want:  []string{"AniLibria"},
		},
		{
			name:  "no studio, original japanese falls back to lang",
			track: AudioTrackInfo{Name: "03. Оригинал (JPN)", Language: "jpn"},
			want:  []string{"jpn"},
		},
		{
			name:  "descriptor only falls back to lang",
			track: AudioTrackInfo{Name: "02. Двухголосый (RUS)", Language: "rus"},
			want:  []string{"rus"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ExtractAudioKeywords(tt.track)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ExtractAudioKeywords(%q) = %v, want %v", tt.track.Name, got, tt.want)
			}
		})
	}
}

func TestBuildAudioPreference_FromMenuChoice(t *testing.T) {
	// User picks AniLibria (index 0) on the first episode.
	pref := BuildAudioPreference(tracksA, []int{0})
	if !reflect.DeepEqual(pref.Include, []string{"AniLibria"}) {
		t.Fatalf("Include = %v, want [AniLibria]", pref.Include)
	}
	if !reflect.DeepEqual(pref.Prefer, []string{"rus"}) {
		t.Fatalf("Prefer = %v, want [rus]", pref.Prefer)
	}

	// That preference must select AniLibria on every episode and fall back to
	// the RUS track when AniLibria is missing.
	if got := SelectAudio(tracksA, pref); !reflect.DeepEqual(got, []int{0}) {
		t.Errorf("tracksA: got %v, want [0]", got)
	}
	if got := SelectAudio(tracksB, pref); !reflect.DeepEqual(got, []int{1}) {
		t.Errorf("tracksB: got %v, want [1]", got)
	}
	if got := SelectAudio(tracksC, pref); !reflect.DeepEqual(got, []int{0}) {
		t.Errorf("tracksC fallback: got %v, want [0] (RUS)", got)
	}
}

func TestBuildAudioPreference_MultipleChoices(t *testing.T) {
	// Pick AniLibria + the JPN original.
	pref := BuildAudioPreference(tracksA, []int{0, 2})
	got := SelectAudio(tracksA, pref)
	if !reflect.DeepEqual(got, []int{0, 2}) {
		t.Fatalf("got %v, want [0 2]; pref=%+v", got, pref)
	}
}

func TestBuildAudioPreference_UnchecksAC3CodecVariant(t *testing.T) {
	// Reproduces the reported bug: two tracks are the same "R5" dub, one is an
	// AC3 codec variant. Unchecking the AC3 one (keeping only the plain R5) must
	// actually drop it, even though both names contain "R5".
	tracks := []AudioTrackInfo{
		{Index: 0, Name: "01. Многоголосый. R5 (RUS)", Language: "rus"},
		{Index: 1, Name: "02. Дубляж. Пифагор (RUS)", Language: "rus"},
		{Index: 2, Name: "10. Многоголосый. R5 (RUS) AC3", Language: "rus"},
	}

	// Keep only the plain R5 dub (index 0).
	pref := BuildAudioPreference(tracks, []int{0})
	if got := SelectAudio(tracks, pref); !reflect.DeepEqual(got, []int{0}) {
		t.Fatalf("keep only plain R5: got %v, want [0]; pref=%+v", got, pref)
	}

	// "Select only this" on the Пифагор dub keeps just it.
	pref2 := BuildAudioPreference(tracks, []int{1})
	if got := SelectAudio(tracks, pref2); !reflect.DeepEqual(got, []int{1}) {
		t.Fatalf("keep only Пифагор: got %v, want [1]; pref=%+v", got, pref2)
	}

	// Keeping everything except the AC3 variant drops only the AC3 track.
	pref3 := BuildAudioPreference(tracks, []int{0, 1})
	if got := SelectAudio(tracks, pref3); !reflect.DeepEqual(got, []int{0, 1}) {
		t.Fatalf("drop AC3 only: got %v, want [0 1]; pref=%+v", got, pref3)
	}
}

func TestDeriveAudioPrefer(t *testing.T) {
	got := DeriveAudioPrefer(tracksA, []string{"anilibria"})
	if !reflect.DeepEqual(got, []string{"rus"}) {
		t.Fatalf("DeriveAudioPrefer = %v, want [rus]", got)
	}
}

func TestAudioPreference_IsAll(t *testing.T) {
	if !(AudioPreference{}).IsAll() {
		t.Error("zero AudioPreference should be IsAll")
	}
	if !(AudioPreference{Prefer: []string{"rus"}}).IsAll() {
		t.Error("Prefer-only AudioPreference should be IsAll (no filtering)")
	}
	if (AudioPreference{Include: []string{"x"}}).IsAll() {
		t.Error("Include AudioPreference should not be IsAll")
	}
	if (AudioPreference{Exclude: []string{"x"}}).IsAll() {
		t.Error("Exclude AudioPreference should not be IsAll")
	}
}
