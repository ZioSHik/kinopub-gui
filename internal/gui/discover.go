package gui

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/ZioSHik/kinopub-gui/internal/services/kinopubapi"
)

// ---------------------------------------------------------------------------
// Normalized DTOs (stable contract for the frontend, decoupled from the raw
// kino.pub schema).
// ---------------------------------------------------------------------------

// DiscoverItem is a catalog card.
type DiscoverItem struct {
	ID              string   `json:"id"`
	Type            string   `json:"type"`
	Title           string   `json:"title"`
	OriginalTitle   string   `json:"originalTitle,omitempty"`
	Year            int      `json:"year"`
	Poster          string   `json:"poster"`
	Director        string   `json:"director,omitempty"`
	Rating          float64  `json:"rating"` // kino.pub local rating
	ImdbRating      float64  `json:"imdbRating"`
	KinopoiskRating float64  `json:"kinopoiskRating"`
	Genres          []string `json:"genres,omitempty"`
	IsSerial        bool     `json:"isSerial"`
	Subtitle        string   `json:"subtitle,omitempty"`  // watching progress label
	WatchedAt       int64    `json:"watchedAt,omitempty"` // history last_seen (unix), for date grouping
	Season          int      `json:"season,omitempty"`    // history last-watched season (0 = n/a)
	Episode         int      `json:"episode,omitempty"`   // history last-watched episode
}

// splitTitle separates a kino.pub combined "Русское / Original" title.
func splitTitle(s string) (title, original string) {
	if i := strings.Index(s, " / "); i > 0 {
		return strings.TrimSpace(s[:i]), strings.TrimSpace(s[i+3:])
	}
	return s, ""
}

// DiscoverPage is a paginated list of items.
type DiscoverPage struct {
	Items   []DiscoverItem `json:"items"`
	Page    int            `json:"page"`
	HasMore bool           `json:"hasMore"`
	Total   int            `json:"total"`
}

// DiscoverAudio is one озвучка the user can pick before downloading.
type DiscoverAudio struct {
	Index  int    `json:"index"`
	Lang   string `json:"lang"`
	Type   string `json:"type"`
	Author string `json:"author"`
	Label  string `json:"label"`
	// Filter is the substring the download audio-preference should match
	// against the HLS track name (author when present, else type/lang).
	Filter string `json:"filter"`
}

// DiscoverEpisode is a single selectable episode.
type DiscoverEpisode struct {
	Season  int    `json:"season"`
	Episode int    `json:"episode"`
	Title   string `json:"title"`
	Watched bool   `json:"watched"`
}

// DiscoverSeason groups episodes.
type DiscoverSeason struct {
	Number   int               `json:"number"`
	Episodes []DiscoverEpisode `json:"episodes"`
}

// DiscoverDetail is the full title view.
type DiscoverDetail struct {
	DiscoverItem
	Plot         string           `json:"plot,omitempty"`
	Cast         string           `json:"cast,omitempty"`
	Countries    []string         `json:"countries,omitempty"`
	DurationMin  int              `json:"durationMin,omitempty"`
	Audios       []DiscoverAudio  `json:"audios"`
	Seasons      []DiscoverSeason `json:"seasons,omitempty"`
	EpisodeCount int              `json:"episodeCount"`
	ItemURL      string           `json:"itemUrl"`
	// Qualities are the distinct downloadable resolutions actually available for
	// this title (highest first), so the download menu shows real options instead
	// of a hardcoded list.
	Qualities []string `json:"qualities,omitempty"`
}

// DiscoverCollection is a подборка card.
type DiscoverCollection struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Poster string `json:"poster"`
}

// DiscoverBookmark is a bookmark-folder card (закладки).
type DiscoverBookmark struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Count int    `json:"count"`
}

// NamedRef is a generic {id,title} for genres etc.
type NamedRef struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// ---------------------------------------------------------------------------
// Conversion helpers
// ---------------------------------------------------------------------------

func titleNames(ns []kinopubapi.NamedID) []string {
	out := make([]string, 0, len(ns))
	for _, n := range ns {
		if n.Title != "" {
			out = append(out, n.Title)
		}
	}
	return out
}

func toDiscoverItem(it kinopubapi.Item) DiscoverItem {
	title, original := splitTitle(it.Title)
	if it.Subname != "" {
		original = it.Subname
	}
	return DiscoverItem{
		ID:              it.ID.String(),
		Type:            it.Type,
		Title:           title,
		OriginalTitle:   original,
		Year:            it.Year,
		Poster:          it.Posters.Best(),
		Director:        it.Director,
		Rating:          float64(it.RatingPercent) / 10, // kino.pub liked% → 0–10 score
		ImdbRating:      it.IMDBRating,
		KinopoiskRating: it.KinopoiskRate,
		Genres:          titleNames(it.Genres),
		IsSerial:        len(it.Seasons) > 0 || strings.Contains(it.Type, "serial") || strings.Contains(it.Type, "show"),
		Subtitle:        it.Note,
		WatchedAt:       it.WatchedAt,
		Season:          it.HistSeason,
		Episode:         it.HistEpisode,
	}
}

func toDiscoverItems(items []kinopubapi.Item) []DiscoverItem {
	out := make([]DiscoverItem, 0, len(items))
	for _, it := range items {
		out = append(out, toDiscoverItem(it))
	}
	return out
}

func audioLabel(a kinopubapi.Audio) (label, filter string) {
	var parts []string
	if a.Type.Title != "" {
		parts = append(parts, a.Type.Title)
	}
	if a.Author.Title != "" {
		parts = append(parts, a.Author.Title)
	}
	label = strings.Join(parts, " · ")
	// The HLS track NAME most reliably contains the author/studio; fall back to
	// the type, then language.
	switch {
	case a.Author.Title != "":
		filter = a.Author.Title
	case a.Type.Title != "":
		filter = a.Type.Title
	default:
		filter = a.Lang
	}
	if label == "" {
		if a.Lang != "" {
			label = a.Lang
		} else {
			label = fmt.Sprintf("Дорожка %d", a.Index)
		}
	}
	return label, filter
}

// collectAudios returns the distinct озвучки for an item (sampled from the first
// available episode/video, deduped by label).
func collectAudios(it kinopubapi.Item) []DiscoverAudio {
	var src []kinopubapi.Audio
	switch {
	case len(it.Seasons) > 0:
		for _, sea := range it.Seasons {
			for _, ep := range sea.Episodes {
				if len(ep.Audios) > 0 {
					src = ep.Audios
					break
				}
			}
			if src != nil {
				break
			}
		}
	case len(it.Videos) > 0:
		for _, v := range it.Videos {
			if len(v.Audios) > 0 {
				src = v.Audios
				break
			}
		}
	}

	seen := map[string]bool{}
	out := make([]DiscoverAudio, 0, len(src))
	for i, a := range src {
		label, filter := audioLabel(a)
		if seen[label] {
			continue
		}
		seen[label] = true
		idx := a.Index
		if idx == 0 {
			idx = i
		}
		out = append(out, DiscoverAudio{
			Index:  idx,
			Lang:   a.Lang,
			Type:   a.Type.Title,
			Author: a.Author.Title,
			Label:  label,
			Filter: filter,
		})
	}
	return out
}

func collectSeasons(it kinopubapi.Item) ([]DiscoverSeason, int) {
	var seasons []DiscoverSeason
	count := 0
	if len(it.Seasons) > 0 {
		for _, sea := range it.Seasons {
			ds := DiscoverSeason{Number: sea.Number}
			for _, ep := range sea.Episodes {
				title := ep.Title
				if title == "" {
					title = fmt.Sprintf("Серия %d", ep.Number)
				}
				ds.Episodes = append(ds.Episodes, DiscoverEpisode{Season: sea.Number, Episode: ep.Number, Title: title, Watched: ep.Watched > 0})
				count++
			}
			seasons = append(seasons, ds)
		}
		sort.Slice(seasons, func(a, b int) bool { return seasons[a].Number < seasons[b].Number })
	} else {
		count = len(it.Videos)
	}
	return seasons, count
}

// collectQualities returns the distinct downloadable quality labels available
// for an item (e.g. ["2160p","1080p","720p","480p"]), highest first. It samples
// the first episode/video with files; mixed-codec masters list each quality
// twice (H.264 + HEVC), so labels are deduped.
func collectQualities(it kinopubapi.Item) []string {
	maxH := map[string]int{}
	add := func(files []kinopubapi.File) {
		for _, f := range files {
			if f.Quality == "" {
				continue
			}
			if h, ok := maxH[f.Quality]; !ok || f.H > h {
				maxH[f.Quality] = f.H
			}
		}
	}
	if len(it.Seasons) > 0 {
		for _, s := range it.Seasons {
			for _, e := range s.Episodes {
				if len(e.Files) > 0 {
					add(e.Files)
					break
				}
			}
			if len(maxH) > 0 {
				break
			}
		}
	} else {
		for _, v := range it.Videos {
			if len(v.Files) > 0 {
				add(v.Files)
				break
			}
		}
	}
	labels := make([]string, 0, len(maxH))
	for label := range maxH {
		labels = append(labels, label)
	}
	sort.Slice(labels, func(a, b int) bool { return maxH[labels[a]] > maxH[labels[b]] })
	return labels
}

func toDiscoverDetail(it kinopubapi.Item) DiscoverDetail {
	seasons, count := collectSeasons(it)
	d := DiscoverDetail{
		DiscoverItem: toDiscoverItem(it),
		Plot:         it.Plot,
		Cast:         it.Cast,
		Countries:    titleNames(it.Countries),
		DurationMin:  int(it.Duration.Average) / 60,
		Audios:       collectAudios(it),
		Seasons:      seasons,
		EpisodeCount: count,
		ItemURL:      kinopubapi.ItemURL(it.ID.String()),
		Qualities:    collectQualities(it),
	}
	return d
}

func pageOf(p kinopubapi.ItemsPage) DiscoverPage {
	cur := p.Pagination.Current
	if cur == 0 {
		cur = 1
	}
	return DiscoverPage{
		Items:   toDiscoverItems(p.Items),
		Page:    cur,
		HasMore: p.Pagination.Total > cur,
		Total:   p.Pagination.TotalItems,
	}
}

func queryInt(r *http.Request, key string, def int) int {
	if v := r.URL.Query().Get(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func queryFloat(r *http.Request, key string, def float64) float64 {
	if v := r.URL.Query().Get(key); v != "" {
		if n, err := strconv.ParseFloat(v, 64); err == nil {
			return n
		}
	}
	return def
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

func (s *Server) handleDiscoverSearch(w http.ResponseWriter, r *http.Request) {
	client, ok := s.kpClientOrErr(w)
	if !ok {
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		writeErr(w, http.StatusBadRequest, "q is required")
		return
	}
	res, err := client.Search(r.Context(), q, queryInt(r, "page", 1))
	if err != nil {
		s.kpFail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, pageOf(res))
}

func (s *Server) handleDiscoverItems(w http.ResponseWriter, r *http.Request) {
	client, ok := s.kpClientOrErr(w)
	if !ok {
		return
	}
	s.ensureUHD(r.Context(), client)
	q := r.URL.Query()
	var conditions []string
	if q.Get("ac3") == "1" {
		conditions = append(conditions, "ac3=1")
	}
	if q.Get("subtitles") == "1" {
		conditions = append(conditions, "subtitles>=1")
	}
	res, err := client.Items(r.Context(), kinopubapi.ItemsParams{
		Type:       q.Get("type"),
		Sort:       q.Get("sort"),
		Genre:      q.Get("genre"),
		Country:    q.Get("country"),
		YearFrom:   queryInt(r, "yearFrom", 0),
		YearTo:     queryInt(r, "yearTo", 0),
		ImdbFrom:   queryFloat(r, "imdbFrom", 0),
		ImdbTo:     queryFloat(r, "imdbTo", 0),
		KpFrom:     queryFloat(r, "kpFrom", 0),
		KpTo:       queryFloat(r, "kpTo", 0),
		Conditions: conditions,
		Page:       queryInt(r, "page", 1),
		Perpage:    queryInt(r, "perpage", 0),
	})
	if err != nil {
		s.kpFail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, pageOf(res))
}

func (s *Server) handleDiscoverCollections(w http.ResponseWriter, r *http.Request) {
	client, ok := s.kpClientOrErr(w)
	if !ok {
		return
	}
	cols, err := client.Collections(r.Context(), r.URL.Query().Get("sort"), queryInt(r, "page", 1))
	if err != nil {
		s.kpFail(w, err)
		return
	}
	out := make([]DiscoverCollection, 0, len(cols))
	for _, c := range cols {
		out = append(out, DiscoverCollection{ID: c.ID.String(), Title: c.Title, Poster: c.Posters.Best()})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}

func (s *Server) handleDiscoverCountries(w http.ResponseWriter, r *http.Request) {
	client, ok := s.kpClientOrErr(w)
	if !ok {
		return
	}
	cs, err := client.Countries(r.Context())
	if err != nil {
		s.kpFail(w, err)
		return
	}
	out := make([]NamedRef, 0, len(cs))
	for _, c := range cs {
		out = append(out, NamedRef{ID: c.ID.String(), Title: c.Title})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}

func (s *Server) handleDiscoverHistory(w http.ResponseWriter, r *http.Request) {
	client, ok := s.kpClientOrErr(w)
	if !ok {
		return
	}
	res, err := client.History(r.Context(), queryInt(r, "page", 1))
	if err != nil {
		s.kpFail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, pageOf(res))
}

func (s *Server) handleDiscoverWatching(w http.ResponseWriter, r *http.Request) {
	client, ok := s.kpClientOrErr(w)
	if !ok {
		return
	}
	typ := r.URL.Query().Get("type")
	if typ == "" {
		typ = "serials"
	}
	res, err := client.Watching(r.Context(), typ, r.URL.Query().Get("subscribed") == "1", queryInt(r, "page", 1))
	if err != nil {
		s.kpFail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, pageOf(res))
}

func (s *Server) handleDiscoverCollection(w http.ResponseWriter, r *http.Request) {
	client, ok := s.kpClientOrErr(w)
	if !ok {
		return
	}
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if id == "" {
		writeErr(w, http.StatusBadRequest, "id is required")
		return
	}
	res, err := client.CollectionItems(r.Context(), id, queryInt(r, "page", 1))
	if err != nil {
		s.kpFail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, pageOf(res))
}

func (s *Server) handleDiscoverBookmarks(w http.ResponseWriter, r *http.Request) {
	client, ok := s.kpClientOrErr(w)
	if !ok {
		return
	}
	folders, err := client.Bookmarks(r.Context())
	if err != nil {
		s.kpFail(w, err)
		return
	}
	out := make([]DiscoverBookmark, 0, len(folders))
	for _, f := range folders {
		out = append(out, DiscoverBookmark{ID: f.ID.String(), Title: f.Title, Count: f.Count})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}

func (s *Server) handleDiscoverBookmark(w http.ResponseWriter, r *http.Request) {
	client, ok := s.kpClientOrErr(w)
	if !ok {
		return
	}
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if id == "" {
		writeErr(w, http.StatusBadRequest, "id is required")
		return
	}
	res, err := client.BookmarkItems(r.Context(), id, queryInt(r, "page", 1))
	if err != nil {
		s.kpFail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, pageOf(res))
}

func (s *Server) handleDiscoverGenres(w http.ResponseWriter, r *http.Request) {
	client, ok := s.kpClientOrErr(w)
	if !ok {
		return
	}
	gs, err := client.Genres(r.Context(), r.URL.Query().Get("type"))
	if err != nil {
		s.kpFail(w, err)
		return
	}
	out := make([]NamedRef, 0, len(gs))
	for _, g := range gs {
		out = append(out, NamedRef{ID: g.ID.String(), Title: g.Title})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": out})
}

func (s *Server) handleDiscoverItem(w http.ResponseWriter, r *http.Request) {
	client, ok := s.kpClientOrErr(w)
	if !ok {
		return
	}
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if id == "" {
		writeErr(w, http.StatusBadRequest, "id is required")
		return
	}
	item, err := client.Item(r.Context(), id)
	if err != nil {
		s.kpFail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toDiscoverDetail(item))
}

// ensureUHD enables 4K/HEVC for this device once (best-effort) so item
// responses include 2160p files. Cheap after the first success.
func (s *Server) ensureUHD(ctx context.Context, client *kinopubapi.Client) {
	s.uhdMu.Lock()
	done := s.uhdOK
	s.uhdMu.Unlock()
	if done {
		return
	}
	if err := client.EnableUHD(ctx); err == nil {
		s.uhdMu.Lock()
		s.uhdOK = true
		s.uhdMu.Unlock()
	}
}

// handleDiscoverStream resolves a playable HLS master-manifest URL for an item
// (optionally a specific season/episode of a serial), for the in-app player.
func (s *Server) handleDiscoverStream(w http.ResponseWriter, r *http.Request) {
	client, ok := s.kpClientOrErr(w)
	if !ok {
		return
	}
	s.ensureUHD(r.Context(), client)
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if id == "" {
		writeErr(w, http.StatusBadRequest, "id is required")
		return
	}
	item, err := client.Item(r.Context(), id)
	if err != nil {
		s.kpFail(w, err)
		return
	}
	pl, err := kinopubapi.BuildPagePlaylist(item)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	season := queryInt(r, "season", 0)
	episode := queryInt(r, "episode", 0)

	manifest := ""
	title := item.Title
	for _, ep := range pl.Episodes {
		if ep.Season == season && ep.Episode == episode {
			manifest = ep.ManifestURL
			if ep.EpisodeTitle != "" {
				title = ep.EpisodeTitle
			}
			break
		}
	}
	// No exact match (e.g. a movie, or season/episode omitted): fall back to the
	// first playable episode.
	if manifest == "" && len(pl.Episodes) > 0 {
		manifest = pl.Episodes[0].ManifestURL
	}
	if manifest == "" {
		writeErr(w, http.StatusNotFound, "no playable stream for this item")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"manifestUrl": manifest,
		"playUrl":     s.proxiedHLSURL(manifest),
		"title":       title,
	})
}

func (s *Server) handleDiscoverSimilar(w http.ResponseWriter, r *http.Request) {
	client, ok := s.kpClientOrErr(w)
	if !ok {
		return
	}
	id := strings.TrimSpace(r.URL.Query().Get("id"))
	if id == "" {
		writeErr(w, http.StatusBadRequest, "id is required")
		return
	}
	items, err := client.Similar(r.Context(), id)
	if err != nil {
		s.kpFail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": toDiscoverItems(items)})
}
