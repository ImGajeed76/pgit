package db

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// Ref represents a named reference to a commit
type Ref struct {
	Name     string
	CommitID string
}

// GetRef retrieves a ref by name
func (db *DB) GetRef(ctx context.Context, name string) (*Ref, error) {
	sql := `SELECT name, commit_id FROM pgit_refs WHERE name = $1`

	r := &Ref{}
	err := db.QueryRow(ctx, sql, name).Scan(&r.Name, &r.CommitID)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return r, nil
}

// SetRef creates or updates a ref
func (db *DB) SetRef(ctx context.Context, name, commitID string) error {
	sql := `
	INSERT INTO pgit_refs (name, commit_id) VALUES ($1, $2)
	ON CONFLICT (name) DO UPDATE SET commit_id = EXCLUDED.commit_id`

	return db.Exec(ctx, sql, name, commitID)
}

// DeleteRef deletes a ref
func (db *DB) DeleteRef(ctx context.Context, name string) error {
	return db.Exec(ctx, "DELETE FROM pgit_refs WHERE name = $1", name)
}

// GetAllRefs retrieves all refs
func (db *DB) GetAllRefs(ctx context.Context) ([]*Ref, error) {
	sql := `SELECT name, commit_id FROM pgit_refs ORDER BY name`

	rows, err := db.Query(ctx, sql)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var refs []*Ref
	for rows.Next() {
		r := &Ref{}
		if err := rows.Scan(&r.Name, &r.CommitID); err != nil {
			return nil, err
		}
		refs = append(refs, r)
	}

	return refs, rows.Err()
}

// GetHead returns the HEAD commit ID
func (db *DB) GetHead(ctx context.Context) (string, error) {
	ref, err := db.GetRef(ctx, "HEAD")
	if err != nil {
		return "", err
	}
	if ref == nil {
		return "", nil
	}
	return ref.CommitID, nil
}

// SetHead sets the HEAD to point to a commit
func (db *DB) SetHead(ctx context.Context, commitID string) error {
	return db.SetRef(ctx, "HEAD", commitID)
}

// RefExists checks if a ref exists
func (db *DB) RefExists(ctx context.Context, name string) (bool, error) {
	var exists bool
	err := db.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM pgit_refs WHERE name = $1)", name).Scan(&exists)
	return exists, err
}
