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
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/imgajeed76/pgit/internal/db"
	"github.com/imgajeed76/pgit/internal/repo"
	"github.com/imgajeed76/pgit/internal/ui"
	"github.com/imgajeed76/pgit/internal/ui/styles"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func newSQLCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sql <query>",
		Short: "Execute SQL queries on the repository database",
		Long: `Execute SQL queries directly on the repository database.

By default, only read-only queries (SELECT) are allowed.
Use --write to enable INSERT, UPDATE, DELETE operations.

Interactive mode shows results in a navigable table.
Use --raw for plain output suitable for piping.

Use with caution - this can corrupt your repository!`,
		Args: cobra.ExactArgs(1),
		RunE: runSQL,
	}

	cmd.Flags().Bool("write", false, "Allow write operations (INSERT, UPDATE, DELETE)")
	cmd.Flags().Bool("raw", false, "Output raw values without formatting (for piping)")
	cmd.Flags().Bool("json", false, "Output results as JSON array")
	cmd.Flags().Bool("no-pager", false, "Disable interactive table view")
	cmd.Flags().Int("timeout", 60, "Query timeout in seconds")

	return cmd
}

func runSQL(cmd *cobra.Command, args []string) error {
	query := args[0]
	allowWrite, _ := cmd.Flags().GetBool("write")
	raw, _ := cmd.Flags().GetBool("raw")
	jsonOutput, _ := cmd.Flags().GetBool("json")
	noPager, _ := cmd.Flags().GetBool("no-pager")
	timeout, _ := cmd.Flags().GetInt("timeout")

	// Check if query is a write operation
	upperQuery := strings.ToUpper(strings.TrimSpace(query))
	isWrite := strings.HasPrefix(upperQuery, "INSERT") ||
		strings.HasPrefix(upperQuery, "UPDATE") ||
		strings.HasPrefix(upperQuery, "DELETE") ||
		strings.HasPrefix(upperQuery, "DROP") ||
		strings.HasPrefix(upperQuery, "CREATE") ||
		strings.HasPrefix(upperQuery, "ALTER") ||
		strings.HasPrefix(upperQuery, "TRUNCATE")

	if isWrite && !allowWrite {
		fmt.Println(styles.Errorf("Error: Write operations require --write flag"))
		fmt.Println()
		fmt.Println("This is a safety measure to prevent accidental data modification.")
		fmt.Println("If you're sure, run again with: pgit sql --write \"" + query + "\"")
		return fmt.Errorf("write operation not allowed")
	}

	r, err := repo.Open()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
	defer cancel()

	// Connect to database
	if err := r.Connect(ctx); err != nil {
		return err
	}
	defer r.Close()

	// Execute query
	if isWrite {
		// Use Exec for write operations
		if err := r.DB.Exec(ctx, query); err != nil {
			return err
		}
		fmt.Println("Query executed successfully")
		return nil
	}

	// Use Query for read operations
	rows, err := r.DB.Query(ctx, query)
	if err != nil {
		return err
	}
	defer rows.Close()

	// Get column descriptions
	fieldDescs := rows.FieldDescriptions()
	colNames := make([]string, len(fieldDescs))
	for i, fd := range fieldDescs {
		colNames[i] = string(fd.Name)
	}

	// Collect all rows
	var allRows [][]string
	for rows.Next() {
		values, err := rows.Values()
		if err != nil {
			return err
		}

		strValues := make([]string, len(values))
		for i, v := range values {
			strValues[i] = formatSQLValue(v)
		}
		allRows = append(allRows, strValues)
	}

	if err := rows.Err(); err != nil {
		return err
	}

	// Decide output mode
	isTTY := term.IsTerminal(int(os.Stdout.Fd()))

	if raw {
		// Raw tab-separated output
		for _, row := range allRows {
			fmt.Println(strings.Join(row, "\t"))
		}
		return nil
	}

	if jsonOutput {
		// JSON array of objects
		return printJSONSQLResults(colNames, allRows)
	}

	if !isTTY || noPager || len(allRows) == 0 {
		// Plain formatted table output
		printPlainTable(colNames, allRows)
		return nil
	}

	// Interactive TUI mode
	return runSQLTableTUI(query, colNames, allRows)
}

// formatSQLValue formats a SQL value for display
func formatSQLValue(v interface{}) string {
	if v == nil {
		return "NULL"
	}

	switch val := v.(type) {
	case []byte:
		// For byte arrays, check if it's printable text
		if len(val) == 0 {
			return ""
		}
		isPrintable := true
		for _, b := range val {
			if b < 32 && b != '\n' && b != '\r' && b != '\t' {
				isPrintable = false
				break
			}
		}
		if isPrintable {
			s := string(val)
			s = strings.ReplaceAll(s, "\n", "\\n")
			s = strings.ReplaceAll(s, "\r", "\\r")
			s = strings.ReplaceAll(s, "\t", "\\t")
			return s
		}
		return fmt.Sprintf("[%d bytes]", len(val))
	case string:
		s := val
		s = strings.ReplaceAll(s, "\n", "\\n")
		s = strings.ReplaceAll(s, "\r", "\\r")
		s = strings.ReplaceAll(s, "\t", "\\t")
		return s
	case time.Time:
		return val.Format("2006-01-02 15:04:05")
	default:
		return fmt.Sprintf("%v", v)
	}
}

// printJSONSQLResults outputs results as a JSON array of objects
func printJSONSQLResults(colNames []string, rows [][]string) error {
	results := make([]map[string]interface{}, len(rows))

	for i, row := range rows {
		obj := make(map[string]interface{})
		for j, colName := range colNames {
			if j < len(row) {
				// Try to preserve types for common cases
				val := row[j]
				if val == "NULL" {
					obj[colName] = nil
				} else {
					obj[colName] = val
				}
			} else {
				obj[colName] = nil
			}
		}
		results[i] = obj
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(results)
}

// printPlainTable prints a properly aligned table for non-TTY output
// Shows full content without truncation
func printPlainTable(colNames []string, rows [][]string) {
	if len(colNames) == 0 {
		fmt.Println("(0 rows)")
		return
	}

	// Calculate column widths based on actual content (no truncation)
	colWidths := make([]int, len(colNames))
	for i, name := range colNames {
		colWidths[i] = len(name)
	}
	for _, row := range rows {
		for i, val := range row {
			if i < len(colWidths) && len(val) > colWidths[i] {
				colWidths[i] = len(val)
			}
		}
	}

	// Print header
	for i, name := range colNames {
		if i > 0 {
			fmt.Print("  ")
		}
		fmt.Print(pad(name, colWidths[i]))
	}
	fmt.Println()

	// Print separator
	for i, w := range colWidths {
		if i > 0 {
			fmt.Print("  ")
		}
		fmt.Print(strings.Repeat("─", w))
	}
	fmt.Println()

	// Print rows (full content, no truncation)
	for _, row := range rows {
		for i, val := range row {
			if i >= len(colWidths) {
				break
			}
			if i > 0 {
				fmt.Print("  ")
			}
			fmt.Print(pad(val, colWidths[i]))
		}
		fmt.Println()
	}

	fmt.Println()
	fmt.Printf("(%d rows)\n", len(rows))
}

// pad adds spaces to reach the desired width (no truncation)
func pad(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return s + strings.Repeat(" ", width-len(s))
}

// truncate shortens a string to fit width, adding "..." if needed
func truncate(s string, width int) string {
	if len(s) <= width {
		return s
	}
	if width > 3 {
		return s[:width-3] + "..."
	}
	return s[:width]
}

// padOrTruncate pads or truncates to exact width (for TUI table)
func padOrTruncate(s string, width int) string {
	if len(s) > width {
		return truncate(s, width)
	}
	return s + strings.Repeat(" ", width-len(s))
}

// ═══════════════════════════════════════════════════════════════════════════
// Interactive SQL Table TUI
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

// Exit mode - what to do after quitting TUI
type exitMode int

const (
	exitNormal exitMode = iota
	exitJSON
	exitRaw
	exitPlain
)

type sqlTableModel struct {
	query         string
	columns       []string
	rows          [][]string
	filteredRows  []int      // indices of rows matching search (nil = show all)
	fullColWidths []int      // actual max width of each column's content
	colStates     []colState // display state for each column
	cursor        int        // selected row (in filtered view)
	colCursor     int        // selected column
	scrollX       int        // horizontal scroll offset (in pixels/chars)
	scrollY       int        // vertical scroll offset (in rows)
	width         int        // terminal width
	height        int        // terminal height
	ready         bool
	mode          tableMode
	searchInput   textinput.Model
	searchQuery   string
	exitMode      exitMode // how to exit (for re-printing data)
}

type sqlKeyMap struct {
	Up          key.Binding
	Down        key.Binding
	Left        key.Binding
	Right       key.Binding
	PageUp      key.Binding
	PageDown    key.Binding
	Home        key.Binding
	End         key.Binding
	Expand      key.Binding
	Hide        key.Binding
	Search      key.Binding
	Quit        key.Binding
	ExportJSON  key.Binding
	ExportRaw   key.Binding
	ExportPlain key.Binding
}

var sqlKeys = sqlKeyMap{
	Up:          key.NewBinding(key.WithKeys("up", "k"), key.WithHelp("↑/k", "up")),
	Down:        key.NewBinding(key.WithKeys("down", "j"), key.WithHelp("↓/j", "down")),
	Left:        key.NewBinding(key.WithKeys("left"), key.WithHelp("←", "prev column")),
	Right:       key.NewBinding(key.WithKeys("right"), key.WithHelp("→", "next column")),
	PageUp:      key.NewBinding(key.WithKeys("pgup", "ctrl+u"), key.WithHelp("pgup", "page up")),
	PageDown:    key.NewBinding(key.WithKeys("pgdown", "ctrl+d"), key.WithHelp("pgdn", "page down")),
	Home:        key.NewBinding(key.WithKeys("home", "g"), key.WithHelp("g", "first row")),
	End:         key.NewBinding(key.WithKeys("end", "G"), key.WithHelp("G", "last row")),
	Expand:      key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "expand/default")),
	Hide:        key.NewBinding(key.WithKeys("H"), key.WithHelp("H", "hide/default")),
	Search:      key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "search")),
	Quit:        key.NewBinding(key.WithKeys("q", "ctrl+c", "esc"), key.WithHelp("q", "quit")),
	ExportJSON:  key.NewBinding(key.WithKeys("J"), key.WithHelp("J", "print as JSON")),
	ExportRaw:   key.NewBinding(key.WithKeys("R"), key.WithHelp("R", "print raw")),
	ExportPlain: key.NewBinding(key.WithKeys("P"), key.WithHelp("P", "print table")),
}

func runSQLTableTUI(query string, columns []string, rows [][]string) error {
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

	m := sqlTableModel{
		query:         query,
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
	if fm, ok := finalModel.(sqlTableModel); ok {
		switch fm.exitMode {
		case exitJSON:
			return printJSONSQLResults(columns, rows)
		case exitRaw:
			for _, row := range rows {
				fmt.Println(strings.Join(row, "\t"))
			}
		case exitPlain:
			printPlainTable(columns, rows)
		}
	}

	return nil
}

func (m sqlTableModel) Init() tea.Cmd {
	return nil
}

func (m sqlTableModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.ready = true

	case tea.KeyMsg:
		// Handle search mode
		if m.mode == tableModeSearch {
			return m.updateSearch(msg)
		}

		// Normal mode
		switch {
		case key.Matches(msg, sqlKeys.Quit):
			return m, tea.Quit

		case key.Matches(msg, sqlKeys.Search):
			m.mode = tableModeSearch
			m.searchInput.Focus()
			return m, textinput.Blink

		case key.Matches(msg, sqlKeys.Up):
			if m.cursor > 0 {
				m.cursor--
				m.ensureRowVisible()
			}

		case key.Matches(msg, sqlKeys.Down):
			maxRows := m.displayRowCount()
			if m.cursor < maxRows-1 {
				m.cursor++
				m.ensureRowVisible()
			}

		case key.Matches(msg, sqlKeys.Left):
			if m.colCursor > 0 {
				m.colCursor--
				m.ensureColVisible()
			}

		case key.Matches(msg, sqlKeys.Right):
			if m.colCursor < len(m.columns)-1 {
				m.colCursor++
				m.ensureColVisible()
			}

		case key.Matches(msg, sqlKeys.PageUp):
			visibleRows := m.visibleRowCount()
			m.cursor -= visibleRows
			if m.cursor < 0 {
				m.cursor = 0
			}
			m.ensureRowVisible()

		case key.Matches(msg, sqlKeys.PageDown):
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

		case key.Matches(msg, sqlKeys.Home):
			m.cursor = 0
			m.scrollY = 0

		case key.Matches(msg, sqlKeys.End):
			maxRows := m.displayRowCount()
			if maxRows > 0 {
				m.cursor = maxRows - 1
				m.ensureRowVisible()
			}

		case key.Matches(msg, sqlKeys.Expand):
			// Toggle between expanded and default
			if m.colCursor < len(m.colStates) {
				if m.colStates[m.colCursor] == colStateExpanded {
					m.colStates[m.colCursor] = colStateDefault
				} else {
					m.colStates[m.colCursor] = colStateExpanded
				}
			}

		case key.Matches(msg, sqlKeys.Hide):
			// Toggle between hidden and default
			if m.colCursor < len(m.colStates) {
				if m.colStates[m.colCursor] == colStateHidden {
					m.colStates[m.colCursor] = colStateDefault
				} else {
					m.colStates[m.colCursor] = colStateHidden
				}
			}

		case key.Matches(msg, sqlKeys.ExportJSON):
			m.exitMode = exitJSON
			return m, tea.Quit

		case key.Matches(msg, sqlKeys.ExportRaw):
			m.exitMode = exitRaw
			return m, tea.Quit

		case key.Matches(msg, sqlKeys.ExportPlain):
			m.exitMode = exitPlain
			return m, tea.Quit
		}
	}

	return m, nil
}

func (m sqlTableModel) updateSearch(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEsc:
		// Cancel search, clear filter
		m.mode = tableModeNormal
		m.searchInput.Blur()
		m.searchInput.SetValue("")
		m.searchQuery = ""
		m.filteredRows = nil
		m.cursor = 0
		m.scrollY = 0
		return m, nil
	case tea.KeyEnter:
		// Confirm search and exit search mode
		m.mode = tableModeNormal
		m.searchInput.Blur()
		return m, nil
	}

	// Update text input
	var cmd tea.Cmd
	m.searchInput, cmd = m.searchInput.Update(msg)

	// Live filter as user types
	m.searchQuery = m.searchInput.Value()
	m.performSearch()

	return m, cmd
}

func (m *sqlTableModel) performSearch() {
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

	// Reset cursor if it's out of bounds
	if m.cursor >= len(m.filteredRows) {
		m.cursor = 0
		m.scrollY = 0
	}
}

// displayRowCount returns the number of rows to display (filtered or all)
func (m sqlTableModel) displayRowCount() int {
	if m.filteredRows != nil {
		return len(m.filteredRows)
	}
	return len(m.rows)
}

// getDisplayRow returns the actual row at the given display index
func (m sqlTableModel) getDisplayRow(displayIdx int) []string {
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

// highlightSearchMatch highlights the search query in a string
func (m sqlTableModel) highlightSearchMatch(text string) string {
	if m.searchQuery == "" {
		return text
	}

	lowerText := strings.ToLower(text)
	lowerQuery := strings.ToLower(m.searchQuery)

	idx := strings.Index(lowerText, lowerQuery)
	if idx == -1 {
		return text
	}

	// Simple highlight - just return text as-is since we're using color on entire cell
	return text
}

// getColDisplayWidth returns the display width for a column based on its state
func (m sqlTableModel) getColDisplayWidth(colIdx int) int {
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

func (m *sqlTableModel) ensureRowVisible() {
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

func (m *sqlTableModel) ensureColVisible() {
	// Calculate column positions and ensure selected column is visible
	// This is done in renderTable, but we track scrollX as character offset
	// For simplicity, we'll just ensure the column cursor doesn't go out of bounds
	// The render function handles actual visibility
}

func (m sqlTableModel) visibleRowCount() int {
	// Account for header (3 lines) + footer (2 lines)
	count := m.height - 5
	if count < 1 {
		count = 1
	}
	return count
}

func (m sqlTableModel) View() string {
	if !m.ready {
		return "Loading..."
	}

	var sb strings.Builder

	// Header with query info
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(styles.Accent)
	displayCount := m.displayRowCount()
	if m.filteredRows != nil {
		sb.WriteString(headerStyle.Render(fmt.Sprintf("pgit sql: %d/%d rows, %d columns", displayCount, len(m.rows), len(m.columns))))
	} else {
		sb.WriteString(headerStyle.Render(fmt.Sprintf("pgit sql: %d rows, %d columns", len(m.rows), len(m.columns))))
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
	if m.mode == tableModeSearch {
		help := styles.MutedMsg("enter confirm  esc cancel")
		sb.WriteString(help)
	} else {
		help := styles.MutedMsg("↑↓←→ navigate  enter expand  H hide  / search  J json  R raw  P table  q quit")
		sb.WriteString(help)
	}

	return sb.String()
}

func (m sqlTableModel) renderTable() string {
	var sb strings.Builder

	if len(m.columns) == 0 {
		return "No columns"
	}

	// Calculate which columns fit in the available width
	availableWidth := m.width - 2 // margin

	// Find columns that fit, starting from scrollX offset
	type visibleCol struct {
		index    int
		width    int
		startPos int
	}
	var visibleCols []visibleCol
	currentPos := 0

	// Calculate total width before scrollX to handle horizontal scrolling
	scrollOffset := 0
	for i := 0; i < len(m.columns); i++ {
		colW := m.getColDisplayWidth(i) + 2 // +2 for padding between columns
		if i < m.colCursor {
			scrollOffset += colW
		}
		if i == m.colCursor {
			break
		}
	}

	// Adjust scrollX to keep selected column visible
	// Start from beginning and add columns until we run out of space
	usedWidth := 0
	startCol := 0

	// Find starting column based on scroll position
	// Try to keep colCursor visible
	for startCol < len(m.columns) {
		testWidth := 0
		canFitCursor := false
		for i := startCol; i < len(m.columns); i++ {
			colW := m.getColDisplayWidth(i) + 2
			if testWidth+colW > availableWidth && i > startCol {
				break
			}
			testWidth += colW
			if i == m.colCursor {
				canFitCursor = true
			}
		}
		if canFitCursor {
			break
		}
		startCol++
	}

	// Now build visible columns list
	usedWidth = 0
	for i := startCol; i < len(m.columns); i++ {
		colW := m.getColDisplayWidth(i)
		totalW := colW + 2 // +2 for separator
		if usedWidth+totalW > availableWidth && len(visibleCols) > 0 {
			break
		}
		visibleCols = append(visibleCols, visibleCol{
			index:    i,
			width:    colW,
			startPos: currentPos,
		})
		usedWidth += totalW
		currentPos += totalW
	}

	if len(visibleCols) == 0 && len(m.columns) > 0 {
		// At least show one column
		visibleCols = append(visibleCols, visibleCol{
			index: m.colCursor,
			width: m.getColDisplayWidth(m.colCursor),
		})
	}

	// Column header
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(styles.Info)
	selectedHeaderStyle := lipgloss.NewStyle().Bold(true).Foreground(styles.Accent)

	for _, vc := range visibleCols {
		var name string
		if m.colStates[vc.index] == colStateHidden {
			name = "..."
		} else {
			name = padOrTruncate(m.columns[vc.index], vc.width)
		}

		if vc.index == m.colCursor {
			sb.WriteString(selectedHeaderStyle.Render(name))
		} else {
			sb.WriteString(headerStyle.Render(name))
		}
		sb.WriteString("  ")
	}
	sb.WriteString("\n")

	// Separator
	separatorStyle := lipgloss.NewStyle().Foreground(styles.Muted)
	selectedSepStyle := lipgloss.NewStyle().Foreground(styles.Accent)
	for _, vc := range visibleCols {
		sep := strings.Repeat("─", vc.width)
		if vc.index == m.colCursor {
			sb.WriteString(selectedSepStyle.Render(sep))
		} else {
			sb.WriteString(separatorStyle.Render(sep))
		}
		sb.WriteString("  ")
	}
	sb.WriteString("\n")

	// Rows
	visibleRows := m.visibleRowCount()
	displayCount := m.displayRowCount()
	endRow := m.scrollY + visibleRows
	if endRow > displayCount {
		endRow = displayCount
	}

	selectedRowStyle := lipgloss.NewStyle().Background(styles.BgHighlight)
	selectedCellStyle := lipgloss.NewStyle().Background(styles.Accent).Foreground(lipgloss.Color("#000000"))
	normalStyle := lipgloss.NewStyle()
	highlightStyle := lipgloss.NewStyle().Foreground(styles.Warning) // highlight search matches

	for displayIdx := m.scrollY; displayIdx < endRow; displayIdx++ {
		row := m.getDisplayRow(displayIdx)
		if row == nil {
			continue
		}
		isSelectedRow := displayIdx == m.cursor

		for _, vc := range visibleCols {
			val := ""
			if vc.index < len(row) {
				val = row[vc.index]
			}

			if m.colStates[vc.index] == colStateHidden {
				val = "..."
			} else {
				val = padOrTruncate(val, vc.width)
			}

			isSelectedCol := vc.index == m.colCursor

			// Highlight search matches in the value
			displayVal := val
			if m.searchQuery != "" && strings.Contains(strings.ToLower(val), strings.ToLower(m.searchQuery)) {
				displayVal = m.highlightSearchMatch(val)
			}

			if isSelectedRow && isSelectedCol {
				sb.WriteString(selectedCellStyle.Render(val))
			} else if isSelectedRow {
				sb.WriteString(selectedRowStyle.Render(val))
			} else if m.searchQuery != "" && strings.Contains(strings.ToLower(val), strings.ToLower(m.searchQuery)) {
				sb.WriteString(highlightStyle.Render(displayVal))
			} else {
				sb.WriteString(normalStyle.Render(val))
			}
			sb.WriteString("  ")
		}
		sb.WriteString("\n")
	}

	// Scroll indicators
	var indicators []string
	if startCol > 0 {
		indicators = append(indicators, "◀")
	}
	lastVisibleCol := 0
	if len(visibleCols) > 0 {
		lastVisibleCol = visibleCols[len(visibleCols)-1].index
	}
	if lastVisibleCol < len(m.columns)-1 {
		indicators = append(indicators, "▶")
	}
	if len(indicators) > 0 {
		sb.WriteString(styles.MutedMsg(strings.Join(indicators, " ")))
	}

	return sb.String()
}

// ═══════════════════════════════════════════════════════════════════════════
// Stats Command (unchanged)
// ═══════════════════════════════════════════════════════════════════════════

func newStatsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Show repository statistics and compression info",
		Long: `Display statistics about the repository including:
  - Number of commits and files
  - Storage size and compression ratio
  - pg-xpatch delta compression statistics`,
		RunE: runStats,
	}

	cmd.Flags().Bool("xpatch", false, "Include detailed pg-xpatch compression stats (slower)")
	cmd.Flags().Bool("json", false, "Output in JSON format")

	return cmd
}

func runStats(cmd *cobra.Command, args []string) error {
	showXpatch, _ := cmd.Flags().GetBool("xpatch")
	jsonOutput, _ := cmd.Flags().GetBool("json")

	r, err := repo.Open()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Connect to database
	if err := r.Connect(ctx); err != nil {
		return err
	}
	defer r.Close()

	// Show spinner while gathering stats (can take a few seconds on large repos)
	var spinner *ui.Spinner
	if !jsonOutput {
		spinner = ui.NewSpinner("Gathering repository statistics")
		spinner.Start()
	}

	stats, err := r.DB.GetRepoStatsFast(ctx)
	if spinner != nil {
		spinner.Stop()
	}
	if err != nil {
		return err
	}

	if jsonOutput {
		return printJSONStats(ctx, r, stats, showXpatch)
	}

	// Display repository overview
	fmt.Println(styles.Boldf("Repository Statistics"))
	fmt.Println()

	fmt.Printf("  Commits:        %s\n", styles.Cyanf("%d", stats.TotalCommits))
	fmt.Printf("  Files tracked:  %s\n", styles.Cyanf("%d", stats.UniqueFiles))
	fmt.Printf("  Blob versions:  %s\n", styles.Cyanf("%d", stats.TotalBlobs))
	if stats.TotalContentSize > 0 {
		fmt.Printf("  Content size:   %s %s\n",
			formatBytes(stats.TotalContentSize),
			styles.Mute("(uncompressed)"))
	}

	// Storage section
	fmt.Println()
	fmt.Println(styles.Boldf("Storage (on disk)"))
	fmt.Println()

	// Calculate total for all data tables
	totalDataStorage := stats.CommitsTableSize + stats.PathsTableSize + stats.FileRefsTableSize + stats.ContentTableSize
	fmt.Printf("  Commits table:   %s\n", formatBytes(stats.CommitsTableSize))
	fmt.Printf("  Paths table:     %s\n", formatBytes(stats.PathsTableSize))
	fmt.Printf("  File refs table: %s\n", formatBytes(stats.FileRefsTableSize))
	fmt.Printf("  Content table:   %s\n", formatBytes(stats.ContentTableSize))
	fmt.Printf("  Indexes:         %s\n", formatBytes(stats.TotalIndexSize))
	fmt.Printf("  ─────────────────────\n")
	fmt.Printf("  Total:           %s\n", styles.SuccessText(formatBytes(totalDataStorage+stats.TotalIndexSize)))

	// Show compression ratio if we have meaningful content size
	if stats.TotalContentSize > 1024 && stats.ContentTableSize > 0 {
		ratio := float64(stats.TotalContentSize) / float64(stats.ContentTableSize)
		savings := (1 - float64(stats.ContentTableSize)/float64(stats.TotalContentSize)) * 100
		if savings > 0 {
			fmt.Printf("\n  %s %.1fx compression (%.0f%% space saved)\n",
				styles.Successf("→"), ratio, savings)
		}
	}

	// xpatch stats (optional, can be slow)
	if showXpatch {
		fmt.Println()
		fmt.Println(styles.Boldf("pg-xpatch Compression"))

		// Commits xpatch
		fmt.Println()
		fmt.Printf("  %s\n", styles.Mute("pgit_commits:"))
		commitXpatch, err := r.DB.GetXpatchStats(ctx, "pgit_commits")
		if err != nil {
			fmt.Printf("    Unable to get stats: %v\n", styles.Mute(err.Error()))
		} else {
			printXpatchStats(commitXpatch)
		}

		// Content xpatch (replaces pgit_blobs in schema v2)
		fmt.Println()
		fmt.Printf("  %s\n", styles.Mute("pgit_content:"))
		contentXpatch, err := r.DB.GetXpatchStats(ctx, "pgit_content")
		if err != nil {
			fmt.Printf("    Unable to get stats: %v\n", styles.Mute(err.Error()))
		} else {
			printXpatchStats(contentXpatch)
		}
	} else {
		fmt.Println()
		fmt.Printf("%s Use --xpatch for detailed compression stats\n", styles.Mute("hint:"))
	}

	return nil
}

func printXpatchStats(stats *db.XpatchStats) {
	if stats == nil {
		fmt.Printf("    No stats available\n")
		return
	}

	fmt.Printf("    Rows:         %d\n", stats.TotalRows)
	fmt.Printf("    Groups:       %d\n", stats.TotalGroups)
	fmt.Printf("    Keyframes:    %d\n", stats.KeyframeCount)
	fmt.Printf("    Deltas:       %d\n", stats.DeltaCount)

	if stats.RawSizeBytes > 0 {
		fmt.Printf("    Raw size:     %s\n", formatBytes(stats.RawSizeBytes))
		fmt.Printf("    Compressed:   %s\n", formatBytes(stats.CompressedBytes))

		// Calculate savings
		savings := float64(stats.RawSizeBytes-stats.CompressedBytes) / float64(stats.RawSizeBytes) * 100
		if savings > 0 {
			fmt.Printf("    Ratio:        %.1fx %s\n",
				stats.CompressionRatio,
				styles.Successf("(%.0f%% saved)", savings))
		} else {
			fmt.Printf("    Ratio:        %.2fx\n", stats.CompressionRatio)
		}
	}

	if stats.AvgChainLength > 0 {
		fmt.Printf("    Avg chain:    %.1f\n", stats.AvgChainLength)
	}

	cacheTotal := stats.CacheHits + stats.CacheMisses
	if cacheTotal > 0 {
		hitRate := float64(stats.CacheHits) / float64(cacheTotal) * 100
		fmt.Printf("    Cache hit:    %.1f%%\n", hitRate)
	}
}

// JSONStats represents stats output in JSON format
type JSONStats struct {
	Repository JSONRepoStats    `json:"repository"`
	Storage    JSONStorageStats `json:"storage"`
	Xpatch     *JSONXpatchStats `json:"xpatch,omitempty"`
}

type JSONRepoStats struct {
	Commits          int64 `json:"commits"`
	FilesTracked     int64 `json:"files_tracked"`
	BlobVersions     int64 `json:"blob_versions"`
	ContentSizeBytes int64 `json:"content_size_bytes"`
}

type JSONStorageStats struct {
	CommitsTableBytes  int64   `json:"commits_table_bytes"`
	PathsTableBytes    int64   `json:"paths_table_bytes"`
	FileRefsTableBytes int64   `json:"file_refs_table_bytes"`
	ContentTableBytes  int64   `json:"content_table_bytes"`
	IndexesBytes       int64   `json:"indexes_bytes"`
	TotalBytes         int64   `json:"total_bytes"`
	CompressionRatio   float64 `json:"compression_ratio,omitempty"`
	SpaceSavedPercent  float64 `json:"space_saved_percent,omitempty"`
}

type JSONXpatchStats struct {
	Commits *JSONXpatchTableStats `json:"commits,omitempty"`
	Content *JSONXpatchTableStats `json:"content,omitempty"`
}

type JSONXpatchTableStats struct {
	TotalRows        int64   `json:"total_rows"`
	TotalGroups      int64   `json:"total_groups"`
	KeyframeCount    int64   `json:"keyframe_count"`
	DeltaCount       int64   `json:"delta_count"`
	RawSizeBytes     int64   `json:"raw_size_bytes"`
	CompressedBytes  int64   `json:"compressed_bytes"`
	CompressionRatio float64 `json:"compression_ratio"`
	AvgChainLength   float64 `json:"avg_chain_length"`
	CacheHitPercent  float64 `json:"cache_hit_percent"`
}

func printJSONStats(ctx context.Context, r *repo.Repository, stats *db.RepoStats, showXpatch bool) error {
	totalStorage := stats.CommitsTableSize + stats.PathsTableSize + stats.FileRefsTableSize + stats.ContentTableSize + stats.TotalIndexSize

	jsonStats := JSONStats{
		Repository: JSONRepoStats{
			Commits:          stats.TotalCommits,
			FilesTracked:     stats.UniqueFiles,
			BlobVersions:     stats.TotalBlobs,
			ContentSizeBytes: stats.TotalContentSize,
		},
		Storage: JSONStorageStats{
			CommitsTableBytes:  stats.CommitsTableSize,
			PathsTableBytes:    stats.PathsTableSize,
			FileRefsTableBytes: stats.FileRefsTableSize,
			ContentTableBytes:  stats.ContentTableSize,
			IndexesBytes:       stats.TotalIndexSize,
			TotalBytes:         totalStorage,
		},
	}

	if stats.TotalContentSize > 1024 && stats.ContentTableSize > 0 {
		ratio := float64(stats.TotalContentSize) / float64(stats.ContentTableSize)
		savings := (1 - float64(stats.ContentTableSize)/float64(stats.TotalContentSize)) * 100
		if savings > 0 {
			jsonStats.Storage.CompressionRatio = ratio
			jsonStats.Storage.SpaceSavedPercent = savings
		}
	}

	if showXpatch {
		jsonStats.Xpatch = &JSONXpatchStats{}

		commitXpatch, err := r.DB.GetXpatchStats(ctx, "pgit_commits")
		if err == nil && commitXpatch != nil {
			jsonStats.Xpatch.Commits = xpatchToJSON(commitXpatch)
		}

		contentXpatch, err := r.DB.GetXpatchStats(ctx, "pgit_content")
		if err == nil && contentXpatch != nil {
			jsonStats.Xpatch.Content = xpatchToJSON(contentXpatch)
		}
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(jsonStats)
}

func xpatchToJSON(stats *db.XpatchStats) *JSONXpatchTableStats {
	result := &JSONXpatchTableStats{
		TotalRows:        stats.TotalRows,
		TotalGroups:      stats.TotalGroups,
		KeyframeCount:    stats.KeyframeCount,
		DeltaCount:       stats.DeltaCount,
		RawSizeBytes:     stats.RawSizeBytes,
		CompressedBytes:  stats.CompressedBytes,
		CompressionRatio: stats.CompressionRatio,
		AvgChainLength:   stats.AvgChainLength,
	}

	cacheTotal := stats.CacheHits + stats.CacheMisses
	if cacheTotal > 0 {
		result.CacheHitPercent = float64(stats.CacheHits) / float64(cacheTotal) * 100
	}

	return result
}

func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}
