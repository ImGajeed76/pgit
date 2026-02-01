package cli

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/imgajeed76/pgit/internal/config"
	"github.com/imgajeed76/pgit/internal/db"
	"github.com/imgajeed76/pgit/internal/repo"
	"github.com/imgajeed76/pgit/internal/ui"
	"github.com/imgajeed76/pgit/internal/ui/styles"
	"github.com/imgajeed76/pgit/internal/util"
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

	// Get remote HEAD
	remoteHead, err := remoteDB.GetHeadCommit(ctx)
	if err != nil {
		return err
	}
	if remoteHead == nil {
		fmt.Println("Remote has no commits")
		return nil
	}

	// Get local HEAD
	localHead, err := r.DB.GetHeadCommit(ctx)
	if err != nil {
		return err
	}

	// Check if already up to date
	if localHead != nil && localHead.ID == remoteHead.ID {
		fmt.Println("Already up to date")
		return nil
	}

	// Get all remote commits
	remoteCommits, err := remoteDB.GetCommitLogFrom(ctx, remoteHead.ID, 10000)
	if err != nil {
		return err
	}

	// Check for divergence
	var localCommits []*db.Commit
	if localHead != nil {
		localCommits, err = r.DB.GetCommitLogFrom(ctx, localHead.ID, 10000)
		if err != nil {
			return err
		}
	}

	// Find common ancestor
	localIDs := make(map[string]bool)
	for _, c := range localCommits {
		localIDs[c.ID] = true
	}

	var commonAncestor string
	var newCommits []*db.Commit
	for _, c := range remoteCommits {
		if localIDs[c.ID] {
			commonAncestor = c.ID
			break
		}
		newCommits = append(newCommits, c)
	}

	// Reverse to get oldest first
	for i, j := 0, len(newCommits)-1; i < j; i, j = i+1, j-1 {
		newCommits[i], newCommits[j] = newCommits[j], newCommits[i]
	}

	if len(newCommits) == 0 {
		fmt.Println("Already up to date")
		return nil
	}

	// Check if this is a fast-forward
	isFF := localHead == nil || commonAncestor == localHead.ID

	if isFF {
		// Fast-forward: just add commits
		fmt.Printf("Fast-forward: %d new commit(s)\n", len(newCommits))
		return pullFastForward(ctx, r, remoteDB, newCommits, remoteName)
	}

	// Diverged: need to handle conflicts
	fmt.Println(styles.Warningf("Histories have diverged"))
	fmt.Printf("Common ancestor: %s\n", styles.Yellow(util.ShortID(commonAncestor)))

	if useRebase {
		return pullRebase(ctx, r, remoteDB, localHead, localCommits, newCommits, commonAncestor, remoteName)
	}

	return pullDiverged(ctx, r, remoteDB, localHead, localCommits, newCommits, commonAncestor, remoteName)
}

func pullFastForward(ctx context.Context, r *repo.Repository, remoteDB *db.DB, commits []*db.Commit, remoteName string) error {
	for i, commit := range commits {
		fmt.Printf("  [%d/%d] %s %s\n", i+1, len(commits),
			styles.Yellow(util.ShortID(commit.ID)), firstLine(commit.Message))

		// Create commit locally
		if err := r.DB.CreateCommit(ctx, commit); err != nil {
			return fmt.Errorf("failed to create commit %s: %w", util.ShortID(commit.ID), err)
		}

		// Get and create blobs
		blobs, err := remoteDB.GetBlobsAtCommit(ctx, commit.ID)
		if err != nil {
			return err
		}
		if err := r.DB.CreateBlobs(ctx, blobs); err != nil {
			return fmt.Errorf("failed to create blobs for %s: %w", util.ShortID(commit.ID), err)
		}
	}

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

func pullDiverged(ctx context.Context, r *repo.Repository, remoteDB *db.DB, localHead *db.Commit, localCommits, remoteCommits []*db.Commit, commonAncestor, remoteName string) error {
	// Find local commits after common ancestor
	var localToReapply []*db.Commit
	for _, c := range localCommits {
		if c.ID == commonAncestor {
			break
		}
		localToReapply = append(localToReapply, c)
	}

	fmt.Printf("Local commits since divergence: %d\n", len(localToReapply))
	fmt.Printf("Remote commits to pull: %d\n", len(remoteCommits))

	// Get trees for conflict detection
	localTree, err := r.DB.GetTreeAtCommit(ctx, localHead.ID)
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

	// Delete local commits after common ancestor
	fmt.Println()
	fmt.Println("Resetting to common ancestor...")
	if commonAncestor != "" {
		for _, c := range localToReapply {
			if err := r.DB.DeleteCommitsAfter(ctx, c.ID); err != nil {
				return err
			}
		}
		if err := r.DB.SetHead(ctx, commonAncestor); err != nil {
			return err
		}
	}

	// Pull remote commits
	fmt.Println("Pulling remote commits...")
	for i, commit := range remoteCommits {
		fmt.Printf("  [%d/%d] %s %s\n", i+1, len(remoteCommits),
			styles.Yellow(util.ShortID(commit.ID)), firstLine(commit.Message))

		if err := r.DB.CreateCommit(ctx, commit); err != nil {
			return fmt.Errorf("failed to create commit %s: %w", util.ShortID(commit.ID), err)
		}

		blobs, err := remoteDB.GetBlobsAtCommit(ctx, commit.ID)
		if err != nil {
			return err
		}
		if err := r.DB.CreateBlobs(ctx, blobs); err != nil {
			return fmt.Errorf("failed to create blobs for %s: %w", util.ShortID(commit.ID), err)
		}
	}

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
		LocalCommitID:  localHead.ID,
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
func pullRebase(ctx context.Context, r *repo.Repository, remoteDB *db.DB, localHead *db.Commit, localCommits, remoteCommits []*db.Commit, commonAncestor, remoteName string) error {
	// Find local commits after common ancestor (in reverse order - oldest first for replay)
	var localToReplay []*db.Commit
	for _, c := range localCommits {
		if c.ID == commonAncestor {
			break
		}
		localToReplay = append(localToReplay, c)
	}
	// Reverse to get oldest first
	for i, j := 0, len(localToReplay)-1; i < j; i, j = i+1, j-1 {
		localToReplay[i], localToReplay[j] = localToReplay[j], localToReplay[i]
	}

	fmt.Printf("Rebasing %d local commit(s) onto remote\n", len(localToReplay))

	// Delete local commits after common ancestor
	fmt.Println("Resetting to common ancestor...")
	for i := len(localToReplay) - 1; i >= 0; i-- {
		if err := r.DB.DeleteCommitsAfter(ctx, localToReplay[i].ID); err != nil {
			return err
		}
	}
	if commonAncestor != "" {
		if err := r.DB.SetHead(ctx, commonAncestor); err != nil {
			return err
		}
	}

	// Pull remote commits
	fmt.Println("Pulling remote commits...")
	for i, commit := range remoteCommits {
		fmt.Printf("  [%d/%d] %s %s\n", i+1, len(remoteCommits),
			styles.Yellow(util.ShortID(commit.ID)), firstLine(commit.Message))

		if err := r.DB.CreateCommit(ctx, commit); err != nil {
			return fmt.Errorf("failed to create commit %s: %w", util.ShortID(commit.ID), err)
		}

		blobs, err := remoteDB.GetBlobsAtCommit(ctx, commit.ID)
		if err != nil {
			return err
		}
		if err := r.DB.CreateBlobs(ctx, blobs); err != nil {
			return fmt.Errorf("failed to create blobs for %s: %w", util.ShortID(commit.ID), err)
		}
	}

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
	if len(localToReplay) > 0 {
		fmt.Println()
		fmt.Println("Replaying local commits...")

		for i, oldCommit := range localToReplay {
			fmt.Printf("  [%d/%d] Replaying: %s\n", i+1, len(localToReplay), firstLine(oldCommit.Message))

			// Get blobs from the old commit
			oldBlobs, err := r.DB.GetBlobsAtCommit(ctx, oldCommit.ID)
			if err != nil {
				return fmt.Errorf("failed to get blobs for replay: %w", err)
			}

			// Create new commit with new ID but same message/author
			newCommitID := util.NewULID()
			currentHead, _ := r.DB.GetHeadCommit(ctx)
			var parentID *string
			if currentHead != nil {
				parentID = &currentHead.ID
			}

			newCommit := &db.Commit{
				ID:          newCommitID,
				ParentID:    parentID,
				TreeHash:    oldCommit.TreeHash,
				Message:     oldCommit.Message,
				AuthorName:  oldCommit.AuthorName,
				AuthorEmail: oldCommit.AuthorEmail,
				CreatedAt:   time.Now(), // New timestamp
			}

			if err := r.DB.CreateCommit(ctx, newCommit); err != nil {
				return fmt.Errorf("failed to replay commit: %w", err)
			}

			// Update blobs with new commit ID
			for _, blob := range oldBlobs {
				newBlob := &db.Blob{
					Path:          blob.Path,
					CommitID:      newCommitID,
					Content:       blob.Content,
					ContentHash:   blob.ContentHash,
					Mode:          blob.Mode,
					IsSymlink:     blob.IsSymlink,
					SymlinkTarget: blob.SymlinkTarget,
				}
				if err := r.DB.CreateBlob(ctx, newBlob); err != nil {
					return fmt.Errorf("failed to replay blob: %w", err)
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
	head, _ := r.DB.GetHeadCommit(ctx)
	if head != nil {
		tree, err := r.DB.GetTreeAtCommit(ctx, head.ID)
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
	fmt.Printf("%s Rebased %d commit(s)\n", styles.Successf("Successfully"), len(localToReplay))
	if head != nil {
		fmt.Printf("HEAD is now at %s\n", styles.Yellow(util.ShortID(head.ID)))
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
