package cli

import (
	"fmt"
	"strings"

	"github.com/imgajeed76/pgit/internal/config"
	"github.com/imgajeed76/pgit/internal/util"
	"github.com/spf13/cobra"
)

func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config <key> [value]",
		Short: "Get and set repository options",
		Long: `Get and set repository configuration options.

Examples:
  pgit config user.name              # Get value
  pgit config user.name "John Doe"   # Set value
  pgit config user.email "j@x.com"   # Set value
  pgit config --list                 # List all config`,
		RunE: runConfig,
	}

	cmd.Flags().BoolP("list", "l", false, "List all configuration")

	return cmd
}

func runConfig(cmd *cobra.Command, args []string) error {
	listAll, _ := cmd.Flags().GetBool("list")

	// Find repo root
	root, err := util.FindRepoRoot()
	if err != nil {
		return err
	}

	// Load config
	cfg, err := config.Load(root)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	if listAll {
		// List all config
		fmt.Printf("core.local_db=%s\n", cfg.Core.LocalDB)
		if cfg.User.Name != "" {
			fmt.Printf("user.name=%s\n", cfg.User.Name)
		}
		if cfg.User.Email != "" {
			fmt.Printf("user.email=%s\n", cfg.User.Email)
		}
		for name, remote := range cfg.Remotes {
			fmt.Printf("remote.%s.url=%s\n", name, remote.URL)
		}
		return nil
	}

	if len(args) == 0 {
		return fmt.Errorf("usage: pgit config <key> [value]")
	}

	key := strings.ToLower(args[0])

	// Get or set?
	if len(args) == 1 {
		// Get value
		value, err := getConfigValue(cfg, key)
		if err != nil {
			return err
		}
		fmt.Println(value)
		return nil
	}

	// Set value
	value := args[1]
	if err := setConfigValue(cfg, key, value); err != nil {
		return err
	}

	// Save config
	if err := cfg.Save(root); err != nil {
		return fmt.Errorf("failed to save config: %w", err)
	}

	return nil
}

func getConfigValue(cfg *config.Config, key string) (string, error) {
	switch key {
	case "user.name":
		return cfg.User.Name, nil
	case "user.email":
		return cfg.User.Email, nil
	case "core.local_db", "core.localdb":
		return cfg.Core.LocalDB, nil
	default:
		// Check for remote.*.url pattern
		if strings.HasPrefix(key, "remote.") && strings.HasSuffix(key, ".url") {
			parts := strings.Split(key, ".")
			if len(parts) == 3 {
				remoteName := parts[1]
				if remote, ok := cfg.Remotes[remoteName]; ok {
					return remote.URL, nil
				}
				return "", fmt.Errorf("remote '%s' not found", remoteName)
			}
		}
		return "", fmt.Errorf("unknown config key: %s", key)
	}
}

func setConfigValue(cfg *config.Config, key, value string) error {
	switch key {
	case "user.name":
		cfg.User.Name = value
	case "user.email":
		cfg.User.Email = value
	default:
		// Check for remote.*.url pattern
		if strings.HasPrefix(key, "remote.") && strings.HasSuffix(key, ".url") {
			parts := strings.Split(key, ".")
			if len(parts) == 3 {
				remoteName := parts[1]
				cfg.SetRemote(remoteName, value)
				return nil
			}
		}
		return fmt.Errorf("unknown or read-only config key: %s", key)
	}
	return nil
}
