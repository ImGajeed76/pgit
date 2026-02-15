package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/imgajeed76/pgit/v2/internal/config"
	"github.com/imgajeed76/pgit/v2/internal/container"
	"github.com/imgajeed76/pgit/v2/internal/db"
	"github.com/imgajeed76/pgit/v2/internal/repo"
	"github.com/imgajeed76/pgit/v2/internal/ui"
	"github.com/imgajeed76/pgit/v2/internal/ui/styles"
	"github.com/imgajeed76/pgit/v2/internal/util"
	"github.com/spf13/cobra"
)

var cloneForce bool

func newCloneCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "clone <url> [directory]",
		Short: "Clone a repository from a remote",
		Long: `Clone a repository from a remote PostgreSQL database.

The URL should be a PostgreSQL connection string.
If directory is not specified, uses the database name.`,
		Args: cobra.RangeArgs(1, 2),
		RunE: runClone,
	}

	cmd.Flags().BoolVarP(&cloneForce, "force", "f", false, "Overwrite existing local database without prompting")

	return cmd
}

func runClone(cmd *cobra.Command, args []string) error {
	url := args[0]

	// Determine target directory
	dir := ""
	if len(args) > 1 {
		dir = args[1]
	} else {
		// Extract database name from URL for directory name
		// Simple extraction - could be improved
		dir = "pgit-clone"
	}

	// Make absolute
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return err
	}

	// Check if directory exists
	if _, err := os.Stat(absDir); err == nil {
		return util.NewError("Directory already exists").
			WithContext(fmt.Sprintf("Cannot clone into '%s'", dir)).
			WithSuggestions(
				fmt.Sprintf("rm -rf %s  # Remove existing directory", dir),
				"pgit clone <url> different-name  # Clone to different directory",
			)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	// Connect to remote first to verify
	spinner := ui.NewSpinner("Connecting to remote")
	spinner.Start()
	remoteDB, err := db.Connect(ctx, url)
	spinner.Stop()
	if err != nil {
		return util.DatabaseConnectionError(url, err)
	}
	defer remoteDB.Close()

	// Check if remote has the schema
	exists, err := remoteDB.SchemaExists(ctx)
	if err != nil {
		return err
	}
	if !exists {
		return util.NewError("Not a pgit repository").
			WithContext("Remote database does not contain pgit tables").
			WithCauses(
				"The database URL might be wrong",
				"The repository was not initialized with 'pgit init'",
			).
			WithSuggestion("pgit init <url>  # Initialize the remote database first")
	}

	// Get remote HEAD
	remoteHeadID, err := remoteDB.GetHead(ctx)
	if err != nil {
		return err
	}
	if remoteHeadID == "" {
		fmt.Println("Warning: Remote repository is empty (no commits)")
	}

	// Create directory
	if err := os.MkdirAll(absDir, 0755); err != nil {
		return err
	}

	// Check container runtime
	runtime := container.DetectRuntime()
	if runtime == container.RuntimeNone {
		os.RemoveAll(absDir)
		return util.ErrNoContainerRuntime
	}

	// Create .pgit directory
	pgitDir := filepath.Join(absDir, util.PgitDir)
	if err := os.MkdirAll(pgitDir, 0755); err != nil {
		os.RemoveAll(absDir)
		return err
	}

	// Create config
	cfg := config.DefaultConfig(absDir)
	cfg.SetRemote("origin", url)

	if err := cfg.Save(absDir); err != nil {
		os.RemoveAll(absDir)
		return err
	}

	// Create empty index
	idx := config.NewIndex()
	if err := idx.Save(absDir); err != nil {
		os.RemoveAll(absDir)
		return err
	}

	// Create local repository object
	r := &repo.Repository{
		Root:    absDir,
		Config:  cfg,
		Runtime: runtime,
	}

	// Start container and connect to local database
	containerSpinner := ui.NewSpinner("Starting local container")
	containerSpinner.Start()
	if err := r.StartContainer(); err != nil {
		containerSpinner.Stop()
		os.RemoveAll(absDir)
		return err
	}
	containerSpinner.Stop()

	if err := r.Connect(ctx); err != nil {
		os.RemoveAll(absDir)
		return err
	}
	defer r.Close()

	// Check if local database already has data
	schemaExists, err := r.DB.SchemaExists(ctx)
	if err != nil {
		os.RemoveAll(absDir)
		return err
	}

	if schemaExists {
		// Check if there are commits
		localHeadID, _ := r.DB.GetHead(ctx)
		if localHeadID != "" {
			if !cloneForce {
				fmt.Printf("%s Local database '%s' already contains data.\n",
					styles.Yellow("Warning:"), cfg.Core.LocalDB)
				fmt.Printf("This can happen if you previously cloned to the same directory path.\n")
				fmt.Printf("Overwrite local database? [y/N] ")

				reader := bufio.NewReader(os.Stdin)
				response, _ := reader.ReadString('\n')
				response = strings.TrimSpace(strings.ToLower(response))

				if response != "y" && response != "yes" {
					os.RemoveAll(absDir)
					fmt.Println("Clone aborted.")
					return nil
				}
			}
		}
	}

	// Drop and recreate schema to ensure clean slate
	if err := r.DB.DropSchema(ctx); err != nil {
		os.RemoveAll(absDir)
		return fmt.Errorf("failed to clean local database: %w", err)
	}
	if err := r.DB.InitSchema(ctx); err != nil {
		os.RemoveAll(absDir)
		return fmt.Errorf("failed to init local database: %w", err)
	}

	// Get all commits from remote
	if remoteHeadID != "" {
		commits, err := remoteDB.GetCommitLogFrom(ctx, remoteHeadID, 100000)
		if err != nil {
			os.RemoveAll(absDir)
			return err
		}

		// Reverse to get oldest first
		for i, j := 0, len(commits)-1; i < j; i, j = i+1, j-1 {
			commits[i], commits[j] = commits[j], commits[i]
		}

		fmt.Printf("Cloning %d commit(s)...\n", len(commits))

		// Copy commits and blobs
		progress := ui.NewProgress("Cloning", len(commits))
		for i, commit := range commits {
			progress.Update(i)
			fmt.Printf("\r\033[K  [%d/%d] %s %s\n", i+1, len(commits),
				styles.Yellow(util.ShortID(commit.ID)), firstLine(commit.Message))

			// Create commit locally
			if err := r.DB.CreateCommit(ctx, commit); err != nil {
				os.RemoveAll(absDir)
				return fmt.Errorf("failed to create commit %s: %w", util.ShortID(commit.ID), err)
			}

			// Get and create blobs
			blobs, err := remoteDB.GetBlobsAtCommit(ctx, commit.ID)
			if err != nil {
				os.RemoveAll(absDir)
				return err
			}
			if err := r.DB.CreateBlobs(ctx, blobs); err != nil {
				os.RemoveAll(absDir)
				return fmt.Errorf("failed to create blobs for %s: %w", util.ShortID(commit.ID), err)
			}
		}
		progress.Done()

		// Set HEAD
		if err := r.DB.SetHead(ctx, remoteHeadID); err != nil {
			os.RemoveAll(absDir)
			return err
		}

		// Set sync state
		if err := r.DB.SetSyncState(ctx, "origin", &remoteHeadID); err != nil {
			os.RemoveAll(absDir)
			return err
		}

		// Checkout working directory
		fmt.Println("Checking out files...")
		tree, err := r.DB.GetTreeAtCommit(ctx, remoteHeadID)
		if err != nil {
			os.RemoveAll(absDir)
			return err
		}

		for _, blob := range tree {
			if blob.ContentHash == nil {
				continue
			}

			absPath := filepath.Join(absDir, blob.Path)
			if err := os.MkdirAll(filepath.Dir(absPath), 0755); err != nil {
				continue
			}

			if blob.IsSymlink && blob.SymlinkTarget != nil {
				_ = os.Symlink(*blob.SymlinkTarget, absPath)
			} else {
				_ = os.WriteFile(absPath, blob.Content, os.FileMode(blob.Mode))
			}
		}
	}

	fmt.Println()
	fmt.Printf("Cloned into '%s'\n", styles.Cyan(dir))

	return nil
}
