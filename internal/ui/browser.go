package ui

import (
	"fmt"
	"os/exec"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/joshkerr/goplexcli/internal/plex"
	"github.com/sahilm/fuzzy"
)

// Message types for async operations
type posterDownloadedMsg struct {
	thumbPath  string
	posterPath string
}

type posterRenderedMsg struct {
	posterPath     string
	renderedOutput string
}

// Browser is a TUI browser for media items
type BrowserModel struct {
	media          []plex.MediaItem
	filteredMedia  []plex.MediaItem
	cursor         int
	searchInput    textinput.Model
	searching      bool
	width          int
	height         int
	plexURL        string
	plexToken      string
	showPoster     bool
	posterCache    map[string]string // thumbPath -> localPath
	posterLoading  map[string]bool   // thumbPath -> loading state
	renderedPoster map[string]string // posterPath -> rendered output
	quitting       bool
	selected       *plex.MediaItem
}

type keyMap struct {
	Up           key.Binding
	Down         key.Binding
	Search       key.Binding
	Select       key.Binding
	TogglePoster key.Binding
	Quit         key.Binding
	ClearSearch  key.Binding
}

var keys = keyMap{
	Up: key.NewBinding(
		key.WithKeys("up", "k"),
		key.WithHelp("↑/k", "up"),
	),
	Down: key.NewBinding(
		key.WithKeys("down", "j"),
		key.WithHelp("↓/j", "down"),
	),
	Search: key.NewBinding(
		key.WithKeys("/"),
		key.WithHelp("/", "search"),
	),
	Select: key.NewBinding(
		key.WithKeys("enter"),
		key.WithHelp("enter", "select"),
	),
	TogglePoster: key.NewBinding(
		key.WithKeys("p"),
		key.WithHelp("p", "toggle poster"),
	),
	Quit: key.NewBinding(
		key.WithKeys("q", "ctrl+c", "esc"),
		key.WithHelp("q", "quit"),
	),
	ClearSearch: key.NewBinding(
		key.WithKeys("esc"),
		key.WithHelp("esc", "clear search"),
	),
}

// NewBrowser creates a new browser model
func NewBrowser(media []plex.MediaItem, plexURL, plexToken string) *BrowserModel {
	ti := textinput.New()
	ti.Placeholder = "Type to search..."
	ti.CharLimit = 100
	ti.Width = 50

	return &BrowserModel{
		media:          media,
		filteredMedia:  media,
		searchInput:    ti,
		plexURL:        plexURL,
		plexToken:      plexToken,
		posterCache:    make(map[string]string),
		posterLoading:  make(map[string]bool),
		renderedPoster: make(map[string]string),
		showPoster:     true,
	}
}

func (m *BrowserModel) Init() tea.Cmd {
	// Start downloading poster for first item
	return m.maybeDownloadPoster()
}

func (m *BrowserModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case posterDownloadedMsg:
		// Store downloaded poster in cache
		if msg.posterPath != "" {
			m.posterCache[msg.thumbPath] = msg.posterPath
			delete(m.posterLoading, msg.thumbPath)
			// Trigger rendering
			return m, m.renderPosterAsync(msg.posterPath)
		}
		delete(m.posterLoading, msg.thumbPath)
		return m, nil

	case posterRenderedMsg:
		// Store rendered poster
		if msg.renderedOutput != "" {
			m.renderedPoster[msg.posterPath] = msg.renderedOutput
		}
		return m, nil

	case tea.KeyMsg:
		// If searching, handle search input
		if m.searching {
			switch msg.Type {
			case tea.KeyEsc:
				m.searching = false
				m.searchInput.Blur()
				m.searchInput.SetValue("")
				m.filteredMedia = m.media
				m.cursor = 0
				return m, nil
			case tea.KeyEnter:
				m.searching = false
				m.searchInput.Blur()
				return m, nil
			default:
				var cmd tea.Cmd
				m.searchInput, cmd = m.searchInput.Update(msg)
				m.filterMedia()
				return m, cmd
			}
		}

		// Normal navigation
		switch {
		case key.Matches(msg, keys.Quit):
			m.quitting = true
			return m, tea.Quit
		case key.Matches(msg, keys.Up):
			if m.cursor > 0 {
				m.cursor--
			}
			// Trigger poster download for newly visible item
			return m, m.maybeDownloadPoster()
		case key.Matches(msg, keys.Down):
			if m.cursor < len(m.filteredMedia)-1 {
				m.cursor++
			}
			// Trigger poster download for newly visible item
			return m, m.maybeDownloadPoster()
		case key.Matches(msg, keys.Search):
			m.searching = true
			m.searchInput.Focus()
			return m, textinput.Blink
		case key.Matches(msg, keys.TogglePoster):
			m.showPoster = !m.showPoster
		case key.Matches(msg, keys.Select):
			if len(m.filteredMedia) > 0 {
				m.selected = &m.filteredMedia[m.cursor]
				m.quitting = true
				return m, tea.Quit
			}
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	}

	return m, nil
}

func (m *BrowserModel) View() string {
	if m.quitting {
		return ""
	}

	var b strings.Builder

	// Header with enhanced styling
	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#C084FC")).
		Background(lipgloss.Color("#1F1F23")).
		Padding(0, 1).
		BorderStyle(lipgloss.ThickBorder()).
		BorderForeground(lipgloss.Color("#C084FC")).
		BorderBottom(true).
		Width(m.width - 2)

	countStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#9CA3AF"))

	header := fmt.Sprintf("Media Browser %s", countStyle.Render(fmt.Sprintf("(%d items)", len(m.filteredMedia))))
	b.WriteString(headerStyle.Render(header))
	b.WriteString("\n\n")

	// Search bar with improved styling
	if m.searching {
		searchLabelStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#C084FC")).
			Bold(true)
		b.WriteString(searchLabelStyle.Render("  Search: "))
		b.WriteString(m.searchInput.View())
		b.WriteString("\n")
		// Divider line (guard against narrow terminals)
		if m.width > 6 {
			dividerStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#374151"))
			b.WriteString(dividerStyle.Render("  " + strings.Repeat("─", min(m.width-6, 60))))
		}
		b.WriteString("\n\n")
	} else if m.searchInput.Value() != "" {
		filterLabelStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#9CA3AF"))
		filterValueStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#C084FC")).
			Bold(true)
		hintStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("#6B7280")).
			Italic(true)
		b.WriteString(fmt.Sprintf("  %s %s %s",
			filterLabelStyle.Render("Filter:"),
			filterValueStyle.Render(m.searchInput.Value()),
			hintStyle.Render("(/ to edit, esc to clear)")))
		b.WriteString("\n")
		// Divider line (guard against narrow terminals)
		if m.width > 6 {
			dividerStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#374151"))
			b.WriteString(dividerStyle.Render("  " + strings.Repeat("─", min(m.width-6, 60))))
		}
		b.WriteString("\n")
	}

	// Calculate layout
	listHeight := m.height - 10 // Reserve space for header, footer, search
	listWidth := m.width / 2
	detailWidth := m.width - listWidth - 4

	// Split screen: list on left, details on right
	if m.width > 80 && m.showPoster {
		// Render list
		listStart := max(0, m.cursor-listHeight/2)
		listEnd := min(len(m.filteredMedia), listStart+listHeight)

		listStyle := lipgloss.NewStyle().
			Width(listWidth).
			Height(listHeight).
			BorderStyle(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#4B5563"))

		var listItems []string
		for i := listStart; i < listEnd; i++ {
			item := m.filteredMedia[i]
			cursor := " "
			if i == m.cursor {
				cursor = ">"
			}

			line := m.formatListItem(item, cursor, i == m.cursor, i%2 == 1)
			listItems = append(listItems, line)
		}

		list := listStyle.Render(strings.Join(listItems, "\n"))

		// Render details
		var details string
		if len(m.filteredMedia) > 0 {
			details = m.renderDetails(m.filteredMedia[m.cursor], detailWidth, listHeight)
		}

		// Combine side by side
		b.WriteString(lipgloss.JoinHorizontal(lipgloss.Top, list, "  ", details))
	} else {
		// Single column mode (narrow terminal or poster disabled)
		listStart := max(0, m.cursor-listHeight+5)
		listEnd := min(len(m.filteredMedia), listStart+listHeight-5)

		for i := listStart; i < listEnd; i++ {
			item := m.filteredMedia[i]
			cursor := " "
			if i == m.cursor {
				cursor = ">"
			}
			b.WriteString(m.formatListItem(item, cursor, i == m.cursor, i%2 == 1))
			b.WriteString("\n")
		}

		// Show details below
		if len(m.filteredMedia) > 0 {
			b.WriteString("\n")
			b.WriteString(m.renderDetailsCompact(m.filteredMedia[m.cursor]))
		}
	}

	// Footer with styled help bar
	b.WriteString("\n\n")
	keyStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#C084FC")).
		Bold(true)
	descStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#6B7280"))
	sepStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#374151"))

	sep := sepStyle.Render(" · ")
	help := "  " +
		keyStyle.Render("↑↓") + descStyle.Render(" navigate") + sep +
		keyStyle.Render("/") + descStyle.Render(" search") + sep +
		keyStyle.Render("p") + descStyle.Render(" poster") + sep +
		keyStyle.Render("enter") + descStyle.Render(" select") + sep +
		keyStyle.Render("q") + descStyle.Render(" quit")
	b.WriteString(help)

	return b.String()
}

func (m *BrowserModel) formatListItem(item plex.MediaItem, cursor string, selected bool, alternate bool) string {
	// Build styles - avoid nested Render calls which inject ANSI resets
	var mainFg, dimFg lipgloss.Color
	var bg lipgloss.Color
	bold := false

	if selected {
		// Selected item: accent color with subtle background highlight
		mainFg = lipgloss.Color("#C084FC")
		dimFg = lipgloss.Color("#9CA3AF")
		bg = lipgloss.Color("#2D2D35")
		bold = true
	} else if alternate {
		// Alternating rows: slightly dimmer for visual rhythm
		mainFg = lipgloss.Color("#9CA3AF")
		dimFg = lipgloss.Color("#6B7280")
	} else {
		mainFg = lipgloss.Color("#D1D5DB")
		dimFg = lipgloss.Color("#6B7280")
	}

	mainStyle := lipgloss.NewStyle().Foreground(mainFg).Bold(bold)
	dimStyle := lipgloss.NewStyle().Foreground(dimFg).Bold(bold)
	if selected {
		mainStyle = mainStyle.Background(bg)
		dimStyle = dimStyle.Background(bg)
	}

	// Build line from separately-rendered segments to avoid ANSI reset issues
	var parts []string
	parts = append(parts, mainStyle.Render("  "+cursor+" "))

	switch item.Type {
	case "movie":
		parts = append(parts, mainStyle.Render(item.Title+" "))
		parts = append(parts, dimStyle.Render(fmt.Sprintf("(%d)", item.Year)))
	case "episode":
		parts = append(parts, mainStyle.Render(item.ParentTitle+" "))
		parts = append(parts, dimStyle.Render(fmt.Sprintf("S%02dE%02d ", item.ParentIndex, item.Index)))
		parts = append(parts, mainStyle.Render(item.Title))
	default:
		parts = append(parts, mainStyle.Render(item.Title))
	}

	return strings.Join(parts, "")
}

func (m *BrowserModel) renderDetails(item plex.MediaItem, width, height int) string {
	detailStyle := lipgloss.NewStyle().
		Width(width).
		Height(height).
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#4B5563")).
		Padding(1)

	var details strings.Builder

	// Title with accent color
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#C084FC"))
	details.WriteString(titleStyle.Render(item.Title))
	details.WriteString("\n\n")

	// Styled labels and values
	labelStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#6B7280")).
		Width(10)
	valueStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("#E5E7EB"))

	if item.Type == "movie" && item.Year > 0 {
		details.WriteString(labelStyle.Render("Year"))
		details.WriteString(valueStyle.Render(fmt.Sprintf("%d", item.Year)))
		details.WriteString("\n")
	} else if item.Type == "episode" {
		details.WriteString(labelStyle.Render("Show"))
		details.WriteString(valueStyle.Render(item.ParentTitle))
		details.WriteString("\n")
		details.WriteString(labelStyle.Render("Episode"))
		details.WriteString(valueStyle.Render(fmt.Sprintf("Season %d, Episode %d", item.ParentIndex, item.Index)))
		details.WriteString("\n")
	}

	if item.Rating > 0 {
		details.WriteString(labelStyle.Render("Rating"))
		ratingStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#FBBF24"))
		details.WriteString(ratingStyle.Render(fmt.Sprintf("%.1f", item.Rating)))
		details.WriteString(valueStyle.Render("/10"))
		details.WriteString("\n")
	}

	if item.Duration > 0 {
		minutes := item.Duration / 60000
		details.WriteString(labelStyle.Render("Duration"))
		details.WriteString(valueStyle.Render(fmt.Sprintf("%d min", minutes)))
		details.WriteString("\n")
	}

	if item.Summary != "" {
		details.WriteString("\n")
		summaryStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#9CA3AF"))
		wrapped := wrapText(item.Summary, width-4)
		details.WriteString(summaryStyle.Render(wrapped))
	}

	// Render poster if available
	if m.showPoster && item.Thumb != "" {
		// Check if we have the rendered poster in cache
		if posterPath, ok := m.posterCache[item.Thumb]; ok {
			if rendered, ok := m.renderedPoster[posterPath]; ok {
				details.WriteString("\n\n")
				details.WriteString(rendered)
			}
		} else if !m.posterLoading[item.Thumb] {
			// Show styled loading indicator
			loadingStyle := lipgloss.NewStyle().
				Foreground(lipgloss.Color("#6B7280")).
				Italic(true)
			details.WriteString("\n\n")
			details.WriteString(loadingStyle.Render("Loading poster..."))
		}
	}

	return detailStyle.Render(details.String())
}

func (m *BrowserModel) renderDetailsCompact(item plex.MediaItem) string {
	// Box container for compact details
	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#4B5563")).
		Padding(0, 1)

	var content strings.Builder

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#C084FC"))
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#6B7280"))
	sepStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#374151"))

	content.WriteString(titleStyle.Render(item.Title))

	if item.Type == "movie" && item.Year > 0 {
		content.WriteString(dimStyle.Render(fmt.Sprintf(" (%d)", item.Year)))
	} else if item.Type == "episode" {
		content.WriteString(dimStyle.Render(fmt.Sprintf(" · %s S%02dE%02d", item.ParentTitle, item.ParentIndex, item.Index)))
	}

	if item.Rating > 0 {
		content.WriteString(sepStyle.Render(" · "))
		ratingStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#FBBF24"))
		content.WriteString(ratingStyle.Render(fmt.Sprintf("%.1f", item.Rating)))
		content.WriteString(dimStyle.Render("/10"))
	}

	if item.Duration > 0 {
		minutes := item.Duration / 60000
		content.WriteString(sepStyle.Render(" · "))
		content.WriteString(dimStyle.Render(fmt.Sprintf("%dm", minutes)))
	}

	return "  " + boxStyle.Render(content.String())
}

// maybeDownloadPoster checks if current item needs poster download and triggers it
func (m *BrowserModel) maybeDownloadPoster() tea.Cmd {
	if !m.showPoster || len(m.filteredMedia) == 0 {
		return nil
	}

	item := m.filteredMedia[m.cursor]
	if item.Thumb == "" {
		return nil
	}

	// Already cached or loading?
	if _, ok := m.posterCache[item.Thumb]; ok {
		return nil
	}
	if m.posterLoading[item.Thumb] {
		return nil
	}

	// Mark as loading and download
	m.posterLoading[item.Thumb] = true
	return m.downloadPosterAsync(item.Thumb)
}

// downloadPosterAsync downloads a poster in the background
func (m *BrowserModel) downloadPosterAsync(thumbPath string) tea.Cmd {
	return func() tea.Msg {
		path := DownloadPoster(m.plexURL, thumbPath, m.plexToken)
		return posterDownloadedMsg{
			thumbPath:  thumbPath,
			posterPath: path,
		}
	}
}

// renderPosterAsync renders a poster image in the background
func (m *BrowserModel) renderPosterAsync(posterPath string) tea.Cmd {
	return func() tea.Msg {
		// Check if already rendered
		if _, ok := m.renderedPoster[posterPath]; ok {
			return posterRenderedMsg{}
		}

		// Check if chafa is available
		if _, err := exec.LookPath("chafa"); err != nil {
			return posterRenderedMsg{}
		}

		// Use fixed size for consistency
		width := 40
		height := int(float64(width) * 1.5) // 2:3 aspect ratio

		// Run chafa with better quality settings
		cmd := exec.Command("chafa",
			"--size", fmt.Sprintf("%dx%d", width, height),
			"--format", "symbols",
			"--symbols", "all",
			"--dither", "ordered",
			posterPath)
		output, err := cmd.Output()
		if err != nil {
			return posterRenderedMsg{}
		}

		return posterRenderedMsg{
			posterPath:     posterPath,
			renderedOutput: string(output),
		}
	}
}

func (m *BrowserModel) filterMedia() {
	query := m.searchInput.Value()
	if query == "" {
		m.filteredMedia = m.media
		m.cursor = 0
		return
	}

	// Build searchable strings for each media item
	var searchStrings []string
	for _, item := range m.media {
		var searchStr string
		switch item.Type {
		case "movie":
			searchStr = fmt.Sprintf("%s %d", item.Title, item.Year)
		case "episode":
			searchStr = fmt.Sprintf("%s %s S%02dE%02d", item.ParentTitle, item.Title, item.ParentIndex, item.Index)
		default:
			searchStr = item.Title
		}
		searchStrings = append(searchStrings, searchStr)
	}

	// Fuzzy search
	matches := fuzzy.Find(query, searchStrings)

	// Build filtered list
	var filtered []plex.MediaItem
	for _, match := range matches {
		filtered = append(filtered, m.media[match.Index])
	}

	m.filteredMedia = filtered
	m.cursor = 0
}

// GetSelected returns the selected media item (if any)
func (m *BrowserModel) GetSelected() *plex.MediaItem {
	return m.selected
}

// Helper functions
// Note: min() and max() are Go 1.21+ builtins

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
