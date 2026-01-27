package styles

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Symbols - Unicode with ASCII fallbacks
const (
	SymbolSuccess = "✓"
	SymbolError   = "✗"
	SymbolWarning = "⚠"
	SymbolInfo    = "●"
	SymbolPending = "○"
	SymbolCommit  = "●"
	SymbolArrow   = "→"
)

// NoColor checks if colors should be disabled
func NoColor() bool {
	return os.Getenv("NO_COLOR") != "" || os.Getenv("PGIT_NO_COLOR") != ""
}

// IsAccessible checks if accessibility mode is enabled
// When enabled: no animations, no spinner, simplified output
func IsAccessible() bool {
	return os.Getenv("PGIT_ACCESSIBLE") == "1" || os.Getenv("PGIT_ACCESSIBLE") == "true"
}

// Base text styles
var (
	Bold      = lipgloss.NewStyle().Bold(true)
	Dim       = lipgloss.NewStyle().Foreground(Muted)
	Underline = lipgloss.NewStyle().Underline(true)
)

// Semantic styles - use these instead of raw colors
var (
	// Status indicators
	Added     = lipgloss.NewStyle().Foreground(ColorAdded)
	Deleted   = lipgloss.NewStyle().Foreground(ColorDeleted)
	Modified  = lipgloss.NewStyle().Foreground(ColorModified)
	Untracked = lipgloss.NewStyle().Foreground(ColorUntracked)
	Renamed   = lipgloss.NewStyle().Foreground(ColorRenamed)

	// Message types
	SuccessStyle = lipgloss.NewStyle().Foreground(Success)
	ErrorStyle   = lipgloss.NewStyle().Foreground(Error)
	WarningStyle = lipgloss.NewStyle().Foreground(Warning)
	InfoStyle    = lipgloss.NewStyle().Foreground(Info)
	MutedStyle   = lipgloss.NewStyle().Foreground(Muted)

	// Commit display
	HashStyle    = lipgloss.NewStyle().Foreground(ColorHash)
	BranchStyle  = lipgloss.NewStyle().Foreground(ColorBranch).Bold(true)
	RemoteStyle  = lipgloss.NewStyle().Foreground(ColorRemote)
	TagStyle     = lipgloss.NewStyle().Foreground(ColorTag)
	AuthorStyle  = lipgloss.NewStyle().Foreground(Success)
	DateStyle    = lipgloss.NewStyle().Foreground(Muted)
	MessageStyle = lipgloss.NewStyle()

	// Diff display
	DiffAddLine     = lipgloss.NewStyle().Foreground(ColorDiffAdd)
	DiffRemoveLine  = lipgloss.NewStyle().Foreground(ColorDiffRemove)
	DiffContextLine = lipgloss.NewStyle().Foreground(ColorDiffContext)
	DiffHunkHeader  = lipgloss.NewStyle().Foreground(ColorDiffHunk)
	DiffFileHeader  = lipgloss.NewStyle().Bold(true)

	// Interactive TUI
	SelectedStyle = lipgloss.NewStyle().
			Background(BgHighlight).
			Foreground(TextPrimary)

	// Help bar
	HelpKey   = lipgloss.NewStyle().Foreground(Accent)
	HelpValue = lipgloss.NewStyle().Foreground(Muted)
)

// ═══════════════════════════════════════════════════════════════════════════
// Render functions - centralized formatting with NoColor support
// ═══════════════════════════════════════════════════════════════════════════

// render applies a style if colors are enabled
func render(s lipgloss.Style, text string) string {
	if NoColor() {
		return text
	}
	return s.Render(text)
}

// Hash formats a commit hash (always lowercase, optionally short)
func Hash(hash string, short bool) string {
	hash = strings.ToLower(hash)
	if short && len(hash) > 7 {
		hash = hash[len(hash)-7:]
	}
	return render(HashStyle, hash)
}

// Branch formats a branch name
func Branch(name string) string {
	return render(BranchStyle, name)
}

// Author formats an author name
func Author(name string) string {
	return render(AuthorStyle, name)
}

// Date formats a date/timestamp
func Date(date string) string {
	return render(DateStyle, date)
}

// Path formats a file path
func Path(path string) string {
	return path // Paths are primary text, no special color
}

// StatusPrefix returns colored status prefix for file listings
func StatusPrefix(status string) string {
	switch status {
	case "A", "new":
		return render(Added, "A")
	case "M", "modified":
		return render(Modified, "M")
	case "D", "deleted":
		return render(Deleted, "D")
	case "R", "renamed":
		return render(Renamed, "R")
	case "?", "untracked":
		return render(Untracked, "?")
	default:
		return status
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// Message formatters - structured output
// ═══════════════════════════════════════════════════════════════════════════

// SuccessMsg formats a success message with checkmark
func SuccessMsg(msg string) string {
	symbol := SymbolSuccess
	if NoColor() {
		symbol = "+"
	}
	return fmt.Sprintf("%s %s", render(SuccessStyle, symbol), msg)
}

// ErrorMsg formats an error message
func ErrorMsg(title string) string {
	return render(ErrorStyle, "Error: "+title)
}

// WarningMsg formats a warning message
func WarningMsg(msg string) string {
	symbol := SymbolWarning
	if NoColor() {
		symbol = "!"
	}
	return fmt.Sprintf("%s %s", render(WarningStyle, symbol), msg)
}

// InfoMsg formats an info message
func InfoMsg(msg string) string {
	return render(InfoStyle, msg)
}

// MutedMsg formats muted/secondary text
func MutedMsg(msg string) string {
	return render(MutedStyle, msg)
}

// ═══════════════════════════════════════════════════════════════════════════
// Section formatters - consistent output structure
// ═══════════════════════════════════════════════════════════════════════════

// SectionHeader formats a section header
func SectionHeader(title string) string {
	return render(Bold, title)
}

// HelpLine formats a help line (key description)
func HelpLine(key, description string) string {
	return fmt.Sprintf("  %s %s", render(HelpKey, key), render(MutedStyle, description))
}

// Indent returns text indented by n spaces
func Indent(text string, n int) string {
	prefix := strings.Repeat(" ", n)
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		if line != "" {
			lines[i] = prefix + line
		}
	}
	return strings.Join(lines, "\n")
}

// ═══════════════════════════════════════════════════════════════════════════
// Color functions - simple string coloring (non-printf versions)
// ═══════════════════════════════════════════════════════════════════════════

func Yellow(s string) string      { return render(Modified, s) }
func Green(s string) string       { return render(Added, s) }
func Red(s string) string         { return render(Deleted, s) }
func Cyan(s string) string        { return render(InfoStyle, s) }
func Mute(s string) string        { return render(MutedStyle, s) }
func SuccessText(s string) string { return render(SuccessStyle, s) }
func WarningText(s string) string { return render(WarningStyle, s) }
func ErrorText(s string) string   { return render(ErrorStyle, s) }

// Printf-style color functions
func Yellowf(format string, a ...any) string  { return Yellow(fmt.Sprintf(format, a...)) }
func Greenf(format string, a ...any) string   { return Green(fmt.Sprintf(format, a...)) }
func Redf(format string, a ...any) string     { return Red(fmt.Sprintf(format, a...)) }
func Cyanf(format string, a ...any) string    { return Cyan(fmt.Sprintf(format, a...)) }
func Mutef(format string, a ...any) string    { return Mute(fmt.Sprintf(format, a...)) }
func Boldf(format string, a ...any) string    { return Bold.Render(fmt.Sprintf(format, a...)) }
func Errorf(format string, a ...any) string   { return ErrorText(fmt.Sprintf(format, a...)) }
func Successf(format string, a ...any) string { return SuccessText(fmt.Sprintf(format, a...)) }
func Warningf(format string, a ...any) string { return WarningText(fmt.Sprintf(format, a...)) }

// Deprecated: Use Hash() instead
func FormatHash(hash string, short bool) string {
	return Hash(hash, short)
}

// Deprecated: Use Branch/RemoteStyle directly
func FormatRef(name string, refType string) string {
	switch refType {
	case "head":
		return render(InfoStyle, name)
	case "branch":
		return render(BranchStyle, name)
	case "remote":
		return render(RemoteStyle, name)
	case "tag":
		return render(TagStyle, name)
	default:
		return name
	}
}
