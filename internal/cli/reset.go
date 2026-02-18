package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/imgajeed76/pgit/v3/internal/db"
	"github.com/imgajeed76/pgit/v3/internal/repo"
	"github.com/imgajeed76/pgit/v3/internal/ui/styles"
	"github.com/imgajeed76/pgit/v3/internal/util"
	"github.com/spf13/cobra"
)

func newResetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "reset [<commit>] [--] [<path>...]",
		Short: "Reset HEAD, index, and working tree",
		Long: `Reset current HEAD to the specified state.

Modes:
  --soft   Only move HEAD to <commit>. Index and working tree unchanged.
  --mixed  Move HEAD and reset index, but not working tree. (default)
  --hard   Move HEAD, reset index AND working tree. Discards all changes!

If paths are specified instead of a commit, reset those files in the
staging area (unstage them).

Examples:
  pgit reset                  # Unstage all files (same as pgit reset HEAD)
  pgit reset HEAD~1           # Move HEAD back one commit (mixed mode)
  pgit reset --soft HEAD~1    # Move HEAD only, keep staged changes
  pgit reset --hard HEAD~1    # Move HEAD and discard all changes
  pgit reset file.txt         # Unstage file.txt
  pgit reset HEAD -- file.txt # Unstage file.txt (explicit syntax)`,
		RunE: runReset,
	}

	cmd.Flags().Bool("soft", false, "Only move HEAD, don't touch index or working tree")
	cmd.Flags().Bool("mixed", false, "Reset index but not working tree (default)")
	cmd.Flags().Bool("hard", false, "Reset index and working tree (discard all changes)")

	return cmd
}

type resetMode int

const (
	resetMixed resetMode = iota // default: move HEAD, reset index
	resetSoft                   // only move HEAD
	resetHard                   // move HEAD, reset index, reset working tree
)

func runReset(cmd *cobra.Command, args []string) error {
	soft, _ := cmd.Flags().GetBool("soft")
	mixed, _ := cmd.Flags().GetBool("mixed")
	hard, _ := cmd.Flags().GetBool("hard")

	// Determine mode
	mode := resetMixed
	modeCount := 0
	if soft {
		mode = resetSoft
		modeCount++
	}
	if mixed {
		mode = resetMixed
		modeCount++
	}
	if hard {
		mode = resetHard
		modeCount++
	}
	if modeCount > 1 {
		return fmt.Errorf("only one of --soft, --mixed, --hard can be specified")
	}

	r, err := repo.Open()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	if err := r.Connect(ctx); err != nil {
		return err
	}
	defer r.Close()

	// Parse arguments: could be [commit] [--] [paths...]
	// Need to figure out if first arg is a commit or a path
	var targetCommit string
	var paths []string

	if len(args) == 0 {
		// No args: reset to HEAD (unstage all in mixed mode)
		headID, err := r.DB.GetHead(ctx)
		if err != nil {
			return err
		}
		if headID == "" {
			// No commits yet, just unstage
			return unstageAll(r)
		}
		targetCommit = headID
	} else {
		// Check if first arg looks like a commit ref
		commitID, refResult, resolveErr := tryResolveCommit(ctx, r, args[0])

		// Handle -- separator
		dashDashIdx := -1
		for i, arg := range args {
			if arg == "--" {
				dashDashIdx = i
				break
			}
		}

		if dashDashIdx >= 0 {
			// Explicit separator: before -- is commit, after is paths
			if dashDashIdx > 0 {
				commitID, refResult, resolveErr = tryResolveCommit(ctx, r, args[0])
				if refResult == commitRefInvalid {
					if resolveErr != nil {
						return formatAmbiguousResetError(ctx, r, resolveErr)
					}
					return util.NewError(fmt.Sprintf("Invalid commit reference: %s", args[0])).
						WithMessage("Cannot resolve this commit reference").
						WithSuggestion("pgit log --oneline  # View available commits")
				}
				if refResult == commitRefNotARef {
					return fmt.Errorf("invalid commit: %s", args[0])
				}
				targetCommit = commitID
			} else {
				// No commit before --, use HEAD
				headID, err := r.DB.GetHead(ctx)
				if err != nil {
					return err
				}
				if headID == "" {
					return util.ErrNoCommits
				}
				targetCommit = headID
			}
			paths = args[dashDashIdx+1:]
		} else if refResult == commitRefResolved {
			// First arg is a valid commit
			targetCommit = commitID
			paths = args[1:]
		} else if refResult == commitRefInvalid {
			// Looks like a commit ref but couldn't resolve (e.g., HEAD~99)
			if resolveErr != nil {
				return formatAmbiguousResetError(ctx, r, resolveErr)
			}
			return util.NewError(fmt.Sprintf("Invalid commit reference: %s", args[0])).
				WithMessage("This looks like a commit reference but cannot be resolved").
				WithCause("The commit may not exist or you're trying to go back too far in history").
				WithSuggestion("pgit log --oneline  # View available commits")
		} else {
			// First arg is not a commit, treat all as paths (unstage mode)
			headID, err := r.DB.GetHead(ctx)
			if err != nil {
				return err
			}
			if headID == "" {
				// No commits, can still unstage
				return unstageFiles(r, args)
			}
			targetCommit = headID
			paths = args
		}
	}

	// If paths specified, only unstage those files (ignore mode flags)
	if len(paths) > 0 {
		return unstageFiles(r, paths)
	}

	// Full reset to commit
	return resetToCommit(ctx, r, targetCommit, mode)
}

// commitRefResult represents the result of trying to resolve a commit reference
type commitRefResult int

const (
	commitRefNotARef  commitRefResult = iota // Doesn't look like a commit ref (probably a path)
	commitRefInvalid                         // Looks like a commit ref but invalid (e.g., HEAD~99)
	commitRefResolved                        // Successfully resolved to a commit
)

// tryResolveCommit attempts to resolve a string to a commit ID.
// Returns the commit ID, a result indicating what happened, and an optional
// error (non-nil only for actionable errors like ambiguous references).
func tryResolveCommit(ctx context.Context, r *repo.Repository, ref string) (string, commitRefResult, error) {
	// Check if this looks like a commit reference pattern
	looksLikeCommitRef := ref == "HEAD" ||
		strings.HasPrefix(ref, "HEAD~") ||
		strings.HasPrefix(ref, "HEAD^") ||
		(len(ref) >= 7 && isHexOrULID(ref)) || // Short commit IDs
		!strings.Contains(ref, "/") && !strings.Contains(ref, ".") // Doesn't look like a path

	// Special refs
	if ref == "HEAD" {
		headID, err := r.DB.GetHead(ctx)
		if err != nil || headID == "" {
			return "", commitRefInvalid, nil
		}
		return headID, commitRefResolved, nil
	}

	// HEAD~N syntax
	if strings.HasPrefix(ref, "HEAD~") {
		n := 1
		if len(ref) > 5 {
			_, _ = fmt.Sscanf(ref[5:], "%d", &n)
		}
		headID, err := r.DB.GetHead(ctx)
		if err != nil || headID == "" {
			return "", commitRefInvalid, nil
		}
		// Walk back n commits
		commits, err := r.DB.GetCommitLogFrom(ctx, headID, n+1)
		if err != nil || len(commits) <= n {
			// This looks like a commit ref but we can't go back that far
			return "", commitRefInvalid, nil
		}
		return commits[n].ID, commitRefResolved, nil
	}

	// HEAD^ syntax
	if strings.HasPrefix(ref, "HEAD^") {
		n := strings.Count(ref, "^")
		headID, err := r.DB.GetHead(ctx)
		if err != nil || headID == "" {
			return "", commitRefInvalid, nil
		}
		commits, err := r.DB.GetCommitLogFrom(ctx, headID, n+1)
		if err != nil || len(commits) <= n {
			return "", commitRefInvalid, nil
		}
		return commits[n].ID, commitRefResolved, nil
	}

	// Try exact match first
	commit, err := r.DB.GetCommit(ctx, ref)
	if err == nil && commit != nil {
		return commit.ID, commitRefResolved, nil
	}

	// Try partial match (short ID) using prefix/suffix search
	partialCommit, err := r.DB.FindCommitByPartialID(ctx, strings.ToUpper(ref))
	if err != nil {
		// Surface ambiguous commit errors to the caller
		var ambErr *db.AmbiguousCommitError
		if errors.As(err, &ambErr) {
			return "", commitRefInvalid, err
		}
	}
	if err == nil && partialCommit != nil {
		return partialCommit.ID, commitRefResolved, nil
	}

	// If it doesn't look like a path (no / or . extension), treat as invalid ref
	// This prevents treating typos like "nonexistent-ref" as filenames
	if looksLikeCommitRef {
		return "", commitRefInvalid, nil
	}

	// Check if a file/directory with this name exists - if so, treat as path
	absPath := r.AbsPath(ref)
	if _, err := os.Stat(absPath); err == nil {
		return "", commitRefNotARef, nil
	}

	// Doesn't exist as file and doesn't look like a path - likely a typo'd ref
	return "", commitRefInvalid, nil
}

// formatAmbiguousResetError formats an ambiguous commit error for the reset command.
// If the error is not an AmbiguousCommitError, it returns the error as-is.
func formatAmbiguousResetError(ctx context.Context, r *repo.Repository, err error) error {
	var ambErr *db.AmbiguousCommitError
	if errors.As(err, &ambErr) {
		return formatAmbiguousError(ctx, r, ambErr)
	}
	return err
}

// isHexOrULID checks if a string looks like a commit ID (hex or ULID chars)
func isHexOrULID(s string) bool {
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')) {
			return false
		}
	}
	return true
}

// unstageAll removes all files from staging area
func unstageAll(r *repo.Repository) error {
	if err := r.UnstageAll(); err != nil {
		return err
	}
	fmt.Println("Unstaged all files")
	return nil
}

// unstageFiles removes specific files from staging area
func unstageFiles(r *repo.Repository, paths []string) error {
	for _, path := range paths {
		absPath, err := filepath.Abs(path)
		if err != nil {
			return err
		}
		relPath, err := r.RelPath(absPath)
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}

		if err := r.UnstageFile(relPath); err != nil {
			fmt.Printf("%s: %s\n", path, styles.Warningf("not staged"))
		} else {
			fmt.Printf("Unstaged '%s'\n", relPath)
		}
	}
	return nil
}

// resetToCommit performs the actual reset operation
func resetToCommit(ctx context.Context, r *repo.Repository, commitID string, mode resetMode) error {
	// Get current HEAD for display
	currentHeadID, err := r.DB.GetHead(ctx)
	if err != nil {
		return err
	}

	// Verify target commit exists
	targetCommit, err := r.DB.GetCommit(ctx, commitID)
	if err != nil {
		return err
	}
	if targetCommit == nil {
		return util.ErrCommitNotFound
	}

	// Check if we're already at this commit
	if currentHeadID != "" && currentHeadID == commitID {
		// Still need to reset index/working tree depending on mode
		if mode == resetMixed || mode == resetHard {
			if err := r.UnstageAll(); err != nil {
				return err
			}
		}
		if mode == resetHard {
			if err := resetWorkingTree(ctx, r, commitID); err != nil {
				return err
			}
		}
		fmt.Printf("HEAD is now at %s %s\n",
			styles.Hash(commitID, true),
			firstLineOf(targetCommit.Message))
		return nil
	}

	switch mode {
	case resetSoft:
		// Only move HEAD
		if err := r.DB.SetHead(ctx, commitID); err != nil {
			return err
		}

	case resetMixed:
		// Move HEAD and clear staging area
		if err := r.DB.SetHead(ctx, commitID); err != nil {
			return err
		}
		if err := r.UnstageAll(); err != nil {
			return err
		}

	case resetHard:
		// Move HEAD, clear staging, and restore working tree
		if err := r.DB.SetHead(ctx, commitID); err != nil {
			return err
		}
		if err := r.UnstageAll(); err != nil {
			return err
		}
		if err := resetWorkingTree(ctx, r, commitID); err != nil {
			return err
		}
	}

	fmt.Printf("HEAD is now at %s %s\n",
		styles.Hash(commitID, true),
		firstLineOf(targetCommit.Message))

	return nil
}

// resetWorkingTree restores the working tree to match a commit
func resetWorkingTree(ctx context.Context, r *repo.Repository, commitID string) error {
	// Get the tree at target commit
	targetTree, err := r.DB.GetTreeAtCommit(ctx, commitID)
	if err != nil {
		return err
	}

	// Get current HEAD tree to know what files to delete
	currentHeadID, _ := r.DB.GetHead(ctx)
	var currentTree map[string]bool
	if currentHeadID != "" {
		blobs, _ := r.DB.GetTreeAtCommit(ctx, currentHeadID)
		currentTree = make(map[string]bool)
		for _, b := range blobs {
			currentTree[b.Path] = true
		}
	}

	// Track files in target tree
	targetFiles := make(map[string]bool)

	// Restore all files from target commit
	for _, blob := range targetTree {
		targetFiles[blob.Path] = true
		absPath := r.AbsPath(blob.Path)

		// Ensure directory exists
		if err := os.MkdirAll(filepath.Dir(absPath), 0755); err != nil {
			return err
		}

		// Write file
		if blob.IsSymlink && blob.SymlinkTarget != nil {
			os.Remove(absPath)
			if err := os.Symlink(*blob.SymlinkTarget, absPath); err != nil {
				return err
			}
		} else {
			if err := os.WriteFile(absPath, blob.Content, os.FileMode(blob.Mode)); err != nil {
				return err
			}
		}
	}

	// Remove files that exist in current tree but not in target
	for path := range currentTree {
		if !targetFiles[path] {
			absPath := r.AbsPath(path)
			os.Remove(absPath)
			// Try to remove empty parent directories
			removeEmptyParents(filepath.Dir(absPath), r.Root)
		}
	}

	return nil
}

// removeEmptyParents removes empty directories up to (but not including) root
func removeEmptyParents(dir, root string) {
	for dir != root && dir != "." && dir != "/" {
		entries, err := os.ReadDir(dir)
		if err != nil || len(entries) > 0 {
			break
		}
		os.Remove(dir)
		dir = filepath.Dir(dir)
	}
}

// firstLineOf returns the first line of a string
func firstLineOf(s string) string {
	if idx := strings.Index(s, "\n"); idx >= 0 {
		return s[:idx]
	}
	return s
}
