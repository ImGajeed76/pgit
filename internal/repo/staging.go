package repo

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/imgajeed76/pgit/v2/internal/config"
	"github.com/imgajeed76/pgit/v2/internal/db"
	"github.com/imgajeed76/pgit/v2/internal/util"
)

// FileChange represents a change to a file
type FileChange struct {
	Path          string
	Status        ChangeStatus
	OldHash       string // Hash in last commit (empty if new)
	NewHash       string // Hash in working dir (empty if deleted)
	Mode          int
	IsSymlink     bool
	SymlinkTarget string
}

// ChangeStatus represents the status of a file change
type ChangeStatus int

const (
	StatusUnmodified ChangeStatus = iota
	StatusNew
	StatusModified
	StatusDeleted
)

func (s ChangeStatus) String() string {
	switch s {
	case StatusNew:
		return "new file"
	case StatusModified:
		return "modified"
	case StatusDeleted:
		return "deleted"
	default:
		return ""
	}
}

// Symbol returns the short symbol for the status
func (s ChangeStatus) Symbol() string {
	switch s {
	case StatusNew:
		return "A"
	case StatusModified:
		return "M"
	case StatusDeleted:
		return "D"
	default:
		return " "
	}
}

// GetWorkingTreeChanges compares the working directory with the last commit
func (r *Repository) GetWorkingTreeChanges(ctx context.Context) ([]FileChange, error) {
	// Get the current tree METADATA from the database (no content - much faster!)
	// We only need paths and content hashes for comparison
	var currentTree []*db.Blob
	if r.DB != nil {
		var err error
		currentTree, err = r.DB.GetCurrentTreeMetadata(ctx)
		if err != nil {
			return nil, err
		}
	}

	// Build a map of committed files
	committedFiles := make(map[string]*db.Blob)
	for _, blob := range currentTree {
		committedFiles[blob.Path] = blob
	}

	// Load ignore patterns
	ignorePatterns, err := r.LoadIgnorePatterns()
	if err != nil {
		return nil, err
	}

	// Scan working directory
	workingFiles := make(map[string]bool)
	var changes []FileChange

	err = filepath.WalkDir(r.Root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		// Get relative path
		relPath, err := r.RelPath(path)
		if err != nil {
			return err
		}

		// Skip .pgit directory
		if relPath == util.PgitDir || strings.HasPrefix(relPath, util.PgitDir+"/") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		// Check ignore patterns
		if ignorePatterns.IsIgnored(relPath, d.IsDir()) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		// Skip directories
		if d.IsDir() {
			return nil
		}

		workingFiles[relPath] = true

		// Check if file is a symlink
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		isSymlink := info.Mode()&os.ModeSymlink != 0

		// Compute hash of working file using BLAKE3 (hex for comparison)
		var newHash string
		var symlinkTarget string
		if isSymlink {
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}
			symlinkTarget = target
			newHash = util.HashBytesBlake3Hex([]byte(target))
		} else {
			newHash, err = util.HashFileBlake3Hex(path)
			if err != nil {
				return err
			}
		}

		mode := int(info.Mode().Perm())

		// Compare with committed version
		committed, exists := committedFiles[relPath]
		if !exists {
			// New file
			changes = append(changes, FileChange{
				Path:          relPath,
				Status:        StatusNew,
				NewHash:       newHash,
				Mode:          mode,
				IsSymlink:     isSymlink,
				SymlinkTarget: symlinkTarget,
			})
		} else {
			// Check if modified
			// ContentHash is []byte (BLAKE3), convert to hex for comparison
			oldHash := util.ContentHashToHex(committed.ContentHash)
			if oldHash != newHash {
				changes = append(changes, FileChange{
					Path:          relPath,
					Status:        StatusModified,
					OldHash:       oldHash,
					NewHash:       newHash,
					Mode:          mode,
					IsSymlink:     isSymlink,
					SymlinkTarget: symlinkTarget,
				})
			}
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	// Check for deleted files
	for path, blob := range committedFiles {
		if !workingFiles[path] {
			// Skip if file is ignored - it may exist but was skipped during scan
			if ignorePatterns.IsIgnored(path, false) {
				continue
			}

			// ContentHash is []byte (BLAKE3), convert to hex
			oldHash := util.ContentHashToHex(blob.ContentHash)
			changes = append(changes, FileChange{
				Path:    path,
				Status:  StatusDeleted,
				OldHash: oldHash,
				Mode:    blob.Mode,
			})
		}
	}

	return changes, nil
}

// GetStagedChanges returns changes that are staged for commit
func (r *Repository) GetStagedChanges(ctx context.Context) ([]FileChange, error) {
	idx, err := r.LoadIndex()
	if err != nil {
		return nil, err
	}

	var changes []FileChange
	for _, entry := range idx.List() {
		change := FileChange{
			Path: entry.Path,
		}
		switch entry.Status {
		case config.StatusAdded:
			change.Status = StatusNew
		case config.StatusModified:
			change.Status = StatusModified
		case config.StatusDeleted:
			change.Status = StatusDeleted
		}

		// Get current hash for new/modified files using BLAKE3
		if change.Status != StatusDeleted {
			absPath := r.AbsPath(entry.Path)
			info, err := os.Lstat(absPath)
			if err == nil {
				change.Mode = int(info.Mode().Perm())
				change.IsSymlink = info.Mode()&os.ModeSymlink != 0
				if change.IsSymlink {
					target, _ := os.Readlink(absPath)
					change.SymlinkTarget = target
					change.NewHash = util.HashBytesBlake3Hex([]byte(target))
				} else {
					change.NewHash, _ = util.HashFileBlake3Hex(absPath)
				}
			}
		}

		changes = append(changes, change)
	}

	return changes, nil
}

// GetUnstagedChanges returns changes that are not staged
func (r *Repository) GetUnstagedChanges(ctx context.Context) ([]FileChange, error) {
	allChanges, err := r.GetWorkingTreeChanges(ctx)
	if err != nil {
		return nil, err
	}

	idx, err := r.LoadIndex()
	if err != nil {
		return nil, err
	}

	var unstaged []FileChange
	for _, change := range allChanges {
		if _, staged := idx.Get(change.Path); !staged {
			unstaged = append(unstaged, change)
		}
	}

	return unstaged, nil
}

// StageFile adds a file to the staging area
func (r *Repository) StageFile(ctx context.Context, path string) error {
	idx, err := r.LoadIndex()
	if err != nil {
		return err
	}

	// Get absolute path
	absPath := r.AbsPath(path)

	// Check if file exists
	_, err = os.Lstat(absPath)
	fileExists := err == nil

	// Check if file exists in current tree (only need to check existence, not load all files)
	inTree := false
	if r.DB != nil {
		head, err := r.DB.GetHeadCommit(ctx)
		if err != nil {
			return err
		}
		if head != nil {
			// Use FileExistsInTree to check if file is tracked (fast, no content load)
			inTree, err = r.DB.FileExistsInTree(ctx, path, head.ID)
			if err != nil {
				return err
			}
		}
	}

	if fileExists {
		// Add or modify
		idx.Add(path, !inTree)
	} else if inTree {
		// Delete
		idx.Delete(path)
	} else {
		// File doesn't exist and wasn't tracked
		return util.ErrFileNotFound
	}

	return idx.Save(r.Root)
}

// UnstageFile removes a file from the staging area
func (r *Repository) UnstageFile(path string) error {
	idx, err := r.LoadIndex()
	if err != nil {
		return err
	}

	idx.Remove(path)
	return idx.Save(r.Root)
}

// StageAll stages all changes
func (r *Repository) StageAll(ctx context.Context) error {
	changes, err := r.GetWorkingTreeChanges(ctx)
	if err != nil {
		return err
	}

	idx, err := r.LoadIndex()
	if err != nil {
		return err
	}

	for _, change := range changes {
		switch change.Status {
		case StatusNew:
			idx.Add(change.Path, true)
		case StatusModified:
			idx.Add(change.Path, false)
		case StatusDeleted:
			idx.Delete(change.Path)
		}
	}

	return idx.Save(r.Root)
}

// UnstageAll unstages all files
func (r *Repository) UnstageAll() error {
	idx, err := r.LoadIndex()
	if err != nil {
		return err
	}

	idx.Clear()
	return idx.Save(r.Root)
}
