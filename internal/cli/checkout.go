package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/imgajeed76/pgit/v4/internal/repo"
	"github.com/imgajeed76/pgit/v4/internal/ui/styles"
	"github.com/imgajeed76/pgit/v4/internal/util"
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
  pgit checkout <commit> -- <path> # Restore file from specific commit

The '--' separates the commit from file paths, useful when restoring
files from HEAD without specifying a commit.

Warning: This will overwrite local changes!`,
		Args: cobra.ArbitraryArgs,
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

	// Parse args using cobra's dash detection
	// pgit checkout -- file.txt        -> dashAt=0, args=[file.txt]
	// pgit checkout HEAD -- file.txt   -> dashAt=1, args=[HEAD, file.txt]
	// pgit checkout HEAD file.txt      -> dashAt=-1, args=[HEAD, file.txt]
	dashAt := cmd.ArgsLenAtDash()

	var commitRef string
	var paths []string

	if dashAt == 0 {
		// "checkout -- <paths>" - restore from HEAD
		commitRef = "HEAD"
		paths = args
	} else if dashAt > 0 {
		// "checkout <commit> -- <paths>"
		commitRef = args[0]
		paths = args[dashAt:]
	} else if len(args) == 0 {
		return fmt.Errorf("nothing specified to checkout\n\nUsage:\n  pgit checkout <commit>           # Switch to commit\n  pgit checkout -- <file>          # Restore file from HEAD\n  pgit checkout <commit> -- <file> # Restore file from commit")
	} else if len(args) == 1 {
		// "checkout <commit>" - full checkout
		commitRef = args[0]
	} else {
		// "checkout <commit> <path>" - restore file from commit (no --)
		commitRef = args[0]
		paths = args[1:]
	}

	// Resolve commit reference (supports HEAD, HEAD~N, HEAD^, short IDs, etc.)
	commitID, err := resolveCommitRef(ctx, r, commitRef)
	if err != nil {
		return err
	}

	// If paths specified, only restore those files
	if len(paths) > 0 {
		for _, path := range paths {
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
	headID, _ := r.DB.GetHead(ctx)
	if headID != "" {
		currentTree, _ := r.DB.GetTreeAtCommit(ctx, headID)
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
