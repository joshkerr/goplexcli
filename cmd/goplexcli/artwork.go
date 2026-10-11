package main

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/joshkerr/goplexcli/internal/artwork"
	"github.com/joshkerr/goplexcli/internal/config"
	"github.com/joshkerr/goplexcli/internal/library"
	"github.com/spf13/cobra"
)

var (
	artworkTitle    string
	artworkYear     int
	artworkTV       bool
	artworkForce    bool
	artworkVertical bool
	artworkDryRun   bool
)

// newArtworkCmd builds `goplexcli artwork`: embed cover art into video files
// that are already on disk (downloads made before this feature existed, or
// files from elsewhere). Titles come from the Plex-style folder and file
// names unless --title/--year are given.
func newArtworkCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "artwork <file or folder>...",
		Short: "Embed cover art into video files (Apple TV art first, then TMDB, then Netflix)",
		Long: `Find cover art for movies and TV shows already on disk and embed it into
the files (mkvpropedit for MKV, an ffmpeg remux for MP4/M4V). Art is looked
up on Apple TV's catalog first, then TMDB, then Netflix (with its title logo
composited on). Folders are walked recursively; files that already carry
cover art are left alone unless --force is given.

Titles and years are read from the names the same way the indexer reads
them ("Title (Year)" folders, release names, "Show/Season 01/Show.S01E02");
override with --title/--year/--tv for a single file.

Downloads made with goplexcli get their art automatically; this command is
for everything else. Needs ffmpeg and ffprobe (mkvpropedit recommended).`,
		Args: cobra.MinimumNArgs(1),
		RunE: runArtwork,
	}
	cmd.Flags().StringVar(&artworkTitle, "title", "", "Title to look up (default: parsed from the name)")
	cmd.Flags().IntVar(&artworkYear, "year", 0, "Release year to narrow the lookup")
	cmd.Flags().BoolVar(&artworkTV, "tv", false, "Treat the file(s) as a TV show (art for the show, not the episode)")
	cmd.Flags().BoolVar(&artworkForce, "force", false, "Replace cover art that is already embedded")
	cmd.Flags().BoolVar(&artworkVertical, "vertical", false, "Use 2:3 poster art instead of 16:9 (overrides artwork_orientation)")
	cmd.Flags().BoolVar(&artworkDryRun, "dry-run", false, "Show what would be looked up without changing files")
	return cmd
}

func runArtwork(cmd *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}
	cacheDir, err := config.GetCacheDir()
	if err != nil {
		return err
	}
	opts, _ := artwork.NewOptions(cfg, cacheDir) // the command runs even when downloads have artwork off
	opts.SkipExisting = !artworkForce
	if artworkVertical {
		opts.Orientation = artwork.Vertical
	}
	opts.Logf = func(format string, a ...any) { fmt.Println(warningStyle.Render("  " + fmt.Sprintf(format, a...))) }
	if missing := opts.Tools.Missing(); len(missing) > 0 && !artworkDryRun {
		return fmt.Errorf("artwork embedding needs %s on PATH", strings.Join(missing, ", "))
	}
	if opts.TMDB == nil {
		fmt.Println(warningStyle.Render("No tmdb_api_key configured: only Apple TV art can be found."))
	}

	var files []string
	for _, arg := range args {
		p, err := filepath.Abs(expandHome(arg))
		if err != nil {
			return err
		}
		st, err := os.Stat(p)
		if err != nil {
			return err
		}
		if !st.IsDir() {
			files = append(files, p)
			continue
		}
		err = filepath.WalkDir(p, func(fp string, d fs.DirEntry, werr error) error {
			if werr != nil {
				return nil
			}
			if fp != p && library.IsIgnoredName(d.Name()) {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if !d.IsDir() && library.IsVideoFile(d.Name()) && artwork.SupportedContainer(fp) {
				files = append(files, fp)
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	if len(files) == 0 {
		return fmt.Errorf("no video files found")
	}
	if len(args) > 1 || len(files) > 1 {
		if artworkTitle != "" {
			return fmt.Errorf("--title applies to a single file; pass one file or drop --title")
		}
	}

	fmt.Println(titleStyle.Render(fmt.Sprintf("Cover Art for %d file(s)", len(files))))
	proc := artwork.NewProcessorWith(opts)
	ctx := context.Background()
	done, skipped, failed := 0, 0, 0
	for _, f := range files {
		it := artworkItemFor(f)
		if artworkTitle != "" {
			it.Title = artworkTitle
		}
		if artworkYear > 0 {
			it.Year = artworkYear
		}
		if artworkTV {
			it.Kind = "tv"
		}
		label := filepath.Base(f)
		if artworkDryRun {
			fmt.Printf("%s  →  %s %q (%d)\n", label, it.Kind, it.Title, it.Year)
			continue
		}
		out := proc.Process(ctx, f, it)
		switch {
		case out.Err != nil:
			failed++
			fmt.Println(warningStyle.Render("✗ " + out.String()))
		case out.Result.Skipped != "":
			skipped++
			fmt.Println(infoStyle.Render("– " + out.String()))
		default:
			done++
			fmt.Println(successStyle.Render("✓ " + out.String()))
		}
	}
	if !artworkDryRun {
		fmt.Println(infoStyle.Render(fmt.Sprintf("\n%d embedded, %d skipped, %d failed", done, skipped, failed)))
	}
	return nil
}

// artworkItemFor derives the lookup for a file from its name and folders:
// an episode name (SxxEyy etc.) means a show, named after the show folder
// above any "Season NN" folder; otherwise the Plex-style parent folder or
// the release name identifies a movie.
func artworkItemFor(file string) artwork.Item {
	base := filepath.Base(file)
	dir := filepath.Dir(file)
	if ep, ok := library.ParseEpisodeFile(base); ok {
		showDir := dir
		if _, isSeason := library.ParseSeasonFolder(filepath.Base(dir)); isSeason {
			showDir = filepath.Dir(dir)
		}
		name := library.ParseTitleFolder(filepath.Base(showDir))
		title, year := name.Title, name.Year
		if ep.Show != "" && (title == "" || looksLikeCategory(filepath.Base(showDir))) {
			title = ep.Show
		}
		return artwork.Item{Title: title, Year: year, Kind: "tv"}
	}
	folder := library.ParseTitleFolder(filepath.Base(dir))
	if folder.Year > 0 || folder.IMDB != "" || folder.TMDB > 0 {
		return artwork.Item{Title: folder.Title, Year: folder.Year, Kind: "movie"}
	}
	rel := library.ParseReleaseName(base)
	if rel.Title == "" {
		rel.Title = strings.TrimSuffix(base, path.Ext(base))
	}
	return artwork.Item{Title: rel.Title, Year: rel.Year, Kind: "movie"}
}

// looksLikeCategory guesses that a folder is a library root or category
// ("Movies", "TV Shows", "Downloads") rather than a title.
func looksLikeCategory(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "movies", "movie", "films", "tv", "tv shows", "tvshows", "shows", "series", "downloads", "media", "plex", "":
		return true
	}
	return false
}
