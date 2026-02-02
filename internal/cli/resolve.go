package cli

import (
	"fmt"
	"os"

	"github.com/imgajeed76/pgit/v2/internal/config"
	"github.com/imgajeed76/pgit/v2/internal/repo"
	"github.com/imgajeed76/pgit/v2/internal/ui/styles"
	"github.com/spf13/cobra"
)

func newResolveCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "resolve <file>...",
		Short: "Mark conflicts as resolved",
		Long: `Mark conflicted files as resolved after fixing merge conflicts.

After a pull with conflicts, use this command to mark each file as
resolved once you have edited it to fix the conflicts.

Example workflow:
  pgit pull                  # Conflicts detected
  # Edit conflicted files to remove conflict markers
  pgit resolve file1.txt     # Mark as resolved
  pgit resolve file2.txt     # Mark as resolved
  pgit add .                 # Stage all changes
  pgit commit -m "Merge"     # Complete the merge`,
		Args: cobra.MinimumNArgs(1),
		RunE: runResolve,
	}

	cmd.Flags().BoolP("all", "a", false, "Mark all conflicted files as resolved")

	return cmd
}

func runResolve(cmd *cobra.Command, args []string) error {
	resolveAll, _ := cmd.Flags().GetBool("all")

	r, err := repo.Open()
	if err != nil {
		return err
	}

	// Load merge state
	mergeState, err := config.LoadMergeState(r.Root)
	if err != nil {
		return err
	}

	if !mergeState.InProgress {
		fmt.Println("No merge in progress")
		return nil
	}

	if len(mergeState.ConflictedFiles) == 0 {
		fmt.Println("No conflicts to resolve")
		fmt.Println()
		fmt.Println("You can now commit to complete the merge:")
		fmt.Println("  pgit add .")
		fmt.Println("  pgit commit -m \"Merge complete\"")
		return nil
	}

	var filesToResolve []string
	if resolveAll {
		filesToResolve = mergeState.ConflictedFiles
	} else {
		filesToResolve = args
	}

	resolved := 0
	for _, path := range filesToResolve {
		// Check if file is in conflict list
		if !mergeState.IsConflicted(path) {
			fmt.Printf("%s is not in the conflict list\n", path)
			continue
		}

		// Check if file exists
		absPath := r.AbsPath(path)
		if _, err := os.Stat(absPath); os.IsNotExist(err) {
			fmt.Printf("%s does not exist\n", path)
			continue
		}

		// Check if file still has conflict markers
		hasMarkers, err := config.HasConflictMarkers(absPath)
		if err != nil {
			return fmt.Errorf("failed to check %s: %w", path, err)
		}

		if hasMarkers {
			fmt.Printf("%s %s still has conflict markers\n",
				styles.Warningf("Warning:"), path)
			fmt.Println("  Edit the file to remove <<<<<<< ======= >>>>>>> markers")
			continue
		}

		// Remove from conflict list
		mergeState.RemoveConflict(path)
		fmt.Printf("%s %s\n", styles.Successf("Resolved:"), path)
		resolved++
	}

	// Save updated merge state
	if err := mergeState.Save(r.Root); err != nil {
		return err
	}

	fmt.Println()
	if len(mergeState.ConflictedFiles) == 0 {
		// All conflicts resolved - clear merge state
		mergeState.InProgress = false
		_ = mergeState.Save(r.Root)

		fmt.Println(styles.Successf("All conflicts resolved!"))
		fmt.Println()
		fmt.Println("Complete the merge:")
		fmt.Println("  pgit add .")
		fmt.Println("  pgit commit -m \"Merge complete\"")
	} else {
		fmt.Printf("Remaining conflicts: %d\n", len(mergeState.ConflictedFiles))
		for _, f := range mergeState.ConflictedFiles {
			fmt.Printf("  %s %s\n", styles.Red("C"), f)
		}
	}

	return nil
}
