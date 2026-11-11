package gui

import (
	"github.com/ZioSHik/kinopub-gui/internal/domain"
	"github.com/ZioSHik/kinopub-gui/internal/services/kinopubapi"
	"reflect"
	"testing"
)

func TestSeasonAudioCatalogScopesStudiosAndEpisodeAvailability(t *testing.T) {
	dub := kinopubapi.Audio{Index: 1, Lang: "rus", Type: kinopubapi.NamedID{Title: "Дубляж"}, Codec: "aac", Channels: 2}
	rentv := dub
	rentv.Author.Title = "РенТВ"
	it := kinopubapi.Item{Seasons: []kinopubapi.Season{
		{Number: 1, Episodes: []kinopubapi.Episode{{Number: 1, Audios: []kinopubapi.Audio{dub}}}},
		{Number: 6, Episodes: []kinopubapi.Episode{{Number: 1, Audios: []kinopubapi.Audio{rentv}}, {Number: 2, Audios: []kinopubapi.Audio{dub}}}},
	}}
	seasons, _ := collectSeasons(it)
	if len(seasons[0].Audios) != 1 || seasons[0].Audios[0].Author != "" {
		t.Fatalf("later studio leaked into season 1: %+v", seasons[0])
	}
	if len(seasons[1].Audios) != 2 || seasons[1].Audios[0].Author != "РенТВ" {
		t.Fatalf("season 6 lost its studio: %+v", seasons[1])
	}
	if !reflect.DeepEqual(seasons[1].Audios[0].EpisodeKeys, []string{"S6E1"}) {
		t.Fatalf("incorrect availability: %+v", seasons[1].Audios[0])
	}
}

func TestPreviewAudioCatalogMatchesDiscoveryCatalog(t *testing.T) {
	playlist := &domain.PagePlaylist{Episodes: []domain.PageEpisode{
		{Season: 1, Episode: 1, AudioSources: []domain.AudioSource{{Index: 1, Language: "eng", Type: "Оригинал", Codec: "aac", Channels: 6}}},
		{Season: 6, Episode: 1, AudioSources: []domain.AudioSource{{Index: 1, Language: "rus", Type: "Дубляж", Author: "РенТВ", Codec: "aac", Channels: 2}}},
	}}
	audios := playlistSeasonAudios(playlist, 6)
	if len(audios) != 1 || audios[0].Author != "РенТВ" || audios[0].Lang != "rus" || !reflect.DeepEqual(audios[0].EpisodeKeys, []string{"S6E1"}) {
		t.Fatalf("incorrect preview season catalog: %+v", audios)
	}
	if len(playlistSeasonAudios(playlist, 2)) != 0 {
		t.Fatal("unavailable season inherited another season's tracks")
	}
}
