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

// GetRepoStatsFast returns repository statistics using xpatch.stats() for O(1) performance
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

	// Query 1: Commit stats from xpatch.stats() - O(1)
	wg.Add(1)
	go func() {
		defer wg.Done()
		err := db.QueryRow(ctx, "SELECT total_rows FROM xpatch.stats('pgit_commits')").Scan(&stats.TotalCommits)
		if err != nil {
			setErr(err)
		}
	}()

	// Query 2: Blob stats from xpatch.stats() - O(1)
	// total_rows = blob count, total_groups = unique files, raw_size_bytes = content size
	wg.Add(1)
	go func() {
		defer wg.Done()
		err := db.QueryRow(ctx, `
			SELECT total_rows, total_groups, raw_size_bytes 
			FROM xpatch.stats('pgit_blobs')
		`).Scan(&stats.TotalBlobs, &stats.UniqueFiles, &stats.TotalContentSize)
		if err != nil {
			setErr(err)
		}
	}()

	// Query 3: Table sizes (fast, from pg_class)
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

	// Query 4: Min/max commit IDs (fast with index)
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

	sql := `
	SELECT 
		total_rows,
		total_groups,
		keyframe_count,
		delta_count,
		raw_size_bytes,
		compressed_size_bytes,
		compression_ratio::float8,
		cache_hits,
		cache_misses,
		avg_compression_depth::float8
	FROM xpatch.stats($1)`

	err := db.QueryRow(ctx, sql, tableName).Scan(
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

	return stats, nil
}

// Legacy functions for backwards compatibility

// GetCommitStats returns statistics about commits using xpatch.stats() for O(1) performance
func (db *DB) GetCommitStats(ctx context.Context) (map[string]interface{}, error) {
	stats := make(map[string]interface{})

	var totalCommits int64
	if err := db.QueryRow(ctx, "SELECT total_rows FROM xpatch.stats('pgit_commits')").Scan(&totalCommits); err != nil {
		return nil, err
	}
	stats["total_commits"] = totalCommits

	return stats, nil
}

// GetBlobStats returns statistics about blobs using xpatch.stats() for O(1) performance
func (db *DB) GetBlobStats(ctx context.Context) (map[string]interface{}, error) {
	stats := make(map[string]interface{})

	var totalBlobs, uniquePaths, totalSize int64
	err := db.QueryRow(ctx, `
		SELECT total_rows, total_groups, raw_size_bytes 
		FROM xpatch.stats('pgit_blobs')
	`).Scan(&totalBlobs, &uniquePaths, &totalSize)
	if err != nil {
		return nil, err
	}

	stats["total_blobs"] = totalBlobs
	stats["unique_paths"] = uniquePaths
	stats["total_content_size"] = totalSize

	return stats, nil
}
