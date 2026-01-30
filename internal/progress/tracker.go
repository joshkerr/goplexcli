package progress

import (
	"context"
	"fmt"
	"log"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/joshkerr/goplexcli/internal/plex"
)

// Position change threshold in seconds - only report if position changed by more than this
const minPositionChangeSec = 5.0

// Tracker monitors MPV playback and reports progress to Plex.
type Tracker struct {
	items      []*plex.MediaItem
	mpv        *MPVClient
	plexClient *plex.Client
	index      int
	mu         sync.RWMutex
	stopCh     chan struct{}
	wg         sync.WaitGroup
}

// NewTracker creates a new progress tracker.
func NewTracker(items []*plex.MediaItem, mpv *MPVClient, plexClient *plex.Client) *Tracker {
	return &Tracker{
		items:      items,
		mpv:        mpv,
		plexClient: plexClient,
		stopCh:     make(chan struct{}),
	}
}

// CurrentIndex returns the current playlist index.
func (t *Tracker) CurrentIndex() int {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.index
}

// SetIndex sets the current playlist index.
func (t *Tracker) SetIndex(idx int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if idx >= 0 && idx < len(t.items) {
		t.index = idx
	}
}

// CurrentMedia returns the currently playing media item.
func (t *Tracker) CurrentMedia() *plex.MediaItem {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if t.index >= 0 && t.index < len(t.items) {
		return t.items[t.index]
	}
	return nil
}

// extractRatingKey extracts the numeric rating key from a Plex media key.
// e.g., "/library/metadata/12345" -> "12345"
func extractRatingKey(key string) string {
	parts := strings.Split(key, "/")
	if len(parts) > 0 {
		return parts[len(parts)-1]
	}
	return key
}

// Start begins tracking playback progress.
// It polls MPV every interval and reports to Plex.
func (t *Tracker) Start(ctx context.Context, interval time.Duration) {
	t.wg.Add(1)
	go func() {
		defer t.wg.Done()
		t.trackLoop(ctx, interval)
	}()
}

// Stop stops the progress tracker.
func (t *Tracker) Stop() {
	close(t.stopCh)
	t.wg.Wait()
}

// trackLoop is the main tracking loop.
func (t *Tracker) trackLoop(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	var lastPos float64
	lastIndex := -1

	for {
		select {
		case <-ctx.Done():
			t.reportFinalPosition()
			return
		case <-t.stopCh:
			t.reportFinalPosition()
			return
		case <-ticker.C:
			t.tick(&lastPos, &lastIndex)
		}
	}
}

// tick performs one tracking iteration.
func (t *Tracker) tick(lastPos *float64, lastIndex *int) {
	if t.mpv == nil {
		return
	}

	// Get current playlist position
	playlistPos, err := t.mpv.GetPlaylistPos()
	if err != nil {
		// MPV may have exited
		return
	}

	// Check if playlist position changed
	if playlistPos != *lastIndex {
		// Report final position for previous item
		if *lastIndex >= 0 && *lastIndex < len(t.items) {
			t.reportPosition(*lastIndex, *lastPos, "stopped")
		}
		*lastIndex = playlistPos
		t.SetIndex(playlistPos)
		*lastPos = 0
	}

	// Get current time position
	pos, err := t.mpv.GetTimePos()
	if err != nil {
		return
	}

	// Only report if position changed significantly
	if math.Abs(pos-*lastPos) > minPositionChangeSec {
		// Get pause state
		paused, err := t.mpv.GetPaused()
		if err != nil {
			paused = false
		}

		state := "playing"
		if paused {
			state = "paused"
		}

		t.reportPosition(playlistPos, pos, state)
		*lastPos = pos
	}
}

// reportPosition reports the current playback position to Plex.
func (t *Tracker) reportPosition(index int, posSeconds float64, state string) {
	if t.plexClient == nil {
		return
	}

	if index < 0 || index >= len(t.items) {
		return
	}

	media := t.items[index]
	ratingKey := extractRatingKey(media.Key)
	timeMs := int(posSeconds * 1000)

	err := t.plexClient.UpdateTimeline(ratingKey, state, timeMs, media.Duration)
	if err != nil {
		log.Printf("Failed to update timeline: %v", err)
	}
}

// reportFinalPosition reports the final position when playback ends.
func (t *Tracker) reportFinalPosition() {
	if t.mpv == nil || t.plexClient == nil {
		return
	}

	pos, err := t.mpv.GetTimePos()
	if err != nil {
		return
	}

	index := t.CurrentIndex()
	t.reportPosition(index, pos, "stopped")
}

// FormatDuration formats milliseconds as HH:MM:SS or MM:SS.
func FormatDuration(ms int) string {
	totalSecs := ms / 1000
	hours := totalSecs / 3600
	mins := (totalSecs % 3600) / 60
	secs := totalSecs % 60

	if hours > 0 {
		return fmt.Sprintf("%d:%02d:%02d", hours, mins, secs)
	}
	return fmt.Sprintf("%d:%02d", mins, secs)
}
