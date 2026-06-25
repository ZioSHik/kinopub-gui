package kinopubapi

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"
)

// newTestClient builds a Client pointed at srv with a long-lived access token so
// no refresh is triggered.
func newTestClient(srv *httptest.Server) *Client {
	c := New(srv.Client(), Tokens{AccessToken: "tok", Expiry: time.Now().Add(time.Hour)})
	c.host = srv.URL
	return c
}

// jsonHandler serves a fixed JSON payload and records the most recent request.
type recordingHandler struct {
	body    string
	lastReq *http.Request
	lastURL url.Values
}

func (h *recordingHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.lastReq = r
	h.lastURL = r.URL.Query()
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(h.body))
}

func TestItemsParamsValues(t *testing.T) {
	tests := []struct {
		name string
		p    ItemsParams
		want url.Values
	}{
		{
			name: "empty produces no params",
			p:    ItemsParams{},
			want: url.Values{},
		},
		{
			name: "basic string filters",
			p:    ItemsParams{Type: "movie", Sort: "views-", Genre: "5", Country: "2"},
			want: url.Values{"type": {"movie"}, "sort": {"views-"}, "genre": {"5"}, "country": {"2"}},
		},
		{
			name: "year range maps to conditions",
			p:    ItemsParams{YearFrom: 2000, YearTo: 2010},
			want: url.Values{"conditions[]": {"year>=2000", "year<=2010"}},
		},
		{
			name: "imdb/kp ratings use correct field names and trimming",
			p:    ItemsParams{ImdbFrom: 7.5, ImdbTo: 9, KpFrom: 6, KpTo: 8.25},
			want: url.Values{"conditions[]": {
				"imdb_rating>=7.5", "imdb_rating<=9",
				"kinopoisk_rating>=6", "kinopoisk_rating<=8.25",
			}},
		},
		{
			name: "ratingTo at or above 10 is dropped (means unset upper bound)",
			p:    ItemsParams{ImdbTo: 10, KpTo: 10},
			want: url.Values{},
		},
		{
			name: "extra raw conditions appended after derived ones",
			p:    ItemsParams{YearFrom: 1990, Conditions: []string{"ac3=1", "quality>=4"}},
			want: url.Values{"conditions[]": {"year>=1990", "ac3=1", "quality>=4"}},
		},
		{
			name: "pagination",
			p:    ItemsParams{Page: 3, Perpage: 50},
			want: url.Values{"page": {"3"}, "perpage": {"50"}},
		},
		{
			name: "negative/zero page and perpage dropped",
			p:    ItemsParams{Page: 0, Perpage: -1},
			want: url.Values{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.p.values()
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("values() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestTrimFloat(t *testing.T) {
	cases := map[float64]string{
		7:     "7",
		7.5:   "7.5",
		8.25:  "8.25",
		0:     "0",
		10:    "10",
		6.100: "6.1",
	}
	for in, want := range cases {
		if got := trimFloat(in); got != want {
			t.Errorf("trimFloat(%v) = %q, want %q", in, got, want)
		}
	}
}

func TestItemsRequestAndDecode(t *testing.T) {
	h := &recordingHandler{body: `{
		"items": [
			{"id": 1, "title": "A", "type": "movie", "year": 2001},
			{"id": 2, "title": "B", "type": "serial"}
		],
		"pagination": {"total": 5, "current": 1, "perpage": 25, "total_items": 120}
	}`}
	srv := httptest.NewServer(h)
	defer srv.Close()
	c := newTestClient(srv)

	page, err := c.Items(context.Background(), ItemsParams{Type: "movie", Page: 1})
	if err != nil {
		t.Fatalf("Items: %v", err)
	}
	if len(page.Items) != 2 {
		t.Fatalf("got %d items, want 2", len(page.Items))
	}
	if page.Items[0].Title != "A" || page.Items[0].Year != 2001 {
		t.Errorf("item[0] = %+v", page.Items[0])
	}
	if page.Pagination.TotalItems != 120 || page.Pagination.Total != 5 {
		t.Errorf("pagination = %+v", page.Pagination)
	}
	// Request must hit /v1/items with the access token and type filter.
	if !strings.HasSuffix(h.lastReq.URL.Path, "/v1/items") {
		t.Errorf("path = %q", h.lastReq.URL.Path)
	}
	if h.lastURL.Get("access_token") != "tok" {
		t.Errorf("access_token = %q", h.lastURL.Get("access_token"))
	}
	if h.lastURL.Get("type") != "movie" {
		t.Errorf("type = %q", h.lastURL.Get("type"))
	}
}

func TestSearchTrimsAndSetsField(t *testing.T) {
	h := &recordingHandler{body: `{"items":[{"id":1,"title":"X"}]}`}
	srv := httptest.NewServer(h)
	defer srv.Close()
	c := newTestClient(srv)

	if _, err := c.Search(context.Background(), "  matrix  ", 2); err != nil {
		t.Fatalf("Search: %v", err)
	}
	if !strings.HasSuffix(h.lastReq.URL.Path, "/v1/items/search") {
		t.Errorf("path = %q", h.lastReq.URL.Path)
	}
	if h.lastURL.Get("q") != "matrix" {
		t.Errorf("q = %q, want trimmed 'matrix'", h.lastURL.Get("q"))
	}
	if h.lastURL.Get("field") != "title" {
		t.Errorf("field = %q, want title", h.lastURL.Get("field"))
	}
	if h.lastURL.Get("page") != "2" {
		t.Errorf("page = %q", h.lastURL.Get("page"))
	}
}

func TestItemUnwrapsAndEscapesID(t *testing.T) {
	h := &recordingHandler{body: `{"item":{"id":42,"title":"Deep","type":"movie","videos":[{"number":1}]}}`}
	srv := httptest.NewServer(h)
	defer srv.Close()
	c := newTestClient(srv)

	item, err := c.Item(context.Background(), "42")
	if err != nil {
		t.Fatalf("Item: %v", err)
	}
	if item.Title != "Deep" || item.ID.String() != "42" {
		t.Errorf("item = %+v", item)
	}
	if !strings.HasSuffix(h.lastReq.URL.Path, "/v1/items/42") {
		t.Errorf("path = %q", h.lastReq.URL.Path)
	}
}

func TestSimilarUnwrapsItems(t *testing.T) {
	h := &recordingHandler{body: `{"items":[{"id":1},{"id":2},{"id":3}]}`}
	srv := httptest.NewServer(h)
	defer srv.Close()
	c := newTestClient(srv)

	items, err := c.Similar(context.Background(), "5")
	if err != nil {
		t.Fatalf("Similar: %v", err)
	}
	if len(items) != 3 {
		t.Fatalf("got %d, want 3", len(items))
	}
	if h.lastURL.Get("id") != "5" {
		t.Errorf("id = %q", h.lastURL.Get("id"))
	}
}

func TestCollections(t *testing.T) {
	h := &recordingHandler{body: `{"items":[{"id":1,"title":"Best"},{"id":2,"title":"New"}]}`}
	srv := httptest.NewServer(h)
	defer srv.Close()
	c := newTestClient(srv)

	cols, err := c.Collections(context.Background(), "views-", 3)
	if err != nil {
		t.Fatalf("Collections: %v", err)
	}
	if len(cols) != 2 || cols[0].Title != "Best" {
		t.Fatalf("cols = %+v", cols)
	}
	if h.lastURL.Get("sort") != "views-" || h.lastURL.Get("page") != "3" {
		t.Errorf("query = %v", h.lastURL)
	}
	// Empty sort and page<=0 must not set those params.
	if _, err := c.Collections(context.Background(), "", 0); err != nil {
		t.Fatalf("Collections: %v", err)
	}
	if _, has := h.lastURL["sort"]; has {
		t.Errorf("sort should be absent, got %v", h.lastURL)
	}
	if _, has := h.lastURL["page"]; has {
		t.Errorf("page should be absent, got %v", h.lastURL)
	}
}

func TestMarkTimeDefaultsAndParams(t *testing.T) {
	h := &recordingHandler{body: `{"status":200}`}
	srv := httptest.NewServer(h)
	defer srv.Close()
	c := newTestClient(srv)

	// video<=0 defaults to 1; season 0 omitted.
	if err := c.MarkTime(context.Background(), "10", 0, 0, 123); err != nil {
		t.Fatalf("MarkTime: %v", err)
	}
	if h.lastURL.Get("video") != "1" {
		t.Errorf("video = %q, want defaulted 1", h.lastURL.Get("video"))
	}
	if _, has := h.lastURL["season"]; has {
		t.Errorf("season should be omitted for movie, got %v", h.lastURL)
	}
	if h.lastURL.Get("time") != "123" || h.lastURL.Get("id") != "10" {
		t.Errorf("query = %v", h.lastURL)
	}

	// season>0 included; video preserved.
	if err := c.MarkTime(context.Background(), "10", 4, 2, 60); err != nil {
		t.Fatalf("MarkTime: %v", err)
	}
	if h.lastURL.Get("season") != "2" || h.lastURL.Get("video") != "4" {
		t.Errorf("query = %v", h.lastURL)
	}
}

func TestCountriesAndGenres(t *testing.T) {
	h := &recordingHandler{body: `{"items":[{"id":1,"title":"USA"},{"id":2,"title":"UK"}]}`}
	srv := httptest.NewServer(h)
	defer srv.Close()
	c := newTestClient(srv)

	cs, err := c.Countries(context.Background())
	if err != nil {
		t.Fatalf("Countries: %v", err)
	}
	if len(cs) != 2 || cs[0].Title != "USA" {
		t.Fatalf("countries = %+v", cs)
	}

	gs, err := c.Genres(context.Background(), "movie")
	if err != nil {
		t.Fatalf("Genres: %v", err)
	}
	if len(gs) != 2 {
		t.Fatalf("genres = %+v", gs)
	}
	if h.lastURL.Get("type") != "movie" {
		t.Errorf("type = %q", h.lastURL.Get("type"))
	}
	// Empty type omits the param.
	if _, err := c.Genres(context.Background(), ""); err != nil {
		t.Fatalf("Genres: %v", err)
	}
	if _, has := h.lastURL["type"]; has {
		t.Errorf("type should be absent, got %v", h.lastURL)
	}
}

func TestHistoryFlatItems(t *testing.T) {
	h := &recordingHandler{body: `{"items":[{"id":1,"title":"Flat"}],"pagination":{"total":2}}`}
	srv := httptest.NewServer(h)
	defer srv.Close()
	c := newTestClient(srv)

	page, err := c.History(context.Background(), 1)
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].Title != "Flat" {
		t.Fatalf("items = %+v", page.Items)
	}
	if page.Pagination.Total != 2 {
		t.Errorf("pagination = %+v", page.Pagination)
	}
}

func TestHistoryWrappedShape(t *testing.T) {
	// No flat items; history[] wrappers must be unwrapped with media/last_seen.
	h := &recordingHandler{body: `{
		"history": [
			{"item": {"id": 9, "title": "Wrapped"}, "last_seen": 1700000000, "media": {"number": 5, "snumber": 2}}
		],
		"pagination": {"total": 1}
	}`}
	srv := httptest.NewServer(h)
	defer srv.Close()
	c := newTestClient(srv)

	page, err := c.History(context.Background(), 0)
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("got %d items, want 1", len(page.Items))
	}
	it := page.Items[0]
	if it.Title != "Wrapped" || it.WatchedAt != 1700000000 || it.HistSeason != 2 || it.HistEpisode != 5 {
		t.Errorf("unwrapped item = %+v", it)
	}
}

func TestHistoryPrefersFlatOverWrapped(t *testing.T) {
	// When both present, flat items win (wrapped ignored).
	h := &recordingHandler{body: `{
		"items": [{"id": 1, "title": "Flat"}],
		"history": [{"item": {"id": 2, "title": "Wrapped"}}]
	}`}
	srv := httptest.NewServer(h)
	defer srv.Close()
	c := newTestClient(srv)

	page, err := c.History(context.Background(), 0)
	if err != nil {
		t.Fatalf("History: %v", err)
	}
	if len(page.Items) != 1 || page.Items[0].Title != "Flat" {
		t.Fatalf("items = %+v", page.Items)
	}
}

func TestWatchingDefaultsTypeAndBuildsNote(t *testing.T) {
	h := &recordingHandler{body: `{"items":[
		{"id":1,"title":"S","total":10,"watched":7,"new":2},
		{"id":2,"title":"NoTotal"}
	]}`}
	srv := httptest.NewServer(h)
	defer srv.Close()
	c := newTestClient(srv)

	page, err := c.Watching(context.Background(), "", true, 2)
	if err != nil {
		t.Fatalf("Watching: %v", err)
	}
	// "" type defaults to serials → path /v1/watching/serials.
	if !strings.HasSuffix(h.lastReq.URL.Path, "/v1/watching/serials") {
		t.Errorf("path = %q, want serials default", h.lastReq.URL.Path)
	}
	if h.lastURL.Get("subscribed") != "1" || h.lastURL.Get("page") != "2" {
		t.Errorf("query = %v", h.lastURL)
	}
	if page.Items[0].Note != "7/10 · +2" {
		t.Errorf("note = %q, want '7/10 · +2'", page.Items[0].Note)
	}
	// Item with no total gets no note.
	if page.Items[1].Note != "" {
		t.Errorf("note for no-total item = %q, want empty", page.Items[1].Note)
	}
}

func TestWatchingNoNewSuffix(t *testing.T) {
	h := &recordingHandler{body: `{"items":[{"id":1,"total":5,"watched":5,"new":0}]}`}
	srv := httptest.NewServer(h)
	defer srv.Close()
	c := newTestClient(srv)

	page, err := c.Watching(context.Background(), "movies", false, 0)
	if err != nil {
		t.Fatalf("Watching: %v", err)
	}
	if !strings.HasSuffix(h.lastReq.URL.Path, "/v1/watching/movies") {
		t.Errorf("path = %q", h.lastReq.URL.Path)
	}
	if _, has := h.lastURL["subscribed"]; has {
		t.Errorf("subscribed should be absent when false, got %v", h.lastURL)
	}
	if page.Items[0].Note != "5/5" {
		t.Errorf("note = %q, want '5/5' (no +new)", page.Items[0].Note)
	}
}

func TestCollectionItemsAndBookmarkItems(t *testing.T) {
	h := &recordingHandler{body: `{"items":[{"id":1}],"pagination":{"total":1}}`}
	srv := httptest.NewServer(h)
	defer srv.Close()
	c := newTestClient(srv)

	if _, err := c.CollectionItems(context.Background(), "77", 2); err != nil {
		t.Fatalf("CollectionItems: %v", err)
	}
	if !strings.HasSuffix(h.lastReq.URL.Path, "/v1/collections/view") {
		t.Errorf("path = %q", h.lastReq.URL.Path)
	}
	if h.lastURL.Get("id") != "77" || h.lastURL.Get("page") != "2" {
		t.Errorf("query = %v", h.lastURL)
	}

	if _, err := c.BookmarkItems(context.Background(), "fold/er", 0); err != nil {
		t.Fatalf("BookmarkItems: %v", err)
	}
	// folderID must be path-escaped.
	if !strings.Contains(h.lastReq.URL.Path, "fold/er") && !strings.Contains(h.lastReq.URL.EscapedPath(), "fold%2Fer") {
		t.Errorf("escaped path = %q", h.lastReq.URL.EscapedPath())
	}
}

func TestBookmarks(t *testing.T) {
	h := &recordingHandler{body: `{"items":[{"id":1,"title":"Fav","count":12}]}`}
	srv := httptest.NewServer(h)
	defer srv.Close()
	c := newTestClient(srv)

	folders, err := c.Bookmarks(context.Background())
	if err != nil {
		t.Fatalf("Bookmarks: %v", err)
	}
	if len(folders) != 1 || folders[0].Count != 12 || folders[0].Title != "Fav" {
		t.Fatalf("folders = %+v", folders)
	}
}

func TestNotifyOmitsEmptyFields(t *testing.T) {
	h := &recordingHandler{body: `{}`}
	srv := httptest.NewServer(h)
	defer srv.Close()
	c := newTestClient(srv)

	if err := c.Notify(context.Background(), DeviceInfo{Title: "kinopub-gui", Software: "0.1.5"}); err != nil {
		t.Fatalf("Notify: %v", err)
	}
	if h.lastURL.Get("title") != "kinopub-gui" || h.lastURL.Get("software") != "0.1.5" {
		t.Errorf("query = %v", h.lastURL)
	}
	if _, has := h.lastURL["hardware"]; has {
		t.Errorf("empty hardware should be omitted, got %v", h.lastURL)
	}
}

func TestUserDecodesSubscriptionDays(t *testing.T) {
	// Days is a json.Number so a fractional value must decode without error.
	h := &recordingHandler{body: `{"user":{
		"username":"bob",
		"reg_date":1600000000,
		"subscription":{"active":true,"end_time":1800000000,"days":12.5},
		"profile":{"name":"Bob","avatar":"a.png"}
	}}`}
	srv := httptest.NewServer(h)
	defer srv.Close()
	c := newTestClient(srv)

	u, err := c.User(context.Background())
	if err != nil {
		t.Fatalf("User: %v", err)
	}
	if u.Username != "bob" || !u.Subscription.Active {
		t.Errorf("user = %+v", u)
	}
	if u.Subscription.Days.String() != "12.5" {
		t.Errorf("days = %q, want 12.5", u.Subscription.Days.String())
	}
	if u.Profile.Name != "Bob" || u.Profile.Avatar != "a.png" {
		t.Errorf("profile = %+v", u.Profile)
	}
}

func TestHTTPErrorStatusSurfacesSnippet(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("boom internal"))
	}))
	defer srv.Close()
	c := newTestClient(srv)

	_, err := c.Items(context.Background(), ItemsParams{})
	if err == nil {
		t.Fatal("expected error on HTTP 500")
	}
	if !strings.Contains(err.Error(), "HTTP 500") || !strings.Contains(err.Error(), "boom internal") {
		t.Errorf("error = %v, want HTTP 500 + snippet", err)
	}
}

func TestDecodeErrorOnMalformedBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not json"))
	}))
	defer srv.Close()
	c := newTestClient(srv)

	_, err := c.Items(context.Background(), ItemsParams{})
	if err == nil || !strings.Contains(err.Error(), "decode") {
		t.Fatalf("error = %v, want decode error", err)
	}
}

func TestNotAuthenticatedShortCircuits(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("server should not be hit without a token")
	}))
	defer srv.Close()
	c := New(srv.Client(), Tokens{})
	c.host = srv.URL

	_, err := c.Items(context.Background(), ItemsParams{})
	if err != ErrNotAuthenticated {
		t.Fatalf("error = %v, want ErrNotAuthenticated", err)
	}
}

func TestGet401TriggersRefreshAndRetry(t *testing.T) {
	var hits int
	mux := http.NewServeMux()
	mux.HandleFunc("/oauth2/token", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "fresh", "refresh_token": "r2", "expires_in": 3600})
	})
	mux.HandleFunc("/v1/items", func(w http.ResponseWriter, r *http.Request) {
		hits++
		if r.URL.Query().Get("access_token") == "stale" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"items": []any{}})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := New(srv.Client(), Tokens{AccessToken: "stale", RefreshToken: "r1", Expiry: time.Now().Add(time.Hour)})
	c.host = srv.URL

	if _, err := c.Items(context.Background(), ItemsParams{}); err != nil {
		t.Fatalf("Items: %v", err)
	}
	if hits != 2 {
		t.Errorf("items endpoint hit %d times, want 2 (initial 401 + retry)", hits)
	}
	if c.Tokens().AccessToken != "fresh" {
		t.Errorf("token after retry = %q, want fresh", c.Tokens().AccessToken)
	}
}

func TestEnableUHDAlreadyEnabledNoWrite(t *testing.T) {
	var posted bool
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/device/info", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"device":{"id":"55"}}`))
	})
	mux.HandleFunc("/v1/device/55/settings", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			posted = true
			return
		}
		_, _ = w.Write([]byte(`{"settings":{"support4k":{"value":1}}}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := newTestClient(srv)

	if err := c.EnableUHD(context.Background()); err != nil {
		t.Fatalf("EnableUHD: %v", err)
	}
	if posted {
		t.Error("EnableUHD should not POST when support4k already enabled")
	}
}

func TestEnableUHDWritesWithPreservedSelections(t *testing.T) {
	var form url.Values
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/device/info", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"device":{"id":"7"}}`))
	})
	mux.HandleFunc("/v1/device/7/settings", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			_ = r.ParseForm()
			form = r.PostForm
			_, _ = w.Write([]byte(`{}`))
			return
		}
		_, _ = w.Write([]byte(`{"settings":{
			"support4k":{"value":0},
			"serverLocation":{"value":[{"id":"3","selected":1},{"id":"9","selected":0}]},
			"streamingType":{"value":[{"id":"4","selected":0}]}
		}}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := newTestClient(srv)

	if err := c.EnableUHD(context.Background()); err != nil {
		t.Fatalf("EnableUHD: %v", err)
	}
	if form == nil {
		t.Fatal("expected a POST when support4k is off")
	}
	if form.Get("support4k") != "1" || form.Get("supportHevc") != "1" || form.Get("mixedPlaylist") != "1" {
		t.Errorf("4k flags wrong: %v", form)
	}
	// serverLocation has a selected id 3 → must be preserved.
	if form.Get("serverLocation") != "3" {
		t.Errorf("serverLocation = %q, want preserved 3", form.Get("serverLocation"))
	}
	// streamingType has no selected → defaults to "4" (HLS4).
	if form.Get("streamingType") != "4" {
		t.Errorf("streamingType = %q, want default 4", form.Get("streamingType"))
	}
}

func TestEnableUHDNoDeviceID(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/device/info", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"device":{}}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	c := newTestClient(srv)

	if err := c.EnableUHD(context.Background()); err == nil || !strings.Contains(err.Error(), "no device id") {
		t.Fatalf("error = %v, want no device id", err)
	}
}

func TestListSettingSelectedID(t *testing.T) {
	l := listSetting{Value: []struct {
		ID       json.Number `json:"id"`
		Selected int         `json:"selected"`
	}{
		{ID: json.Number("1"), Selected: 0},
		{ID: json.Number("8"), Selected: 1},
	}}
	if got := l.selectedID("99"); got != "8" {
		t.Errorf("selectedID = %q, want 8", got)
	}
	empty := listSetting{}
	if got := empty.selectedID("99"); got != "99" {
		t.Errorf("selectedID empty = %q, want default 99", got)
	}
}

func TestRawPostFormEncoding(t *testing.T) {
	var gotBody string
	var gotCT string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := readAll(r)
		gotBody = b
		gotCT = r.Header.Get("Content-Type")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()
	c := newTestClient(srv)

	var out struct {
		OK bool `json:"ok"`
	}
	form := url.Values{"a": {"1"}, "b": {"two"}}
	if err := c.RawPost(context.Background(), "device/1/settings", form, &out); err != nil {
		t.Fatalf("RawPost: %v", err)
	}
	if !out.OK {
		t.Error("response not decoded")
	}
	if gotCT != "application/x-www-form-urlencoded" {
		t.Errorf("content-type = %q", gotCT)
	}
	want := form.Encode()
	if gotBody != want {
		t.Errorf("body = %q, want %q", gotBody, want)
	}
}

func TestRawPostHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("denied"))
	}))
	defer srv.Close()
	c := newTestClient(srv)

	err := c.RawPost(context.Background(), "x", url.Values{}, nil)
	if err == nil || !strings.Contains(err.Error(), "HTTP 403") {
		t.Fatalf("error = %v, want HTTP 403", err)
	}
}

func TestRawPostNilOutSkipsDecode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("garbage-not-json"))
	}))
	defer srv.Close()
	c := newTestClient(srv)

	// out=nil → no decode attempted → no error even with non-JSON body.
	if err := c.RawPost(context.Background(), "x", nil, nil); err != nil {
		t.Fatalf("RawPost with nil out: %v", err)
	}
}

func TestRawDelegatesToGet(t *testing.T) {
	h := &recordingHandler{body: `{"hello":"world"}`}
	srv := httptest.NewServer(h)
	defer srv.Close()
	c := newTestClient(srv)

	var out map[string]string
	if err := c.Raw(context.Background(), "probe", url.Values{"k": {"v"}}, &out); err != nil {
		t.Fatalf("Raw: %v", err)
	}
	if out["hello"] != "world" {
		t.Errorf("out = %v", out)
	}
	if h.lastURL.Get("k") != "v" {
		t.Errorf("query k = %q", h.lastURL.Get("k"))
	}
}

// readAll reads a request body fully.
func readAll(r *http.Request) (string, error) {
	defer r.Body.Close()
	b, err := io.ReadAll(r.Body)
	return string(b), err
}
