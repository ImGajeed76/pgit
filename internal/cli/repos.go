package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/imgajeed76/pgit/v3/internal/config"
	"github.com/imgajeed76/pgit/v3/internal/container"
	"github.com/imgajeed76/pgit/v3/internal/db"
	"github.com/imgajeed76/pgit/v3/internal/ui/styles"
	"github.com/imgajeed76/pgit/v3/internal/util"
	"github.com/spf13/cobra"
)

func newReposCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "repos",
		Short: "Manage pgit repositories in the local container",
		Long: `List and manage all pgit repositories stored in the local container.

This command works from any directory.`,
		RunE: runReposList, // Default action is to list
	}

	cmd.Flags().Bool("json", false, "Output in JSON format")
	cmd.Flags().Bool("search", false, "Search filesystem for repos without stored paths")
	cmd.Flags().String("path", "", "Directory to search in (default: home directory)")
	cmd.Flags().Int("depth", 0, "Max search depth (default: 8 from home, 10 from --path)")

	cmd.AddCommand(
		newReposListCmd(),
		newReposCleanupCmd(),
		newReposDeleteCmd(),
	)

	return cmd
}

func newReposListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all pgit repositories",
		Long: `List all pgit repositories stored in the local container.

Shows database name, working directory path, commit count, and size.`,
		RunE: runReposList,
	}

	cmd.Flags().Bool("json", false, "Output in JSON format")
	cmd.Flags().Bool("search", false, "Search filesystem for repos without stored paths")
	cmd.Flags().String("path", "", "Directory to search in (default: home directory)")
	cmd.Flags().Int("depth", 0, "Max search depth (default: 8 from home, 10 from --path)")

	return cmd
}

func newReposCleanupCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cleanup",
		Short: "Remove databases for non-existent directories",
		Long: `Remove pgit databases whose working directories no longer exist.

This helps clean up orphaned databases from deleted projects.`,
		RunE: runReposCleanup,
	}

	cmd.Flags().Bool("dry-run", false, "Show what would be removed without actually removing")
	cmd.Flags().Bool("unknown", false, "Also remove databases with no stored path")

	return cmd
}

func newReposDeleteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete [path|database]",
		Short: "Delete a pgit repository",
		Long: `Delete a pgit repository by dropping its database and removing the .pgit folder.

This is a destructive operation that cannot be undone. Your project files
will NOT be deleted - only the pgit data (.pgit folder and database).

The argument is auto-detected:
  - Starts with "pgit_": treated as database name
  - Otherwise: treated as path

When deleting by database name and the path is not stored, use --search
to find the .pgit folder on the filesystem.

Examples:
  pgit repos delete --force              # Delete repo in current directory
  pgit repos delete --force ./my-project # Delete repo at path
  pgit repos delete --force pgit_abc123  # Delete by database name
  pgit repos delete --force --search pgit_abc123  # Search for .pgit folder`,
		Args: cobra.MaximumNArgs(1),
		RunE: runReposDelete,
	}

	cmd.Flags().Bool("force", false, "Required to confirm deletion")
	cmd.Flags().Bool("search", false, "Search filesystem for .pgit folder (when deleting by database name)")

	return cmd
}

type repoInfo struct {
	Database  string `json:"database"`
	Path      string `json:"path"`
	Status    string `json:"status"` // "ok", "orphaned", "unknown"
	Commits   int64  `json:"commits"`
	Size      string `json:"size"`
	SizeBytes int64  `json:"size_bytes"`
}

func runReposList(cmd *cobra.Command, args []string) error {
	jsonOutput, _ := cmd.Flags().GetBool("json")
	searchFS, _ := cmd.Flags().GetBool("search")
	searchPath, _ := cmd.Flags().GetString("path")
	searchDepth, _ := cmd.Flags().GetInt("depth")

	repos, err := gatherRepoInfo(searchFS, searchPath, searchDepth)
	if err != nil {
		return err
	}

	if len(repos) == 0 {
		fmt.Println("No pgit repositories found.")
		return nil
	}

	// JSON output
	if jsonOutput {
		fmt.Println("[")
		for i, r := range repos {
			comma := ","
			if i == len(repos)-1 {
				comma = ""
			}
			fmt.Printf(`  {"database": "%s", "path": "%s", "status": "%s", "commits": %d, "size": "%s", "size_bytes": %d}%s`+"\n",
				r.Database, r.Path, r.Status, r.Commits, r.Size, r.SizeBytes, comma)
		}
		fmt.Println("]")
		return nil
	}

	// Pretty output
	fmt.Printf("Found %d pgit repository(s):\n\n", len(repos))

	var orphanCount, unknownCount int
	for _, r := range repos {
		var statusStr string
		switch r.Status {
		case "ok":
			statusStr = styles.Green("OK")
		case "orphaned":
			statusStr = styles.Yellow("orphaned")
			orphanCount++
		case "unknown":
			statusStr = styles.Mute("unknown")
			unknownCount++
		}

		fmt.Printf("  %s [%s]\n", styles.Boldf("%s", r.Database), statusStr)
		if r.Path != "" {
			fmt.Printf("    Path:    %s\n", r.Path)
		} else {
			fmt.Printf("    Path:    %s\n", styles.Mute("(not stored)"))
		}
		fmt.Printf("    Commits: %d\n", r.Commits)
		fmt.Printf("    Size:    %s\n", r.Size)
		fmt.Println()
	}

	if orphanCount > 0 {
		fmt.Printf("%s %d database(s) have missing directories.\n",
			styles.Yellow("Note:"), orphanCount)
		fmt.Println("Run 'pgit repos cleanup' to remove them.")
	}
	if unknownCount > 0 {
		fmt.Printf("%s %d database(s) have no stored path. Run any pgit command in those repos to register them.\n",
			styles.Mute("Note:"), unknownCount)
	}

	return nil
}

func runReposCleanup(cmd *cobra.Command, args []string) error {
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	includeUnknown, _ := cmd.Flags().GetBool("unknown")

	repos, err := gatherRepoInfo(false, "", 0) // Don't search FS for cleanup
	if err != nil {
		return err
	}

	// Find repos to remove
	var toRemove []repoInfo
	for _, r := range repos {
		if r.Status == "orphaned" {
			toRemove = append(toRemove, r)
		} else if includeUnknown && r.Status == "unknown" {
			toRemove = append(toRemove, r)
		}
	}

	if len(toRemove) == 0 {
		if includeUnknown {
			fmt.Println("No orphaned or unknown databases found. Nothing to clean up.")
		} else {
			fmt.Println("No orphaned databases found. Nothing to clean up.")
		}
		return nil
	}

	if dryRun {
		fmt.Printf("Would remove %d database(s):\n\n", len(toRemove))
		for _, r := range toRemove {
			fmt.Printf("  %s [%s] (%s, %d commits)\n", r.Database, r.Status, r.Size, r.Commits)
		}
		fmt.Println()
		fmt.Println(styles.Mute("Run without --dry-run to actually remove them."))
		return nil
	}

	runtime := container.DetectRuntime()
	if runtime == container.RuntimeNone {
		return fmt.Errorf("no container runtime found")
	}

	fmt.Printf("Removing %d database(s)...\n\n", len(toRemove))

	for _, r := range toRemove {
		fmt.Printf("  Dropping %s... ", r.Database)
		err := container.DropDatabase(runtime, r.Database)
		if err != nil {
			fmt.Println(styles.Errorf("FAILED: %v", err))
		} else {
			fmt.Println(styles.Successf("OK"))
		}
	}

	fmt.Println()
	fmt.Println(styles.Successf("Cleanup complete."))

	return nil
}

func runReposDelete(cmd *cobra.Command, args []string) error {
	force, _ := cmd.Flags().GetBool("force")
	searchFS, _ := cmd.Flags().GetBool("search")

	if !force {
		return fmt.Errorf("this will permanently delete the repository\n\nUse --force to confirm")
	}

	var dbName string
	var repoPath string

	if len(args) == 0 {
		// No argument: use current directory
		root, err := util.FindRepoRoot()
		if err != nil {
			return fmt.Errorf("not in a pgit repository (use a path or database name as argument)")
		}
		repoPath = root

		// Load config to get database name
		cfg, err := config.Load(root)
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}
		dbName = cfg.Core.LocalDB
	} else {
		arg := args[0]

		if strings.HasPrefix(arg, "pgit_") {
			// Argument is a database name
			dbName = arg

			// Try to find the path from database metadata
			repoPath = getPathFromDatabase(dbName)

			// If not found and --search flag is set, search filesystem
			if repoPath == "" && searchFS {
				fmt.Println("Searching for repository on filesystem...")
				repoPath = findWorkingDir(dbName, "", 0)
			}
		} else {
			// Argument is a path
			absPath, err := filepath.Abs(arg)
			if err != nil {
				return fmt.Errorf("invalid path: %w", err)
			}

			// Check path exists
			if _, err := os.Stat(absPath); err != nil {
				return fmt.Errorf("path not found: %s", absPath)
			}

			// Check it's a pgit repo
			pgitDir := filepath.Join(absPath, ".pgit")
			if _, err := os.Stat(pgitDir); err != nil {
				return fmt.Errorf("not a pgit repository: %s", absPath)
			}

			repoPath = absPath

			// Load config to get database name
			cfg, err := config.Load(absPath)
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}
			dbName = cfg.Core.LocalDB
		}
	}

	// Print what we're about to delete
	fmt.Println("Deleting repository...")
	fmt.Printf("  Database: %s\n", styles.Cyan(dbName))
	if repoPath != "" {
		fmt.Printf("  Path: %s\n", repoPath)
	} else {
		fmt.Printf("  Path: %s\n", styles.Mute("(unknown)"))
	}
	fmt.Println()

	// Drop database
	runtime := container.DetectRuntime()
	if runtime == container.RuntimeNone {
		return fmt.Errorf("no container runtime found")
	}

	fmt.Printf("Dropping database... ")
	if err := container.DropDatabase(runtime, dbName); err != nil {
		fmt.Println(styles.Errorf("FAILED"))
		fmt.Printf("  %v\n", err)
	} else {
		fmt.Println(styles.Successf("OK"))
	}

	// Remove .pgit folder
	if repoPath != "" {
		pgitDir := filepath.Join(repoPath, ".pgit")
		if _, err := os.Stat(pgitDir); err == nil {
			fmt.Printf("Removing .pgit folder... ")
			if err := os.RemoveAll(pgitDir); err != nil {
				fmt.Println(styles.Errorf("FAILED"))
				fmt.Printf("  %v\n", err)
			} else {
				fmt.Println(styles.Successf("OK"))
			}
		}
	}

	fmt.Println()
	fmt.Println(styles.Successf("Repository deleted."))

	return nil
}

// getPathFromDatabase retrieves the stored path from a database's metadata
func getPathFromDatabase(dbName string) string {
	runtime := container.DetectRuntime()
	if runtime == container.RuntimeNone {
		return ""
	}

	if !container.IsContainerRunning(runtime) {
		return ""
	}

	port, err := container.GetContainerPort(runtime)
	if err != nil {
		return ""
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	connURL := container.LocalConnectionURL(port, dbName)

	// Use lightweight single connection instead of pool for quick metadata lookup
	repoDb, err := db.ConnectLite(ctx, connURL)
	if err != nil {
		return ""
	}
	defer repoDb.Close()

	return repoDb.GetRepoPath(ctx)
}

// gatherRepoInfo collects information about all pgit databases
func gatherRepoInfo(searchFS bool, searchPath string, searchDepth int) ([]repoInfo, error) {
	runtime := container.DetectRuntime()
	if runtime == container.RuntimeNone {
		return nil, fmt.Errorf("no container runtime found (docker or podman required)")
	}

	if !container.ContainerExists(runtime) {
		return nil, nil // No container = no repos
	}

	if !container.IsContainerRunning(runtime) {
		fmt.Println("Container is not running. Starting...")
		if err := container.StartContainer(runtime, container.DefaultPort); err != nil {
			return nil, fmt.Errorf("failed to start container: %w", err)
		}
		if err := container.WaitForPostgres(runtime, 30); err != nil {
			return nil, fmt.Errorf("container failed to start: %w", err)
		}
	}

	// List databases via psql in container
	cmd := exec.Command(string(runtime), "exec", container.ContainerName,
		"psql", "-U", "postgres", "-tAc",
		"SELECT datname FROM pg_database WHERE datname LIKE 'pgit_%' ORDER BY datname")
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to list databases: %w", err)
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) == 0 || (len(lines) == 1 && lines[0] == "") {
		return nil, nil
	}

	port, err := container.GetContainerPort(runtime)
	if err != nil {
		return nil, fmt.Errorf("failed to get container port: %w", err)
	}

	// Gather info for all databases in parallel
	repos := make([]repoInfo, len(lines))
	var wg sync.WaitGroup

	for i, dbName := range lines {
		dbName = strings.TrimSpace(dbName)
		if dbName == "" {
			continue
		}

		wg.Add(1)
		go func(idx int, name string) {
			defer wg.Done()
			repos[idx] = getRepoInfoForDB(name, port, searchFS, searchPath, searchDepth)
		}(i, dbName)
	}

	wg.Wait()

	// Filter out empty entries
	var result []repoInfo
	for _, r := range repos {
		if r.Database != "" {
			result = append(result, r)
		}
	}

	return result, nil
}

// getRepoInfoForDB gathers info for a single database
func getRepoInfoForDB(dbName string, port int, searchFS bool, searchPath string, searchDepth int) repoInfo {
	info := repoInfo{
		Database: dbName,
		Status:   "unknown",
		Size:     "N/A",
	}

	// Connect and get stats
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	connURL := container.LocalConnectionURL(port, dbName)
	repoDb, err := db.Connect(ctx, connURL)
	if err != nil {
		return info
	}
	defer repoDb.Close()

	// Get path, commit count and table size in parallel
	var wg sync.WaitGroup

	// Get repo path from metadata
	wg.Add(1)
	go func() {
		defer wg.Done()
		info.Path = repoDb.GetRepoPath(ctx)
		if info.Path != "" {
			// Check if path still exists
			if _, err := os.Stat(info.Path); err == nil {
				info.Status = "ok"
			} else {
				info.Status = "orphaned"
			}
		} else if searchFS {
			// Fallback to filesystem search if requested
			info.Path = findWorkingDir(dbName, searchPath, searchDepth)
			if info.Path != "" {
				info.Status = "ok"
				// Save the found path to metadata for future use
				_ = repoDb.EnsureMetadataTable(ctx)
				_ = repoDb.SetRepoPath(ctx, info.Path)
			}
		}
	}()

	// Commit count via xpatch.stats() - O(1)
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = repoDb.QueryRow(ctx, "SELECT total_rows FROM xpatch.stats('pgit_commits')").Scan(&info.Commits)
	}()

	// Table sizes via pg_relation_size - fast
	wg.Add(1)
	go func() {
		defer wg.Done()
		var commitsSize, blobsSize int64
		_ = repoDb.QueryRow(ctx, "SELECT pg_relation_size('pgit_commits')").Scan(&commitsSize)
		_ = repoDb.QueryRow(ctx, "SELECT pg_relation_size('pgit_blobs')").Scan(&blobsSize)
		info.SizeBytes = commitsSize + blobsSize
		info.Size = formatBytes(info.SizeBytes)
	}()

	wg.Wait()

	return info
}

// findWorkingDir tries to locate the working directory for a database
func findWorkingDir(dbName string, searchPath string, depth int) string {
	if searchPath != "" {
		// Search only in the specified path
		if _, err := os.Stat(searchPath); err != nil {
			return ""
		}
		if depth <= 0 {
			depth = 10 // default for custom path
		}
		return searchForPgitConfig(searchPath, dbName, depth)
	}

	// Default: search broadly from home directory
	home, _ := os.UserHomeDir()
	if home == "" {
		return ""
	}

	if depth <= 0 {
		depth = 8 // default from home
	}
	return searchForPgitConfig(home, dbName, depth)
}

// searchForPgitConfig recursively searches for a .pgit/config.toml with the given database
func searchForPgitConfig(dir string, dbName string, maxDepth int) string {
	if maxDepth <= 0 {
		return ""
	}

	// Check if this directory has .pgit/config.toml with our database
	configPath := filepath.Join(dir, ".pgit", "config.toml")
	if data, err := os.ReadFile(configPath); err == nil {
		if strings.Contains(string(data), dbName) {
			return dir
		}
	}

	// Search subdirectories
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		// Skip hidden dirs and common non-project directories
		if strings.HasPrefix(name, ".") {
			continue
		}
		if name == "node_modules" || name == "vendor" || name == "target" ||
			name == "__pycache__" || name == "venv" || name == ".venv" {
			continue
		}

		found := searchForPgitConfig(filepath.Join(dir, name), dbName, maxDepth-1)
		if found != "" {
			return found
		}
	}

	return ""
}
