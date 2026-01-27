package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/imgajeed76/pgit/internal/config"
	"github.com/imgajeed76/pgit/internal/db"
	"github.com/imgajeed76/pgit/internal/repo"
	"github.com/imgajeed76/pgit/internal/ui/styles"
	"github.com/imgajeed76/pgit/internal/util"
	"github.com/spf13/cobra"
)

func newStatusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show the working tree status",
		Long: `Displays paths that have differences between the staging area
and the current HEAD commit, paths that have differences between
the working tree and the staging area, and paths in the working
tree that are not tracked.`,
		RunE: runStatus,
	}

	cmd.Flags().BoolP("short", "s", false, "Give output in short format")
	cmd.Flags().Bool("json", false, "Output in JSON format")

	return cmd
}

func runStatus(cmd *cobra.Command, args []string) error {
	short, _ := cmd.Flags().GetBool("short")
	jsonOutput, _ := cmd.Flags().GetBool("json")

	r, err := repo.Open()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Connect to database
	if err := r.Connect(ctx); err != nil {
		return err
	}
	defer r.Close()

	// Get staged changes
	staged, err := r.GetStagedChanges(ctx)
	if err != nil {
		return err
	}

	// Get unstaged changes
	unstaged, err := r.GetUnstagedChanges(ctx)
	if err != nil {
		return err
	}

	// Get current HEAD
	head, err := r.DB.GetHeadCommit(ctx)
	if err != nil {
		return err
	}

	// Check for merge conflicts
	var conflicts []string
	mergeState, _ := config.LoadMergeState(r.Root)
	if mergeState != nil && mergeState.HasConflicts() {
		conflicts = mergeState.ConflictedFiles
	}

	if jsonOutput {
		return printJSONStatus(staged, unstaged, conflicts, head)
	}

	if short {
		return printShortStatus(staged, unstaged)
	}

	return printLongStatus(staged, unstaged, head)
}

// JSONStatus represents status output in JSON format
type JSONStatus struct {
	Branch    string           `json:"branch"`
	Head      *JSONCommitBrief `json:"head,omitempty"`
	Staged    []JSONFileChange `json:"staged"`
	Unstaged  []JSONFileChange `json:"unstaged"`
	Untracked []string         `json:"untracked"`
	Conflicts []string         `json:"conflicts,omitempty"`
}

type JSONCommitBrief struct {
	ID        string `json:"id"`
	ShortID   string `json:"short_id"`
	Message   string `json:"message"`
	Author    string `json:"author"`
	Timestamp string `json:"timestamp"`
}

type JSONFileChange struct {
	Path   string `json:"path"`
	Status string `json:"status"`
}

func printJSONStatus(staged, unstaged []repo.FileChange, conflicts []string, head *db.Commit) error {
	status := JSONStatus{
		Branch:    "main",
		Staged:    make([]JSONFileChange, 0),
		Unstaged:  make([]JSONFileChange, 0),
		Untracked: make([]string, 0),
		Conflicts: conflicts,
	}

	if head != nil {
		status.Head = &JSONCommitBrief{
			ID:        head.ID,
			ShortID:   util.ShortID(head.ID),
			Message:   firstLine(head.Message),
			Author:    head.AuthorName,
			Timestamp: head.CreatedAt.Format(time.RFC3339),
		}
	}

	for _, c := range staged {
		status.Staged = append(status.Staged, JSONFileChange{
			Path:   c.Path,
			Status: string(c.Status.Symbol()),
		})
	}

	for _, c := range unstaged {
		if c.Status == repo.StatusNew {
			status.Untracked = append(status.Untracked, c.Path)
		} else {
			status.Unstaged = append(status.Unstaged, JSONFileChange{
				Path:   c.Path,
				Status: string(c.Status.Symbol()),
			})
		}
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(status)
}

func printShortStatus(staged, unstaged []repo.FileChange) error {
	// Combine and dedupe
	seen := make(map[string]bool)

	for _, c := range staged {
		var stagedSymbol, unstagedSymbol string
		stagedSymbol = statusSymbol(c.Status, true)

		// Check if also modified in working tree
		for _, u := range unstaged {
			if u.Path == c.Path {
				unstagedSymbol = statusSymbol(u.Status, false)
				break
			}
		}

		if unstagedSymbol == "" {
			unstagedSymbol = " "
		}

		fmt.Printf("%s%s %s\n", stagedSymbol, unstagedSymbol, c.Path)
		seen[c.Path] = true
	}

	for _, c := range unstaged {
		if seen[c.Path] {
			continue
		}
		fmt.Printf(" %s %s\n", statusSymbol(c.Status, false), c.Path)
	}

	return nil
}

// statusSymbol returns a colored symbol for the status
func statusSymbol(status repo.ChangeStatus, staged bool) string {
	var symbol string
	var style func(string) string

	switch status {
	case repo.StatusNew:
		if staged {
			symbol = "A"
			style = func(s string) string { return styles.Green(s) }
		} else {
			symbol = "?"
			style = func(s string) string { return styles.Mute(s) }
		}
	case repo.StatusModified:
		symbol = "M"
		if staged {
			style = func(s string) string { return styles.Green(s) }
		} else {
			style = func(s string) string { return styles.Red(s) }
		}
	case repo.StatusDeleted:
		symbol = "D"
		if staged {
			style = func(s string) string { return styles.Green(s) }
		} else {
			style = func(s string) string { return styles.Red(s) }
		}
	default:
		symbol = " "
		style = func(s string) string { return s }
	}

	return style(symbol)
}

func printLongStatus(staged, unstaged []repo.FileChange, head *db.Commit) error {
	// Branch info
	fmt.Printf("On branch %s\n", styles.Branch("main"))

	// Show HEAD info if exists
	if head != nil {
		fmt.Printf("HEAD: %s %s\n", styles.Hash(head.ID, true), styles.MutedMsg(relativeTime(head.CreatedAt)))
	} else {
		fmt.Println()
		fmt.Println("No commits yet")
	}

	// Check for merge in progress
	// Get repo root from current directory (hacky but works for status)
	root, _ := util.FindRepoRoot()
	if root != "" {
		mergeState, err := config.LoadMergeState(root)
		if err == nil && mergeState.HasConflicts() {
			fmt.Println()
			fmt.Println(styles.Warningf("You have unmerged paths."))
			fmt.Println(styles.MutedMsg("  (fix conflicts and run \"pgit resolve <file>\")"))
			fmt.Println()
			fmt.Println("Unmerged paths:")
			for _, f := range mergeState.ConflictedFiles {
				fmt.Printf("  %s  %s\n", styles.Red("C"), f)
			}
		}
	}

	// Count for summary
	stagedCount := len(staged)
	modifiedCount := 0
	untrackedCount := 0

	// Staged changes
	if len(staged) > 0 {
		fmt.Println()
		fmt.Println("Changes to be committed:")
		fmt.Println(styles.MutedMsg("  (use \"pgit reset <file>...\" to unstage)"))
		fmt.Println()

		for _, c := range staged {
			prefix := styles.StatusPrefix(c.Status.Symbol())
			fmt.Printf("  %s  %s\n", prefix, c.Path)
		}
	}

	// Unstaged changes
	if len(unstaged) > 0 {
		// Separate tracked and untracked
		var modified, untracked []repo.FileChange
		for _, c := range unstaged {
			if c.Status == repo.StatusNew {
				untracked = append(untracked, c)
			} else {
				modified = append(modified, c)
			}
		}
		modifiedCount = len(modified)
		untrackedCount = len(untracked)

		if len(modified) > 0 {
			fmt.Println()
			fmt.Println("Changes not staged for commit:")
			fmt.Println(styles.MutedMsg("  (use \"pgit add <file>...\" to update what will be committed)"))
			fmt.Println()

			for _, c := range modified {
				prefix := styles.StatusPrefix(c.Status.Symbol())
				fmt.Printf("  %s  %s\n", prefix, c.Path)
			}
		}

		if len(untracked) > 0 {
			fmt.Println()
			fmt.Println("Untracked files:")
			fmt.Println(styles.MutedMsg("  (use \"pgit add <file>...\" to include in what will be committed)"))
			fmt.Println()

			for _, c := range untracked {
				fmt.Printf("  %s  %s\n", styles.StatusPrefix("?"), c.Path)
			}
		}
	}

	// Summary line at bottom (per TUI guidelines)
	fmt.Println()
	if stagedCount == 0 && modifiedCount == 0 && untrackedCount == 0 {
		fmt.Println("nothing to commit, working tree clean")
	} else {
		parts := []string{}
		if stagedCount > 0 {
			parts = append(parts, fmt.Sprintf("%d staged", stagedCount))
		}
		if modifiedCount > 0 {
			parts = append(parts, fmt.Sprintf("%d modified", modifiedCount))
		}
		if untrackedCount > 0 {
			parts = append(parts, fmt.Sprintf("%d untracked", untrackedCount))
		}
		fmt.Println(strings.Join(parts, ", "))
	}

	return nil
}

// relativeTime formats a time as relative (e.g., "2 hours ago")
func relativeTime(t time.Time) string {
	now := time.Now()
	diff := now.Sub(t)

	switch {
	case diff < time.Minute:
		return "just now"
	case diff < time.Hour:
		m := int(diff.Minutes())
		if m == 1 {
			return "1 minute ago"
		}
		return fmt.Sprintf("%d minutes ago", m)
	case diff < 24*time.Hour:
		h := int(diff.Hours())
		if h == 1 {
			return "1 hour ago"
		}
		return fmt.Sprintf("%d hours ago", h)
	case diff < 7*24*time.Hour:
		d := int(diff.Hours() / 24)
		if d == 1 {
			return "1 day ago"
		}
		return fmt.Sprintf("%d days ago", d)
	case diff < 30*24*time.Hour:
		w := int(diff.Hours() / 24 / 7)
		if w == 1 {
			return "1 week ago"
		}
		return fmt.Sprintf("%d weeks ago", w)
	default:
		return t.Format("Jan 2, 2006")
	}
}

// Helper for util
func init() {
	_ = util.NewULID // ensure import
}
