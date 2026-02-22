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

func newMvCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mv <source> <destination>",
		Short: "Move or rename a file and stage the change",
		Long: `Move or rename a file, directory, or symlink and stage the change.

This is equivalent to:
  mv <source> <destination>
  pgit rm <source>
  pgit add <destination>

Examples:
  pgit mv old.txt new.txt          # Rename a file
  pgit mv file.txt subdir/         # Move to directory
  pgit mv src/ newsrc/             # Rename directory`,
		Args: cobra.ExactArgs(2),
		RunE: runMv,
	}

	cmd.Flags().BoolP("force", "f", false, "Force move even if destination exists")

	return cmd
}

func runMv(cmd *cobra.Command, args []string) error {
	force, _ := cmd.Flags().GetBool("force")

	source := args[0]
	dest := args[1]

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

	// Get absolute paths
	srcAbs, err := filepath.Abs(source)
	if err != nil {
		return err
	}
	destAbs, err := filepath.Abs(dest)
	if err != nil {
		return err
	}

	// Check source exists
	srcInfo, err := os.Stat(srcAbs)
	if os.IsNotExist(err) {
		return fmt.Errorf("bad source, source=%s, destination=%s", source, dest)
	}
	if err != nil {
		return err
	}

	// Check if destination is a directory
	destInfo, destErr := os.Stat(destAbs)
	if destErr == nil && destInfo.IsDir() {
		// Moving into a directory
		destAbs = filepath.Join(destAbs, filepath.Base(srcAbs))
	} else if destErr == nil && !force {
		return fmt.Errorf("destination '%s' already exists (use -f to overwrite)", dest)
	}

	// Get relative paths for pgit
	srcRel, err := r.RelPath(srcAbs)
	if err != nil {
		return fmt.Errorf("source %s: %w", source, err)
	}
	destRel, err := r.RelPath(destAbs)
	if err != nil {
		return fmt.Errorf("destination %s: %w", dest, err)
	}

	if srcInfo.IsDir() {
		// Move directory
		return moveDirectory(ctx, r, srcAbs, destAbs, srcRel, destRel)
	}

	// Move single file
	return moveFile(ctx, r, srcAbs, destAbs, srcRel, destRel)
}

func moveFile(ctx context.Context, r *repo.Repository, srcAbs, destAbs, srcRel, destRel string) error {
	// Ensure destination directory exists
	if err := os.MkdirAll(filepath.Dir(destAbs), 0755); err != nil {
		return err
	}

	// Move the file
	if err := os.Rename(srcAbs, destAbs); err != nil {
		return err
	}

	// Stage deletion of source
	if err := r.StageDelete(srcRel); err != nil {
		return err
	}

	// Stage addition of destination
	if err := r.StageFile(ctx, destRel); err != nil {
		return err
	}

	fmt.Printf("Renamed '%s' -> '%s'\n", srcRel, destRel)
	return nil
}

func moveDirectory(ctx context.Context, r *repo.Repository, srcAbs, destAbs, srcRel, destRel string) error {
	// Collect all files to move first
	type fileMove struct {
		srcAbs, destAbs, srcRel, destRel string
	}
	var moves []fileMove

	err := filepath.WalkDir(srcAbs, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}

		relToSrc, _ := filepath.Rel(srcAbs, path)
		newDestAbs := filepath.Join(destAbs, relToSrc)
		srcRelPath, _ := r.RelPath(path)
		destRelPath, _ := r.RelPath(newDestAbs)

		moves = append(moves, fileMove{
			srcAbs:  path,
			destAbs: newDestAbs,
			srcRel:  srcRelPath,
			destRel: destRelPath,
		})
		return nil
	})
	if err != nil {
		return err
	}

	// Actually move the directory
	if err := os.Rename(srcAbs, destAbs); err != nil {
		return err
	}

	// Stage all the changes
	for _, m := range moves {
		if err := r.StageDelete(m.srcRel); err != nil {
			return err
		}
		if err := r.StageFile(ctx, m.destRel); err != nil {
			return err
		}
	}

	fmt.Printf("Renamed '%s' -> '%s' (%d files)\n", srcRel, destRel, len(moves))
	return nil
}
