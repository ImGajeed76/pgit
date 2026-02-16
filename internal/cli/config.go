package cli

import (
	"fmt"
	"strings"

	"github.com/imgajeed76/pgit/v3/internal/config"
	"github.com/imgajeed76/pgit/v3/internal/ui/styles"
	"github.com/imgajeed76/pgit/v3/internal/util"
	"github.com/spf13/cobra"
)

func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config <key> [value]",
		Short: "Get and set repository options",
		Long: `Get and set repository configuration options.

Use --global for system-wide settings that affect the container and defaults.
Without --global, settings are stored per-repository.

Examples:
  pgit config user.name              # Get repo value
  pgit config user.name "John Doe"   # Set repo value
  pgit config --list                 # List repo config

  pgit config --global --list              # List global config
  pgit config --global container.shm_size  # Get global value
  pgit config --global container.shm_size 512m  # Set global value

Global settings:
` + config.GenerateHelpText(),
		RunE: runConfig,
	}

	cmd.Flags().BoolP("list", "l", false, "List all configuration")
	cmd.Flags().BoolP("global", "g", false, "Use global (system-wide) config instead of repository config")

	return cmd
}

func runConfig(cmd *cobra.Command, args []string) error {
	listAll, _ := cmd.Flags().GetBool("list")
	global, _ := cmd.Flags().GetBool("global")

	// If no args and no flags, show all config (global + local if in repo)
	if len(args) == 0 && !listAll && !global {
		return showAllConfig()
	}

	if global {
		return runGlobalConfig(cmd, args, listAll)
	}

	return runRepoConfig(cmd, args, listAll)
}

func showAllConfig() error {
	// Load and show global config
	globalCfg, err := config.LoadGlobal()
	if err != nil {
		return fmt.Errorf("failed to load global config: %w", err)
	}

	fmt.Println(styles.Boldf("Global config:"))
	for _, key := range config.ListGlobalKeys() {
		if value, ok := globalCfg.GetValue(key); ok && value != "" {
			fmt.Printf("  %s = %s\n", key, value)
		}
	}

	// Try to load local config if in a repo
	root, err := util.FindRepoRoot()
	if err == nil {
		localCfg, err := config.Load(root)
		if err == nil {
			fmt.Println()
			fmt.Println(styles.Boldf("Local config:"))
			for _, key := range config.ListLocalKeys() {
				if value, ok := localCfg.GetValue(key); ok && value != "" {
					fmt.Printf("  %s = %s\n", key, value)
				}
			}
			// Show read-only fields
			fmt.Printf("  %s = %s %s\n", "core.local_db", localCfg.Core.LocalDB, styles.Mute("(read-only)"))
			// Show remotes
			for name, remote := range localCfg.Remotes {
				fmt.Printf("  remote.%s.url = %s\n", name, remote.URL)
			}
		}
	}

	return nil
}

func runGlobalConfig(cmd *cobra.Command, args []string, listAll bool) error {
	// Load global config
	cfg, err := config.LoadGlobal()
	if err != nil {
		return fmt.Errorf("failed to load global config: %w", err)
	}

	if listAll {
		// List all global config
		for _, key := range config.ListGlobalKeys() {
			if value, ok := cfg.GetValue(key); ok && value != "" {
				fmt.Printf("%s=%s\n", key, value)
			}
		}
		return nil
	}

	if len(args) == 0 {
		return fmt.Errorf("usage: pgit config --global <key> [value]")
	}

	key := strings.ToLower(args[0])

	// Get or set?
	if len(args) == 1 {
		// Get value
		value, ok := cfg.GetValue(key)
		if !ok {
			return fmt.Errorf("unknown global config key: %s\nValid keys: %s",
				key, strings.Join(config.ListGlobalKeys(), ", "))
		}
		fmt.Println(value)
		return nil
	}

	// Set value
	value := args[1]
	if err := cfg.SetValue(key, value); err != nil {
		return fmt.Errorf("invalid value for %s: %w", key, err)
	}

	// Save config
	if err := cfg.Save(); err != nil {
		return fmt.Errorf("failed to save global config: %w", err)
	}

	fmt.Printf("%s %s=%s\n", styles.Green("Set"), key, value)

	// Special hint for container settings
	if strings.HasPrefix(key, "container.") {
		fmt.Println(styles.Mute("Note: Restart container for changes to take effect:"))
		fmt.Println(styles.Mute("  pgit local destroy && pgit local start"))
	}

	return nil
}

func runRepoConfig(cmd *cobra.Command, args []string, listAll bool) error {
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

	fmt.Printf("%s %s=%s\n", styles.Green("Set"), key, value)
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
