package gui

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/ZioSHik/kinopub-gui/internal/services/kinopubapi"
)

func TestSeasonKeepsDistinctChannelCounts(t *testing.T) {
	stereo := kinopubapi.Audio{Index: 1, Lang: "rus", Type: kinopubapi.NamedID{Title: "Дубляж"}, Codec: "aac", Channels: 2}
	mono := stereo
	mono.Channels = 1
	got := collectSeasonAudios(1, []kinopubapi.Episode{{Number: 1, Audios: []kinopubapi.Audio{stereo}}, {Number: 2, Audios: []kinopubapi.Audio{mono}}})
	if len(got) != 2 {
		t.Fatalf("season has two exact source identities, but picker exposes %d: %+v", len(got), got)
	}
	if got[0].Label == got[1].Label {
		t.Fatal("different sources must be independently selectable")
	}
	if len(got[1].EpisodeKeys) != 1 || got[1].EpisodeKeys[0] != "S1E2" {
		t.Fatalf("incorrect mono availability: %+v", got[1])
	}
}

func TestAudioCatalogPreservesUnnamedTracks(t *testing.T) {
	got := collectAudios(kinopubapi.Item{Videos: []kinopubapi.Video{{Audios: []kinopubapi.Audio{{Index: 1}, {Index: 2}}}}})
	if len(got) != 2 || got[0].Label == got[1].Label {
		t.Fatalf("unnamed tracks collapsed: %+v", got)
	}
}

func TestMovieDiscoveryAndPreviewKeepExactAudioVariants(t *testing.T) {
	audios := []kinopubapi.Audio{
		{Index: 1, Lang: "rus", Type: kinopubapi.NamedID{Title: "Дубляж"}, Codec: "aac", Channels: 2},
		{Index: 2, Lang: "rus", Type: kinopubapi.NamedID{Title: "Дубляж"}, Codec: "aac", Channels: 1},
		{Index: 3, Lang: "rus", Type: kinopubapi.NamedID{Title: "Дубляж"}, Codec: "mp3", Channels: 2},
		{Index: 4, Lang: "rus", Type: kinopubapi.NamedID{Title: "Дубляж"}, Codec: "ac3", Channels: 6},
		{Index: 5, Lang: "eng", Type: kinopubapi.NamedID{Title: "Оригинал"}, Codec: "aac", Channels: 2},
	}
	item := kinopubapi.Item{ID: json.Number("7"), Type: "movie", Videos: []kinopubapi.Video{{Number: 1, Audios: audios, Files: []kinopubapi.File{{URL: kinopubapi.FileURL{HLS4: "https://example.test/master.m3u8"}}}}}}
	detail := toDiscoverDetail(item)
	if detail.IsSerial || len(detail.Seasons) != 0 || len(detail.Audios) != len(audios) {
		t.Fatalf("incorrect movie catalog: %+v", detail)
	}
	pl, err := kinopubapi.BuildPagePlaylist(item)
	if err != nil {
		t.Fatal(err)
	}
	preview := playlistSeasonAudios(pl, 1)
	labels := map[string]bool{}
	for i := range preview {
		if !reflect.DeepEqual(preview[i].EpisodeKeys, []string{"S1E1"}) {
			t.Fatalf("incorrect movie availability: %+v", preview[i])
		}
		preview[i].EpisodeKeys = nil
		if labels[preview[i].Label] {
			t.Fatalf("ambiguous movie picker label: %s", preview[i].Label)
		}
		labels[preview[i].Label] = true
	}
	if !reflect.DeepEqual(detail.Audios, preview) {
		t.Fatalf("discovery and preview disagree: %+v != %+v", detail.Audios, preview)
	}
}
