package db

import (
	"context"
	"sync"
)

// XpatchStats represents compression statistics from pg-xpatch
type XpatchStats struct {
	TotalRows        int64
	TotalGroups      int64
	KeyframeCount    int64
	DeltaCount       int64
	RawSizeBytes     int64
	CompressedBytes  int64
	CompressionRatio float64
	CacheHits        int64
	CacheMisses      int64
	AvgChainLength   float64
}

// RepoStats contains all repository statistics
type RepoStats struct {
	// Commit stats
	TotalCommits  int64
	FirstCommitID *string
	LastCommitID  *string

	// Blob stats
	TotalBlobs       int64
	UniqueFiles      int64
	TotalContentSize int64
	DeletedEntries   int64

	// Size from PostgreSQL (actual disk usage)
	CommitsTableSize int64
	BlobsTableSize   int64
	TotalIndexSize   int64
}

// GetRepoStatsFast returns repository statistics using parallel queries
func (db *DB) GetRepoStatsFast(ctx context.Context) (*RepoStats, error) {
	stats := &RepoStats{}
	var wg sync.WaitGroup
	var mu sync.Mutex
	var firstErr error

	// Helper to set error only once
	setErr := func(err error) {
		mu.Lock()
		if firstErr == nil {
			firstErr = err
		}
		mu.Unlock()
	}

	// Query 1: Commit count (slow on xpatch, ~3s)
	wg.Add(1)
	go func() {
		defer wg.Done()
		err := db.QueryRow(ctx, "SELECT COUNT(*) FROM pgit_commits").Scan(&stats.TotalCommits)
		if err != nil {
			setErr(err)
		}
	}()

	// Query 2: Blob count (slow on xpatch, ~5s)
	wg.Add(1)
	go func() {
		defer wg.Done()
		err := db.QueryRow(ctx, "SELECT COUNT(*) FROM pgit_blobs").Scan(&stats.TotalBlobs)
		if err != nil {
			setErr(err)
		}
	}()

	// Query 3: Unique files count
	wg.Add(1)
	go func() {
		defer wg.Done()
		err := db.QueryRow(ctx, "SELECT COUNT(DISTINCT path) FROM pgit_blobs").Scan(&stats.UniqueFiles)
		if err != nil {
			setErr(err)
		}
	}()

	// Query 4: Total content size
	wg.Add(1)
	go func() {
		defer wg.Done()
		var size *int64
		_ = db.QueryRow(ctx, "SELECT SUM(LENGTH(content)) FROM pgit_blobs WHERE content IS NOT NULL").Scan(&size)
		if size != nil {
			stats.TotalContentSize = *size
		}
	}()

	// Query 5: Table sizes (fast, from pg_class)
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = db.QueryRow(ctx, "SELECT pg_relation_size('pgit_commits')").Scan(&stats.CommitsTableSize)
		_ = db.QueryRow(ctx, "SELECT pg_relation_size('pgit_blobs')").Scan(&stats.BlobsTableSize)
		_ = db.QueryRow(ctx, `
			SELECT COALESCE(SUM(pg_relation_size(indexrelid)), 0)
			FROM pg_index
			WHERE indrelid IN (
				'pgit_commits'::regclass,
				'pgit_blobs'::regclass,
				'pgit_refs'::regclass,
				'pgit_sync_state'::regclass
			)
		`).Scan(&stats.TotalIndexSize)
	}()

	// Query 6: Min/max commit IDs (fast with index)
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = db.QueryRow(ctx, "SELECT MIN(id), MAX(id) FROM pgit_commits").Scan(
			&stats.FirstCommitID, &stats.LastCommitID)
	}()

	wg.Wait()

	if firstErr != nil {
		return nil, firstErr
	}

	return stats, nil
}

// GetXpatchStats retrieves compression statistics for a table
func (db *DB) GetXpatchStats(ctx context.Context, tableName string) (*XpatchStats, error) {
	stats := &XpatchStats{}

	rows, err := db.Query(ctx, `SELECT * FROM xpatch_stats($1)`, tableName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	if rows.Next() {
		err = rows.Scan(
			&stats.TotalRows,
			&stats.TotalGroups,
			&stats.KeyframeCount,
			&stats.DeltaCount,
			&stats.RawSizeBytes,
			&stats.CompressedBytes,
			&stats.CompressionRatio,
			&stats.CacheHits,
			&stats.CacheMisses,
			&stats.AvgChainLength,
		)
		if err != nil {
			return nil, err
		}
	}

	return stats, rows.Err()
}

// Legacy functions for backwards compatibility

// GetCommitStats returns statistics about commits
func (db *DB) GetCommitStats(ctx context.Context) (map[string]interface{}, error) {
	stats := make(map[string]interface{})

	var totalCommits int64
	if err := db.QueryRow(ctx, "SELECT COUNT(*) FROM pgit_commits").Scan(&totalCommits); err != nil {
		return nil, err
	}
	stats["total_commits"] = totalCommits

	return stats, nil
}

// GetBlobStats returns statistics about blobs
func (db *DB) GetBlobStats(ctx context.Context) (map[string]interface{}, error) {
	stats := make(map[string]interface{})

	var totalBlobs int64
	if err := db.QueryRow(ctx, "SELECT COUNT(*) FROM pgit_blobs").Scan(&totalBlobs); err != nil {
		return nil, err
	}
	stats["total_blobs"] = totalBlobs

	var uniquePaths int64
	if err := db.QueryRow(ctx, "SELECT COUNT(DISTINCT path) FROM pgit_blobs").Scan(&uniquePaths); err != nil {
		return nil, err
	}
	stats["unique_paths"] = uniquePaths

	var totalSize *int64
	_ = db.QueryRow(ctx, "SELECT SUM(LENGTH(content)) FROM pgit_blobs WHERE content IS NOT NULL").Scan(&totalSize)
	if totalSize != nil {
		stats["total_content_size"] = *totalSize
	} else {
		stats["total_content_size"] = int64(0)
	}

	return stats, nil
}
