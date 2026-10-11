// Package library indexes local media folders into the same MediaItem model
// the rest of goplexcli consumes, replacing a Plex Media Server: it scans
// folders for video files, parses titles from folder and file names, enriches
// them from TMDB, and stamps each item with keys the built-in library server
// resolves for streaming and artwork.
package library

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/joshkerr/goplexcli/internal/config"
	"github.com/joshkerr/goplexcli/internal/libserver"
	"github.com/joshkerr/goplexcli/internal/plex"
	"github.com/joshkerr/goplexcli/internal/tmdb"
	"golang.org/x/sync/errgroup"
)

// Options configures an index run.
type Options struct {
	// TMDB enriches items with metadata and artwork; nil indexes names only.
	TMDB *tmdb.Client
	// ServerName labels the items (MediaItem.ServerName); ServerURL is the
	// base URL of the library server that streams them (MediaItem.ServerURL).
	ServerName string
	ServerURL  string
	// PathMappings translate local paths into rclone paths for libraries
	// that don't set their own Remote.
	PathMappings []config.PathMapping
	// Scan options are passed through to ScanFolder.
	Scan ScanOptions
	// Probe runs ffprobe on new files for exact durations and audio languages.
	Probe      bool
	FFProbeBin string
	// Force re-enriches every file instead of reusing unchanged items
	// (playback progress is still carried over).
	Force bool
	// Overrides pins titles to chosen TMDB ids (manual match fixes).
	Overrides *Overrides
	// Workers bounds concurrent TMDB lookups (default 8).
	Workers int
	// Logf receives per-item warnings (unmatched titles, probe failures).
	Logf func(format string, args ...any)
	// Now overrides the clock in tests.
	Now func() time.Time
}

// Progress reports index activity: Phase is "scan" (Done = files found so
// far, Total 0) or "match" (Done/Total titles enriched).
type Progress struct {
	Library   string
	Phase     string
	Done      int
	Total     int
	LibNum    int
	Libraries int
}

// ProgressFunc receives Progress updates.
type ProgressFunc func(Progress)

// Result is the outcome of an index run.
type Result struct {
	Items []plex.MediaItem
	// Unmatched counts titles that TMDB could not identify.
	Unmatched int
	// Reused counts items carried over unchanged from the previous index.
	Reused int
	// KeyMigration maps keys of replaced items (e.g. Plex items for the same
	// files) to the new local keys; see KeyMigration.
	KeyMigration map[string]string
}

// Index scans the given libraries and returns the complete set of items for
// them. existing is the previous index (any items; only those with this
// ServerName and a local key are consulted) so unchanged files keep their
// metadata, playback progress and identity without another TMDB round trip.
// Files that disappeared are simply absent from the result.
func Index(ctx context.Context, libs []config.LibraryFolder, existing []plex.MediaItem, opts Options, progress ProgressFunc) (*Result, error) {
	if opts.Workers <= 0 {
		opts.Workers = 8
	}
	if opts.Logf == nil {
		opts.Logf = func(string, ...any) {}
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if progress == nil {
		progress = func(Progress) {}
	}

	ix := newIndexer(opts, existing)
	res := &Result{}
	for n, lib := range libs {
		if !lib.Enabled {
			continue
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		report := func(p Progress) {
			p.Library = lib.Name
			p.LibNum = n + 1
			p.Libraries = len(libs)
			progress(p)
		}
		report(Progress{Phase: "scan"})
		scanOpts := opts.Scan
		scanOpts.Progress = func(found int) { report(Progress{Phase: "scan", Done: found}) }
		files, err := ScanFolder(ctx, lib, scanOpts)
		if err != nil {
			return nil, fmt.Errorf("scan %s: %w", lib.Name, err)
		}

		var items []plex.MediaItem
		switch lib.Type {
		case "show":
			items, err = ix.indexShows(ctx, lib, files, report)
		default:
			items, err = ix.indexMovies(ctx, lib, files, report)
		}
		if err != nil {
			return nil, err
		}
		res.Items = append(res.Items, items...)
	}
	res.Unmatched = ix.unmatched
	res.Reused = ix.reused
	if opts.Overrides.MarkApplied() {
		if err := opts.Overrides.Save(); err != nil {
			opts.Logf("save match overrides: %v", err)
		}
	}
	return res, nil
}

// ItemID derives the stable identity of a file within a library: the library
// name and the file's relative path. Moving the library folder (or mount)
// keeps every key, favorite and playback position intact.
func ItemID(libraryName, rel string) string {
	sum := sha1.Sum([]byte(libraryName + "\x00" + rel))
	return hex.EncodeToString(sum[:8])
}

type indexer struct {
	opts       Options
	prev       map[string]*plex.MediaItem
	prevByPath map[string]*plex.MediaItem
	mu         sync.Mutex
	unmatched  int
	reused     int
}

// newIndexer prepares an indexer that can reuse and carry progress from the
// existing items.
func newIndexer(opts Options, existing []plex.MediaItem) *indexer {
	if opts.Workers <= 0 {
		opts.Workers = 8
	}
	if opts.Logf == nil {
		opts.Logf = func(string, ...any) {}
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	prev := make(map[string]*plex.MediaItem, len(existing))
	prevByPath := make(map[string]*plex.MediaItem)
	for i := range existing {
		it := &existing[i]
		if it.ServerName == opts.ServerName && libserver.IsLocalKey(it.Key) {
			prev[it.Key] = it
			continue
		}
		// Items from other sources (a Plex server indexing the same files)
		// lend their playback progress to the local item for the same file.
		if it.RclonePath != "" {
			prevByPath["r:"+it.RclonePath] = it
		}
		if it.FilePath != "" {
			prevByPath["f:"+it.FilePath] = it
		}
	}
	return &indexer{opts: opts, prev: prev, prevByPath: prevByPath}
}

// base returns an item with the fields every local item shares.
func (ix *indexer) base(lib config.LibraryFolder, f ScannedFile) plex.MediaItem {
	id := ItemID(lib.Name, f.Rel)
	return plex.MediaItem{
		Key:        libserver.ItemKey(id),
		FilePath:   f.Path,
		RclonePath: rclonePath(lib, f, ix.opts.PathMappings),
		ServerName: ix.opts.ServerName,
		ServerURL:  ix.opts.ServerURL,
		AddedAt:    f.ModTime.Unix(),
	}
}

// reuse returns the previous item for a file when nothing about it changed,
// refreshed with the current server labels.
func (ix *indexer) reuse(item plex.MediaItem, pending bool) (plex.MediaItem, bool) {
	if ix.opts.Force || pending {
		return item, false
	}
	old, ok := ix.prev[item.Key]
	if !ok || old.FilePath != item.FilePath || old.AddedAt != item.AddedAt {
		return item, false
	}
	// A previous run that couldn't identify the title gets another chance
	// (the TMDB disk cache makes repeats cheap).
	if old.Thumb == "" && ix.opts.TMDB != nil {
		return item, false
	}
	out := *old
	out.ServerName = item.ServerName
	out.ServerURL = item.ServerURL
	out.RclonePath = item.RclonePath
	return out, true
}

// carryProgress keeps playback state from the previous index when an item is
// rebuilt.
func (ix *indexer) carryProgress(item *plex.MediaItem) {
	old, ok := ix.prev[item.Key]
	if !ok && item.RclonePath != "" {
		old, ok = ix.prevByPath["r:"+item.RclonePath]
	}
	if !ok && item.FilePath != "" {
		old, ok = ix.prevByPath["f:"+item.FilePath]
	}
	if ok {
		item.ViewOffset = old.ViewOffset
		item.ViewCount = old.ViewCount
		item.LastViewedAt = old.LastViewedAt
	}
}

func (ix *indexer) noteUnmatched(what string) {
	ix.mu.Lock()
	ix.unmatched++
	ix.mu.Unlock()
	ix.opts.Logf("no TMDB match for %s", what)
}

// ---- movies ----

func (ix *indexer) indexMovies(ctx context.Context, lib config.LibraryFolder, files []ScannedFile, report func(Progress)) ([]plex.MediaItem, error) {
	// Count videos per folder so a folder holding a single file can lend
	// its name to a file that doesn't carry a year of its own.
	perDir := map[string]int{}
	for _, f := range files {
		perDir[path.Dir(f.Rel)]++
	}

	items := make([]plex.MediaItem, len(files))
	var done int
	var doneMu sync.Mutex
	report(Progress{Phase: "match", Total: len(files)})

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(ix.opts.Workers)
	for i, f := range files {
		i, f := i, f
		g.Go(func() error {
			item, err := ix.movieItem(gctx, lib, f, perDir[path.Dir(f.Rel)])
			if err != nil {
				return err
			}
			items[i] = item
			doneMu.Lock()
			done++
			d := done
			doneMu.Unlock()
			report(Progress{Phase: "match", Done: d, Total: len(files)})
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	return items, nil
}

func (ix *indexer) movieItem(ctx context.Context, lib config.LibraryFolder, f ScannedFile, siblings int) (plex.MediaItem, error) {
	item := ix.base(lib, f)
	item.Type = "movie"
	ov, hasOv := ix.opts.Overrides.Get(item.Key)
	if old, ok := ix.reuse(item, hasOv && !ov.Applied); ok {
		ix.mu.Lock()
		ix.reused++
		ix.mu.Unlock()
		return old, nil
	}

	name := movieName(f.Rel, siblings)
	if hasOv && ov.TMDB > 0 {
		name.TMDB, name.IMDB = ov.TMDB, ""
	}
	item.Title = name.Title
	item.Year = name.Year

	if ix.opts.TMDB != nil {
		m, err := ix.lookupMovie(ctx, name)
		if err != nil {
			return item, err
		}
		if m != nil {
			applyMovie(&item, m)
		} else {
			ix.noteUnmatched(f.Rel)
		}
	}
	if ix.opts.Probe {
		ix.probe(ctx, &item)
	}
	ix.carryProgress(&item)
	return item, nil
}

// movieName decides what a movie file is called: the Plex-style folder name
// when it carries a year or id, else the release name, else a single-file
// folder's name, else the bare file name.
func movieName(rel string, siblings int) NameInfo {
	dir := path.Base(path.Dir(rel))
	file := path.Base(rel)
	var folder NameInfo
	if dir != "." && dir != "/" && dir != "" {
		folder = ParseTitleFolder(dir)
		if folder.Year > 0 || folder.IMDB != "" || folder.TMDB > 0 {
			return folder
		}
	}
	release := ParseReleaseName(file)
	if release.Year > 0 || release.IMDB != "" || release.TMDB > 0 {
		return release
	}
	if folder.Title != "" && siblings == 1 {
		return folder
	}
	return release
}

// lookupMovie resolves a name to TMDB movie details, or nil when nothing
// matches. Transport errors abort the run so a flaky network doesn't leave a
// half-enriched index that later runs consider finished.
func (ix *indexer) lookupMovie(ctx context.Context, name NameInfo) (*tmdb.Movie, error) {
	c := ix.opts.TMDB
	id := name.TMDB
	if id == 0 && name.IMDB != "" {
		mid, _, err := c.FindByIMDB(ctx, name.IMDB)
		if err != nil {
			return nil, err
		}
		id = mid
	}
	if id == 0 && name.Title != "" {
		mid, err := c.SearchMovie(ctx, name.Title, name.Year)
		if err != nil {
			return nil, err
		}
		id = mid
	}
	if id == 0 {
		return nil, nil
	}
	m, err := c.Movie(ctx, id)
	if errors.Is(err, tmdb.ErrNotFound) {
		return nil, nil
	}
	return m, err
}

func applyMovie(item *plex.MediaItem, m *tmdb.Movie) {
	if m.Title != "" {
		item.Title = m.Title
	}
	if m.Year > 0 {
		item.Year = m.Year
	}
	item.Summary = m.Overview
	item.Rating = m.VoteAverage
	if m.Runtime > 0 {
		item.Duration = m.Runtime * 60000
	}
	item.ContentRating = m.Certification
	if len(m.Studios) > 0 {
		item.Studio = m.Studios[0]
	}
	item.Director = strings.Join(m.Directors, ", ")
	item.Genre = strings.Join(m.Genres, ", ")
	item.Cast = strings.Join(m.Cast, ", ")
	item.OriginallyAired = m.ReleaseDate
	if m.OriginalTitle != "" && m.OriginalTitle != m.Title {
		item.OriginalTitle = m.OriginalTitle
	}
	if m.PosterPath != "" {
		item.Thumb = libserver.ThumbPath(m.PosterPath)
	}
}

// ---- shows ----

// showGroup is one show folder (or one loose show name) with its episode files.
type showGroup struct {
	key   string // "dir:<show folder>" or "loose:<title key>"; see ShowOverrideKey
	dir   string // show folder relative path, "" for loose episodes
	name  NameInfo
	files []episodeFile
}

type episodeFile struct {
	file    ScannedFile
	season  int
	episode int
	info    EpisodeInfo
	hasTag  bool
	seasonF int // season from the folder name, -1 if none
}

func (ix *indexer) indexShows(ctx context.Context, lib config.LibraryFolder, files []ScannedFile, report func(Progress)) ([]plex.MediaItem, error) {
	groups := groupEpisodes(files)
	report(Progress{Phase: "match", Total: len(groups)})

	results := make([][]plex.MediaItem, len(groups))
	var done int
	var doneMu sync.Mutex
	titles := &showTitles{seen: map[string]int{}}

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(ix.opts.Workers)
	for i, grp := range groups {
		i, grp := i, grp
		g.Go(func() error {
			items, err := ix.showItems(gctx, lib, grp, titles)
			if err != nil {
				return err
			}
			results[i] = items
			doneMu.Lock()
			done++
			d := done
			doneMu.Unlock()
			report(Progress{Phase: "match", Done: d, Total: len(groups)})
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	var items []plex.MediaItem
	for _, r := range results {
		items = append(items, r...)
	}
	return items, nil
}

// groupEpisodes buckets files by show folder: a file inside "Season NN" belongs
// to the folder above it, otherwise to its parent folder; files at the library
// root are grouped by the show name in their file name.
func groupEpisodes(files []ScannedFile) []*showGroup {
	byKey := map[string]*showGroup{}
	var order []string
	for _, f := range files {
		info, hasTag := ParseEpisodeFile(path.Base(f.Rel))
		ef := episodeFile{file: f, info: info, hasTag: hasTag, seasonF: -1}

		parent := path.Dir(f.Rel)
		showDir := parent
		if parent != "." {
			if s, ok := ParseSeasonFolder(path.Base(parent)); ok {
				ef.seasonF = s
				showDir = path.Dir(parent)
			}
		}

		var key string
		var name NameInfo
		if showDir == "." || showDir == "" {
			showDir = ""
			name = NameInfo{Title: info.Show}
			key = "loose:" + TitleKey(info.Show)
		} else {
			name = ParseTitleFolder(path.Base(showDir))
			key = "dir:" + showDir
		}

		switch {
		case hasTag && info.SeasonKnown:
			ef.season, ef.episode = info.Season, info.Episode
		case hasTag && ef.seasonF >= 0:
			// "E05" alone: the season comes from the folder.
			ef.season, ef.episode = ef.seasonF, info.Episode
		case hasTag:
			ef.season, ef.episode = 1, info.Episode
		case ef.seasonF >= 0:
			ef.season = ef.seasonF
		}

		grp, ok := byKey[key]
		if !ok {
			grp = &showGroup{key: key, dir: showDir, name: name}
			byKey[key] = grp
			order = append(order, key)
		}
		grp.files = append(grp.files, ef)
	}
	out := make([]*showGroup, 0, len(order))
	for _, k := range order {
		out = append(out, byKey[k])
	}
	return out
}

// showTitles de-duplicates show display names across different TMDB ids so
// two shows that share a name don't merge in views that group by ParentTitle.
type showTitles struct {
	mu   sync.Mutex
	seen map[string]int // title -> tmdb id
}

func (t *showTitles) claim(title string, id, year int) string {
	t.mu.Lock()
	defer t.mu.Unlock()
	if owner, ok := t.seen[title]; ok && owner != id && year > 0 {
		alt := fmt.Sprintf("%s (%d)", title, year)
		if _, taken := t.seen[alt]; !taken {
			t.seen[alt] = id
			return alt
		}
	}
	if _, ok := t.seen[title]; !ok {
		t.seen[title] = id
	}
	return title
}

func (ix *indexer) showItems(ctx context.Context, lib config.LibraryFolder, grp *showGroup, titles *showTitles) ([]plex.MediaItem, error) {
	// Build the per-file base items first so unchanged episodes can be
	// reused before any lookup happens.
	items := make([]plex.MediaItem, len(grp.files))
	reused := make([]bool, len(grp.files))
	needLookup := false
	ov, hasOv := ix.opts.Overrides.Get(ShowOverrideKey(lib.Name, grp.key))
	for i, ef := range grp.files {
		item := ix.base(lib, ef.file)
		item.Type = "episode"
		if old, ok := ix.reuse(item, hasOv && !ov.Applied); ok {
			items[i] = old
			reused[i] = true
			ix.mu.Lock()
			ix.reused++
			ix.mu.Unlock()
			continue
		}
		items[i] = item
		needLookup = true
	}
	if !needLookup {
		return items, nil
	}

	showTitle := grp.name.Title
	showYear := grp.name.Year
	var show *tmdb.Show
	seasons := map[int]map[int]tmdb.Episode{}
	if ix.opts.TMDB != nil {
		name := grp.name
		if hasOv && ov.TMDB > 0 {
			name.TMDB, name.IMDB = ov.TMDB, ""
		}
		var err error
		show, err = ix.lookupShow(ctx, name)
		if err != nil {
			return nil, err
		}
		if show == nil {
			what := grp.dir
			if what == "" {
				what = "loose episodes of " + grp.name.Title
			}
			ix.noteUnmatched(what)
		} else {
			if show.Name != "" {
				showTitle = titles.claim(show.Name, show.ID, show.Year)
			}
			if show.Year > 0 {
				showYear = show.Year
			}
			wanted := map[int]bool{}
			for i, ef := range grp.files {
				if !reused[i] {
					wanted[ef.season] = true
					// Date-named episodes may live in a numbered season
					// folder; look there too for the air date.
					if ef.info.AirDate != "" && ef.seasonF >= 0 {
						wanted[ef.seasonF] = true
					}
				}
			}
			for s := range wanted {
				eps, err := ix.opts.TMDB.Season(ctx, show.ID, s)
				if err != nil && !errors.Is(err, tmdb.ErrNotFound) {
					return nil, err
				}
				m := map[int]tmdb.Episode{}
				for _, e := range eps {
					m[e.EpisodeNumber] = e
				}
				seasons[s] = m
			}
		}
	}

	for i, ef := range grp.files {
		if reused[i] {
			continue
		}
		item := &items[i]
		item.ParentTitle = showTitle
		item.ParentIndex = int64(ef.season)
		item.Index = int64(ef.episode)
		item.GrandTitle = seasonTitle(ef.season)
		item.Year = showYear
		item.Title = ef.info.EpisodeTitle
		if show != nil {
			applyShow(item, show)
		}
		if ef.info.AirDate != "" {
			ix.resolveByAirDate(item, ef, seasons)
		} else if ep, ok := seasons[ef.season][ef.episode]; ok && show != nil {
			applyEpisode(item, ep)
		}
		if item.Title == "" {
			if ef.episode > 0 {
				item.Title = fmt.Sprintf("Episode %d", ef.episode)
			} else {
				// Nothing recognizable in the name: show the file name so
				// the episode is at least identifiable.
				item.Title = CleanTitle(strings.TrimSuffix(path.Base(ef.file.Rel), path.Ext(ef.file.Rel)))
			}
		}
		if ix.opts.Probe {
			ix.probe(ctx, item)
		}
		ix.carryProgress(item)
	}
	return items, nil
}

// resolveByAirDate numbers a date-named episode from the TMDB season data
// (the folder's season first, then the year as a season), falling back to a
// chronological MMDD index when the date isn't listed.
func (ix *indexer) resolveByAirDate(item *plex.MediaItem, ef episodeFile, seasons map[int]map[int]tmdb.Episode) {
	candidates := []int{}
	if ef.seasonF >= 0 {
		candidates = append(candidates, ef.seasonF)
	}
	candidates = append(candidates, ef.season)
	for _, s := range candidates {
		for _, ep := range seasons[s] {
			if ep.AirDate == ef.info.AirDate {
				item.ParentIndex = int64(s)
				item.GrandTitle = seasonTitle(s)
				item.Index = int64(ep.EpisodeNumber)
				applyEpisode(item, ep)
				return
			}
		}
	}
	// Unknown to TMDB: keep the folder's season (or the year) and order by
	// month and day so the episodes still line up chronologically.
	if ef.seasonF >= 0 {
		item.ParentIndex = int64(ef.seasonF)
		item.GrandTitle = seasonTitle(ef.seasonF)
	}
	if d, err := time.Parse("2006-01-02", ef.info.AirDate); err == nil {
		item.Index = int64(int(d.Month())*100 + d.Day())
	}
	item.OriginallyAired = ef.info.AirDate
	if item.Title == "" {
		item.Title = ef.info.AirDate
	}
}

func seasonTitle(season int) string {
	if season == 0 {
		return "Specials"
	}
	return fmt.Sprintf("Season %d", season)
}

func (ix *indexer) lookupShow(ctx context.Context, name NameInfo) (*tmdb.Show, error) {
	c := ix.opts.TMDB
	id := name.TMDB
	if id == 0 && name.IMDB != "" {
		_, tid, err := c.FindByIMDB(ctx, name.IMDB)
		if err != nil {
			return nil, err
		}
		id = tid
	}
	if id == 0 && name.Title != "" {
		tid, err := c.SearchTV(ctx, name.Title, name.Year)
		if err != nil {
			return nil, err
		}
		id = tid
	}
	if id == 0 {
		return nil, nil
	}
	s, err := c.TV(ctx, id)
	if errors.Is(err, tmdb.ErrNotFound) {
		return nil, nil
	}
	return s, err
}

func applyShow(item *plex.MediaItem, s *tmdb.Show) {
	item.ContentRating = s.ContentRating
	if len(s.Studios) > 0 {
		item.Studio = s.Studios[0]
	}
	item.Genre = strings.Join(s.Genres, ", ")
	item.Cast = strings.Join(s.Cast, ", ")
	item.Director = strings.Join(s.Creators, ", ")
	if s.EpisodeRunTime > 0 {
		item.Duration = s.EpisodeRunTime * 60000
	}
	if s.PosterPath != "" {
		item.GrandparentThumb = libserver.ThumbPath(s.PosterPath)
		item.Thumb = item.GrandparentThumb
	}
}

func applyEpisode(item *plex.MediaItem, e tmdb.Episode) {
	if e.Name != "" {
		item.Title = e.Name
	}
	item.Summary = e.Overview
	item.OriginallyAired = e.AirDate
	if e.VoteAverage > 0 {
		item.Rating = e.VoteAverage
	}
	if e.Runtime > 0 {
		item.Duration = e.Runtime * 60000
	}
	if len(e.Directors) > 0 {
		item.Director = strings.Join(e.Directors, ", ")
	}
	if e.StillPath != "" {
		item.Thumb = libserver.ThumbPath(e.StillPath)
	}
}

// ---- helpers ----

// rclonePath maps a scanned file onto an rclone path: the library's Remote
// when set, else the longest matching configured path mapping, else "".
func rclonePath(lib config.LibraryFolder, f ScannedFile, mappings []config.PathMapping) string {
	if lib.Remote != "" {
		if strings.HasSuffix(lib.Remote, ":") {
			return lib.Remote + f.Rel
		}
		return strings.TrimRight(lib.Remote, "/") + "/" + f.Rel
	}
	local := strings.ReplaceAll(f.Path, "\\", "/")
	best := -1
	for i, m := range mappings {
		p := strings.ReplaceAll(m.Prefix, "\\", "/")
		if p != "" && strings.HasPrefix(local, p) && (best < 0 || len(p) > len(mappings[best].Prefix)) {
			best = i
		}
	}
	if best < 0 {
		return ""
	}
	return mappings[best].Remote + strings.TrimPrefix(local, strings.ReplaceAll(mappings[best].Prefix, "\\", "/"))
}

// probe fills Duration and AudioLanguages from ffprobe when available.
func (ix *indexer) probe(ctx context.Context, item *plex.MediaItem) {
	info, err := Probe(ctx, ix.opts.FFProbeBin, item.FilePath)
	if err != nil {
		ix.opts.Logf("ffprobe %s: %v", item.FilePath, err)
		return
	}
	if info.DurationMs > 0 {
		item.Duration = info.DurationMs
	}
	if len(info.AudioLanguages) > 0 {
		item.AudioLanguages = strings.Join(info.AudioLanguages, ", ")
	}
}

// SortItems orders items the way Plex listings arrive (movies by title, then
// episodes by show/season/episode) so cache diffs stay readable.
func SortItems(items []plex.MediaItem) {
	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i], items[j]
		if a.Type != b.Type {
			return a.Type < b.Type
		}
		if a.Type == "episode" {
			if a.ParentTitle != b.ParentTitle {
				return a.ParentTitle < b.ParentTitle
			}
			if a.ParentIndex != b.ParentIndex {
				return a.ParentIndex < b.ParentIndex
			}
			if a.Index != b.Index {
				return a.Index < b.Index
			}
		}
		if a.Title != b.Title {
			return a.Title < b.Title
		}
		return a.Key < b.Key
	})
}
