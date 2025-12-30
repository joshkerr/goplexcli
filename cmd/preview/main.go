package main

import (
	"crypto/md5"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	
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
	fmt.Println(strings.Repeat("=", 60))
	fmt.Printf(" %s\n", item.Title)
	fmt.Println(strings.Repeat("=", 60))
	
	if item.Type == "movie" {
		if item.Year > 0 {
			fmt.Printf("\nYear: %d\n", item.Year)
		}
	} else if item.Type == "episode" {
		fmt.Printf("\nShow: %s\n", item.ParentTitle)
		fmt.Printf("Season %d, Episode %d\n", item.ParentIndex, item.Index)
	}
	
	if item.Rating > 0 {
		fmt.Printf("Rating: %.1f/10\n", item.Rating)
	}
	
	if item.Duration > 0 {
		minutes := item.Duration / 60000
		fmt.Printf("Duration: %d minutes\n", minutes)
	}
	
	if item.Summary != "" {
		fmt.Printf("\nSummary:\n%s\n", wrapText(item.Summary, 58))
	}
	
	if item.FilePath != "" {
		fmt.Printf("\nFile: %s\n", item.FilePath)
	}
	
	fmt.Println(strings.Repeat("=", 60))
	
	// Note: Poster display disabled in preview window due to terminal artifacts
	// Chafa images persist in the terminal and overlap when scrolling through items
	// Consider adding poster display in a separate full-screen view mode
	
	fmt.Println("\nPress 'i' to toggle this preview")
}

// downloadPoster downloads the poster image and returns the local path
func downloadPoster(plexURL, thumbPath, token string) string {
	if thumbPath == "" {
		return ""
	}
	
	// Create cache directory
	cacheDir := filepath.Join(os.TempDir(), "goplexcli-posters")
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return ""
	}
	
	// Create filename from hash of thumb path
	hash := md5.Sum([]byte(thumbPath))
	posterFile := filepath.Join(cacheDir, fmt.Sprintf("%x.jpg", hash))
	
	// Check if already downloaded
	if _, err := os.Stat(posterFile); err == nil {
		return posterFile
	}
	
	// Download poster
	url := plexURL + thumbPath + "?X-Plex-Token=" + token
	resp, err := http.Get(url)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	
	if resp.StatusCode != 200 {
		return ""
	}
	
	// Save to file
	out, err := os.Create(posterFile)
	if err != nil {
		return ""
	}
	defer out.Close()
	
	if _, err := io.Copy(out, resp.Body); err != nil {
		os.Remove(posterFile)
		return ""
	}
	
	return posterFile
}

// displayPoster renders the poster using chafa
func displayPoster(posterPath string) {
	// Check if chafa is available
	if _, err := exec.LookPath("chafa"); err != nil {
		return
	}
	
	// Run chafa to display the image
	// Use --size to fit preview window (80 columns, 20 rows max for poster)
	cmd := exec.Command("chafa", "--size", "40x20", "--animate", "off", posterPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Run()
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
