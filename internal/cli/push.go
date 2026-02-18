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
	"github.com/jackc/pgx/v5"
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

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
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

	// Check for divergence (no limit — just check if remote HEAD exists locally)
	if remoteHeadID != "" && !force {
		localHasRemoteHead, err := r.DB.CommitExists(ctx, remoteHeadID)
		if err != nil {
			return err
		}
		if !localHasRemoteHead {
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

	// Get commits to push — no limit
	var commitsToPush []*db.Commit
	if remoteHeadID == "" {
		// First push: push everything
		commitsToPush, err = r.DB.GetAllCommits(ctx)
	} else {
		// Incremental push: only commits after remote HEAD
		commitsToPush, err = r.DB.GetCommitsAfter(ctx, remoteHeadID)
	}
	if err != nil {
		return err
	}

	if len(commitsToPush) == 0 {
		fmt.Println("Everything up-to-date")
		return nil
	}

	fmt.Printf("Pushing %d commit(s)...\n", len(commitsToPush))

	// Push in batches of 100, each batch wrapped in a transaction
	const batchSize = 100
	progress := ui.NewProgress("Pushing", len(commitsToPush))

	for i := 0; i < len(commitsToPush); i += batchSize {
		end := min(i+batchSize, len(commitsToPush))
		batch := commitsToPush[i:end]

		err := remoteDB.WithTx(ctx, func(tx pgx.Tx) error {
			// Insert commits via COPY on tx
			if err := remoteDB.CreateCommitsBatchTx(ctx, tx, batch); err != nil {
				return fmt.Errorf("failed to push commits: %w", err)
			}

			// Insert blobs per commit within same tx
			for _, commit := range batch {
				blobs, err := r.DB.GetBlobsAtCommit(ctx, commit.ID)
				if err != nil {
					return err
				}
				if len(blobs) > 0 {
					if err := remoteDB.CreateBlobsTx(ctx, tx, blobs); err != nil {
						return fmt.Errorf("failed to push blobs for %s: %w", util.ShortID(commit.ID), err)
					}
				}
			}
			return nil
		})
		if err != nil {
			return err
		}

		progress.Update(end)
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
