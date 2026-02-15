package cli

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/imgajeed76/pgit/v2/internal/db"
	"github.com/imgajeed76/pgit/v2/internal/repo"
	"github.com/imgajeed76/pgit/v2/internal/ui/styles"
	"github.com/imgajeed76/pgit/v2/internal/util"
	"github.com/spf13/cobra"
)

func newShowCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show [commit] | [commit:path]",
		Short: "Show various types of objects",
		Long: `Show commit details, or file content at a specific commit.

Examples:
  pgit show              # Show HEAD commit
  pgit show abc123       # Show specific commit  
  pgit show abc123:file  # Show file content at commit`,
		RunE: runShow,
	}

	cmd.Flags().Bool("stat", false, "Show diffstat instead of full diff")
	cmd.Flags().Bool("no-patch", false, "Suppress diff output")

	return cmd
}

func runShow(cmd *cobra.Command, args []string) error {
	showStat, _ := cmd.Flags().GetBool("stat")
	noPatch, _ := cmd.Flags().GetBool("no-patch")

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

	// Parse argument
	arg := "HEAD"
	if len(args) > 0 {
		arg = args[0]
	}

	// Check for commit:path format
	if strings.Contains(arg, ":") {
		parts := strings.SplitN(arg, ":", 2)
		return showFileAtCommit(ctx, r, parts[0], parts[1])
	}

	// Show commit
	return showCommitDetails(ctx, r, arg, showStat, noPatch)
}

func showCommitDetails(ctx context.Context, r *repo.Repository, ref string, showStat, noPatch bool) error {
	commitID, err := resolveCommitRef(ctx, r, ref)
	if err != nil {
		return err
	}

	commit, err := r.DB.GetCommit(ctx, commitID)
	if err != nil {
		return err
	}
	if commit == nil {
		return util.ErrCommitNotFound
	}

	// Print commit header with proper styling
	fmt.Printf("commit %s\n", styles.Hash(commit.ID, false))
	fmt.Printf("Author: %s <%s>\n",
		styles.Author(commit.AuthorName),
		commit.AuthorEmail)
	fmt.Printf("Date:   %s\n",
		styles.Date(commit.AuthoredAt.Format("Mon Jan 2 15:04:05 2006 -0700")))
	if commit.CommitterName != commit.AuthorName || commit.CommitterEmail != commit.AuthorEmail {
		fmt.Printf("Committer: %s <%s>\n", commit.CommitterName, commit.CommitterEmail)
		fmt.Printf("CommitDate: %s\n", commit.CommittedAt.Format("Mon Jan 2 15:04:05 2006 -0700"))
	}
	fmt.Println()

	// Print message (indented)
	for _, line := range strings.Split(commit.Message, "\n") {
		fmt.Printf("    %s\n", line)
	}

	if noPatch {
		return nil
	}

	// Get changes in this commit
	blobs, err := r.DB.GetBlobsAtCommit(ctx, commitID)
	if err != nil {
		return err
	}

	if len(blobs) == 0 {
		return nil
	}

	fmt.Println()

	// Get parent commit's tree for comparison
	var parentBlobs map[string][]byte
	if commit.ParentID != nil {
		parentTree, err := r.DB.GetTreeAtCommit(ctx, *commit.ParentID)
		if err == nil {
			parentBlobs = make(map[string][]byte)
			for _, b := range parentTree {
				parentBlobs[b.Path] = b.Content
			}
		}
	}

	if showStat {
		// Show diffstat summary
		return showDiffstat(blobs, parentBlobs)
	}

	// Show full diffs
	for _, blob := range blobs {
		var oldContent, newContent string

		if parentBlobs != nil {
			if old, ok := parentBlobs[blob.Path]; ok {
				oldContent = string(old)
			}
		}

		if blob.Content != nil {
			newContent = string(blob.Content)
		}

		result := repo.DiffResult{
			Path:       blob.Path,
			OldContent: oldContent,
			NewContent: newContent,
		}

		if blob.ContentHash == nil {
			result.Status = repo.StatusDeleted
		} else if oldContent == "" {
			result.Status = repo.StatusNew
		} else {
			result.Status = repo.StatusModified
		}

		result.Hunks = repo.GenerateHunks(oldContent, newContent, 3)
		fmt.Print(repo.FormatDiff(result, styles.NoColor()))
	}

	return nil
}

func showDiffstat(blobs []*db.Blob, parentBlobs map[string][]byte) error {
	var totalInsertions, totalDeletions int

	for _, blob := range blobs {
		var oldLines, newLines int

		if parentBlobs != nil {
			if old, ok := parentBlobs[blob.Path]; ok {
				oldLines = countLines(string(old))
			}
		}

		if blob.Content != nil {
			newLines = countLines(string(blob.Content))
		}

		insertions := 0
		deletions := 0

		if oldLines == 0 && newLines > 0 {
			// New file
			insertions = newLines
			fmt.Printf(" %s | %d %s\n",
				styles.Green(blob.Path),
				insertions,
				styles.Green(strings.Repeat("+", min(insertions, 40))))
		} else if newLines == 0 && oldLines > 0 {
			// Deleted file
			deletions = oldLines
			fmt.Printf(" %s | %d %s\n",
				styles.Red(blob.Path),
				deletions,
				styles.Red(strings.Repeat("-", min(deletions, 40))))
		} else {
			// Modified - simplified count
			insertions = max(0, newLines-oldLines)
			deletions = max(0, oldLines-newLines)
			if insertions == 0 && deletions == 0 {
				insertions = 1 // At least mark as changed
			}
			bar := styles.Green(strings.Repeat("+", min(insertions, 20))) +
				styles.Red(strings.Repeat("-", min(deletions, 20)))
			fmt.Printf(" %s | %d %s\n", blob.Path, insertions+deletions, bar)
		}

		totalInsertions += insertions
		totalDeletions += deletions
	}

	fmt.Println()
	fmt.Printf(" %d file(s) changed, %s, %s\n",
		len(blobs),
		styles.Green(fmt.Sprintf("%d insertions(+)", totalInsertions)),
		styles.Red(fmt.Sprintf("%d deletions(-)", totalDeletions)))

	return nil
}

func countLines(s string) int {
	if s == "" {
		return 0
	}
	return strings.Count(s, "\n") + 1
}

func resolveCommitRef(ctx context.Context, r *repo.Repository, ref string) (string, error) {
	// Parse ancestor notation: HEAD~N, HEAD^, commit~N, commit^
	baseRef, ancestorCount := parseAncestorNotation(ref)

	// Resolve the base reference
	commitID, err := resolveBaseRef(ctx, r, baseRef)
	if err != nil {
		return "", err
	}

	// Walk back through ancestors if needed
	for i := 0; i < ancestorCount; i++ {
		commit, err := r.DB.GetCommit(ctx, commitID)
		if err != nil {
			return "", err
		}
		if commit == nil {
			return "", util.ErrCommitNotFound
		}
		if commit.ParentID == nil {
			return "", util.NewError("Cannot go back further").
				WithMessage(fmt.Sprintf("Commit %s has no parent (root commit)", util.ShortID(commitID))).
				WithSuggestion("Use a smaller ancestor number")
		}
		commitID = *commit.ParentID
	}

	return commitID, nil
}

// parseAncestorNotation parses ref~N, ref^, ref^^, etc.
// Returns the base reference and the number of ancestors to traverse
func parseAncestorNotation(ref string) (string, int) {
	// Handle ~N notation (e.g., HEAD~3)
	if idx := strings.LastIndex(ref, "~"); idx != -1 {
		base := ref[:idx]
		numStr := ref[idx+1:]

		// Default to 1 if no number after ~
		num := 1
		if numStr != "" {
			if n, err := strconv.Atoi(numStr); err == nil && n >= 0 {
				num = n
			}
		}
		return base, num
	}

	// Handle ^ notation (e.g., HEAD^, HEAD^^, HEAD^^^)
	if strings.HasSuffix(ref, "^") {
		count := 0
		base := ref
		for strings.HasSuffix(base, "^") {
			base = strings.TrimSuffix(base, "^")
			count++
		}
		return base, count
	}

	return ref, 0
}

// resolveBaseRef resolves a base reference (without ancestor notation) to a commit ID
func resolveBaseRef(ctx context.Context, r *repo.Repository, ref string) (string, error) {
	// Handle HEAD
	if ref == "HEAD" {
		head, err := r.DB.GetHeadCommit(ctx)
		if err != nil {
			return "", err
		}
		if head == nil {
			return "", util.ErrNoCommits
		}
		return head.ID, nil
	}

	// Normalize to uppercase for ULID matching
	refUpper := strings.ToUpper(ref)

	// Try exact match first
	commit, err := r.DB.GetCommit(ctx, refUpper)
	if err != nil {
		return "", err
	}
	if commit != nil {
		return commit.ID, nil
	}

	// Try partial match using SQL LIKE (much faster than loading all commits)
	commit, err = r.DB.FindCommitByPartialID(ctx, refUpper)
	if err != nil {
		return "", err // This includes "ambiguous reference" errors
	}
	if commit != nil {
		return commit.ID, nil
	}

	return "", util.ErrCommitNotFound
}

func showFileAtCommit(ctx context.Context, r *repo.Repository, ref, path string) error {
	commitID, err := resolveCommitRef(ctx, r, ref)
	if err != nil {
		return err
	}

	blob, err := r.DB.GetFileAtCommit(ctx, path, commitID)
	if err != nil {
		return err
	}
	if blob == nil {
		return util.ErrFileNotFound
	}

	fmt.Print(string(blob.Content))
	return nil
}
