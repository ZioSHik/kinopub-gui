package domain

import (
	"sort"
	"strconv"
	"strings"
)

// AudioTrackInfo describes a single audio rendition available for an episode.
// It is the lightweight, transport-agnostic view used by audio selection and
// the interactive picker (the HLS layer builds these from its renditions).
type AudioTrackInfo struct {
	// Index is the position of the track within the episode's audio list
	// (0-based). SelectAudio returns these indices.
	Index int
	// Name is the human label from the source, e.g.
	// "01. Многоголосый. AniLibria (RUS)" or "02. AniLibria".
	Name string
	// Language is the raw language tag from the source, e.g. "rus", "jpn".
	// It may be empty when the source only encodes the language in Name.
	Language string
}

// AudioPreference describes which audio tracks to keep for a download run.
//
// Selection is substring-based and case-insensitive so it survives the naming
// drift seen across episodes (e.g. "01. Многоголосый. AniLibria (RUS)" in one
// episode and "02. AniLibria" in another both match the pattern "anilibria").
//
// The zero value (no Include, no Exclude) means "keep every track".
type AudioPreference struct {
	// Include lists patterns to keep. A track is kept when its name or language
	// contains any Include pattern (case-insensitive). When Include is empty,
	// every track is kept (subject to Exclude).
	Include []string
	// Exclude lists patterns to drop. A track is dropped when its name or
	// language contains any Exclude pattern (case-insensitive). Exclude is
	// applied before Include. If excluding would remove every track, the
	// exclusion is ignored so the output always carries audio.
	Exclude []string
	// Prefer lists language hints used only to rank the fallback track. When
	// Include matches nothing in a given episode, the highest-ranked remaining
	// track is chosen, preferring tracks whose language matches a Prefer hint;
	// ties break toward the track highest in the source list.
	Prefer []string
}

// IsAll reports whether the preference keeps every available track unchanged.
func (p AudioPreference) IsAll() bool {
	return len(p.Include) == 0 && len(p.Exclude) == 0
}

// langAliases maps common language spellings to a canonical ISO 639-2 code so
// that "ru", "rus", "russian" and "русский" all compare equal.
var langAliases = map[string]string{
	"ru": "rus", "rus": "rus", "russian": "rus", "русский": "rus", "рус": "rus",
	"en": "eng", "eng": "eng", "english": "eng", "англ": "eng",
	"ja": "jpn", "jp": "jpn", "jpn": "jpn", "japanese": "jpn", "яп": "jpn", "японский": "jpn",
	"uk": "ukr", "ukr": "ukr", "ukrainian": "ukr", "укр": "ukr",
	"de": "ger", "ger": "ger", "deu": "ger", "german": "ger",
	"fr": "fre", "fre": "fre", "fra": "fre", "french": "fre",
	"es": "spa", "spa": "spa", "spanish": "spa",
	"it": "ita", "ita": "ita", "italian": "ita",
	"ko": "kor", "kor": "kor", "korean": "kor",
	"zh": "chi", "chi": "chi", "zho": "chi", "chinese": "chi",
}

// normLang canonicalizes a language token for comparison. Unknown tokens are
// returned lowercased and trimmed (of surrounding spaces and parentheses).
func normLang(s string) string {
	k := strings.ToLower(strings.TrimSpace(s))
	k = strings.Trim(k, "()[] ")
	if v, ok := langAliases[k]; ok {
		return v
	}
	return k
}

// audioMatches reports whether track t matches pattern. A match occurs when the
// track's combined name+language contains the pattern (case-insensitive), or
// when their canonical languages are equal (so "rus" matches a "(RUS)" suffix
// as well as a "ru" language tag).
func audioMatches(t AudioTrackInfo, pattern string) bool {
	p := strings.ToLower(strings.TrimSpace(pattern))
	if p == "" {
		return false
	}
	hay := strings.ToLower(t.Name + " " + t.Language)
	if strings.Contains(hay, p) {
		return true
	}
	if tl := normLang(t.Language); tl != "" && tl == normLang(p) {
		return true
	}
	return false
}

// matchesAny reports whether t matches any of the patterns.
func matchesAny(t AudioTrackInfo, patterns []string) bool {
	for _, p := range patterns {
		if audioMatches(t, p) {
			return true
		}
	}
	return false
}

// preferRank returns the rank of a track against the Prefer hints: a lower
// value means a stronger preference. Tracks matching no hint rank last.
func preferRank(t AudioTrackInfo, prefer []string) int {
	for i, p := range prefer {
		if audioMatches(t, p) {
			return i
		}
	}
	return len(prefer)
}

// SelectAudio resolves which audio tracks to keep for an episode given a
// preference. It returns the indices (into tracks) to download, in ascending
// order. The result is deterministic and never empty unless tracks is empty.
//
// Algorithm:
//  1. Drop tracks matching any Exclude pattern. If that removes everything, the
//     exclusion is ignored (a video must keep some audio).
//  2. If Include is empty, keep all remaining tracks.
//  3. Otherwise keep the remaining tracks that match any Include pattern.
//  4. If Include matched nothing (the desired dub is missing this episode),
//     fall back to a single best remaining track: prefer tracks matching a
//     Prefer hint, then the one highest in the source list.
func SelectAudio(tracks []AudioTrackInfo, pref AudioPreference) []int {
	if len(tracks) == 0 {
		return nil
	}

	// 1. Apply excludes.
	remaining := make([]int, 0, len(tracks))
	for i, t := range tracks {
		if !matchesAny(t, pref.Exclude) {
			remaining = append(remaining, i)
		}
	}
	if len(remaining) == 0 {
		// Excludes nuked everything — keep all so the output still has audio.
		remaining = remaining[:0]
		for i := range tracks {
			remaining = append(remaining, i)
		}
	}

	// 2. No positive filter → keep everything that survived excludes.
	if len(pref.Include) == 0 {
		return remaining
	}

	// 3. Keep includes among the remaining tracks.
	matched := make([]int, 0, len(remaining))
	for _, i := range remaining {
		if matchesAny(tracks[i], pref.Include) {
			matched = append(matched, i)
		}
	}
	if len(matched) > 0 {
		return matched
	}

	// 4. Fallback: pick the single best remaining track.
	best := append([]int(nil), remaining...)
	sort.SliceStable(best, func(a, b int) bool {
		ra, rb := preferRank(tracks[best[a]], pref.Prefer), preferRank(tracks[best[b]], pref.Prefer)
		if ra != rb {
			return ra < rb
		}
		return best[a] < best[b]
	})
	return []int{best[0]}
}

// DeriveAudioPrefer inspects the available tracks and returns the canonical
// languages of the tracks matched by the include patterns. It is used to make
// the fallback prefer the language of the originally desired dub (e.g. choosing
// "anilibria" yields ["rus"], so a missing AniLibria falls back to another RUS
// dub rather than the JPN original). Returns nil when nothing matches.
func DeriveAudioPrefer(tracks []AudioTrackInfo, include []string) []string {
	seen := make(map[string]bool)
	var out []string
	for _, t := range tracks {
		if !matchesAny(t, include) {
			continue
		}
		lang := normLang(t.Language)
		if lang == "" {
			lang = parseTrailingLang(t.Name)
		}
		if lang != "" && !seen[lang] {
			seen[lang] = true
			out = append(out, lang)
		}
	}
	return out
}

// parseTrailingLang extracts a language hint from a trailing parenthetical in a
// track name, e.g. "Оригинал (JPN)" → "jpn". Returns "" when none is found.
func parseTrailingLang(name string) string {
	open := strings.LastIndex(name, "(")
	close := strings.LastIndex(name, ")")
	if open >= 0 && close > open {
		return normLang(name[open+1 : close])
	}
	return ""
}

// audioStopwords are non-distinctive tokens in audio track names: voice-count
// and source descriptors plus bare language words. They are removed when
// isolating the distinctive studio keyword so a chosen dub matches across
// episodes whose names drift (e.g. "01. Многоголосый. AniLibria (RUS)" vs
// "02. AniLibria").
var audioStopwords = map[string]bool{
	// Russian voice-count / source descriptors.
	"многоголосый": true, "многоголосная": true, "многоголоска": true,
	"двухголосый": true, "двухголосная": true, "двухголоска": true,
	"одноголосый": true, "одноголосная": true, "одноголоска": true,
	"оригинал": true, "оригинальный": true, "оригинальная": true,
	"дубляж": true, "дублированный": true, "дублированная": true,
	"закадровый": true, "закадровая": true, "закадровое": true, "закадр": true,
	"профессиональный": true, "профессиональная": true, "профессиональное": true,
	"любительский": true, "любительская": true, "любительское": true,
	"авторский": true, "авторская": true,
	"озвучка": true, "озвучивание": true, "перевод": true, "субтитры": true,
	"русский": true, "английский": true, "японский": true, "украинский": true,
	// Latin descriptors.
	"mvo": true, "dvo": true, "svo": true,
	"original": true, "dub": true, "dubbed": true, "sub": true, "subbed": true,
	"voiceover": true, "voice": true, "multi": true, "dual": true,
}

// audioSplitter splits a track name into tokens on punctuation and whitespace.
func audioSplitter(r rune) bool {
	switch r {
	case '.', ',', '(', ')', '[', ']', '/', '\\', ':', ';', '-', '_', ' ', '\t':
		return true
	}
	return false
}

// ExtractAudioKeywords reduces a track name to the distinctive substrings that
// identify its dub/studio, suitable for use as Include patterns that match the
// same dub across episodes. It strips a leading ordinal ("01."), drops
// voice-count/source descriptors and bare language words, and ignores pure
// numbers and language codes. When nothing distinctive remains (e.g.
// "Оригинал (JPN)"), it falls back to the canonical language ("jpn").
func ExtractAudioKeywords(track AudioTrackInfo) []string {
	var keywords []string
	seen := make(map[string]bool)
	for _, tok := range strings.FieldsFunc(track.Name, audioSplitter) {
		low := strings.ToLower(tok)
		if low == "" || audioStopwords[low] {
			continue
		}
		if _, err := strconv.Atoi(low); err == nil {
			continue // pure number (ordinal prefix, etc.)
		}
		if _, ok := langAliases[low]; ok {
			continue // bare language code/word handled via fallback
		}
		if len([]rune(low)) < 2 {
			continue
		}
		if !seen[low] {
			seen[low] = true
			keywords = append(keywords, tok)
		}
	}
	if len(keywords) > 0 {
		return keywords
	}
	// No distinctive studio token — fall back to language so the choice still
	// targets a specific track (e.g. the original-language audio).
	lang := normLang(track.Language)
	if lang == "" {
		lang = parseTrailingLang(track.Name)
	}
	if lang != "" {
		return []string{lang}
	}
	return nil
}

// BuildAudioPreference constructs an AudioPreference that keeps exactly the
// chosen tracks across episodes. It derives Include patterns from the chosen
// tracks' distinctive keywords and Prefer hints from their languages so a dub
// missing in some episode falls back to another track in the same language.
func BuildAudioPreference(tracks []AudioTrackInfo, chosen []int) AudioPreference {
	var include, prefer []string
	seenInc := make(map[string]bool)
	seenPref := make(map[string]bool)
	for _, idx := range chosen {
		if idx < 0 || idx >= len(tracks) {
			continue
		}
		for _, kw := range ExtractAudioKeywords(tracks[idx]) {
			key := strings.ToLower(kw)
			if !seenInc[key] {
				seenInc[key] = true
				include = append(include, kw)
			}
		}
		lang := normLang(tracks[idx].Language)
		if lang == "" {
			lang = parseTrailingLang(tracks[idx].Name)
		}
		if lang != "" && !seenPref[lang] {
			seenPref[lang] = true
			prefer = append(prefer, lang)
		}
	}
	return AudioPreference{Include: include, Prefer: prefer}
}
