package artwork

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"github.com/joshkerr/goplexcli/internal/config"
	"github.com/joshkerr/goplexcli/internal/dlengine"
	"github.com/joshkerr/goplexcli/internal/library"
)

// Processor applies artwork to completed downloads one at a time (embedding
// can rewrite a large file, so parallel runs would thrash the disk).
type Processor struct {
	opts Options
	mu   sync.Mutex
}

// NewProcessor builds a Processor from config, or nil when artwork is off.
func NewProcessor(cfg *config.Config, logf func(string, ...any)) *Processor {
	cacheDir, err := config.GetCacheDir()
	if err != nil {
		return nil
	}
	opts, on := NewOptions(cfg, cacheDir)
	if !on {
		return nil
	}
	opts.Logf = logf
	return &Processor{opts: opts}
}

// NewProcessorWith wraps explicit options.
func NewProcessorWith(opts Options) *Processor { return &Processor{opts: opts} }

// Options exposes the processor's options (for status messages).
func (p *Processor) Options() Options { return p.opts }

// Outcome describes one processed file for logs and toasts.
type Outcome struct {
	File    string
	Item    Item
	Result  Result
	Err     error
	Sidecar bool // poster.jpg written next to the file
}

// String renders the outcome for a log line.
func (o Outcome) String() string {
	name := filepath.Base(o.File)
	switch {
	case o.Err != nil:
		return fmt.Sprintf("artwork for %s: %v", name, o.Err)
	case o.Result.Skipped != "":
		return fmt.Sprintf("artwork for %s skipped: %s", name, o.Result.Skipped)
	default:
		s := fmt.Sprintf("artwork for %s: %s art embedded with %s", name, o.Result.Source, o.Result.Tool)
		if o.Sidecar {
			s += " (+ poster.jpg)"
		}
		return s
	}
}

// Process embeds art into one file. For shows, a poster.jpg is also dropped
// into the file's folder when the folder is named after the show (the
// sorted "TV Shows/<show>/" layout) and has no poster yet.
func (p *Processor) Process(ctx context.Context, file string, it Item) Outcome {
	if p == nil {
		return Outcome{File: file, Item: it, Result: Result{Skipped: "artwork disabled"}}
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	out := Outcome{File: file, Item: it}
	out.Result, out.Err = Apply(ctx, p.opts, file, it)
	if out.Err != nil || out.Result.ArtPath == "" {
		return out
	}
	if it.Kind == "tv" {
		if dir := showFolder(file, it.Title); dir != "" {
			if wrote, err := WriteSidecar(dir, out.Result.ArtPath); err == nil && wrote {
				out.Sidecar = true
			}
		}
	}
	return out
}

// showFolder returns the folder that represents the show for an episode
// file — its parent, or the grandparent when the parent is a "Season NN"
// folder — provided that folder is named after the show (sorted
// "TV Shows/<show>/" downloads, or Plex-style "Show (Year)/Season 01/").
// "" when the file sits somewhere that isn't clearly the show's own folder.
func showFolder(file, show string) string {
	dir := filepath.Dir(file)
	if _, isSeason := library.ParseSeasonFolder(filepath.Base(dir)); isSeason {
		dir = filepath.Dir(dir)
	}
	base := filepath.Base(dir)
	if base == dlengine.SanitizeDirName(show) {
		return dir
	}
	if library.TitleKey(library.ParseTitleFolder(base).Title) == library.TitleKey(show) {
		return dir
	}
	return ""
}

// ProcessDownload embeds art for a finished download record, deriving the
// lookup from the title, year and media type it carries (episode records
// carry their show's title).
func (p *Processor) ProcessDownload(ctx context.Context, rec dlengine.Progress) Outcome {
	it := Item{Title: rec.Title, Year: rec.Year, Kind: "movie"}
	if rec.MediaType == "episode" {
		it.Kind = "tv"
	}
	return p.Process(ctx, rec.Dest, it)
}
