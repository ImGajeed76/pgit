package db

import (
	"context"
	"fmt"
	"strings"

	"github.com/imgajeed76/pgit/v2/internal/util"
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
func (db *DB) createBlobTx(ctx context.Context, tx pgx.Tx, b *Blob) error {
	// 1. Get or create path -> group_id
	groupID, err := db.GetOrCreatePathTx(ctx, tx, b.Path)
	if err != nil {
		return err
	}

	// 2. Get next version_id for this group
	versionID, err := db.GetNextVersionIDTx(ctx, tx, groupID)
	if err != nil {
		return err
	}

	// 3. Create file ref
	ref := &FileRef{
		GroupID:       groupID,
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
		// Collect all unique paths
		pathSet := make(map[string]bool)
		for _, b := range blobs {
			pathSet[b.Path] = true
		}
		paths := make([]string, 0, len(pathSet))
		for p := range pathSet {
			paths = append(paths, p)
		}

		// Batch get/create paths
		pathToGroupID, err := db.getOrCreatePathsBatchTx(ctx, tx, paths)
		if err != nil {
			return err
		}

		// Group blobs by path to assign sequential version IDs
		blobsByPath := make(map[string][]*Blob)
		for _, b := range blobs {
			blobsByPath[b.Path] = append(blobsByPath[b.Path], b)
		}

		// Get current max version_id for each group
		groupIDs := make([]int32, 0, len(pathToGroupID))
		for _, gid := range pathToGroupID {
			groupIDs = append(groupIDs, gid)
		}
		maxVersions, err := db.getMaxVersionIDsBatchTx(ctx, tx, groupIDs)
		if err != nil {
			return err
		}

		// Prepare batch inserts
		var fileRefs []*FileRef
		var contents []*Content

		for path, pathBlobs := range blobsByPath {
			groupID := pathToGroupID[path]
			baseVersion := maxVersions[groupID]

			for i, b := range pathBlobs {
				versionID := baseVersion + int32(i) + 1

				fileRefs = append(fileRefs, &FileRef{
					GroupID:       groupID,
					CommitID:      b.CommitID,
					VersionID:     versionID,
					ContentHash:   b.ContentHash,
					Mode:          b.Mode,
					IsSymlink:     b.IsSymlink,
					SymlinkTarget: b.SymlinkTarget,
					IsBinary:      b.IsBinary,
				})

				// Only create content entries for non-deleted files.
				// Deleted files (ContentHash == nil) only need a file ref.
				if b.ContentHash != nil {
					contents = append(contents, &Content{
						GroupID:   groupID,
						VersionID: versionID,
						Content:   b.Content,
						IsBinary:  b.IsBinary,
					})
				}
			}
		}

		// Batch insert file refs
		if err := db.createFileRefsBatchTx(ctx, tx, fileRefs); err != nil {
			return err
		}

		// Batch insert contents (splits into text/binary internally)
		if err := db.createContentsBatchTx(ctx, tx, contents); err != nil {
			return err
		}

		return nil
	})
}

// getOrCreatePathsBatchTx handles multiple paths within a transaction.
func (db *DB) getOrCreatePathsBatchTx(ctx context.Context, tx pgx.Tx, paths []string) (map[string]int32, error) {
	if len(paths) == 0 {
		return make(map[string]int32), nil
	}

	result := make(map[string]int32, len(paths))

	// First, try to get all existing paths
	rows, err := tx.Query(ctx,
		"SELECT group_id, path FROM pgit_paths WHERE path = ANY($1)",
		paths,
	)
	if err != nil {
		return nil, err
	}

	for rows.Next() {
		var groupID int32
		var path string
		if err := rows.Scan(&groupID, &path); err != nil {
			rows.Close()
			return nil, err
		}
		result[path] = groupID
	}
	rows.Close()

	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Insert missing paths
	for _, path := range paths {
		if _, exists := result[path]; !exists {
			var groupID int32
			err := tx.QueryRow(ctx,
				`INSERT INTO pgit_paths (path) VALUES ($1)
				 ON CONFLICT (path) DO UPDATE SET path = EXCLUDED.path
				 RETURNING group_id`,
				path,
			).Scan(&groupID)
			if err != nil {
				return nil, err
			}
			result[path] = groupID
		}
	}

	return result, nil
}

// getMaxVersionIDsBatchTx gets max version_id for each group_id.
func (db *DB) getMaxVersionIDsBatchTx(ctx context.Context, tx pgx.Tx, groupIDs []int32) (map[int32]int32, error) {
	result := make(map[int32]int32)

	if len(groupIDs) == 0 {
		return result, nil
	}

	rows, err := tx.Query(ctx,
		"SELECT group_id, COALESCE(MAX(version_id), 0) FROM pgit_file_refs WHERE group_id = ANY($1) GROUP BY group_id",
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
			ref.GroupID, ref.CommitID, ref.VersionID, ref.ContentHash,
			ref.Mode, ref.IsSymlink, ref.SymlinkTarget, ref.IsBinary,
		}
	}

	_, err := tx.CopyFrom(
		ctx,
		pgx.Identifier{"pgit_file_refs"},
		[]string{"group_id", "commit_id", "version_id", "content_hash", "mode", "is_symlink", "symlink_target", "is_binary"},
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
	// Get group_id for path
	groupID, err := db.GetGroupIDByPath(ctx, path)
	if err != nil {
		return nil, err
	}
	if groupID == 0 {
		return nil, nil // Path doesn't exist
	}

	// Get file ref
	ref, err := db.GetFileRef(ctx, groupID, commitID)
	if err != nil {
		return nil, err
	}
	if ref == nil {
		return nil, nil
	}

	// Get content from the correct table based on is_binary
	content, err := db.GetContent(ctx, ref.GroupID, ref.VersionID, ref.IsBinary)
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
	SELECT DISTINCT ON (r.group_id)
		p.path, r.commit_id, r.content_hash, r.mode, r.is_symlink, r.symlink_target,
		r.group_id, r.version_id, r.is_binary
	FROM pgit_file_refs r
	JOIN pgit_paths p ON p.group_id = r.group_id
	WHERE r.commit_id <= $1
	ORDER BY r.group_id, r.commit_id DESC`

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
	head, err := db.GetHeadCommit(ctx)
	if err != nil {
		return nil, err
	}
	if head == nil {
		return nil, nil // No commits yet
	}
	return db.GetTreeAtCommit(ctx, head.ID)
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
	head, err := db.GetHeadCommit(ctx)
	if err != nil {
		return nil, err
	}
	if head == nil {
		return nil, nil // No commits yet
	}
	return db.GetTreeMetadataAtCommit(ctx, head.ID)
}

// GetFileHistory retrieves all versions of a file.
// Uses a two-step approach: get refs, then batch-fetch content from both tables.
func (db *DB) GetFileHistory(ctx context.Context, path string) ([]*Blob, error) {
	// Get group_id for path
	groupID, err := db.GetGroupIDByPath(ctx, path)
	if err != nil {
		return nil, err
	}
	if groupID == 0 {
		return nil, nil
	}

	// Get all file refs for this group
	refs, err := db.GetFileRefHistory(ctx, groupID)
	if err != nil {
		return nil, err
	}

	if len(refs) == 0 {
		return nil, nil
	}

	// Batch fetch content (only for non-deleted refs)
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
	// Get group_id for path
	groupID, err := db.GetGroupIDByPath(ctx, path)
	if err != nil {
		return nil, err
	}
	if groupID == 0 {
		return nil, nil
	}

	// Get file ref at or before this commit
	ref, err := db.GetFileRefAtCommit(ctx, groupID, commitID)
	if err != nil {
		return nil, err
	}
	if ref == nil {
		return nil, nil // File doesn't exist or was deleted
	}

	// Get content from the correct table
	content, err := db.GetContent(ctx, ref.GroupID, ref.VersionID, ref.IsBinary)
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

// BlobExists checks if a blob exists at a specific commit.
func (db *DB) BlobExists(ctx context.Context, path, commitID string) (bool, error) {
	groupID, err := db.GetGroupIDByPath(ctx, path)
	if err != nil {
		return false, err
	}
	if groupID == 0 {
		return false, nil
	}

	return db.FileRefExists(ctx, groupID, commitID)
}

// FileExistsInTree checks if a file exists (is not deleted) in the tree at a commit.
// This finds the latest version of the file at or before commitID and checks if it's not deleted.
func (db *DB) FileExistsInTree(ctx context.Context, path, commitID string) (bool, error) {
	groupID, err := db.GetGroupIDByPath(ctx, path)
	if err != nil {
		return false, err
	}
	if groupID == 0 {
		return false, nil // Path never existed
	}

	// Get the latest file ref at or before this commit
	ref, err := db.GetFileRefAtCommit(ctx, groupID, commitID)
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

// SearchContent searches text file contents using server-side regex on pgit_text_content.
// Binary files are excluded from search results.
// Strategy:
// 1. Query pgit_text_content with WHERE content ~ pattern (server-side regex)
// 2. Join with file_refs and paths for metadata
// 3. Return matching files with content for Go-side line extraction
func (db *DB) SearchContent(ctx context.Context, opts SearchContentOptions) ([]*SearchContentResult, error) {
	var whereClauses []string
	var args []interface{}
	argNum := 1

	// Content must exist (not deleted) and must be text
	whereClauses = append(whereClauses, "r.content_hash IS NOT NULL")
	whereClauses = append(whereClauses, "r.is_binary = FALSE")

	// Path filter
	if opts.PathPattern != "" {
		likePattern := opts.PathPattern
		likePattern = strings.ReplaceAll(likePattern, "*", "%")
		likePattern = strings.ReplaceAll(likePattern, "?", "_")
		whereClauses = append(whereClauses, fmt.Sprintf("p.path LIKE $%d", argNum))
		args = append(args, likePattern)
		argNum++
	}

	// Commit filter (search at or before this commit)
	if opts.CommitID != "" {
		whereClauses = append(whereClauses, fmt.Sprintf("r.commit_id <= $%d", argNum))
		args = append(args, opts.CommitID)
		argNum++
	}

	// Regex pattern (server-side)
	regexOp := "~"
	if opts.IgnoreCase {
		regexOp = "~*"
	}
	whereClauses = append(whereClauses, fmt.Sprintf("c.content %s $%d", regexOp, argNum))
	args = append(args, opts.Pattern)
	argNum++

	whereClause := strings.Join(whereClauses, " AND ")

	limitClause := ""
	if opts.Limit > 0 {
		limitClause = fmt.Sprintf(" LIMIT $%d", argNum)
		args = append(args, opts.Limit)
	}

	sql := fmt.Sprintf(`
		SELECT p.path, r.group_id, r.commit_id, r.version_id, c.content
		FROM pgit_file_refs r
		JOIN pgit_paths p ON p.group_id = r.group_id
		JOIN pgit_text_content c ON c.group_id = r.group_id AND c.version_id = r.version_id
		WHERE %s
		ORDER BY r.group_id, r.version_id%s`, whereClause, limitClause)

	rows, err := db.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("search query failed: %w", err)
	}
	defer rows.Close()

	var results []*SearchContentResult
	for rows.Next() {
		r := &SearchContentResult{}
		var textContent string
		if err := rows.Scan(&r.Path, &r.GroupID, &r.CommitID, &r.VersionID, &textContent); err != nil {
			return nil, err
		}
		r.Content = []byte(textContent)
		results = append(results, r)
	}

	return results, rows.Err()
}

// SearchContentAtCommit searches text file contents at a specific commit using server-side regex.
// This searches the tree state at that commit (latest version of each file <= commitID).
// Binary files are excluded from search results.
func (db *DB) SearchContentAtCommit(ctx context.Context, commitID string, opts SearchContentOptions) ([]*SearchContentResult, error) {
	var args []interface{}
	argNum := 1

	args = append(args, commitID)
	argNum++

	pathFilter := ""
	if opts.PathPattern != "" {
		likePattern := opts.PathPattern
		likePattern = strings.ReplaceAll(likePattern, "*", "%")
		likePattern = strings.ReplaceAll(likePattern, "?", "_")
		pathFilter = fmt.Sprintf("AND p.path LIKE $%d", argNum)
		args = append(args, likePattern)
		argNum++
	}

	// Regex operator
	regexOp := "~"
	if opts.IgnoreCase {
		regexOp = "~*"
	}

	// Build CTE: get tree state, then join with text content and filter by regex
	limitClause := ""
	if opts.Limit > 0 {
		limitClause = fmt.Sprintf(" LIMIT $%d", argNum+1) // +1 because pattern is argNum
		// We'll add the limit arg after the pattern arg
	}

	sql := fmt.Sprintf(`
		WITH tree AS (
			SELECT DISTINCT ON (r.group_id)
				r.group_id, r.version_id, r.commit_id
			FROM pgit_file_refs r
			WHERE r.commit_id <= $1 AND r.content_hash IS NOT NULL AND r.is_binary = FALSE
			ORDER BY r.group_id, r.commit_id DESC
		)
		SELECT p.path, t.group_id, t.commit_id, t.version_id, c.content
		FROM tree t
		JOIN pgit_paths p ON p.group_id = t.group_id
		JOIN pgit_text_content c ON c.group_id = t.group_id AND c.version_id = t.version_id
		WHERE c.content %s $%d %s
		ORDER BY t.group_id, t.version_id%s`, regexOp, argNum, pathFilter, limitClause)

	args = append(args, opts.Pattern)
	argNum++

	if opts.Limit > 0 {
		args = append(args, opts.Limit)
	}

	rows, err := db.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("search query failed: %w", err)
	}
	defer rows.Close()

	var results []*SearchContentResult
	for rows.Next() {
		r := &SearchContentResult{}
		var textContent string
		if err := rows.Scan(&r.Path, &r.GroupID, &r.CommitID, &r.VersionID, &textContent); err != nil {
			return nil, err
		}
		r.Content = []byte(textContent)
		results = append(results, r)
	}

	return results, rows.Err()
}

// CountBlobs returns the total number of blob versions (file refs).
func (db *DB) CountBlobs(ctx context.Context) (int64, error) {
	return db.CountFileRefs(ctx)
}
