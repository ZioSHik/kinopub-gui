package domain

import (
	"reflect"
	"testing"
)

// A title can have several anonymous translations and the same studio in
// multiple languages, codecs and channel layouts. One click must keep one.
func TestExactAudioSourceSelection(t *testing.T) {
	sources := []AudioSource{
		{1, "rus", "Многоголосый", "Jask", "aac", 6},
		{2, "rus", "Многоголосый", "Jask Plus", "aac", 6},
		{3, "ukr", "Многоголосый", "", "aac", 6},
		{4, "ukr", "Многоголосый", "", "aac", 2},
		{5, "ukr", "Многоголосый", "НЛО-TV", "aac", 2},
		{6, "rus", "Многоголосый", "Jask", "ac3", 6},
		{7, "eng", "Оригинал", "", "aac", 6},
		{8, "eng", "Оригинал", "Комментарии", "aac", 2},
	}
	tracks := make([]AudioTrackInfo, len(sources))
	for i, s := range sources {
		// Stereo/5.1 share an untagged HLS name except for the ordinal.
		s.Codec = ""
		s.Channels = 0
		tracks[i] = AudioTrackInfo{Index: i, Name: string(rune('1'+i)) + ". " + s.RecordedName(), Language: s.Language}
	}
	AttachAudioSources(tracks, sources)
	for i, source := range sources {
		t.Run(source.RecordedName(), func(t *testing.T) {
			pref := AudioPreference{Specs: []AudioSpec{{Require: []string{"Многоголосый"}, Source: &source}}}
			got, fallback := SelectAudioResolved(tracks, pref)
			if fallback || !reflect.DeepEqual(got, []int{i}) {
				t.Fatalf("selected %v fallback=%t, want only %d", got, fallback, i)
			}
			if !pref.Covers([]string{source.RecordedName()}, false) {
				t.Fatal("own recording not recognized")
			}
			for j, other := range sources {
				if j != i && pref.Covers([]string{other.RecordedName()}, false) {
					t.Fatalf("claims other rendition %d", j)
				}
			}
		})
	}
	// Ordinals drift next episode: metadata, not the first episode's index, wins.
	swapped := append([]AudioSource(nil), sources...)
	swapped[0].Index, swapped[2].Index = 3, 1
	next := []AudioTrackInfo{{Name: "1. Многоголосый (UKR)", Language: "ukr"}, {Name: "3. Многоголосый. Jask (RUS)", Language: "rus"}}
	AttachAudioSources(next, swapped)
	pref := AudioPreference{Specs: []AudioSpec{{Require: []string{"Jask"}, Source: &sources[0]}}}
	if got, fallback := SelectAudioResolved(next, pref); fallback || !reflect.DeepEqual(got, []int{1}) {
		t.Fatalf("reordered selection=%v fallback=%t", got, fallback)
	}
}

func TestExactAudioMissingDoesNotSubstituteOrPartiallySucceed(t *testing.T) {
	a := AudioSource{1, "rus", "Дубляж", "Studio", "aac", 6}
	b := AudioSource{2, "eng", "Оригинал", "", "aac", 6}
	tracks := []AudioTrackInfo{{Name: "1. Дубляж. Studio (RUS)", Language: "rus", Source: &a}}
	for _, specs := range [][]AudioSpec{
		{{Require: []string{"Original"}, Source: &b}},
		{{Require: []string{"Studio"}, Source: &a}, {Require: []string{"Original"}, Source: &b}},
	} {
		if got, fallback := SelectAudioResolved(tracks, AudioPreference{Specs: specs}); len(got) != 0 || !fallback {
			t.Fatalf("silently substituted/omitted audio: %v %t", got, fallback)
		}
	}
}

func TestAudioSourceMappingRefusesMismatchedLabels(t *testing.T) {
	original := AudioSource{Index: 18, Language: "eng", Type: "Оригинал", Codec: "aac", Channels: 6}
	tracks := []AudioTrackInfo{{Name: "18. Оригинал. Комментарии (ENG)", Language: "eng"}}
	AttachAudioSources(tracks, []AudioSource{original})
	if tracks[0].Source != nil {
		t.Fatal("commentary was relabelled as the original just because its ordinal matched")
	}
	studio := AudioSource{Index: 1, Language: "rus", Type: "Многоголосый", Author: "Jask", Codec: "aac", Channels: 6}
	tracks = []AudioTrackInfo{{Name: "1. Многоголосый. Jask Plus (RUS)", Language: "rus"}}
	AttachAudioSources(tracks, []AudioSource{studio})
	if tracks[0].Source != nil {
		t.Fatal("a studio substring was treated as the exact studio")
	}
}

func TestOnePickerEntryDoesNotDownloadDuplicateSourceMetadata(t *testing.T) {
	source := AudioSource{Language: "rus", Type: "Авторский", Author: "С. Казаков", Codec: "aac", Channels: 6}
	tracks := []AudioTrackInfo{{Index: 0, Name: "11. Авторский. С. Казаков (RUS)", Source: &source}, {Index: 1, Name: "12. Авторский. С. Казаков (RUS)", Source: &source}}
	got, fallback := SelectAudioResolved(tracks, AudioPreference{Specs: []AudioSpec{{Source: &source}}})
	if fallback || !reflect.DeepEqual(got, []int{0}) {
		t.Fatalf("one entry fetched duplicates: %v fallback=%t", got, fallback)
	}
}

func TestRecordedRenditionIdentityKeepsLegacyCodecAndDuplicateSuffixesCompatible(t *testing.T) {
	for _, pair := range [][2]string{
		{"05. Многоголосый. Jask (RUS) AC3 5.1 [AC3 6ch]", "Многоголосый. Jask (RUS) [AC3 6ch]"},
		{"05. Дубляж. Studio (RUS) (2) [EAC3 6ch]", "Дубляж. Studio (RUS) [E-AC3 6ch]"},
	} {
		if !SameAudioRendition(pair[0], pair[1]) {
			t.Fatalf("would duplicate an existing rendition: %v", pair)
		}
	}
	if SameAudioRendition("Studio (RUS) [AAC 2ch]", "Studio (RUS) [AAC 6ch]") {
		t.Fatal("stereo and surround collapsed")
	}
}
