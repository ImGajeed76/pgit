package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"golang.org/x/sync/errgroup"
)

// SchemaVersion is the current schema version.
// Version 4 introduces:
// - N:1 path-to-group mapping (multiple paths can share one delta group)
// - path_id as PK in pgit_paths and pgit_file_refs
// - group_id remains for delta compression grouping in content tables
// - compress_depth increased to 10 for better deduplication
// - Removed reset and resolve commands (v4 is append-only)
const SchemaVersion = 4

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
	if err := db.createCommitGraphTable(ctx); err != nil {
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
// In v4, path_id is the PK and group_id is a shared FK (N paths can share 1 group).
func (db *DB) createPathsTable(ctx context.Context) error {
	sql := `
	CREATE TABLE IF NOT EXISTS pgit_paths (
		path_id     INTEGER PRIMARY KEY GENERATED ALWAYS AS IDENTITY,
		group_id    INTEGER NOT NULL,
		path        TEXT NOT NULL UNIQUE
	)`

	if err := db.Exec(ctx, sql); err != nil {
		return fmt.Errorf("failed to create pgit_paths: %w", err)
	}

	_ = db.Exec(ctx, "CREATE INDEX IF NOT EXISTS idx_paths_path ON pgit_paths(path)")
	_ = db.Exec(ctx, "CREATE INDEX IF NOT EXISTS idx_paths_group ON pgit_paths(group_id)")

	return nil
}

// createFileRefsTable creates the file references table.
// In v4, PK is (path_id, commit_id). group_id is accessed via pgit_paths.
func (db *DB) createFileRefsTable(ctx context.Context) error {
	sql := `
	CREATE TABLE IF NOT EXISTS pgit_file_refs (
		path_id         INTEGER NOT NULL,
		commit_id       TEXT NOT NULL,
		version_id      INTEGER NOT NULL,
		content_hash    BYTEA,
		mode            INTEGER NOT NULL DEFAULT 33188,
		is_symlink      BOOLEAN NOT NULL DEFAULT FALSE,
		symlink_target  TEXT,
		is_binary       BOOLEAN NOT NULL DEFAULT FALSE,
		PRIMARY KEY (path_id, commit_id)
	)`

	if err := db.Exec(ctx, sql); err != nil {
		return fmt.Errorf("failed to create pgit_file_refs: %w", err)
	}

	_ = db.Exec(ctx, "CREATE INDEX IF NOT EXISTS idx_file_refs_commit ON pgit_file_refs(commit_id)")
	_ = db.Exec(ctx, "CREATE INDEX IF NOT EXISTS idx_file_refs_version ON pgit_file_refs(path_id, version_id)")

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
		compress_depth => 10
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
		compress_depth => 10
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

// createCommitGraphTable creates the commit graph table for O(1) ancestry lookups.
// This is a normal heap table (not xpatch) that stores the commit DAG structure
// with binary lifting ancestor pointers for O(log N) ancestor traversal.
// The xpatch pgit_commits table stores the heavy content (messages, author info);
// this table stores only the lightweight graph structure.
func (db *DB) createCommitGraphTable(ctx context.Context) error {
	sql := `
	CREATE TABLE IF NOT EXISTS pgit_commit_graph (
		seq       SERIAL PRIMARY KEY,
		id        TEXT NOT NULL UNIQUE,
		depth     INTEGER NOT NULL,
		ancestors INTEGER[]
	)`

	if err := db.Exec(ctx, sql); err != nil {
		return fmt.Errorf("failed to create pgit_commit_graph: %w", err)
	}

	return nil
}

// DropCommitGraphIndexes drops the secondary indexes on pgit_commit_graph.
func (db *DB) DropCommitGraphIndexes(ctx context.Context) error {
	// The PK (seq) and UNIQUE (id) are kept — only drop secondary indexes if any.
	// Currently no secondary indexes beyond PK and UNIQUE constraint.
	return nil
}

// CreateCommitGraphIndexes creates the secondary indexes on pgit_commit_graph.
func (db *DB) CreateCommitGraphIndexes(ctx context.Context) error {
	// PK (seq) and UNIQUE (id) are created with the table.
	// No additional secondary indexes needed — queries use PK or id UNIQUE index.
	return nil
}

// DropCommitsIndexes drops the secondary indexes on pgit_commits.
func (db *DB) DropCommitsIndexes(ctx context.Context) error {
	if err := db.Exec(ctx, "DROP INDEX IF EXISTS idx_commits_parent"); err != nil {
		return fmt.Errorf("failed to drop idx_commits_parent: %w", err)
	}
	if err := db.Exec(ctx, "DROP INDEX IF EXISTS idx_commits_authored"); err != nil {
		return fmt.Errorf("failed to drop idx_commits_authored: %w", err)
	}
	return nil
}

// CreateCommitsIndexes creates the secondary indexes on pgit_commits in parallel.
func (db *DB) CreateCommitsIndexes(ctx context.Context) error {
	g, ctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		if err := db.Exec(ctx, "CREATE INDEX IF NOT EXISTS idx_commits_parent ON pgit_commits(parent_id)"); err != nil {
			return fmt.Errorf("failed to create idx_commits_parent: %w", err)
		}
		return nil
	})
	g.Go(func() error {
		if err := db.Exec(ctx, "CREATE INDEX IF NOT EXISTS idx_commits_authored ON pgit_commits(authored_at DESC)"); err != nil {
			return fmt.Errorf("failed to create idx_commits_authored: %w", err)
		}
		return nil
	})
	return g.Wait()
}

// DropPathsIndexes drops the secondary indexes on pgit_paths.
func (db *DB) DropPathsIndexes(ctx context.Context) error {
	if err := db.Exec(ctx, "DROP INDEX IF EXISTS idx_paths_path"); err != nil {
		return fmt.Errorf("failed to drop idx_paths_path: %w", err)
	}
	if err := db.Exec(ctx, "DROP INDEX IF EXISTS idx_paths_group"); err != nil {
		return fmt.Errorf("failed to drop idx_paths_group: %w", err)
	}
	return nil
}

// CreatePathsIndexes creates the secondary indexes on pgit_paths in parallel.
func (db *DB) CreatePathsIndexes(ctx context.Context) error {
	g, ctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		if err := db.Exec(ctx, "CREATE INDEX IF NOT EXISTS idx_paths_path ON pgit_paths(path)"); err != nil {
			return fmt.Errorf("failed to create idx_paths_path: %w", err)
		}
		return nil
	})
	g.Go(func() error {
		if err := db.Exec(ctx, "CREATE INDEX IF NOT EXISTS idx_paths_group ON pgit_paths(group_id)"); err != nil {
			return fmt.Errorf("failed to create idx_paths_group: %w", err)
		}
		return nil
	})
	return g.Wait()
}

// DropFileRefsIndexes drops the secondary indexes on pgit_file_refs.
// The primary key (path_id, commit_id) is kept for COPY conflict detection.
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

// CreateFileRefsIndexes creates the secondary indexes on pgit_file_refs in parallel.
// This is the biggest win from parallelization — with 24M rows at Linux kernel scale,
// each index can take minutes. Building both concurrently halves the total time.
func (db *DB) CreateFileRefsIndexes(ctx context.Context) error {
	g, ctx := errgroup.WithContext(ctx)
	g.Go(func() error {
		if err := db.Exec(ctx, "CREATE INDEX IF NOT EXISTS idx_file_refs_commit ON pgit_file_refs(commit_id)"); err != nil {
			return fmt.Errorf("failed to create idx_file_refs_commit: %w", err)
		}
		return nil
	})
	g.Go(func() error {
		if err := db.Exec(ctx, "CREATE INDEX IF NOT EXISTS idx_file_refs_version ON pgit_file_refs(path_id, version_id)"); err != nil {
			return fmt.Errorf("failed to create idx_file_refs_version: %w", err)
		}
		return nil
	})
	return g.Wait()
}

// DropAllIndexes drops all secondary indexes across all pgit tables.
// Call this before bulk import to maximize insert throughput, then call
// CreateAllIndexes after import to rebuild them efficiently in one pass.
// Primary keys are kept (required for COPY conflict detection and xpatch).
func (db *DB) DropAllIndexes(ctx context.Context) error {
	g, ctx := errgroup.WithContext(ctx)
	g.Go(func() error { return db.DropCommitsIndexes(ctx) })
	g.Go(func() error { return db.DropCommitGraphIndexes(ctx) })
	g.Go(func() error { return db.DropPathsIndexes(ctx) })
	g.Go(func() error { return db.DropFileRefsIndexes(ctx) })
	return g.Wait()
}

// CreateAllIndexes creates all secondary indexes across all pgit tables
// in parallel. Each table's indexes are independent and can be built
// concurrently. Within each table, multiple indexes are also built in
// parallel. This is much faster than sequential creation, especially
// for large tables like pgit_file_refs where each index can take minutes.
func (db *DB) CreateAllIndexes(ctx context.Context) error {
	g, ctx := errgroup.WithContext(ctx)
	g.Go(func() error { return db.CreateCommitsIndexes(ctx) })
	g.Go(func() error { return db.CreateCommitGraphIndexes(ctx) })
	g.Go(func() error { return db.CreatePathsIndexes(ctx) })
	g.Go(func() error { return db.CreateFileRefsIndexes(ctx) })
	return g.Wait()
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
		"pgit_commit_graph",
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
