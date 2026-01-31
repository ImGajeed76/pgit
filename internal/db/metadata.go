package db

import (
	"context"
)

// Metadata keys
const (
	MetaKeyRepoPath = "repo_path"
)

// EnsureMetadataTable creates the metadata table if it doesn't exist
func (db *DB) EnsureMetadataTable(ctx context.Context) error {
	return db.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS pgit_metadata (
			key     TEXT PRIMARY KEY,
			value   TEXT NOT NULL
		)
	`)
}

// GetMetadata retrieves a metadata value by key
func (db *DB) GetMetadata(ctx context.Context, key string) (string, error) {
	var value string
	err := db.QueryRow(ctx, "SELECT value FROM pgit_metadata WHERE key = $1", key).Scan(&value)
	if err != nil {
		return "", err
	}
	return value, nil
}

// SetMetadata sets a metadata key-value pair (upsert)
func (db *DB) SetMetadata(ctx context.Context, key, value string) error {
	return db.Exec(ctx, `
		INSERT INTO pgit_metadata (key, value) VALUES ($1, $2)
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value
	`, key, value)
}

// DeleteMetadata removes a metadata key
func (db *DB) DeleteMetadata(ctx context.Context, key string) error {
	return db.Exec(ctx, "DELETE FROM pgit_metadata WHERE key = $1", key)
}

// GetRepoPath returns the stored repository path, or empty string if not set
func (db *DB) GetRepoPath(ctx context.Context) string {
	path, err := db.GetMetadata(ctx, MetaKeyRepoPath)
	if err != nil {
		return ""
	}
	return path
}

// SetRepoPath stores the repository working directory path
func (db *DB) SetRepoPath(ctx context.Context, path string) error {
	return db.SetMetadata(ctx, MetaKeyRepoPath, path)
}
