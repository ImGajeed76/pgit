package db

import (
	"context"
	"strings"

	"github.com/jackc/pgx/v5"
)

// Blob represents a file at a specific commit
type Blob struct {
	Path          string
	CommitID      string
	Content       []byte  // file bytes (empty for empty files)
	ContentHash   *string // nil = deleted, otherwise hash of content
	Mode          int
	IsSymlink     bool
	SymlinkTarget *string
}

// CreateBlob inserts a new blob into the database
func (db *DB) CreateBlob(ctx context.Context, b *Blob) error {
	sql := `
	INSERT INTO pgit_blobs (path, commit_id, content, content_hash, mode, is_symlink, symlink_target)
	VALUES ($1, $2, $3, $4, $5, $6, $7)`

	// Ensure content is never nil (delta columns can't be NULL)
	content := b.Content
	if content == nil {
		content = []byte{}
	}

	return db.Exec(ctx, sql, b.Path, b.CommitID, content, b.ContentHash, b.Mode, b.IsSymlink, b.SymlinkTarget)
}

// CreateBlobs inserts multiple blobs using COPY for speed
func (db *DB) CreateBlobs(ctx context.Context, blobs []*Blob) error {
	if len(blobs) == 0 {
		return nil
	}

	rows := make([][]interface{}, len(blobs))
	for i, b := range blobs {
		// Ensure content is never nil (delta columns can't be NULL)
		content := b.Content
		if content == nil {
			content = []byte{}
		}
		rows[i] = []interface{}{
			b.Path, b.CommitID, content, b.ContentHash,
			b.Mode, b.IsSymlink, b.SymlinkTarget,
		}
	}

	_, err := db.pool.CopyFrom(
		ctx,
		pgx.Identifier{"pgit_blobs"},
		[]string{"path", "commit_id", "content", "content_hash", "mode", "is_symlink", "symlink_target"},
		pgx.CopyFromRows(rows),
	)
	return err
}

// GetBlob retrieves a specific blob
func (db *DB) GetBlob(ctx context.Context, path, commitID string) (*Blob, error) {
	sql := `
	SELECT path, commit_id, content, content_hash, mode, is_symlink, symlink_target
	FROM pgit_blobs
	WHERE path = $1 AND commit_id = $2`

	b := &Blob{}
	err := db.QueryRow(ctx, sql, path, commitID).Scan(
		&b.Path, &b.CommitID, &b.Content, &b.ContentHash,
		&b.Mode, &b.IsSymlink, &b.SymlinkTarget,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return b, nil
}

// GetBlobsAtCommit retrieves all blobs at a specific commit
func (db *DB) GetBlobsAtCommit(ctx context.Context, commitID string) ([]*Blob, error) {
	sql := `
	SELECT path, commit_id, content, content_hash, mode, is_symlink, symlink_target
	FROM pgit_blobs
	WHERE commit_id = $1
	ORDER BY path`

	rows, err := db.Query(ctx, sql, commitID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var blobs []*Blob
	for rows.Next() {
		b := &Blob{}
		if err := rows.Scan(
			&b.Path, &b.CommitID, &b.Content, &b.ContentHash,
			&b.Mode, &b.IsSymlink, &b.SymlinkTarget,
		); err != nil {
			return nil, err
		}
		blobs = append(blobs, b)
	}

	return blobs, rows.Err()
}

// GetTreeAtCommit retrieves the full tree (all files) at a commit
// This uses the ULID ordering to find the latest version of each file
func (db *DB) GetTreeAtCommit(ctx context.Context, commitID string) ([]*Blob, error) {
	sql := `
	WITH latest_versions AS (
		SELECT DISTINCT ON (path) 
			path, commit_id, content, content_hash, mode, is_symlink, symlink_target
		FROM pgit_blobs
		WHERE commit_id <= $1
		ORDER BY path, commit_id DESC
	)
	SELECT path, commit_id, content, content_hash, mode, is_symlink, symlink_target
	FROM latest_versions
	WHERE content_hash IS NOT NULL
	ORDER BY path`

	rows, err := db.Query(ctx, sql, commitID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var blobs []*Blob
	for rows.Next() {
		b := &Blob{}
		if err := rows.Scan(
			&b.Path, &b.CommitID, &b.Content, &b.ContentHash,
			&b.Mode, &b.IsSymlink, &b.SymlinkTarget,
		); err != nil {
			return nil, err
		}
		blobs = append(blobs, b)
	}

	return blobs, rows.Err()
}

// GetCurrentTree retrieves the tree at HEAD
func (db *DB) GetCurrentTree(ctx context.Context) ([]*Blob, error) {
	head, err := db.GetHeadCommit(ctx)
	if err != nil {
		return nil, err
	}
	if head == nil {
		return nil, nil // No commits yet
	}
	return db.GetTreeAtCommit(ctx, head.ID)
}

// GetFileHistory retrieves all versions of a file
func (db *DB) GetFileHistory(ctx context.Context, path string) ([]*Blob, error) {
	sql := `
	SELECT b.path, b.commit_id, b.content, b.content_hash, b.mode, b.is_symlink, b.symlink_target
	FROM pgit_blobs b
	WHERE b.path = $1
	ORDER BY b.commit_id DESC`

	rows, err := db.Query(ctx, sql, path)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var blobs []*Blob
	for rows.Next() {
		b := &Blob{}
		if err := rows.Scan(
			&b.Path, &b.CommitID, &b.Content, &b.ContentHash,
			&b.Mode, &b.IsSymlink, &b.SymlinkTarget,
		); err != nil {
			return nil, err
		}
		blobs = append(blobs, b)
	}

	return blobs, rows.Err()
}

// GetFileAtCommit retrieves a file at a specific commit (or the latest version before it)
func (db *DB) GetFileAtCommit(ctx context.Context, path, commitID string) (*Blob, error) {
	sql := `
	SELECT path, commit_id, content, content_hash, mode, is_symlink, symlink_target
	FROM pgit_blobs
	WHERE path = $1 AND commit_id <= $2
	ORDER BY commit_id DESC
	LIMIT 1`

	b := &Blob{}
	err := db.QueryRow(ctx, sql, path, commitID).Scan(
		&b.Path, &b.CommitID, &b.Content, &b.ContentHash,
		&b.Mode, &b.IsSymlink, &b.SymlinkTarget,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	// Check if file was deleted
	if b.ContentHash == nil {
		return nil, nil
	}

	return b, nil
}

// GetChangedFiles returns files that changed between two commits
func (db *DB) GetChangedFiles(ctx context.Context, fromCommit, toCommit string) ([]*Blob, error) {
	sql := `
	SELECT path, commit_id, content, content_hash, mode, is_symlink, symlink_target
	FROM pgit_blobs
	WHERE commit_id > $1 AND commit_id <= $2
	ORDER BY path, commit_id`

	rows, err := db.Query(ctx, sql, fromCommit, toCommit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var blobs []*Blob
	for rows.Next() {
		b := &Blob{}
		if err := rows.Scan(
			&b.Path, &b.CommitID, &b.Content, &b.ContentHash,
			&b.Mode, &b.IsSymlink, &b.SymlinkTarget,
		); err != nil {
			return nil, err
		}
		blobs = append(blobs, b)
	}

	return blobs, rows.Err()
}

// GetAllPaths returns all unique file paths in the repository
func (db *DB) GetAllPaths(ctx context.Context) ([]string, error) {
	sql := `SELECT DISTINCT path FROM pgit_blobs ORDER BY path`

	rows, err := db.Query(ctx, sql)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var paths []string
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			return nil, err
		}
		paths = append(paths, path)
	}

	return paths, rows.Err()
}

// BlobExists checks if a blob exists at a specific commit
func (db *DB) BlobExists(ctx context.Context, path, commitID string) (bool, error) {
	var exists bool
	err := db.QueryRow(ctx,
		"SELECT EXISTS(SELECT 1 FROM pgit_blobs WHERE path = $1 AND commit_id = $2)",
		path, commitID).Scan(&exists)
	return exists, err
}

// SearchAllBlobs retrieves all blobs, optionally filtered by path pattern
func (db *DB) SearchAllBlobs(ctx context.Context, pathPattern string) ([]*Blob, error) {
	var sql string
	var args []interface{}

	if pathPattern != "" {
		// Convert glob to SQL LIKE pattern
		likePattern := pathPattern
		likePattern = strings.ReplaceAll(likePattern, "*", "%")
		likePattern = strings.ReplaceAll(likePattern, "?", "_")

		sql = `
		SELECT path, commit_id, content, content_hash, mode, is_symlink, symlink_target
		FROM pgit_blobs
		WHERE content_hash IS NOT NULL AND path LIKE $1
		ORDER BY path, commit_id DESC`
		args = []interface{}{likePattern}
	} else {
		sql = `
		SELECT path, commit_id, content, content_hash, mode, is_symlink, symlink_target
		FROM pgit_blobs
		WHERE content_hash IS NOT NULL
		ORDER BY path, commit_id DESC`
	}

	rows, err := db.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var blobs []*Blob
	for rows.Next() {
		b := &Blob{}
		if err := rows.Scan(
			&b.Path, &b.CommitID, &b.Content, &b.ContentHash,
			&b.Mode, &b.IsSymlink, &b.SymlinkTarget,
		); err != nil {
			return nil, err
		}
		blobs = append(blobs, b)
	}

	return blobs, rows.Err()
}
