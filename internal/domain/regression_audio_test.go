package domain

import (
	"reflect"
	"testing"
)

func TestAC3SelectionDoesNotIncludeEAC3(t *testing.T) {
	tracks := []AudioTrackInfo{{Name: "Studio AC3", Language: "rus"}, {Name: "Studio E-AC3", Language: "rus"}}
	pref := AudioPreference{Specs: []AudioSpec{{Require: []string{"Studio", "ac3"}}}}
	got, fallback := SelectAudioResolved(tracks, pref)
	if !reflect.DeepEqual(got, []int{0}) || fallback {
		t.Fatalf("selected=%v fallback=%v: AC3 selection included E-AC3", got, fallback)
	}
}
