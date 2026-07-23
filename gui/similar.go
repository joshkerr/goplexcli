package main

import (
	"math"
	"sort"
	"strings"
	"unicode"

	"github.com/joshkerr/goplexcli/internal/cache"
	"github.com/joshkerr/goplexcli/internal/plex"
)

// similarLimit caps the "More like this" row in the detail modal.
const similarLimit = 24

// simMinScore drops results whose hybrid score is effectively noise (no
// summary overlap and no shared metadata), so a movie with nothing in common
// never pads the row.
const simMinScore = 0.05

// Hybrid score weights. Summary cosine dominates because it is the only signal
// that captures *subject* (submarine, heist, boxing); the metadata bonuses pull
// in same-director and same-genre neighbors whose summaries share little
// vocabulary. Weights sum to 1 so scores stay comparable across libraries.
const (
	simWeightSummary  = 0.5  // TF-IDF cosine over summary text
	simWeightGenre    = 0.2  // Jaccard overlap of genre tags
	simWeightDirector = 0.15 // any shared director
	simWeightCast     = 0.1  // shared cast, saturating at 3 names
	simWeightYear     = 0.05 // era proximity, fading to 0 at 20 years apart
)

// simDoc is one candidate in the similarity index: the ready-to-serve card plus
// the normalized signals scoring reads. Movies and shows both become docs;
// results are ranked within the seed's own type.
type simDoc struct {
	card     MediaCardDTO
	typ      string // movie | show
	genres   map[string]bool
	director map[string]bool
	cast     map[string]bool
	year     int
	vec      map[string]float64 // unit-length TF-IDF vector of the summary
}

type similarIndex struct {
	docs  []simDoc
	byKey map[string]int
}

// SimilarItems returns cards ranked most-similar-first to the given item,
// scored by summary TF-IDF cosine plus genre/director/cast/year overlap. The
// key may be a movie key, a synthetic "show:<title>" key, or an episode key
// (resolved to its show). Results are the seed's own type only.
func (a *App) SimilarItems(key string) []MediaCardDTO {
	idx := a.similarIndex()
	if idx == nil {
		return []MediaCardDTO{}
	}
	seed, ok := idx.byKey[key]
	if !ok {
		// An episode key isn't a doc; similarity for an episode means "shows
		// like this episode's show".
		if c := a.media(); c != nil {
			for i := range c.Media {
				if c.Media[i].Key == key && c.Media[i].Type == "episode" {
					seed, ok = idx.byKey["show:"+c.Media[i].ParentTitle]
					break
				}
			}
		}
	}
	if !ok {
		return []MediaCardDTO{}
	}
	cards := idx.similar(seed, similarLimit)
	a.warmCards(cards)
	return nonNilCards(cards)
}

// similarIndex returns the memoized index, rebuilding it only when the media
// cache it was built from has been replaced (reindex, LAN sync). A build over a
// 10k-item library takes well under a second and runs at most once per cache
// generation.
func (a *App) similarIndex() *similarIndex {
	c := a.media()
	if c == nil {
		return nil
	}
	a.simMu.Lock()
	defer a.simMu.Unlock()
	if a.simIdx != nil && a.simBuiltFrom == c {
		return a.simIdx
	}
	a.simIdx = a.buildSimilarIndex(c)
	a.simBuiltFrom = c
	return a.simIdx
}

func (a *App) buildSimilarIndex(c *cache.Cache) *similarIndex {
	idx := &similarIndex{byKey: make(map[string]int)}

	add := func(card MediaCardDTO, typ, genre, director, castNames, summary string, year int) {
		idx.byKey[card.Key] = len(idx.docs)
		idx.docs = append(idx.docs, simDoc{
			card:     card,
			typ:      typ,
			genres:   tagSet(genre),
			director: tagSet(director),
			cast:     tagSet(castNames),
			year:     year,
			// vec holds raw term counts here; tf-idf weighting and
			// normalization happen below once document frequencies are known.
			vec: termCounts(summary),
		})
	}

	for i := range c.Media {
		m := &c.Media[i]
		if m.Type != "movie" {
			continue
		}
		add(a.toCard(m), "movie", m.Genre, m.Director, m.Cast, m.Summary, m.Year)
	}

	// One doc per show, mirroring showDTO: the first episode encountered
	// supplies the text fields. groupShowCards supplies the ready-made cards.
	firstEp := map[string]*plex.MediaItem{}
	for i := range c.Media {
		m := &c.Media[i]
		if m.Type == "episode" && m.ParentTitle != "" && firstEp[m.ParentTitle] == nil {
			firstEp[m.ParentTitle] = m
		}
	}
	for _, card := range a.groupShowCards(c) {
		ep := firstEp[card.Title]
		if ep == nil {
			continue
		}
		add(card, "show", ep.Genre, ep.Director, ep.Cast, ep.Summary, ep.Year)
	}

	// Convert term counts to unit-length TF-IDF vectors. IDF makes rare,
	// distinctive words (u-boat, heist) dominate over words half the library's
	// summaries share (life, family, world).
	df := map[string]int{}
	for i := range idx.docs {
		for term := range idx.docs[i].vec {
			df[term]++
		}
	}
	n := float64(len(idx.docs))
	for i := range idx.docs {
		vec := idx.docs[i].vec
		var norm float64
		for term, tf := range vec {
			w := tf * math.Log(1+n/float64(df[term]))
			vec[term] = w
			norm += w * w
		}
		if norm > 0 {
			norm = math.Sqrt(norm)
			for term := range vec {
				vec[term] /= norm
			}
		}
	}
	return idx
}

// similar ranks every same-type doc against the seed and returns the top
// `limit` cards above the noise floor.
func (idx *similarIndex) similar(seed, limit int) []MediaCardDTO {
	s := &idx.docs[seed]
	type scored struct {
		i     int
		score float64
	}
	matches := make([]scored, 0, 64)
	for i := range idx.docs {
		d := &idx.docs[i]
		if i == seed || d.typ != s.typ {
			continue
		}
		score := simWeightSummary*dotSparse(s.vec, d.vec) +
			simWeightGenre*jaccard(s.genres, d.genres) +
			simWeightDirector*boolScore(overlapCount(s.director, d.director) > 0) +
			simWeightCast*math.Min(float64(overlapCount(s.cast, d.cast)), 3)/3 +
			simWeightYear*yearProximity(s.year, d.year)
		if score >= simMinScore {
			matches = append(matches, scored{i, score})
		}
	}
	sort.Slice(matches, func(a, b int) bool {
		if matches[a].score != matches[b].score {
			return matches[a].score > matches[b].score
		}
		return idx.docs[matches[a].i].card.Title < idx.docs[matches[b].i].card.Title
	})
	if len(matches) > limit {
		matches = matches[:limit]
	}
	out := make([]MediaCardDTO, 0, len(matches))
	for _, m := range matches {
		out = append(out, idx.docs[m.i].card)
	}
	return out
}

// tagSet splits a comma-separated tag field into a lowercased set.
func tagSet(field string) map[string]bool {
	out := map[string]bool{}
	for _, t := range strings.Split(field, ",") {
		t = strings.ToLower(strings.TrimSpace(t))
		if t != "" {
			out[t] = true
		}
	}
	return out
}

// termCounts tokenizes a summary into lowercase word counts, dropping
// stopwords and words shorter than 3 runes.
func termCounts(s string) map[string]float64 {
	out := map[string]float64{}
	for _, w := range strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	}) {
		if len([]rune(w)) < 3 || simStopwords[w] {
			continue
		}
		out[w]++
	}
	return out
}

// dotSparse is the cosine similarity of two unit-length sparse vectors.
func dotSparse(a, b map[string]float64) float64 {
	if len(b) < len(a) {
		a, b = b, a
	}
	var dot float64
	for term, w := range a {
		dot += w * b[term]
	}
	return dot
}

func jaccard(a, b map[string]bool) float64 {
	shared := overlapCount(a, b)
	union := len(a) + len(b) - shared
	if union == 0 {
		return 0
	}
	return float64(shared) / float64(union)
}

func overlapCount(a, b map[string]bool) int {
	if len(b) < len(a) {
		a, b = b, a
	}
	n := 0
	for t := range a {
		if b[t] {
			n++
		}
	}
	return n
}

func boolScore(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

// yearProximity is 1 for the same year, fading linearly to 0 at 20 years apart.
func yearProximity(a, b int) float64 {
	if a <= 0 || b <= 0 {
		return 0
	}
	d := math.Abs(float64(a - b))
	return math.Max(0, 1-d/20)
}

// simStopwords are common English words (plus summary boilerplate) that carry
// no subject signal and would otherwise dominate summary overlap.
var simStopwords = func() map[string]bool {
	words := strings.Fields(`
		the and for with his her their its this that these those from into
		onto out about after before during against between over under above
		below off down when while where which who whom whose what why how all
		any both each few more most other some such only own same than too
		very can will just should now not but they them then there here she
		him has have had was were are is be been being do does did doing would
		could may might must shall upon within without among along across
		behind beyond near through until toward towards
		one two three new old young first last next years year day days life
		man woman men women story film movie series set finds find takes take
		becomes become begins begin must gets get goes way back home world
		named called known soon also
	`)
	out := make(map[string]bool, len(words))
	for _, w := range words {
		out[w] = true
	}
	return out
}()
