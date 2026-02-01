package db

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// Content represents file content stored in the content table.
// Content is delta-compressed by xpatch, grouped by group_id.
type Content struct {
	GroupID   int32
	VersionID int32
	Content   []byte
}

// ContentKey is used for batch lookups.
type ContentKey struct {
	GroupID   int32
	VersionID int32
}

// CreateContent inserts content into the content table.
func (db *DB) CreateContent(ctx context.Context, c *Content) error {
	sql := `
	INSERT INTO pgit_content (group_id, version_id, content)
	VALUES ($1, $2, $3)`

	// Ensure content is never nil (delta columns can't be NULL)
	content := c.Content
	if content == nil {
		content = []byte{}
	}

	return db.Exec(ctx, sql, c.GroupID, c.VersionID, content)
}

// CreateContentTx inserts content within a transaction.
func (db *DB) CreateContentTx(ctx context.Context, tx pgx.Tx, c *Content) error {
	sql := `
	INSERT INTO pgit_content (group_id, version_id, content)
	VALUES ($1, $2, $3)`

	content := c.Content
	if content == nil {
		content = []byte{}
	}

	_, err := tx.Exec(ctx, sql, c.GroupID, c.VersionID, content)
	return err
}

// CreateContentBatch inserts multiple content entries using COPY for speed.
// This is optimized for bulk imports.
func (db *DB) CreateContentBatch(ctx context.Context, contents []*Content) error {
	if len(contents) == 0 {
		return nil
	}

	rows := make([][]interface{}, len(contents))
	for i, c := range contents {
		// Ensure content is never nil (delta columns can't be NULL)
		content := c.Content
		if content == nil {
			content = []byte{}
		}
		rows[i] = []interface{}{c.GroupID, c.VersionID, content}
	}

	_, err := db.pool.CopyFrom(
		ctx,
		pgx.Identifier{"pgit_content"},
		[]string{"group_id", "version_id", "content"},
		pgx.CopyFromRows(rows),
	)
	return err
}

// GetContent retrieves content by (group_id, version_id).
func (db *DB) GetContent(ctx context.Context, groupID, versionID int32) ([]byte, error) {
	var content []byte
	err := db.QueryRow(ctx,
		"SELECT content FROM pgit_content WHERE group_id = $1 AND version_id = $2",
		groupID, versionID,
	).Scan(&content)

	if err == pgx.ErrNoRows {
		return nil, nil
	}

	return content, err
}

// GetContentsBatch retrieves multiple contents by their keys.
// Returns a map of ContentKey -> content bytes.
// Uses parallel queries per group for faster xpatch decompression.
func (db *DB) GetContentsBatch(ctx context.Context, keys []ContentKey) (map[ContentKey][]byte, error) {
	if len(keys) == 0 {
		return make(map[ContentKey][]byte), nil
	}

	// Group keys by group_id for parallel fetching
	keysByGroup := make(map[int32][]int32) // group_id -> []version_id
	for _, k := range keys {
		keysByGroup[k.GroupID] = append(keysByGroup[k.GroupID], k.VersionID)
	}

	// For small batches or single group, use simple query
	if len(keysByGroup) <= 4 {
		return db.getContentsBatchSimple(ctx, keys)
	}

	// Parallel fetch per group
	type groupResult struct {
		groupID int32
		content map[int32][]byte // version_id -> content
		err     error
	}

	results := make(chan groupResult, len(keysByGroup))

	for groupID, versionIDs := range keysByGroup {
		go func(gid int32, vids []int32) {
			content, err := db.getContentsForGroup(ctx, gid, vids)
			results <- groupResult{groupID: gid, content: content, err: err}
		}(groupID, versionIDs)
	}

	// Collect results
	result := make(map[ContentKey][]byte, len(keys))
	var firstErr error
	for i := 0; i < len(keysByGroup); i++ {
		gr := <-results
		if gr.err != nil && firstErr == nil {
			firstErr = gr.err
			continue
		}
		for versionID, content := range gr.content {
			result[ContentKey{GroupID: gr.groupID, VersionID: versionID}] = content
		}
	}

	if firstErr != nil {
		return nil, firstErr
	}
	return result, nil
}

// getContentsBatchSimple uses a single query for small batches.
func (db *DB) getContentsBatchSimple(ctx context.Context, keys []ContentKey) (map[ContentKey][]byte, error) {
	result := make(map[ContentKey][]byte, len(keys))

	groupIDs := make([]int32, len(keys))
	versionIDs := make([]int32, len(keys))
	for i, k := range keys {
		groupIDs[i] = k.GroupID
		versionIDs[i] = k.VersionID
	}

	sql := `
	SELECT c.group_id, c.version_id, c.content
	FROM pgit_content c
	JOIN unnest($1::integer[], $2::integer[]) WITH ORDINALITY AS t(gid, vid, ord)
		ON c.group_id = t.gid AND c.version_id = t.vid`

	rows, err := db.Query(ctx, sql, groupIDs, versionIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var groupID, versionID int32
		var content []byte
		if err := rows.Scan(&groupID, &versionID, &content); err != nil {
			return nil, err
		}
		result[ContentKey{GroupID: groupID, VersionID: versionID}] = content
	}

	return result, rows.Err()
}

// getContentsForGroup fetches content for a single group.
func (db *DB) getContentsForGroup(ctx context.Context, groupID int32, versionIDs []int32) (map[int32][]byte, error) {
	sql := `
	SELECT version_id, content
	FROM pgit_content
	WHERE group_id = $1 AND version_id = ANY($2)`

	rows, err := db.Query(ctx, sql, groupID, versionIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[int32][]byte, len(versionIDs))
	for rows.Next() {
		var versionID int32
		var content []byte
		if err := rows.Scan(&versionID, &content); err != nil {
			return nil, err
		}
		result[versionID] = content
	}

	return result, rows.Err()
}

// GetContentsByGroupID retrieves all content versions for a group.
// Returns content ordered by version_id ascending.
func (db *DB) GetContentsByGroupID(ctx context.Context, groupID int32) ([]*Content, error) {
	sql := `
	SELECT group_id, version_id, content
	FROM pgit_content
	WHERE group_id = $1
	ORDER BY version_id`

	rows, err := db.Query(ctx, sql, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var contents []*Content
	for rows.Next() {
		c := &Content{}
		if err := rows.Scan(&c.GroupID, &c.VersionID, &c.Content); err != nil {
			return nil, err
		}
		contents = append(contents, c)
	}

	return contents, rows.Err()
}

// ContentExists checks if content exists for a specific (group_id, version_id).
func (db *DB) ContentExists(ctx context.Context, groupID, versionID int32) (bool, error) {
	var exists bool
	err := db.QueryRow(ctx,
		"SELECT EXISTS(SELECT 1 FROM pgit_content WHERE group_id = $1 AND version_id = $2)",
		groupID, versionID).Scan(&exists)
	return exists, err
}

// CountContents returns the total number of content entries.
func (db *DB) CountContents(ctx context.Context) (int64, error) {
	var count int64
	err := db.QueryRow(ctx, "SELECT COUNT(*) FROM pgit_content").Scan(&count)
	return count, err
}

// GetContentForFileRef retrieves content for a file ref.
// This is a convenience method that combines the file ref lookup with content retrieval.
func (db *DB) GetContentForFileRef(ctx context.Context, ref *FileRef) ([]byte, error) {
	if ref == nil {
		return nil, nil
	}
	return db.GetContent(ctx, ref.GroupID, ref.VersionID)
}

// GetContentsForFileRefs retrieves content for multiple file refs.
// Returns a map of ContentKey -> content bytes.
func (db *DB) GetContentsForFileRefs(ctx context.Context, refs []*FileRef) (map[ContentKey][]byte, error) {
	if len(refs) == 0 {
		return make(map[ContentKey][]byte), nil
	}

	keys := make([]ContentKey, len(refs))
	for i, ref := range refs {
		keys[i] = ContentKey{GroupID: ref.GroupID, VersionID: ref.VersionID}
	}

	return db.GetContentsBatch(ctx, keys)
}
