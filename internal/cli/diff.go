package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/imgajeed76/pgit/internal/repo"
	"github.com/spf13/cobra"
)

func newDiffCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "diff [path]",
		Short: "Show changes between commits, working tree, etc",
		Long: `Show changes between the working tree and the staging area,
or between the staging area and the last commit.

Without arguments, shows unstaged changes.
With --staged, shows changes staged for the next commit.`,
		RunE: runDiff,
	}

	cmd.Flags().Bool("staged", false, "Show staged changes")
	cmd.Flags().Bool("cached", false, "Alias for --staged")
	cmd.Flags().Bool("name-only", false, "Show only names of changed files")
	cmd.Flags().Bool("name-status", false, "Show names and status of changed files")
	cmd.Flags().Bool("no-color", false, "Disable colored output")

	return cmd
}

func runDiff(cmd *cobra.Command, args []string) error {
	staged, _ := cmd.Flags().GetBool("staged")
	cached, _ := cmd.Flags().GetBool("cached")
	nameOnly, _ := cmd.Flags().GetBool("name-only")
	nameStatus, _ := cmd.Flags().GetBool("name-status")
	noColor, _ := cmd.Flags().GetBool("no-color")

	if cached {
		staged = true
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

	opts := repo.DiffOptions{
		Staged:     staged,
		NameOnly:   nameOnly,
		NameStatus: nameStatus,
		NoColor:    noColor,
	}

	if len(args) > 0 {
		opts.Path = args[0]
	}

	results, err := r.Diff(ctx, opts)
	if err != nil {
		return err
	}

	for _, result := range results {
		if nameOnly {
			fmt.Println(result.Path)
		} else if nameStatus {
			fmt.Printf("%s\t%s\n", result.Status.Symbol(), result.Path)
		} else {
			fmt.Print(repo.FormatDiff(result, noColor))
		}
	}

	return nil
}
