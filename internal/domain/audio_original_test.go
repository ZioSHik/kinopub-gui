package domain

import (
	"reflect"
	"testing"
)

func TestOriginalSelectionUsesSourceLanguageForGenericLabels(t *testing.T) {
	tracks := []AudioTrackInfo{
		{Name: "01. Многоголосый (RUS)", Language: "rus"},
		{Name: "Track 8 (ENG)", Language: "eng"},
		{Name: "English commentary", Language: "eng"},
		{Name: "Track 9 (JPN)", Language: "jpn"},
	}
	for _, selector := range []string{"Оригинал", "Original"} {
		pref := AudioPreference{OriginalLanguage: "eng", Specs: []AudioSpec{{Require: []string{selector}, Forbid: []string{"ac3"}}}}
		got, fallback := SelectAudioResolved(tracks, pref)
		if !reflect.DeepEqual(got, []int{1}) || fallback {
			t.Fatalf("%s: got %v, fallback=%v", selector, got, fallback)
		}
	}
	if tracks[1].Name != "Track 8 (ENG)" {
		t.Fatal("mutated source metadata")
	}
	got, fallback := SelectAudioResolved(tracks, AudioPreference{OriginalLanguage: "jpn", Include: []string{"оригинал"}})
	if !reflect.DeepEqual(got, []int{3}) || fallback {
		t.Fatalf("Japanese original: %v %v", got, fallback)
	}
}

func TestOriginalSelectionReadsLanguageFromGenericLabel(t *testing.T) {
	tracks := []AudioTrackInfo{{Name: "01. Многоголосый (RUS)"}, {Name: "Track 8 (ENG)"}}
	got, fallback := SelectAudioResolved(tracks, AudioPreference{OriginalLanguage: "eng", Include: []string{"оригинал"}})
	if !reflect.DeepEqual(got, []int{1}) || fallback {
		t.Fatalf("got %v, fallback=%v", got, fallback)
	}
}

func TestGenericTrackIsNotAssumedToBeEnglishOriginal(t *testing.T) {
	_, fallback := SelectAudioResolved([]AudioTrackInfo{{Name: "Track 8 (ENG)", Language: "eng"}}, AudioPreference{Specs: []AudioSpec{{Require: []string{"Оригинал"}}}})
	if !fallback {
		t.Fatal("guessed original language without API metadata")
	}
}
