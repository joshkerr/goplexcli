package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joshkerr/goplexcli/internal/plex"
)

type PreviewData struct {
	Media     []plex.MediaItem `json:"media"`
	PlexURL   string           `json:"plex_url"`
	PlexToken string           `json:"plex_token"`
}

func main() {
	if len(os.Args) < 3 {
		fmt.Println("Usage: preview <data-file> <index>")
		os.Exit(1)
	}

	dataFile := os.Args[1]
	indexStr := os.Args[2]

	index, err := strconv.Atoi(indexStr)
	if err != nil {
		fmt.Printf("Invalid index: %v\n", err)
		os.Exit(1)
	}

	// Read preview data
	data, err := os.ReadFile(dataFile)
	if err != nil {
		fmt.Printf("Error reading data file: %v\n", err)
		os.Exit(1)
	}

	var previewData PreviewData
	if err := json.Unmarshal(data, &previewData); err != nil {
		fmt.Printf("Error parsing data: %v\n", err)
		os.Exit(1)
	}

	if index < 0 || index >= len(previewData.Media) {
		fmt.Println("Index out of range")
		os.Exit(1)
	}

	item := previewData.Media[index]

	// Display metadata
	fmt.Println(strings.Repeat("─", 60))
	fmt.Printf(" %s\n", item.Title)
	fmt.Println(strings.Repeat("─", 60))

	// Type-specific info
	switch item.Type {
	case "movie":
		if item.Year > 0 {
			fmt.Printf("\nYear: %d\n", item.Year)
		}
	case "episode":
		fmt.Printf("\nShow: %s\n", item.ParentTitle)
		fmt.Printf("Season %d, Episode %d\n", item.ParentIndex, item.Index)
		if item.OriginallyAired != "" {
			fmt.Printf("Aired: %s\n", item.OriginallyAired)
		}
	}

	// Watch status
	if item.Duration > 0 {
		if item.ViewCount > 0 {
			fmt.Printf("\nWatched (%d time", item.ViewCount)
			if item.ViewCount > 1 {
				fmt.Print("s")
			}
			fmt.Println(")")
		} else if item.ViewOffset > 0 {
			pct := int(float64(item.ViewOffset) * 100 / float64(item.Duration))
			if pct >= 95 {
				fmt.Println("\nWatched")
			} else {
				mins := item.ViewOffset / 60000
				fmt.Printf("\nIn Progress: %d%% (%d min)\n", pct, mins)
			}
		} else {
			fmt.Println("\nUnwatched")
		}
	}

	// Ratings and duration
	if item.Rating > 0 || item.ContentRating != "" {
		fmt.Println()
		if item.Rating > 0 {
			fmt.Printf("Rating: %.1f/10", item.Rating)
			if item.ContentRating != "" {
				fmt.Printf("  |  %s", item.ContentRating)
			}
			fmt.Println()
		} else if item.ContentRating != "" {
			fmt.Printf("%s\n", item.ContentRating)
		}
	}

	if item.Duration > 0 {
		minutes := item.Duration / 60000
		if minutes >= 60 {
			hours := minutes / 60
			mins := minutes % 60
			fmt.Printf("Duration: %dh %dm\n", hours, mins)
		} else {
			fmt.Printf("Duration: %d min\n", minutes)
		}
	}

	// Genre
	if item.Genre != "" {
		fmt.Printf("Genre: %s\n", item.Genre)
	}

	// Director
	if item.Director != "" {
		fmt.Printf("Director: %s\n", item.Director)
	}

	// Cast
	if item.Cast != "" {
		fmt.Printf("Cast: %s\n", item.Cast)
	}

	// Studio
	if item.Studio != "" {
		fmt.Printf("Studio: %s\n", item.Studio)
	}

	// Summary
	if item.Summary != "" {
		fmt.Printf("\nSummary:\n%s\n", wrapText(item.Summary, 56))
	}

	// Added to library
	if item.AddedAt > 0 {
		addedTime := time.Unix(item.AddedAt, 0)
		fmt.Printf("\nAdded: %s\n", addedTime.Format("Jan 2, 2006"))
	}

	// File info
	if item.FilePath != "" {
		fmt.Printf("\nFile: %s\n", item.FilePath)
	}

	fmt.Println(strings.Repeat("─", 60))
	fmt.Println("\nPress Ctrl+P to toggle this preview")
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
