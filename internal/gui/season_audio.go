package gui

import (
	"fmt"

	"github.com/ZioSHik/kinopub-gui/internal/domain"
	"github.com/ZioSHik/kinopub-gui/internal/services/kinopubapi"
)

// Keep the availability of each exact rendition inside its own season. A
// studio offered in one season or one episode must not describe the whole show.
func collectSeasonAudios(number int, episodes []kinopubapi.Episode) []DiscoverAudio {
	audios := collectAudios(kinopubapi.Item{Seasons: []kinopubapi.Season{{Episodes: episodes}}})
	for i := range audios {
		identity := catalogAudioIdentity(audios[i].Lang, audios[i].Type, audios[i].Author, audios[i].Codec, audios[i].Channels)
		for _, ep := range episodes {
			for _, a := range ep.Audios {
				if catalogAudioIdentity(a.Lang, a.Type.Title, a.Author.Title, a.Codec, a.Channels) == identity {
					audios[i].EpisodeKeys = append(audios[i].EpisodeKeys, fmt.Sprintf("S%dE%d", number, ep.Number))
					break
				}
			}
		}
	}
	return audios
}

// Preview already has provider metadata in the resolved playlist; reuse it
// without another network request or probing a stream (which consumes sessions).
func playlistSeasonAudios(playlist *domain.PagePlaylist, number int) []DiscoverAudio {
	var episodes []kinopubapi.Episode
	for _, ep := range playlist.Episodes {
		if ep.Season != number {
			continue
		}
		audios := make([]kinopubapi.Audio, 0, len(ep.AudioSources))
		for _, a := range ep.AudioSources {
			audios = append(audios, kinopubapi.Audio{Index: a.Index, Lang: a.Language, Codec: a.Codec, Channels: a.Channels,
				Type: kinopubapi.NamedID{Title: a.Type}, Author: kinopubapi.NamedID{Title: a.Author}})
		}
		episodes = append(episodes, kinopubapi.Episode{Number: ep.Episode, Audios: audios})
	}
	return collectSeasonAudios(number, episodes)
}
