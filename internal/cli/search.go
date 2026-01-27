package cli

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/imgajeed76/pgit/internal/repo"
	"github.com/imgajeed76/pgit/internal/ui"
	"github.com/imgajeed76/pgit/internal/ui/styles"
	"github.com/imgajeed76/pgit/internal/util"
	"github.com/spf13/cobra"
)

func newSearchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "search <pattern>",
		Short: "Search for a pattern in file contents across history",
		Long: `Search for a regular expression pattern across all file versions
in the repository history.

This searches the actual file content stored in the database, not just
the current working directory.

Examples:
  pgit search "TODO"              # Find all TODOs
  pgit search "func.*Error"       # Regex search
  pgit search -i "fixme"          # Case-insensitive
  pgit search --path "*.go" "fmt" # Search only Go files`,
		Args: cobra.ExactArgs(1),
		RunE: runSearch,
	}

	cmd.Flags().BoolP("ignore-case", "i", false, "Case-insensitive search")
	cmd.Flags().StringP("path", "p", "", "Filter by file path pattern (glob)")
	cmd.Flags().IntP("limit", "n", 50, "Maximum number of results")
	cmd.Flags().Bool("all", false, "Search all versions (not just latest per file)")
	cmd.Flags().String("commit", "", "Search only at specific commit")

	return cmd
}

func runSearch(cmd *cobra.Command, args []string) error {
	pattern := args[0]
	ignoreCase, _ := cmd.Flags().GetBool("ignore-case")
	pathFilter, _ := cmd.Flags().GetString("path")
	limit, _ := cmd.Flags().GetInt("limit")
	searchAll, _ := cmd.Flags().GetBool("all")
	commitRef, _ := cmd.Flags().GetString("commit")

	// Compile regex
	if ignoreCase {
		pattern = "(?i)" + pattern
	}
	re, err := regexp.Compile(pattern)
	if err != nil {
		return fmt.Errorf("invalid pattern: %w", err)
	}

	r, err := repo.Open()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	if err := r.Connect(ctx); err != nil {
		return err
	}
	defer r.Close()

	// Determine which commit(s) to search
	var commitID string
	if commitRef != "" {
		commitID, err = resolveCommitRef(ctx, r, commitRef)
		if err != nil {
			return err
		}
	} else if !searchAll {
		// Default: search at HEAD
		head, err := r.DB.GetHeadCommit(ctx)
		if err != nil {
			return err
		}
		if head == nil {
			return util.ErrNoCommits
		}
		commitID = head.ID
	}

	spinner := ui.NewSpinner("Searching repository")
	spinner.Start()

	type searchResult struct {
		Path     string
		CommitID string
		LineNum  int
		Line     string
		MatchPos []int
	}

	var results []searchResult
	resultCount := 0

	if searchAll {
		// Search all blobs
		blobs, err := r.DB.SearchAllBlobs(ctx, pathFilter)
		spinner.Stop()
		if err != nil {
			return err
		}

		for _, blob := range blobs {
			if blob.Content == nil {
				continue
			}

			lines := strings.Split(string(blob.Content), "\n")
			for lineNum, line := range lines {
				if matches := re.FindStringIndex(line); matches != nil {
					results = append(results, searchResult{
						Path:     blob.Path,
						CommitID: blob.CommitID,
						LineNum:  lineNum + 1,
						Line:     line,
						MatchPos: matches,
					})
					resultCount++
					if resultCount >= limit {
						break
					}
				}
			}
			if resultCount >= limit {
				break
			}
		}
	} else {
		// Search at specific commit
		tree, err := r.DB.GetTreeAtCommit(ctx, commitID)
		spinner.Stop()
		if err != nil {
			return err
		}

		for _, blob := range tree {
			if blob.Content == nil {
				continue
			}

			// Path filter
			if pathFilter != "" {
				matched, _ := matchGlob(pathFilter, blob.Path)
				if !matched {
					continue
				}
			}

			lines := strings.Split(string(blob.Content), "\n")
			for lineNum, line := range lines {
				if matches := re.FindStringIndex(line); matches != nil {
					results = append(results, searchResult{
						Path:     blob.Path,
						CommitID: commitID,
						LineNum:  lineNum + 1,
						Line:     line,
						MatchPos: matches,
					})
					resultCount++
					if resultCount >= limit {
						break
					}
				}
			}
			if resultCount >= limit {
				break
			}
		}
	}

	if len(results) == 0 {
		fmt.Println("No matches found")
		return nil
	}

	// Print results
	currentPath := ""
	for _, res := range results {
		if res.Path != currentPath {
			if currentPath != "" {
				fmt.Println()
			}
			if searchAll {
				fmt.Printf("%s %s\n",
					styles.Cyan(res.Path),
					styles.Mute("("+util.ShortID(res.CommitID)+")"))
			} else {
				fmt.Println(styles.Cyan(res.Path))
			}
			currentPath = res.Path
		}

		// Highlight the match in the line
		line := res.Line
		if len(line) > 200 {
			// Truncate long lines around the match
			start := res.MatchPos[0] - 50
			if start < 0 {
				start = 0
			}
			end := res.MatchPos[1] + 50
			if end > len(line) {
				end = len(line)
			}
			line = line[start:end]
			if start > 0 {
				line = "..." + line
			}
			if end < len(res.Line) {
				line = line + "..."
			}
		}

		// Highlight match
		highlighted := highlightMatch(line, re)

		fmt.Printf("  %s: %s\n",
			styles.Mute(fmt.Sprintf("%4d", res.LineNum)),
			highlighted)
	}

	fmt.Println()
	if resultCount >= limit {
		fmt.Printf("%s (showing first %d, use --limit to see more)\n",
			styles.Mute(fmt.Sprintf("Found %d+ matches", resultCount)), limit)
	} else {
		fmt.Printf("%s\n", styles.Mute(fmt.Sprintf("Found %d matches", resultCount)))
	}

	return nil
}

// highlightMatch highlights regex matches in a line
func highlightMatch(line string, re *regexp.Regexp) string {
	matches := re.FindAllStringIndex(line, -1)
	if len(matches) == 0 {
		return line
	}

	var result strings.Builder
	lastEnd := 0
	for _, match := range matches {
		// Add text before match
		result.WriteString(line[lastEnd:match[0]])
		// Add highlighted match
		result.WriteString(styles.Yellow(line[match[0]:match[1]]))
		lastEnd = match[1]
	}
	// Add remaining text
	result.WriteString(line[lastEnd:])

	return result.String()
}

// matchGlob does simple glob matching
func matchGlob(pattern, name string) (bool, error) {
	// Simple implementation - supports * and ?
	pattern = strings.ReplaceAll(pattern, ".", "\\.")
	pattern = strings.ReplaceAll(pattern, "*", ".*")
	pattern = strings.ReplaceAll(pattern, "?", ".")
	pattern = "^" + pattern + "$"

	re, err := regexp.Compile(pattern)
	if err != nil {
		return false, err
	}
	return re.MatchString(name), nil
}
