package cli

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/imgajeed76/pgit/v4/internal/db"
	"github.com/imgajeed76/pgit/v4/internal/repo"
	"github.com/imgajeed76/pgit/v4/internal/ui/styles"
	"github.com/imgajeed76/pgit/v4/internal/util"
	"github.com/spf13/cobra"
	"golang.org/x/sync/errgroup"
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
	cmd.Flags().IntP("unified", "U", 3, "Number of lines of unified diff context")
	cmd.Flags().String("remote", "", "Show object from a remote database (e.g. 'origin')")

	return cmd
}

func runShow(cmd *cobra.Command, args []string) error {
	showStat, _ := cmd.Flags().GetBool("stat")
	noPatch, _ := cmd.Flags().GetBool("no-patch")
	contextLines, _ := cmd.Flags().GetInt("unified")

	remoteName, _ := cmd.Flags().GetString("remote")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	r, err := connectForCommand(ctx, remoteName)
	if err != nil {
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
	return showCommitDetails(ctx, r, arg, showStat, noPatch, contextLines)
}

func showCommitDetails(ctx context.Context, r *repo.Repository, ref string, showStat, noPatch bool, contextLines int) error {
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

	// Get parent content only for files changed in this commit.
	// Instead of materializing the entire tree (7k+ files), we fetch only the
	// specific files we need via scoped xpatch queries (group_id resolved via path_id JOIN).
	// Fetch parent content in parallel.
	// Each file's content lives in a delta compression group in xpatch, so parallel
	// fetches across files are safe — they hit independent delta chains.
	var parentBlobs map[string][]byte
	if commit.ParentID != nil {
		parentBlobs = make(map[string][]byte)
		var mu sync.Mutex
		g, gCtx := errgroup.WithContext(ctx)
		g.SetLimit(15)

		parentID := *commit.ParentID
		for _, blob := range blobs {
			path := blob.Path
			g.Go(func() error {
				parentBlob, err := r.DB.GetFileAtCommit(gCtx, path, parentID)
				if err == nil && parentBlob != nil {
					mu.Lock()
					parentBlobs[parentBlob.Path] = parentBlob.Content
					mu.Unlock()
				}
				return nil
			})
		}
		if err := g.Wait(); err != nil {
			return err
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

		result.Hunks = repo.GenerateHunks(oldContent, newContent, contextLines)
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

	if ancestorCount == 0 {
		return commitID, nil
	}

	// Use binary lifting on the commit graph table for O(log N) ancestry.
	// The graph table is a normal heap table with pre-computed power-of-2
	// ancestor pointers, so HEAD~5000 takes ~13 B-tree lookups instead of
	// 5000 xpatch decompressions.
	ancestorID, err := r.DB.GetAncestorID(ctx, commitID, ancestorCount)
	if err != nil {
		return "", util.NewError("Cannot go back further").
			WithMessage(fmt.Sprintf("Cannot resolve %s~%d: %v", baseRef, ancestorCount, err)).
			WithSuggestion("Use a smaller ancestor number")
	}

	return ancestorID, nil
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

// resolveBaseRef resolves a base reference (without ancestor notation) to a commit ID.
// Uses the heap-based pgit_commit_graph table for O(1) lookups instead of the xpatch
// pgit_commits table, avoiding costly delta chain decompression.
func resolveBaseRef(ctx context.Context, r *repo.Repository, ref string) (string, error) {
	// Handle HEAD
	if ref == "HEAD" {
		headID, err := r.DB.GetHead(ctx)
		if err != nil {
			return "", err
		}
		if headID == "" {
			return "", util.ErrNoCommits
		}
		return headID, nil
	}

	// Normalize to uppercase for ULID matching
	refUpper := strings.ToUpper(ref)

	// Try exact match on the graph table (heap B-tree, instant)
	exists, err := r.DB.CommitExistsInGraph(ctx, refUpper)
	if err != nil {
		return "", err
	}
	if exists {
		return refUpper, nil
	}

	// Try partial prefix match on the graph table (heap B-tree range scan)
	fullID, err := r.DB.FindCommitByPartialIDInGraph(ctx, refUpper)
	if err != nil {
		var ambErr *db.AmbiguousCommitError
		if errors.As(err, &ambErr) {
			return "", formatAmbiguousError(ctx, r, ambErr)
		}
		return "", err
	}
	if fullID != "" {
		return fullID, nil
	}

	// Fall back to suffix match on pgit_file_refs (normal table)
	commit, err := r.DB.FindCommitByPartialID(ctx, refUpper)
	if err != nil {
		var ambErr *db.AmbiguousCommitError
		if errors.As(err, &ambErr) {
			return "", formatAmbiguousError(ctx, r, ambErr)
		}
		return "", err
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

// formatAmbiguousError creates a rich PgitError listing the matching candidates.
func formatAmbiguousError(ctx context.Context, r *repo.Repository, ambErr *db.AmbiguousCommitError) error {
	// Build candidate list with commit metadata
	var lines []string
	for _, id := range ambErr.MatchIDs {
		c, err := r.DB.GetCommit(ctx, id)
		if err != nil || c == nil {
			lines = append(lines, util.ShortID(id))
			continue
		}
		// First line of commit message, truncated
		subject := strings.SplitN(c.Message, "\n", 2)[0]
		if len(subject) > 60 {
			subject = subject[:57] + "..."
		}
		lines = append(lines, fmt.Sprintf("%s  %s  %s",
			util.ShortID(id), c.AuthoredAt.Format("2006-01-02"), subject))
	}

	// Join with "\n  " so every line gets the same 2-space indent
	// that PgitError.Format() applies to the first line of Message.
	msg := strings.Join(lines, "\n  ")
	if len(ambErr.MatchIDs) >= 10 {
		msg += "\n  (showing first 10 matches)"
	}

	return util.NewError(fmt.Sprintf("ambiguous commit reference '%s' matches %d commits",
		strings.ToLower(ambErr.PartialID), len(ambErr.MatchIDs))).
		WithMessage(msg).
		WithSuggestion("Use more characters to narrow the match")
}
