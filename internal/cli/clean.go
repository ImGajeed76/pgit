package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/imgajeed76/pgit/v2/internal/repo"
	"github.com/imgajeed76/pgit/v2/internal/ui/styles"
	"github.com/spf13/cobra"
)

func newCleanCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "clean",
		Short: "Remove untracked files from the working tree",
		Long: `Remove untracked files from the working tree.

By default, shows what would be removed without actually deleting.
Use -f to actually remove the files.

Examples:
  pgit clean              # Show what would be removed (dry run)
  pgit clean -f           # Remove untracked files
  pgit clean -fd          # Remove untracked files and directories
  pgit clean -n           # Dry run (same as no flags)`,
		RunE: runClean,
	}

	cmd.Flags().BoolP("force", "f", false, "Actually remove files (required to delete)")
	cmd.Flags().BoolP("dry-run", "n", false, "Show what would be removed")
	cmd.Flags().BoolP("directories", "d", false, "Also remove untracked directories")

	return cmd
}

func runClean(cmd *cobra.Command, args []string) error {
	force, _ := cmd.Flags().GetBool("force")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	removeDirs, _ := cmd.Flags().GetBool("directories")

	// Default to dry run unless -f specified
	if !force {
		dryRun = true
	}

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

	// Get all tracked files
	tracked := make(map[string]bool)
	head, err := r.DB.GetHeadCommit(ctx)
	if err != nil {
		return err
	}
	if head != nil {
		tree, err := r.DB.GetTreeAtCommit(ctx, head.ID)
		if err != nil {
			return err
		}
		for _, blob := range tree {
			tracked[blob.Path] = true
			// Also mark all parent directories as "tracked" (so we don't delete them)
			dir := filepath.Dir(blob.Path)
			for dir != "." && dir != "" {
				tracked[dir] = true
				dir = filepath.Dir(dir)
			}
		}
	}

	// Also consider staged files as tracked
	idx, err := r.LoadIndex()
	if err != nil {
		return err
	}
	for _, entry := range idx.Entries {
		tracked[entry.Path] = true
		dir := filepath.Dir(entry.Path)
		for dir != "." && dir != "" {
			tracked[dir] = true
			dir = filepath.Dir(dir)
		}
	}

	// Find untracked files/directories
	var toRemove []string
	var dirsToRemove []string

	err = filepath.WalkDir(r.Root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil // Skip errors
		}

		// Skip .pgit directory
		if d.IsDir() && d.Name() == ".pgit" {
			return filepath.SkipDir
		}

		relPath, err := r.RelPath(path)
		if err != nil || relPath == "" || relPath == "." {
			return nil
		}

		if d.IsDir() {
			if !tracked[relPath] && removeDirs {
				// Check if directory is empty or only contains untracked files
				isEmpty := true
				_ = filepath.WalkDir(path, func(p string, d os.DirEntry, err error) error {
					if p == path {
						return nil
					}
					rel, _ := r.RelPath(p)
					if tracked[rel] {
						isEmpty = false
						return filepath.SkipAll
					}
					return nil
				})
				if isEmpty {
					dirsToRemove = append(dirsToRemove, relPath)
					return filepath.SkipDir
				}
			}
			return nil
		}

		// Regular file
		if !tracked[relPath] {
			toRemove = append(toRemove, relPath)
		}

		return nil
	})
	if err != nil {
		return err
	}

	if len(toRemove) == 0 && len(dirsToRemove) == 0 {
		fmt.Println("Nothing to clean - working directory is clean")
		return nil
	}

	if dryRun {
		fmt.Println(styles.Boldf("Would remove:"))
		for _, f := range toRemove {
			fmt.Printf("  %s\n", f)
		}
		for _, d := range dirsToRemove {
			fmt.Printf("  %s/\n", d)
		}
		fmt.Println()
		fmt.Println(styles.Mute("Use 'pgit clean -f' to actually remove these files"))
		return nil
	}

	// Actually remove
	removedCount := 0
	for _, f := range toRemove {
		absPath := r.AbsPath(f)
		if err := os.Remove(absPath); err != nil {
			fmt.Printf("  warning: could not remove %s: %v\n", f, err)
		} else {
			fmt.Printf("  Removing %s\n", f)
			removedCount++
		}
	}

	for _, d := range dirsToRemove {
		absPath := r.AbsPath(d)
		if err := os.RemoveAll(absPath); err != nil {
			fmt.Printf("  warning: could not remove %s/: %v\n", d, err)
		} else {
			fmt.Printf("  Removing %s/\n", d)
			removedCount++
		}
	}

	fmt.Printf("\nRemoved %d item(s)\n", removedCount)
	return nil
}
