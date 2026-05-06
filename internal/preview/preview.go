// Package preview renders the fzf preview pane for goplexcli media items.
// It is invoked as a hidden subcommand of the main binary so users only
// need to install one executable.
package preview

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joshkerr/goplexcli/internal/plex"
)

type previewData struct {
	Media     []plex.MediaItem `json:"media"`
	PlexURL   string           `json:"plex_url"`
	PlexToken string           `json:"plex_token"`
}

// Run reads the JSON data file, looks up the item at index, and writes the
// formatted preview to out. Returns an error suitable for surfacing in fzf's
// preview pane (also rendered to out so the user sees it).
func Run(out io.Writer, dataFile, indexStr string) error {
	index, err := strconv.Atoi(indexStr)
	if err != nil {
		fmt.Fprintf(out, "Invalid index: %v\n", err)
		return err
	}

	data, err := os.ReadFile(dataFile)
	if err != nil {
		fmt.Fprintf(out, "Error reading data file: %v\n", err)
		return err
	}

	var pd previewData
	if err := json.Unmarshal(data, &pd); err != nil {
		fmt.Fprintf(out, "Error parsing data: %v\n", err)
		return err
	}

	if index < 0 || index >= len(pd.Media) {
		fmt.Fprintln(out, "Index out of range")
		return fmt.Errorf("index %d out of range", index)
	}

	render(out, pd.Media[index])
	return nil
}

func render(out io.Writer, item plex.MediaItem) {
	fmt.Fprintln(out, strings.Repeat("─", 60))
	fmt.Fprintf(out, " %s\n", item.Title)
	fmt.Fprintln(out, strings.Repeat("─", 60))

	switch item.Type {
	case "movie":
		if item.Year > 0 {
			fmt.Fprintf(out, "\nYear: %d\n", item.Year)
		}
	case "episode":
		fmt.Fprintf(out, "\nShow: %s\n", item.ParentTitle)
		fmt.Fprintf(out, "Season %d, Episode %d\n", item.ParentIndex, item.Index)
		if item.OriginallyAired != "" {
			fmt.Fprintf(out, "Aired: %s\n", item.OriginallyAired)
		}
	}

	if item.Duration > 0 {
		if item.ViewCount > 0 {
			fmt.Fprintf(out, "\nWatched (%d time", item.ViewCount)
			if item.ViewCount > 1 {
				fmt.Fprint(out, "s")
			}
			fmt.Fprintln(out, ")")
		} else if item.ViewOffset > 0 {
			pct := int(float64(item.ViewOffset) * 100 / float64(item.Duration))
			if pct >= 95 {
				fmt.Fprintln(out, "\nWatched")
			} else {
				mins := item.ViewOffset / 60000
				fmt.Fprintf(out, "\nIn Progress: %d%% (%d min)\n", pct, mins)
			}
		} else {
			fmt.Fprintln(out, "\nUnwatched")
		}
	}

	if item.Rating > 0 || item.ContentRating != "" {
		fmt.Fprintln(out)
		if item.Rating > 0 {
			fmt.Fprintf(out, "Rating: %.1f/10", item.Rating)
			if item.ContentRating != "" {
				fmt.Fprintf(out, "  |  %s", item.ContentRating)
			}
			fmt.Fprintln(out)
		} else if item.ContentRating != "" {
			fmt.Fprintf(out, "%s\n", item.ContentRating)
		}
	}

	if item.Duration > 0 {
		minutes := item.Duration / 60000
		if minutes >= 60 {
			hours := minutes / 60
			mins := minutes % 60
			fmt.Fprintf(out, "Duration: %dh %dm\n", hours, mins)
		} else {
			fmt.Fprintf(out, "Duration: %d min\n", minutes)
		}
	}

	if item.Genre != "" {
		fmt.Fprintf(out, "Genre: %s\n", item.Genre)
	}
	if item.Director != "" {
		fmt.Fprintf(out, "Director: %s\n", item.Director)
	}
	if item.Cast != "" {
		fmt.Fprintf(out, "Cast: %s\n", item.Cast)
	}
	if item.Studio != "" {
		fmt.Fprintf(out, "Studio: %s\n", item.Studio)
	}

	if item.Summary != "" {
		fmt.Fprintf(out, "\nSummary:\n%s\n", wrapText(item.Summary, 56))
	}

	if item.AddedAt > 0 {
		addedTime := time.Unix(item.AddedAt, 0)
		fmt.Fprintf(out, "\nAdded: %s\n", addedTime.Format("Jan 2, 2006"))
	}

	if item.FilePath != "" {
		fmt.Fprintf(out, "\nFile: %s\n", item.FilePath)
	}

	fmt.Fprintln(out, strings.Repeat("─", 60))
	fmt.Fprintln(out, "\nPress Ctrl+P to toggle this preview")
}

func wrapText(text string, width int) string {
	words := strings.Fields(text)
	if len(words) == 0 {
		return text
	}

	var lines []string
	var currentLine string

	for _, word := range words {
		if len(currentLine)+len(word)+1 > width {
			if currentLine != "" {
				lines = append(lines, currentLine)
			}
			currentLine = word
		} else {
			if currentLine == "" {
				currentLine = word
			} else {
				currentLine += " " + word
			}
		}
	}

	if currentLine != "" {
		lines = append(lines, currentLine)
	}

	return strings.Join(lines, "\n")
}
