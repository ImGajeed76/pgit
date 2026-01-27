package container

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Runtime represents a container runtime (Docker or Podman)
type Runtime string

const (
	RuntimeDocker Runtime = "docker"
	RuntimePodman Runtime = "podman"
	RuntimeNone   Runtime = ""
)

// ContainerName is the name of the shared pgit local container
const ContainerName = "pgit-local"

// DefaultImage is the pg-xpatch Docker image
const DefaultImage = "ghcr.io/imgajeed76/pg-xpatch:latest"

// DefaultPort is the default PostgreSQL port for the local container
const DefaultPort = 5433

// DefaultPassword is the default password for the local PostgreSQL container
const DefaultPassword = "pgit"

// DetectRuntime finds an available container runtime
func DetectRuntime() Runtime {
	// Check environment variable override
	if env := os.Getenv("PGIT_CONTAINER_RUNTIME"); env != "" {
		switch strings.ToLower(env) {
		case "docker":
			if isRuntimeAvailable("docker") {
				return RuntimeDocker
			}
		case "podman":
			if isRuntimeAvailable("podman") {
				return RuntimePodman
			}
		}
	}

	// Auto-detect: prefer Docker, fallback to Podman
	if isRuntimeAvailable("docker") {
		return RuntimeDocker
	}
	if isRuntimeAvailable("podman") {
		return RuntimePodman
	}
	return RuntimeNone
}

// isRuntimeAvailable checks if a container runtime is installed and working
func isRuntimeAvailable(runtime string) bool {
	cmd := exec.Command(runtime, "version")
	return cmd.Run() == nil
}

// GetRuntimeVersion returns the version of the container runtime
func GetRuntimeVersion(runtime Runtime) (string, error) {
	if runtime == RuntimeNone {
		return "", fmt.Errorf("no container runtime")
	}

	cmd := exec.Command(string(runtime), "version", "--format", "{{.Server.Version}}")
	output, err := cmd.Output()
	if err != nil {
		// Try without format (podman compatibility)
		cmd = exec.Command(string(runtime), "version")
		output, err = cmd.Output()
		if err != nil {
			return "", err
		}
		// Parse first line
		lines := strings.Split(string(output), "\n")
		if len(lines) > 0 {
			return strings.TrimSpace(lines[0]), nil
		}
	}
	return strings.TrimSpace(string(output)), nil
}

// IsContainerRunning checks if the pgit-local container is running
func IsContainerRunning(runtime Runtime) bool {
	if runtime == RuntimeNone {
		return false
	}

	cmd := exec.Command(string(runtime), "inspect", "-f", "{{.State.Running}}", ContainerName)
	output, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(output)) == "true"
}

// ContainerExists checks if the pgit-local container exists (running or stopped)
func ContainerExists(runtime Runtime) bool {
	if runtime == RuntimeNone {
		return false
	}

	cmd := exec.Command(string(runtime), "inspect", ContainerName)
	return cmd.Run() == nil
}

// GetContainerPort returns the host port mapped to PostgreSQL (5432)
func GetContainerPort(runtime Runtime) (int, error) {
	if runtime == RuntimeNone {
		return 0, fmt.Errorf("no container runtime")
	}

	cmd := exec.Command(string(runtime), "port", ContainerName, "5432")
	output, err := cmd.Output()
	if err != nil {
		return 0, err
	}

	// Output format: "0.0.0.0:5433" or "[::]:5433"
	parts := strings.Split(strings.TrimSpace(string(output)), ":")
	if len(parts) < 2 {
		return 0, fmt.Errorf("unexpected port format: %s", output)
	}

	var port int
	fmt.Sscanf(parts[len(parts)-1], "%d", &port)
	return port, nil
}

// StartContainer starts the pgit-local container
func StartContainer(runtime Runtime, port int) error {
	if runtime == RuntimeNone {
		return fmt.Errorf("no container runtime available")
	}

	// Check if container exists but is stopped
	if ContainerExists(runtime) {
		if IsContainerRunning(runtime) {
			return nil // Already running
		}
		// Start existing container
		cmd := exec.Command(string(runtime), "start", ContainerName)
		return cmd.Run()
	}

	// Create and start new container
	args := []string{
		"run", "-d",
		"--name", ContainerName,
		"-p", fmt.Sprintf("%d:5432", port),
		"-e", "POSTGRES_PASSWORD=" + DefaultPassword,
		"-e", "POSTGRES_HOST_AUTH_METHOD=trust", // Allow local connections without password
		"--restart", "unless-stopped",
		DefaultImage,
	}

	cmd := exec.Command(string(runtime), args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// StopContainer stops the pgit-local container
func StopContainer(runtime Runtime) error {
	if runtime == RuntimeNone {
		return fmt.Errorf("no container runtime available")
	}

	if !ContainerExists(runtime) {
		return nil // Nothing to stop
	}

	cmd := exec.Command(string(runtime), "stop", ContainerName)
	return cmd.Run()
}

// RemoveContainer removes the pgit-local container
func RemoveContainer(runtime Runtime) error {
	if runtime == RuntimeNone {
		return fmt.Errorf("no container runtime available")
	}

	if !ContainerExists(runtime) {
		return nil
	}

	// Stop first if running
	if IsContainerRunning(runtime) {
		if err := StopContainer(runtime); err != nil {
			return err
		}
	}

	cmd := exec.Command(string(runtime), "rm", ContainerName)
	return cmd.Run()
}

// GetContainerLogs returns the container logs
func GetContainerLogs(runtime Runtime, tail int) (string, error) {
	if runtime == RuntimeNone {
		return "", fmt.Errorf("no container runtime available")
	}

	args := []string{"logs"}
	if tail > 0 {
		args = append(args, "--tail", fmt.Sprintf("%d", tail))
	}
	args = append(args, ContainerName)

	cmd := exec.Command(string(runtime), args...)
	output, err := cmd.CombinedOutput()
	return string(output), err
}

// WaitForPostgres waits for PostgreSQL to be ready in the container
func WaitForPostgres(runtime Runtime, maxAttempts int) error {
	if runtime == RuntimeNone {
		return fmt.Errorf("no container runtime available")
	}

	for i := 0; i < maxAttempts; i++ {
		cmd := exec.Command(string(runtime), "exec", ContainerName,
			"pg_isready", "-U", "postgres")
		if cmd.Run() == nil {
			return nil
		}
		// Wait a bit before retrying
		exec.Command("sleep", "1").Run()
	}
	return fmt.Errorf("PostgreSQL not ready after %d attempts", maxAttempts)
}

// LocalConnectionURL returns the connection URL for the local container
func LocalConnectionURL(port int, database string) string {
	return fmt.Sprintf("postgres://postgres:%s@localhost:%d/%s?sslmode=disable",
		DefaultPassword, port, database)
}

// EnsureDatabase creates the database if it doesn't exist
func EnsureDatabase(runtime Runtime, database string) error {
	if runtime == RuntimeNone {
		return fmt.Errorf("no container runtime available")
	}

	// Check if database exists
	checkCmd := exec.Command(string(runtime), "exec", ContainerName,
		"psql", "-U", "postgres", "-tAc",
		fmt.Sprintf("SELECT 1 FROM pg_database WHERE datname='%s'", database))
	output, _ := checkCmd.Output()
	if strings.TrimSpace(string(output)) == "1" {
		return nil // Database exists
	}

	// Create database
	createCmd := exec.Command(string(runtime), "exec", ContainerName,
		"psql", "-U", "postgres", "-c",
		fmt.Sprintf("CREATE DATABASE %s", database))
	return createCmd.Run()
}

// DropDatabase drops a database
func DropDatabase(runtime Runtime, database string) error {
	if runtime == RuntimeNone {
		return fmt.Errorf("no container runtime available")
	}

	cmd := exec.Command(string(runtime), "exec", ContainerName,
		"psql", "-U", "postgres", "-c",
		fmt.Sprintf("DROP DATABASE IF EXISTS %s", database))
	return cmd.Run()
}

// IsPortAvailable checks if a port is available
func IsPortAvailable(port int) bool {
	cmd := exec.Command("sh", "-c", fmt.Sprintf("lsof -i:%d", port))
	return cmd.Run() != nil // Port is available if lsof returns error
}

// FindAvailablePort finds an available port starting from the given port
func FindAvailablePort(startPort int) int {
	for port := startPort; port < startPort+100; port++ {
		if IsPortAvailable(port) {
			return port
		}
	}
	return startPort // Return original if none found
}
