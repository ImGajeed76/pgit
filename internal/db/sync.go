package db

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

// SyncState represents the sync state with a remote
type SyncState struct {
	RemoteName   string
	LastCommitID *string
	SyncedAt     time.Time
}

// GetSyncState retrieves the sync state for a remote
func (db *DB) GetSyncState(ctx context.Context, remoteName string) (*SyncState, error) {
	sql := `SELECT remote_name, last_commit_id, synced_at FROM pgit_sync_state WHERE remote_name = $1`

	s := &SyncState{}
	err := db.QueryRow(ctx, sql, remoteName).Scan(&s.RemoteName, &s.LastCommitID, &s.SyncedAt)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return s, nil
}

// SetSyncState creates or updates the sync state for a remote
func (db *DB) SetSyncState(ctx context.Context, remoteName string, lastCommitID *string) error {
	sql := `
	INSERT INTO pgit_sync_state (remote_name, last_commit_id, synced_at)
	VALUES ($1, $2, NOW())
	ON CONFLICT (remote_name) DO UPDATE SET 
		last_commit_id = EXCLUDED.last_commit_id,
		synced_at = NOW()`

	return db.Exec(ctx, sql, remoteName, lastCommitID)
}

// DeleteSyncState deletes the sync state for a remote
func (db *DB) DeleteSyncState(ctx context.Context, remoteName string) error {
	return db.Exec(ctx, "DELETE FROM pgit_sync_state WHERE remote_name = $1", remoteName)
}

// GetAllSyncStates retrieves all sync states
func (db *DB) GetAllSyncStates(ctx context.Context) ([]*SyncState, error) {
	sql := `SELECT remote_name, last_commit_id, synced_at FROM pgit_sync_state ORDER BY remote_name`

	rows, err := db.Query(ctx, sql)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var states []*SyncState
	for rows.Next() {
		s := &SyncState{}
		if err := rows.Scan(&s.RemoteName, &s.LastCommitID, &s.SyncedAt); err != nil {
			return nil, err
		}
		states = append(states, s)
	}

	return states, rows.Err()
}
