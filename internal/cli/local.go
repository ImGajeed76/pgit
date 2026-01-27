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

func runLocalStatus(cmd *cobra.Command, args []string) error {
	runtime := container.DetectRuntime()
	if runtime == container.RuntimeNone {
		return util.ErrNoContainerRuntime
	}

	fmt.Printf("Container runtime: %s\n", styles.Cyan(string(runtime)))
	fmt.Printf("Container name: %s\n", container.ContainerName)

	if !container.ContainerExists(runtime) {
		fmt.Printf("Status: %s\n", styles.Mute("not created"))
		return nil
	}

	if container.IsContainerRunning(runtime) {
		port, _ := container.GetContainerPort(runtime)
		fmt.Printf("Status: %s\n", styles.Successf("running"))
		fmt.Printf("Port: %d\n", port)
		fmt.Printf("Image: %s\n", container.DefaultImage)
	} else {
		fmt.Printf("Status: %s\n", styles.Warningf("stopped"))
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
