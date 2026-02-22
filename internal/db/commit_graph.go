package db

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// CommitGraphEntry represents a row in pgit_commit_graph.
// The Ancestors slice implements binary lifting: Ancestors[k] is the seq
// of the ancestor 2^k steps back. This allows O(log N) ancestor lookups
// instead of O(N) parent-chain walks.
type CommitGraphEntry struct {
	Seq       int32   // monotonic import order (PK)
	ID        string  // ULID
	Depth     int32   // distance from root (root=0)
	Ancestors []int32 // binary lifting: [parent_seq, 2nd_seq, 4th_seq, 8th_seq, ...]
}

// CreateCommitGraphBatch inserts multiple commit graph entries using COPY.
// Entries must have Seq values assigned (typically by SERIAL on insert),
// but since we compute ancestors in Go before insert, we use explicit seq values.
func (db *DB) CreateCommitGraphBatch(ctx context.Context, entries []*CommitGraphEntry) error {
	if len(entries) == 0 {
		return nil
	}

	rows := make([][]interface{}, len(entries))
	for i, e := range entries {
		rows[i] = []interface{}{e.Seq, e.ID, e.Depth, e.Ancestors}
	}

	_, err := db.pool.CopyFrom(
		ctx,
		pgx.Identifier{"pgit_commit_graph"},
		[]string{"seq", "id", "depth", "ancestors"},
		pgx.CopyFromRows(rows),
	)
	return err
}

// GetCommitGraphByID retrieves a commit graph entry by its ULID.
// Uses the UNIQUE index on id — O(1) B-tree lookup on a heap table.
func (db *DB) GetCommitGraphByID(ctx context.Context, id string) (*CommitGraphEntry, error) {
	sql := `SELECT seq, id, depth, ancestors FROM pgit_commit_graph WHERE id = $1`

	e := &CommitGraphEntry{}
	err := db.QueryRow(ctx, sql, id).Scan(&e.Seq, &e.ID, &e.Depth, &e.Ancestors)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return e, nil
}

// GetCommitGraphBySeq retrieves a commit graph entry by its seq number.
// Uses the PRIMARY KEY — O(1) B-tree lookup on a heap table.
func (db *DB) GetCommitGraphBySeq(ctx context.Context, seq int32) (*CommitGraphEntry, error) {
	sql := `SELECT seq, id, depth, ancestors FROM pgit_commit_graph WHERE seq = $1`

	e := &CommitGraphEntry{}
	err := db.QueryRow(ctx, sql, seq).Scan(&e.Seq, &e.ID, &e.Depth, &e.Ancestors)
	if err == pgx.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return e, nil
}

// GetAncestorID resolves the Nth ancestor of a commit using binary lifting.
// Returns the ULID of the ancestor, or an error if the ancestor doesn't exist.
// This performs O(log N) heap table lookups regardless of N.
func (db *DB) GetAncestorID(ctx context.Context, commitID string, n int) (string, error) {
	if n == 0 {
		return commitID, nil
	}

	// Look up the starting commit in the graph
	entry, err := db.GetCommitGraphByID(ctx, commitID)
	if err != nil {
		return "", err
	}
	if entry == nil {
		return "", fmt.Errorf("commit %s not found in graph", commitID)
	}

	if n > int(entry.Depth) {
		return "", fmt.Errorf("cannot go back %d steps from commit at depth %d", n, entry.Depth)
	}

	// Binary lifting: decompose N into powers of 2 and jump
	current := entry
	remaining := n
	for bit := 0; remaining > 0; bit++ {
		if remaining&1 == 1 {
			if bit >= len(current.Ancestors) || current.Ancestors[bit] == 0 {
				return "", fmt.Errorf("ancestor table incomplete at bit %d for seq %d", bit, current.Seq)
			}
			ancestorSeq := current.Ancestors[bit]
			current, err = db.GetCommitGraphBySeq(ctx, ancestorSeq)
			if err != nil {
				return "", err
			}
			if current == nil {
				return "", fmt.Errorf("ancestor seq %d not found in graph", ancestorSeq)
			}
		}
		remaining >>= 1
	}

	return current.ID, nil
}

// CommitExistsInGraph checks if a commit exists using the heap graph table.
// Much faster than checking the xpatch pgit_commits table.
func (db *DB) CommitExistsInGraph(ctx context.Context, id string) (bool, error) {
	var exists bool
	err := db.QueryRow(ctx,
		"SELECT EXISTS(SELECT 1 FROM pgit_commit_graph WHERE id = $1)", id,
	).Scan(&exists)
	return exists, err
}

// FindCommitByPartialIDInGraph finds a commit by partial ID prefix match
// using the graph table's UNIQUE index on id. Returns the full ID.
func (db *DB) FindCommitByPartialIDInGraph(ctx context.Context, partialID string) (string, error) {
	upperBound := partialID[:len(partialID)-1] + string(partialID[len(partialID)-1]+1)
	sql := `SELECT id FROM pgit_commit_graph WHERE id >= $1 AND id < $2 LIMIT 10`

	rows, err := db.Query(ctx, sql, partialID, upperBound)
	if err != nil {
		return "", err
	}
	defer rows.Close()

	var matchIDs []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return "", err
		}
		matchIDs = append(matchIDs, id)
	}
	if err := rows.Err(); err != nil {
		return "", err
	}

	switch len(matchIDs) {
	case 0:
		return "", nil
	case 1:
		return matchIDs[0], nil
	default:
		return "", &AmbiguousCommitError{PartialID: partialID, MatchIDs: matchIDs}
	}
}

// CountCommitsFromGraph returns the commit count using the graph table.
// O(1) on a heap table vs O(N) full-chain decompression on xpatch.
func (db *DB) CountCommitsFromGraph(ctx context.Context) (int, error) {
	var count int
	err := db.QueryRow(ctx, "SELECT COUNT(*) FROM pgit_commit_graph").Scan(&count)
	return count, err
}
