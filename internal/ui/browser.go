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
	quitting       bool
	selected       *plex.MediaItem
}

type keyMap struct {
	Up       key.Binding
	Down     key.Binding
	Search   key.Binding
	Select   key.Binding
	TogglePoster key.Binding
	Quit     key.Binding
	ClearSearch key.Binding
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
		media:         media,
		filteredMedia: media,
		searchInput:   ti,
		plexURL:       plexURL,
		plexToken:     plexToken,
		posterCache:   make(map[string]string),
		showPoster:    true,
	}
}

func (m *BrowserModel) Init() tea.Cmd {
	return nil
}

func (m *BrowserModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
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
		case key.Matches(msg, keys.Down):
			if m.cursor < len(m.filteredMedia)-1 {
				m.cursor++
			}
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

	// Header
	headerStyle := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("205")).
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color("240")).
		BorderBottom(true).
		Width(m.width - 2)

	header := fmt.Sprintf("  Media Browser - %d items", len(m.filteredMedia))
	b.WriteString(headerStyle.Render(header))
	b.WriteString("\n\n")

	// Search bar
	if m.searching {
		searchStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("205")).
			Bold(true)
		b.WriteString(searchStyle.Render("  Search: "))
		b.WriteString(m.searchInput.View())
		b.WriteString("\n\n")
	} else if m.searchInput.Value() != "" {
		searchStyle := lipgloss.NewStyle().
			Foreground(lipgloss.Color("240"))
		b.WriteString(searchStyle.Render(fmt.Sprintf("  Filter: %s (press / to search again, esc to clear)", m.searchInput.Value())))
		b.WriteString("\n\n")
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
			BorderForeground(lipgloss.Color("240"))

		var listItems []string
		for i := listStart; i < listEnd; i++ {
			item := m.filteredMedia[i]
			cursor := " "
			if i == m.cursor {
				cursor = ">"
			}

			line := m.formatListItem(item, cursor, i == m.cursor)
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
			b.WriteString(m.formatListItem(item, cursor, i == m.cursor))
			b.WriteString("\n")
		}

		// Show details below
		if len(m.filteredMedia) > 0 {
			b.WriteString("\n")
			b.WriteString(m.renderDetailsCompact(m.filteredMedia[m.cursor]))
		}
	}

	// Footer with help
	b.WriteString("\n\n")
	helpStyle := lipgloss.NewStyle().
		Foreground(lipgloss.Color("240"))

	help := "  ↑/↓: navigate • /: search • p: toggle poster • enter: select • q: quit"
	b.WriteString(helpStyle.Render(help))

	return b.String()
}

func (m *BrowserModel) formatListItem(item plex.MediaItem, cursor string, selected bool) string {
	style := lipgloss.NewStyle()
	if selected {
		style = style.Foreground(lipgloss.Color("205")).Bold(true)
	}

	var line string
	if item.Type == "movie" {
		line = fmt.Sprintf("%s %s (%d)", cursor, item.Title, item.Year)
	} else if item.Type == "episode" {
		line = fmt.Sprintf("%s %s - S%02dE%02d: %s", cursor, item.ParentTitle, item.ParentIndex, item.Index, item.Title)
	} else {
		line = fmt.Sprintf("%s %s", cursor, item.Title)
	}

	return style.Render("  " + line)
}

func (m *BrowserModel) renderDetails(item plex.MediaItem, width, height int) string {
	detailStyle := lipgloss.NewStyle().
		Width(width).
		Height(height).
		BorderStyle(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("240")).
		Padding(1)

	var details strings.Builder

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205"))
	details.WriteString(titleStyle.Render(item.Title))
	details.WriteString("\n\n")

	if item.Type == "movie" && item.Year > 0 {
		details.WriteString(fmt.Sprintf("Year: %d\n", item.Year))
	} else if item.Type == "episode" {
		details.WriteString(fmt.Sprintf("Show: %s\n", item.ParentTitle))
		details.WriteString(fmt.Sprintf("Season %d, Episode %d\n", item.ParentIndex, item.Index))
	}

	if item.Rating > 0 {
		details.WriteString(fmt.Sprintf("Rating: %.1f/10\n", item.Rating))
	}

	if item.Duration > 0 {
		minutes := item.Duration / 60000
		details.WriteString(fmt.Sprintf("Duration: %d min\n", minutes))
	}

	if item.Summary != "" {
		details.WriteString("\n")
		wrapped := wrapText(item.Summary, width-4)
		details.WriteString(wrapped)
	}

	// Render poster if available
	if m.showPoster && item.Thumb != "" {
		posterPath := m.getPosterPath(item.Thumb)
		if posterPath != "" {
			details.WriteString("\n\n")
			poster := m.renderPoster(posterPath, width-4)
			details.WriteString(poster)
		}
	}

	return detailStyle.Render(details.String())
}

func (m *BrowserModel) renderDetailsCompact(item plex.MediaItem) string {
	var details strings.Builder

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205"))
	details.WriteString("  ")
	details.WriteString(titleStyle.Render(item.Title))
	details.WriteString(" - ")

	if item.Type == "movie" && item.Year > 0 {
		details.WriteString(fmt.Sprintf("%d", item.Year))
	} else if item.Type == "episode" {
		details.WriteString(fmt.Sprintf("%s S%02dE%02d", item.ParentTitle, item.ParentIndex, item.Index))
	}

	if item.Rating > 0 {
		details.WriteString(fmt.Sprintf(" • %.1f★", item.Rating))
	}

	if item.Duration > 0 {
		minutes := item.Duration / 60000
		details.WriteString(fmt.Sprintf(" • %dm", minutes))
	}

	return details.String()
}

func (m *BrowserModel) getPosterPath(thumbPath string) string {
	// Check cache
	if path, ok := m.posterCache[thumbPath]; ok {
		return path
	}

	// Download poster (reuse shared function)
	path := DownloadPoster(m.plexURL, thumbPath, m.plexToken)
	if path != "" {
		m.posterCache[thumbPath] = path
	}
	return path
}

func (m *BrowserModel) renderPoster(posterPath string, maxWidth int) string {
	// Check if chafa is available
	if _, err := exec.LookPath("chafa"); err != nil {
		return ""
	}

	// Use larger size for better quality
	// Movie posters are typically 2:3 aspect ratio
	width := min(maxWidth-2, 50)
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
		return ""
	}

	return string(output)
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
		if item.Type == "movie" {
			searchStr = fmt.Sprintf("%s %d", item.Title, item.Year)
		} else if item.Type == "episode" {
			searchStr = fmt.Sprintf("%s %s S%02dE%02d", item.ParentTitle, item.Title, item.ParentIndex, item.Index)
		} else {
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
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
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
