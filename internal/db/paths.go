package db

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// Path represents a file path in the path registry.
// In v4, path_id is the unique identifier and group_id is shared across renames/copies.
type Path struct {
	PathID  int32  // Unique path identifier (PK)
	GroupID int32  // Content group (shared across renames/copies)
	Path    string // File path
}

// GetOrCreatePath returns (pathID, groupID) for a path, creating it if needed.
// In v4, groupID must be provided for new paths (caller decides which group).
// If the path already exists, the existing (pathID, groupID) is returned and
// the provided groupID is ignored.
func (db *DB) GetOrCreatePath(ctx context.Context, path string, groupID int32) (int32, int32, error) {
	// Try to get existing path first
	var pathID, existingGroupID int32
	err := db.QueryRow(ctx,
		"SELECT path_id, group_id FROM pgit_paths WHERE path = $1",
		path,
	).Scan(&pathID, &existingGroupID)

	if err == nil {
		return pathID, existingGroupID, nil
	}

	if err != pgx.ErrNoRows {
		return 0, 0, err
	}

	// Path doesn't exist, create it with the provided groupID
	err = db.QueryRow(ctx,
		"INSERT INTO pgit_paths (group_id, path) VALUES ($1, $2) RETURNING path_id",
		groupID, path,
	).Scan(&pathID)

	if err != nil {
		// Handle race condition - another connection may have inserted
		// Try to get it again
		err2 := db.QueryRow(ctx,
			"SELECT path_id, group_id FROM pgit_paths WHERE path = $1",
			path,
		).Scan(&pathID, &existingGroupID)
		if err2 == nil {
			return pathID, existingGroupID, nil
		}
		return 0, 0, err
	}

	return pathID, groupID, nil
}

// GetOrCreatePathTx returns (pathID, groupID) for a path within a transaction.
// In v4, groupID must be provided for new paths (caller decides which group).
func (db *DB) GetOrCreatePathTx(ctx context.Context, tx pgx.Tx, path string, groupID int32) (int32, int32, error) {
	var pathID, existingGroupID int32
	err := tx.QueryRow(ctx,
		"SELECT path_id, group_id FROM pgit_paths WHERE path = $1",
		path,
	).Scan(&pathID, &existingGroupID)

	if err == nil {
		return pathID, existingGroupID, nil
	}

	if err != pgx.ErrNoRows {
		return 0, 0, err
	}

	// Path doesn't exist, create it with the provided groupID
	err = tx.QueryRow(ctx,
		"INSERT INTO pgit_paths (group_id, path) VALUES ($1, $2) RETURNING path_id",
		groupID, path,
	).Scan(&pathID)

	return pathID, groupID, err
}

// PathIDs holds the dual identifiers for a registered path.
type PathIDs struct {
	PathID  int32
	GroupID int32
}

// GetOrCreatePathsBatch handles multiple paths efficiently.
// Returns a map of path -> PathIDs.
// In v4, groupID must be provided for new paths via the pathToGroupID map.
// Paths not in pathToGroupID that don't exist yet will get groupID = 0 (caller must provide all).
func (db *DB) GetOrCreatePathsBatch(ctx context.Context, paths []string, pathToGroupID map[string]int32) (map[string]PathIDs, error) {
	if len(paths) == 0 {
		return make(map[string]PathIDs), nil
	}

	result := make(map[string]PathIDs, len(paths))

	// First, try to get all existing paths
	rows, err := db.Query(ctx,
		"SELECT path_id, group_id, path FROM pgit_paths WHERE path = ANY($1)",
		paths,
	)
	if err != nil {
		return nil, err
	}

	for rows.Next() {
		var pathID, groupID int32
		var path string
		if err := rows.Scan(&pathID, &groupID, &path); err != nil {
			rows.Close()
			return nil, err
		}
		result[path] = PathIDs{PathID: pathID, GroupID: groupID}
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
			groupID := pathToGroupID[path]
			var pathID int32

			// Try insert, handle conflict
			err := tx.QueryRow(ctx,
				`INSERT INTO pgit_paths (group_id, path) VALUES ($1, $2)
				 ON CONFLICT (path) DO UPDATE SET path = EXCLUDED.path
				 RETURNING path_id, group_id`,
				groupID, path,
			).Scan(&pathID, &groupID)

			if err != nil {
				return err
			}

			result[path] = PathIDs{PathID: pathID, GroupID: groupID}
		}
		return nil
	})

	if err != nil {
		return nil, err
	}

	return result, nil
}

// GetPathByPathID retrieves a path by path_id.
func (db *DB) GetPathByPathID(ctx context.Context, pathID int32) (string, error) {
	var path string
	err := db.QueryRow(ctx,
		"SELECT path FROM pgit_paths WHERE path_id = $1",
		pathID,
	).Scan(&path)

	if err == pgx.ErrNoRows {
		return "", nil
	}

	return path, err
}

// GetPathsByPathIDs retrieves multiple paths by path_ids.
// Returns a map of path_id -> path.
func (db *DB) GetPathsByPathIDs(ctx context.Context, pathIDs []int32) (map[int32]string, error) {
	if len(pathIDs) == 0 {
		return make(map[int32]string), nil
	}

	result := make(map[int32]string, len(pathIDs))

	rows, err := db.Query(ctx,
		"SELECT path_id, path FROM pgit_paths WHERE path_id = ANY($1)",
		pathIDs,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		var pathID int32
		var path string
		if err := rows.Scan(&pathID, &path); err != nil {
			return nil, err
		}
		result[pathID] = path
	}

	return result, rows.Err()
}

// GetPathsByGroupIDs retrieves multiple paths by group_ids.
// In v4, a single group_id can map to multiple paths (N:1 relationship).
// Returns a map of group_id -> []path.
func (db *DB) GetPathsByGroupIDs(ctx context.Context, groupIDs []int32) (map[int32][]string, error) {
	if len(groupIDs) == 0 {
		return make(map[int32][]string), nil
	}

	result := make(map[int32][]string, len(groupIDs))

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
		result[groupID] = append(result[groupID], path)
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

// GetPathIDAndGroupIDByPath retrieves both path_id and group_id for a specific path.
// Returns (0, 0, nil) if the path doesn't exist.
func (db *DB) GetPathIDAndGroupIDByPath(ctx context.Context, path string) (int32, int32, error) {
	var pathID, groupID int32
	err := db.QueryRow(ctx,
		"SELECT path_id, group_id FROM pgit_paths WHERE path = $1",
		path,
	).Scan(&pathID, &groupID)

	if err == pgx.ErrNoRows {
		return 0, 0, nil
	}

	return pathID, groupID, err
}

// GetGroupIDByPath retrieves the group_id for a specific path.
// Returns 0 and nil error if the path doesn't exist.
// Deprecated: Use GetPathIDAndGroupIDByPath for v4 code that needs both IDs.
func (db *DB) GetGroupIDByPath(ctx context.Context, path string) (int32, error) {
	_, groupID, err := db.GetPathIDAndGroupIDByPath(ctx, path)
	return groupID, err
}

// RegisterPathWithGroup creates a path entry with a specific group_id.
// Used during import when group assignment is already determined.
// Returns the assigned path_id.
func (db *DB) RegisterPathWithGroup(ctx context.Context, path string, groupID int32) (int32, error) {
	var pathID int32
	err := db.QueryRow(ctx,
		`INSERT INTO pgit_paths (group_id, path) VALUES ($1, $2)
		 ON CONFLICT (path) DO UPDATE SET path = EXCLUDED.path
		 RETURNING path_id`,
		groupID, path,
	).Scan(&pathID)

	return pathID, err
}
