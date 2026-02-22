package db

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/imgajeed76/pgit/v4/internal/util"
	"github.com/jackc/pgx/v5"
)

// Blob represents a file at a specific commit.
// This is the compatibility layer that combines data from pgit_paths,
// pgit_file_refs, and pgit_text_content/pgit_binary_content tables in schema v3.
type Blob struct {
	Path          string
	CommitID      string
	Content       []byte // file bytes (empty for empty files)
	ContentHash   []byte // 16 bytes BLAKE3, nil = deleted
	Mode          int
	IsSymlink     bool
	SymlinkTarget *string
	IsBinary      bool
}

// IsDeleted returns true if this blob represents a deletion.
func (b *Blob) IsDeleted() bool {
	return b.ContentHash == nil
}

// CreateBlob inserts a new blob into the database.
// This writes to pgit_paths, pgit_file_refs, and pgit_text_content or pgit_binary_content.
func (db *DB) CreateBlob(ctx context.Context, b *Blob) error {
	return db.WithTx(ctx, func(tx pgx.Tx) error {
		return db.createBlobTx(ctx, tx, b)
	})
}

// createBlobTx inserts a blob within a transaction.
// For single-blob inserts (commit, add), paths get their own group (pathID == groupID).
func (db *DB) createBlobTx(ctx context.Context, tx pgx.Tx, b *Blob) error {
	// 1. Get or create path -> (pathID, groupID)
	// For non-import operations, each path gets its own group.
	// Pass groupID=0 as placeholder; GetOrCreatePathTx will return existing group
	// if path exists, or we handle it below.
	pathID, groupID, err := db.GetOrCreatePathTx(ctx, tx, b.Path, 0)
	if err != nil {
		return err
	}

	// If this is a new path (groupID=0 from placeholder), assign pathID as groupID
	if groupID == 0 {
		// Update the path's group_id to be its own path_id (singleton group)
		_, err = tx.Exec(ctx,
			"UPDATE pgit_paths SET group_id = $1 WHERE path_id = $2",
			pathID, pathID,
		)
		if err != nil {
			return err
		}
		groupID = pathID
	}

	// 2. Get next version_id for this group
	versionID, err := db.GetNextVersionIDTx(ctx, tx, groupID)
	if err != nil {
		return err
	}

	// 3. Create file ref
	ref := &FileRef{
		PathID:        pathID,
		CommitID:      b.CommitID,
		VersionID:     versionID,
		ContentHash:   b.ContentHash,
		Mode:          b.Mode,
		IsSymlink:     b.IsSymlink,
		SymlinkTarget: b.SymlinkTarget,
		IsBinary:      b.IsBinary,
	}
	if err := db.CreateFileRefTx(ctx, tx, ref); err != nil {
		return err
	}

	// 4. Create content (skip for deletions — only file ref needed)
	if b.ContentHash != nil {
		content := &Content{
			GroupID:   groupID,
			VersionID: versionID,
			Content:   b.Content,
			IsBinary:  b.IsBinary,
		}
		if err := db.CreateContentTx(ctx, tx, content); err != nil {
			return err
		}
	}

	return nil
}

// CreateBlobs inserts multiple blobs efficiently.
// Blobs should be pre-sorted by path for optimal delta compression.
func (db *DB) CreateBlobs(ctx context.Context, blobs []*Blob) error {
	if len(blobs) == 0 {
		return nil
	}

	return db.WithTx(ctx, func(tx pgx.Tx) error {
		return db.createBlobsBatchTx(ctx, tx, blobs)
	})
}

// CreateBlobsTx inserts multiple blobs within an existing transaction.
// Same logic as CreateBlobs but uses the provided tx instead of creating its own.
func (db *DB) CreateBlobsTx(ctx context.Context, tx pgx.Tx, blobs []*Blob) error {
	return db.createBlobsBatchTx(ctx, tx, blobs)
}

// createBlobsBatchTx is the shared implementation for batch blob insertion.
func (db *DB) createBlobsBatchTx(ctx context.Context, tx pgx.Tx, blobs []*Blob) error {
	if len(blobs) == 0 {
		return nil
	}

	// Collect all unique paths
	pathSet := make(map[string]bool)
	for _, b := range blobs {
		pathSet[b.Path] = true
	}
	paths := make([]string, 0, len(pathSet))
	for p := range pathSet {
		paths = append(paths, p)
	}

	// Batch get/create paths (returns PathIDs with both pathID and groupID)
	pathReg, err := db.getOrCreatePathsBatchTx(ctx, tx, paths)
	if err != nil {
		return err
	}

	// Group blobs by path to assign sequential version IDs
	blobsByPath := make(map[string][]*Blob)
	for _, b := range blobs {
		blobsByPath[b.Path] = append(blobsByPath[b.Path], b)
	}

	// Get current max version_id for each group
	groupIDs := make([]int32, 0, len(pathReg))
	seen := make(map[int32]bool)
	for _, ids := range pathReg {
		if !seen[ids.GroupID] {
			seen[ids.GroupID] = true
			groupIDs = append(groupIDs, ids.GroupID)
		}
	}
	maxVersions, err := db.getMaxVersionIDsBatchTx(ctx, tx, groupIDs)
	if err != nil {
		return err
	}

	// Prepare batch inserts
	var fileRefs []*FileRef
	var contents []*Content

	for path, pathBlobs := range blobsByPath {
		ids := pathReg[path]
		baseVersion := maxVersions[ids.GroupID]

		for i, b := range pathBlobs {
			versionID := baseVersion + int32(i) + 1

			fileRefs = append(fileRefs, &FileRef{
				PathID:        ids.PathID,
				CommitID:      b.CommitID,
				VersionID:     versionID,
				ContentHash:   b.ContentHash,
				Mode:          b.Mode,
				IsSymlink:     b.IsSymlink,
				SymlinkTarget: b.SymlinkTarget,
				IsBinary:      b.IsBinary,
			})

			if b.ContentHash != nil {
				contents = append(contents, &Content{
					GroupID:   ids.GroupID,
					VersionID: versionID,
					Content:   b.Content,
					IsBinary:  b.IsBinary,
				})
			}
		}
	}

	if err := db.createFileRefsBatchTx(ctx, tx, fileRefs); err != nil {
		return err
	}
	if err := db.createContentsBatchTx(ctx, tx, contents); err != nil {
		return err
	}

	return nil
}

// DeleteBlobsForCommits removes all file_refs and content data for the given
// commit IDs. For xpatch content tables, it truncates each affected file's
// chain at the lowest version_id being removed (xpatch cascade-deletes the rest).
// This must be called BEFORE DeleteCommits since we need the commit data
// to identify which content versions to clean up.
func (db *DB) DeleteBlobsForCommits(ctx context.Context, commitIDs []string) error {
	if len(commitIDs) == 0 {
		return nil
	}

	// Step 1: Get (group_id, version_id, is_binary) for all file_refs being removed.
	// This tells us which content chain entries to truncate.
	type versionInfo struct {
		groupID   int32
		versionID int32
		isBinary  bool
	}

	// Find the minimum version_id per group among the commits being deleted.
	// Deleting that version from the xpatch content table cascades to all later versions.
	minVersionByGroup := make(map[int32]versionInfo) // group_id -> lowest versionInfo to delete

	for _, cid := range commitIDs {
		rows, err := db.Query(ctx,
			`SELECT p.group_id, r.version_id, r.is_binary
			 FROM pgit_file_refs r
			 JOIN pgit_paths p ON p.path_id = r.path_id
			 WHERE r.commit_id = $1`, cid)
		if err != nil {
			return fmt.Errorf("failed to query file_refs for commit %s: %w", cid, err)
		}

		for rows.Next() {
			var vi versionInfo
			if err := rows.Scan(&vi.groupID, &vi.versionID, &vi.isBinary); err != nil {
				rows.Close()
				return err
			}
			if existing, ok := minVersionByGroup[vi.groupID]; !ok || vi.versionID < existing.versionID {
				minVersionByGroup[vi.groupID] = vi
			}
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
	}

	// Step 2: Truncate content chains. For each group, delete the row at the
	// minimum version_id — xpatch cascades to all subsequent versions.
	for _, vi := range minVersionByGroup {
		table := "pgit_text_content"
		if vi.isBinary {
			table = "pgit_binary_content"
		}
		// Only delete if content exists (deleted files have no content row)
		var exists bool
		err := db.QueryRow(ctx,
			fmt.Sprintf("SELECT EXISTS(SELECT 1 FROM %s WHERE group_id = $1 AND version_id = $2)", table),
			vi.groupID, vi.versionID,
		).Scan(&exists)
		if err != nil {
			return err
		}
		if exists {
			if err := db.Exec(ctx,
				fmt.Sprintf("DELETE FROM %s WHERE group_id = $1 AND version_id = $2", table),
				vi.groupID, vi.versionID,
			); err != nil {
				return fmt.Errorf("failed to truncate content for group %d: %w", vi.groupID, err)
			}
		}
	}

	// Step 3: Delete file_refs (normal heap table, simple delete)
	for _, cid := range commitIDs {
		if err := db.Exec(ctx, "DELETE FROM pgit_file_refs WHERE commit_id = $1", cid); err != nil {
			return fmt.Errorf("failed to delete file_refs for commit %s: %w", cid, err)
		}
	}

	// Note: xpatch stats for content tables become stale after deletion.
	// We don't refresh them here since refresh_stats() is table-wide (scans
	// all groups) and content tables have thousands of groups. The stats
	// are only used for display (pgit stats) and self-correct on next refresh.
	// The caller should refresh pgit_commits stats separately (single group, cheap).

	return nil
}

// getOrCreatePathsBatchTx handles multiple paths within a transaction.
// For non-import operations, each new path gets its own group (singleton).
// Returns a map of path -> PathIDs.
func (db *DB) getOrCreatePathsBatchTx(ctx context.Context, tx pgx.Tx, paths []string) (map[string]PathIDs, error) {
	if len(paths) == 0 {
		return make(map[string]PathIDs), nil
	}

	result := make(map[string]PathIDs, len(paths))

	// First, try to get all existing paths
	rows, err := tx.Query(ctx,
		"SELECT path_id, group_id, path FROM pgit_paths WHERE path = ANY($1)",
		paths,
	)
	if err != nil {
		return nil, err
	}

	for rows.Next() {
		var pathID, groupID int32
		var path string
		if err := rows.Scan(&pathID, &groupID, &path); err != nil {
			rows.Close()
			return nil, err
		}
		result[path] = PathIDs{PathID: pathID, GroupID: groupID}
	}
	rows.Close()

	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Insert missing paths (each new path gets its own singleton group)
	// First, get max group_id for assigning new groups
	var maxGroupID int32
	err = tx.QueryRow(ctx, "SELECT COALESCE(MAX(group_id), 0) FROM pgit_paths").Scan(&maxGroupID)
	if err != nil {
		return nil, err
	}
	nextGroupID := maxGroupID + 1

	for _, path := range paths {
		if _, exists := result[path]; !exists {
			var pathID, groupID int32
			err := tx.QueryRow(ctx,
				`INSERT INTO pgit_paths (group_id, path) VALUES ($1, $2)
				 ON CONFLICT (path) DO UPDATE SET path = EXCLUDED.path
				 RETURNING path_id, group_id`,
				nextGroupID, path,
			).Scan(&pathID, &groupID)
			if err != nil {
				return nil, err
			}
			result[path] = PathIDs{PathID: pathID, GroupID: groupID}
			nextGroupID++
		}
	}

	return result, nil
}

// getMaxVersionIDsBatchTx gets max version_id for each group_id.
// In v4, file_refs no longer has group_id, so we JOIN through pgit_paths.
func (db *DB) getMaxVersionIDsBatchTx(ctx context.Context, tx pgx.Tx, groupIDs []int32) (map[int32]int32, error) {
	result := make(map[int32]int32)

	if len(groupIDs) == 0 {
		return result, nil
	}

	rows, err := tx.Query(ctx,
		`SELECT p.group_id, COALESCE(MAX(r.version_id), 0)
		 FROM pgit_paths p
		 LEFT JOIN pgit_file_refs r ON r.path_id = p.path_id
		 WHERE p.group_id = ANY($1)
		 GROUP BY p.group_id`,
		groupIDs,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var groupID, maxVersion int32
		if err := rows.Scan(&groupID, &maxVersion); err != nil {
			return nil, err
		}
		result[groupID] = maxVersion
	}

	// Set 0 for groups that don't exist yet
	for _, gid := range groupIDs {
		if _, exists := result[gid]; !exists {
			result[gid] = 0
		}
	}

	return result, rows.Err()
}

// createFileRefsBatchTx inserts file refs using COPY within a transaction.
func (db *DB) createFileRefsBatchTx(ctx context.Context, tx pgx.Tx, refs []*FileRef) error {
	if len(refs) == 0 {
		return nil
	}

	rows := make([][]interface{}, len(refs))
	for i, ref := range refs {
		rows[i] = []interface{}{
			ref.PathID, ref.CommitID, ref.VersionID, ref.ContentHash,
			ref.Mode, ref.IsSymlink, ref.SymlinkTarget, ref.IsBinary,
		}
	}

	_, err := tx.CopyFrom(
		ctx,
		pgx.Identifier{"pgit_file_refs"},
		[]string{"path_id", "commit_id", "version_id", "content_hash", "mode", "is_symlink", "symlink_target", "is_binary"},
		pgx.CopyFromRows(rows),
	)
	return err
}

// createContentsBatchTx inserts contents using COPY within a transaction.
// Splits into text and binary batches and writes to respective tables.
func (db *DB) createContentsBatchTx(ctx context.Context, tx pgx.Tx, contents []*Content) error {
	if len(contents) == 0 {
		return nil
	}

	// Split into text and binary
	var textContents []*Content
	var binaryContents []*Content
	for _, c := range contents {
		if c.IsBinary {
			binaryContents = append(binaryContents, c)
		} else {
			textContents = append(textContents, c)
		}
	}

	// Insert text content
	if len(textContents) > 0 {
		rows := make([][]interface{}, len(textContents))
		for i, c := range textContents {
			content := c.Content
			if content == nil {
				content = []byte{}
			}
			rows[i] = []interface{}{c.GroupID, c.VersionID, util.ToValidUTF8(string(content))}
		}

		_, err := tx.CopyFrom(
			ctx,
			pgx.Identifier{"pgit_text_content"},
			[]string{"group_id", "version_id", "content"},
			pgx.CopyFromRows(rows),
		)
		if err != nil {
			return err
		}
	}

	// Insert binary content
	if len(binaryContents) > 0 {
		rows := make([][]interface{}, len(binaryContents))
		for i, c := range binaryContents {
			content := c.Content
			if content == nil {
				content = []byte{}
			}
			rows[i] = []interface{}{c.GroupID, c.VersionID, content}
		}

		_, err := tx.CopyFrom(
			ctx,
			pgx.Identifier{"pgit_binary_content"},
			[]string{"group_id", "version_id", "content"},
			pgx.CopyFromRows(rows),
		)
		if err != nil {
			return err
		}
	}

	return nil
}

// GetBlob retrieves a specific blob by path and commit.
func (db *DB) GetBlob(ctx context.Context, path, commitID string) (*Blob, error) {
	// Get (pathID, groupID) for path
	pathID, groupID, err := db.GetPathIDAndGroupIDByPath(ctx, path)
	if err != nil {
		return nil, err
	}
	if pathID == 0 {
		return nil, nil // Path doesn't exist
	}

	// Get file ref by pathID
	ref, err := db.GetFileRef(ctx, pathID, commitID)
	if err != nil {
		return nil, err
	}
	if ref == nil {
		return nil, nil
	}

	// Get content from the correct table based on is_binary
	// Content is keyed by (group_id, version_id)
	content, err := db.GetContent(ctx, groupID, ref.VersionID, ref.IsBinary)
	if err != nil {
		return nil, err
	}

	return &Blob{
		Path:          path,
		CommitID:      ref.CommitID,
		Content:       content,
		ContentHash:   ref.ContentHash,
		Mode:          ref.Mode,
		IsSymlink:     ref.IsSymlink,
		SymlinkTarget: ref.SymlinkTarget,
		IsBinary:      ref.IsBinary,
	}, nil
}

// GetBlobsAtCommit retrieves all blobs at a specific commit.
// This returns only files that were changed in that specific commit.
// Uses a two-step approach: get refs, then batch-fetch content from both tables.
func (db *DB) GetBlobsAtCommit(ctx context.Context, commitID string) ([]*Blob, error) {
	// Step 1: Get file refs with paths
	refs, err := db.GetFileRefsAtCommitWithPaths(ctx, commitID)
	if err != nil {
		return nil, err
	}

	if len(refs) == 0 {
		return nil, nil
	}

	// Step 2: Batch fetch content (only for non-deleted refs)
	keys := make([]ContentKey, 0, len(refs))
	isBinaryMap := make(map[ContentKey]bool)
	for _, ref := range refs {
		if ref.ContentHash == nil {
			continue // deleted — no content row exists
		}
		k := ContentKey{GroupID: ref.GroupID, VersionID: ref.VersionID}
		keys = append(keys, k)
		if ref.IsBinary {
			isBinaryMap[k] = true
		}
	}

	contentMap, err := db.GetContentsBatch(ctx, keys, isBinaryMap)
	if err != nil {
		return nil, err
	}

	// Step 3: Build blobs
	blobs := make([]*Blob, len(refs))
	for i, ref := range refs {
		key := ContentKey{GroupID: ref.GroupID, VersionID: ref.VersionID}
		blobs[i] = &Blob{
			Path:          ref.Path,
			CommitID:      ref.CommitID,
			Content:       contentMap[key],
			ContentHash:   ref.ContentHash,
			Mode:          ref.Mode,
			IsSymlink:     ref.IsSymlink,
			SymlinkTarget: ref.SymlinkTarget,
			IsBinary:      ref.IsBinary,
		}
	}

	return blobs, nil
}

// GetTreeAtCommit retrieves the full tree (all files) at a commit.
// Uses a two-step approach: get refs with DISTINCT ON, then batch-fetch content.
func (db *DB) GetTreeAtCommit(ctx context.Context, commitID string) ([]*Blob, error) {
	// Step 1: Get tree refs with paths and is_binary
	sql := `
	SELECT DISTINCT ON (r.path_id)
		p.path, r.commit_id, r.content_hash, r.mode, r.is_symlink, r.symlink_target,
		p.group_id, r.version_id, r.is_binary
	FROM pgit_file_refs r
	JOIN pgit_paths p ON p.path_id = r.path_id
	WHERE r.commit_id <= $1
	ORDER BY r.path_id, r.commit_id DESC`

	rows, err := db.Query(ctx, sql, commitID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	type treeEntry struct {
		blob      *Blob
		groupID   int32
		versionID int32
	}
	var entries []treeEntry

	for rows.Next() {
		var e treeEntry
		e.blob = &Blob{}
		if err := rows.Scan(
			&e.blob.Path, &e.blob.CommitID, &e.blob.ContentHash,
			&e.blob.Mode, &e.blob.IsSymlink, &e.blob.SymlinkTarget,
			&e.groupID, &e.versionID, &e.blob.IsBinary,
		); err != nil {
			return nil, err
		}
		// Skip deleted files
		if e.blob.ContentHash != nil {
			entries = append(entries, e)
		}
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	if len(entries) == 0 {
		return nil, nil
	}

	// Step 2: Batch fetch content from both tables
	keys := make([]ContentKey, len(entries))
	isBinaryMap := make(map[ContentKey]bool)
	for i, e := range entries {
		k := ContentKey{GroupID: e.groupID, VersionID: e.versionID}
		keys[i] = k
		if e.blob.IsBinary {
			isBinaryMap[k] = true
		}
	}

	contentMap, err := db.GetContentsBatch(ctx, keys, isBinaryMap)
	if err != nil {
		return nil, err
	}

	// Step 3: Attach content to blobs
	blobs := make([]*Blob, len(entries))
	for i, e := range entries {
		key := ContentKey{GroupID: e.groupID, VersionID: e.versionID}
		e.blob.Content = contentMap[key]
		blobs[i] = e.blob
	}

	return blobs, nil
}

// GetCurrentTree retrieves the tree at HEAD.
func (db *DB) GetCurrentTree(ctx context.Context) ([]*Blob, error) {
	headID, err := db.GetHead(ctx)
	if err != nil {
		return nil, err
	}
	if headID == "" {
		return nil, nil // No commits yet
	}
	return db.GetTreeAtCommit(ctx, headID)
}

// GetTreeMetadataAtCommit retrieves the full tree metadata (all files) at a commit
// WITHOUT loading content. This is much faster than GetTreeAtCommit when you only
// need paths and hashes (e.g., for status, ls-tree).
// The returned Blobs have Content set to nil.
func (db *DB) GetTreeMetadataAtCommit(ctx context.Context, commitID string) ([]*Blob, error) {
	refs, err := db.GetTreeRefsAtCommitWithPaths(ctx, commitID)
	if err != nil {
		return nil, err
	}

	if len(refs) == 0 {
		return nil, nil
	}

	// Build blobs without content
	blobs := make([]*Blob, len(refs))
	for i, ref := range refs {
		blobs[i] = &Blob{
			Path:          ref.Path,
			CommitID:      ref.CommitID,
			Content:       nil, // Explicitly not loading content
			ContentHash:   ref.ContentHash,
			Mode:          ref.Mode,
			IsSymlink:     ref.IsSymlink,
			SymlinkTarget: ref.SymlinkTarget,
			IsBinary:      ref.IsBinary,
		}
	}

	return blobs, nil
}

// GetCurrentTreeMetadata retrieves the tree metadata at HEAD without content.
// Use this for operations that only need paths and hashes.
func (db *DB) GetCurrentTreeMetadata(ctx context.Context) ([]*Blob, error) {
	headID, err := db.GetHead(ctx)
	if err != nil {
		return nil, err
	}
	if headID == "" {
		return nil, nil // No commits yet
	}
	return db.GetTreeMetadataAtCommit(ctx, headID)
}

// GetFileHistory retrieves all versions of a file.
// Uses a two-step approach: get refs, then batch-fetch content from both tables.
func (db *DB) GetFileHistory(ctx context.Context, path string) ([]*Blob, error) {
	// Get (pathID, groupID) for path
	pathID, groupID, err := db.GetPathIDAndGroupIDByPath(ctx, path)
	if err != nil {
		return nil, err
	}
	if pathID == 0 {
		return nil, nil
	}

	// Get all file refs for this path
	refs, err := db.GetFileRefHistory(ctx, pathID)
	if err != nil {
		return nil, err
	}

	if len(refs) == 0 {
		return nil, nil
	}

	// Batch fetch content (only for non-deleted refs)
	// Content is keyed by (group_id, version_id) — groupID from paths table
	keys := make([]ContentKey, 0, len(refs))
	isBinaryMap := make(map[ContentKey]bool)
	for _, ref := range refs {
		if ref.ContentHash == nil {
			continue // deleted — no content row exists
		}
		k := ContentKey{GroupID: groupID, VersionID: ref.VersionID}
		keys = append(keys, k)
		if ref.IsBinary {
			isBinaryMap[k] = true
		}
	}

	contentMap, err := db.GetContentsBatch(ctx, keys, isBinaryMap)
	if err != nil {
		return nil, err
	}

	// Build blobs
	blobs := make([]*Blob, len(refs))
	for i, ref := range refs {
		key := ContentKey{GroupID: groupID, VersionID: ref.VersionID}
		blobs[i] = &Blob{
			Path:          path,
			CommitID:      ref.CommitID,
			Content:       contentMap[key],
			ContentHash:   ref.ContentHash,
			Mode:          ref.Mode,
			IsSymlink:     ref.IsSymlink,
			SymlinkTarget: ref.SymlinkTarget,
			IsBinary:      ref.IsBinary,
		}
	}

	return blobs, nil
}

// GetFileAtCommit retrieves a file at a specific commit (or the latest version before it).
// Uses file ref lookup + content fetch from the correct table.
func (db *DB) GetFileAtCommit(ctx context.Context, path, commitID string) (*Blob, error) {
	// Get (pathID, groupID) for path
	pathID, groupID, err := db.GetPathIDAndGroupIDByPath(ctx, path)
	if err != nil {
		return nil, err
	}
	if pathID == 0 {
		return nil, nil
	}

	// Get file ref at or before this commit
	ref, err := db.GetFileRefAtCommit(ctx, pathID, commitID)
	if err != nil {
		return nil, err
	}
	if ref == nil {
		return nil, nil // File doesn't exist or was deleted
	}

	// Get content from the correct table
	// Content is keyed by (group_id, version_id)
	content, err := db.GetContent(ctx, groupID, ref.VersionID, ref.IsBinary)
	if err != nil {
		return nil, err
	}

	return &Blob{
		Path:          path,
		CommitID:      ref.CommitID,
		Content:       content,
		ContentHash:   ref.ContentHash,
		Mode:          ref.Mode,
		IsSymlink:     ref.IsSymlink,
		SymlinkTarget: ref.SymlinkTarget,
		IsBinary:      ref.IsBinary,
	}, nil
}

// GetChangedFiles returns files that changed between two commits.
func (db *DB) GetChangedFiles(ctx context.Context, fromCommit, toCommit string) ([]*Blob, error) {
	// Get changed file refs with paths
	refs, err := db.GetChangedFileRefsWithPaths(ctx, fromCommit, toCommit)
	if err != nil {
		return nil, err
	}

	if len(refs) == 0 {
		return nil, nil
	}

	// Get content for non-deleted refs only
	keys := make([]ContentKey, 0, len(refs))
	isBinaryMap := make(map[ContentKey]bool)
	for _, ref := range refs {
		if ref.ContentHash == nil {
			continue // deleted — no content row exists
		}
		k := ContentKey{GroupID: ref.GroupID, VersionID: ref.VersionID}
		keys = append(keys, k)
		if ref.IsBinary {
			isBinaryMap[k] = true
		}
	}

	contentMap, err := db.GetContentsBatch(ctx, keys, isBinaryMap)
	if err != nil {
		return nil, err
	}

	// Build blobs
	blobs := make([]*Blob, len(refs))
	for i, ref := range refs {
		key := ContentKey{GroupID: ref.GroupID, VersionID: ref.VersionID}
		blobs[i] = &Blob{
			Path:          ref.Path,
			CommitID:      ref.CommitID,
			Content:       contentMap[key],
			ContentHash:   ref.ContentHash,
			Mode:          ref.Mode,
			IsSymlink:     ref.IsSymlink,
			SymlinkTarget: ref.SymlinkTarget,
			IsBinary:      ref.IsBinary,
		}
	}

	return blobs, nil
}

// GetChangedFilesMetadata returns files that changed between two commits WITHOUT content.
// Use this for operations that only need paths and hashes (e.g., diff --name-only).
func (db *DB) GetChangedFilesMetadata(ctx context.Context, fromCommit, toCommit string) ([]*Blob, error) {
	refs, err := db.GetChangedFileRefsWithPaths(ctx, fromCommit, toCommit)
	if err != nil {
		return nil, err
	}

	if len(refs) == 0 {
		return nil, nil
	}

	// Build blobs without content
	blobs := make([]*Blob, len(refs))
	for i, ref := range refs {
		blobs[i] = &Blob{
			Path:          ref.Path,
			CommitID:      ref.CommitID,
			Content:       nil, // No content
			ContentHash:   ref.ContentHash,
			Mode:          ref.Mode,
			IsSymlink:     ref.IsSymlink,
			SymlinkTarget: ref.SymlinkTarget,
			IsBinary:      ref.IsBinary,
		}
	}

	return blobs, nil
}

// GetBlobsAtCommitMetadata retrieves all blobs at a specific commit WITHOUT content.
// This returns only files that were changed in that specific commit (not the full tree).
func (db *DB) GetBlobsAtCommitMetadata(ctx context.Context, commitID string) ([]*Blob, error) {
	refs, err := db.GetFileRefsAtCommitWithPaths(ctx, commitID)
	if err != nil {
		return nil, err
	}

	if len(refs) == 0 {
		return nil, nil
	}

	// Build blobs without content
	blobs := make([]*Blob, len(refs))
	for i, ref := range refs {
		blobs[i] = &Blob{
			Path:          ref.Path,
			CommitID:      ref.CommitID,
			Content:       nil, // No content
			ContentHash:   ref.ContentHash,
			Mode:          ref.Mode,
			IsSymlink:     ref.IsSymlink,
			SymlinkTarget: ref.SymlinkTarget,
			IsBinary:      ref.IsBinary,
		}
	}

	return blobs, nil
}

// GetAllPaths returns all unique file paths in the repository.
// This is very fast as it queries the small pgit_paths table.
func (db *DB) GetAllPaths(ctx context.Context) ([]string, error) {
	return db.GetAllPathsV2(ctx)
}

// GetImportedPaths returns the set of paths that have at least one file ref.
// Used during import resume to skip paths whose blobs are already imported.
func (db *DB) GetImportedPaths(ctx context.Context) (map[string]bool, error) {
	rows, err := db.Query(ctx,
		"SELECT DISTINCT p.path FROM pgit_paths p "+
			"JOIN pgit_file_refs fr ON fr.path_id = p.path_id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]bool)
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			return nil, err
		}
		result[path] = true
	}
	return result, rows.Err()
}

// BlobExists checks if a blob exists at a specific commit.
func (db *DB) BlobExists(ctx context.Context, path, commitID string) (bool, error) {
	pathID, _, err := db.GetPathIDAndGroupIDByPath(ctx, path)
	if err != nil {
		return false, err
	}
	if pathID == 0 {
		return false, nil
	}

	return db.FileRefExists(ctx, pathID, commitID)
}

// FileExistsInTree checks if a file exists (is not deleted) in the tree at a commit.
// This finds the latest version of the file at or before commitID and checks if it's not deleted.
func (db *DB) FileExistsInTree(ctx context.Context, path, commitID string) (bool, error) {
	pathID, _, err := db.GetPathIDAndGroupIDByPath(ctx, path)
	if err != nil {
		return false, err
	}
	if pathID == 0 {
		return false, nil // Path never existed
	}

	// Get the latest file ref at or before this commit
	ref, err := db.GetFileRefAtCommit(ctx, pathID, commitID)
	if err != nil {
		return false, err
	}

	// ref is nil if file doesn't exist or was deleted at this point
	return ref != nil, nil
}

// SearchContentOptions configures content search behavior.
type SearchContentOptions struct {
	// Pattern is the regex pattern to search for (PostgreSQL regex syntax).
	Pattern string
	// IgnoreCase enables case-insensitive matching.
	IgnoreCase bool
	// PathPattern is an optional glob pattern to filter files by path.
	PathPattern string
	// CommitID limits search to files at or before this commit.
	// Empty string means search all versions.
	CommitID string
	// Limit is the maximum number of matching files to return.
	// 0 means no limit.
	Limit int
}

// SearchContentResult represents a file that matched the search.
type SearchContentResult struct {
	Path      string
	GroupID   int32
	CommitID  string
	VersionID int32
	Content   []byte
}

// SearchContent searches text file contents using parallel-by-group fetching.
// Binary files are excluded from search results.
// Strategy:
//  1. Load all matching file refs into memory
//  2. Group by group_id, build version→ref lookup
//  3. Worker goroutines process groups in parallel, each issuing a single
//     server-side regex query per group for optimal xpatch delta-chain access
func (db *DB) SearchContent(ctx context.Context, opts SearchContentOptions) ([]*SearchContentResult, error) {
	// Step 1: Load file refs
	refs, err := db.getSearchFileRefs(ctx, opts.PathPattern, opts.CommitID)
	if err != nil {
		return nil, err
	}

	if len(refs) == 0 {
		return nil, nil
	}

	// Step 2 & 3: Batch search (one query per group with server-side regex)
	return db.searchBatchParallel(ctx, refs, opts)
}

// SearchContentAtCommit searches text file contents at a specific commit.
// This searches the tree state at that commit (latest version of each file <= commitID).
// Binary files are excluded from search results.
func (db *DB) SearchContentAtCommit(ctx context.Context, commitID string, opts SearchContentOptions) ([]*SearchContentResult, error) {
	// Step 1: Load tree refs at commit
	refs, err := db.getTreeSearchRefs(ctx, commitID, opts.PathPattern)
	if err != nil {
		return nil, err
	}

	if len(refs) == 0 {
		return nil, nil
	}

	// Step 2 & 3: Per-version search (only 1 ref per group at a specific commit)
	return db.searchPerVersion(ctx, refs, opts)
}

// searchRef is a lightweight file ref for search operations.
type searchRef struct {
	groupID   int32
	versionID int32
	commitID  string
	path      string
}

// getSearchFileRefs loads all text file refs matching the search filters.
func (db *DB) getSearchFileRefs(ctx context.Context, pathPattern, commitID string) ([]searchRef, error) {
	var whereClauses []string
	var args []interface{}
	argNum := 1

	whereClauses = append(whereClauses, "r.content_hash IS NOT NULL")
	whereClauses = append(whereClauses, "r.is_binary = FALSE")

	if pathPattern != "" {
		likePattern := pathPattern
		likePattern = strings.ReplaceAll(likePattern, "*", "%")
		likePattern = strings.ReplaceAll(likePattern, "?", "_")
		whereClauses = append(whereClauses, fmt.Sprintf("p.path LIKE $%d", argNum))
		args = append(args, likePattern)
		argNum++
	}

	if commitID != "" {
		whereClauses = append(whereClauses, fmt.Sprintf("r.commit_id <= $%d", argNum))
		args = append(args, commitID)
	}

	whereClause := strings.Join(whereClauses, " AND ")

	sql := fmt.Sprintf(`
		SELECT p.group_id, r.version_id, r.commit_id, p.path
		FROM pgit_file_refs r
		JOIN pgit_paths p ON p.path_id = r.path_id
		WHERE %s`, whereClause)

	rows, err := db.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to load file refs: %w", err)
	}
	defer rows.Close()

	var refs []searchRef
	for rows.Next() {
		var r searchRef
		if err := rows.Scan(&r.groupID, &r.versionID, &r.commitID, &r.path); err != nil {
			return nil, err
		}
		refs = append(refs, r)
	}
	return refs, rows.Err()
}

// getTreeSearchRefs loads the tree state at a commit (latest version per file).
func (db *DB) getTreeSearchRefs(ctx context.Context, commitID, pathPattern string) ([]searchRef, error) {
	var args []interface{}
	argNum := 1

	args = append(args, commitID)
	argNum++

	pathFilter := ""
	if pathPattern != "" {
		likePattern := pathPattern
		likePattern = strings.ReplaceAll(likePattern, "*", "%")
		likePattern = strings.ReplaceAll(likePattern, "?", "_")
		pathFilter = fmt.Sprintf("AND p.path LIKE $%d", argNum)
		args = append(args, likePattern)
	}

	sql := fmt.Sprintf(`
		SELECT DISTINCT ON (r.path_id)
			p.group_id, r.version_id, r.commit_id, p.path
		FROM pgit_file_refs r
		JOIN pgit_paths p ON p.path_id = r.path_id
		WHERE r.commit_id <= $1 AND r.content_hash IS NOT NULL AND r.is_binary = FALSE %s
		ORDER BY r.path_id, r.commit_id DESC`, pathFilter)

	rows, err := db.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("failed to load tree refs: %w", err)
	}
	defer rows.Close()

	var refs []searchRef
	for rows.Next() {
		var r searchRef
		if err := rows.Scan(&r.groupID, &r.versionID, &r.commitID, &r.path); err != nil {
			return nil, err
		}
		refs = append(refs, r)
	}
	return refs, rows.Err()
}

// searchBatchParallel runs parallel search with one server-side regex query per group.
// This is optimal for --all mode where many versions per group need checking.
// xpatch decompresses the delta chain once per group, and the regex filter runs
// server-side so non-matching content never crosses the wire.
func (db *DB) searchBatchParallel(ctx context.Context, refs []searchRef, opts SearchContentOptions) ([]*SearchContentResult, error) {
	// Build version→ref lookup per group
	type groupInfo struct {
		refs    map[int32]searchRef // version_id → ref
		path    string
		groupID int32
	}
	groups := make(map[int32]*groupInfo)
	for _, r := range refs {
		g, ok := groups[r.groupID]
		if !ok {
			g = &groupInfo{
				refs:    make(map[int32]searchRef),
				path:    r.path,
				groupID: r.groupID,
			}
			groups[r.groupID] = g
		}
		g.refs[r.versionID] = r
	}

	// Sort groups by version count ascending — fewest versions first.
	// Groups with fewer versions have shorter delta chains, so they decompress faster.
	// This lets early termination kick in before we hit the expensive groups.
	groupIDs := make([]int32, 0, len(groups))
	for gid := range groups {
		groupIDs = append(groupIDs, gid)
	}
	sort.Slice(groupIDs, func(i, j int) bool {
		return len(groups[groupIDs[i]].refs) < len(groups[groupIDs[j]].refs)
	})

	// Dispatch groups to workers
	groupChan := make(chan int32, len(groupIDs))
	for _, gid := range groupIDs {
		groupChan <- gid
	}
	close(groupChan)

	// Build the PG regex operator
	regexOp := "~"
	if opts.IgnoreCase {
		regexOp = "~*"
	}
	query := fmt.Sprintf(
		"SELECT version_id, content FROM pgit_text_content WHERE group_id = $1 AND content %s $2 ORDER BY version_id",
		regexOp)

	var allResults []*SearchContentResult
	var mu sync.Mutex
	var wg sync.WaitGroup
	var resultCount atomic.Int64
	var firstErr atomic.Pointer[error]

	limit := int64(opts.Limit)
	if limit <= 0 {
		limit = int64(^uint64(0) >> 1) // effectively unlimited
	}

	workers := 8

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for gid := range groupChan {
				if firstErr.Load() != nil {
					return
				}
				if resultCount.Load() >= limit {
					return
				}

				g := groups[gid]

				// Single query per group: server-side regex on all versions
				rows, err := db.Query(ctx, query, gid, opts.Pattern)
				if err != nil {
					firstErr.CompareAndSwap(nil, &err)
					return
				}

				for rows.Next() {
					if resultCount.Load() >= limit {
						rows.Close()
						return
					}

					var versionID int32
					var content []byte
					if err := rows.Scan(&versionID, &content); err != nil {
						rows.Close()
						firstErr.CompareAndSwap(nil, &err)
						return
					}

					ref, ok := g.refs[versionID]
					if !ok {
						continue // version not in our search set
					}

					mu.Lock()
					allResults = append(allResults, &SearchContentResult{
						Path:      ref.path,
						GroupID:   ref.groupID,
						CommitID:  ref.commitID,
						VersionID: ref.versionID,
						Content:   content,
					})
					mu.Unlock()
					resultCount.Add(1)
				}
				rows.Close()
			}
		}()
	}

	wg.Wait()

	if errPtr := firstErr.Load(); errPtr != nil {
		return nil, *errPtr
	}

	return allResults, nil
}

// searchPerVersion runs parallel search with individual PK lookups per version.
// This is optimal for single-commit search where each group has ~1 ref,
// avoiding unnecessary decompression of the full delta chain.
func (db *DB) searchPerVersion(ctx context.Context, refs []searchRef, opts SearchContentOptions) ([]*SearchContentResult, error) {
	// Compile Go regex
	goPattern := opts.Pattern
	if opts.IgnoreCase {
		goPattern = "(?i)" + goPattern
	}
	re, err := regexp.Compile(goPattern)
	if err != nil {
		return nil, fmt.Errorf("invalid regex: %w", err)
	}

	// Group by group_id, track the version_id we need to reconstruct (= chain depth cost)
	groups := make(map[int32][]searchRef)
	maxVersion := make(map[int32]int32) // group_id → max version_id (= chain depth)
	for _, r := range refs {
		groups[r.groupID] = append(groups[r.groupID], r)
		if r.versionID > maxVersion[r.groupID] {
			maxVersion[r.groupID] = r.versionID
		}
	}

	// Sort groups by chain depth ascending — cheapest to decompress first.
	// This lets early termination kick in quickly by searching shallow-chain files first.
	groupIDs := make([]int32, 0, len(groups))
	for gid := range groups {
		groupIDs = append(groupIDs, gid)
	}
	sort.Slice(groupIDs, func(i, j int) bool {
		return maxVersion[groupIDs[i]] < maxVersion[groupIDs[j]]
	})

	// Dispatch groups to workers
	groupChan := make(chan int32, len(groupIDs))
	for _, gid := range groupIDs {
		groupChan <- gid
	}
	close(groupChan)

	var allResults []*SearchContentResult
	var mu sync.Mutex
	var wg sync.WaitGroup
	var resultCount atomic.Int64
	var firstErr atomic.Pointer[error]

	limit := int64(opts.Limit)
	if limit <= 0 {
		limit = int64(^uint64(0) >> 1) // effectively unlimited
	}

	workers := 8

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for gid := range groupChan {
				if firstErr.Load() != nil {
					return
				}
				if resultCount.Load() >= limit {
					return
				}

				refsInGroup := groups[gid]

				for _, ref := range refsInGroup {
					if resultCount.Load() >= limit {
						return
					}

					var content []byte
					err := db.QueryRow(ctx,
						"SELECT content FROM pgit_text_content WHERE group_id = $1 AND version_id = $2",
						ref.groupID, ref.versionID).Scan(&content)
					if err != nil {
						continue
					}

					if re.Match(content) {
						mu.Lock()
						allResults = append(allResults, &SearchContentResult{
							Path:      ref.path,
							GroupID:   ref.groupID,
							CommitID:  ref.commitID,
							VersionID: ref.versionID,
							Content:   content,
						})
						mu.Unlock()
						resultCount.Add(1)
					}
				}
			}
		}()
	}

	wg.Wait()

	if errPtr := firstErr.Load(); errPtr != nil {
		return nil, *errPtr
	}

	return allResults, nil
}

// PreRegisterPaths inserts all paths into pgit_paths with their group assignments
// and returns the complete path -> PathIDs mapping. This eliminates per-worker
// path lookup queries during import. Paths that already exist are returned as-is.
//
// pathToLocalGroup maps each path to a local group index (0-based, from union-find).
// Paths with the same local group index get the same database group_id.
// The first path in each local group determines the database group_id (auto-assigned
// by PostgreSQL's IDENTITY column), and subsequent paths in the group reuse it.
func (db *DB) PreRegisterPaths(ctx context.Context, paths []string, pathToLocalGroup map[string]int) (map[string]PathIDs, error) {
	if len(paths) == 0 {
		return make(map[string]PathIDs), nil
	}

	result := make(map[string]PathIDs, len(paths))

	// First, get all existing paths in one query
	rows, err := db.Query(ctx,
		"SELECT path_id, group_id, path FROM pgit_paths WHERE path = ANY($1)",
		paths,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to query existing paths: %w", err)
	}
	for rows.Next() {
		var pathID, groupID int32
		var path string
		if err := rows.Scan(&pathID, &groupID, &path); err != nil {
			rows.Close()
			return nil, err
		}
		result[path] = PathIDs{PathID: pathID, GroupID: groupID}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Find missing paths
	var missingPaths []string
	for _, p := range paths {
		if _, exists := result[p]; !exists {
			missingPaths = append(missingPaths, p)
		}
	}

	if len(missingPaths) == 0 {
		return result, nil
	}

	// Assign database group_ids for each local group.
	// Strategy: for each local group, the first path inserted gets a new path_id
	// (from IDENTITY). We use that path_id as the group_id for the whole group.
	// This is because group_id is NOT auto-generated — we control it.
	//
	// Two-pass approach:
	// Pass 1: Insert the first path of each local group to get auto-assigned path_ids,
	//         then use those path_ids as group_ids.
	// Pass 2: Insert remaining paths in each group with the assigned group_id.
	//
	// Simpler approach: pre-compute group_ids using a counter, then insert all paths.
	// We use a sequence query to get a block of group_ids.

	// Find unique local groups among missing paths
	localGroupPaths := make(map[int][]string) // localGroup → paths
	for _, path := range missingPaths {
		lg := pathToLocalGroup[path]
		localGroupPaths[lg] = append(localGroupPaths[lg], path)
	}

	// Check if any local group already has some paths registered (for resume).
	// If so, reuse their group_id.
	localGroupToDBGroup := make(map[int]int32)
	for _, path := range paths {
		if reg, exists := result[path]; exists {
			lg := pathToLocalGroup[path]
			localGroupToDBGroup[lg] = reg.GroupID
		}
	}

	// For groups that don't have a database group_id yet, assign sequential ones.
	// We use a simple counter starting from MAX(group_id)+1.
	var maxGroupID int32
	err = db.QueryRow(ctx, "SELECT COALESCE(MAX(group_id), 0) FROM pgit_paths").Scan(&maxGroupID)
	if err != nil {
		return nil, fmt.Errorf("failed to get max group_id: %w", err)
	}

	nextGroupID := maxGroupID + 1
	for lg := range localGroupPaths {
		if _, exists := localGroupToDBGroup[lg]; !exists {
			localGroupToDBGroup[lg] = nextGroupID
			nextGroupID++
		}
	}

	// Insert all missing paths with their group_ids
	const batchSize = 1000
	err = db.WithTx(ctx, func(tx pgx.Tx) error {
		for i := 0; i < len(missingPaths); i += batchSize {
			end := i + batchSize
			if end > len(missingPaths) {
				end = len(missingPaths)
			}
			batch := missingPaths[i:end]

			for _, path := range batch {
				lg := pathToLocalGroup[path]
				groupID := localGroupToDBGroup[lg]
				var pathID int32
				err := tx.QueryRow(ctx,
					`INSERT INTO pgit_paths (group_id, path) VALUES ($1, $2)
					 ON CONFLICT (path) DO UPDATE SET path = EXCLUDED.path
					 RETURNING path_id, group_id`,
					groupID, path,
				).Scan(&pathID, &groupID)
				if err != nil {
					return err
				}
				result[path] = PathIDs{PathID: pathID, GroupID: groupID}
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to register paths: %w", err)
	}

	return result, nil
}

// CreateBlobsForGroup inserts all blobs for a single content group in one
// transaction. In v4, a group may contain multiple paths (renames/copies).
// Large groups are split into chunks of copyChunkSize, each chunk getting
// its own COPY calls — but all within the same transaction to keep xpatch's
// delta chain on one connection.
//
// groupID is the database group_id for content tables.
// versionCounter points to the caller-managed next version_id for this group.
// pathReg maps path → PathIDs for resolving each blob's path_id.
// onProgress is called after each chunk with the number of blobs in that chunk (may be nil).
func (db *DB) CreateBlobsForGroup(ctx context.Context, blobs []*Blob, groupID int32, versionCounter *int32, pathReg map[string]PathIDs, onProgress func(int)) error {
	if len(blobs) == 0 {
		return nil
	}

	const copyChunkSize = 200

	// Track content hash → version_id within this group so that duplicate
	// content (renames, copies, reverts) reuses the existing version_id
	// instead of creating a new content row.
	hashToVersion := make(map[string]int32, len(blobs))

	return db.WithTx(ctx, func(tx pgx.Tx) error {
		for i := 0; i < len(blobs); i += copyChunkSize {
			end := i + copyChunkSize
			if end > len(blobs) {
				end = len(blobs)
			}
			chunk := blobs[i:end]

			var fileRefs []*FileRef
			var contents []*Content

			for _, b := range chunk {
				// Look up path_id from the pre-registration map
				pathIDs := pathReg[b.Path]

				var versionID int32
				if b.ContentHash != nil {
					hashKey := string(b.ContentHash)
					if existing, ok := hashToVersion[hashKey]; ok {
						// Same content already stored in this group — reuse version_id,
						// skip content insert.
						versionID = existing
					} else {
						// New content — assign next version_id and store it.
						*versionCounter++
						versionID = *versionCounter
						hashToVersion[hashKey] = versionID

						contents = append(contents, &Content{
							GroupID:   groupID,
							VersionID: versionID,
							Content:   b.Content,
							IsBinary:  b.IsBinary,
						})
					}
				} else {
					// Deletion — no content to store, but still needs a version_id
					// for the file_ref. Deletions are unique events (no dedup).
					*versionCounter++
					versionID = *versionCounter
				}

				fileRefs = append(fileRefs, &FileRef{
					PathID:        pathIDs.PathID,
					CommitID:      b.CommitID,
					VersionID:     versionID,
					ContentHash:   b.ContentHash,
					Mode:          b.Mode,
					IsSymlink:     b.IsSymlink,
					SymlinkTarget: b.SymlinkTarget,
					IsBinary:      b.IsBinary,
				})
			}

			if err := db.createFileRefsBatchTx(ctx, tx, fileRefs); err != nil {
				return err
			}

			if err := db.createContentsBatchTx(ctx, tx, contents); err != nil {
				return err
			}

			if onProgress != nil {
				onProgress(len(chunk))
			}
		}

		return nil
	})
}

// CountBlobs returns the total number of blob versions (file refs).
func (db *DB) CountBlobs(ctx context.Context) (int64, error) {
	return db.CountFileRefs(ctx)
}
