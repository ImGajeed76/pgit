package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/imgajeed76/pgit/internal/repo"
	"github.com/imgajeed76/pgit/internal/ui/styles"
	"github.com/spf13/cobra"
)

func newAddCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add <path>...",
		Short: "Add file contents to the staging area",
		Long: `Add file contents to the staging area.

This command updates the index using the current content found in
the working tree, to prepare the content staged for the next commit.

Use "pgit add ." to add all changes in the current directory.`,
		Args: cobra.MinimumNArgs(1),
		RunE: runAdd,
	}

	cmd.Flags().BoolP("all", "A", false, "Add all changes (including deletions)")
	cmd.Flags().BoolP("verbose", "v", false, "Be verbose")

	return cmd
}

func runAdd(cmd *cobra.Command, args []string) error {
	verbose, _ := cmd.Flags().GetBool("verbose")

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

	// Handle "." special case
	if len(args) == 1 && args[0] == "." {
		if err := r.StageAll(ctx); err != nil {
			return err
		}
		if verbose {
			fmt.Println("Added all changes to staging area")
		}
		return nil
	}

	// Process each path
	for _, path := range args {
		// Resolve to absolute path
		absPath, err := filepath.Abs(path)
		if err != nil {
			return err
		}

		// Check if path exists or was tracked
		info, statErr := os.Lstat(absPath)

		// Get relative path
		relPath, err := r.RelPath(absPath)
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}

		if statErr != nil && !os.IsNotExist(statErr) {
			return fmt.Errorf("%s: %w", path, statErr)
		}

		// If it's a directory, add all files in it
		if statErr == nil && info.IsDir() {
			err = filepath.WalkDir(absPath, func(p string, d os.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if d.IsDir() {
					return nil
				}
				rel, err := r.RelPath(p)
				if err != nil {
					return err
				}
				if verbose {
					fmt.Printf("add '%s'\n", rel)
				}
				return r.StageFile(ctx, rel)
			})
			if err != nil {
				return err
			}
		} else {
			// Single file
			if err := r.StageFile(ctx, relPath); err != nil {
				return fmt.Errorf("%s: %w", path, err)
			}
			if verbose {
				fmt.Printf("add '%s'\n", relPath)
			}
		}
	}

	return nil
}

func newResetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "reset [path]...",
		Short: "Unstage files from the staging area",
		Long: `Remove files from the staging area.

This does not modify the working directory, only the staging area.
Without arguments, unstages all files.`,
		RunE: runReset,
	}
}

func runReset(cmd *cobra.Command, args []string) error {
	r, err := repo.Open()
	if err != nil {
		return err
	}

	if len(args) == 0 {
		// Unstage all
		if err := r.UnstageAll(); err != nil {
			return err
		}
		fmt.Println("Unstaged all files")
		return nil
	}

	// Unstage specific files
	for _, path := range args {
		// Resolve path
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
