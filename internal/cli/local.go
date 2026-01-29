package cli

import (
	"fmt"

	"github.com/imgajeed76/pgit/internal/container"
	"github.com/imgajeed76/pgit/internal/ui/styles"
	"github.com/imgajeed76/pgit/internal/util"
	"github.com/spf13/cobra"
)

func newLocalCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "local",
		Short: "Manage the local PostgreSQL container",
		Long: `Manage the local pg-xpatch PostgreSQL container.

The local container stores your repository data and is shared across
all pgit repositories on your machine. Each repository has its own
database within the container.`,
	}

	cmd.AddCommand(
		newLocalStatusCmd(),
		newLocalStartCmd(),
		newLocalStopCmd(),
		newLocalLogsCmd(),
		newLocalDestroyCmd(),
		newLocalMigrateCmd(),
	)

	return cmd
}

func newLocalStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show container status",
		RunE:  runLocalStatus,
	}
}

func newLocalStartCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start the local container",
		RunE:  runLocalStart,
	}
	cmd.Flags().IntP("port", "p", container.DefaultPort, "PostgreSQL port")
	return cmd
}

func newLocalStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop the local container",
		RunE:  runLocalStop,
	}
}

func newLocalLogsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "logs",
		Short: "Show container logs",
		RunE:  runLocalLogs,
	}
	cmd.Flags().IntP("tail", "n", 50, "Number of lines to show")
	return cmd
}

func newLocalDestroyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "destroy",
		Short: "Remove container and optionally delete all data",
		Long: `Remove the pgit-local container.

By default, the data volume is preserved so you can recreate the container
later without losing your repositories. Use --purge to also delete the data.`,
		RunE: runLocalDestroy,
	}
	cmd.Flags().Bool("purge", false, "Also delete the data volume (DESTROYS ALL DATA)")
	return cmd
}

func newLocalMigrateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "migrate",
		Short: "Migrate legacy container to persistent storage",
		Long: `Migrate data from a legacy container (using anonymous volume) to the new
persistent named volume storage.

This command:
1. Stops the current container
2. Copies all data to the new named volume
3. Recreates the container with persistent storage
4. Removes the old anonymous volume

Your data is preserved throughout the process.`,
		RunE: runLocalMigrate,
	}
}

func runLocalStatus(cmd *cobra.Command, args []string) error {
	runtime := container.DetectRuntime()
	if runtime == container.RuntimeNone {
		return util.ErrNoContainerRuntime
	}

	fmt.Printf("Container runtime: %s\n", styles.Cyan(string(runtime)))
	fmt.Printf("Container name: %s\n", container.ContainerName)

	if !container.ContainerExists(runtime) {
		fmt.Printf("Status: %s\n", styles.Mute("not created"))
	} else if container.IsContainerRunning(runtime) {
		port, _ := container.GetContainerPort(runtime)
		fmt.Printf("Status: %s\n", styles.Successf("running"))
		fmt.Printf("Port: %d\n", port)
		fmt.Printf("Image: %s\n", container.DefaultImage)
	} else {
		fmt.Printf("Status: %s\n", styles.Warningf("stopped"))
	}

	// Show volume info
	fmt.Printf("\nData volume: %s\n", container.VolumeName)
	if container.VolumeExists(runtime) {
		_, size, err := container.GetVolumeInfo(runtime)
		if err == nil && size != "" {
			fmt.Printf("Volume size: %s\n", size)
		}
		fmt.Printf("Volume status: %s\n", styles.Successf("exists"))
	} else {
		fmt.Printf("Volume status: %s\n", styles.Mute("not created"))
	}

	// Warn if container exists but doesn't use named volume (legacy container)
	if container.ContainerExists(runtime) && !container.ContainerHasNamedVolume(runtime) {
		fmt.Println()
		fmt.Println(styles.Warningf("Warning: Container uses anonymous volume (legacy)"))
		fmt.Println(styles.Mute("Data will be lost if container is removed."))
		fmt.Println()
		fmt.Println("Run 'pgit local migrate' to migrate to persistent storage.")
	}

	return nil
}

func runLocalStart(cmd *cobra.Command, args []string) error {
	runtime := container.DetectRuntime()
	if runtime == container.RuntimeNone {
		return util.ErrNoContainerRuntime
	}

	port, _ := cmd.Flags().GetInt("port")

	// Check if already running
	if container.IsContainerRunning(runtime) {
		existingPort, _ := container.GetContainerPort(runtime)
		fmt.Printf("Container already running on port %d\n", existingPort)
		return nil
	}

	// Check port availability
	if !container.IsPortAvailable(port) {
		suggestedPort := container.FindAvailablePort(port)
		fmt.Printf("Port %d is in use. ", port)
		fmt.Printf("Try: pgit local start --port %d\n", suggestedPort)
		return fmt.Errorf("port %d unavailable", port)
	}

	fmt.Printf("Starting %s container on port %d...\n", container.ContainerName, port)

	if err := container.StartContainer(runtime, port); err != nil {
		return fmt.Errorf("failed to start container: %w", err)
	}

	fmt.Print("Waiting for PostgreSQL to be ready...")
	if err := container.WaitForPostgres(runtime, 30); err != nil {
		fmt.Println(styles.Errorf(" FAILED"))
		return err
	}
	fmt.Println(styles.Successf(" OK"))

	fmt.Println(styles.Successf("Container started successfully"))
	return nil
}

func runLocalStop(cmd *cobra.Command, args []string) error {
	runtime := container.DetectRuntime()
	if runtime == container.RuntimeNone {
		return util.ErrNoContainerRuntime
	}

	if !container.ContainerExists(runtime) {
		fmt.Println("Container does not exist")
		return nil
	}

	if !container.IsContainerRunning(runtime) {
		fmt.Println("Container is not running")
		return nil
	}

	fmt.Print("Stopping container...")
	if err := container.StopContainer(runtime); err != nil {
		fmt.Println(styles.Errorf(" FAILED"))
		return err
	}
	fmt.Println(styles.Successf(" OK"))

	return nil
}

func runLocalLogs(cmd *cobra.Command, args []string) error {
	runtime := container.DetectRuntime()
	if runtime == container.RuntimeNone {
		return util.ErrNoContainerRuntime
	}

	if !container.ContainerExists(runtime) {
		return fmt.Errorf("container does not exist")
	}

	tail, _ := cmd.Flags().GetInt("tail")
	logs, err := container.GetContainerLogs(runtime, tail)
	if err != nil {
		return err
	}

	fmt.Print(logs)
	return nil
}

func runLocalDestroy(cmd *cobra.Command, args []string) error {
	runtime := container.DetectRuntime()
	if runtime == container.RuntimeNone {
		return util.ErrNoContainerRuntime
	}

	purge, _ := cmd.Flags().GetBool("purge")

	// Remove container if it exists
	if container.ContainerExists(runtime) {
		fmt.Print("Removing container...")
		if err := container.RemoveContainer(runtime); err != nil {
			fmt.Println(styles.Errorf(" FAILED"))
			return err
		}
		fmt.Println(styles.Successf(" OK"))
	} else {
		fmt.Println("Container does not exist")
	}

	// Optionally remove volume
	if purge {
		if container.VolumeExists(runtime) {
			fmt.Print("Removing data volume...")
			if err := container.RemoveVolume(runtime); err != nil {
				fmt.Println(styles.Errorf(" FAILED"))
				return err
			}
			fmt.Println(styles.Successf(" OK"))
			fmt.Println(styles.Warningf("All pgit data has been permanently deleted"))
		} else {
			fmt.Println("Data volume does not exist")
		}
	} else {
		if container.VolumeExists(runtime) {
			fmt.Println(styles.Mute("Data volume preserved. Use --purge to delete all data."))
		}
	}

	return nil
}

func runLocalMigrate(cmd *cobra.Command, args []string) error {
	runtime := container.DetectRuntime()
	if runtime == container.RuntimeNone {
		return util.ErrNoContainerRuntime
	}

	// Check if migration is needed
	if !container.ContainerExists(runtime) {
		fmt.Println("No container exists. Run 'pgit local start' to create one with persistent storage.")
		return nil
	}

	if container.ContainerHasNamedVolume(runtime) {
		fmt.Println(styles.Successf("Container already uses persistent storage. No migration needed."))
		return nil
	}

	// Check for anonymous volume
	anonVolume := container.GetContainerAnonymousVolume(runtime)
	if anonVolume == "" {
		return fmt.Errorf("could not find anonymous volume to migrate from")
	}

	fmt.Println("Migrating to persistent storage...")
	fmt.Printf("Source volume: %s\n", styles.Mute(anonVolume))
	fmt.Printf("Target volume: %s\n", styles.Cyan(container.VolumeName))
	fmt.Println()

	err := container.MigrateToNamedVolume(runtime, func(stage string) {
		fmt.Printf("  %s...\n", stage)
	})

	if err != nil {
		return fmt.Errorf("migration failed: %w", err)
	}

	fmt.Println()
	fmt.Println(styles.Successf("Migration complete!"))
	fmt.Println("Your data is now stored in a persistent named volume.")
	fmt.Println("It will survive container removal and recreation.")

	return nil
}
