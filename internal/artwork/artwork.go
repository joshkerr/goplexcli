// Package artwork finds cover art for a downloaded movie or show and embeds
// it into the file, the way gowebdav's poster picker does but without a
// human in the loop: Apple TV's catalog art is preferred, then TMDB, then
// Netflix (with its title logo composited on).
package artwork

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/joshkerr/goplexcli/internal/config"
	"github.com/joshkerr/goplexcli/internal/plex"
	"github.com/joshkerr/goplexcli/internal/tmdb"
)

// Orientations.
const (
	Horizontal = "horizontal" // 16:9 art with the title treatment (gowebdav's cards)
	Vertical   = "vertical"   // 2:3 poster
)

// Item identifies the title to find art for.
type Item struct {
	Title string
	Year  int
	Kind  string // "movie" | "tv"
}

// ItemFor derives the lookup from a cached media item: movies by their own
// title, episodes by their show.
func ItemFor(it *plex.MediaItem) Item {
	if it.Type == "episode" {
		return Item{Title: it.ParentTitle, Year: it.Year, Kind: "tv"}
	}
	return Item{Title: it.Title, Year: it.Year, Kind: "movie"}
}

// Candidate is a piece of art chosen for an item.
type Candidate struct {
	Source  string // "apple" | "tmdb" | "netflix"
	URL     string
	LogoURL string // Netflix title logo to composite, if any
	Title   string
	Year    int
}

// Options configures lookups and embedding.
type Options struct {
	Tools       Tools
	Apple       *AppleClient   // nil disables Apple
	TMDB        *tmdb.Client   // nil disables TMDB and Netflix
	Netflix     *NetflixClient // nil disables Netflix
	Orientation string         // Horizontal (default) or Vertical
	// CacheDir keeps downloaded art so the episodes of a show share one
	// download; empty uses a temp dir per call.
	CacheDir string
	// SkipExisting leaves files that already carry cover art alone.
	SkipExisting bool
	Logf         func(format string, args ...any)
}

// Result reports what Apply did.
type Result struct {
	Source  string // art source used, "" when skipped
	Tool    string // embedding tool used
	Skipped string // reason nothing was embedded, "" when embedded
	ArtPath string // the cached artwork file
}

// ErrNoArtwork is returned by Find when no source has art for the item.
var ErrNoArtwork = errors.New("artwork: nothing found")

// NewOptions builds Options from config: the artwork feature is on unless
// `artwork` is "off"; TMDB and Netflix need a tmdb_api_key.
func NewOptions(cfg *config.Config, cacheDir string) (Options, bool) {
	if strings.EqualFold(strings.TrimSpace(cfg.Artwork), "off") {
		return Options{}, false
	}
	opts := Options{
		Tools:        Tools{FFmpeg: cfg.FFmpegPath, FFprobe: cfg.FFprobePath, MkvPropEdit: cfg.MkvPropEditPath},
		Apple:        NewAppleClient(cfg.AppleStorefront),
		Orientation:  cfg.ArtworkOrientation,
		CacheDir:     filepath.Join(cacheDir, "artwork"),
		SkipExisting: true,
	}
	if key := strings.TrimSpace(cfg.TMDBAPIKey); key != "" {
		store, err := tmdb.NewDiskStore(filepath.Join(cacheDir, "library", "tmdb"), 30*24*time.Hour)
		if err == nil {
			opts.TMDB = tmdb.New(key, store)
		} else {
			opts.TMDB = tmdb.New(key, nil)
		}
		opts.Netflix = NewNetflixClient()
	}
	return opts, true
}

func (o Options) orientation() string {
	if o.Orientation == Vertical {
		return Vertical
	}
	return Horizontal
}

func (o Options) logf(format string, args ...any) {
	if o.Logf != nil {
		o.Logf(format, args...)
	}
}

// Find picks art for an item: Apple (either orientation, preferred one
// first), then TMDB (backdrop for horizontal, poster for vertical), then
// Netflix via the title's IMDb id.
func Find(ctx context.Context, opts Options, it Item) (Candidate, error) {
	if strings.TrimSpace(it.Title) == "" {
		return Candidate{}, ErrNoArtwork
	}
	kind := it.Kind
	if kind != "tv" {
		kind = "movie"
	}
	orient := opts.orientation()
	other := Vertical
	if orient == Vertical {
		other = Horizontal
	}

	if opts.Apple != nil {
		r, err := opts.Apple.Find(ctx, it.Title, it.Year, kind)
		if err != nil {
			opts.logf("apple lookup for %q: %v", it.Title, err)
		} else if r != nil {
			for _, o := range []string{orient, other} {
				if u := r.ArtworkURL(o, false); u != "" {
					return Candidate{Source: "apple", URL: u, Title: r.Title, Year: r.Year}, nil
				}
			}
		}
	}

	if opts.TMDB == nil {
		return Candidate{}, ErrNoArtwork
	}
	var poster, backdrop, imdb, title string
	var year int
	if kind == "tv" {
		id, err := opts.TMDB.SearchTV(ctx, it.Title, it.Year)
		if err != nil {
			return Candidate{}, err
		}
		if id > 0 {
			s, err := opts.TMDB.TV(ctx, id)
			if err != nil && !errors.Is(err, tmdb.ErrNotFound) {
				return Candidate{}, err
			}
			if s != nil {
				poster, backdrop, imdb, title, year = s.PosterPath, s.BackdropPath, s.IMDBID, s.Name, s.Year
			}
		}
	} else {
		id, err := opts.TMDB.SearchMovie(ctx, it.Title, it.Year)
		if err != nil {
			return Candidate{}, err
		}
		if id > 0 {
			m, err := opts.TMDB.Movie(ctx, id)
			if err != nil && !errors.Is(err, tmdb.ErrNotFound) {
				return Candidate{}, err
			}
			if m != nil {
				poster, backdrop, imdb, title, year = m.PosterPath, m.BackdropPath, m.IMDBID, m.Title, m.Year
			}
		}
	}
	first, second := backdrop, poster
	firstSize, secondSize := "w1280", "w780"
	if orient == Vertical {
		first, second = poster, backdrop
		firstSize, secondSize = "w780", "w1280"
	}
	if first != "" {
		return Candidate{Source: "tmdb", URL: tmdb.ImageURL(first, firstSize), Title: title, Year: year}, nil
	}

	if opts.Netflix != nil && imdb != "" {
		nctx, cancel := context.WithTimeout(ctx, 20*time.Second)
		defer cancel()
		if id, err := opts.Netflix.LookupID(nctx, imdb); err != nil {
			opts.logf("netflix lookup for %q: %v", it.Title, err)
		} else if id != "" {
			if t, err := opts.Netflix.Title(nctx, id); err != nil {
				opts.logf("netflix title %s: %v", id, err)
			} else if img := t.Best(orient); img != nil {
				return Candidate{Source: "netflix", URL: img.URL, LogoURL: t.Logo, Title: t.Name, Year: year}, nil
			}
		}
	}

	if second != "" {
		return Candidate{Source: "tmdb", URL: tmdb.ImageURL(second, secondSize), Title: title, Year: year}, nil
	}
	return Candidate{}, ErrNoArtwork
}

// Fetch finds art for an item and returns a local file with it (cached per
// title, year, kind and orientation).
func Fetch(ctx context.Context, opts Options, it Item) (string, Candidate, error) {
	dir := opts.CacheDir
	if dir == "" {
		dir = filepath.Join(os.TempDir(), "goplexcli-artwork")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", Candidate{}, err
	}
	sum := sha1.Sum([]byte(strings.ToLower(it.Kind+"|"+it.Title) + fmt.Sprintf("|%d|%s", it.Year, opts.orientation())))
	path := filepath.Join(dir, hex.EncodeToString(sum[:8])+".jpg")
	meta := path + ".src"
	if st, err := os.Stat(path); err == nil && st.Size() > 0 {
		c := Candidate{}
		if b, err := os.ReadFile(meta); err == nil {
			c.Source = strings.TrimSpace(string(b))
		}
		return path, c, nil
	}

	cand, err := Find(ctx, opts, it)
	if err != nil {
		return "", Candidate{}, err
	}
	if err := DownloadImage(ctx, cand.URL, path); err != nil {
		return "", cand, fmt.Errorf("download %s art: %w", cand.Source, err)
	}
	if cand.LogoURL != "" {
		logo := path + ".logo.png"
		if err := DownloadImage(ctx, cand.LogoURL, logo); err == nil {
			if err := ComposeLogo(ctx, opts.Tools, path, logo); err != nil {
				opts.logf("logo composite for %q: %v", it.Title, err)
			}
			_ = os.Remove(logo)
		}
	}
	_ = os.WriteFile(meta, []byte(cand.Source), 0o644)
	return path, cand, nil
}

// Apply embeds art for an item into a downloaded file. Missing tools,
// unsupported containers and files that already have art are reported as
// skips rather than errors.
func Apply(ctx context.Context, opts Options, file string, it Item) (Result, error) {
	if !SupportedContainer(file) {
		return Result{Skipped: "unsupported container"}, nil
	}
	if !opts.Tools.Available() {
		return Result{Skipped: "ffmpeg/ffprobe not installed"}, nil
	}
	if opts.SkipExisting && HasCoverArt(ctx, opts.Tools, file) {
		return Result{Skipped: "already has cover art"}, nil
	}
	art, cand, err := Fetch(ctx, opts, it)
	if err != nil {
		if errors.Is(err, ErrNoArtwork) {
			return Result{Skipped: "no artwork found"}, nil
		}
		return Result{}, err
	}
	tool, err := Embed(ctx, opts.Tools, file, art)
	if err != nil {
		return Result{ArtPath: art}, err
	}
	return Result{Source: cand.Source, Tool: tool, ArtPath: art}, nil
}

// WriteSidecar copies art to <dir>/poster.jpg when no poster/cover/folder
// image exists there yet, so show folders get the artwork gowebdav and other
// players look for.
func WriteSidecar(dir, art string) (bool, error) {
	for _, base := range []string{"poster", "cover", "folder"} {
		for _, ext := range []string{".jpg", ".jpeg", ".png", ".webp"} {
			if _, err := os.Stat(filepath.Join(dir, base+ext)); err == nil {
				return false, nil
			}
		}
	}
	src, err := os.Open(art)
	if err != nil {
		return false, err
	}
	defer src.Close()
	tmp, err := os.CreateTemp(dir, ".poster-*.jpg")
	if err != nil {
		return false, err
	}
	tmpPath := tmp.Name()
	if _, err := io.Copy(tmp, src); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return false, err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return false, err
	}
	_ = os.Chmod(tmpPath, 0o644)
	if err := os.Rename(tmpPath, filepath.Join(dir, "poster.jpg")); err != nil {
		os.Remove(tmpPath)
		return false, err
	}
	return true, nil
}
