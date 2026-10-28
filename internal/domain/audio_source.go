package domain

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// AudioSource is provider metadata for one rendition. Index identifies it only
// inside this episode; it is never used as a preference across episodes.
type AudioSource struct {
	Index    int    `json:"index,omitempty"`
	Language string `json:"language"`
	Type     string `json:"type"`
	Author   string `json:"author"`
	Codec    string `json:"codec"`
	Channels int    `json:"channels"`
}

func (a AudioSource) sameRendition(b AudioSource) bool {
	equal := func(x, y string) bool { return strings.EqualFold(strings.TrimSpace(x), strings.TrimSpace(y)) }
	if a.Language == "" && a.Type == "" && a.Author == "" {
		return false
	}
	return (a.Language == "" || normLang(a.Language) == normLang(b.Language)) && (a.Type == "" || equal(a.Type, b.Type)) && equal(a.Author, b.Author) &&
		(a.Codec == "" || equal(strings.ReplaceAll(a.Codec, "-", ""), strings.ReplaceAll(b.Codec, "-", ""))) && (a.Channels == 0 || a.Channels == b.Channels)
}

func (p AudioPreference) IsExactSourceSelection() bool {
	for _, spec := range p.Specs {
		if spec.Source != nil {
			return true
		}
	}
	return false
}

var sourceOrdinal = regexp.MustCompile(`(?i)^\s*(?:(?:track|дорожка)\s+)?(\d+)[.\s]`)
var sourceDetails = regexp.MustCompile(`\s*\[([^]:]+) (\d+)ch\]$`)
var legacyCodecSuffix = regexp.MustCompile(`(?i)(\))\s+(?:e-?ac3|ac3|dts(?:-hd)?|true-?hd)(?:\s+[257]\.1)?$`)

// AttachAudioSources uses the source ordinal and language together. Refusing an
// unverified mapping is safer than guessing from array position or a substring.
func AttachAudioSources(tracks []AudioTrackInfo, sources []AudioSource) {
	for i := range tracks {
		match := sourceOrdinal.FindStringSubmatch(tracks[i].Name)
		if len(match) == 0 {
			continue
		}
		index, _ := strconv.Atoi(match[1])
		for j := range sources {
			source := sources[j]
			lang := tracks[i].Language
			if lang == "" {
				lang = parseTrailingLang(tracks[i].Name)
			}
			if source.Index == index && (lang == "" || normLang(lang) == normLang(source.Language)) && source.describesTrack(tracks[i]) {
				tracks[i].Source = &source
				break
			}
		}
	}
}

func (a AudioSource) describesTrack(track AudioTrackInfo) bool {
	if IsCommentaryAudio(track.Name) != IsCommentaryAudio(a.Type+" "+a.Author) {
		return false
	}
	if genericAudioName.MatchString(track.Name) {
		return true
	}
	plain := a
	plain.Codec, plain.Channels = "", 0
	name := legacyCodecSuffix.ReplaceAllString(strings.TrimSpace(track.Name), "$1")
	if SameAudioRendition(name, plain.RecordedName()) {
		return true
	}
	// Some episodes omit the translation type while keeping the studio.
	if plain.Author != "" {
		plain.Type = ""
		return SameAudioRendition(name, plain.RecordedName())
	}
	return false
}

func (a AudioSource) RecordedName() string {
	parts := []string{}
	if a.Type != "" {
		parts = append(parts, a.Type)
	}
	if a.Author != "" {
		parts = append(parts, a.Author)
	}
	name := strings.Join(parts, ". ")
	if a.Language != "" {
		name += " (" + strings.ToUpper(a.Language) + ")"
	}
	if a.Codec != "" && a.Channels > 0 {
		name += fmt.Sprintf(" [%s %dch]", strings.ToUpper(a.Codec), a.Channels)
	}
	return strings.TrimSpace(name)
}

func (a AudioSource) matchesRecorded(name string) bool {
	// New files carry the metadata that distinguishes AAC stereo from AAC 5.1;
	// old files are enriched from ffprobe before reaching this comparison.
	details := sourceDetails.FindStringSubmatch(name)
	if len(details) == 0 {
		return false
	}
	channels, _ := strconv.Atoi(details[2])
	b := a
	b.Codec, b.Channels = details[1], channels
	if !a.sameRendition(b) {
		return false
	}
	name = sourceDetails.ReplaceAllString(name, "")
	name = legacyCodecSuffix.ReplaceAllString(name, "$1")
	expected := a
	expected.Codec = ""
	expected.Channels = 0
	return SameAudioRendition(name, expected.RecordedName())
}
