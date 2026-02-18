package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/imgajeed76/pgit/v3/internal/config"
	"github.com/imgajeed76/pgit/v3/internal/db"
	"github.com/imgajeed76/pgit/v3/internal/repo"
	"github.com/imgajeed76/pgit/v3/internal/ui"
	"github.com/imgajeed76/pgit/v3/internal/ui/styles"
	"github.com/imgajeed76/pgit/v3/internal/util"
	"github.com/spf13/cobra"
)

func newPullCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pull [remote]",
		Short: "Fetch from and integrate with remote repository",
		Long: `Fetches commits from a remote repository and integrates them
into the local repository.

If local and remote have diverged, pgit will by default:
1. Find the common ancestor
2. Pull remote commits
3. Detect conflicting files (modified in both local and remote)
4. Write conflict markers to conflicting files
5. Leave non-conflicting local changes in working directory

With --rebase, pgit will:
1. Find the common ancestor
2. Save local commits since divergence
3. Reset to remote HEAD
4. Replay local commits on top of remote (creating new commit IDs)

Use 'pgit resolve <file>' to mark conflicts as resolved, then commit.`,
		RunE: runPull,
	}

	cmd.Flags().Bool("rebase", false, "Rebase local commits on top of remote")

	return cmd
}

func runPull(cmd *cobra.Command, args []string) error {
	remoteName := "origin"
	if len(args) > 0 {
		remoteName = args[0]
	}
	useRebase, _ := cmd.Flags().GetBool("rebase")

	r, err := repo.Open()
	if err != nil {
		return err
	}

	// Check for existing merge in progress
	mergeState, err := config.LoadMergeState(r.Root)
	if err != nil {
		return err
	}
	if mergeState.HasConflicts() {
		conflictList := ""
		for _, f := range mergeState.ConflictedFiles {
			conflictList += "\n    " + f
		}
		return util.NewError("Unresolved conflicts from previous pull").
			WithMessage("Conflicted files:"+conflictList).
			WithSuggestions(
				"pgit resolve <file>    # Mark file as resolved",
				"pgit commit -m \"...\"   # Complete the merge",
			)
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

	// Get remote HEAD
	remoteHeadID, err := remoteDB.GetHead(ctx)
	if err != nil {
		return err
	}
	if remoteHeadID == "" {
		fmt.Println("Remote has no commits")
		return nil
	}

	// Get local HEAD
	localHeadID, err := r.DB.GetHead(ctx)
	if err != nil {
		return err
	}

	// Check if already up to date
	if localHeadID != "" && localHeadID == remoteHeadID {
		fmt.Println("Already up to date")
		return nil
	}

	// Determine relationship between local and remote
	if localHeadID == "" {
		// Local is empty — full fast-forward
		newCommits, err := remoteDB.GetAllCommits(ctx)
		if err != nil {
			return err
		}
		fmt.Printf("Fast-forward: %d new commit(s)\n", len(newCommits))
		return pullFastForward(ctx, r, remoteDB, newCommits, remoteName)
	}

	// Check if this is a fast-forward (local HEAD exists on remote as ancestor)
	localExistsOnRemote, err := remoteDB.CommitExists(ctx, localHeadID)
	if err != nil {
		return err
	}

	if localExistsOnRemote {
		// Fast-forward: remote has everything we have, plus more
		newCommits, err := remoteDB.GetCommitsAfter(ctx, localHeadID)
		if err != nil {
			return err
		}
		if len(newCommits) == 0 {
			fmt.Println("Already up to date")
			return nil
		}
		fmt.Printf("Fast-forward: %d new commit(s)\n", len(newCommits))
		return pullFastForward(ctx, r, remoteDB, newCommits, remoteName)
	}

	// Check if remote HEAD exists locally (we're ahead, nothing to pull)
	remoteExistsLocally, err := r.DB.CommitExists(ctx, remoteHeadID)
	if err != nil {
		return err
	}
	if remoteExistsLocally {
		fmt.Println("Already up to date (local is ahead of remote)")
		return nil
	}

	// Diverged: find common ancestor via cross-DB walk
	commonAncestor, err := findCommonAncestorCrossDB(ctx, r.DB, remoteDB, remoteHeadID)
	if err != nil {
		return err
	}

	fmt.Println(styles.Warningf("Histories have diverged"))
	if commonAncestor != "" {
		fmt.Printf("Common ancestor: %s\n", styles.Yellow(util.ShortID(commonAncestor)))
	}

	// Get new remote commits since common ancestor
	var newRemoteCommits []*db.Commit
	if commonAncestor == "" {
		newRemoteCommits, err = remoteDB.GetAllCommits(ctx)
	} else {
		newRemoteCommits, err = remoteDB.GetCommitsAfter(ctx, commonAncestor)
	}
	if err != nil {
		return err
	}

	// Get local commits since common ancestor
	var localCommitsAfter []*db.Commit
	if commonAncestor == "" {
		localCommitsAfter, err = r.DB.GetAllCommits(ctx)
	} else {
		localCommitsAfter, err = r.DB.GetCommitsAfter(ctx, commonAncestor)
	}
	if err != nil {
		return err
	}

	if useRebase {
		return pullRebase(ctx, r, remoteDB, localHeadID, localCommitsAfter, newRemoteCommits, commonAncestor, remoteName)
	}

	return pullDiverged(ctx, r, remoteDB, localHeadID, localCommitsAfter, newRemoteCommits, commonAncestor, remoteName)
}

// findCommonAncestorCrossDB finds the latest commit that exists in both databases.
// Walks remote commits backward (newest first) and checks each against local.
// Returns the common ancestor ID (may be empty if no common history).
func findCommonAncestorCrossDB(ctx context.Context, localDB, remoteDB *db.DB, remoteHeadID string) (string, error) {
	pageSize := 500
	currentID := remoteHeadID

	for {
		remoteCommits, err := remoteDB.GetCommitLogFrom(ctx, currentID, pageSize)
		if err != nil {
			return "", err
		}
		if len(remoteCommits) == 0 {
			return "", nil // No common ancestor found
		}

		for _, rc := range remoteCommits {
			exists, err := localDB.CommitExists(ctx, rc.ID)
			if err != nil {
				return "", err
			}
			if exists {
				return rc.ID, nil
			}
		}

		// Move to next page: oldest commit in this page
		lastCommit := remoteCommits[len(remoteCommits)-1]
		if lastCommit.ParentID == nil {
			return "", nil // Reached root of remote, no common ancestor
		}
		currentID = *lastCommit.ParentID
	}
}

func pullFastForward(ctx context.Context, r *repo.Repository, remoteDB *db.DB, commits []*db.Commit, remoteName string) error {
	// Batched insertion: 100 commits per batch
	const batchSize = 100
	progress := ui.NewProgress("Pulling", len(commits))

	for i := 0; i < len(commits); i += batchSize {
		end := min(i+batchSize, len(commits))
		batch := commits[i:end]

		// Batch insert commits
		if err := r.DB.CreateCommitsBatch(ctx, batch); err != nil {
			return fmt.Errorf("failed to create commits: %w", err)
		}

		// Insert blobs per commit
		for _, commit := range batch {
			blobs, err := remoteDB.GetBlobsAtCommit(ctx, commit.ID)
			if err != nil {
				return err
			}
			if len(blobs) > 0 {
				if err := r.DB.CreateBlobs(ctx, blobs); err != nil {
					return fmt.Errorf("failed to create blobs for %s: %w", util.ShortID(commit.ID), err)
				}
			}
		}

		progress.Update(end)
	}
	progress.Done()

	// Update HEAD
	lastCommit := commits[len(commits)-1]
	if err := r.DB.SetHead(ctx, lastCommit.ID); err != nil {
		return err
	}

	// Update sync state
	if err := r.DB.SetSyncState(ctx, remoteName, &lastCommit.ID); err != nil {
		return err
	}

	// Update working directory
	fmt.Println("Updating working directory...")
	tree, err := r.DB.GetTreeAtCommit(ctx, lastCommit.ID)
	if err != nil {
		return err
	}

	for _, blob := range tree {
		absPath := r.AbsPath(blob.Path)
		if err := os.MkdirAll(filepath.Dir(absPath), 0755); err != nil {
			continue
		}
		if blob.IsSymlink && blob.SymlinkTarget != nil {
			_ = os.Remove(absPath)
			_ = os.Symlink(*blob.SymlinkTarget, absPath)
		} else if blob.Content != nil {
			_ = os.WriteFile(absPath, blob.Content, os.FileMode(blob.Mode))
		}
	}

	fmt.Println()
	fmt.Printf("%s %s\n", styles.Successf("Updated to"), styles.Yellow(util.ShortID(lastCommit.ID)))

	return nil
}

func pullDiverged(ctx context.Context, r *repo.Repository, remoteDB *db.DB, localHeadID string, localCommits, remoteCommits []*db.Commit, commonAncestor, remoteName string) error {
	fmt.Printf("Local commits since divergence: %d\n", len(localCommits))
	fmt.Printf("Remote commits to pull: %d\n", len(remoteCommits))

	// Get trees for conflict detection
	localTree, err := r.DB.GetTreeAtCommit(ctx, localHeadID)
	if err != nil {
		return err
	}

	var ancestorTree []*db.Blob
	if commonAncestor != "" {
		ancestorTree, err = r.DB.GetTreeAtCommit(ctx, commonAncestor)
		if err != nil {
			return err
		}
	}

	remoteHeadCommit := remoteCommits[len(remoteCommits)-1]
	remoteTree, err := remoteDB.GetTreeAtCommit(ctx, remoteHeadCommit.ID)
	if err != nil {
		return err
	}

	// Build maps for easy lookup
	localFiles := make(map[string]*db.Blob)
	for _, b := range localTree {
		localFiles[b.Path] = b
	}

	ancestorFiles := make(map[string]*db.Blob)
	for _, b := range ancestorTree {
		ancestorFiles[b.Path] = b
	}

	remoteFiles := make(map[string]*db.Blob)
	for _, b := range remoteTree {
		remoteFiles[b.Path] = b
	}

	// Detect conflicts and modified files
	var conflicts []string
	var localOnlyChanges []string
	var remoteOnlyChanges []string

	// Check all files
	allPaths := make(map[string]bool)
	for p := range localFiles {
		allPaths[p] = true
	}
	for p := range remoteFiles {
		allPaths[p] = true
	}
	for p := range ancestorFiles {
		allPaths[p] = true
	}

	for path := range allPaths {
		local := localFiles[path]
		remote := remoteFiles[path]
		ancestor := ancestorFiles[path]

		localChanged := fileChanged(ancestor, local)
		remoteChanged := fileChanged(ancestor, remote)

		if localChanged && remoteChanged {
			// Both modified - potential conflict
			if !filesEqual(local, remote) {
				conflicts = append(conflicts, path)
			}
		} else if localChanged {
			localOnlyChanges = append(localOnlyChanges, path)
		} else if remoteChanged {
			remoteOnlyChanges = append(remoteOnlyChanges, path)
		}
	}

	fmt.Println()
	if len(conflicts) > 0 {
		fmt.Printf("Conflicting files: %s\n", styles.Redf("%d", len(conflicts)))
	}
	if len(localOnlyChanges) > 0 {
		fmt.Printf("Local-only changes: %d\n", len(localOnlyChanges))
	}
	if len(remoteOnlyChanges) > 0 {
		fmt.Printf("Remote-only changes: %d\n", len(remoteOnlyChanges))
	}

	// Collect ALL commit IDs after common ancestor (local-only commits plus
	// any previously-pulled remote commits that may be interleaved in the
	// xpatch chain). We need these to clean up file_refs and content.
	var allAfterIDs []string
	for _, c := range localCommits {
		allAfterIDs = append(allAfterIDs, c.ID)
	}
	// Also include any remote commits that already exist locally from a
	// previous partial pull — they'll be re-pulled fresh after truncation.
	for _, rc := range remoteCommits {
		exists, err := r.DB.CommitExists(ctx, rc.ID)
		if err != nil {
			return err
		}
		if exists {
			allAfterIDs = append(allAfterIDs, rc.ID)
		}
	}

	// DELETE FIRST, THEN PULL — required by xpatch's append-only delta chain.
	// Local changes are already loaded in memory (localTree) and the working
	// directory files are untouched, so no data is lost.
	fmt.Println()
	fmt.Println("Cleaning up diverged commits...")

	// Step 1: Delete blobs (file_refs + truncate content chains) for all
	// commits after the common ancestor
	if len(allAfterIDs) > 0 {
		if err := r.DB.DeleteBlobsForCommits(ctx, allAfterIDs); err != nil {
			return fmt.Errorf("failed to clean up blobs: %w", err)
		}
	}

	// Step 2: Delete commits by PK. xpatch cascade-deletes all rows with
	// higher _xp_seq, so the first delete effectively truncates the chain.
	if err := r.DB.DeleteCommits(ctx, allAfterIDs); err != nil {
		return fmt.Errorf("failed to delete diverged commits: %w", err)
	}

	// Step 3: Pull remote commits fresh (they append to the truncated chain)
	fmt.Println("Pulling remote commits...")

	const batchSize = 100
	progress := ui.NewProgress("Pulling", len(remoteCommits))

	for i := 0; i < len(remoteCommits); i += batchSize {
		end := min(i+batchSize, len(remoteCommits))
		batch := remoteCommits[i:end]

		if err := r.DB.CreateCommitsBatch(ctx, batch); err != nil {
			return fmt.Errorf("failed to create commits: %w", err)
		}

		for _, commit := range batch {
			blobs, err := remoteDB.GetBlobsAtCommit(ctx, commit.ID)
			if err != nil {
				return err
			}
			if len(blobs) > 0 {
				if err := r.DB.CreateBlobs(ctx, blobs); err != nil {
					return fmt.Errorf("failed to create blobs for %s: %w", util.ShortID(commit.ID), err)
				}
			}
		}

		progress.Update(end)
	}
	progress.Done()

	// Update HEAD to remote
	if err := r.DB.SetHead(ctx, remoteHeadCommit.ID); err != nil {
		return err
	}

	// Update sync state
	if err := r.DB.SetSyncState(ctx, remoteName, &remoteHeadCommit.ID); err != nil {
		return err
	}

	// Update working directory
	fmt.Println()
	fmt.Println("Updating working directory...")

	// Start merge state if we have conflicts
	mergeState := &config.MergeState{
		InProgress:     len(conflicts) > 0,
		RemoteName:     remoteName,
		RemoteCommitID: remoteHeadCommit.ID,
		LocalCommitID:  localHeadID,
		CommonAncestor: commonAncestor,
	}

	// Write files to working directory
	for _, blob := range remoteTree {
		absPath := r.AbsPath(blob.Path)

		// Check if this is a conflict
		isConflict := false
		for _, cp := range conflicts {
			if cp == blob.Path {
				isConflict = true
				break
			}
		}

		if isConflict {
			// Write with conflict markers
			localBlob := localFiles[blob.Path]
			var localContent, remoteContent []byte
			if localBlob != nil {
				localContent = localBlob.Content
			}
			if blob.Content != nil {
				remoteContent = blob.Content
			}

			if err := config.CreateConflictedFile(absPath, localContent, remoteContent, remoteName); err != nil {
				return fmt.Errorf("failed to create conflicted file %s: %w", blob.Path, err)
			}
			mergeState.AddConflict(blob.Path)
		} else {
			// Check if local has changes we should preserve
			isLocalOnly := false
			for _, lp := range localOnlyChanges {
				if lp == blob.Path {
					isLocalOnly = true
					break
				}
			}

			if isLocalOnly {
				// Keep local version (it's already in working dir)
				continue
			}

			// Write remote version
			if err := os.MkdirAll(filepath.Dir(absPath), 0755); err != nil {
				continue
			}
			if blob.IsSymlink && blob.SymlinkTarget != nil {
				_ = os.Remove(absPath)
				_ = os.Symlink(*blob.SymlinkTarget, absPath)
			} else if blob.Content != nil {
				_ = os.WriteFile(absPath, blob.Content, os.FileMode(blob.Mode))
			}
		}
	}

	// Also restore local-only files to working directory
	for _, path := range localOnlyChanges {
		if blob, ok := localFiles[path]; ok && blob.Content != nil {
			absPath := r.AbsPath(path)
			_ = os.MkdirAll(filepath.Dir(absPath), 0755)
			_ = os.WriteFile(absPath, blob.Content, os.FileMode(blob.Mode))
		}
	}

	// Save merge state
	if err := mergeState.Save(r.Root); err != nil {
		return err
	}

	fmt.Println()
	if len(conflicts) > 0 {
		fmt.Println(styles.Warningf("CONFLICTS detected in %d file(s):", len(conflicts)))
		fmt.Println()
		for _, f := range conflicts {
			fmt.Printf("  %s %s\n", styles.Red("C"), f)
		}
		fmt.Println()
		fmt.Println("Fix the conflicts, then:")
		fmt.Println("  pgit resolve <file>    # Mark file as resolved")
		fmt.Println("  pgit add <file>        # Stage resolved file")
		fmt.Println("  pgit commit -m \"...\"   # Complete the merge")
	} else if len(localOnlyChanges) > 0 {
		fmt.Println(styles.Successf("Merged successfully"))
		fmt.Println()
		fmt.Println("Your local changes are preserved in the working directory:")
		for _, f := range localOnlyChanges {
			fmt.Printf("  %s %s\n", styles.Yellow("M"), f)
		}
		fmt.Println()
		fmt.Println("Stage and commit them when ready:")
		fmt.Println("  pgit add .")
		fmt.Println("  pgit commit -m \"Merge with local changes\"")
	} else {
		fmt.Printf("%s %s\n", styles.Successf("Updated to"), styles.Yellow(util.ShortID(remoteHeadCommit.ID)))
	}

	return nil
}

// pullRebase rebases local commits on top of remote
func pullRebase(ctx context.Context, r *repo.Repository, remoteDB *db.DB, localHeadID string, localCommits, remoteCommits []*db.Commit, commonAncestor, remoteName string) error {
	fmt.Printf("Rebasing %d local commit(s) onto remote\n", len(localCommits))

	// Delete local commits after common ancestor.
	// Must delete blobs first, then truncate the xpatch commit chain.
	fmt.Println("Resetting to common ancestor...")
	localIDs := make([]string, len(localCommits))
	for i, c := range localCommits {
		localIDs[i] = c.ID
	}
	if err := r.DB.DeleteBlobsForCommits(ctx, localIDs); err != nil {
		return fmt.Errorf("failed to clean up blobs: %w", err)
	}
	if err := r.DB.DeleteCommits(ctx, localIDs); err != nil {
		return fmt.Errorf("failed to delete local commits: %w", err)
	}
	if commonAncestor != "" {
		if err := r.DB.SetHead(ctx, commonAncestor); err != nil {
			return err
		}
	}

	// Pull remote commits in batches
	fmt.Println("Pulling remote commits...")
	const batchSize = 100
	progress := ui.NewProgress("Pulling", len(remoteCommits))

	for i := 0; i < len(remoteCommits); i += batchSize {
		end := min(i+batchSize, len(remoteCommits))
		batch := remoteCommits[i:end]

		if err := r.DB.CreateCommitsBatch(ctx, batch); err != nil {
			return fmt.Errorf("failed to create commits: %w", err)
		}

		for _, commit := range batch {
			blobs, err := remoteDB.GetBlobsAtCommit(ctx, commit.ID)
			if err != nil {
				return err
			}
			if len(blobs) > 0 {
				if err := r.DB.CreateBlobs(ctx, blobs); err != nil {
					return fmt.Errorf("failed to create blobs for %s: %w", util.ShortID(commit.ID), err)
				}
			}
		}

		progress.Update(end)
	}
	progress.Done()

	// Update HEAD to remote head
	remoteHeadCommit := remoteCommits[len(remoteCommits)-1]
	if err := r.DB.SetHead(ctx, remoteHeadCommit.ID); err != nil {
		return err
	}

	// Update sync state
	if err := r.DB.SetSyncState(ctx, remoteName, &remoteHeadCommit.ID); err != nil {
		return err
	}

	// Now replay local commits
	if len(localCommits) > 0 {
		fmt.Println()
		fmt.Println("Replaying local commits...")

		for i, oldCommit := range localCommits {
			fmt.Printf("  [%d/%d] Replaying: %s\n", i+1, len(localCommits), firstLine(oldCommit.Message))

			// Get blobs from the old commit
			oldBlobs, err := r.DB.GetBlobsAtCommit(ctx, oldCommit.ID)
			if err != nil {
				return fmt.Errorf("failed to get blobs for replay: %w", err)
			}

			// Create new commit with new ID but same message/author
			newCommitID := util.NewULID()
			currentHeadID, _ := r.DB.GetHead(ctx)
			var parentID *string
			if currentHeadID != "" {
				parentID = &currentHeadID
			}

			newCommit := &db.Commit{
				ID:             newCommitID,
				ParentID:       parentID,
				TreeHash:       oldCommit.TreeHash,
				Message:        oldCommit.Message,
				AuthorName:     oldCommit.AuthorName,
				AuthorEmail:    oldCommit.AuthorEmail,
				AuthoredAt:     oldCommit.AuthoredAt,
				CommitterName:  oldCommit.CommitterName,
				CommitterEmail: oldCommit.CommitterEmail,
				CommittedAt:    time.Now(), // New timestamp for replay
			}

			if err := r.DB.CreateCommit(ctx, newCommit); err != nil {
				return fmt.Errorf("failed to replay commit: %w", err)
			}

			// Batch insert blobs with new commit ID (fix: was single CreateBlob)
			if len(oldBlobs) > 0 {
				replayedBlobs := make([]*db.Blob, len(oldBlobs))
				for j, blob := range oldBlobs {
					replayedBlobs[j] = &db.Blob{
						Path:          blob.Path,
						CommitID:      newCommitID,
						Content:       blob.Content,
						ContentHash:   blob.ContentHash,
						Mode:          blob.Mode,
						IsBinary:      blob.IsBinary,
						IsSymlink:     blob.IsSymlink,
						SymlinkTarget: blob.SymlinkTarget,
					}
				}
				if err := r.DB.CreateBlobs(ctx, replayedBlobs); err != nil {
					return fmt.Errorf("failed to replay blobs: %w", err)
				}
			}

			// Update HEAD
			if err := r.DB.SetHead(ctx, newCommitID); err != nil {
				return err
			}

			fmt.Printf("           %s -> %s\n",
				styles.Mute(util.ShortID(oldCommit.ID)),
				styles.Yellow(util.ShortID(newCommitID)))
		}
	}

	// Update working directory
	fmt.Println()
	fmt.Println("Updating working directory...")
	headID, _ := r.DB.GetHead(ctx)
	if headID != "" {
		tree, err := r.DB.GetTreeAtCommit(ctx, headID)
		if err != nil {
			return err
		}

		for _, blob := range tree {
			absPath := r.AbsPath(blob.Path)
			if err := os.MkdirAll(filepath.Dir(absPath), 0755); err != nil {
				continue
			}
			if blob.IsSymlink && blob.SymlinkTarget != nil {
				_ = os.Remove(absPath)
				_ = os.Symlink(*blob.SymlinkTarget, absPath)
			} else if blob.Content != nil {
				_ = os.WriteFile(absPath, blob.Content, os.FileMode(blob.Mode))
			}
		}
	}

	fmt.Println()
	fmt.Printf("%s Rebased %d commit(s)\n", styles.Successf("Successfully"), len(localCommits))
	if headID != "" {
		fmt.Printf("HEAD is now at %s\n", styles.Yellow(util.ShortID(headID)))
	}

	return nil
}

// fileChanged checks if a file changed between ancestor and current
func fileChanged(ancestor, current *db.Blob) bool {
	if ancestor == nil && current == nil {
		return false
	}
	if ancestor == nil || current == nil {
		return true // Added or deleted
	}
	// Compare hashes using ContentHashEqual
	return !util.ContentHashEqual(ancestor.ContentHash, current.ContentHash)
}

// filesEqual checks if two blobs have the same content
func filesEqual(a, b *db.Blob) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return util.ContentHashEqual(a.ContentHash, b.ContentHash)
}
