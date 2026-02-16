package cli

import (
	"context"
	"fmt"
	"time"

	"github.com/imgajeed76/pgit/v3/internal/db"
	"github.com/imgajeed76/pgit/v3/internal/repo"
	"github.com/imgajeed76/pgit/v3/internal/ui"
	"github.com/imgajeed76/pgit/v3/internal/ui/styles"
	"github.com/imgajeed76/pgit/v3/internal/util"
	"github.com/spf13/cobra"
)

func newPushCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "push [remote]",
		Short: "Update remote refs along with associated objects",
		Long: `Updates remote database with commits from the local repository.

If no remote is specified, uses 'origin' by default.

Note: Push will fail if the remote has commits that you don't have locally.
In that case, pull first to sync.`,
		RunE: runPush,
	}

	cmd.Flags().BoolP("force", "f", false, "Force push (overwrite remote)")

	return cmd
}

func runPush(cmd *cobra.Command, args []string) error {
	force, _ := cmd.Flags().GetBool("force")

	remoteName := "origin"
	if len(args) > 0 {
		remoteName = args[0]
	}

	r, err := repo.Open()
	if err != nil {
		return err
	}

	// Get remote config
	remote, exists := r.Config.GetRemote(remoteName)
	if !exists {
		return util.RemoteNotFoundError(remoteName)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Connect to local database
	if err := r.Connect(ctx); err != nil {
		return err
	}
	defer r.Close()

	// Connect to remote database
	spinner := ui.NewSpinner(fmt.Sprintf("Connecting to %s", styles.Cyan(remoteName)))
	spinner.Start()
	remoteDB, err := r.ConnectTo(ctx, remote.URL)
	spinner.Stop()
	if err != nil {
		return util.DatabaseConnectionError(remote.URL, err)
	}
	defer remoteDB.Close()

	// Initialize remote schema if needed
	exists, err = remoteDB.SchemaExists(ctx)
	if err != nil {
		return err
	}
	if !exists {
		fmt.Println("Initializing remote schema...")
		if err := remoteDB.InitSchema(ctx); err != nil {
			return err
		}
	}

	// Get local HEAD
	localHeadID, err := r.DB.GetHead(ctx)
	if err != nil {
		return err
	}
	if localHeadID == "" {
		fmt.Println("Nothing to push (no commits)")
		return nil
	}

	// Get remote HEAD
	remoteHeadID, err := remoteDB.GetHead(ctx)
	if err != nil {
		return err
	}

	// Check if we need to push
	if remoteHeadID != "" && remoteHeadID == localHeadID {
		fmt.Println("Everything up-to-date")
		return nil
	}

	// Check for divergence
	if remoteHeadID != "" && !force {
		// Check if remote HEAD is an ancestor of local HEAD
		localCommits, err := r.DB.GetCommitLogFrom(ctx, localHeadID, 1000)
		if err != nil {
			return err
		}

		isAncestor := false
		for _, c := range localCommits {
			if c.ID == remoteHeadID {
				isAncestor = true
				break
			}
		}

		if !isAncestor {
			return util.NewError("Push rejected (non-fast-forward)").
				WithMessage("Remote has commits that you don't have locally").
				WithCauses(
					"Someone else pushed to the remote",
					"Your local branch is out of date",
				).
				WithSuggestions(
					"pgit pull "+remoteName+"  # Pull first to sync",
					"pgit push --force "+remoteName+"  # Force push (overwrites remote)",
				)
		}
	}

	// Get commits to push
	fmt.Println("Calculating commits to push...")

	var commitsToPush []*db.Commit
	localCommits, err := r.DB.GetCommitLogFrom(ctx, localHeadID, 1000)
	if err != nil {
		return err
	}

	// Find commits that remote doesn't have
	for _, c := range localCommits {
		if remoteHeadID != "" && c.ID == remoteHeadID {
			break
		}
		commitsToPush = append(commitsToPush, c)
	}

	// Reverse to push in correct order
	for i, j := 0, len(commitsToPush)-1; i < j; i, j = i+1, j-1 {
		commitsToPush[i], commitsToPush[j] = commitsToPush[j], commitsToPush[i]
	}

	if len(commitsToPush) == 0 {
		fmt.Println("Everything up-to-date")
		return nil
	}

	fmt.Printf("Pushing %d commit(s)...\n", len(commitsToPush))

	// Push commits and blobs
	progress := ui.NewProgress("Pushing", len(commitsToPush))
	for i, commit := range commitsToPush {
		progress.Update(i)
		fmt.Printf("\r\033[K  [%d/%d] %s %s\n", i+1, len(commitsToPush),
			styles.Yellow(util.ShortID(commit.ID)), firstLine(commit.Message))

		// Create commit on remote
		if err := remoteDB.CreateCommit(ctx, commit); err != nil {
			return fmt.Errorf("failed to push commit %s: %w", util.ShortID(commit.ID), err)
		}

		// Get and push blobs for this commit
		blobs, err := r.DB.GetBlobsAtCommit(ctx, commit.ID)
		if err != nil {
			return err
		}
		if err := remoteDB.CreateBlobs(ctx, blobs); err != nil {
			return fmt.Errorf("failed to push blobs for %s: %w", util.ShortID(commit.ID), err)
		}
	}
	progress.Done()

	// Update remote HEAD
	if err := remoteDB.SetHead(ctx, localHeadID); err != nil {
		return err
	}

	// Update sync state
	if err := r.DB.SetSyncState(ctx, remoteName, &localHeadID); err != nil {
		return err
	}

	fmt.Println()
	fmt.Printf("%s %s -> %s\n", styles.Successf("Pushed"),
		styles.Yellow(util.ShortID(commitsToPush[0].ID)),
		styles.Yellow(util.ShortID(localHeadID)))

	return nil
}
