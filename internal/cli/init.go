package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/imgajeed76/pgit/internal/repo"
	"github.com/imgajeed76/pgit/internal/ui/styles"
	"github.com/imgajeed76/pgit/internal/util"
	"github.com/spf13/cobra"
)

func newInitCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "init [path]",
		Short: "Initialize a new pgit repository",
		Long: `Create an empty pgit repository or reinitialize an existing one.

This command creates a .pgit directory with the necessary configuration
files. It also detects Docker or Podman for the local database container.`,
		Args: cobra.MaximumNArgs(1),
		RunE: runInit,
	}
}

func runInit(cmd *cobra.Command, args []string) error {
	path := ""
	if len(args) > 0 {
		path = args[0]
	}

	// Resolve path for display
	displayPath := path
	if displayPath == "" {
		var err error
		displayPath, err = os.Getwd()
		if err != nil {
			return err
		}
	} else {
		var err error
		displayPath, err = filepath.Abs(displayPath)
		if err != nil {
			return err
		}
	}

	// Initialize repository
	r, err := repo.Init(path)
	if err != nil {
		if err == util.ErrAlreadyInitialized {
			fmt.Println(styles.Warningf("Reinitialized existing pgit repository in %s", filepath.Join(displayPath, ".pgit")))
			return nil
		}
		if err == util.ErrNoContainerRuntime {
			fmt.Println(styles.Errorf("Error: No container runtime found"))
			fmt.Println()
			fmt.Println("pgit requires Docker or Podman to run the local PostgreSQL database.")
			fmt.Println()
			fmt.Println("Install Docker:")
			fmt.Println("  https://docs.docker.com/get-docker/")
			fmt.Println()
			fmt.Println("Or install Podman:")
			fmt.Println("  https://podman.io/getting-started/installation")
			return err
		}
		return err
	}

	fmt.Printf("Initialized empty pgit repository in %s\n", filepath.Join(displayPath, ".pgit"))
	fmt.Printf("  Container runtime: %s\n", styles.Cyan(string(r.Runtime)))
	fmt.Printf("  Local database: %s\n", styles.Cyan(r.Config.Core.LocalDB))

	// Check if user config is set
	if r.Config.GetUserName() == "" || r.Config.GetUserEmail() == "" {
		fmt.Println()
		fmt.Println(styles.Warningf("Warning: User identity not configured"))
		fmt.Println()
		fmt.Println("Set your identity with:")
		fmt.Println("  pgit config user.name \"Your Name\"")
		fmt.Println("  pgit config user.email \"your@email.com\"")
	}

	return nil
}
