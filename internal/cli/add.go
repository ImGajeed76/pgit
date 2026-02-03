package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/imgajeed76/pgit/v2/internal/repo"
	"github.com/spf13/cobra"
)

func newAddCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add [path]...",
		Short: "Add file contents to the staging area",
		Long: `Add file contents to the staging area.

This command updates the index using the current content found in
the working tree, to prepare the content staged for the next commit.

Use "pgit add ." to add all changes in the current directory.
Use "pgit add -A" to add all changes including untracked files.`,
		RunE: runAdd,
	}

	cmd.Flags().BoolP("all", "A", false, "Add all changes (including untracked files)")
	cmd.Flags().BoolP("verbose", "v", false, "Be verbose")

	return cmd
}

func runAdd(cmd *cobra.Command, args []string) error {
	verbose, _ := cmd.Flags().GetBool("verbose")
	addAll, _ := cmd.Flags().GetBool("all")

	// If no args and no -A flag, show helpful message
	if len(args) == 0 && !addAll {
		fmt.Println("Nothing specified, nothing added.")
		fmt.Println()
		fmt.Println("Maybe you wanted to say 'pgit add .'?")
		fmt.Println()
		fmt.Println("Use 'pgit add <path>...' to add specific files")
		fmt.Println("Use 'pgit add .' to add all changes in current directory")
		fmt.Println("Use 'pgit add -A' to add all changes including untracked files")
		return nil
	}

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

	// Handle -A flag or "." special case
	if addAll || (len(args) == 1 && args[0] == ".") {
		count, err := r.StageAll(ctx)
		if err != nil {
			return err
		}
		if verbose {
			fmt.Printf("Added %d file(s) to staging area\n", count)
		} else if count > 0 {
			fmt.Printf("Added %d file(s)\n", count)
		}
		return nil
	}

	// Process each path
	addedCount := 0
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
				addedCount++
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
			addedCount++
		}
	}

	// Show summary if not verbose (verbose already shows per-file output)
	if !verbose && addedCount > 0 {
		fmt.Printf("Added %d file(s)\n", addedCount)
	}

	return nil
}
