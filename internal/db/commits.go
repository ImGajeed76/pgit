package db

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

// Commit represents a commit in the database
type Commit struct {
	ID          string
	ParentID    *string
	TreeHash    string
	Message     string
	AuthorName  string
	AuthorEmail string
	CreatedAt   time.Time
}

// CreateCommit inserts a new commit into the database
func (db *DB) CreateCommit(ctx context.Context, c *Commit) error {
	sql := `
	INSERT INTO pgit_commits (id, parent_id, tree_hash, message, author_name, author_email, created_at)
	VALUES ($1, $2, $3, $4, $5, $6, $7)`

	return db.Exec(ctx, sql, c.ID, c.ParentID, c.TreeHash, c.Message, c.AuthorName, c.AuthorEmail, c.CreatedAt)
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
			c.AuthorName, c.AuthorEmail, c.CreatedAt,
		}
	}

	_, err := db.pool.CopyFrom(
		ctx,
		pgx.Identifier{"pgit_commits"},
		[]string{"id", "parent_id", "tree_hash", "message", "author_name", "author_email", "created_at"},
		pgx.CopyFromRows(rows),
	)
	return err
}

// GetCommit retrieves a commit by ID
func (db *DB) GetCommit(ctx context.Context, id string) (*Commit, error) {
	sql := `
	SELECT id, parent_id, tree_hash, message, author_name, author_email, created_at
	FROM pgit_commits
	WHERE id = $1`

	c := &Commit{}
	err := db.QueryRow(ctx, sql, id).Scan(
		&c.ID, &c.ParentID, &c.TreeHash, &c.Message,
		&c.AuthorName, &c.AuthorEmail, &c.CreatedAt,
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
	SELECT c.id, c.parent_id, c.tree_hash, c.message, c.author_name, c.author_email, c.created_at
	FROM pgit_commits c
	JOIN pgit_refs r ON r.commit_id = c.id
	WHERE r.name = 'HEAD'`

	c := &Commit{}
	err := db.QueryRow(ctx, sql).Scan(
		&c.ID, &c.ParentID, &c.TreeHash, &c.Message,
		&c.AuthorName, &c.AuthorEmail, &c.CreatedAt,
	)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return c, nil
}

// GetCommitLog retrieves commits starting from HEAD, walking up the parent chain
func (db *DB) GetCommitLog(ctx context.Context, limit int) ([]*Commit, error) {
	sql := `
	WITH RECURSIVE ancestors AS (
		SELECT c.id, c.parent_id, c.tree_hash, c.message, c.author_name, c.author_email, c.created_at, 1 as depth
		FROM pgit_commits c
		JOIN pgit_refs r ON r.commit_id = c.id
		WHERE r.name = 'HEAD'
		
		UNION ALL
		
		SELECT c.id, c.parent_id, c.tree_hash, c.message, c.author_name, c.author_email, c.created_at, a.depth + 1
		FROM pgit_commits c
		JOIN ancestors a ON c.id = a.parent_id
		WHERE a.depth < $1
	)
	SELECT id, parent_id, tree_hash, message, author_name, author_email, created_at
	FROM ancestors
	ORDER BY depth`

	rows, err := db.Query(ctx, sql, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var commits []*Commit
	for rows.Next() {
		c := &Commit{}
		if err := rows.Scan(
			&c.ID, &c.ParentID, &c.TreeHash, &c.Message,
			&c.AuthorName, &c.AuthorEmail, &c.CreatedAt,
		); err != nil {
			return nil, err
		}
		commits = append(commits, c)
	}

	return commits, rows.Err()
}

// GetCommitLogFrom retrieves commits starting from a specific commit
func (db *DB) GetCommitLogFrom(ctx context.Context, commitID string, limit int) ([]*Commit, error) {
	sql := `
	WITH RECURSIVE ancestors AS (
		SELECT id, parent_id, tree_hash, message, author_name, author_email, created_at, 1 as depth
		FROM pgit_commits
		WHERE id = $1
		
		UNION ALL
		
		SELECT c.id, c.parent_id, c.tree_hash, c.message, c.author_name, c.author_email, c.created_at, a.depth + 1
		FROM pgit_commits c
		JOIN ancestors a ON c.id = a.parent_id
		WHERE a.depth < $2
	)
	SELECT id, parent_id, tree_hash, message, author_name, author_email, created_at
	FROM ancestors
	ORDER BY depth`

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
			&c.AuthorName, &c.AuthorEmail, &c.CreatedAt,
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
	SELECT id, parent_id, tree_hash, message, author_name, author_email, created_at
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
			&c.AuthorName, &c.AuthorEmail, &c.CreatedAt,
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
	// Due to ON DELETE CASCADE, this will also delete blobs
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
