package domain

import (
	"reflect"
	"testing"
)

func TestOriginalSelectionDoesNotIncludeOriginalCommentary(t *testing.T) {
	tracks := []AudioTrackInfo{
		{Name: "18. Оригинал (ENG)", Language: "eng"},
		{Name: "19. Оригинал. Комментарии (ENG)", Language: "eng"},
	}
	for _, pref := range []AudioPreference{
		{Specs: []AudioSpec{{Require: []string{"Оригинал"}}}},
		{Include: []string{"Оригинал"}},
		{Include: []string{"eng"}},
	} {
		got, fallback := SelectAudioResolved(tracks, pref)
		if !reflect.DeepEqual(got, []int{0}) || fallback {
			t.Fatalf("original picked commentary: %v %v, pref=%+v", got, fallback, pref)
		}
		if pref.Covers([]string{tracks[1].Name}, false) {
			t.Fatal("commentary was counted as a completed original")
		}
		if !pref.Covers([]string{tracks[0].Name}, false) {
			t.Fatal("original was not recognized")
		}
	}
}

func TestCommentaryCanBeExplicitlySelected(t *testing.T) {
	tracks := []AudioTrackInfo{{Name: "Original (ENG)", Language: "eng"}, {Name: "Original commentary (ENG)", Language: "eng"}}
	cases := []struct {
		pref AudioPreference
		want []int
	}{
		{AudioPreference{Specs: []AudioSpec{{Require: []string{"commentary"}}}}, []int{1}},
		{AudioPreference{Specs: []AudioSpec{{Require: []string{"Original", "commentary"}}}}, []int{1}},
		{AudioPreference{Specs: []AudioSpec{{Require: []string{"Original"}}, {Require: []string{"commentary"}}}}, []int{0, 1}},
		{AudioPreference{}, []int{0, 1}},
	}
	for _, tc := range cases {
		got, fallback := SelectAudioResolved(tracks, tc.pref)
		if !reflect.DeepEqual(got, tc.want) || fallback {
			t.Fatalf("got=%v fallback=%v want=%v", got, fallback, tc.want)
		}
	}
}

func TestCommentaryIsNeverAnImplicitFallback(t *testing.T) {
	tracks := []AudioTrackInfo{{Name: "Original commentary (ENG)", Language: "eng"}, {Name: "Russian dub", Language: "rus"}}
	got, fallback := SelectAudioResolved(tracks, AudioPreference{Include: []string{"missing studio"}, Prefer: []string{"eng"}})
	if !fallback || !reflect.DeepEqual(got, []int{1}) {
		t.Fatalf("fallback selected commentary: %v %v", got, fallback)
	}
	got, fallback = SelectAudioResolved(tracks[:1], AudioPreference{Specs: []AudioSpec{{Require: []string{"Original"}}}})
	if len(got) != 0 || !fallback {
		t.Fatalf("only commentary cannot satisfy original: %v %v", got, fallback)
	}
}
