package db

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Commit represents a commit in the database
type Commit struct {
	ID             string
	ParentID       *string
	TreeHash       string
	Message        string
	AuthorName     string
	AuthorEmail    string
	AuthoredAt     time.Time
	CommitterName  string
	CommitterEmail string
	CommittedAt    time.Time
}

// CreateCommit inserts a new commit into the database
func (db *DB) CreateCommit(ctx context.Context, c *Commit) error {
	sql := `
	INSERT INTO pgit_commits (id, parent_id, tree_hash, message, author_name, author_email, authored_at, committer_name, committer_email, committed_at)
	VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`

	return db.Exec(ctx, sql, c.ID, c.ParentID, c.TreeHash, c.Message,
		c.AuthorName, c.AuthorEmail, c.AuthoredAt,
		c.CommitterName, c.CommitterEmail, c.CommittedAt)
}

// CreateCommitsBatch inserts multiple commits using pgx.CopyFrom for speed
// Commits must be in order (parents before children)
func (db *DB) CreateCommitsBatch(ctx context.Context, commits []*Commit) error {
	if len(commits) == 0 {
		return nil
	}

	rows := make([][]interface{}, len(commits))
	for i, c := range commits {
		rows[i] = []interface{}{
			c.ID, c.ParentID, c.TreeHash, c.Message,
			c.AuthorName, c.AuthorEmail, c.AuthoredAt,
			c.CommitterName, c.CommitterEmail, c.CommittedAt,
		}
	}

	_, err := db.pool.CopyFrom(
		ctx,
		pgx.Identifier{"pgit_commits"},
		[]string{"id", "parent_id", "tree_hash", "message",
			"author_name", "author_email", "authored_at",
			"committer_name", "committer_email", "committed_at"},
		pgx.CopyFromRows(rows),
	)
	return err
}

// GetCommit retrieves a commit by ID
func (db *DB) GetCommit(ctx context.Context, id string) (*Commit, error) {
	sql := `
	SELECT id, parent_id, tree_hash, message, author_name, author_email, authored_at,
	       committer_name, committer_email, committed_at
	FROM pgit_commits
	WHERE id = $1`

	c := &Commit{}
	err := db.QueryRow(ctx, sql, id).Scan(
		&c.ID, &c.ParentID, &c.TreeHash, &c.Message,
		&c.AuthorName, &c.AuthorEmail, &c.AuthoredAt,
		&c.CommitterName, &c.CommitterEmail, &c.CommittedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return c, nil
}

// GetHeadCommit retrieves the commit that HEAD points to
func (db *DB) GetHeadCommit(ctx context.Context) (*Commit, error) {
	sql := `
	SELECT c.id, c.parent_id, c.tree_hash, c.message, c.author_name, c.author_email, c.authored_at,
	       c.committer_name, c.committer_email, c.committed_at
	FROM pgit_commits c
	JOIN pgit_refs r ON r.commit_id = c.id
	WHERE r.name = 'HEAD'`

	c := &Commit{}
	err := db.QueryRow(ctx, sql).Scan(
		&c.ID, &c.ParentID, &c.TreeHash, &c.Message,
		&c.AuthorName, &c.AuthorEmail, &c.AuthoredAt,
		&c.CommitterName, &c.CommitterEmail, &c.CommittedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return c, nil
}

// GetCommitLog retrieves commits starting from HEAD in reverse chronological order.
// Uses a range query on ULID-ordered IDs instead of a recursive CTE,
// which is much faster on xpatch tables (sequential scan vs random PK lookups).
func (db *DB) GetCommitLog(ctx context.Context, limit int) ([]*Commit, error) {
	// Get HEAD from pgit_refs (normal table, instant)
	headID, err := db.GetHead(ctx)
	if err != nil {
		return nil, err
	}
	if headID == "" {
		return nil, nil
	}
	return db.GetCommitLogFrom(ctx, headID, limit)
}

// GetCommitLogFrom retrieves commits in reverse chronological order starting from a commit.
// Since commit IDs are ULIDs (lexicographically = chronologically ordered),
// we use a simple range query instead of walking parent_id chains.
func (db *DB) GetCommitLogFrom(ctx context.Context, commitID string, limit int) ([]*Commit, error) {
	sql := `
	SELECT id, parent_id, tree_hash, message, author_name, author_email, authored_at,
	       committer_name, committer_email, committed_at
	FROM pgit_commits
	WHERE id <= $1
	ORDER BY id DESC
	LIMIT $2`

	rows, err := db.Query(ctx, sql, commitID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var commits []*Commit
	for rows.Next() {
		c := &Commit{}
		if err := rows.Scan(
			&c.ID, &c.ParentID, &c.TreeHash, &c.Message,
			&c.AuthorName, &c.AuthorEmail, &c.AuthoredAt,
			&c.CommitterName, &c.CommitterEmail, &c.CommittedAt,
		); err != nil {
			return nil, err
		}
		commits = append(commits, c)
	}

	return commits, rows.Err()
}

// GetAllCommits retrieves all commits ordered by ID (ULID = time order)
func (db *DB) GetAllCommits(ctx context.Context) ([]*Commit, error) {
	sql := `
	SELECT id, parent_id, tree_hash, message, author_name, author_email, authored_at,
	       committer_name, committer_email, committed_at
	FROM pgit_commits
	ORDER BY id`

	rows, err := db.Query(ctx, sql)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var commits []*Commit
	for rows.Next() {
		c := &Commit{}
		if err := rows.Scan(
			&c.ID, &c.ParentID, &c.TreeHash, &c.Message,
			&c.AuthorName, &c.AuthorEmail, &c.AuthoredAt,
			&c.CommitterName, &c.CommitterEmail, &c.CommittedAt,
		); err != nil {
			return nil, err
		}
		commits = append(commits, c)
	}

	return commits, rows.Err()
}

// CountCommits returns the total number of commits
func (db *DB) CountCommits(ctx context.Context) (int, error) {
	var count int
	err := db.QueryRow(ctx, "SELECT COUNT(*) FROM pgit_commits").Scan(&count)
	return count, err
}

// DeleteCommitsAfter deletes all commits after (and including) the given commit ID
// This is used during the "rebuild on divergence" process
func (db *DB) DeleteCommitsAfter(ctx context.Context, commitID string) error {
	sql := `DELETE FROM pgit_commits WHERE id >= $1`
	return db.Exec(ctx, sql, commitID)
}

// CommitExists checks if a commit exists
func (db *DB) CommitExists(ctx context.Context, id string) (bool, error) {
	var exists bool
	err := db.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pgit_commits WHERE id = $1)", id).Scan(&exists)
	return exists, err
}

// GetLatestCommitID returns the ID of the latest commit (by ULID order)
func (db *DB) GetLatestCommitID(ctx context.Context) (string, error) {
	var id string
	err := db.QueryRow(ctx, "SELECT id FROM pgit_commits ORDER BY id DESC LIMIT 1").Scan(&id)
	if err == pgx.ErrNoRows {
		return "", nil
	}
	return id, err
}

// FindCommonAncestor finds the common ancestor between two commits
func (db *DB) FindCommonAncestor(ctx context.Context, commitA, commitB string) (string, error) {
	sql := `
	WITH RECURSIVE 
	ancestors_a AS (
		SELECT id, parent_id FROM pgit_commits WHERE id = $1
		UNION ALL
		SELECT c.id, c.parent_id FROM pgit_commits c JOIN ancestors_a a ON c.id = a.parent_id
	),
	ancestors_b AS (
		SELECT id, parent_id FROM pgit_commits WHERE id = $2
		UNION ALL
		SELECT c.id, c.parent_id FROM pgit_commits c JOIN ancestors_b b ON c.id = b.parent_id
	)
	SELECT a.id FROM ancestors_a a JOIN ancestors_b b ON a.id = b.id
	ORDER BY a.id DESC LIMIT 1`

	var id string
	err := db.QueryRow(ctx, sql, commitA, commitB).Scan(&id)
	if err == pgx.ErrNoRows {
		return "", nil
	}
	return id, err
}

// FindCommitByPartialID finds a commit by partial ID match.
// Uses prefix range scan first (fast, uses xpatch PK index), then falls back
// to suffix match on pgit_file_refs (normal table) if no prefix match found.
func (db *DB) FindCommitByPartialID(ctx context.Context, partialID string) (*Commit, error) {
	// Step 1: Try prefix match using range scan (fast on xpatch PK index)
	upperBound := partialID[:len(partialID)-1] + string(partialID[len(partialID)-1]+1)
	prefixSQL := `
	SELECT id FROM pgit_commits
	WHERE id >= $1 AND id < $2
	LIMIT 2`

	rows, err := db.Query(ctx, prefixSQL, partialID, upperBound)
	if err != nil {
		return nil, err
	}

	var matchIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		matchIDs = append(matchIDs, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Step 2: If no prefix match, try suffix match on pgit_file_refs (normal table)
	if len(matchIDs) == 0 {
		suffixSQL := `
		SELECT DISTINCT commit_id FROM pgit_file_refs
		WHERE commit_id LIKE '%' || $1
		LIMIT 2`

		rows, err := db.Query(ctx, suffixSQL, partialID)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return nil, err
			}
			matchIDs = append(matchIDs, id)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}

	switch len(matchIDs) {
	case 0:
		return nil, nil
	case 1:
		return db.GetCommit(ctx, matchIDs[0])
	default:
		return nil, fmt.Errorf("ambiguous commit reference '%s' matches multiple commits", partialID)
	}
}

// GetCommitsBatch retrieves multiple commits by their IDs in a single query.
func (db *DB) GetCommitsBatch(ctx context.Context, ids []string) (map[string]*Commit, error) {
	if len(ids) == 0 {
		return make(map[string]*Commit), nil
	}

	sql := `
	SELECT id, parent_id, tree_hash, message, author_name, author_email, authored_at,
	       committer_name, committer_email, committed_at
	FROM pgit_commits
	WHERE id = ANY($1)`

	rows, err := db.Query(ctx, sql, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]*Commit, len(ids))
	for rows.Next() {
		c := &Commit{}
		if err := rows.Scan(
			&c.ID, &c.ParentID, &c.TreeHash, &c.Message,
			&c.AuthorName, &c.AuthorEmail, &c.AuthoredAt,
			&c.CommitterName, &c.CommitterEmail, &c.CommittedAt,
		); err != nil {
			return nil, err
		}
		result[c.ID] = c
	}

	return result, rows.Err()
}

// GetCommitsBatchByRange retrieves multiple commits using a range scan instead
// of ANY(). This is much faster on xpatch tables because it scans a contiguous
// range of the delta chain instead of doing random-access per ID.
// The ids slice is used to filter results in Go after the range scan.
func (db *DB) GetCommitsBatchByRange(ctx context.Context, ids []string) (map[string]*Commit, error) {
	if len(ids) == 0 {
		return make(map[string]*Commit), nil
	}

	// Build a set for fast lookup
	idSet := make(map[string]bool, len(ids))
	minID := ids[0]
	maxID := ids[0]
	for _, id := range ids {
		idSet[id] = true
		if id < minID {
			minID = id
		}
		if id > maxID {
			maxID = id
		}
	}

	// Range scan: sequential read of the xpatch delta chain from min to max.
	// This decompresses the chain segment once vs random-access per ID.
	sql := `
	SELECT id, parent_id, tree_hash, message, author_name, author_email, authored_at,
	       committer_name, committer_email, committed_at
	FROM pgit_commits
	WHERE id >= $1 AND id <= $2
	ORDER BY id`

	rows, err := db.Query(ctx, sql, minID, maxID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]*Commit, len(ids))
	for rows.Next() {
		c := &Commit{}
		if err := rows.Scan(
			&c.ID, &c.ParentID, &c.TreeHash, &c.Message,
			&c.AuthorName, &c.AuthorEmail, &c.AuthoredAt,
			&c.CommitterName, &c.CommitterEmail, &c.CommittedAt,
		); err != nil {
			return nil, err
		}
		// Filter: only keep commits we actually asked for
		if idSet[c.ID] {
			result[c.ID] = c
		}
	}

	return result, rows.Err()
}
