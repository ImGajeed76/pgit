package cli

import (
	"fmt"

	"github.com/imgajeed76/pgit/internal/repo"
	"github.com/imgajeed76/pgit/internal/ui/styles"
	"github.com/imgajeed76/pgit/internal/util"
	"github.com/spf13/cobra"
)

func newRemoteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "remote",
		Short: "Manage remote repositories",
		Long: `Manage the set of remote repositories.

Remotes are PostgreSQL databases with pg-xpatch that can be used
for push/pull operations. The URL format is a standard PostgreSQL
connection string.`,
		RunE: runRemoteList,
	}

	cmd.AddCommand(
		newRemoteAddCmd(),
		newRemoteRemoveCmd(),
		newRemoteSetURLCmd(),
	)

	return cmd
}

func newRemoteAddCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "add <name> <url>",
		Short: "Add a new remote",
		Long: `Add a new remote repository.

The URL should be a PostgreSQL connection string, e.g.:
  postgres://user:pass@host:5432/dbname
  postgres://user@neon.tech/myrepo`,
		Args: cobra.ExactArgs(2),
		RunE: runRemoteAdd,
	}
}

func newRemoteRemoveCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "remove <name>",
		Aliases: []string{"rm"},
		Short:   "Remove a remote",
		Args:    cobra.ExactArgs(1),
		RunE:    runRemoteRemove,
	}
}

func newRemoteSetURLCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set-url <name> <url>",
		Short: "Change the URL of a remote",
		Args:  cobra.ExactArgs(2),
		RunE:  runRemoteSetURL,
	}
}

func runRemoteList(cmd *cobra.Command, args []string) error {
	r, err := repo.Open()
	if err != nil {
		return err
	}

	if len(r.Config.Remotes) == 0 {
		fmt.Println("No remotes configured")
		fmt.Println()
		fmt.Println("Add a remote with:")
		fmt.Println("  pgit remote add origin postgres://user@host/database")
		return nil
	}

	verbose, _ := cmd.Flags().GetBool("verbose")

	for name, remote := range r.Config.Remotes {
		if verbose {
			fmt.Printf("%s\t%s (fetch)\n", name, remote.URL)
			fmt.Printf("%s\t%s (push)\n", name, remote.URL)
		} else {
			fmt.Println(name)
		}
	}

	return nil
}

func runRemoteAdd(cmd *cobra.Command, args []string) error {
	name := args[0]
	url := args[1]

	r, err := repo.Open()
	if err != nil {
		return err
	}

	// Check if remote already exists
	if _, exists := r.Config.GetRemote(name); exists {
		return util.ErrRemoteExists
	}

	// Add remote
	r.Config.SetRemote(name, url)

	if err := r.SaveConfig(); err != nil {
		return err
	}

	fmt.Printf("Added remote '%s'\n", styles.Cyan(name))
	return nil
}

func runRemoteRemove(cmd *cobra.Command, args []string) error {
	name := args[0]

	r, err := repo.Open()
	if err != nil {
		return err
	}

	if !r.Config.RemoveRemote(name) {
		return util.ErrRemoteNotFound
	}

	if err := r.SaveConfig(); err != nil {
		return err
	}

	fmt.Printf("Removed remote '%s'\n", name)
	return nil
}

func runRemoteSetURL(cmd *cobra.Command, args []string) error {
	name := args[0]
	url := args[1]

	r, err := repo.Open()
	if err != nil {
		return err
	}

	if _, exists := r.Config.GetRemote(name); !exists {
		return util.ErrRemoteNotFound
	}

	r.Config.SetRemote(name, url)

	if err := r.SaveConfig(); err != nil {
		return err
	}

	fmt.Printf("Updated URL for '%s'\n", name)
	return nil
}
