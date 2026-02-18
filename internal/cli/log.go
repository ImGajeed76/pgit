package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/imgajeed76/pgit/v3/internal/db"
	"github.com/imgajeed76/pgit/v3/internal/repo"
	"github.com/imgajeed76/pgit/v3/internal/ui/styles"
	"github.com/imgajeed76/pgit/v3/internal/util"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func newLogCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "log [commit]",
		Short: "Show commit logs",
		Long: `Shows the commit logs starting from HEAD, or from a specific commit.

Examples:
  pgit log                 # Log from HEAD
  pgit log abc123          # Log from specific commit
  pgit log HEAD~10         # Log from 10 commits back

Interactive mode (default):
  Use j/k or arrows to navigate, Enter to view details, q to quit.

Use --oneline for compact non-interactive output.
Use --graph for ASCII commit graph visualization.`,
		RunE: runLog,
	}

	cmd.Flags().IntP("max-count", "n", 0, "Limit number of commits to show")
	cmd.Flags().Bool("oneline", false, "Show each commit on one line (non-interactive)")
	cmd.Flags().Bool("graph", false, "Show ASCII commit graph")
	cmd.Flags().Bool("no-pager", false, "Disable interactive pager")
	cmd.Flags().Bool("json", false, "Output in JSON format")

	return cmd
}

func runLog(cmd *cobra.Command, args []string) error {
	maxCount, _ := cmd.Flags().GetInt("max-count")
	oneline, _ := cmd.Flags().GetBool("oneline")
	graph, _ := cmd.Flags().GetBool("graph")
	noPager, _ := cmd.Flags().GetBool("no-pager")
	jsonOutput, _ := cmd.Flags().GetBool("json")

	if maxCount == 0 {
		maxCount = 1000 // Default limit
	}

	r, err := repo.Open()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := r.Connect(ctx); err != nil {
		return err
	}
	defer r.Close()

	var commits []*db.Commit
	isFromHead := true
	if len(args) > 0 {
		// Start from specified commit
		commitID, err := resolveCommitRef(ctx, r, args[0])
		if err != nil {
			return err
		}
		commits, err = r.DB.GetCommitLogFrom(ctx, commitID, maxCount)
		if err != nil {
			return err
		}
		// Only show HEAD label if the resolved commit actually is HEAD
		headID, _ := r.DB.GetHead(ctx)
		isFromHead = len(commits) > 0 && commits[0].ID == headID
	} else {
		commits, err = r.DB.GetCommitLog(ctx, maxCount)
		if err != nil {
			return err
		}
	}

	if len(commits) == 0 {
		if jsonOutput {
			fmt.Println("[]")
			return nil
		}
		fmt.Println("No commits yet")
		return nil
	}

	// JSON mode
	if jsonOutput {
		return printJSONLog(commits)
	}

	// Graph mode - ASCII visualization
	if graph {
		return printGraphLog(commits, oneline, isFromHead)
	}

	// Oneline mode - simple output
	if oneline {
		for _, commit := range commits {
			fmt.Printf("%s %s\n",
				styles.Hash(commit.ID, true),
				firstLine(commit.Message))
		}
		return nil
	}

	// Check if we should use interactive mode
	isTTY := term.IsTerminal(int(os.Stdout.Fd()))
	accessible := styles.IsAccessible()
	if !isTTY || noPager || accessible {
		// Non-interactive full output
		for i, commit := range commits {
			if i > 0 {
				fmt.Println()
			}
			printCommitFull(commit, isFromHead && i == 0)
		}
		return nil
	}

	// Interactive TUI mode
	return runLogTUI(commits, isFromHead)
}

// printGraphLog prints commits with ASCII graph
func printGraphLog(commits []*db.Commit, oneline, isFromHead bool) error {
	for i, commit := range commits {
		// Simple linear graph for now (pgit is typically single-branch)
		var graphPrefix string
		if i == 0 {
			graphPrefix = styles.Green("*") + " "
		} else if i == len(commits)-1 {
			graphPrefix = styles.Green("*") + " "
		} else {
			graphPrefix = styles.Green("*") + " "
		}

		// Add connecting line
		connector := styles.Mute("│ ")

		if oneline {
			fmt.Printf("%s%s %s\n",
				graphPrefix,
				styles.Hash(commit.ID, true),
				firstLine(commit.Message))
		} else {
			// Full format with graph
			refs := ""
			if isFromHead && i == 0 {
				refs = " " + lipgloss.NewStyle().Foreground(styles.Accent).Render("(HEAD → main)")
			}

			fmt.Printf("%s%s%s  %s\n",
				graphPrefix,
				styles.Hash(commit.ID, false),
				refs,
				styles.MutedMsg(util.RelativeTimeShort(commit.AuthoredAt)))
			fmt.Printf("%s%s - %s\n",
				connector,
				styles.Author(commit.AuthorName),
				firstLine(commit.Message))

			// Add spacing between commits
			if i < len(commits)-1 {
				fmt.Printf("%s\n", connector)
			}
		}
	}
	return nil
}

// JSONLogEntry represents a commit in JSON format
type JSONLogEntry struct {
	ID             string  `json:"id"`
	ShortID        string  `json:"short_id"`
	ParentID       *string `json:"parent_id,omitempty"`
	Message        string  `json:"message"`
	AuthorName     string  `json:"author_name"`
	AuthorEmail    string  `json:"author_email"`
	Timestamp      string  `json:"timestamp"`
	CommitterName  string  `json:"committer_name"`
	CommitterEmail string  `json:"committer_email"`
	CommittedAt    string  `json:"committed_at"`
}

func printJSONLog(commits []*db.Commit) error {
	entries := make([]JSONLogEntry, len(commits))
	for i, c := range commits {
		entries[i] = JSONLogEntry{
			ID:             c.ID,
			ShortID:        util.ShortID(c.ID),
			ParentID:       c.ParentID,
			Message:        c.Message,
			AuthorName:     c.AuthorName,
			AuthorEmail:    c.AuthorEmail,
			Timestamp:      c.AuthoredAt.Format(time.RFC3339),
			CommitterName:  c.CommitterName,
			CommitterEmail: c.CommitterEmail,
			CommittedAt:    c.CommittedAt.Format(time.RFC3339),
		}
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(entries)
}

// ═══════════════════════════════════════════════════════════════════════════
// Interactive Log TUI
// ═══════════════════════════════════════════════════════════════════════════

type logMode int

const (
	logModeNormal logMode = iota
	logModeSearch
	logModeHelp
)

type logModel struct {
	commits      []*db.Commit
	cursor       int
	viewport     viewport.Model
	ready        bool
	width        int
	height       int
	mode         logMode
	searchInput  textinput.Model
	searchQuery  string
	searchHits   []int // indices of matching commits
	searchCursor int   // current position in searchHits
	isFromHead   bool  // true if the first commit is HEAD
}

type keyMap struct {
	Up        key.Binding
	Down      key.Binding
	PageUp    key.Binding
	PageDown  key.Binding
	Home      key.Binding
	End       key.Binding
	Quit      key.Binding
	Search    key.Binding
	Help      key.Binding
	NextMatch key.Binding
	PrevMatch key.Binding
}

var keys = keyMap{
	Up:        key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
	Down:      key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
	PageUp:    key.NewBinding(key.WithKeys("pgup", "ctrl+u"), key.WithHelp("pgup", "page up")),
	PageDown:  key.NewBinding(key.WithKeys("pgdown", "ctrl+d"), key.WithHelp("pgdn", "page down")),
	Home:      key.NewBinding(key.WithKeys("home", "g"), key.WithHelp("g", "top")),
	End:       key.NewBinding(key.WithKeys("end", "G"), key.WithHelp("G", "bottom")),
	Quit:      key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
	Search:    key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "search")),
	Help:      key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
	NextMatch: key.NewBinding(key.WithKeys("n"), key.WithHelp("n", "next match")),
	PrevMatch: key.NewBinding(key.WithKeys("N"), key.WithHelp("N", "prev match")),
}

func runLogTUI(commits []*db.Commit, isFromHead bool) error {
	ti := textinput.New()
	ti.Placeholder = "search commits..."
	ti.CharLimit = 100
	ti.Width = 40

	m := logModel{
		commits:     commits,
		cursor:      0,
		mode:        logModeNormal,
		searchInput: ti,
		isFromHead:  isFromHead,
	}

	p := tea.NewProgram(m, tea.WithAltScreen())
	_, err := p.Run()
	return err
}

func (m logModel) Init() tea.Cmd {
	return nil
}

func (m logModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		headerHeight := 2 // Title + blank
		footerHeight := 2 // Help + blank

		if !m.ready {
			m.viewport = viewport.New(msg.Width, msg.Height-headerHeight-footerHeight)
			m.viewport.YPosition = headerHeight
			m.ready = true
		} else {
			m.viewport.Width = msg.Width
			m.viewport.Height = msg.Height - headerHeight - footerHeight
		}
		m.viewport.SetContent(m.renderCommits())

	case tea.KeyMsg:
		// Handle mode-specific input
		switch m.mode {
		case logModeSearch:
			return m.updateSearch(msg)
		case logModeHelp:
			// Any key exits help mode
			m.mode = logModeNormal
			return m, nil
		}

		// Normal mode key handling
		switch {
		case key.Matches(msg, keys.Quit):
			return m, tea.Quit
		case key.Matches(msg, keys.Search):
			m.mode = logModeSearch
			m.searchInput.Focus()
			return m, textinput.Blink
		case key.Matches(msg, keys.Help):
			m.mode = logModeHelp
			return m, nil
		case key.Matches(msg, keys.NextMatch):
			m.jumpToNextMatch()
		case key.Matches(msg, keys.PrevMatch):
			m.jumpToPrevMatch()
		case key.Matches(msg, keys.Up):
			m.viewport.ScrollUp(1)
		case key.Matches(msg, keys.Down):
			m.viewport.ScrollDown(1)
		case key.Matches(msg, keys.PageUp):
			m.viewport.HalfPageUp()
		case key.Matches(msg, keys.PageDown):
			m.viewport.HalfPageDown()
		case key.Matches(msg, keys.Home):
			m.viewport.GotoTop()
		case key.Matches(msg, keys.End):
			m.viewport.GotoBottom()
		}
	}

	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

func (m logModel) updateSearch(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEnter:
		// Execute search
		m.searchQuery = m.searchInput.Value()
		m.performSearch()
		m.mode = logModeNormal
		m.searchInput.Blur()
		m.viewport.SetContent(m.renderCommits())
		if len(m.searchHits) > 0 {
			m.jumpToNextMatch()
		}
		return m, nil
	case tea.KeyEsc:
		// Cancel search
		m.mode = logModeNormal
		m.searchInput.Blur()
		m.searchInput.SetValue("")
		return m, nil
	}

	var cmd tea.Cmd
	m.searchInput, cmd = m.searchInput.Update(msg)
	return m, cmd
}

func (m *logModel) performSearch() {
	query := strings.ToLower(m.searchQuery)
	m.searchHits = nil
	m.searchCursor = -1

	if query == "" {
		return
	}

	for i, commit := range m.commits {
		// Search in hash, message, and author
		if strings.Contains(strings.ToLower(commit.ID), query) ||
			strings.Contains(strings.ToLower(commit.Message), query) ||
			strings.Contains(strings.ToLower(commit.AuthorName), query) ||
			strings.Contains(strings.ToLower(commit.AuthorEmail), query) {
			m.searchHits = append(m.searchHits, i)
		}
	}
}

func (m *logModel) jumpToNextMatch() {
	if len(m.searchHits) == 0 {
		return
	}

	m.searchCursor++
	if m.searchCursor >= len(m.searchHits) {
		m.searchCursor = 0 // Wrap around
	}

	// Jump to the commit (each commit takes ~3 lines)
	commitIdx := m.searchHits[m.searchCursor]
	lineNum := commitIdx * 3
	m.viewport.SetYOffset(lineNum)
}

func (m *logModel) jumpToPrevMatch() {
	if len(m.searchHits) == 0 {
		return
	}

	m.searchCursor--
	if m.searchCursor < 0 {
		m.searchCursor = len(m.searchHits) - 1 // Wrap around
	}

	commitIdx := m.searchHits[m.searchCursor]
	lineNum := commitIdx * 3
	m.viewport.SetYOffset(lineNum)
}

func (m logModel) View() string {
	if !m.ready {
		return "Loading..."
	}

	// Help overlay mode
	if m.mode == logModeHelp {
		return m.renderHelp()
	}

	// Header
	title := styles.SectionHeader(fmt.Sprintf("pgit log (%d commits)", len(m.commits)))

	// Search indicator
	if m.searchQuery != "" && len(m.searchHits) > 0 {
		title += styles.MutedMsg(fmt.Sprintf("  [%d/%d matches for '%s']",
			m.searchCursor+1, len(m.searchHits), m.searchQuery))
	} else if m.searchQuery != "" {
		title += styles.MutedMsg(fmt.Sprintf("  [no matches for '%s']", m.searchQuery))
	}

	// Footer
	var help string
	if m.mode == logModeSearch {
		help = fmt.Sprintf("/%s", m.searchInput.View())
	} else {
		help = styles.MutedMsg("↑/↓ scroll  / search  ? help  q quit")
		if m.searchQuery != "" && len(m.searchHits) > 0 {
			help = styles.MutedMsg("↑/↓ scroll  n/N next/prev match  / search  ? help  q quit")
		}
	}

	// Combine
	return fmt.Sprintf("%s\n\n%s\n\n%s", title, m.viewport.View(), help)
}

func (m logModel) renderHelp() string {
	var sb strings.Builder

	boxStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(styles.Muted).
		Padding(1, 2)

	title := styles.SectionHeader("Keyboard Shortcuts")
	sb.WriteString(title)
	sb.WriteString("\n\n")

	helpItems := []struct {
		key  string
		desc string
	}{
		{"↑/k", "Move up"},
		{"↓/j", "Move down"},
		{"g/Home", "Go to top"},
		{"G/End", "Go to bottom"},
		{"Ctrl+U", "Page up"},
		{"Ctrl+D", "Page down"},
		{"", ""},
		{"/", "Search commits"},
		{"n", "Next search match"},
		{"N", "Previous search match"},
		{"Esc", "Clear search / cancel"},
		{"", ""},
		{"?", "Show this help"},
		{"q", "Quit"},
	}

	for _, item := range helpItems {
		if item.key == "" {
			sb.WriteString("\n")
			continue
		}
		keyStyle := lipgloss.NewStyle().Foreground(styles.Accent).Width(10)
		sb.WriteString(fmt.Sprintf("  %s %s\n", keyStyle.Render(item.key), item.desc))
	}

	sb.WriteString("\n")
	sb.WriteString(styles.MutedMsg("Press any key to close"))

	// Center the help box
	content := boxStyle.Render(sb.String())
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, content)
}

func (m logModel) renderCommits() string {
	var sb strings.Builder

	// Build a set of matching indices for quick lookup
	matchSet := make(map[int]bool)
	for _, idx := range m.searchHits {
		matchSet[idx] = true
	}

	for i, commit := range m.commits {
		isMatch := matchSet[i]
		isCurrentMatch := len(m.searchHits) > 0 && m.searchCursor >= 0 &&
			m.searchCursor < len(m.searchHits) && m.searchHits[m.searchCursor] == i

		// Commit line with symbol
		symbol := styles.SymbolCommit
		if isCurrentMatch {
			symbol = "▶" // Highlight current match
		} else if isMatch {
			symbol = "●"
		}

		hash := styles.Hash(commit.ID, false)
		refs := ""
		if m.isFromHead && i == 0 {
			refs = " " + lipgloss.NewStyle().Foreground(styles.Accent).Render("(HEAD → main)")
		}

		// Highlight matching commits
		symbolStyle := lipgloss.NewStyle().Foreground(styles.Success)
		if isCurrentMatch {
			symbolStyle = lipgloss.NewStyle().Foreground(styles.Warning).Bold(true)
		} else if isMatch {
			symbolStyle = lipgloss.NewStyle().Foreground(styles.Warning)
		}

		// First line: symbol, hash, relative time
		sb.WriteString(fmt.Sprintf("%s %s%s  %s\n",
			symbolStyle.Render(symbol),
			hash,
			refs,
			styles.MutedMsg(util.RelativeTimeShort(commit.AuthoredAt))))

		// Second line: author and message
		author := styles.Author(commit.AuthorName)
		message := firstLine(commit.Message)

		// Highlight search terms in message if searching
		if m.searchQuery != "" && isMatch {
			message = m.highlightSearchTerm(message)
		}

		sb.WriteString(fmt.Sprintf("│ %s - %s\n", author, message))

		// Spacing between commits
		if i < len(m.commits)-1 {
			sb.WriteString("│\n")
		}
	}

	return sb.String()
}

func (m logModel) highlightSearchTerm(text string) string {
	if m.searchQuery == "" {
		return text
	}

	lowerText := strings.ToLower(text)
	lowerQuery := strings.ToLower(m.searchQuery)

	idx := strings.Index(lowerText, lowerQuery)
	if idx == -1 {
		return text
	}

	// Highlight the matched portion
	highlightStyle := lipgloss.NewStyle().Background(styles.Warning).Foreground(lipgloss.Color("#000000"))
	before := text[:idx]
	match := text[idx : idx+len(m.searchQuery)]
	after := text[idx+len(m.searchQuery):]

	return before + highlightStyle.Render(match) + after
}

// ═══════════════════════════════════════════════════════════════════════════
// Helpers
// ═══════════════════════════════════════════════════════════════════════════

func printCommitFull(commit *db.Commit, isHead bool) {
	// Commit line
	hash := styles.Hash(commit.ID, false)
	if isHead {
		refs := lipgloss.NewStyle().Foreground(styles.Accent).Render("(HEAD → main)")
		fmt.Printf("commit %s %s\n", hash, refs)
	} else {
		fmt.Printf("commit %s\n", hash)
	}

	// Author
	fmt.Printf("Author: %s <%s>\n", commit.AuthorName, commit.AuthorEmail)

	// Date
	fmt.Printf("Date:   %s\n", commit.AuthoredAt.Format("Mon Jan 2 15:04:05 2006 -0700"))

	// Committer (only shown when different from author, like git log --format=full)
	committerDiffers := commit.CommitterName != commit.AuthorName || commit.CommitterEmail != commit.AuthorEmail
	if committerDiffers {
		fmt.Printf("Committer: %s <%s>\n", commit.CommitterName, commit.CommitterEmail)
		fmt.Printf("CommitDate: %s\n", commit.CommittedAt.Format("Mon Jan 2 15:04:05 2006 -0700"))
	}

	// Message (indented)
	fmt.Println()
	for _, line := range splitLines(commit.Message) {
		fmt.Printf("    %s\n", line)
	}
}

func splitLines(s string) []string {
	if s == "" {
		return []string{""}
	}
	return strings.Split(s, "\n")
}
