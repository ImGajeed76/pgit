package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/imgajeed76/pgit/v4/internal/repo"
	"github.com/spf13/cobra"
)

func newRmCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rm <file>...",
		Short: "Remove files from the working tree and stage the deletion",
		Long: `Remove files from the working tree and stage the removal.

This command stages file deletions for the next commit. By default,
it also removes the files from the working directory.

Use --cached to only unstage/remove from tracking without deleting
the actual files.

Examples:
  pgit rm file.txt            # Delete file and stage removal
  pgit rm -r directory/       # Recursively remove directory
  pgit rm --cached file.txt   # Stop tracking but keep file`,
		Args: cobra.MinimumNArgs(1),
		RunE: runRm,
	}

	cmd.Flags().Bool("cached", false, "Only remove from staging/tracking, keep the file")
	cmd.Flags().BoolP("recursive", "r", false, "Recursively remove directories")
	cmd.Flags().BoolP("force", "f", false, "Force removal even with local modifications")

	return cmd
}

func runRm(cmd *cobra.Command, args []string) error {
	cached, _ := cmd.Flags().GetBool("cached")
	recursive, _ := cmd.Flags().GetBool("recursive")
	force, _ := cmd.Flags().GetBool("force")

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

	removedCount := 0

	for _, path := range args {
		absPath, err := filepath.Abs(path)
		if err != nil {
			return err
		}

		relPath, err := r.RelPath(absPath)
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}

		info, err := os.Stat(absPath)
		if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("%s: %w", path, err)
		}

		// Handle directory
		if info != nil && info.IsDir() {
			if !recursive {
				return fmt.Errorf("%s is a directory, use -r to remove recursively", path)
			}

			// Walk directory and remove all files
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

				if err := removeFile(ctx, r, rel, p, cached, force); err != nil {
					return err
				}
				removedCount++
				return nil
			})
			if err != nil {
				return err
			}

			// Remove the directory itself if not cached
			if !cached {
				os.RemoveAll(absPath)
			}
		} else {
			// Single file
			if err := removeFile(ctx, r, relPath, absPath, cached, force); err != nil {
				return err
			}
			removedCount++
		}
	}

	fmt.Printf("rm '%d file(s)'\n", removedCount)
	return nil
}

func removeFile(ctx context.Context, r *repo.Repository, relPath, absPath string, cached, force bool) error {
	// Check if file is tracked
	headID, err := r.DB.GetHead(ctx)
	if err != nil {
		return err
	}

	var isTracked bool
	if headID != "" {
		blob, _ := r.DB.GetFileAtCommit(ctx, relPath, headID)
		isTracked = blob != nil
	}

	// Check if file exists
	_, statErr := os.Stat(absPath)
	fileExists := statErr == nil

	if !isTracked && !fileExists {
		return fmt.Errorf("'%s' did not match any files", relPath)
	}

	// If not forcing, check for local modifications
	if !force && isTracked && fileExists {
		// Compare with HEAD version
		blob, _ := r.DB.GetFileAtCommit(ctx, relPath, headID)
		if blob != nil {
			currentContent, err := os.ReadFile(absPath)
			if err == nil && string(currentContent) != string(blob.Content) {
				return fmt.Errorf("'%s' has local modifications (use -f to force)", relPath)
			}
		}
	}

	// Stage the deletion
	if err := r.StageDelete(relPath); err != nil {
		return err
	}

	// Delete the actual file unless --cached
	if !cached && fileExists {
		if err := os.Remove(absPath); err != nil {
			return err
		}
	}

	return nil
}
