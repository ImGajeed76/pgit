package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/imgajeed76/pgit/v2/internal/repo"
	"github.com/imgajeed76/pgit/v2/internal/ui/styles"
	"github.com/imgajeed76/pgit/v2/internal/util"
	"github.com/spf13/cobra"
)

func newCheckoutCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "checkout [commit] [--] [path...]",
		Short: "Restore working tree files",
		Long: `Restore working tree files from a commit.

Usage:
  pgit checkout <commit>           # Switch to commit (updates HEAD)
  pgit checkout <commit> <path>    # Restore file from commit
  pgit checkout -- <path>          # Restore file from HEAD (discard changes)

The '--' separates the commit from file paths, useful when restoring
files from HEAD without specifying a commit.

Warning: This will overwrite local changes!`,
		Args: cobra.MinimumNArgs(1),
		RunE: runCheckout,
	}

	cmd.Flags().BoolP("force", "f", false, "Force checkout, discarding local changes")

	return cmd
}

func runCheckout(cmd *cobra.Command, args []string) error {
	force, _ := cmd.Flags().GetBool("force")

	r, err := repo.Open()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Connect to database
	if err := r.Connect(ctx); err != nil {
		return err
	}
	defer r.Close()

	// Handle "checkout -- <path>" syntax (restore from HEAD)
	if args[0] == "--" {
		if len(args) < 2 {
			return fmt.Errorf("no files specified after '--'")
		}
		// Get HEAD commit
		head, err := r.DB.GetHeadCommit(ctx)
		if err != nil {
			return err
		}
		if head == nil {
			return util.ErrNoCommits
		}
		// Restore each file from HEAD
		for _, path := range args[1:] {
			if err := checkoutPath(ctx, r, head.ID, path, force); err != nil {
				return err
			}
		}
		return nil
	}

	// Parse commit reference
	ref := args[0]
	var commitID string

	if ref == "HEAD" {
		head, err := r.DB.GetHeadCommit(ctx)
		if err != nil {
			return err
		}
		if head == nil {
			return util.ErrNoCommits
		}
		commitID = head.ID
	} else {
		// Try exact match
		commit, err := r.DB.GetCommit(ctx, ref)
		if err != nil {
			return err
		}
		if commit != nil {
			commitID = commit.ID
		} else {
			// Try partial match (short IDs use last 7 chars, case-insensitive)
			commits, err := r.DB.GetAllCommits(ctx)
			if err != nil {
				return err
			}
			refUpper := strings.ToUpper(ref)
			for _, c := range commits {
				// Check suffix (short ID) or prefix (full ID partial)
				if strings.HasSuffix(c.ID, refUpper) || strings.HasPrefix(c.ID, refUpper) {
					commitID = c.ID
					break
				}
			}
			if commitID == "" {
				return util.ErrCommitNotFound
			}
		}
	}

	// If path specified, only restore that file/directory
	if len(args) > 1 {
		// Handle "checkout <commit> -- <path>" by skipping the "--"
		startIdx := 1
		if args[1] == "--" {
			startIdx = 2
		}
		for _, path := range args[startIdx:] {
			if err := checkoutPath(ctx, r, commitID, path, force); err != nil {
				return err
			}
		}
		return nil
	}

	// Full checkout
	return checkoutFull(ctx, r, commitID, force)
}

func checkoutPath(ctx context.Context, r *repo.Repository, commitID, path string, force bool) error {
	// Get file at commit
	blob, err := r.DB.GetFileAtCommit(ctx, path, commitID)
	if err != nil {
		return err
	}
	if blob == nil {
		return util.ErrFileNotFound
	}

	absPath := r.AbsPath(path)

	// Check for uncommitted changes
	if !force {
		if _, err := os.Stat(absPath); err == nil {
			// File exists, check if modified
			// Compare using BLAKE3 hash (hex format for comparison)
			currentHash, _ := util.HashFileBlake3Hex(absPath)
			blobHash := util.ContentHashToHex(blob.ContentHash)
			if blob.ContentHash != nil && currentHash != blobHash {
				return fmt.Errorf("uncommitted changes in %s (use -f to force)", path)
			}
		}
	}

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(absPath), 0755); err != nil {
		return err
	}

	// Write file
	if blob.IsSymlink && blob.SymlinkTarget != nil {
		// Remove existing file if any
		os.Remove(absPath)
		if err := os.Symlink(*blob.SymlinkTarget, absPath); err != nil {
			return err
		}
	} else {
		if err := os.WriteFile(absPath, blob.Content, os.FileMode(blob.Mode)); err != nil {
			return err
		}
	}

	fmt.Printf("Updated '%s'\n", path)
	return nil
}

func checkoutFull(ctx context.Context, r *repo.Repository, commitID string, force bool) error {
	// Check for uncommitted changes
	if !force {
		changes, err := r.GetWorkingTreeChanges(ctx)
		if err != nil {
			return err
		}
		if len(changes) > 0 {
			fmt.Println(styles.Errorf("Error: You have uncommitted changes"))
			fmt.Println()
			fmt.Println("Commit your changes or use -f to discard them:")
			fmt.Println("  pgit commit -m \"message\"")
			fmt.Println("  pgit checkout -f " + util.ShortID(commitID))
			return util.ErrUncommittedChanges
		}
	}

	// Get tree at commit
	tree, err := r.DB.GetTreeAtCommit(ctx, commitID)
	if err != nil {
		return err
	}

	// Track files to keep
	keepFiles := make(map[string]bool)

	// Restore all files
	for _, blob := range tree {
		keepFiles[blob.Path] = true

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

	// Remove files that shouldn't exist at this commit
	// Get current tree
	head, _ := r.DB.GetHeadCommit(ctx)
	if head != nil {
		currentTree, _ := r.DB.GetTreeAtCommit(ctx, head.ID)
		for _, blob := range currentTree {
			if !keepFiles[blob.Path] {
				absPath := r.AbsPath(blob.Path)
				os.Remove(absPath)
			}
		}
	}

	// Update HEAD
	if err := r.DB.SetHead(ctx, commitID); err != nil {
		return err
	}

	fmt.Printf("HEAD is now at %s\n", styles.Yellow(util.ShortID(commitID)))
	return nil
}
