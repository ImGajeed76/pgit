package styles

import "github.com/charmbracelet/lipgloss"

// Color palette from TUI_GUIDELINES.md
// Dark mode optimized, semantic colors
var (
	// Primary semantic colors
	Accent  = lipgloss.Color("#7C3AED") // violet-500 - highlights, interactive
	Success = lipgloss.Color("#10B981") // emerald-500 - success, additions
	Warning = lipgloss.Color("#F59E0B") // amber-500 - warnings, modified
	Error   = lipgloss.Color("#EF4444") // red-500 - errors, deletions
	Info    = lipgloss.Color("#3B82F6") // blue-500 - info, commit hashes
	Muted   = lipgloss.Color("#6B7280") // gray-500 - secondary text

	// Text colors
	TextPrimary   = lipgloss.Color("#F9FAFB") // gray-50 - main text
	TextSecondary = lipgloss.Color("#9CA3AF") // gray-400 - descriptions
	TextTertiary  = lipgloss.Color("#6B7280") // gray-500 - timestamps

	// Background colors
	BgHighlight = lipgloss.Color("#1F2937") // gray-800 - selected items
	BgBorder    = lipgloss.Color("#374151") // gray-700 - borders
)

// Semantic color aliases for clarity
var (
	// File status colors
	ColorAdded     = Success // New files
	ColorDeleted   = Error   // Deleted files
	ColorModified  = Warning // Modified files
	ColorUntracked = Muted   // Untracked files (gray, not red)
	ColorRenamed   = Info    // Renamed files

	// Commit/ref colors
	ColorHash   = Info    // Commit hashes (blue per guidelines)
	ColorBranch = Success // Branch names
	ColorRemote = Error   // Remote refs
	ColorTag    = Warning // Tags

	// Diff colors
	ColorDiffAdd     = Success // Added lines
	ColorDiffRemove  = Error   // Removed lines
	ColorDiffContext = Muted   // Context lines
	ColorDiffHunk    = Accent  // Hunk headers (violet)
)
