package db

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// FileRef represents a file reference at a specific commit.
// This is the metadata-only representation without actual content.
type FileRef struct {
	GroupID       int32
	CommitID      string
	VersionID     int32
	ContentHash   []byte // 16 bytes BLAKE3, nil = deleted
	Mode          int
	IsSymlink     bool
	SymlinkTarget *string
}

// FileRefWithPath combines FileRef with resolved path.
// Used when returning results that need the actual file path.
type FileRefWithPath struct {
	Path          string
	GroupID       int32
	CommitID      string
	VersionID     int32
	ContentHash   []byte
	Mode          int
	IsSymlink     bool
	SymlinkTarget *string
}

// CreateFileRef inserts a new file reference.
func (db *DB) CreateFileRef(ctx context.Context, ref *FileRef) error {
	sql := `
	INSERT INTO pgit_file_refs (group_id, commit_id, version_id, content_hash, mode, is_symlink, symlink_target)
	VALUES ($1, $2, $3, $4, $5, $6, $7)`

	return db.Exec(ctx, sql,
		ref.GroupID, ref.CommitID, ref.VersionID, ref.ContentHash,
		ref.Mode, ref.IsSymlink, ref.SymlinkTarget)
}

// CreateFileRefTx inserts a new file reference within a transaction.
func (db *DB) CreateFileRefTx(ctx context.Context, tx pgx.Tx, ref *FileRef) error {
	sql := `
	INSERT INTO pgit_file_refs (group_id, commit_id, version_id, content_hash, mode, is_symlink, symlink_target)
	VALUES ($1, $2, $3, $4, $5, $6, $7)`

	_, err := tx.Exec(ctx, sql,
		ref.GroupID, ref.CommitID, ref.VersionID, ref.ContentHash,
		ref.Mode, ref.IsSymlink, ref.SymlinkTarget)
	return err
}

// CreateFileRefsBatch inserts multiple file references using COPY for speed.
func (db *DB) CreateFileRefsBatch(ctx context.Context, refs []*FileRef) error {
	if len(refs) == 0 {
		return nil
	}

	rows := make([][]interface{}, len(refs))
	for i, ref := range refs {
		rows[i] = []interface{}{
			ref.GroupID, ref.CommitID, ref.VersionID, ref.ContentHash,
			ref.Mode, ref.IsSymlink, ref.SymlinkTarget,
		}
	}

	_, err := db.pool.CopyFrom(
		ctx,
		pgx.Identifier{"pgit_file_refs"},
		[]string{"group_id", "commit_id", "version_id", "content_hash", "mode", "is_symlink", "symlink_target"},
		pgx.CopyFromRows(rows),
	)
	return err
}

// GetFileRef retrieves a specific file reference.
func (db *DB) GetFileRef(ctx context.Context, groupID int32, commitID string) (*FileRef, error) {
	sql := `
	SELECT group_id, commit_id, version_id, content_hash, mode, is_symlink, symlink_target
	FROM pgit_file_refs
	WHERE group_id = $1 AND commit_id = $2`

	ref := &FileRef{}
	err := db.QueryRow(ctx, sql, groupID, commitID).Scan(
		&ref.GroupID, &ref.CommitID, &ref.VersionID, &ref.ContentHash,
		&ref.Mode, &ref.IsSymlink, &ref.SymlinkTarget,
	)

	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	return ref, nil
}

// GetFileRefsAtCommit retrieves all file refs at a specific commit.
// This returns only files that were changed in that specific commit.
func (db *DB) GetFileRefsAtCommit(ctx context.Context, commitID string) ([]*FileRef, error) {
	sql := `
	SELECT group_id, commit_id, version_id, content_hash, mode, is_symlink, symlink_target
	FROM pgit_file_refs
	WHERE commit_id = $1
	ORDER BY group_id`

	rows, err := db.Query(ctx, sql, commitID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var refs []*FileRef
	for rows.Next() {
		ref := &FileRef{}
		if err := rows.Scan(
			&ref.GroupID, &ref.CommitID, &ref.VersionID, &ref.ContentHash,
			&ref.Mode, &ref.IsSymlink, &ref.SymlinkTarget,
		); err != nil {
			return nil, err
		}
		refs = append(refs, ref)
	}

	return refs, rows.Err()
}

// GetFileRefsAtCommitWithPaths retrieves file refs with resolved paths.
func (db *DB) GetFileRefsAtCommitWithPaths(ctx context.Context, commitID string) ([]*FileRefWithPath, error) {
	sql := `
	SELECT p.path, r.group_id, r.commit_id, r.version_id, r.content_hash, r.mode, r.is_symlink, r.symlink_target
	FROM pgit_file_refs r
	JOIN pgit_paths p ON p.group_id = r.group_id
	WHERE r.commit_id = $1
	ORDER BY p.path`

	rows, err := db.Query(ctx, sql, commitID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var refs []*FileRefWithPath
	for rows.Next() {
		ref := &FileRefWithPath{}
		if err := rows.Scan(
			&ref.Path, &ref.GroupID, &ref.CommitID, &ref.VersionID, &ref.ContentHash,
			&ref.Mode, &ref.IsSymlink, &ref.SymlinkTarget,
		); err != nil {
			return nil, err
		}
		refs = append(refs, ref)
	}

	return refs, rows.Err()
}

// GetTreeRefsAtCommit retrieves the full tree (latest version per path <= commitID).
// This is the core query for getting the state of the repository at a commit.
func (db *DB) GetTreeRefsAtCommit(ctx context.Context, commitID string) ([]*FileRef, error) {
	sql := `
	WITH latest_versions AS (
		SELECT DISTINCT ON (group_id)
			group_id, commit_id, version_id, content_hash, mode, is_symlink, symlink_target
		FROM pgit_file_refs
		WHERE commit_id <= $1
		ORDER BY group_id, commit_id DESC
	)
	SELECT group_id, commit_id, version_id, content_hash, mode, is_symlink, symlink_target
	FROM latest_versions
	WHERE content_hash IS NOT NULL
	ORDER BY group_id`

	rows, err := db.Query(ctx, sql, commitID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var refs []*FileRef
	for rows.Next() {
		ref := &FileRef{}
		if err := rows.Scan(
			&ref.GroupID, &ref.CommitID, &ref.VersionID, &ref.ContentHash,
			&ref.Mode, &ref.IsSymlink, &ref.SymlinkTarget,
		); err != nil {
			return nil, err
		}
		refs = append(refs, ref)
	}

	return refs, rows.Err()
}

// GetTreeRefsAtCommitWithPaths retrieves the full tree with resolved paths.
// This is a metadata-only query - no content is fetched.
func (db *DB) GetTreeRefsAtCommitWithPaths(ctx context.Context, commitID string) ([]*FileRefWithPath, error) {
	sql := `
	WITH latest_versions AS (
		SELECT DISTINCT ON (r.group_id)
			r.group_id, r.commit_id, r.version_id, r.content_hash, r.mode, r.is_symlink, r.symlink_target
		FROM pgit_file_refs r
		WHERE r.commit_id <= $1
		ORDER BY r.group_id, r.commit_id DESC
	)
	SELECT p.path, lv.group_id, lv.commit_id, lv.version_id, lv.content_hash, lv.mode, lv.is_symlink, lv.symlink_target
	FROM latest_versions lv
	JOIN pgit_paths p ON p.group_id = lv.group_id
	WHERE lv.content_hash IS NOT NULL
	ORDER BY p.path`

	rows, err := db.Query(ctx, sql, commitID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var refs []*FileRefWithPath
	for rows.Next() {
		ref := &FileRefWithPath{}
		if err := rows.Scan(
			&ref.Path, &ref.GroupID, &ref.CommitID, &ref.VersionID, &ref.ContentHash,
			&ref.Mode, &ref.IsSymlink, &ref.SymlinkTarget,
		); err != nil {
			return nil, err
		}
		refs = append(refs, ref)
	}

	return refs, rows.Err()
}

// GetFileRefHistory retrieves all versions of a file by group_id.
func (db *DB) GetFileRefHistory(ctx context.Context, groupID int32) ([]*FileRef, error) {
	sql := `
	SELECT group_id, commit_id, version_id, content_hash, mode, is_symlink, symlink_target
	FROM pgit_file_refs
	WHERE group_id = $1
	ORDER BY commit_id DESC`

	rows, err := db.Query(ctx, sql, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var refs []*FileRef
	for rows.Next() {
		ref := &FileRef{}
		if err := rows.Scan(
			&ref.GroupID, &ref.CommitID, &ref.VersionID, &ref.ContentHash,
			&ref.Mode, &ref.IsSymlink, &ref.SymlinkTarget,
		); err != nil {
			return nil, err
		}
		refs = append(refs, ref)
	}

	return refs, rows.Err()
}

// GetFileRefAtCommit retrieves a file ref at or before a specific commit.
func (db *DB) GetFileRefAtCommit(ctx context.Context, groupID int32, commitID string) (*FileRef, error) {
	sql := `
	SELECT group_id, commit_id, version_id, content_hash, mode, is_symlink, symlink_target
	FROM pgit_file_refs
	WHERE group_id = $1 AND commit_id <= $2
	ORDER BY commit_id DESC
	LIMIT 1`

	ref := &FileRef{}
	err := db.QueryRow(ctx, sql, groupID, commitID).Scan(
		&ref.GroupID, &ref.CommitID, &ref.VersionID, &ref.ContentHash,
		&ref.Mode, &ref.IsSymlink, &ref.SymlinkTarget,
	)

	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	// Check if file was deleted
	if ref.ContentHash == nil {
		return nil, nil
	}

	return ref, nil
}

// GetNextVersionID returns the next version_id for a group.
// This is used when creating new file refs.
func (db *DB) GetNextVersionID(ctx context.Context, groupID int32) (int32, error) {
	var maxVersion *int32
	err := db.QueryRow(ctx,
		"SELECT MAX(version_id) FROM pgit_file_refs WHERE group_id = $1",
		groupID,
	).Scan(&maxVersion)

	if err != nil {
		return 0, err
	}

	if maxVersion == nil {
		return 1, nil
	}

	return *maxVersion + 1, nil
}

// GetNextVersionIDTx returns the next version_id within a transaction.
func (db *DB) GetNextVersionIDTx(ctx context.Context, tx pgx.Tx, groupID int32) (int32, error) {
	var maxVersion *int32
	err := tx.QueryRow(ctx,
		"SELECT MAX(version_id) FROM pgit_file_refs WHERE group_id = $1",
		groupID,
	).Scan(&maxVersion)

	if err != nil {
		return 0, err
	}

	if maxVersion == nil {
		return 1, nil
	}

	return *maxVersion + 1, nil
}

// GetChangedFileRefs returns file refs that changed between two commits.
func (db *DB) GetChangedFileRefs(ctx context.Context, fromCommit, toCommit string) ([]*FileRef, error) {
	sql := `
	SELECT group_id, commit_id, version_id, content_hash, mode, is_symlink, symlink_target
	FROM pgit_file_refs
	WHERE commit_id > $1 AND commit_id <= $2
	ORDER BY group_id, commit_id`

	rows, err := db.Query(ctx, sql, fromCommit, toCommit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var refs []*FileRef
	for rows.Next() {
		ref := &FileRef{}
		if err := rows.Scan(
			&ref.GroupID, &ref.CommitID, &ref.VersionID, &ref.ContentHash,
			&ref.Mode, &ref.IsSymlink, &ref.SymlinkTarget,
		); err != nil {
			return nil, err
		}
		refs = append(refs, ref)
	}

	return refs, rows.Err()
}

// GetChangedFileRefsWithPaths returns file refs with paths that changed between two commits.
func (db *DB) GetChangedFileRefsWithPaths(ctx context.Context, fromCommit, toCommit string) ([]*FileRefWithPath, error) {
	sql := `
	SELECT p.path, r.group_id, r.commit_id, r.version_id, r.content_hash, r.mode, r.is_symlink, r.symlink_target
	FROM pgit_file_refs r
	JOIN pgit_paths p ON p.group_id = r.group_id
	WHERE r.commit_id > $1 AND r.commit_id <= $2
	ORDER BY p.path, r.commit_id`

	rows, err := db.Query(ctx, sql, fromCommit, toCommit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var refs []*FileRefWithPath
	for rows.Next() {
		ref := &FileRefWithPath{}
		if err := rows.Scan(
			&ref.Path, &ref.GroupID, &ref.CommitID, &ref.VersionID, &ref.ContentHash,
			&ref.Mode, &ref.IsSymlink, &ref.SymlinkTarget,
		); err != nil {
			return nil, err
		}
		refs = append(refs, ref)
	}

	return refs, rows.Err()
}

// CountFileRefs returns the total number of file refs.
func (db *DB) CountFileRefs(ctx context.Context) (int64, error) {
	var count int64
	err := db.QueryRow(ctx, "SELECT COUNT(*) FROM pgit_file_refs").Scan(&count)
	return count, err
}

// FileRefExists checks if a file ref exists at a specific commit.
func (db *DB) FileRefExists(ctx context.Context, groupID int32, commitID string) (bool, error) {
	var exists bool
	err := db.QueryRow(ctx,
		"SELECT EXISTS(SELECT 1 FROM pgit_file_refs WHERE group_id = $1 AND commit_id = $2)",
		groupID, commitID).Scan(&exists)
	return exists, err
}
