package library

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// ProbeInfo is what ffprobe reports about a media file.
type ProbeInfo struct {
	DurationMs     int
	AudioLanguages []string // distinct ISO 639 tags in stream order
}

// Probe runs ffprobe on a file. bin empty means "ffprobe" from PATH.
func Probe(ctx context.Context, bin, file string) (ProbeInfo, error) {
	if bin == "" {
		bin = "ffprobe"
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin,
		"-v", "error",
		"-show_entries", "format=duration:stream=codec_type:stream_tags=language",
		"-of", "json",
		file,
	)
	out, err := cmd.Output()
	if err != nil {
		return ProbeInfo{}, fmt.Errorf("ffprobe: %w", err)
	}
	var doc struct {
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
		Streams []struct {
			CodecType string `json:"codec_type"`
			Tags      struct {
				Language string `json:"language"`
			} `json:"tags"`
		} `json:"streams"`
	}
	if err := json.Unmarshal(out, &doc); err != nil {
		return ProbeInfo{}, fmt.Errorf("ffprobe output: %w", err)
	}
	var info ProbeInfo
	if secs, err := strconv.ParseFloat(doc.Format.Duration, 64); err == nil && secs > 0 {
		info.DurationMs = int(secs * 1000)
	}
	seen := map[string]bool{}
	for _, s := range doc.Streams {
		if s.CodecType != "audio" {
			continue
		}
		lang := strings.ToLower(strings.TrimSpace(s.Tags.Language))
		if lang == "" || lang == "und" || seen[lang] {
			continue
		}
		seen[lang] = true
		info.AudioLanguages = append(info.AudioLanguages, lang)
	}
	return info, nil
}
