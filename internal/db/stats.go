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

// RepoStats contains all repository statistics for schema v2.
type RepoStats struct {
	// Commit stats
	TotalCommits  int64
	FirstCommitID *string
	LastCommitID  *string

	// File stats (from new tables)
	TotalBlobs       int64 // Total file refs (from pgit_file_refs)
	UniqueFiles      int64 // Unique paths (from pgit_paths)
	TotalContentSize int64 // Raw content size (from pgit_content via xpatch.stats)
	DeletedEntries   int64

	// Table sizes from PostgreSQL (actual disk usage)
	CommitsTableSize  int64
	PathsTableSize    int64 // New in v2
	FileRefsTableSize int64 // New in v2
	ContentTableSize  int64 // New in v2 (replaces BlobsTableSize)
	TotalIndexSize    int64

	// Legacy field for compatibility (sum of paths + file_refs + content)
	BlobsTableSize int64
}

// GetRepoStatsFast returns repository statistics using xpatch.stats() for O(1) performance.
// This version supports the new schema v2 with separate tables.
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

	// Query 2: Content stats from xpatch.stats() - O(1)
	// In v2, content is in pgit_content table
	wg.Add(1)
	go func() {
		defer wg.Done()
		err := db.QueryRow(ctx, `
			SELECT total_rows, total_groups, raw_size_bytes 
			FROM xpatch.stats('pgit_content')
		`).Scan(&stats.TotalBlobs, &stats.UniqueFiles, &stats.TotalContentSize)
		if err != nil {
			setErr(err)
		}
	}()

	// Query 3: Unique files count from pgit_paths (fast, small table)
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = db.QueryRow(ctx, "SELECT COUNT(*) FROM pgit_paths").Scan(&stats.UniqueFiles)
	}()

	// Query 4: Table sizes (fast, from pg_class)
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = db.QueryRow(ctx, "SELECT pg_relation_size('pgit_commits')").Scan(&stats.CommitsTableSize)
		_ = db.QueryRow(ctx, "SELECT pg_relation_size('pgit_paths')").Scan(&stats.PathsTableSize)
		_ = db.QueryRow(ctx, "SELECT pg_relation_size('pgit_file_refs')").Scan(&stats.FileRefsTableSize)
		_ = db.QueryRow(ctx, "SELECT pg_relation_size('pgit_content')").Scan(&stats.ContentTableSize)

		// Calculate legacy BlobsTableSize as sum
		stats.BlobsTableSize = stats.PathsTableSize + stats.FileRefsTableSize + stats.ContentTableSize

		_ = db.QueryRow(ctx, `
			SELECT COALESCE(SUM(pg_relation_size(indexrelid)), 0)
			FROM pg_index
			WHERE indrelid IN (
				'pgit_commits'::regclass,
				'pgit_paths'::regclass,
				'pgit_file_refs'::regclass,
				'pgit_content'::regclass,
				'pgit_refs'::regclass,
				'pgit_sync_state'::regclass
			)
		`).Scan(&stats.TotalIndexSize)
	}()

	// Query 5: Min/max commit IDs (fast with index)
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = db.QueryRow(ctx, "SELECT MIN(id), MAX(id) FROM pgit_commits").Scan(
			&stats.FirstCommitID, &stats.LastCommitID)
	}()

	// Query 6: File refs count (actual blob versions)
	wg.Add(1)
	go func() {
		defer wg.Done()
		_ = db.QueryRow(ctx, "SELECT COUNT(*) FROM pgit_file_refs").Scan(&stats.TotalBlobs)
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

// GetDetailedTableSizes returns detailed size information for each table.
type DetailedTableSizes struct {
	Commits   int64
	Paths     int64
	FileRefs  int64
	Content   int64
	Refs      int64
	SyncState int64
	Metadata  int64
	Indexes   int64
}

// GetDetailedTableSizes returns sizes for all pgit tables.
func (db *DB) GetDetailedTableSizes(ctx context.Context) (*DetailedTableSizes, error) {
	sizes := &DetailedTableSizes{}

	// Get all table sizes in parallel
	var wg sync.WaitGroup
	wg.Add(7)

	go func() {
		defer wg.Done()
		_ = db.QueryRow(ctx, "SELECT pg_relation_size('pgit_commits')").Scan(&sizes.Commits)
	}()
	go func() {
		defer wg.Done()
		_ = db.QueryRow(ctx, "SELECT pg_relation_size('pgit_paths')").Scan(&sizes.Paths)
	}()
	go func() {
		defer wg.Done()
		_ = db.QueryRow(ctx, "SELECT pg_relation_size('pgit_file_refs')").Scan(&sizes.FileRefs)
	}()
	go func() {
		defer wg.Done()
		_ = db.QueryRow(ctx, "SELECT pg_relation_size('pgit_content')").Scan(&sizes.Content)
	}()
	go func() {
		defer wg.Done()
		_ = db.QueryRow(ctx, "SELECT pg_relation_size('pgit_refs')").Scan(&sizes.Refs)
	}()
	go func() {
		defer wg.Done()
		_ = db.QueryRow(ctx, "SELECT pg_relation_size('pgit_sync_state')").Scan(&sizes.SyncState)
	}()
	go func() {
		defer wg.Done()
		_ = db.QueryRow(ctx, "SELECT pg_relation_size('pgit_metadata')").Scan(&sizes.Metadata)
	}()

	wg.Wait()

	// Get total index size
	_ = db.QueryRow(ctx, `
		SELECT COALESCE(SUM(pg_relation_size(indexrelid)), 0)
		FROM pg_index
		WHERE indrelid IN (
			'pgit_commits'::regclass,
			'pgit_paths'::regclass,
			'pgit_file_refs'::regclass,
			'pgit_content'::regclass,
			'pgit_refs'::regclass,
			'pgit_sync_state'::regclass,
			'pgit_metadata'::regclass
		)
	`).Scan(&sizes.Indexes)

	return sizes, nil
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

// GetBlobStats returns statistics about blobs using the new schema.
func (db *DB) GetBlobStats(ctx context.Context) (map[string]interface{}, error) {
	stats := make(map[string]interface{})

	// Get content stats from xpatch
	var totalContent, uniqueGroups, totalSize int64
	err := db.QueryRow(ctx, `
		SELECT total_rows, total_groups, raw_size_bytes 
		FROM xpatch.stats('pgit_content')
	`).Scan(&totalContent, &uniqueGroups, &totalSize)
	if err != nil {
		return nil, err
	}

	// Get file refs count
	var totalRefs int64
	err = db.QueryRow(ctx, "SELECT COUNT(*) FROM pgit_file_refs").Scan(&totalRefs)
	if err != nil {
		return nil, err
	}

	// Get unique paths count
	var uniquePaths int64
	err = db.QueryRow(ctx, "SELECT COUNT(*) FROM pgit_paths").Scan(&uniquePaths)
	if err != nil {
		return nil, err
	}

	stats["total_blobs"] = totalRefs
	stats["unique_paths"] = uniquePaths
	stats["total_content_size"] = totalSize

	return stats, nil
}
