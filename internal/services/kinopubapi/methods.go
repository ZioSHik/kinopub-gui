package kinopubapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// Pagination mirrors the API's pagination block.
type Pagination struct {
	Total      int `json:"total"`
	Current    int `json:"current"`
	Perpage    int `json:"perpage"`
	TotalItems int `json:"total_items"`
}

// ItemsPage is a page of catalog items.
type ItemsPage struct {
	Items      []Item     `json:"items"`
	Pagination Pagination `json:"pagination"`
}

// ItemsParams filters the catalog (/v1/items). Range fields use 0 to mean
// "unset". Conditions carries extra raw filter expressions (e.g. "ac3=1").
type ItemsParams struct {
	Type    string // movie, serial, 4k, 3d, concert, documovie, docuserial, tvshow ("" = all)
	Sort    string // e.g. "updated-", "views-", "created-", "watchers-", "year-", "kinopoisk-", "imdb-"
	Genre   string // genre id
	Country string // country id

	YearFrom, YearTo int     // inclusive year range (0 = unset)
	ImdbFrom, ImdbTo float64 // IMDb rating range (0 = unset)
	KpFrom, KpTo     float64 // Kinopoisk rating range (0 = unset)

	Conditions []string // extra raw conditions (e.g. "ac3=1", "quality>=4")

	Page    int
	Perpage int
}

func (p ItemsParams) values() url.Values {
	q := url.Values{}
	if p.Type != "" {
		q.Set("type", p.Type)
	}
	if p.Sort != "" {
		q.Set("sort", p.Sort)
	}
	if p.Genre != "" {
		q.Set("genre", p.Genre)
	}
	if p.Country != "" {
		q.Set("country", p.Country)
	}

	// Range filters go through the conditions[] array (kino.pub filter syntax).
	var cond []string
	if p.YearFrom > 0 {
		cond = append(cond, "year>="+strconv.Itoa(p.YearFrom))
	}
	if p.YearTo > 0 {
		cond = append(cond, "year<="+strconv.Itoa(p.YearTo))
	}
	// Field names verified against the live API: imdb_rating / kinopoisk_rating
	// (NOT imdb / kinopoisk), and only the conditions[] array form filters.
	if p.ImdbFrom > 0 {
		cond = append(cond, "imdb_rating>="+trimFloat(p.ImdbFrom))
	}
	if p.ImdbTo > 0 && p.ImdbTo < 10 {
		cond = append(cond, "imdb_rating<="+trimFloat(p.ImdbTo))
	}
	if p.KpFrom > 0 {
		cond = append(cond, "kinopoisk_rating>="+trimFloat(p.KpFrom))
	}
	if p.KpTo > 0 && p.KpTo < 10 {
		cond = append(cond, "kinopoisk_rating<="+trimFloat(p.KpTo))
	}
	cond = append(cond, p.Conditions...)
	for _, c := range cond {
		q.Add("conditions[]", c)
	}

	if p.Page > 0 {
		q.Set("page", strconv.Itoa(p.Page))
	}
	if p.Perpage > 0 {
		q.Set("perpage", strconv.Itoa(p.Perpage))
	}
	return q
}

func trimFloat(f float64) string {
	return strconv.FormatFloat(f, 'f', -1, 64)
}

// Items lists/filters the catalog. Tops are expressed via Sort:
// popular = "views-", fresh = "created-", hot serials = "watchers-".
func (c *Client) Items(ctx context.Context, p ItemsParams) (ItemsPage, error) {
	var out ItemsPage
	err := c.get(ctx, "items", p.values(), &out)
	return out, err
}

// Search finds items by title.
func (c *Client) Search(ctx context.Context, query string, page int) (ItemsPage, error) {
	q := url.Values{}
	q.Set("q", strings.TrimSpace(query))
	q.Set("field", "title")
	if page > 0 {
		q.Set("page", strconv.Itoa(page))
	}
	var out ItemsPage
	err := c.get(ctx, "items/search", q, &out)
	return out, err
}

// Item fetches a single item's full details (videos/seasons, files, audios).
func (c *Client) Item(ctx context.Context, id string) (Item, error) {
	var out struct {
		Item Item `json:"item"`
	}
	err := c.get(ctx, "items/"+url.PathEscape(id), nil, &out)
	return out.Item, err
}

// Similar returns items similar to the given id.
func (c *Client) Similar(ctx context.Context, id string) ([]Item, error) {
	q := url.Values{}
	q.Set("id", id)
	var out struct {
		Items []Item `json:"items"`
	}
	err := c.get(ctx, "items/similar", q, &out)
	return out.Items, err
}

// Collections lists curated подборки. sort is one of "created-" (new),
// "views-" (popular), "watchers-" (most watched); "" defaults to updated.
func (c *Client) Collections(ctx context.Context, sort string, page int) ([]Collection, error) {
	q := url.Values{}
	if sort != "" {
		q.Set("sort", sort)
	}
	if page > 0 {
		q.Set("page", strconv.Itoa(page))
	}
	var out struct {
		Items []Collection `json:"items"`
	}
	err := c.get(ctx, "collections", q, &out)
	return out.Items, err
}

// Countries lists countries for the filter (mirrors Genres).
func (c *Client) Countries(ctx context.Context) ([]NamedID, error) {
	var out struct {
		Items []NamedID `json:"items"`
	}
	err := c.get(ctx, "countries", nil, &out)
	return out.Items, err
}

// History returns the account's watch history (most recent first). The endpoint
// may return either a flat `items` array or a `history` array of {item:…}
// wrappers, so both shapes are accepted.
func (c *Client) History(ctx context.Context, page int) (ItemsPage, error) {
	q := url.Values{}
	if page > 0 {
		q.Set("page", strconv.Itoa(page))
	}
	var raw struct {
		Items   []Item `json:"items"`
		History []struct {
			Item     Item  `json:"item"`
			LastSeen int64 `json:"last_seen"`
			Media    struct {
				Number  int `json:"number"`
				Snumber int `json:"snumber"`
			} `json:"media"`
		} `json:"history"`
		Pagination Pagination `json:"pagination"`
	}
	if err := c.get(ctx, "history", q, &raw); err != nil {
		return ItemsPage{}, err
	}
	out := ItemsPage{Pagination: raw.Pagination, Items: raw.Items}
	if len(out.Items) == 0 {
		for _, h := range raw.History {
			it := h.Item
			it.WatchedAt = h.LastSeen
			it.HistSeason = h.Media.Snumber
			it.HistEpisode = h.Media.Number
			out.Items = append(out.Items, it)
		}
	}
	return out, nil
}

// Watching returns the account's watching list. typ is "serials" or "movies";
// when subscribed is true, only subscribed series are returned.
func (c *Client) Watching(ctx context.Context, typ string, subscribed bool, page int) (ItemsPage, error) {
	if typ == "" {
		typ = "serials"
	}
	q := url.Values{}
	if subscribed {
		q.Set("subscribed", "1")
	}
	if page > 0 {
		q.Set("page", strconv.Itoa(page))
	}
	var out ItemsPage
	if err := c.get(ctx, "watching/"+typ, q, &out); err != nil {
		return ItemsPage{}, err
	}
	for i := range out.Items {
		if it := &out.Items[i]; it.Total > 0 {
			it.Note = fmt.Sprintf("%d/%d", it.Watched, it.Total)
			if it.New > 0 {
				it.Note += fmt.Sprintf(" · +%d", it.New)
			}
		}
	}
	return out, nil
}

// CollectionItems returns the items inside a collection.
func (c *Client) CollectionItems(ctx context.Context, id string, page int) (ItemsPage, error) {
	q := url.Values{}
	q.Set("id", id)
	if page > 0 {
		q.Set("page", strconv.Itoa(page))
	}
	var out ItemsPage
	err := c.get(ctx, "collections/view", q, &out)
	return out, err
}

// Bookmarks lists the account's bookmark folders (закладки).
func (c *Client) Bookmarks(ctx context.Context) ([]BookmarkFolder, error) {
	var out struct {
		Items []BookmarkFolder `json:"items"`
	}
	err := c.get(ctx, "bookmarks", nil, &out)
	return out.Items, err
}

// BookmarkItems returns the items inside a bookmark folder.
func (c *Client) BookmarkItems(ctx context.Context, folderID string, page int) (ItemsPage, error) {
	q := url.Values{}
	if page > 0 {
		q.Set("page", strconv.Itoa(page))
	}
	var out ItemsPage
	err := c.get(ctx, "bookmarks/"+url.PathEscape(folderID), q, &out)
	return out, err
}

// Genres lists genres, optionally for a given content type.
func (c *Client) Genres(ctx context.Context, typ string) ([]NamedID, error) {
	q := url.Values{}
	if typ != "" {
		q.Set("type", typ)
	}
	var out struct {
		Items []NamedID `json:"items"`
	}
	err := c.get(ctx, "genres", q, &out)
	return out.Items, err
}

// DeviceInfo identifies this client in the account's device list on kino.pub.
type DeviceInfo struct {
	Title    string // friendly device name (e.g. "kinopub-gui (hostname)")
	Hardware string // e.g. the OS / machine
	Software string // e.g. "kinopub-gui 0.1.5"
}

// Notify registers/updates this device's identity (recommended right after
// login) so it shows up named — not "unknown" — in the account's device list.
// Errors are non-fatal and may be ignored by the caller.
func (c *Client) Notify(ctx context.Context, info DeviceInfo) error {
	q := url.Values{}
	if info.Title != "" {
		q.Set("title", info.Title)
	}
	if info.Hardware != "" {
		q.Set("hardware", info.Hardware)
	}
	if info.Software != "" {
		q.Set("software", info.Software)
	}
	var out map[string]any
	return c.get(ctx, "device/notify", q, &out)
}

// User is the authenticated account profile (kino.pub GET /v1/user → "user").
type User struct {
	Username     string           `json:"username"`
	RegDate      int64            `json:"reg_date"`
	Subscription UserSubscription `json:"subscription"`
	Profile      UserProfile      `json:"profile"`
}

// UserSubscription describes the account's premium status and remaining time.
// Days may be fractional, so it is decoded as a number.
type UserSubscription struct {
	Active  bool        `json:"active"`
	EndTime int64       `json:"end_time"`
	Days    json.Number `json:"days"`
}

// UserProfile holds the account's display name and avatar URL.
type UserProfile struct {
	Name   string `json:"name"`
	Avatar string `json:"avatar"`
}

// User returns the authenticated account profile, including subscription status
// and remaining days (kino.pub GET /v1/user).
func (c *Client) User(ctx context.Context) (User, error) {
	var out struct {
		User User `json:"user"`
	}
	err := c.get(ctx, "user", nil, &out)
	return out.User, err
}

// EnableUHD turns on 4K/HEVC/HDR + mixed-playlist support for this device so the
// API includes 2160p files (in both HEVC and H.264) in item responses. kino.pub
// gates 4K behind these per-device flags — exactly the "4K support" checkbox the
// official apps expose. The current server/streaming-type selections are
// preserved. Safe to call repeatedly; it is a no-op once already enabled.
func (c *Client) EnableUHD(ctx context.Context) error {
	var info struct {
		Device struct {
			ID json.Number `json:"id"`
		} `json:"device"`
	}
	if err := c.get(ctx, "device/info", nil, &info); err != nil {
		return err
	}
	id := info.Device.ID.String()
	if id == "" {
		return fmt.Errorf("kino.pub: no device id")
	}

	// Read current settings to preserve list selections and avoid a needless
	// write when 4K is already on.
	var cur struct {
		Settings struct {
			Support4k      flagSetting `json:"support4k"`
			ServerLocation listSetting `json:"serverLocation"`
			StreamingType  listSetting `json:"streamingType"`
		} `json:"settings"`
	}
	if err := c.get(ctx, "device/"+id+"/settings", nil, &cur); err != nil {
		return err
	}
	if cur.Settings.Support4k.Value == 1 {
		return nil // already enabled
	}

	form := url.Values{}
	form.Set("supportSsl", "1")
	form.Set("supportHevc", "1")
	form.Set("supportHdr", "1")
	form.Set("support4k", "1")
	form.Set("mixedPlaylist", "1")
	form.Set("serverLocation", cur.Settings.ServerLocation.selectedID("1"))
	form.Set("streamingType", cur.Settings.StreamingType.selectedID("4")) // 4 = HLS4
	return c.RawPost(ctx, "device/"+id+"/settings", form, nil)
}

type flagSetting struct {
	Value int `json:"value"`
}

type listSetting struct {
	Value []struct {
		ID       json.Number `json:"id"`
		Selected int         `json:"selected"`
	} `json:"value"`
}

func (l listSetting) selectedID(def string) string {
	for _, v := range l.Value {
		if v.Selected == 1 {
			return v.ID.String()
		}
	}
	return def
}

// Raw performs a raw GET against the given API path and decodes into out. Used
// for probing endpoints (device/user settings) that lack a typed wrapper.
func (c *Client) Raw(ctx context.Context, path string, q url.Values, out any) error {
	return c.get(ctx, path, q, out)
}

// RawPost performs an authenticated POST with a form-encoded body (kino.pub's
// settings endpoints expect form fields) and decodes the JSON response.
func (c *Client) RawPost(ctx context.Context, path string, form url.Values, out any) error {
	token, err := c.ensureToken(ctx)
	if err != nil {
		return err
	}
	reqURL := c.host + "/v1/" + strings.TrimPrefix(path, "/") + "?access_token=" + url.QueryEscape(token)
	var bodyStr string
	if form != nil {
		bodyStr = form.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, strings.NewReader(bodyStr))
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("kino.pub API %s: %w", path, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode >= 400 {
		return fmt.Errorf("kino.pub API %s: HTTP %d: %s", path, resp.StatusCode, snippet(body))
	}
	if out != nil {
		if err := json.Unmarshal(body, out); err != nil {
			return fmt.Errorf("kino.pub API %s: decode: %w", path, err)
		}
	}
	return nil
}
