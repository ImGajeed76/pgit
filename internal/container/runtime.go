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

// VolumeName is the named Docker volume for persistent PostgreSQL data
// Named volumes are used instead of bind mounts for cross-platform compatibility:
// - Work identically on Linux, macOS, and Windows
// - No UID/GID permission issues (Docker manages this)
// - No filesystem compatibility issues (NFS, NTFS, etc.)
// - Survive container removal (docker rm)
const VolumeName = "pgit-data"

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
	_, _ = fmt.Sscanf(parts[len(parts)-1], "%d", &port)
	return port, nil
}

// ContainerHasNamedVolume checks if the container uses the named pgit-data volume
func ContainerHasNamedVolume(runtime Runtime) bool {
	if runtime == RuntimeNone {
		return false
	}

	// Get mounts info from container
	cmd := exec.Command(string(runtime), "inspect", ContainerName,
		"--format", "{{range .Mounts}}{{.Name}}{{end}}")
	output, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.Contains(string(output), VolumeName)
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

	// Create and start new container with named volume for data persistence
	// Named volumes are cross-platform compatible (Linux, macOS, Windows)
	// and avoid UID/GID permission issues that plague bind mounts
	args := []string{
		"run", "-d",
		"--name", ContainerName,
		"-p", fmt.Sprintf("%d:5432", port),
		"-v", fmt.Sprintf("%s:/var/lib/postgresql/data", VolumeName),
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
		_ = exec.Command("sleep", "1").Run()
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

// VolumeExists checks if the pgit data volume exists
func VolumeExists(runtime Runtime) bool {
	if runtime == RuntimeNone {
		return false
	}

	cmd := exec.Command(string(runtime), "volume", "inspect", VolumeName)
	return cmd.Run() == nil
}

// GetVolumeInfo returns information about the pgit data volume
func GetVolumeInfo(runtime Runtime) (mountpoint string, size string, err error) {
	if runtime == RuntimeNone {
		return "", "", fmt.Errorf("no container runtime available")
	}

	// Get mountpoint
	cmd := exec.Command(string(runtime), "volume", "inspect", VolumeName, "--format", "{{.Mountpoint}}")
	output, err := cmd.Output()
	if err != nil {
		return "", "", fmt.Errorf("volume %s not found", VolumeName)
	}
	mountpoint = strings.TrimSpace(string(output))

	// Get size using du inside a container (cross-platform way)
	sizeCmd := exec.Command(string(runtime), "run", "--rm",
		"-v", fmt.Sprintf("%s:/data:ro", VolumeName),
		"alpine", "du", "-sh", "/data")
	sizeOutput, err := sizeCmd.Output()
	if err == nil {
		parts := strings.Fields(string(sizeOutput))
		if len(parts) > 0 {
			size = parts[0]
		}
	}

	return mountpoint, size, nil
}

// RemoveVolume removes the pgit data volume (WARNING: destroys all data!)
func RemoveVolume(runtime Runtime) error {
	if runtime == RuntimeNone {
		return fmt.Errorf("no container runtime available")
	}

	// Container must be removed first
	if ContainerExists(runtime) {
		return fmt.Errorf("cannot remove volume while container exists; run 'pgit local destroy' first")
	}

	cmd := exec.Command(string(runtime), "volume", "rm", VolumeName)
	return cmd.Run()
}

// GetContainerAnonymousVolume returns the anonymous volume ID used by the container
// for /var/lib/postgresql/data, or empty string if using named volume or not found
func GetContainerAnonymousVolume(runtime Runtime) string {
	if runtime == RuntimeNone {
		return ""
	}

	// Get all mounts and find the one for postgres data
	// Format: {{.Type}} {{.Name}} {{.Destination}}
	cmd := exec.Command(string(runtime), "inspect", ContainerName,
		"--format", "{{range .Mounts}}{{.Type}}|{{.Name}}|{{.Destination}}\n{{end}}")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}

	for _, line := range strings.Split(string(output), "\n") {
		parts := strings.Split(line, "|")
		if len(parts) != 3 {
			continue
		}
		mountType, name, dest := parts[0], parts[1], parts[2]
		// Look for volume mount to postgres data dir that's NOT our named volume
		if dest == "/var/lib/postgresql/data" && mountType == "volume" && name != VolumeName {
			return name
		}
	}
	return ""
}

// MigrateToNamedVolume migrates data from an anonymous volume to the named pgit-data volume
// This is used to upgrade legacy containers that used anonymous volumes
func MigrateToNamedVolume(runtime Runtime, progressFn func(stage string)) error {
	if runtime == RuntimeNone {
		return fmt.Errorf("no container runtime available")
	}

	// Check container exists and uses anonymous volume
	if !ContainerExists(runtime) {
		return fmt.Errorf("no container to migrate")
	}

	if ContainerHasNamedVolume(runtime) {
		return fmt.Errorf("container already uses named volume")
	}

	// Get the anonymous volume ID
	anonVolume := GetContainerAnonymousVolume(runtime)
	if anonVolume == "" {
		return fmt.Errorf("could not find anonymous volume to migrate from")
	}

	// Get current port for restarting later
	port := DefaultPort
	if IsContainerRunning(runtime) {
		if p, err := GetContainerPort(runtime); err == nil {
			port = p
		}
	}

	// Step 1: Stop the container
	if progressFn != nil {
		progressFn("Stopping container")
	}
	if err := StopContainer(runtime); err != nil {
		return fmt.Errorf("failed to stop container: %w", err)
	}

	// Step 2: Create the named volume if it doesn't exist
	if progressFn != nil {
		progressFn("Creating named volume")
	}
	if !VolumeExists(runtime) {
		createCmd := exec.Command(string(runtime), "volume", "create", VolumeName)
		if err := createCmd.Run(); err != nil {
			return fmt.Errorf("failed to create named volume: %w", err)
		}
	}

	// Step 3: Copy data from anonymous volume to named volume using a temporary container
	// We use --privileged because the postgres data directory has restricted permissions (700)
	// that prevent even root from reading across volume boundaries on some systems (SELinux, etc.)
	if progressFn != nil {
		progressFn("Copying data to named volume")
	}
	copyCmd := exec.Command(string(runtime), "run", "--rm",
		"--privileged",
		"-v", fmt.Sprintf("%s:/source:ro", anonVolume),
		"-v", fmt.Sprintf("%s:/dest", VolumeName),
		DefaultImage, "sh", "-c", "cp -a /source/. /dest/")
	if err := copyCmd.Run(); err != nil {
		return fmt.Errorf("failed to copy data: %w", err)
	}

	// Step 4: Remove the old container
	if progressFn != nil {
		progressFn("Removing old container")
	}
	rmCmd := exec.Command(string(runtime), "rm", ContainerName)
	if err := rmCmd.Run(); err != nil {
		return fmt.Errorf("failed to remove old container: %w", err)
	}

	// Step 5: Create new container with named volume
	if progressFn != nil {
		progressFn("Creating new container with named volume")
	}
	if err := StartContainer(runtime, port); err != nil {
		return fmt.Errorf("failed to start new container: %w", err)
	}

	// Step 6: Wait for postgres to be ready
	if progressFn != nil {
		progressFn("Waiting for PostgreSQL")
	}
	if err := WaitForPostgres(runtime, 30); err != nil {
		return fmt.Errorf("PostgreSQL failed to start: %w", err)
	}

	// Step 7: Remove the old anonymous volume
	if progressFn != nil {
		progressFn("Cleaning up old volume")
	}
	cleanupCmd := exec.Command(string(runtime), "volume", "rm", anonVolume)
	// Don't fail if cleanup fails - data is already migrated
	_ = cleanupCmd.Run()

	return nil
}
