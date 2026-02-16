package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/imgajeed76/pgit/v3/internal/container"
	"github.com/imgajeed76/pgit/v3/internal/repo"
	"github.com/imgajeed76/pgit/v3/internal/ui/styles"
	"github.com/spf13/cobra"
)

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check system health and diagnose issues",
		Long: `Run diagnostics to check if pgit is properly configured.

This command checks:
  - Container runtime (Docker/Podman) availability
  - Local container status
  - Database connectivity
  - Repository configuration`,
		RunE: runDoctor,
	}
}

func runDoctor(cmd *cobra.Command, args []string) error {
	fmt.Println(styles.Boldf("pgit doctor"))
	fmt.Println()

	allOK := true

	// Check container runtime
	fmt.Print("Checking container runtime... ")
	runtime := container.DetectRuntime()
	if runtime == container.RuntimeNone {
		fmt.Println(styles.Errorf("NOT FOUND"))
		fmt.Println("  Install Docker or Podman to use pgit")
		allOK = false
	} else {
		version, _ := container.GetRuntimeVersion(runtime)
		fmt.Println(styles.Successf("OK") + fmt.Sprintf(" (%s %s)", runtime, version))
	}

	// Check container status
	fmt.Print("Checking local container... ")
	if runtime != container.RuntimeNone {
		if container.ContainerExists(runtime) {
			if container.IsContainerRunning(runtime) {
				port, _ := container.GetContainerPort(runtime)
				fmt.Println(styles.Successf("RUNNING") + fmt.Sprintf(" (port %d)", port))
			} else {
				fmt.Println(styles.Warningf("STOPPED"))
				fmt.Println("  Run 'pgit local start' to start the container")
			}
		} else {
			fmt.Println(styles.Mute("NOT CREATED"))
			fmt.Println("  Container will be created on first use")
		}
	} else {
		fmt.Println(styles.Mute("SKIPPED"))
	}

	// Check if we're in a repository
	fmt.Print("Checking repository... ")
	r, err := repo.Open()
	if err != nil {
		fmt.Println(styles.Mute("NOT IN REPO"))
		fmt.Println("  Run 'pgit init' to create a repository")
	} else {
		fmt.Println(styles.Successf("OK") + fmt.Sprintf(" (%s)", r.Root))

		// Check database connection
		fmt.Print("Checking database connection... ")
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := r.Connect(ctx); err != nil {
			fmt.Println(styles.Errorf("FAILED"))
			fmt.Printf("  Error: %v\n", err)
			allOK = false
		} else {
			fmt.Println(styles.Successf("OK"))
			r.Close()
		}

		// Check user config
		fmt.Print("Checking user identity... ")
		name := r.Config.GetUserName()
		email := r.Config.GetUserEmail()
		if name == "" || email == "" {
			fmt.Println(styles.Warningf("NOT SET"))
			if name == "" {
				fmt.Println("  Missing: user.name")
			}
			if email == "" {
				fmt.Println("  Missing: user.email")
			}
		} else {
			fmt.Println(styles.Successf("OK") + fmt.Sprintf(" (%s <%s>)", name, email))
		}

		// Check remotes
		fmt.Print("Checking remotes... ")
		if len(r.Config.Remotes) == 0 {
			fmt.Println(styles.Mute("NONE"))
		} else {
			fmt.Println(styles.Successf("%d configured", len(r.Config.Remotes)))
			for name := range r.Config.Remotes {
				fmt.Printf("  - %s\n", name)
			}
		}
	}

	fmt.Println()
	if allOK {
		fmt.Println(styles.Successf("All checks passed!"))
	} else {
		fmt.Println(styles.Warningf("Some issues were found. See above for details."))
	}

	return nil
}
