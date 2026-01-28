package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/imgajeed76/pgit/internal/repo"
	"github.com/imgajeed76/pgit/internal/ui/styles"
	"github.com/imgajeed76/pgit/internal/util"
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
		head, err := r.DB.GetHeadCommit(ctx)
		if err != nil {
			return err
		}
		if head == nil {
			// No commits yet, just unstage
			return unstageAll(r)
		}
		targetCommit = head.ID
	} else {
		// Check if first arg looks like a commit ref
		commitID, isCommit := tryResolveCommit(ctx, r, args[0])

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
				commitID, isCommit = tryResolveCommit(ctx, r, args[0])
				if !isCommit {
					return fmt.Errorf("invalid commit: %s", args[0])
				}
				targetCommit = commitID
			} else {
				// No commit before --, use HEAD
				head, err := r.DB.GetHeadCommit(ctx)
				if err != nil {
					return err
				}
				if head == nil {
					return util.ErrNoCommits
				}
				targetCommit = head.ID
			}
			paths = args[dashDashIdx+1:]
		} else if isCommit {
			// First arg is a commit
			targetCommit = commitID
			paths = args[1:]
		} else {
			// First arg is not a commit, treat all as paths (unstage mode)
			head, err := r.DB.GetHeadCommit(ctx)
			if err != nil {
				return err
			}
			if head == nil {
				// No commits, can still unstage
				return unstageFiles(r, args)
			}
			targetCommit = head.ID
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

// tryResolveCommit attempts to resolve a string to a commit ID
// Returns the commit ID and true if found, empty string and false if not
func tryResolveCommit(ctx context.Context, r *repo.Repository, ref string) (string, bool) {
	// Special refs
	if ref == "HEAD" {
		head, err := r.DB.GetHeadCommit(ctx)
		if err != nil || head == nil {
			return "", false
		}
		return head.ID, true
	}

	// HEAD~N syntax
	if strings.HasPrefix(ref, "HEAD~") {
		n := 1
		if len(ref) > 5 {
			fmt.Sscanf(ref[5:], "%d", &n)
		}
		head, err := r.DB.GetHeadCommit(ctx)
		if err != nil || head == nil {
			return "", false
		}
		// Walk back n commits
		commits, err := r.DB.GetCommitLogFrom(ctx, head.ID, n+1)
		if err != nil || len(commits) <= n {
			return "", false
		}
		return commits[n].ID, true
	}

	// Try exact match first
	commit, err := r.DB.GetCommit(ctx, ref)
	if err == nil && commit != nil {
		return commit.ID, true
	}

	// Try partial match (short ID)
	commits, err := r.DB.GetAllCommits(ctx)
	if err != nil {
		return "", false
	}

	refUpper := strings.ToUpper(ref)
	for _, c := range commits {
		// Match suffix (short ID uses last 7 chars) or prefix
		if strings.HasSuffix(c.ID, refUpper) || strings.HasPrefix(c.ID, refUpper) {
			return c.ID, true
		}
	}

	return "", false
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
	currentHead, err := r.DB.GetHeadCommit(ctx)
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
	if currentHead != nil && currentHead.ID == commitID {
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
	currentHead, _ := r.DB.GetHeadCommit(ctx)
	var currentTree map[string]bool
	if currentHead != nil {
		blobs, _ := r.DB.GetTreeAtCommit(ctx, currentHead.ID)
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
	if currentTree != nil {
		for path := range currentTree {
			if !targetFiles[path] {
				absPath := r.AbsPath(path)
				os.Remove(absPath)
				// Try to remove empty parent directories
				removeEmptyParents(filepath.Dir(absPath), r.Root)
			}
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
