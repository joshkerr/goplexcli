# CLI Command Simplification Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Replace `browse` command with direct `movie`, `tv`, and `queue` commands to eliminate the media type selection step.

**Architecture:** Extract shared media browsing logic into a helper function that both `movie` and `tv` commands call with different filters. Add a standalone `queue` command for queue management.

**Tech Stack:** Go, Cobra CLI framework, fzf for interactive selection

---

## Task 1: Add `movieCmd` and `tvCmd` to main.go

**Files:**
- Modify: `cmd/goplexcli/main.go:51-70` (command definitions)
- Modify: `cmd/goplexcli/main.go:156` (AddCommand registration)

**Step 1: Add movieCmd and tvCmd definitions**

After line 64 (after loginCmd), add:

```go
	// Movie command
	movieCmd := &cobra.Command{
		Use:   "movie",
		Short: "Browse and play movies from your Plex server",
		RunE:  runMovie,
	}

	// TV command
	tvCmd := &cobra.Command{
		Use:   "tv",
		Short: "Browse and play TV shows from your Plex server",
		RunE:  runTV,
	}

	// Queue command
	queueCmd := &cobra.Command{
		Use:   "queue",
		Short: "View and manage download queue",
		RunE:  runQueueCommand,
	}
```

**Step 2: Update AddCommand registration**

Change line 156 from:
```go
	rootCmd.AddCommand(loginCmd, browseCmd, cacheCmd, configCmd, streamCmd, serverCmd, versionCmd)
```

To:
```go
	rootCmd.AddCommand(loginCmd, movieCmd, tvCmd, queueCmd, cacheCmd, configCmd, streamCmd, serverCmd, versionCmd)
```

**Step 3: Run build to verify syntax**

Run: `go build ./cmd/goplexcli`
Expected: Build fails with "undefined: runMovie" (expected - we'll add these next)

**Step 4: Commit command definitions**

```bash
git add cmd/goplexcli/main.go
git commit -m "feat: add movie, tv, and queue command definitions

Replace browse with direct movie/tv/queue commands
"
```

---

## Task 2: Create shared helper function `runMediaBrowser`

**Files:**
- Modify: `cmd/goplexcli/main.go` (add new function after line 605)

**Step 1: Add runMediaBrowser function**

After `runBrowse` function (line 607), add:

```go
// runMediaBrowser handles the shared media browsing flow for movie and tv commands
// mediaType should be "movie" or "episode"
func runMediaBrowser(mediaType string) error {
	// Show logo
	ui.Logo(version)

	// Load config
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("invalid config: %w. Please run 'goplexcli login' first", err)
	}

	// Load cache
	mediaCache, err := cache.Load()
	if err != nil {
		return fmt.Errorf("failed to load cache: %w", err)
	}

	if len(mediaCache.Media) == 0 {
		fmt.Println(warningStyle.Render("Cache is empty. Run 'goplexcli cache reindex' first."))
		return nil
	}

	// Load queue
	q, err := queue.Load()
	if err != nil {
		return fmt.Errorf("failed to load queue: %w", err)
	}

	// Filter media by type
	var filteredMedia []plex.MediaItem
	for _, item := range mediaCache.Media {
		if item.Type == mediaType {
			filteredMedia = append(filteredMedia, item)
		}
	}

	if len(filteredMedia) == 0 {
		typeName := "movies"
		if mediaType == "episode" {
			typeName = "TV shows"
		}
		fmt.Println(warningStyle.Render(fmt.Sprintf("No %s found in cache.", typeName)))
		return nil
	}

	fmt.Println(infoStyle.Render(fmt.Sprintf("Loaded %d items from cache", len(filteredMedia))))
	fmt.Println(infoStyle.Render(fmt.Sprintf("Last updated: %s", mediaCache.LastUpdated.Format(time.RFC822))))

	if q.Len() > 0 {
		fmt.Println(infoStyle.Render(fmt.Sprintf("Queue has %s", ui.PluralizeItems(q.Len()))))
	}

browseLoop:
	for {
		// Check if user wants to view queue first (show option at top of list)
		if q.Len() > 0 && ui.IsAvailable(cfg.FzfPath) {
			// Show queue option in selection
			viewQueue, err := ui.PromptViewQueue(cfg.FzfPath, q.Len())
			if err != nil {
				if err.Error() == "cancelled by user" {
					return nil
				}
				return err
			}
			if viewQueue {
				result, err := handleQueueView(cfg, q)
				if err != nil {
					return err
				}
				if result == "done" {
					return nil
				}
				continue browseLoop
			}
		}

		fmt.Println(infoStyle.Render(fmt.Sprintf("\nBrowsing %d items...\n", len(filteredMedia))))

		// Select media
		var selectedMediaItems []*plex.MediaItem
		if ui.IsAvailable(cfg.FzfPath) {
			selectedIndices, err := ui.SelectMediaWithPreview(filteredMedia, "Select media (TAB for multi-select):", cfg.FzfPath, cfg.PlexURL, cfg.PlexToken)
			if err != nil {
				if err.Error() == "cancelled by user" {
					return nil
				}
				return fmt.Errorf("media selection failed: %w", err)
			}

			for _, index := range selectedIndices {
				if index >= 0 && index < len(filteredMedia) {
					selectedMediaItems = append(selectedMediaItems, &filteredMedia[index])
				}
			}
		} else {
			selectedMedia, err := selectMediaManual(filteredMedia)
			if err != nil {
				return err
			}
			selectedMediaItems = []*plex.MediaItem{selectedMedia}
		}

		if len(selectedMediaItems) == 0 {
			return fmt.Errorf("no media selected")
		}

		// Ask what to do
		var action string
		if ui.IsAvailable(cfg.FzfPath) {
			action, err = ui.PromptActionWithQueue(cfg.FzfPath, q.Len())
			if err != nil {
				if err.Error() == "cancelled by user" {
					return nil
				}
				return err
			}
		} else {
			action, err = promptActionManualWithQueue(q.Len())
			if err != nil {
				return err
			}
		}

		switch action {
		case "watch":
			return handleWatchMultiple(cfg, selectedMediaItems)
		case "download":
			return handleDownloadMultiple(cfg, selectedMediaItems)
		case "queue":
			added := q.Add(selectedMediaItems)
			if err := q.Save(); err != nil {
				return fmt.Errorf("failed to save queue: %w", err)
			}
			skipped := len(selectedMediaItems) - added
			if skipped > 0 {
				fmt.Println(successStyle.Render(fmt.Sprintf("Added %d item(s) to queue (%d duplicate(s) skipped). Queue now has %s.", added, skipped, ui.PluralizeItems(q.Len()))))
			} else {
				fmt.Println(successStyle.Render(fmt.Sprintf("Added %d item(s) to queue. Queue now has %s.", added, ui.PluralizeItems(q.Len()))))
			}
			continue browseLoop
		case "stream":
			if len(selectedMediaItems) > 1 {
				fmt.Println(warningStyle.Render("Note: Stream only supports single selection, using first item"))
			}
			return handleStream(cfg, selectedMediaItems[0])
		case "cancel":
			return nil
		default:
			return nil
		}
	}
}
```

**Step 2: Run build to verify syntax**

Run: `go build ./cmd/goplexcli`
Expected: Build fails with "undefined: ui.PromptViewQueue" (expected - we'll add this in Task 3)

**Step 3: Commit helper function**

```bash
git add cmd/goplexcli/main.go
git commit -m "feat: add runMediaBrowser shared helper function

Extracts common media browsing logic for reuse by movie and tv commands
"
```

---

## Task 3: Add PromptViewQueue to fzf.go

**Files:**
- Modify: `internal/ui/fzf.go` (add new function after line 438)

**Step 1: Add PromptViewQueue function**

After `SelectMediaTypeWithQueue` function, add:

```go
// PromptViewQueue asks if user wants to view queue before browsing
// Returns true if user wants to view queue, false to continue browsing
func PromptViewQueue(fzfPath string, queueCount int) (bool, error) {
	options := []string{
		"Browse Media",
		fmt.Sprintf("View Queue (%s)", PluralizeItems(queueCount)),
	}

	selected, _, err := SelectWithFzf(options, "What would you like to do?", fzfPath)
	if err != nil {
		return false, err
	}

	return strings.HasPrefix(selected, "View Queue"), nil
}
```

**Step 2: Run build to verify**

Run: `go build ./cmd/goplexcli`
Expected: Build fails with "undefined: runMovie" (expected - we'll add wrappers next)

**Step 3: Commit**

```bash
git add internal/ui/fzf.go
git commit -m "feat: add PromptViewQueue function for queue access in movie/tv commands"
```

---

## Task 4: Add runMovie, runTV, and runQueueCommand functions

**Files:**
- Modify: `cmd/goplexcli/main.go` (add after runMediaBrowser)

**Step 1: Add command runner functions**

After `runMediaBrowser`, add:

```go
func runMovie(cmd *cobra.Command, args []string) error {
	return runMediaBrowser("movie")
}

func runTV(cmd *cobra.Command, args []string) error {
	return runMediaBrowser("episode")
}

func runQueueCommand(cmd *cobra.Command, args []string) error {
	// Show logo
	ui.Logo(version)

	// Load config
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Load queue
	q, err := queue.Load()
	if err != nil {
		return fmt.Errorf("failed to load queue: %w", err)
	}

	if q.IsEmpty() {
		fmt.Println(warningStyle.Render("Queue is empty."))
		fmt.Println(infoStyle.Render("Add items with 'goplexcli movie' or 'goplexcli tv'"))
		return nil
	}

	result, err := handleQueueView(cfg, q)
	if err != nil {
		return err
	}

	// If user wants to go back, just exit (no browse to return to)
	if result == "back" {
		return nil
	}

	return nil
}
```

**Step 2: Run build to verify**

Run: `go build ./cmd/goplexcli`
Expected: Build succeeds

**Step 3: Run basic smoke test**

Run: `./goplexcli --help`
Expected: Shows movie, tv, queue commands (not browse)

**Step 4: Commit**

```bash
git add cmd/goplexcli/main.go
git commit -m "feat: add runMovie, runTV, and runQueueCommand functions

Complete implementation of new CLI commands
"
```

---

## Task 5: Remove browseCmd and runBrowse

**Files:**
- Modify: `cmd/goplexcli/main.go:65-70` (remove browseCmd)
- Modify: `cmd/goplexcli/main.go:431-607` (remove runBrowse)

**Step 1: Remove browseCmd definition**

Delete lines 65-70:
```go
	// Browse command
	browseCmd := &cobra.Command{
		Use:   "browse",
		Short: "Browse and play media from your Plex server",
		RunE:  runBrowse,
	}
```

**Step 2: Remove runBrowse function**

Delete the entire `runBrowse` function (lines 431-607).

**Step 3: Run build to verify**

Run: `go build ./cmd/goplexcli`
Expected: Build succeeds

**Step 4: Run help to verify browse is gone**

Run: `./goplexcli --help`
Expected: Shows movie, tv, queue commands; NO browse command

**Step 5: Commit**

```bash
git add cmd/goplexcli/main.go
git commit -m "feat: remove browse command

Replaced by movie, tv, and queue commands for streamlined UX
"
```

---

## Task 6: Remove obsolete SelectMediaTypeWithQueue function

**Files:**
- Modify: `internal/ui/fzf.go:417-438` (remove function)

**Step 1: Remove SelectMediaTypeWithQueue**

Delete the function at lines 417-438:
```go
// SelectMediaTypeWithQueue adds "View Queue" option when queue has items
func SelectMediaTypeWithQueue(fzfPath string, queueCount int) (string, error) {
	...
}
```

**Step 2: Run build to verify**

Run: `go build ./cmd/goplexcli`
Expected: Build succeeds

**Step 3: Commit**

```bash
git add internal/ui/fzf.go
git commit -m "refactor: remove obsolete SelectMediaTypeWithQueue function

No longer needed after browse command removal
"
```

---

## Task 7: Remove obsolete selectMediaTypeManualWithQueue function

**Files:**
- Modify: `cmd/goplexcli/main.go:957-1003` (remove function)

**Step 1: Remove selectMediaTypeManualWithQueue**

Delete the function `selectMediaTypeManualWithQueue` (lines 957-1003).

**Step 2: Run build to verify**

Run: `go build ./cmd/goplexcli`
Expected: Build succeeds

**Step 3: Commit**

```bash
git add cmd/goplexcli/main.go
git commit -m "refactor: remove obsolete selectMediaTypeManualWithQueue function

No longer needed after browse command removal
"
```

---

## Task 8: Update help text in login command

**Files:**
- Modify: `cmd/goplexcli/main.go:347`

**Step 1: Update the help text**

Change line 347 from:
```go
	fmt.Println(infoStyle.Render("\nRun 'goplexcli cache reindex' to build your media cache"))
```

To:
```go
	fmt.Println(infoStyle.Render("\nRun 'goplexcli cache reindex' to build your media cache"))
	fmt.Println(infoStyle.Render("Then use 'goplexcli movie' or 'goplexcli tv' to browse"))
```

**Step 2: Run build to verify**

Run: `go build ./cmd/goplexcli`
Expected: Build succeeds

**Step 3: Commit**

```bash
git add cmd/goplexcli/main.go
git commit -m "docs: update login help text to reference new commands"
```

---

## Task 9: Manual testing

**Step 1: Test movie command**

Run: `./goplexcli movie`
Expected: Shows movie list directly without media type selection

**Step 2: Test tv command**

Run: `./goplexcli tv`
Expected: Shows TV episode list directly without media type selection

**Step 3: Test queue command**

Run: `./goplexcli queue`
Expected: Shows "Queue is empty" message or queue contents

**Step 4: Test help**

Run: `./goplexcli --help`
Expected: Shows movie, tv, queue; NO browse command

**Step 5: Document any issues found**

If issues found, create additional tasks to fix them.

---

## Task 10: Final cleanup and verification

**Step 1: Run go mod tidy**

Run: `go mod tidy`
Expected: No changes (dependencies unchanged)

**Step 2: Run go fmt**

Run: `go fmt ./...`
Expected: No changes (code already formatted)

**Step 3: Final build**

Run: `make build` or `go build ./cmd/goplexcli`
Expected: Build succeeds

**Step 4: Final commit if any formatting changes**

```bash
git add -A
git commit -m "chore: final cleanup and formatting"
```

(Only if there are changes)
