package repo

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/imgajeed76/pgit/v2/internal/db"
	"github.com/imgajeed76/pgit/v2/internal/ui/styles"
	"github.com/sergi/go-diff/diffmatchpatch"
)

// DiffOptions contains options for generating diffs
type DiffOptions struct {
	Staged     bool   // Show staged changes
	Path       string // Specific file path (empty for all)
	Context    int    // Number of context lines (default 3)
	NoColor    bool   // Disable colors
	NameOnly   bool   // Only show file names
	NameStatus bool   // Show file names with status
}

// DiffResult represents a diff for a single file
type DiffResult struct {
	Path       string
	Status     ChangeStatus
	OldContent string
	NewContent string
	Hunks      []DiffHunk
}

// DiffHunk represents a single hunk in a diff
type DiffHunk struct {
	OldStart int
	OldCount int
	NewStart int
	NewCount int
	Lines    []DiffLine
}

// DiffLine represents a single line in a diff
type DiffLine struct {
	Type    DiffLineType
	Content string
}

// DiffLineType represents the type of a diff line
type DiffLineType int

const (
	DiffLineContext DiffLineType = iota
	DiffLineAdd
	DiffLineDelete
)

// Diff generates diffs for changes
func (r *Repository) Diff(ctx context.Context, opts DiffOptions) ([]DiffResult, error) {
	var changes []FileChange
	var err error

	if opts.Staged {
		changes, err = r.GetStagedChanges(ctx)
	} else {
		changes, err = r.GetUnstagedChanges(ctx)
	}
	if err != nil {
		return nil, err
	}

	// Filter by path if specified
	if opts.Path != "" {
		var filtered []FileChange
		for _, c := range changes {
			if c.Path == opts.Path || strings.HasPrefix(c.Path, opts.Path+"/") {
				filtered = append(filtered, c)
			}
		}
		changes = filtered
	}

	if opts.Context == 0 {
		opts.Context = 3
	}

	var results []DiffResult
	for _, change := range changes {
		result := DiffResult{
			Path:   change.Path,
			Status: change.Status,
		}

		// For name-only or name-status, we don't need content at all
		if opts.NameOnly || opts.NameStatus {
			results = append(results, result)
			continue
		}

		// Get old content (from database)
		if change.Status != StatusNew {
			blob, err := r.getFileContent(ctx, change.Path)
			if err == nil && blob != nil && blob.Content != nil {
				result.OldContent = string(blob.Content)
			}
		}

		// Get new content (from working directory)
		if change.Status != StatusDeleted {
			absPath := r.AbsPath(change.Path)
			content, err := os.ReadFile(absPath)
			if err == nil {
				result.NewContent = string(content)
			}
		}

		// Generate hunks
		result.Hunks = GenerateHunks(result.OldContent, result.NewContent, opts.Context)

		results = append(results, result)
	}

	return results, nil
}

// getFileContent gets the content of a file at HEAD
func (r *Repository) getFileContent(ctx context.Context, path string) (*db.Blob, error) {
	head, err := r.DB.GetHeadCommit(ctx)
	if err != nil || head == nil {
		return nil, err
	}
	return r.DB.GetFileAtCommit(ctx, path, head.ID)
}

// GenerateHunks creates diff hunks from old and new content
func GenerateHunks(oldContent, newContent string, contextLines int) []DiffHunk {
	dmp := diffmatchpatch.New()

	// Convert to runes for proper Unicode handling
	oldRunes, newRunes, lineArray := dmp.DiffLinesToRunes(oldContent, newContent)
	diffs := dmp.DiffMainRunes(oldRunes, newRunes, false)
	diffs = dmp.DiffCharsToLines(diffs, lineArray)

	// Convert to line-based diff
	var lines []DiffLine
	for _, diff := range diffs {
		text := diff.Text
		// Split by lines but keep the structure
		diffLines := strings.Split(text, "\n")
		for i, line := range diffLines {
			// Skip empty last line from split
			if i == len(diffLines)-1 && line == "" {
				continue
			}

			var lineType DiffLineType
			switch diff.Type {
			case diffmatchpatch.DiffEqual:
				lineType = DiffLineContext
			case diffmatchpatch.DiffInsert:
				lineType = DiffLineAdd
			case diffmatchpatch.DiffDelete:
				lineType = DiffLineDelete
			}

			lines = append(lines, DiffLine{
				Type:    lineType,
				Content: line,
			})
		}
	}

	// Group into hunks with context
	return groupIntoHunks(lines, contextLines)
}

// groupIntoHunks groups diff lines into hunks with context
func groupIntoHunks(lines []DiffLine, contextLines int) []DiffHunk {
	if len(lines) == 0 {
		return nil
	}

	var hunks []DiffHunk
	var currentHunk *DiffHunk

	oldLine := 1
	newLine := 1

	for i, line := range lines {
		isChange := line.Type != DiffLineContext

		// Check if we need context before a change
		needsNewHunk := isChange && currentHunk == nil

		// Check if this change is far from the last one
		if isChange && currentHunk != nil {
			// Count context lines since last change
			contextCount := 0
			for j := i - 1; j >= 0 && lines[j].Type == DiffLineContext; j-- {
				contextCount++
			}
			if contextCount > contextLines*2 {
				// Too far, start new hunk
				hunks = append(hunks, *currentHunk)
				currentHunk = nil
				needsNewHunk = true
			}
		}

		if needsNewHunk {
			// Start new hunk with leading context
			hunk := DiffHunk{
				OldStart: oldLine,
				NewStart: newLine,
			}

			// Add leading context
			start := i - contextLines
			if start < 0 {
				start = 0
			}
			for j := start; j < i; j++ {
				if lines[j].Type == DiffLineContext {
					hunk.Lines = append(hunk.Lines, lines[j])
					hunk.OldCount++
					hunk.NewCount++
				}
			}
			hunk.OldStart = oldLine - len(hunk.Lines)
			hunk.NewStart = newLine - len(hunk.Lines)

			currentHunk = &hunk
		}

		if currentHunk != nil {
			currentHunk.Lines = append(currentHunk.Lines, line)
			switch line.Type {
			case DiffLineContext:
				currentHunk.OldCount++
				currentHunk.NewCount++
			case DiffLineAdd:
				currentHunk.NewCount++
			case DiffLineDelete:
				currentHunk.OldCount++
			}
		}

		// Update line counters
		switch line.Type {
		case DiffLineContext:
			oldLine++
			newLine++
		case DiffLineAdd:
			newLine++
		case DiffLineDelete:
			oldLine++
		}
	}

	if currentHunk != nil {
		hunks = append(hunks, *currentHunk)
	}

	return hunks
}

// FormatDiff formats a diff result as a string
func FormatDiff(result DiffResult, noColor bool) string {
	var sb strings.Builder

	// File header
	header := fmt.Sprintf("diff --pgit a/%s b/%s", result.Path, result.Path)
	if noColor {
		sb.WriteString(header + "\n")
	} else {
		sb.WriteString(styles.DiffFileHeader.Render(header) + "\n")
	}

	switch result.Status {
	case StatusNew:
		if noColor {
			sb.WriteString("new file\n")
		} else {
			sb.WriteString(styles.DiffAddLine.Render("new file") + "\n")
		}
	case StatusDeleted:
		if noColor {
			sb.WriteString("deleted file\n")
		} else {
			sb.WriteString(styles.DiffRemoveLine.Render("deleted file") + "\n")
		}
	}

	sb.WriteString(fmt.Sprintf("--- a/%s\n", result.Path))
	sb.WriteString(fmt.Sprintf("+++ b/%s\n", result.Path))

	// Hunks
	for _, hunk := range result.Hunks {
		// Hunk header
		header := fmt.Sprintf("@@ -%d,%d +%d,%d @@",
			hunk.OldStart, hunk.OldCount,
			hunk.NewStart, hunk.NewCount)
		if noColor {
			sb.WriteString(header + "\n")
		} else {
			sb.WriteString(styles.DiffHunkHeader.Render(header) + "\n")
		}

		// Lines
		for _, line := range hunk.Lines {
			var lineStr string

			switch line.Type {
			case DiffLineContext:
				lineStr = " " + line.Content
				if !noColor {
					lineStr = styles.DiffContextLine.Render(lineStr)
				}
			case DiffLineAdd:
				lineStr = "+" + line.Content
				if !noColor {
					lineStr = styles.DiffAddLine.Render(lineStr)
				}
			case DiffLineDelete:
				lineStr = "-" + line.Content
				if !noColor {
					lineStr = styles.DiffRemoveLine.Render(lineStr)
				}
			}

			sb.WriteString(lineStr + "\n")
		}
	}

	return sb.String()
}
