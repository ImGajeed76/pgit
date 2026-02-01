package db

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// Path represents a file path in the path registry.
// Each unique path gets a unique group_id for efficient storage.
type Path struct {
	GroupID int32
	Path    string
}

// GetOrCreatePath returns the group_id for a path, creating it if needed.
// This is the primary method for getting a group_id during blob creation.
func (db *DB) GetOrCreatePath(ctx context.Context, path string) (int32, error) {
	// Try to get existing path first
	var groupID int32
	err := db.QueryRow(ctx,
		"SELECT group_id FROM pgit_paths WHERE path = $1",
		path,
	).Scan(&groupID)

	if err == nil {
		return groupID, nil
	}

	if err != pgx.ErrNoRows {
		return 0, err
	}

	// Path doesn't exist, create it
	err = db.QueryRow(ctx,
		"INSERT INTO pgit_paths (path) VALUES ($1) RETURNING group_id",
		path,
	).Scan(&groupID)

	if err != nil {
		// Handle race condition - another connection may have inserted
		// Try to get it again
		err2 := db.QueryRow(ctx,
			"SELECT group_id FROM pgit_paths WHERE path = $1",
			path,
		).Scan(&groupID)
		if err2 == nil {
			return groupID, nil
		}
		return 0, err
	}

	return groupID, nil
}

// GetOrCreatePathTx returns the group_id for a path within a transaction.
func (db *DB) GetOrCreatePathTx(ctx context.Context, tx pgx.Tx, path string) (int32, error) {
	var groupID int32
	err := tx.QueryRow(ctx,
		"SELECT group_id FROM pgit_paths WHERE path = $1",
		path,
	).Scan(&groupID)

	if err == nil {
		return groupID, nil
	}

	if err != pgx.ErrNoRows {
		return 0, err
	}

	// Path doesn't exist, create it
	err = tx.QueryRow(ctx,
		"INSERT INTO pgit_paths (path) VALUES ($1) RETURNING group_id",
		path,
	).Scan(&groupID)

	return groupID, err
}

// GetOrCreatePathsBatch handles multiple paths efficiently.
// Returns a map of path -> group_id.
func (db *DB) GetOrCreatePathsBatch(ctx context.Context, paths []string) (map[string]int32, error) {
	if len(paths) == 0 {
		return make(map[string]int32), nil
	}

	result := make(map[string]int32, len(paths))

	// First, try to get all existing paths
	rows, err := db.Query(ctx,
		"SELECT group_id, path FROM pgit_paths WHERE path = ANY($1)",
		paths,
	)
	if err != nil {
		return nil, err
	}

	for rows.Next() {
		var groupID int32
		var path string
		if err := rows.Scan(&groupID, &path); err != nil {
			rows.Close()
			return nil, err
		}
		result[path] = groupID
	}
	rows.Close()

	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Find paths that don't exist yet
	var missingPaths []string
	for _, path := range paths {
		if _, exists := result[path]; !exists {
			missingPaths = append(missingPaths, path)
		}
	}

	if len(missingPaths) == 0 {
		return result, nil
	}

	// Insert missing paths in a transaction
	err = db.WithTx(ctx, func(tx pgx.Tx) error {
		for _, path := range missingPaths {
			var groupID int32

			// Try insert, handle conflict
			err := tx.QueryRow(ctx,
				`INSERT INTO pgit_paths (path) VALUES ($1)
				 ON CONFLICT (path) DO UPDATE SET path = EXCLUDED.path
				 RETURNING group_id`,
				path,
			).Scan(&groupID)

			if err != nil {
				return err
			}

			result[path] = groupID
		}
		return nil
	})

	if err != nil {
		return nil, err
	}

	return result, nil
}

// GetPath retrieves a path by group_id.
func (db *DB) GetPath(ctx context.Context, groupID int32) (string, error) {
	var path string
	err := db.QueryRow(ctx,
		"SELECT path FROM pgit_paths WHERE group_id = $1",
		groupID,
	).Scan(&path)

	if err == pgx.ErrNoRows {
		return "", nil
	}

	return path, err
}

// GetPathsByGroupIDs retrieves multiple paths by group_ids.
// Returns a map of group_id -> path.
func (db *DB) GetPathsByGroupIDs(ctx context.Context, groupIDs []int32) (map[int32]string, error) {
	if len(groupIDs) == 0 {
		return make(map[int32]string), nil
	}

	result := make(map[int32]string, len(groupIDs))

	rows, err := db.Query(ctx,
		"SELECT group_id, path FROM pgit_paths WHERE group_id = ANY($1)",
		groupIDs,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var groupID int32
		var path string
		if err := rows.Scan(&groupID, &path); err != nil {
			return nil, err
		}
		result[groupID] = path
	}

	return result, rows.Err()
}

// GetAllPathsV2 retrieves all unique file paths in the repository.
// This is very fast as it queries the small pgit_paths table.
// Note: This replaces the old GetAllPaths from blobs.go in schema v2.
func (db *DB) GetAllPathsV2(ctx context.Context) ([]string, error) {
	rows, err := db.Query(ctx, "SELECT path FROM pgit_paths ORDER BY path")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var paths []string
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			return nil, err
		}
		paths = append(paths, path)
	}

	return paths, rows.Err()
}

// CountPaths returns the number of unique paths in the repository.
func (db *DB) CountPaths(ctx context.Context) (int64, error) {
	var count int64
	err := db.QueryRow(ctx, "SELECT COUNT(*) FROM pgit_paths").Scan(&count)
	return count, err
}

// GetGroupIDByPath retrieves the group_id for a specific path.
// Returns 0 and nil error if the path doesn't exist.
func (db *DB) GetGroupIDByPath(ctx context.Context, path string) (int32, error) {
	var groupID int32
	err := db.QueryRow(ctx,
		"SELECT group_id FROM pgit_paths WHERE path = $1",
		path,
	).Scan(&groupID)

	if err == pgx.ErrNoRows {
		return 0, nil
	}

	return groupID, err
}
