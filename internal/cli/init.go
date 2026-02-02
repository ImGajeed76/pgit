package cli

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/imgajeed76/pgit/v2/internal/config"
	"github.com/imgajeed76/pgit/v2/internal/repo"
	"github.com/imgajeed76/pgit/v2/internal/ui/styles"
	"github.com/imgajeed76/pgit/v2/internal/util"
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

	// Try to copy user config from pgit global or git config
	name, email, source := getUserConfigWithFallback()
	configUpdated := false
	if name != "" && r.Config.User.Name == "" {
		r.Config.User.Name = name
		configUpdated = true
	}
	if email != "" && r.Config.User.Email == "" {
		r.Config.User.Email = email
		configUpdated = true
	}
	if configUpdated {
		_ = r.Config.Save(r.Root)
	}

	// Show user config status
	if r.Config.GetUserName() != "" && r.Config.GetUserEmail() != "" {
		if source != "" {
			fmt.Printf("Using %s config: user.name %q, user.email %q\n", source, r.Config.GetUserName(), r.Config.GetUserEmail())
		}
	} else {
		fmt.Println()
		fmt.Println(styles.Mute("Tip: Set user config with:"))
		if r.Config.GetUserName() == "" {
			fmt.Println(styles.Mute("  pgit config user.name \"Your Name\""))
		}
		if r.Config.GetUserEmail() == "" {
			fmt.Println(styles.Mute("  pgit config user.email \"your@email.com\""))
		}
	}

	return nil
}

// getUserConfigWithFallback returns user name/email from pgit global config or git config.
// Returns the values and the source ("pgit global", "git", "pgit global and git", or "").
func getUserConfigWithFallback() (name, email, source string) {
	// 1. Try pgit global config first
	globalCfg, err := config.LoadGlobal()
	if err == nil {
		if globalCfg.User.Name != "" {
			name = globalCfg.User.Name
			source = "pgit global"
		}
		if globalCfg.User.Email != "" {
			email = globalCfg.User.Email
			if source == "" {
				source = "pgit global"
			}
		}
	}

	// 2. Fall back to git config for missing values
	if name == "" {
		if out, err := exec.Command("git", "config", "--global", "user.name").Output(); err == nil {
			name = strings.TrimSpace(string(out))
			if name != "" {
				if source == "" {
					source = "git"
				} else {
					source = "pgit global and git"
				}
			}
		}
	}
	if email == "" {
		if out, err := exec.Command("git", "config", "--global", "user.email").Output(); err == nil {
			email = strings.TrimSpace(string(out))
			if email != "" {
				if source == "" {
					source = "git"
				} else if source == "pgit global" {
					source = "pgit global and git"
				}
			}
		}
	}

	return name, email, source
}
