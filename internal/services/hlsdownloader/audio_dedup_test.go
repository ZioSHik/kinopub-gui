package hlsdownloader

import "testing"

// kino.pub's mixed-codec (4K) master lists each dub twice within one audio group
// (AVC + HEVC), under the identical NAME. audioRenditionsFor must return each
// distinct dub once so a single picked track isn't downloaded twice.
func TestAudioRenditionsFor_DeduplicatesMixedCodecTwins(t *testing.T) {
	master := &MasterPlaylist{
		Audio: []AudioRendition{
			{GroupID: "audio1080", Name: "02. Дубляж. Невафильм (RUS)", Language: "rus", URI: "a1"},
			{GroupID: "audio1080", Name: "02. Дубляж. Невафильм (RUS)", Language: "rus", URI: "a2"}, // HEVC twin
			{GroupID: "audio1080", Name: "07. Оригинал (ENG)", Language: "eng", URI: "a3"},
			{GroupID: "audio1080", Name: "07. Оригинал (ENG)", Language: "eng", URI: "a4"},         // HEVC twin
			{GroupID: "audio720", Name: "02. Дубляж. Невафильм (RUS)", Language: "rus", URI: "b1"}, // other group
		},
	}
	got := audioRenditionsFor(master, Variant{AudioGroup: "audio1080"})
	if len(got) != 2 {
		t.Fatalf("got %d renditions, want 2 (deduped): %+v", len(got), got)
	}
	if got[0].Name != "02. Дубляж. Невафильм (RUS)" || got[0].URI != "a1" {
		t.Errorf("first should be the first dub occurrence, got %+v", got[0])
	}
	if got[1].Name != "07. Оригинал (ENG)" || got[1].URI != "a3" {
		t.Errorf("second should be the original, got %+v", got[1])
	}
}

func TestAudioRenditionsFor_NoGroupMuxed(t *testing.T) {
	master := &MasterPlaylist{Audio: []AudioRendition{{GroupID: "x", Name: "n", URI: "u"}}}
	if got := audioRenditionsFor(master, Variant{AudioGroup: ""}); len(got) != 0 {
		t.Fatalf("expected no renditions when the variant has no audio group, got %d", len(got))
	}
}
