package table

import (
	"fmt"
	"strings"
	"time"

	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/imgajeed76/pgit/v4/internal/ui/styles"
)

// ═══════════════════════════════════════════════════════════════════════════
// Constants
// ═══════════════════════════════════════════════════════════════════════════

const (
	defaultColWidth = 20
	minColWidth     = 3
	hiddenColWidth  = 3
)

// Column display state
type colState int

const (
	colStateDefault  colState = iota // truncated to defaultColWidth
	colStateExpanded                 // full width
	colStateHidden                   // minimal width (just "...")
)

// Table mode
type tableMode int

const (
	tableModeNormal tableMode = iota
	tableModeSearch
)

// Exit mode — what to do after quitting TUI
type exitMode int

const (
	exitNormal exitMode = iota
	exitJSON
	exitRaw
	exitPlain
)

// ═══════════════════════════════════════════════════════════════════════════
// Model
// ═══════════════════════════════════════════════════════════════════════════

type tableModel struct {
	title         string
	columns       []string
	rows          [][]string
	filteredRows  []int      // indices of rows matching search (nil = show all)
	fullColWidths []int      // actual max width of each column's content
	colStates     []colState // display state for each column
	cursor        int        // selected row (in filtered view)
	colCursor     int        // selected column
	scrollX       int        // horizontal scroll offset in characters
	scrollY       int        // vertical scroll offset in rows
	width         int        // terminal width
	height        int        // terminal height
	ready         bool
	mode          tableMode
	searchInput   textinput.Model
	searchQuery   string
	exitMode      exitMode // how to exit (for re-printing data)

	// Animation state for smooth scrolling
	animating   bool // whether animation is in progress
	animTargetX int  // target scrollX for animation
	animTargetY int  // target scrollY for animation

	// Status message (flash notification, e.g. after yank)
	statusMsg   string    // message to show in footer
	statusUntil time.Time // when to clear the message
}

// ═══════════════════════════════════════════════════════════════════════════
// Key Bindings
// ═══════════════════════════════════════════════════════════════════════════

type tableKeyMap struct {
	Up          key.Binding
	Down        key.Binding
	Left        key.Binding
	Right       key.Binding
	ShiftUp     key.Binding
	ShiftDown   key.Binding
	ShiftLeft   key.Binding
	ShiftRight  key.Binding
	PageUp      key.Binding
	PageDown    key.Binding
	Home        key.Binding
	End         key.Binding
	Expand      key.Binding
	Hide        key.Binding
	Search      key.Binding
	Quit        key.Binding
	YankCell    key.Binding
	YankRow     key.Binding
	ExportJSON  key.Binding
	ExportRaw   key.Binding
	ExportPlain key.Binding
}

var tableKeys = tableKeyMap{
	Up:          key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
	Down:        key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
	Left:        key.NewBinding(key.WithKeys("left"), key.WithHelp("←", "prev column")),
	Right:       key.NewBinding(key.WithKeys("right"), key.WithHelp("→", "next column")),
	ShiftUp:     key.NewBinding(key.WithKeys("shift+up"), key.WithHelp("⇧↑", "half page up")),
	ShiftDown:   key.NewBinding(key.WithKeys("shift+down"), key.WithHelp("⇧↓", "half page down")),
	ShiftLeft:   key.NewBinding(key.WithKeys("shift+left"), key.WithHelp("⇧←", "scroll half left")),
	ShiftRight:  key.NewBinding(key.WithKeys("shift+right"), key.WithHelp("⇧→", "scroll half right")),
	PageUp:      key.NewBinding(key.WithKeys("pgup", "ctrl+u"), key.WithHelp("pgup", "page up")),
	PageDown:    key.NewBinding(key.WithKeys("pgdown", "ctrl+d"), key.WithHelp("pgdn", "page down")),
	Home:        key.NewBinding(key.WithKeys("home", "g"), key.WithHelp("g", "first row")),
	End:         key.NewBinding(key.WithKeys("end", "G"), key.WithHelp("G", "last row")),
	Expand:      key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "expand/default")),
	Hide:        key.NewBinding(key.WithKeys("H"), key.WithHelp("H", "hide/default")),
	Search:      key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "search")),
	Quit:        key.NewBinding(key.WithKeys("q", "ctrl+c", "esc"), key.WithHelp("q", "quit")),
	YankCell:    key.NewBinding(key.WithKeys("y"), key.WithHelp("y", "copy cell")),
	YankRow:     key.NewBinding(key.WithKeys("Y"), key.WithHelp("Y", "copy row")),
	ExportJSON:  key.NewBinding(key.WithKeys("J"), key.WithHelp("J", "print as JSON")),
	ExportRaw:   key.NewBinding(key.WithKeys("R"), key.WithHelp("R", "print raw")),
	ExportPlain: key.NewBinding(key.WithKeys("P"), key.WithHelp("P", "print table")),
}

// ═══════════════════════════════════════════════════════════════════════════
// Entry Point
// ═══════════════════════════════════════════════════════════════════════════

// RunTableTUI launches the interactive table viewer. It blocks until the
// user quits. If the user requests an export (J/R/P), the data is printed
// to stdout after the TUI exits.
func RunTableTUI(title string, columns []string, rows [][]string) error {
	// Calculate full column widths based on content
	fullColWidths := make([]int, len(columns))
	for i, name := range columns {
		fullColWidths[i] = len(name)
	}
	for _, row := range rows {
		for i, val := range row {
			if i < len(fullColWidths) && len(val) > fullColWidths[i] {
				fullColWidths[i] = len(val)
			}
		}
	}

	// Initialize all columns to default state
	colStates := make([]colState, len(columns))
	for i := range colStates {
		colStates[i] = colStateDefault
	}

	// Initialize search input
	ti := textinput.New()
	ti.Placeholder = "search..."
	ti.CharLimit = 100
	ti.Width = 30

	m := tableModel{
		title:         title,
		columns:       columns,
		rows:          rows,
		filteredRows:  nil, // nil means show all
		fullColWidths: fullColWidths,
		colStates:     colStates,
		cursor:        0,
		colCursor:     0,
		scrollX:       0,
		scrollY:       0,
		mode:          tableModeNormal,
		searchInput:   ti,
		searchQuery:   "",
		exitMode:      exitNormal,
	}

	p := tea.NewProgram(m, tea.WithAltScreen())
	finalModel, err := p.Run()
	if err != nil {
		return err
	}

	// Check if user requested output after exit
	if fm, ok := finalModel.(tableModel); ok {
		switch fm.exitMode {
		case exitJSON:
			return PrintJSONResults(columns, rows)
		case exitRaw:
			for _, row := range rows {
				fmt.Println(strings.Join(row, "\t"))
			}
		case exitPlain:
			PrintPlainTable(columns, rows)
		}
	}

	return nil
}

// ═══════════════════════════════════════════════════════════════════════════
// Bubble Tea Interface
// ═══════════════════════════════════════════════════════════════════════════

func (m tableModel) Init() tea.Cmd {
	return nil
}

func (m tableModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.ready = true

	case animTickMsg:
		// Handle animation frame
		cmd := m.updateAnimation()
		return m, cmd

	case statusClearMsg:
		// Clear the flash message if it has expired
		if !m.statusUntil.IsZero() && time.Now().After(m.statusUntil) {
			m.statusMsg = ""
			m.statusUntil = time.Time{}
		}
		return m, nil

	case tea.KeyMsg:
		// Cancel any ongoing animation when user presses a key
		m.cancelAnimation()

		// Handle search mode
		if m.mode == tableModeSearch {
			return m.updateSearch(msg)
		}

		// Normal mode
		switch {
		case key.Matches(msg, tableKeys.Quit):
			return m, tea.Quit

		case key.Matches(msg, tableKeys.Search):
			m.mode = tableModeSearch
			m.searchInput.Focus()
			return m, textinput.Blink

		case key.Matches(msg, tableKeys.Up):
			if m.cursor > 0 {
				m.cursor--
				m.ensureRowVisible()
			}

		case key.Matches(msg, tableKeys.Down):
			maxRows := m.displayRowCount()
			if m.cursor < maxRows-1 {
				m.cursor++
				m.ensureRowVisible()
			}

		case key.Matches(msg, tableKeys.Left):
			colStartX := m.getColStartX(m.colCursor)

			if colStartX < m.scrollX {
				m.scrollX -= 3
				if m.scrollX < colStartX {
					m.scrollX = colStartX
				}
				if m.scrollX < 0 {
					m.scrollX = 0
				}
			} else if m.colCursor > 0 {
				m.colCursor--
				m.ensureColVisibleFromRight()
			}

		case key.Matches(msg, tableKeys.Right):
			colEndX := m.getColEndX(m.colCursor)
			viewportEndX := m.scrollX + m.width - 2

			if colEndX > viewportEndX {
				m.scrollX += 3
				maxX := m.getMaxScrollX()
				if m.scrollX > maxX {
					m.scrollX = maxX
				}
			} else if m.colCursor < len(m.columns)-1 {
				m.colCursor++
				m.ensureColVisibleFromLeft()
			}

		case key.Matches(msg, tableKeys.ShiftLeft):
			halfWidth := m.width / 2
			if halfWidth < 1 {
				halfWidth = 1
			}
			targetX := m.scrollX - halfWidth
			cmd := m.startAnimation(targetX, m.scrollY)
			return m, cmd

		case key.Matches(msg, tableKeys.ShiftRight):
			halfWidth := m.width / 2
			if halfWidth < 1 {
				halfWidth = 1
			}
			targetX := m.scrollX + halfWidth
			cmd := m.startAnimation(targetX, m.scrollY)
			return m, cmd

		case key.Matches(msg, tableKeys.ShiftUp):
			halfPage := m.visibleRowCount() / 2
			if halfPage < 1 {
				halfPage = 1
			}
			targetY := m.scrollY - halfPage
			m.cursor -= halfPage
			if m.cursor < 0 {
				m.cursor = 0
			}
			cmd := m.startAnimation(m.scrollX, targetY)
			return m, cmd

		case key.Matches(msg, tableKeys.ShiftDown):
			halfPage := m.visibleRowCount() / 2
			if halfPage < 1 {
				halfPage = 1
			}
			targetY := m.scrollY + halfPage
			maxRows := m.displayRowCount()
			m.cursor += halfPage
			if m.cursor >= maxRows {
				m.cursor = maxRows - 1
			}
			if m.cursor < 0 {
				m.cursor = 0
			}
			cmd := m.startAnimation(m.scrollX, targetY)
			return m, cmd

		case key.Matches(msg, tableKeys.PageUp):
			visibleRows := m.visibleRowCount()
			m.cursor -= visibleRows
			if m.cursor < 0 {
				m.cursor = 0
			}
			m.ensureRowVisible()

		case key.Matches(msg, tableKeys.PageDown):
			visibleRows := m.visibleRowCount()
			maxRows := m.displayRowCount()
			m.cursor += visibleRows
			if m.cursor >= maxRows {
				m.cursor = maxRows - 1
			}
			if m.cursor < 0 {
				m.cursor = 0
			}
			m.ensureRowVisible()

		case key.Matches(msg, tableKeys.Home):
			m.cursor = 0
			m.scrollY = 0
			m.scrollX = 0

		case key.Matches(msg, tableKeys.End):
			maxRows := m.displayRowCount()
			if maxRows > 0 {
				m.cursor = maxRows - 1
				m.ensureRowVisible()
			}

		case key.Matches(msg, tableKeys.Expand):
			if m.colCursor < len(m.colStates) {
				if m.colStates[m.colCursor] == colStateExpanded {
					m.colStates[m.colCursor] = colStateDefault
				} else {
					m.colStates[m.colCursor] = colStateExpanded
				}
				m.ensureColVisible()
			}

		case key.Matches(msg, tableKeys.Hide):
			if m.colCursor < len(m.colStates) {
				if m.colStates[m.colCursor] == colStateHidden {
					m.colStates[m.colCursor] = colStateDefault
				} else {
					m.colStates[m.colCursor] = colStateHidden
				}
				m.ensureColVisible()
			}

		case key.Matches(msg, tableKeys.YankCell):
			cmd := m.yankCell()
			return m, cmd

		case key.Matches(msg, tableKeys.YankRow):
			cmd := m.yankRow()
			return m, cmd

		case key.Matches(msg, tableKeys.ExportJSON):
			m.exitMode = exitJSON
			return m, tea.Quit

		case key.Matches(msg, tableKeys.ExportRaw):
			m.exitMode = exitRaw
			return m, tea.Quit

		case key.Matches(msg, tableKeys.ExportPlain):
			m.exitMode = exitPlain
			return m, tea.Quit
		}
	}

	return m, nil
}

// ═══════════════════════════════════════════════════════════════════════════
// Search
// ═══════════════════════════════════════════════════════════════════════════

func (m tableModel) updateSearch(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		m.mode = tableModeNormal
		m.searchInput.Blur()
		m.searchInput.SetValue("")
		m.searchQuery = ""
		m.filteredRows = nil
		m.cursor = 0
		m.scrollY = 0
		return m, nil
	case tea.KeyEnter:
		m.mode = tableModeNormal
		m.searchInput.Blur()
		return m, nil
	}

	var cmd tea.Cmd
	m.searchInput, cmd = m.searchInput.Update(msg)

	// Live filter as user types
	m.searchQuery = m.searchInput.Value()
	m.performSearch()

	return m, cmd
}

func (m *tableModel) performSearch() {
	query := strings.ToLower(m.searchQuery)

	if query == "" {
		m.filteredRows = nil
		return
	}

	m.filteredRows = nil
	for i, row := range m.rows {
		for _, val := range row {
			if strings.Contains(strings.ToLower(val), query) {
				m.filteredRows = append(m.filteredRows, i)
				break
			}
		}
	}

	if m.cursor >= len(m.filteredRows) {
		m.cursor = 0
		m.scrollY = 0
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// Row / Column Helpers
// ═══════════════════════════════════════════════════════════════════════════

func (m tableModel) displayRowCount() int {
	if m.filteredRows != nil {
		return len(m.filteredRows)
	}
	return len(m.rows)
}

func (m tableModel) getDisplayRow(displayIdx int) []string {
	if m.filteredRows != nil {
		if displayIdx < len(m.filteredRows) {
			return m.rows[m.filteredRows[displayIdx]]
		}
		return nil
	}
	if displayIdx < len(m.rows) {
		return m.rows[displayIdx]
	}
	return nil
}

func (m tableModel) getColDisplayWidth(colIdx int) int {
	if colIdx >= len(m.colStates) {
		return defaultColWidth
	}

	switch m.colStates[colIdx] {
	case colStateExpanded:
		w := m.fullColWidths[colIdx]
		if w < minColWidth {
			w = minColWidth
		}
		return w
	case colStateHidden:
		return hiddenColWidth
	default:
		w := m.fullColWidths[colIdx]
		if w > defaultColWidth {
			w = defaultColWidth
		}
		if w < minColWidth {
			w = minColWidth
		}
		return w
	}
}

func (m tableModel) getColStartX(colIdx int) int {
	x := 0
	for i := 0; i < colIdx && i < len(m.columns); i++ {
		x += m.getColDisplayWidth(i) + 2 // +2 for column separator spacing
	}
	return x
}

func (m tableModel) getColEndX(colIdx int) int {
	return m.getColStartX(colIdx) + m.getColDisplayWidth(colIdx)
}

func (m tableModel) getTotalWidth() int {
	total := 0
	for i := range m.columns {
		total += m.getColDisplayWidth(i) + 2
	}
	return total
}

func (m tableModel) getMaxScrollX() int {
	maxX := m.getTotalWidth() - m.width + 2 // +2 for some padding
	if maxX < 0 {
		return 0
	}
	return maxX
}

func (m tableModel) getMaxScrollY() int {
	maxY := m.displayRowCount() - m.visibleRowCount()
	if maxY < 0 {
		return 0
	}
	return maxY
}

// ═══════════════════════════════════════════════════════════════════════════
// Animation
// ═══════════════════════════════════════════════════════════════════════════

type animTickMsg time.Time

const animationFrameInterval = 16 * time.Millisecond
const animationFraction = 0.25
const animationSnapThreshold = 1

func animTick() tea.Cmd {
	return tea.Tick(animationFrameInterval, func(t time.Time) tea.Msg {
		return animTickMsg(t)
	})
}

func (m *tableModel) startAnimation(targetX, targetY int) tea.Cmd {
	maxX := m.getMaxScrollX()
	if targetX < 0 {
		targetX = 0
	} else if targetX > maxX {
		targetX = maxX
	}

	maxY := m.getMaxScrollY()
	if targetY < 0 {
		targetY = 0
	} else if targetY > maxY {
		targetY = maxY
	}

	m.animTargetX = targetX
	m.animTargetY = targetY

	if targetX == m.scrollX && targetY == m.scrollY {
		m.animating = false
		return nil
	}

	if !m.animating {
		m.animating = true
		return animTick()
	}

	return nil
}

func (m *tableModel) updateAnimation() tea.Cmd {
	if !m.animating {
		return nil
	}

	remainingX := m.animTargetX - m.scrollX
	remainingY := m.animTargetY - m.scrollY

	if abs(remainingX) <= animationSnapThreshold && abs(remainingY) <= animationSnapThreshold {
		m.scrollX = m.animTargetX
		m.scrollY = m.animTargetY
		m.animating = false
		return nil
	}

	if remainingX != 0 {
		deltaX := int(float64(remainingX) * animationFraction)
		if deltaX == 0 {
			if remainingX > 0 {
				deltaX = 1
			} else {
				deltaX = -1
			}
		}
		m.scrollX += deltaX
	}

	if remainingY != 0 {
		deltaY := int(float64(remainingY) * animationFraction)
		if deltaY == 0 {
			if remainingY > 0 {
				deltaY = 1
			} else {
				deltaY = -1
			}
		}
		m.scrollY += deltaY
	}

	return animTick()
}

func (m *tableModel) cancelAnimation() {
	m.animating = false
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// ═══════════════════════════════════════════════════════════════════════════
// Status Message (flash notification)
// ═══════════════════════════════════════════════════════════════════════════

type statusClearMsg struct{}

const statusDuration = 2 * time.Second

// setStatus sets a temporary status message that auto-clears.
func (m *tableModel) setStatus(msg string) tea.Cmd {
	m.statusMsg = msg
	m.statusUntil = time.Now().Add(statusDuration)
	return tea.Tick(statusDuration, func(t time.Time) tea.Msg {
		return statusClearMsg{}
	})
}

// ═══════════════════════════════════════════════════════════════════════════
// Clipboard (yank)
// ═══════════════════════════════════════════════════════════════════════════

// yankCell copies the selected cell value to the system clipboard.
func (m *tableModel) yankCell() tea.Cmd {
	row := m.getDisplayRow(m.cursor)
	if row == nil {
		return nil
	}
	var val string
	if m.colCursor < len(row) {
		val = row[m.colCursor]
	}
	if err := clipboard.WriteAll(val); err != nil {
		return m.setStatus(fmt.Sprintf("clipboard error: %s", err))
	}
	display := val
	if len(display) > 40 {
		display = display[:37] + "..."
	}
	return m.setStatus(fmt.Sprintf("Copied: %s", display))
}

// yankRow copies the entire selected row (tab-separated) to the clipboard.
func (m *tableModel) yankRow() tea.Cmd {
	row := m.getDisplayRow(m.cursor)
	if row == nil {
		return nil
	}
	val := strings.Join(row, "\t")
	if err := clipboard.WriteAll(val); err != nil {
		return m.setStatus(fmt.Sprintf("clipboard error: %s", err))
	}
	return m.setStatus(fmt.Sprintf("Copied row (%d columns)", len(row)))
}

// ═══════════════════════════════════════════════════════════════════════════
// ANSI-aware Viewport Slicing
// ═══════════════════════════════════════════════════════════════════════════

// applyViewport extracts a horizontal slice of a string, handling ANSI escape
// codes properly. It returns the portion of the string from visual column
// startX with the given width.
func applyViewport(s string, startX, width int) string {
	if width <= 0 {
		return ""
	}
	if startX < 0 {
		startX = 0
	}

	var result strings.Builder
	result.Grow(width + 64)

	visualPos := 0
	outputChars := 0
	stylesApplied := false
	inEscape := false
	escapeSeq := strings.Builder{}

	var activeStyles []string

	runes := []rune(s)
	i := 0

	for i < len(runes) && outputChars < width {
		r := runes[i]

		if r == '\x1b' && i+1 < len(runes) && runes[i+1] == '[' {
			inEscape = true
			escapeSeq.Reset()
			escapeSeq.WriteRune(r)
			i++
			continue
		}

		if inEscape {
			escapeSeq.WriteRune(r)
			if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') {
				inEscape = false
				seq := escapeSeq.String()

				if r == 'm' {
					if seq == "\x1b[0m" || seq == "\x1b[m" {
						activeStyles = nil
					} else {
						activeStyles = append(activeStyles, seq)
					}
				}

				if visualPos >= startX {
					result.WriteString(seq)
				}
			}
			i++
			continue
		}

		if visualPos >= startX {
			if !stylesApplied && len(activeStyles) > 0 {
				for _, style := range activeStyles {
					result.WriteString(style)
				}
				stylesApplied = true
			}
			result.WriteRune(r)
			outputChars++
		}

		visualPos++
		i++
	}

	if len(activeStyles) > 0 && outputChars > 0 {
		result.WriteString("\x1b[0m")
	}

	if outputChars < width {
		result.WriteString(strings.Repeat(" ", width-outputChars))
	}

	return result.String()
}

// ═══════════════════════════════════════════════════════════════════════════
// Scroll Helpers
// ═══════════════════════════════════════════════════════════════════════════

func (m *tableModel) ensureRowVisible() {
	visibleRows := m.visibleRowCount()
	if visibleRows <= 0 {
		visibleRows = 1
	}
	if m.cursor < m.scrollY {
		m.scrollY = m.cursor
	} else if m.cursor >= m.scrollY+visibleRows {
		m.scrollY = m.cursor - visibleRows + 1
	}
}

func (m *tableModel) ensureColVisible() {
	colStartX := m.getColStartX(m.colCursor)
	colEndX := m.getColEndX(m.colCursor)
	colWidth := colEndX - colStartX
	viewportWidth := m.width - 2

	if colStartX < m.scrollX {
		m.scrollX = colStartX
	} else if colEndX > m.scrollX+viewportWidth {
		if colWidth <= viewportWidth {
			m.scrollX = colEndX - viewportWidth
		} else {
			m.scrollX = colStartX
		}
	}

	maxX := m.getMaxScrollX()
	if m.scrollX < 0 {
		m.scrollX = 0
	} else if m.scrollX > maxX {
		m.scrollX = maxX
	}
}

func (m *tableModel) ensureColVisibleFromLeft() {
	colStartX := m.getColStartX(m.colCursor)
	m.scrollX = colStartX

	maxX := m.getMaxScrollX()
	if m.scrollX < 0 {
		m.scrollX = 0
	} else if m.scrollX > maxX {
		m.scrollX = maxX
	}
}

func (m *tableModel) ensureColVisibleFromRight() {
	colStartX := m.getColStartX(m.colCursor)
	colEndX := m.getColEndX(m.colCursor)
	colWidth := colEndX - colStartX
	viewportWidth := m.width - 2

	if colWidth <= viewportWidth {
		m.scrollX = colEndX - viewportWidth
		if m.scrollX < colStartX {
			m.scrollX = colStartX
		}
	} else {
		m.scrollX = colEndX - viewportWidth
	}

	maxX := m.getMaxScrollX()
	if m.scrollX < 0 {
		m.scrollX = 0
	} else if m.scrollX > maxX {
		m.scrollX = maxX
	}
}

func (m tableModel) visibleRowCount() int {
	count := m.height - 5 // header (3 lines) + footer (2 lines)
	if count < 1 {
		count = 1
	}
	return count
}

// ═══════════════════════════════════════════════════════════════════════════
// View
// ═══════════════════════════════════════════════════════════════════════════

func (m tableModel) View() string {
	if !m.ready {
		return "Loading..."
	}

	var sb strings.Builder

	// Header with title info
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(styles.Accent)
	displayCount := m.displayRowCount()
	if m.filteredRows != nil {
		sb.WriteString(headerStyle.Render(fmt.Sprintf("%s: %d/%d rows, %d columns", m.title, displayCount, len(m.rows), len(m.columns))))
	} else {
		sb.WriteString(headerStyle.Render(fmt.Sprintf("%s: %d rows, %d columns", m.title, len(m.rows), len(m.columns))))
	}

	// Show state indicators for modified columns
	var stateInfo []string
	for i, state := range m.colStates {
		if state == colStateExpanded {
			stateInfo = append(stateInfo, fmt.Sprintf("%s+", m.columns[i]))
		} else if state == colStateHidden {
			stateInfo = append(stateInfo, fmt.Sprintf("%s-", m.columns[i]))
		}
	}
	if len(stateInfo) > 0 {
		sb.WriteString(styles.MutedMsg(fmt.Sprintf("  [%s]", strings.Join(stateInfo, ", "))))
	}
	sb.WriteString("\n")

	// Search bar
	if m.mode == tableModeSearch {
		sb.WriteString(fmt.Sprintf("/%s\n", m.searchInput.View()))
	} else if m.searchQuery != "" {
		sb.WriteString(styles.MutedMsg(fmt.Sprintf("filter: %s\n", m.searchQuery)))
	} else {
		sb.WriteString("\n")
	}

	sb.WriteString(m.renderTable())

	// Footer
	sb.WriteString("\n")
	if m.statusMsg != "" && time.Now().Before(m.statusUntil) {
		sb.WriteString(styles.SuccessMsg(m.statusMsg))
	} else if m.mode == tableModeSearch {
		help := styles.MutedMsg("enter confirm  esc cancel")
		sb.WriteString(help)
	} else {
		help := styles.MutedMsg("↑↓←→ nav  ⇧+arrow scroll  enter expand  H hide  / search  y copy  J json  R raw  P table  q quit")
		sb.WriteString(help)
	}

	return sb.String()
}

// ═══════════════════════════════════════════════════════════════════════════
// Render Table
// ═══════════════════════════════════════════════════════════════════════════

func (m tableModel) renderTable() string {
	var sb strings.Builder

	if len(m.columns) == 0 {
		return "No columns"
	}

	viewportWidth := m.width - 2

	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(styles.Info)
	selectedHeaderStyle := lipgloss.NewStyle().Bold(true).Foreground(styles.Accent)
	separatorStyle := lipgloss.NewStyle().Foreground(styles.Muted)
	selectedSepStyle := lipgloss.NewStyle().Foreground(styles.Accent)
	selectedRowStyle := lipgloss.NewStyle().Background(styles.BgHighlight)
	selectedCellStyle := lipgloss.NewStyle().Background(styles.Accent).Foreground(lipgloss.Color("#000000"))
	normalStyle := lipgloss.NewStyle()
	highlightStyle := lipgloss.NewStyle().Foreground(styles.Warning)

	headerLine := m.buildFullHeaderLine(headerStyle, selectedHeaderStyle)
	separatorLine := m.buildFullSeparatorLine(separatorStyle, selectedSepStyle)

	sb.WriteString(applyViewport(headerLine, m.scrollX, viewportWidth))
	sb.WriteString("\n")
	sb.WriteString(applyViewport(separatorLine, m.scrollX, viewportWidth))
	sb.WriteString("\n")

	visibleRows := m.visibleRowCount()
	displayCount := m.displayRowCount()
	endRow := m.scrollY + visibleRows
	if endRow > displayCount {
		endRow = displayCount
	}

	for displayIdx := m.scrollY; displayIdx < endRow; displayIdx++ {
		row := m.getDisplayRow(displayIdx)
		if row == nil {
			continue
		}
		isSelectedRow := displayIdx == m.cursor

		rowLine := m.buildFullRowLine(row, isSelectedRow, normalStyle, selectedRowStyle, selectedCellStyle, highlightStyle)
		sb.WriteString(applyViewport(rowLine, m.scrollX, viewportWidth))
		sb.WriteString("\n")
	}

	// Scroll indicators
	var indicators []string
	if m.scrollX > 0 {
		indicators = append(indicators, "◀")
	}
	totalWidth := m.getTotalWidth()
	if m.scrollX+viewportWidth < totalWidth {
		indicators = append(indicators, "▶")
	}
	if m.scrollY > 0 {
		indicators = append(indicators, "▲")
	}
	if m.scrollY+visibleRows < displayCount {
		indicators = append(indicators, "▼")
	}
	if len(indicators) > 0 {
		sb.WriteString(styles.MutedMsg(strings.Join(indicators, " ")))
	}

	return sb.String()
}

func (m tableModel) buildFullHeaderLine(normalStyle, selectedStyle lipgloss.Style) string {
	var sb strings.Builder

	for i, colName := range m.columns {
		colWidth := m.getColDisplayWidth(i)

		var displayName string
		if m.colStates[i] == colStateHidden {
			displayName = PadOrTruncate("...", colWidth)
		} else {
			displayName = PadOrTruncate(colName, colWidth)
		}

		if i == m.colCursor {
			sb.WriteString(selectedStyle.Render(displayName))
		} else {
			sb.WriteString(normalStyle.Render(displayName))
		}
		sb.WriteString("  ")
	}

	return sb.String()
}

func (m tableModel) buildFullSeparatorLine(normalStyle, selectedStyle lipgloss.Style) string {
	var sb strings.Builder

	for i := range m.columns {
		colWidth := m.getColDisplayWidth(i)
		sep := strings.Repeat("─", colWidth)

		if i == m.colCursor {
			sb.WriteString(selectedStyle.Render(sep))
		} else {
			sb.WriteString(normalStyle.Render(sep))
		}
		sb.WriteString("  ")
	}

	return sb.String()
}

func (m tableModel) buildFullRowLine(row []string, isSelectedRow bool, normalStyle, selectedRowStyle, selectedCellStyle, highlightStyle lipgloss.Style) string {
	var sb strings.Builder

	for i := range m.columns {
		colWidth := m.getColDisplayWidth(i)

		var val string
		if i < len(row) {
			val = row[i]
		}

		var displayVal string
		if m.colStates[i] == colStateHidden {
			displayVal = PadOrTruncate("...", colWidth)
		} else {
			displayVal = PadOrTruncate(val, colWidth)
		}

		isSelectedCol := i == m.colCursor
		hasSearchMatch := m.searchQuery != "" && strings.Contains(strings.ToLower(val), strings.ToLower(m.searchQuery))

		if isSelectedRow && isSelectedCol {
			sb.WriteString(selectedCellStyle.Render(displayVal))
		} else if isSelectedRow {
			sb.WriteString(selectedRowStyle.Render(displayVal))
		} else if hasSearchMatch {
			sb.WriteString(highlightStyle.Render(displayVal))
		} else {
			sb.WriteString(normalStyle.Render(displayVal))
		}
		sb.WriteString("  ")
	}

	return sb.String()
}
