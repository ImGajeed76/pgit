package repo

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/imgajeed76/pgit/v4/internal/config"
	"github.com/imgajeed76/pgit/v4/internal/db"
	"github.com/imgajeed76/pgit/v4/internal/util"
	"github.com/jackc/pgx/v5"
)

// CommitOptions contains options for creating a commit
type CommitOptions struct {
	Message     string
	AuthorName  string
	AuthorEmail string
	Time        time.Time // If zero, use current time
}

// Commit creates a new commit from staged changes
func (r *Repository) Commit(ctx context.Context, opts CommitOptions) (*db.Commit, error) {
	// Load index
	idx, err := r.LoadIndex()
	if err != nil {
		return nil, err
	}

	if idx.IsEmpty() {
		return nil, util.ErrNothingStaged
	}

	// Get author info
	authorName := opts.AuthorName
	if authorName == "" {
		authorName = r.Config.GetUserName()
	}

	authorEmail := opts.AuthorEmail
	if authorEmail == "" {
		authorEmail = r.Config.GetUserEmail()
	}

	// Check for missing config
	var missingFields []string
	if authorName == "" {
		missingFields = append(missingFields, "user.name")
	}
	if authorEmail == "" {
		missingFields = append(missingFields, "user.email")
	}
	if len(missingFields) > 0 {
		return nil, &MissingConfigError{Fields: missingFields}
	}

	// Get timestamp
	commitTime := opts.Time
	if commitTime.IsZero() {
		commitTime = time.Now()
	}

	// Generate commit ID
	commitID := util.NewULIDWithTime(commitTime)

	// Get parent commit
	var parentID *string
	headID, err := r.DB.GetHead(ctx)
	if err != nil {
		return nil, err
	}
	if headID != "" {
		parentID = &headID
	}

	// Create blobs for staged files
	var blobs []*db.Blob
	var treeEntries []util.TreeEntry

	// First, get the current tree to carry forward unchanged files
	var currentTree []*db.Blob
	if headID != "" {
		currentTree, err = r.DB.GetTreeMetadataAtCommit(ctx, headID)
		if err != nil {
			return nil, err
		}
	}

	// Build map of current tree
	currentTreeMap := make(map[string]*db.Blob)
	for _, blob := range currentTree {
		currentTreeMap[blob.Path] = blob
	}

	// Track which files are being modified
	stagedPaths := make(map[string]bool)
	for _, entry := range idx.List() {
		stagedPaths[entry.Path] = true
	}

	// Process staged files
	for _, entry := range idx.List() {
		blob := &db.Blob{
			Path:     entry.Path,
			CommitID: commitID,
		}

		switch entry.Status {
		case config.StatusAdded, config.StatusModified:
			// Read file content
			absPath := r.AbsPath(entry.Path)
			info, err := os.Lstat(absPath)
			if err != nil {
				return nil, err
			}

			blob.Mode = int(info.Mode().Perm())
			blob.IsSymlink = info.Mode()&os.ModeSymlink != 0

			if blob.IsSymlink {
				target, err := os.Readlink(absPath)
				if err != nil {
					return nil, err
				}
				blob.SymlinkTarget = &target
				blob.Content = []byte(target)
				blob.ContentHash = util.HashBytesBlake3(blob.Content)
			} else {
				content, err := os.ReadFile(absPath)
				if err != nil {
					return nil, err
				}
				blob.Content = content
				blob.ContentHash = util.HashBytesBlake3(content)
			}

			// Add to tree entries
			treeEntries = append(treeEntries, util.TreeEntry{
				Mode:        blob.Mode,
				Path:        blob.Path,
				ContentHash: blob.ContentHash,
			})

		case config.StatusDeleted:
			// Mark as deleted (empty content, nil hash)
			blob.Content = []byte{}
			blob.ContentHash = nil
			blob.Mode = 0
		}

		blobs = append(blobs, blob)
	}

	// Add unchanged files from current tree to tree entries
	for path, blob := range currentTreeMap {
		if !stagedPaths[path] && blob.ContentHash != nil {
			treeEntries = append(treeEntries, util.TreeEntry{
				Mode:        blob.Mode,
				Path:        blob.Path,
				ContentHash: blob.ContentHash,
			})
		}
	}

	// Compute tree hash
	treeHash := util.ComputeTreeHash(treeEntries)

	// Create commit (committer = author for native pgit commits)
	commit := &db.Commit{
		ID:             commitID,
		ParentID:       parentID,
		TreeHash:       treeHash,
		Message:        opts.Message,
		AuthorName:     authorName,
		AuthorEmail:    authorEmail,
		AuthoredAt:     commitTime,
		CommitterName:  authorName,
		CommitterEmail: authorEmail,
		CommittedAt:    commitTime,
	}

	// Detect binary for each staged blob
	for _, blob := range blobs {
		if blob.Content != nil && blob.ContentHash != nil {
			blob.IsBinary = util.DetectBinary(blob.Content)
		}
	}

	// Create everything in a single transaction
	err = r.DB.WithTx(ctx, func(tx pgx.Tx) error {
		// Create commit first
		_, err := tx.Exec(ctx, `
			INSERT INTO pgit_commits (id, parent_id, tree_hash, message, author_name, author_email, authored_at, committer_name, committer_email, committed_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
			commit.ID, commit.ParentID, commit.TreeHash, commit.Message,
			commit.AuthorName, commit.AuthorEmail, commit.AuthoredAt,
			commit.CommitterName, commit.CommitterEmail, commit.CommittedAt)
		if err != nil {
			return err
		}

		// Create blobs using the new schema
		if err := r.DB.CreateBlobs(ctx, blobs); err != nil {
			return err
		}

		// Update HEAD
		_, err = tx.Exec(ctx, `
			INSERT INTO pgit_refs (name, commit_id) VALUES ('HEAD', $1)
			ON CONFLICT (name) DO UPDATE SET commit_id = EXCLUDED.commit_id`,
			commitID)
		return err
	})

	if err != nil {
		return nil, err
	}

	// Clear index
	idx.Clear()
	if err := idx.Save(r.Root); err != nil {
		return nil, err
	}

	return commit, nil
}

// MissingConfigError indicates missing configuration values
type MissingConfigError struct {
	Fields []string
}

func (e *MissingConfigError) Error() string {
	var sb strings.Builder
	sb.WriteString("missing configuration: ")
	sb.WriteString(strings.Join(e.Fields, ", "))
	sb.WriteString("\n\nPlease set with:\n")
	for _, field := range e.Fields {
		sb.WriteString(fmt.Sprintf("  pgit config %s \"Your Value\"\n", field))
	}
	return strings.TrimSuffix(sb.String(), "\n")
}
