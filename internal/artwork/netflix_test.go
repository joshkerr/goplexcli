package artwork

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// netflixJSQuote escapes JSON text the way it appears inside a JavaScript
// single-quoted string literal.
func netflixJSQuote(s string) string {
	return strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(s)
}

// netflixTestGraphQL is the embedded cache for the title page: the page's
// own Movie record with two BILLBOARD renditions of one asset, a BOXSHOT,
// the title logo, the "N" brand badge, a keyless placeholder, plus a
// recommendation tile that only has a boxshot.
const netflixTestGraphQL = `{"data":{
"Movie:{\"videoId\":81234567}":{
 "__typename":"Movie",
 "title":"The Example's Story",
 "artwork({\"format\":\"JPG\",\"params\":{\"artworkType\":\"BILLBOARD\",\"dimension\":{\"width\":1280}}})":{"key":"a|asset-bb|1280","url":"https://occ.example/bb-1280.jpg?x=1&y=2"},
 "artwork({\"format\":\"JPG\",\"params\":{\"artworkType\":\"BILLBOARD\",\"dimension\":{\"width\":1920}}})":{"key":"a|asset-bb|1920","url":"https://occ.example/bb-1920.jpg"},
 "artwork({\"format\":\"JPG\",\"params\":{\"artworkType\":\"BOXSHOT\",\"dimension\":{\"width\":342}}})":{"key":"a|asset-bs|342","url":"https://occ.example/bs-342.jpg"},
 "artwork({\"format\":\"PNG\",\"params\":{\"artworkType\":\"LOGO_HORIZONTAL_CROPPED\",\"dimension\":{\"width\":550}}})":{"key":"a|asset-logo|550","url":"https://occ.example/logo.png"},
 "artwork({\"format\":\"PNG\",\"params\":{\"artworkType\":\"BRAND_LOGO\",\"dimension\":{\"width\":100}}})":{"key":"a|asset-brand|100","url":"https://occ.example/brand.png"},
 "artwork({\"format\":\"JPG\",\"params\":{\"artworkType\":\"ECLIPSE_BILLBOARD_REDUX\",\"dimension\":{\"width\":1920}}})":{"key":"","url":"https://occ.example/placeholder.jpg"}
},
"Movie:{\"videoId\":99}":{
 "__typename":"Movie","title":"Recommended",
 "artwork({\"format\":\"JPG\",\"params\":{\"artworkType\":\"BOXSHOT\",\"dimension\":{\"width\":342}}})":{"key":"a|asset-rec|342","url":"https://occ.example/rec.jpg"}
}}}`

func netflixTestPage(withGraphQL bool) string {
	var b strings.Builder
	b.WriteString(`<!DOCTYPE html><html><head>
<meta charset="utf-8">
<meta property="og:title" content="Watch The Example | Netflix Official Site">
<meta property="og:url" content="https://www.netflix.com/title/81234567">
<meta property="og:image" content="https://occ.example/og.jpg?a=1&amp;b=2">
<meta name="description" content="not an og tag">
</head><body>
<script>window.netflix = window.netflix || {};netflix.reactContext = {models:{}};
`)
	if withGraphQL {
		b.WriteString("netflix.reactContext.models.graphql = JSON.parse('")
		b.WriteString(netflixJSQuote(netflixTestGraphQL))
		b.WriteString("');\n")
	}
	b.WriteString("</script></body></html>")
	return b.String()
}

const netflixTestSPARQL = `{"head":{"vars":["nf"]},"results":{"bindings":[{"nf":{"type":"literal","value":"81234567"}}]}}`
const netflixTestSPARQLEmpty = `{"head":{"vars":["nf"]},"results":{"bindings":[]}}`

// netflixTestClient points a client at an httptest server for both the
// title pages and the SPARQL endpoint.
func netflixTestClient(t *testing.T, handler http.HandlerFunc) *NetflixClient {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	c := NewNetflixClient()
	c.BaseURL = srv.URL
	c.WikidataURL = srv.URL + "/sparql"
	return c
}

func TestNetflixLookupID(t *testing.T) {
	var mu sync.Mutex
	var seen []*http.Request
	c := netflixTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.Clone(context.Background()))
		mu.Unlock()
		if r.URL.Path != "/sparql" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/sparql-results+json")
		if strings.Contains(r.URL.Query().Get("query"), `"tt0111161"`) {
			_, _ = w.Write([]byte(netflixTestSPARQL))
			return
		}
		_, _ = w.Write([]byte(netflixTestSPARQLEmpty))
	})
	ctx := context.Background()

	id, err := c.LookupID(ctx, "tt0111161")
	if err != nil {
		t.Fatal(err)
	}
	if id != "81234567" {
		t.Fatalf("LookupID = %q, want 81234567", id)
	}
	req := seen[0]
	if got := req.Header.Get("User-Agent"); got != netflixWikidataUA {
		t.Errorf("User-Agent = %q", got)
	}
	if got := req.URL.Query().Get("format"); got != "json" {
		t.Errorf("format = %q, want json", got)
	}
	q := req.URL.Query().Get("query")
	if !strings.Contains(q, "wdt:P345") || !strings.Contains(q, "wdt:P1874") {
		t.Errorf("query lacks the IMDb/Netflix properties: %s", q)
	}

	// Unmapped title: "" without error.
	id, err = c.LookupID(ctx, "tt0000001")
	if err != nil {
		t.Fatal(err)
	}
	if id != "" {
		t.Fatalf("unmapped LookupID = %q, want \"\"", id)
	}

	// Cached: no further request for a repeat lookup.
	if _, err := c.LookupID(ctx, "tt0111161"); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 2 {
		t.Fatalf("server saw %d requests, want 2", len(seen))
	}

	if _, err := c.LookupID(ctx, "81234567"); err == nil {
		t.Fatal("expected an error for a non-IMDb id")
	}
}

func TestNetflixTitleParsesGraphQL(t *testing.T) {
	var mu sync.Mutex
	var ua string
	calls := 0
	c := netflixTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		ua = r.Header.Get("User-Agent")
		mu.Unlock()
		if r.URL.Path != "/title/81234567" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(netflixTestPage(true)))
	})

	title, err := c.Title(context.Background(), "81234567")
	if err != nil {
		t.Fatal(err)
	}
	if ua != netflixBrowserUA {
		t.Errorf("title page fetched with User-Agent %q", ua)
	}
	if title.ID != "81234567" {
		t.Errorf("ID = %q", title.ID)
	}
	if title.Name != "The Example's Story" {
		t.Errorf("Name = %q, want the entity title with the JS escape undone", title.Name)
	}
	if title.Kind != "movie" {
		t.Errorf("Kind = %q, want movie", title.Kind)
	}
	if title.Logo != "https://occ.example/logo.png" {
		t.Errorf("Logo = %q, want the LOGO_HORIZONTAL_CROPPED url (not the brand badge)", title.Logo)
	}
	if len(title.Images) != 2 {
		t.Fatalf("Images = %+v, want one BILLBOARD and one BOXSHOT", title.Images)
	}
	if bb := title.Images[0]; bb.Type != "BILLBOARD" || bb.Width != 1920 || bb.URL != "https://occ.example/bb-1920.jpg" {
		t.Errorf("Images[0] = %+v, want the widest BILLBOARD rendition", bb)
	}
	if bs := title.Images[1]; bs.Type != "BOXSHOT" || bs.Width != 342 || bs.URL != "https://occ.example/bs-342.jpg" {
		t.Errorf("Images[1] = %+v, want the BOXSHOT", bs)
	}
	for _, img := range title.Images {
		if strings.Contains(img.URL, "placeholder") || strings.Contains(img.URL, "rec.jpg") {
			t.Errorf("unexpected image %+v", img)
		}
	}

	if h := title.Best("horizontal"); h == nil || h.Type != "BILLBOARD" {
		t.Errorf("Best(horizontal) = %+v", h)
	}
	if v := title.Best("vertical"); v == nil || v.Type != "BOXSHOT" {
		t.Errorf("Best(vertical) = %+v", v)
	}

	// Cached.
	if _, err := c.Title(context.Background(), "81234567"); err != nil {
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("server saw %d requests, want 1", calls)
	}

	if _, err := c.Title(context.Background(), "abc"); err == nil {
		t.Fatal("expected an error for a malformed id")
	}
}

func TestNetflixTitleOGImageFallback(t *testing.T) {
	c := netflixTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(netflixTestPage(false)))
	})
	title, err := c.Title(context.Background(), "81234567")
	if err != nil {
		t.Fatal(err)
	}
	if title.Name != "The Example" {
		t.Errorf("Name = %q, want og:title cleaned of the Watch/Netflix decoration", title.Name)
	}
	if title.Kind != "" {
		t.Errorf("Kind = %q, want \"\" (page doesn't say)", title.Kind)
	}
	if len(title.Images) != 1 || title.Images[0].Type != "BILLBOARD" || title.Images[0].URL != "https://occ.example/og.jpg?a=1&b=2" {
		t.Fatalf("Images = %+v, want the og:image as a BILLBOARD", title.Images)
	}
	if h := title.Best("horizontal"); h == nil || h.URL != "https://occ.example/og.jpg?a=1&b=2" {
		t.Errorf("Best(horizontal) = %+v", h)
	}
	if v := title.Best("vertical"); v != nil {
		t.Errorf("Best(vertical) = %+v, want nil", v)
	}
}

func TestNetflixTitleCanonicalIDFromOGURL(t *testing.T) {
	// A /watch/-style id resolves to the canonical title id in og:url.
	title, err := netflixParseTitlePage(netflixTestPage(true), "70000001")
	if err != nil {
		t.Fatal(err)
	}
	if title.ID != "81234567" {
		t.Errorf("ID = %q, want the canonical og:url id", title.ID)
	}
}

func TestNetflixTitlePlaceholderOnlyIsAnError(t *testing.T) {
	cache := `{"data":{"Movie:{\"videoId\":81234567}":{"__typename":"Movie","title":"Gone",
"artwork({\"format\":\"JPG\",\"params\":{\"artworkType\":\"BILLBOARD\",\"dimension\":{\"width\":1920}}})":{"key":"","url":"https://occ.example/placeholder.jpg"}}}}`
	page := strings.Replace(netflixTestPage(true), netflixJSQuote(netflixTestGraphQL), netflixJSQuote(cache), 1)
	if _, err := netflixParseTitlePage(page, "81234567"); err == nil {
		t.Fatal("expected an error: placeholder art must not fall back to og:image")
	}
}

func TestNetflixTitleNoArtworkAtAll(t *testing.T) {
	if _, err := netflixParseTitlePage("<html><head><title>x</title></head></html>", "81234567"); err == nil {
		t.Fatal("expected an error when the page has neither graphql nor og:image")
	}
}

func TestNetflixTitleNotFound(t *testing.T) {
	appleFastRetries(t)
	calls := 0
	c := netflixTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		http.NotFound(w, r)
	})
	_, err := c.Title(context.Background(), "81234567")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("err = %v, want a not-found error", err)
	}
	if calls != 1 {
		t.Fatalf("server saw %d calls, want 1 (404 is permanent)", calls)
	}
}

func TestNetflixTitleRetriesOn503(t *testing.T) {
	appleFastRetries(t)
	var mu sync.Mutex
	calls := 0
	c := netflixTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		if n == 1 {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(netflixTestPage(true)))
	})
	title, err := c.Title(context.Background(), "81234567")
	if err != nil {
		t.Fatalf("Title after 503: %v", err)
	}
	if title.Best("horizontal") == nil {
		t.Fatal("no billboard after retry")
	}
	if calls != 2 {
		t.Fatalf("server saw %d calls, want 2 (503 then 200)", calls)
	}
}

func TestNetflixLookupIDRetriesOn503(t *testing.T) {
	appleFastRetries(t)
	var mu sync.Mutex
	calls := 0
	c := netflixTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		calls++
		n := calls
		mu.Unlock()
		if n == 1 {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte(netflixTestSPARQL))
	})
	id, err := c.LookupID(context.Background(), "tt0111161")
	if err != nil {
		t.Fatal(err)
	}
	if id != "81234567" || calls != 2 {
		t.Fatalf("id %q after %d calls", id, calls)
	}
}

func TestNetflixBestRanking(t *testing.T) {
	// Unsorted on purpose: Best must rank, not trust slice order.
	title := &NetflixTitle{Images: []NetflixImage{
		{Type: "ECLIPSE_BILLBOARD_REDUX", URL: "hero"},
		{Type: "BOXSHOT", URL: "box"},
		{Type: "STORY_ART", URL: "story"},
		{Type: "BILLBOARD", URL: "bb"},
	}}
	if got := title.Best("horizontal").URL; got != "bb" {
		t.Errorf("Best(horizontal) = %q, want bb", got)
	}
	if got := title.Best("vertical").URL; got != "box" {
		t.Errorf("Best(vertical) = %q, want box", got)
	}

	title.Images = title.Images[:3] // drop BILLBOARD
	if got := title.Best("horizontal").URL; got != "story" {
		t.Errorf("without BILLBOARD, Best(horizontal) = %q, want story", got)
	}
	title.Images = title.Images[:2] // drop STORY_ART
	if got := title.Best("horizontal").URL; got != "hero" {
		t.Errorf("without story art, Best(horizontal) = %q, want hero", got)
	}
	title.Images = title.Images[1:2] // BOXSHOT only
	if got := title.Best("horizontal"); got != nil {
		t.Errorf("boxshot-only Best(horizontal) = %+v, want nil", got)
	}
	var none *NetflixTitle
	if none.Best("horizontal") != nil || (&NetflixTitle{}).Best("vertical") != nil {
		t.Error("empty titles must yield nil")
	}
}

func TestNetflixOpenGraph(t *testing.T) {
	og := netflixOpenGraph(netflixTestPage(false))
	if og["og:title"] != "Watch The Example | Netflix Official Site" {
		t.Errorf("og:title = %q", og["og:title"])
	}
	if og["og:image"] != "https://occ.example/og.jpg?a=1&b=2" {
		t.Errorf("og:image = %q (entities should be unescaped)", og["og:image"])
	}
	if _, ok := og["description"]; ok {
		t.Error("non-og meta leaked into the map")
	}
	if got := netflixCleanOGTitle("Watch Stranger Things | Netflix Official Site"); got != "Stranger Things" {
		t.Errorf("netflixCleanOGTitle = %q", got)
	}
}

func TestNetflixTitleBillboardURLUnescaped(t *testing.T) {
	// The JSON-level \\u0026 escape in a URL survives the JS string literal
	// (where its backslash is doubled) and decodes back to "&".
	escaped := "https://occ.example/bb-1920.jpg?x=1\\u0026y=2" // literal backslash-u
	cache := strings.Replace(netflixTestGraphQL, "https://occ.example/bb-1920.jpg", escaped, 1)
	page := strings.Replace(netflixTestPage(true), netflixJSQuote(netflixTestGraphQL), netflixJSQuote(cache), 1)
	if !strings.Contains(page, "\\\\u0026") {
		t.Fatal("test page should carry a JS-escaped backslash before u0026")
	}
	title, err := netflixParseTitlePage(page, "81234567")
	if err != nil {
		t.Fatal(err)
	}
	if got := title.Best("horizontal").URL; got != "https://occ.example/bb-1920.jpg?x=1&y=2" {
		t.Errorf("billboard URL = %q", got)
	}
}

func TestNetflixUnescapeJS(t *testing.T) {
	// Inputs are written as interpreted strings so each "\\" is one literal
	// backslash in the JS source being decoded.
	cases := []struct{ in, want string }{
		{"plain", "plain"},
		{"a\\'b", "a'b"},
		{"a\\\"b", "a\"b"},
		{"a\\\\b", "a\\b"},
		{"a\\/b", "a/b"},
		{"x\\u0026y", "x&y"},
		{"caf\\u00e9", "café"},
		{"\\ud83c\\udfac", "\U0001F3AC"}, // surrogate pair
		{"\\x41\\n\\t", "A\n\t"},
		{"{\"k\":\"v\\\\u0026w\"}", "{\"k\":\"v\\u0026w\"}"}, // JSON-level escape stays for the JSON decoder
		{"trailing backslash \\", "trailing backslash \\"},
	}
	for _, tc := range cases {
		if got := netflixUnescapeJS(tc.in); got != tc.want {
			t.Errorf("netflixUnescapeJS(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
