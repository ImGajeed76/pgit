package db

import (
	"context"

	"github.com/imgajeed76/pgit/v4/internal/util"
	"github.com/jackc/pgx/v5"
)

// Content represents file content stored in text or binary content tables.
// Content is delta-compressed by xpatch, grouped by group_id.
type Content struct {
	GroupID   int32
	VersionID int32
	Content   []byte
	IsBinary  bool // Determines which table to use
}

// ContentKey is used for batch lookups.
type ContentKey struct {
	GroupID   int32
	VersionID int32
}

// CreateContent inserts content into the appropriate content table.
func (db *DB) CreateContent(ctx context.Context, c *Content) error {
	content := c.Content
	if content == nil {
		content = []byte{}
	}

	if c.IsBinary {
		sql := `INSERT INTO pgit_binary_content (group_id, version_id, content) VALUES ($1, $2, $3)`
		return db.Exec(ctx, sql, c.GroupID, c.VersionID, content)
	}

	sql := `INSERT INTO pgit_text_content (group_id, version_id, content) VALUES ($1, $2, $3)`
	return db.Exec(ctx, sql, c.GroupID, c.VersionID, util.ToValidUTF8(string(content)))
}

// CreateContentTx inserts content within a transaction.
func (db *DB) CreateContentTx(ctx context.Context, tx pgx.Tx, c *Content) error {
	content := c.Content
	if content == nil {
		content = []byte{}
	}

	if c.IsBinary {
		_, err := tx.Exec(ctx,
			`INSERT INTO pgit_binary_content (group_id, version_id, content) VALUES ($1, $2, $3)`,
			c.GroupID, c.VersionID, content)
		return err
	}

	_, err := tx.Exec(ctx,
		`INSERT INTO pgit_text_content (group_id, version_id, content) VALUES ($1, $2, $3)`,
		c.GroupID, c.VersionID, util.ToValidUTF8(string(content)))
	return err
}

// CreateContentBatch inserts multiple content entries using COPY for speed.
// Splits into text and binary batches and writes to respective tables.
func (db *DB) CreateContentBatch(ctx context.Context, contents []*Content) error {
	if len(contents) == 0 {
		return nil
	}

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

		_, err := db.pool.CopyFrom(
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

		_, err := db.pool.CopyFrom(
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

// GetContent retrieves content by (group_id, version_id).
// isBinary determines which table to query.
func (db *DB) GetContent(ctx context.Context, groupID, versionID int32, isBinary bool) ([]byte, error) {
	var content []byte
	var err error

	if isBinary {
		err = db.QueryRow(ctx,
			"SELECT content FROM pgit_binary_content WHERE group_id = $1 AND version_id = $2",
			groupID, versionID,
		).Scan(&content)
	} else {
		var textContent string
		err = db.QueryRow(ctx,
			"SELECT content FROM pgit_text_content WHERE group_id = $1 AND version_id = $2",
			groupID, versionID,
		).Scan(&textContent)
		if err == nil {
			content = []byte(textContent)
		}
	}

	if err == pgx.ErrNoRows {
		return nil, nil
	}

	return content, err
}

// GetContentsBatch retrieves multiple contents by their keys.
// isBinaryMap indicates which keys are binary. Keys not in the map default to text.
func (db *DB) GetContentsBatch(ctx context.Context, keys []ContentKey, isBinaryMap map[ContentKey]bool) (map[ContentKey][]byte, error) {
	if len(keys) == 0 {
		return make(map[ContentKey][]byte), nil
	}

	// Split keys into text and binary
	var textKeys []ContentKey
	var binaryKeys []ContentKey
	for _, k := range keys {
		if isBinaryMap[k] {
			binaryKeys = append(binaryKeys, k)
		} else {
			textKeys = append(textKeys, k)
		}
	}

	result := make(map[ContentKey][]byte, len(keys))

	// Fetch text content
	if len(textKeys) > 0 {
		textResult, err := db.getContentsBatchFromTable(ctx, "pgit_text_content", textKeys, false)
		if err != nil {
			return nil, err
		}
		for k, v := range textResult {
			result[k] = v
		}
	}

	// Fetch binary content
	if len(binaryKeys) > 0 {
		binResult, err := db.getContentsBatchFromTable(ctx, "pgit_binary_content", binaryKeys, true)
		if err != nil {
			return nil, err
		}
		for k, v := range binResult {
			result[k] = v
		}
	}

	return result, nil
}

// getContentsBatchFromTable fetches content from a specific table.
func (db *DB) getContentsBatchFromTable(ctx context.Context, tableName string, keys []ContentKey, isBinary bool) (map[ContentKey][]byte, error) {
	if len(keys) == 0 {
		return make(map[ContentKey][]byte), nil
	}

	// Group keys by group_id for parallel fetching
	keysByGroup := make(map[int32][]int32)
	for _, k := range keys {
		keysByGroup[k.GroupID] = append(keysByGroup[k.GroupID], k.VersionID)
	}

	// For small batches, use simple query
	if len(keysByGroup) <= 4 {
		return db.getContentsBatchSimpleFromTable(ctx, tableName, keys, isBinary)
	}

	// Parallel fetch per group
	type groupResult struct {
		groupID int32
		content map[int32][]byte
		err     error
	}

	results := make(chan groupResult, len(keysByGroup))

	for groupID, versionIDs := range keysByGroup {
		go func(gid int32, vids []int32) {
			content, err := db.getContentsForGroupFromTable(ctx, tableName, gid, vids, isBinary)
			results <- groupResult{groupID: gid, content: content, err: err}
		}(groupID, versionIDs)
	}

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

// getContentsBatchSimpleFromTable uses a single query for small batches.
func (db *DB) getContentsBatchSimpleFromTable(ctx context.Context, tableName string, keys []ContentKey, isBinary bool) (map[ContentKey][]byte, error) {
	result := make(map[ContentKey][]byte, len(keys))

	groupIDs := make([]int32, len(keys))
	versionIDs := make([]int32, len(keys))
	for i, k := range keys {
		groupIDs[i] = k.GroupID
		versionIDs[i] = k.VersionID
	}

	sql := `
	SELECT c.group_id, c.version_id, c.content
	FROM ` + tableName + ` c
	JOIN unnest($1::integer[], $2::integer[]) WITH ORDINALITY AS t(gid, vid, ord)
		ON c.group_id = t.gid AND c.version_id = t.vid`

	rows, err := db.Query(ctx, sql, groupIDs, versionIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var groupID, versionID int32
		if isBinary {
			var content []byte
			if err := rows.Scan(&groupID, &versionID, &content); err != nil {
				return nil, err
			}
			result[ContentKey{GroupID: groupID, VersionID: versionID}] = content
		} else {
			var content string
			if err := rows.Scan(&groupID, &versionID, &content); err != nil {
				return nil, err
			}
			result[ContentKey{GroupID: groupID, VersionID: versionID}] = []byte(content)
		}
	}

	return result, rows.Err()
}

// getContentsForGroupFromTable fetches content for a single group from a specific table.
func (db *DB) getContentsForGroupFromTable(ctx context.Context, tableName string, groupID int32, versionIDs []int32, isBinary bool) (map[int32][]byte, error) {
	sql := `
	SELECT version_id, content
	FROM ` + tableName + `
	WHERE group_id = $1 AND version_id = ANY($2)`

	rows, err := db.Query(ctx, sql, groupID, versionIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[int32][]byte, len(versionIDs))
	for rows.Next() {
		var versionID int32
		if isBinary {
			var content []byte
			if err := rows.Scan(&versionID, &content); err != nil {
				return nil, err
			}
			result[versionID] = content
		} else {
			var content string
			if err := rows.Scan(&versionID, &content); err != nil {
				return nil, err
			}
			result[versionID] = []byte(content)
		}
	}

	return result, rows.Err()
}

// GetContentsByGroupID retrieves all content versions for a group.
// Queries both tables and merges results.
func (db *DB) GetContentsByGroupID(ctx context.Context, groupID int32) ([]*Content, error) {
	var contents []*Content

	// Query text content
	textSQL := `SELECT group_id, version_id, content FROM pgit_text_content WHERE group_id = $1 ORDER BY version_id`
	rows, err := db.Query(ctx, textSQL, groupID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		c := &Content{IsBinary: false}
		var textContent string
		if err := rows.Scan(&c.GroupID, &c.VersionID, &textContent); err != nil {
			rows.Close()
			return nil, err
		}
		c.Content = []byte(textContent)
		contents = append(contents, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Query binary content
	binSQL := `SELECT group_id, version_id, content FROM pgit_binary_content WHERE group_id = $1 ORDER BY version_id`
	rows, err = db.Query(ctx, binSQL, groupID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		c := &Content{IsBinary: true}
		if err := rows.Scan(&c.GroupID, &c.VersionID, &c.Content); err != nil {
			rows.Close()
			return nil, err
		}
		contents = append(contents, c)
	}
	rows.Close()

	return contents, rows.Err()
}

// GetAllContentForGroup retrieves all content versions for a single group,
// ordered by version_id ASC (front-to-back). This is the fastest access pattern
// for xpatch: a single Index Scan through the delta chain, decompressing
// sequentially. The caller can reverse the slice in Go for newest-first iteration.
func (db *DB) GetAllContentForGroup(ctx context.Context, groupID int32, isBinary bool) ([]ContentVersionPair, error) {
	var rows pgx.Rows
	var err error

	if isBinary {
		rows, err = db.Query(ctx,
			"SELECT version_id, content FROM pgit_binary_content WHERE group_id = $1 ORDER BY version_id ASC",
			groupID)
	} else {
		rows, err = db.Query(ctx,
			"SELECT version_id, content FROM pgit_text_content WHERE group_id = $1 ORDER BY version_id ASC",
			groupID)
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var result []ContentVersionPair
	for rows.Next() {
		var p ContentVersionPair
		if isBinary {
			if err := rows.Scan(&p.VersionID, &p.Content); err != nil {
				return nil, err
			}
		} else {
			var text string
			if err := rows.Scan(&p.VersionID, &text); err != nil {
				return nil, err
			}
			p.Content = []byte(text)
		}
		result = append(result, p)
	}

	return result, rows.Err()
}

// ContentVersionPair holds a version_id and its content.
type ContentVersionPair struct {
	VersionID int32
	Content   []byte
}

// ContentExists checks if content exists in either table.
func (db *DB) ContentExists(ctx context.Context, groupID, versionID int32) (bool, error) {
	var exists bool
	err := db.QueryRow(ctx,
		`SELECT EXISTS(
			SELECT 1 FROM pgit_text_content WHERE group_id = $1 AND version_id = $2
			UNION ALL
			SELECT 1 FROM pgit_binary_content WHERE group_id = $1 AND version_id = $2
		)`,
		groupID, versionID).Scan(&exists)
	return exists, err
}

// CountContents returns the total number of content entries across both tables.
func (db *DB) CountContents(ctx context.Context) (int64, error) {
	var count int64
	err := db.QueryRow(ctx, `
		SELECT (SELECT COUNT(*) FROM pgit_text_content) + (SELECT COUNT(*) FROM pgit_binary_content)
	`).Scan(&count)
	return count, err
}

// GetContentForFileRef retrieves content for a file ref.
// Returns nil for deleted refs (ContentHash == nil) since they have no content row.
// In v4, groupID must be provided since FileRef no longer carries it.
func (db *DB) GetContentForFileRef(ctx context.Context, ref *FileRef, groupID int32) ([]byte, error) {
	if ref == nil || ref.ContentHash == nil {
		return nil, nil
	}
	return db.GetContent(ctx, groupID, ref.VersionID, ref.IsBinary)
}

// GetContentsForFileRefs retrieves content for multiple file refs.
// Skips deleted refs (ContentHash == nil) since they have no content row.
// In v4, groupID must be provided since FileRef no longer carries it.
// All refs must belong to the same group.
func (db *DB) GetContentsForFileRefs(ctx context.Context, refs []*FileRef, groupID int32) (map[ContentKey][]byte, error) {
	if len(refs) == 0 {
		return make(map[ContentKey][]byte), nil
	}

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

	return db.GetContentsBatch(ctx, keys, isBinaryMap)
}
