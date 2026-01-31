package db

import (
	"context"
	"fmt"
)

// Schema version for migrations
const SchemaVersion = 1

// InitSchema creates the pgit schema in the database
func (db *DB) InitSchema(ctx context.Context) error {
	// Create extension first
	if err := db.Exec(ctx, "CREATE EXTENSION IF NOT EXISTS pg_xpatch"); err != nil {
		return fmt.Errorf("failed to create pg_xpatch extension: %w", err)
	}

	// Create tables in order (respecting foreign keys)
	if err := db.createCommitsTable(ctx); err != nil {
		return err
	}
	if err := db.createBlobsTable(ctx); err != nil {
		return err
	}
	if err := db.createRefsTable(ctx); err != nil {
		return err
	}
	if err := db.createSyncStateTable(ctx); err != nil {
		return err
	}
	if err := db.createMetadataTable(ctx); err != nil {
		return err
	}

	return nil
}

func (db *DB) createCommitsTable(ctx context.Context) error {
	// Create table
	// NOTE: No self-referential FK because FK constraints don't work properly
	// with xpatch tables. Referential integrity enforced at app level.
	sql := `
	CREATE TABLE IF NOT EXISTS pgit_commits (
		id              TEXT PRIMARY KEY,
		parent_id       TEXT,
		tree_hash       TEXT NOT NULL,
		message         TEXT NOT NULL,
		author_name     TEXT NOT NULL,
		author_email    TEXT NOT NULL,
		created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
	) USING xpatch`

	if err := db.Exec(ctx, sql); err != nil {
		return fmt.Errorf("failed to create pgit_commits: %w", err)
	}

	// Configure xpatch - use created_at for ordering since id is TEXT (ULID)
	// pg-xpatch requires INT or TIMESTAMP for order_by
	configSQL := `
	SELECT xpatch.configure('pgit_commits',
		order_by => 'created_at',
		delta_columns => ARRAY['message', 'author_name', 'author_email'],
		keyframe_every => 100,
		compress_depth => 50
	)`

	// Ignore error if already configured
	_ = db.Exec(ctx, configSQL)

	// Create indexes
	_ = db.Exec(ctx, "CREATE INDEX IF NOT EXISTS idx_commits_parent ON pgit_commits(parent_id)")
	_ = db.Exec(ctx, "CREATE INDEX IF NOT EXISTS idx_commits_created ON pgit_commits(created_at DESC)")

	return nil
}

func (db *DB) createBlobsTable(ctx context.Context) error {
	// Create table with insert_seq for xpatch ordering
	// pg-xpatch requires INT or TIMESTAMP for order_by, so we add a sequence
	// NOTE: No FK to pgit_commits because FK constraints don't work properly
	// between two xpatch tables. Referential integrity enforced at app level.
	sql := `
	CREATE TABLE IF NOT EXISTS pgit_blobs (
		path            TEXT NOT NULL,
		commit_id       TEXT NOT NULL,
		insert_seq      BIGINT GENERATED ALWAYS AS IDENTITY,
		content         BYTEA NOT NULL DEFAULT ''::bytea,
		content_hash    TEXT,
		mode            INTEGER NOT NULL DEFAULT 33188,
		is_symlink      BOOLEAN NOT NULL DEFAULT FALSE,
		symlink_target  TEXT,
		PRIMARY KEY (path, commit_id)
	) USING xpatch`

	if err := db.Exec(ctx, sql); err != nil {
		return fmt.Errorf("failed to create pgit_blobs: %w", err)
	}

	// Configure xpatch - use insert_seq for ordering
	configSQL := `
	SELECT xpatch.configure('pgit_blobs',
		group_by => 'path',
		order_by => 'insert_seq',
		delta_columns => ARRAY['content'],
		keyframe_every => 100,
		compress_depth => 50
	)`

	// Ignore error if already configured
	_ = db.Exec(ctx, configSQL)

	// Create indexes
	_ = db.Exec(ctx, "CREATE INDEX IF NOT EXISTS idx_blobs_commit ON pgit_blobs(commit_id)")
	_ = db.Exec(ctx, "CREATE INDEX IF NOT EXISTS idx_blobs_path ON pgit_blobs(path)")

	return nil
}

func (db *DB) createRefsTable(ctx context.Context) error {
	// NOTE: No FK to pgit_commits because FK constraints don't work properly
	// when referencing xpatch tables. Referential integrity enforced at app level.
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
	// NOTE: No FK to pgit_commits because FK constraints don't work properly
	// when referencing xpatch tables. Referential integrity enforced at app level.
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

// DropSchema drops all pgit tables (use with caution!)
func (db *DB) DropSchema(ctx context.Context) error {
	tables := []string{
		"pgit_metadata",
		"pgit_sync_state",
		"pgit_refs",
		"pgit_blobs",
		"pgit_commits",
	}

	for _, table := range tables {
		if err := db.Exec(ctx, fmt.Sprintf("DROP TABLE IF EXISTS %s CASCADE", table)); err != nil {
			return fmt.Errorf("failed to drop %s: %w", table, err)
		}
	}

	return nil
}
