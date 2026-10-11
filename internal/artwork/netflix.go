package artwork

// Netflix artwork client. Netflix has no public catalog API and its search
// needs a login, so this works from two public surfaces instead: the
// logged-out title page (netflix.com/title/<id>), whose embedded page data
// carries the story art the Netflix apps show, and Wikidata's IMDb-id →
// Netflix-id mapping for finding that page. Neither is a contract — page
// changes degrade gracefully to the Open Graph image, and beyond that to
// "not found".

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	netflixBaseURL     = "https://www.netflix.com"
	netflixWikidataURL = "https://query.wikidata.org/sparql"

	// netflixBrowserUA is what the title page is fetched with — Netflix
	// serves the full logged-out page to browsers only.
	netflixBrowserUA = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/128.0.0.0 Safari/537.36"
	// netflixWikidataUA identifies the tool, as Wikimedia's API policy asks.
	netflixWikidataUA = "goplexcli/1.0 (https://github.com/joshkerr/goplexcli)"

	netflixMaxPageBytes    = 8 << 20
	netflixMaxWikidataByte = 1 << 20
)

// NetflixImage is one piece of a title's artwork.
type NetflixImage struct {
	// Type is Netflix's artwork type, e.g. "BILLBOARD" (the 16:9 story art
	// behind a title page), "ECLIPSE_BILLBOARD_REDUX" (the same art in a
	// wider hero crop) or "BOXSHOT" (the 2:3 poster).
	Type  string
	Width int // pixel width Netflix rendered it at; 0 when unknown
	URL   string

	asset string // asset id shared by every rendition of one picture
}

// NetflixTitle is one Netflix title with its distinct artwork, best first.
type NetflixTitle struct {
	ID     string
	Name   string
	Kind   string // "movie", "tv", or "" when the page doesn't say
	Images []NetflixImage
	// Logo is the title-treatment PNG (transparent wordmark), when present.
	Logo string
}

// Best returns the preferred image for an orientation: "horizontal" prefers
// BILLBOARD, then *STORY* art, then other *BILLBOARD* crops; "vertical"
// takes BOXSHOT types. It returns nil when the title has nothing suitable.
func (t *NetflixTitle) Best(orientation string) *NetflixImage {
	if t == nil {
		return nil
	}
	var best *NetflixImage
	for i := range t.Images {
		img := &t.Images[i]
		if orientation == "vertical" {
			if strings.Contains(img.Type, "BOXSHOT") {
				return img
			}
			continue
		}
		r := netflixRank(img.Type)
		if r >= 3 {
			continue
		}
		if best == nil || r < netflixRank(best.Type) {
			best = img
		}
	}
	return best
}

// NetflixClient fetches Netflix title pages and Wikidata id lookups.
type NetflixClient struct {
	// BaseURL is the Netflix site root; WikidataURL the SPARQL endpoint.
	// Both are exported so tests can point them at httptest servers.
	BaseURL     string
	WikidataURL string
	HTTPClient  *http.Client

	ids    *appleCache[string]
	titles *appleCache[*NetflixTitle]
}

// NewNetflixClient returns a client with the production endpoints and a
// small in-memory cache of lookups and pages.
func NewNetflixClient() *NetflixClient {
	return &NetflixClient{
		BaseURL:     netflixBaseURL,
		WikidataURL: netflixWikidataURL,
		HTTPClient:  &http.Client{Timeout: 20 * time.Second},
		ids:         newAppleCache[string](),
		titles:      newAppleCache[*NetflixTitle](),
	}
}

var (
	netflixBareIDRe = regexp.MustCompile(`^\d{6,}$`)
	netflixIMDBIDRe = regexp.MustCompile(`^tt\d+$`)
)

// LookupID maps an IMDb id ("tt0111161") to a Netflix title id via Wikidata
// (P345 → P1874). It returns "", nil when Wikidata has no Netflix id for the
// title.
func (c *NetflixClient) LookupID(ctx context.Context, imdbID string) (string, error) {
	if !netflixIMDBIDRe.MatchString(imdbID) {
		return "", fmt.Errorf("invalid IMDb id %q", imdbID)
	}
	cacheKey := "imdb:" + imdbID
	if id, ok := c.ids.get(cacheKey); ok {
		return id, nil
	}

	query := fmt.Sprintf(`SELECT ?nf WHERE { ?item wdt:P345 %q ; wdt:P1874 ?nf } LIMIT 1`, imdbID)
	params := url.Values{"format": {"json"}, "query": {query}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.WikidataURL+"?"+params.Encode(), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", netflixWikidataUA)
	req.Header.Set("Accept", "application/sparql-results+json")

	body, err := appleFetch(ctx, c.HTTPClient, req, netflixMaxWikidataByte)
	if err != nil {
		return "", fmt.Errorf("wikidata lookup failed: %w", err)
	}
	var parsed struct {
		Results struct {
			Bindings []struct {
				NF struct {
					Value string `json:"value"`
				} `json:"nf"`
			} `json:"bindings"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", fmt.Errorf("wikidata lookup failed: %w", err)
	}
	id := ""
	for _, b := range parsed.Results.Bindings {
		if netflixBareIDRe.MatchString(b.NF.Value) {
			id = b.NF.Value
			break
		}
	}
	c.ids.set(cacheKey, id)
	return id, nil
}

// Title fetches a title's public page and returns its artwork.
func (c *NetflixClient) Title(ctx context.Context, id string) (*NetflixTitle, error) {
	if !netflixBareIDRe.MatchString(id) {
		return nil, fmt.Errorf("invalid Netflix title id %q", id)
	}
	cacheKey := "title:" + id
	if t, ok := c.titles.get(cacheKey); ok {
		return t, nil
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSuffix(c.BaseURL, "/")+"/title/"+id, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", netflixBrowserUA)
	req.Header.Set("Accept", "text/html")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")

	page, err := appleFetch(ctx, c.HTTPClient, req, netflixMaxPageBytes)
	if err != nil {
		if appleHTTPStatus(err) == http.StatusNotFound {
			return nil, fmt.Errorf("Netflix title %s not found (not available in this region?)", id)
		}
		return nil, fmt.Errorf("fetching Netflix title page: %w", err)
	}

	t, err := netflixParseTitlePage(string(page), id)
	if err != nil {
		return nil, err
	}
	c.titles.set(cacheKey, t)
	return t, nil
}

var (
	netflixMetaTagRe   = regexp.MustCompile(`(?is)<meta\b[^>]*>`)
	netflixMetaAttrRe  = regexp.MustCompile(`(?is)\b(property|name|content)\s*=\s*"([^"]*)"`)
	netflixOGTitleIDRe = regexp.MustCompile(`/title/(\d+)`)
	netflixGraphqlRe   = regexp.MustCompile(`(?s)models\.graphql\s*=\s*JSON\.parse\('(.*?)'\)`)
	netflixArtTypeRe   = regexp.MustCompile(`"artworkType":"([A-Z_,]+)"`)
	netflixArtWidthRe  = regexp.MustCompile(`"width":(\d+)`)
)

// netflixOpenGraph returns the page's og:* meta values.
func netflixOpenGraph(page string) map[string]string {
	og := map[string]string{}
	for _, tag := range netflixMetaTagRe.FindAllString(page, -1) {
		var prop, content string
		for _, m := range netflixMetaAttrRe.FindAllStringSubmatch(tag, -1) {
			switch strings.ToLower(m[1]) {
			case "property", "name":
				prop = m[2]
			case "content":
				content = m[2]
			}
		}
		if strings.HasPrefix(prop, "og:") && content != "" {
			if _, dup := og[prop]; !dup {
				og[prop] = netflixHTMLUnescape(content)
			}
		}
	}
	return og
}

func netflixHTMLUnescape(s string) string {
	r := strings.NewReplacer("&amp;", "&", "&quot;", `"`, "&#39;", "'", "&#x27;", "'", "&lt;", "<", "&gt;", ">")
	return r.Replace(s)
}

// netflixCleanOGTitle turns "Watch Stranger Things | Netflix Official Site"
// into "Stranger Things".
func netflixCleanOGTitle(s string) string {
	if i := strings.LastIndex(s, " | "); i >= 0 {
		s = s[:i]
	}
	s = strings.TrimPrefix(s, "Watch ")
	return strings.TrimSpace(s)
}

// netflixParseTitlePage extracts the title's own artwork from the page's
// embedded GraphQL cache, falling back to the Open Graph image when that
// structure isn't there.
func netflixParseTitlePage(page, requested string) (*NetflixTitle, error) {
	og := netflixOpenGraph(page)
	canonical := requested
	if m := netflixOGTitleIDRe.FindStringSubmatch(og["og:url"]); m != nil {
		canonical = m[1]
	}
	t := &NetflixTitle{ID: canonical, Name: netflixCleanOGTitle(og["og:title"])}

	if m := netflixGraphqlRe.FindStringSubmatch(page); m != nil {
		if ent := netflixFindEntity(netflixUnescapeJS(m[1]), canonical, requested); ent != nil {
			netflixFillFromEntity(t, ent)
			if len(t.Images) == 0 {
				// The record is there but carries only placeholder art, so
				// the og:image would be the same placeholder — don't fall
				// back to it.
				return nil, fmt.Errorf("Netflix has no artwork for %s (title not available here?)", requested)
			}
		}
	}
	if len(t.Images) == 0 {
		if img := og["og:image"]; img != "" {
			t.Images = []NetflixImage{{Type: "BILLBOARD", URL: img}}
		}
	}
	if len(t.Images) == 0 {
		return nil, fmt.Errorf("no artwork found on the Netflix page for %s", requested)
	}
	if t.Name == "" {
		t.Name = "Netflix title " + t.ID
	}
	return t, nil
}

type netflixEntity struct {
	typename string
	title    string
	artKeys  []string                   // artwork keys in page order
	artwork  map[string]json.RawMessage // key → image payload
}

// netflixObjectKeys returns a JSON object's keys in document order, which
// the page uses to rank a title's artwork (the main billboard comes first).
func netflixObjectKeys(raw json.RawMessage) []string {
	dec := json.NewDecoder(bytes.NewReader(raw))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return nil
	}
	var keys []string
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return keys
		}
		k, _ := tok.(string)
		keys = append(keys, k)
		var skip json.RawMessage
		if dec.Decode(&skip) != nil {
			return keys
		}
	}
	return keys
}

// netflixDataObject returns the raw "data" object of the cache document.
func netflixDataObject(cacheJSON string) json.RawMessage {
	var doc struct {
		Data json.RawMessage `json:"data"`
	}
	_ = json.Unmarshal([]byte(cacheJSON), &doc)
	return doc.Data
}

// netflixFindEntity locates the page title's own Show/Movie record in the
// GraphQL cache: by canonical id, then the requested id, then whichever
// record has billboard art (a /watch/ id resolves to an episode, whose show
// carries the art).
func netflixFindEntity(cacheJSON, canonical, requested string) *netflixEntity {
	var doc struct {
		Data map[string]json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal([]byte(cacheJSON), &doc); err != nil || len(doc.Data) == 0 {
		return nil
	}
	decode := func(raw json.RawMessage) *netflixEntity {
		var fields map[string]json.RawMessage
		if json.Unmarshal(raw, &fields) != nil {
			return nil
		}
		e := &netflixEntity{artwork: map[string]json.RawMessage{}}
		_ = json.Unmarshal(fields["__typename"], &e.typename)
		_ = json.Unmarshal(fields["title"], &e.title)
		for _, k := range netflixObjectKeys(raw) {
			if strings.HasPrefix(k, "artwork(") {
				e.artKeys = append(e.artKeys, k)
				e.artwork[k] = fields[k]
			}
		}
		return e
	}
	for _, id := range []string{canonical, requested} {
		for _, kind := range []string{"Show", "Movie"} {
			if raw, ok := doc.Data[fmt.Sprintf(`%s:{"videoId":%s}`, kind, id)]; ok {
				if e := decode(raw); e != nil && len(e.artwork) > 0 {
					return e
				}
			}
		}
	}
	// Fallback: the record with billboard art. Recommendation tiles only
	// carry boxshots, so this singles out the page's own title.
	for _, k := range netflixObjectKeys(netflixDataObject(cacheJSON)) {
		if !strings.HasPrefix(k, "Show:") && !strings.HasPrefix(k, "Movie:") {
			continue
		}
		e := decode(doc.Data[k])
		if e == nil {
			continue
		}
		for ak := range e.artwork {
			if strings.Contains(ak, `"artworkType":"BILLBOARD"`) {
				return e
			}
		}
	}
	return nil
}

// netflixFillFromEntity turns a record's artwork fields into distinct
// images: every rendition of one asset collapses into a single image whose
// URL is the 16:9 billboard when there is one (else the widest rendition).
func netflixFillFromEntity(t *NetflixTitle, e *netflixEntity) {
	switch e.typename {
	case "Show":
		t.Kind = "tv"
	case "Movie":
		t.Kind = "movie"
	}
	if e.title != "" {
		t.Name = e.title
	}

	type rendition struct {
		typ   string
		width int
		url   string
	}
	byAsset := map[string][]rendition{}
	var order []string
	for _, k := range e.artKeys {
		var img struct {
			Key string `json:"key"`
			URL string `json:"url"`
		}
		if json.Unmarshal(e.artwork[k], &img) != nil || img.URL == "" {
			continue
		}
		typ := ""
		if m := netflixArtTypeRe.FindStringSubmatch(k); m != nil {
			typ = m[1]
		}
		if typ == "" {
			continue
		}
		width := 0
		if m := netflixArtWidthRe.FindStringSubmatch(k); m != nil {
			width, _ = strconv.Atoi(m[1])
		}
		if strings.Contains(typ, "LOGO") {
			// The title treatment; BRAND_* logos are the "N" badge, not art.
			isTitleLogo := !strings.HasPrefix(typ, "BRAND")
			if isTitleLogo && (typ == "LOGO_HORIZONTAL_CROPPED" || t.Logo == "") {
				t.Logo = img.URL
			}
			continue
		}
		if !strings.Contains(typ, "BILLBOARD") && !strings.Contains(typ, "STORY") && !strings.Contains(typ, "BOXSHOT") {
			continue
		}
		// Renditions of one picture share the asset id in their key. Art
		// without a key is Netflix's grey "N" placeholder, served for titles
		// that aren't (or are no longer) in the catalog here — not artwork.
		parts := strings.Split(img.Key, "|")
		if len(parts) < 2 || parts[1] == "" {
			continue
		}
		asset := parts[1]
		if _, seen := byAsset[asset]; !seen {
			order = append(order, asset)
		}
		byAsset[asset] = append(byAsset[asset], rendition{typ: typ, width: width, url: img.URL})
	}

	for _, asset := range order {
		rs := byAsset[asset]
		best := rs[0]
		for _, r := range rs[1:] {
			bestIsBillboard, rIsBillboard := best.typ == "BILLBOARD", r.typ == "BILLBOARD"
			if rIsBillboard && !bestIsBillboard || rIsBillboard == bestIsBillboard && r.width > best.width {
				best = r
			}
		}
		t.Images = append(t.Images, NetflixImage{Type: best.typ, Width: best.width, URL: best.url, asset: asset})
	}
	// Billboards (16:9 story art) lead; boxshots and hero crops follow.
	sort.SliceStable(t.Images, func(i, j int) bool {
		return netflixRank(t.Images[i].Type) < netflixRank(t.Images[j].Type)
	})
}

// netflixRank orders artwork types for the horizontal preference: the main
// billboard, then story art, then other billboard crops, then the rest.
func netflixRank(typ string) int {
	switch {
	case typ == "BILLBOARD":
		return 0
	case strings.Contains(typ, "STORY"):
		return 1
	case strings.Contains(typ, "BILLBOARD"):
		return 2
	default:
		return 3
	}
}

// netflixUnescapeJS decodes the body of a JavaScript single-quoted string
// literal (the argument Netflix hands to JSON.parse) back into the JSON
// text.
func netflixUnescapeJS(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if ch != '\\' || i+1 >= len(s) {
			b.WriteByte(ch)
			continue
		}
		i++
		switch e := s[i]; e {
		case 'n':
			b.WriteByte('\n')
		case 'r':
			b.WriteByte('\r')
		case 't':
			b.WriteByte('\t')
		case 'b':
			b.WriteByte('\b')
		case 'f':
			b.WriteByte('\f')
		case 'v':
			b.WriteByte('\v')
		case '0':
			b.WriteByte(0)
		case 'x':
			if i+2 < len(s) {
				if n, err := strconv.ParseUint(s[i+1:i+3], 16, 8); err == nil {
					b.WriteRune(rune(n))
					i += 2
					continue
				}
			}
			b.WriteByte('x')
		case 'u':
			if i+4 < len(s) {
				if n, err := strconv.ParseUint(s[i+1:i+5], 16, 16); err == nil {
					r := rune(n)
					i += 4
					// A surrogate pair arrives as two \u escapes.
					if r >= 0xD800 && r < 0xDC00 && i+6 < len(s) && s[i+1] == '\\' && s[i+2] == 'u' {
						if lo, err := strconv.ParseUint(s[i+3:i+7], 16, 16); err == nil && lo >= 0xDC00 && lo < 0xE000 {
							r = 0x10000 + (r-0xD800)<<10 + (rune(lo) - 0xDC00)
							i += 6
						}
					}
					b.WriteRune(r)
					continue
				}
			}
			b.WriteByte('u')
		default:
			// \\ \' \" \/ and anything unknown: keep the character itself.
			b.WriteByte(e)
		}
	}
	return b.String()
}
