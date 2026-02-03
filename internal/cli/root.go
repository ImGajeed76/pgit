package cli

import (
	"errors"
	"fmt"
	"os"

	"github.com/imgajeed76/pgit/v2/internal/ui/styles"
	"github.com/imgajeed76/pgit/v2/internal/util"
	"github.com/spf13/cobra"
)

var (
	// Version information (set at build time)
	Version   = "dev"
	CommitSHA = "unknown"
	BuildDate = "unknown"
)

var rootCmd = &cobra.Command{
	Use:   "pgit",
	Short: "A Git-like version control system backed by PostgreSQL",
	Long: `pgit is a version control CLI that uses PostgreSQL with pg-xpatch
for delta-compressed versioned file storage.

The PostgreSQL connection URL serves as the "remote" - no separate
authentication system needed. Local repositories use a Docker/Podman
container running pg-xpatch.

For more information, see: https://github.com/imgajeed76/pgit`,
	SilenceUsage:  true,
	SilenceErrors: true,
	Version:       Version,
}

func Execute() error {
	if err := rootCmd.Execute(); err != nil {
		// Check if it's a structured PgitError
		var pgitErr *util.PgitError
		if errors.As(err, &pgitErr) {
			fmt.Fprintln(os.Stderr, pgitErr.Format())
		} else {
			// Simple error - still format nicely
			fmt.Fprintln(os.Stderr, styles.ErrorMsg(err.Error()))
		}
		return err
	}
	return nil
}

func init() {
	// Global flags
	rootCmd.PersistentFlags().BoolP("verbose", "v", false, "Enable verbose output")
	rootCmd.PersistentFlags().Bool("no-color", false, "Disable colored output")

	// Version flag template to show more info
	rootCmd.SetVersionTemplate(fmt.Sprintf("pgit version %s\n  commit: %s\n  built:  %s\n", Version, CommitSHA, BuildDate))

	// Set up pre-run to handle global flags
	rootCmd.PersistentPreRun = func(cmd *cobra.Command, args []string) {
		noColor, _ := cmd.Flags().GetBool("no-color")
		if noColor {
			styles.SetNoColor(true)
		}
	}

	// Add all subcommands
	rootCmd.AddCommand(
		newVersionCmd(),
		newInitCmd(),
		newConfigCmd(),
		newDoctorCmd(),
		newLocalCmd(),
		newAddCmd(),
		newRmCmd(),
		newMvCmd(),
		newResetCmd(),
		newStatusCmd(),
		newCommitCmd(),
		newLogCmd(),
		newDiffCmd(),
		newShowCmd(),
		newCheckoutCmd(),
		newBlameCmd(),
		newRemoteCmd(),
		newPushCmd(),
		newPullCmd(),
		newResolveCmd(),
		newCloneCmd(),
		newImportCmd(),
		newSQLCmd(),
		newStatsCmd(),
		newSearchCmd(),
		newGrepCmd(),
		newCleanCmd(),
		newCompletionCmd(),
		newReposCmd(),
		newUpdateCmd(),
	)
}

// newGrepCmd creates an alias for search
func newGrepCmd() *cobra.Command {
	cmd := newSearchCmd()
	cmd.Use = "grep <pattern>"
	cmd.Short = "Search for a pattern in file contents (alias for search)"
	cmd.Hidden = false // Show in help as an alias
	return cmd
}

func newCompletionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "completion [bash|zsh|fish|powershell]",
		Short: "Generate shell completion scripts",
		Long: `Generate shell completion scripts for pgit.

To load completions:

Bash:
  $ source <(pgit completion bash)

  # To load completions for each session, execute once:
  # Linux:
  $ pgit completion bash > /etc/bash_completion.d/pgit
  # macOS:
  $ pgit completion bash > $(brew --prefix)/etc/bash_completion.d/pgit

Zsh:
  # If shell completion is not already enabled in your environment,
  # you will need to enable it. You can execute the following once:
  $ echo "autoload -U compinit; compinit" >> ~/.zshrc

  # To load completions for each session, execute once:
  $ pgit completion zsh > "${fpath[1]}/_pgit"

  # You will need to start a new shell for this setup to take effect.

Fish:
  $ pgit completion fish | source

  # To load completions for each session, execute once:
  $ pgit completion fish > ~/.config/fish/completions/pgit.fish

PowerShell:
  PS> pgit completion powershell | Out-String | Invoke-Expression

  # To load completions for every new session, run:
  PS> pgit completion powershell > pgit.ps1
  # and source this file from your PowerShell profile.
`,
		DisableFlagsInUseLine: true,
		ValidArgs:             []string{"bash", "zsh", "fish", "powershell"},
		Args:                  cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
		RunE: func(cmd *cobra.Command, args []string) error {
			switch args[0] {
			case "bash":
				return rootCmd.GenBashCompletion(os.Stdout)
			case "zsh":
				return rootCmd.GenZshCompletion(os.Stdout)
			case "fish":
				return rootCmd.GenFishCompletion(os.Stdout, true)
			case "powershell":
				return rootCmd.GenPowerShellCompletionWithDesc(os.Stdout)
			}
			return nil
		},
	}
	return cmd
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("pgit version %s\n", Version)
			fmt.Printf("  commit: %s\n", CommitSHA)
			fmt.Printf("  built:  %s\n", BuildDate)
		},
	}
}
