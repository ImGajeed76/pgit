package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// SchemaVersion is the current schema version.
// Version 3 introduces:
// - Committer fields (committer_name, committer_email, committed_at)
// - Renamed created_at -> authored_at
// - Split content into pgit_text_content (TEXT) + pgit_binary_content (BYTEA)
// - Added is_binary flag to pgit_file_refs
const SchemaVersion = 3

// InitSchema creates the pgit schema in the database
func (db *DB) InitSchema(ctx context.Context) error {
	// Check for existing schema and validate version
	exists, err := db.SchemaExists(ctx)
	if err != nil {
		return fmt.Errorf("failed to check schema: %w", err)
	}

	if exists {
		// Check schema version
		version, err := db.GetSchemaVersion(ctx)
		if err != nil {
			return fmt.Errorf("failed to get schema version: %w", err)
		}

		if version < SchemaVersion {
			return fmt.Errorf("schema version %d detected (current is %d).\n\n"+
				"The database schema has changed. Please re-import your repository:\n"+
				"  pgit import --force /path/to/git/repo\n\n"+
				"This will recreate the database with the new optimized schema.",
				version, SchemaVersion)
		}

		// Schema is up to date, nothing to do
		return nil
	}

	// Create extension first
	if err := db.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS pg_xpatch"); err != nil {
		return fmt.Errorf("failed to create pg_xpatch extension: %w", err)
	}

	// Create tables in order (respecting dependencies)
	if err := db.createMetadataTable(ctx); err != nil {
		return err
	}
	if err := db.createCommitsTable(ctx); err != nil {
		return err
	}
	if err := db.createPathsTable(ctx); err != nil {
		return err
	}
	if err := db.createFileRefsTable(ctx); err != nil {
		return err
	}
	if err := db.createTextContentTable(ctx); err != nil {
		return err
	}
	if err := db.createBinaryContentTable(ctx); err != nil {
		return err
	}
	if err := db.createRefsTable(ctx); err != nil {
		return err
	}
	if err := db.createSyncStateTable(ctx); err != nil {
		return err
	}

	// Set schema version
	if err := db.SetSchemaVersion(ctx, SchemaVersion); err != nil {
		return fmt.Errorf("failed to set schema version: %w", err)
	}

	return nil
}

func (db *DB) createMetadataTable(ctx context.Context) error {
	sql := `
	CREATE TABLE IF NOT EXISTS pgit_metadata (
		key     TEXT PRIMARY KEY,
		value   TEXT NOT NULL
	)`

	if err := db.Exec(ctx, sql); err != nil {
		return fmt.Errorf("failed to create pgit_metadata: %w", err)
	}

	return nil
}

func (db *DB) createCommitsTable(ctx context.Context) error {
	// Create table with committer fields and renamed authored_at
	sql := `
	CREATE TABLE IF NOT EXISTS pgit_commits (
		id              TEXT PRIMARY KEY,
		parent_id       TEXT,
		tree_hash       TEXT NOT NULL,
		message         TEXT NOT NULL,
		author_name     TEXT NOT NULL,
		author_email    TEXT NOT NULL,
		authored_at     TIMESTAMPTZ NOT NULL,
		committer_name  TEXT NOT NULL,
		committer_email TEXT NOT NULL,
		committed_at    TIMESTAMPTZ NOT NULL
	) USING xpatch`

	if err := db.Exec(ctx, sql); err != nil {
		return fmt.Errorf("failed to create pgit_commits: %w", err)
	}

	// Configure xpatch
	configSQL := `
	SELECT xpatch.configure('pgit_commits',
		order_by => 'authored_at',
		delta_columns => ARRAY['message', 'author_name', 'author_email',
		                       'committer_name', 'committer_email'],
		keyframe_every => 100,
		compress_depth => 50
	)`

	// Ignore error if already configured
	_ = db.Exec(ctx, configSQL)

	// Create indexes
	_ = db.Exec(ctx, "CREATE INDEX IF NOT EXISTS idx_commits_parent ON pgit_commits(parent_id)")
	_ = db.Exec(ctx, "CREATE INDEX IF NOT EXISTS idx_commits_authored ON pgit_commits(authored_at DESC)")

	return nil
}

// createPathsTable creates the path registry table.
func (db *DB) createPathsTable(ctx context.Context) error {
	sql := `
	CREATE TABLE IF NOT EXISTS pgit_paths (
		group_id    INTEGER PRIMARY KEY GENERATED ALWAYS AS IDENTITY,
		path        TEXT NOT NULL UNIQUE
	)`

	if err := db.Exec(ctx, sql); err != nil {
		return fmt.Errorf("failed to create pgit_paths: %w", err)
	}

	_ = db.Exec(ctx, "CREATE INDEX IF NOT EXISTS idx_paths_path ON pgit_paths(path)")

	return nil
}

// createFileRefsTable creates the file references table with is_binary flag.
func (db *DB) createFileRefsTable(ctx context.Context) error {
	sql := `
	CREATE TABLE IF NOT EXISTS pgit_file_refs (
		group_id        INTEGER NOT NULL,
		commit_id       TEXT NOT NULL,
		version_id      INTEGER NOT NULL,
		content_hash    BYTEA,
		mode            INTEGER NOT NULL DEFAULT 33188,
		is_symlink      BOOLEAN NOT NULL DEFAULT FALSE,
		symlink_target  TEXT,
		is_binary       BOOLEAN NOT NULL DEFAULT FALSE,
		PRIMARY KEY (group_id, commit_id)
	)`

	if err := db.Exec(ctx, sql); err != nil {
		return fmt.Errorf("failed to create pgit_file_refs: %w", err)
	}

	_ = db.Exec(ctx, "CREATE INDEX IF NOT EXISTS idx_file_refs_commit ON pgit_file_refs(commit_id)")
	_ = db.Exec(ctx, "CREATE INDEX IF NOT EXISTS idx_file_refs_version ON pgit_file_refs(group_id, version_id)")

	return nil
}

// createTextContentTable creates the text content storage table (TEXT column).
func (db *DB) createTextContentTable(ctx context.Context) error {
	sql := `
	CREATE TABLE IF NOT EXISTS pgit_text_content (
		group_id    INTEGER NOT NULL,
		version_id  INTEGER NOT NULL,
		content     TEXT NOT NULL DEFAULT '',
		PRIMARY KEY (group_id, version_id)
	) USING xpatch`

	if err := db.Exec(ctx, sql); err != nil {
		return fmt.Errorf("failed to create pgit_text_content: %w", err)
	}

	configSQL := `
	SELECT xpatch.configure('pgit_text_content',
		group_by => 'group_id',
		order_by => 'version_id',
		delta_columns => ARRAY['content'],
		keyframe_every => 100,
		compress_depth => 5
	)`

	_ = db.Exec(ctx, configSQL)

	return nil
}

// createBinaryContentTable creates the binary content storage table (BYTEA column).
func (db *DB) createBinaryContentTable(ctx context.Context) error {
	sql := `
	CREATE TABLE IF NOT EXISTS pgit_binary_content (
		group_id    INTEGER NOT NULL,
		version_id  INTEGER NOT NULL,
		content     BYTEA NOT NULL DEFAULT ''::bytea,
		PRIMARY KEY (group_id, version_id)
	) USING xpatch`

	if err := db.Exec(ctx, sql); err != nil {
		return fmt.Errorf("failed to create pgit_binary_content: %w", err)
	}

	configSQL := `
	SELECT xpatch.configure('pgit_binary_content',
		group_by => 'group_id',
		order_by => 'version_id',
		delta_columns => ARRAY['content'],
		keyframe_every => 100,
		compress_depth => 5
	)`

	_ = db.Exec(ctx, configSQL)

	return nil
}

func (db *DB) createRefsTable(ctx context.Context) error {
	sql := `
	CREATE TABLE IF NOT EXISTS pgit_refs (
		name        TEXT PRIMARY KEY,
		commit_id   TEXT NOT NULL
	)`

	if err := db.Exec(ctx, sql); err != nil {
		return fmt.Errorf("failed to create pgit_refs: %w", err)
	}

	return nil
}

func (db *DB) createSyncStateTable(ctx context.Context) error {
	sql := `
	CREATE TABLE IF NOT EXISTS pgit_sync_state (
		remote_name     TEXT PRIMARY KEY,
		last_commit_id  TEXT,
		synced_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
	)`

	if err := db.Exec(ctx, sql); err != nil {
		return fmt.Errorf("failed to create pgit_sync_state: %w", err)
	}

	return nil
}

// DropFileRefsIndexes drops the secondary indexes on pgit_file_refs.
// The primary key (group_id, commit_id) is kept for COPY conflict detection.
// Call this before bulk import to avoid random B-tree insertions, then
// call CreateFileRefsIndexes after import to rebuild them efficiently.
func (db *DB) DropFileRefsIndexes(ctx context.Context) error {
	if err := db.Exec(ctx, "DROP INDEX IF EXISTS idx_file_refs_commit"); err != nil {
		return fmt.Errorf("failed to drop idx_file_refs_commit: %w", err)
	}
	if err := db.Exec(ctx, "DROP INDEX IF EXISTS idx_file_refs_version"); err != nil {
		return fmt.Errorf("failed to drop idx_file_refs_version: %w", err)
	}
	return nil
}

// CreateFileRefsIndexes creates the secondary indexes on pgit_file_refs.
// This is much faster than maintaining indexes during bulk insert because
// PostgreSQL can sort and bulk-load the B-tree in a single pass.
func (db *DB) CreateFileRefsIndexes(ctx context.Context) error {
	if err := db.Exec(ctx, "CREATE INDEX IF NOT EXISTS idx_file_refs_commit ON pgit_file_refs(commit_id)"); err != nil {
		return fmt.Errorf("failed to create idx_file_refs_commit: %w", err)
	}
	if err := db.Exec(ctx, "CREATE INDEX IF NOT EXISTS idx_file_refs_version ON pgit_file_refs(group_id, version_id)"); err != nil {
		return fmt.Errorf("failed to create idx_file_refs_version: %w", err)
	}
	return nil
}

// SchemaExists checks if the pgit schema exists
func (db *DB) SchemaExists(ctx context.Context) (bool, error) {
	var exists bool
	err := db.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT FROM information_schema.tables 
			WHERE table_name = 'pgit_commits'
		)
	`).Scan(&exists)
	return exists, err
}

// GetSchemaVersion returns the current schema version from the database.
// Returns 1 for legacy schemas that don't have a version set.
func (db *DB) GetSchemaVersion(ctx context.Context) (int, error) {
	var value string
	err := db.QueryRow(ctx,
		"SELECT value FROM pgit_metadata WHERE key = 'schema_version'",
	).Scan(&value)

	if err == pgx.ErrNoRows {
		// Legacy schema without version - assume version 1
		return 1, nil
	}
	if err != nil {
		return 0, err
	}

	var version int
	_, err = fmt.Sscanf(value, "%d", &version)
	if err != nil {
		return 0, fmt.Errorf("invalid schema version: %s", value)
	}

	return version, nil
}

// SetSchemaVersion sets the schema version in the database.
func (db *DB) SetSchemaVersion(ctx context.Context, version int) error {
	sql := `
	INSERT INTO pgit_metadata (key, value) VALUES ('schema_version', $1)
	ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value`

	return db.Exec(ctx, sql, fmt.Sprintf("%d", version))
}

// DropSchema drops all pgit tables (use with caution!)
func (db *DB) DropSchema(ctx context.Context) error {
	tables := []string{
		"pgit_metadata",
		"pgit_sync_state",
		"pgit_refs",
		"pgit_text_content",
		"pgit_binary_content",
		"pgit_content", // Legacy v2 table
		"pgit_file_refs",
		"pgit_paths",
		"pgit_commits",
		// Legacy table from schema v1 (may not exist)
		"pgit_blobs",
	}

	for _, table := range tables {
		if err := db.Exec(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s CASCADE", table)); err != nil {
			return fmt.Errorf("failed to drop %s: %w", table, err)
		}
	}

	return nil
}

// IsSchemaAtLeast checks if the database schema is at least the given version.
func (db *DB) IsSchemaAtLeast(ctx context.Context, minVersion int) (bool, error) {
	version, err := db.GetSchemaVersion(ctx)
	if err != nil {
		return false, err
	}
	return version >= minVersion, nil
}
