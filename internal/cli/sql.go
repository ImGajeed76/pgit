package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/imgajeed76/pgit/internal/db"
	"github.com/imgajeed76/pgit/internal/repo"
	"github.com/imgajeed76/pgit/internal/ui"
	"github.com/imgajeed76/pgit/internal/ui/styles"
	"github.com/spf13/cobra"
)

func newSQLCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sql <query>",
		Short: "Execute SQL queries on the repository database",
		Long: `Execute SQL queries directly on the repository database.

By default, only read-only queries (SELECT) are allowed.
Use --write to enable INSERT, UPDATE, DELETE operations.

Use with caution - this can corrupt your repository!`,
		Args: cobra.ExactArgs(1),
		RunE: runSQL,
	}

	cmd.Flags().Bool("write", false, "Allow write operations (INSERT, UPDATE, DELETE)")
	cmd.Flags().Bool("raw", false, "Output raw values without formatting")

	return cmd
}

func runSQL(cmd *cobra.Command, args []string) error {
	query := args[0]
	allowWrite, _ := cmd.Flags().GetBool("write")
	raw, _ := cmd.Flags().GetBool("raw")

	// Check if query is a write operation
	upperQuery := strings.ToUpper(strings.TrimSpace(query))
	isWrite := strings.HasPrefix(upperQuery, "INSERT") ||
		strings.HasPrefix(upperQuery, "UPDATE") ||
		strings.HasPrefix(upperQuery, "DELETE") ||
		strings.HasPrefix(upperQuery, "DROP") ||
		strings.HasPrefix(upperQuery, "CREATE") ||
		strings.HasPrefix(upperQuery, "ALTER") ||
		strings.HasPrefix(upperQuery, "TRUNCATE")

	if isWrite && !allowWrite {
		fmt.Println(styles.Errorf("Error: Write operations require --write flag"))
		fmt.Println()
		fmt.Println("This is a safety measure to prevent accidental data modification.")
		fmt.Println("If you're sure, run again with: pgit sql --write \"" + query + "\"")
		return fmt.Errorf("write operation not allowed")
	}

	r, err := repo.Open()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// Connect to database
	if err := r.Connect(ctx); err != nil {
		return err
	}
	defer r.Close()

	// Execute query
	if isWrite {
		// Use Exec for write operations
		if err := r.DB.Exec(ctx, query); err != nil {
			return err
		}
		fmt.Println("Query executed successfully")
		return nil
	}

	// Use Query for read operations
	rows, err := r.DB.Query(ctx, query)
	if err != nil {
		return err
	}
	defer rows.Close()

	// Get column descriptions
	fieldDescs := rows.FieldDescriptions()
	colNames := make([]string, len(fieldDescs))
	for i, fd := range fieldDescs {
		colNames[i] = string(fd.Name)
	}

	if !raw {
		// Print header
		fmt.Println(strings.Join(colNames, "\t"))
		fmt.Println(strings.Repeat("-", len(strings.Join(colNames, "\t"))+8))
	}

	// Print rows
	rowCount := 0
	for rows.Next() {
		values, err := rows.Values()
		if err != nil {
			return err
		}

		strValues := make([]string, len(values))
		for i, v := range values {
			if v == nil {
				strValues[i] = "NULL"
			} else {
				strValues[i] = fmt.Sprintf("%v", v)
			}
		}

		fmt.Println(strings.Join(strValues, "\t"))
		rowCount++
	}

	if err := rows.Err(); err != nil {
		return err
	}

	if !raw {
		fmt.Println()
		fmt.Printf("(%d rows)\n", rowCount)
	}

	return nil
}

func newStatsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "stats",
		Short: "Show repository statistics and compression info",
		Long: `Display statistics about the repository including:
  - Number of commits and files
  - Storage size and compression ratio
  - pg-xpatch delta compression statistics`,
		RunE: runStats,
	}

	cmd.Flags().Bool("xpatch", false, "Include detailed pg-xpatch compression stats (slower)")
	cmd.Flags().Bool("json", false, "Output in JSON format")

	return cmd
}

func runStats(cmd *cobra.Command, args []string) error {
	showXpatch, _ := cmd.Flags().GetBool("xpatch")
	jsonOutput, _ := cmd.Flags().GetBool("json")

	r, err := repo.Open()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Connect to database
	if err := r.Connect(ctx); err != nil {
		return err
	}
	defer r.Close()

	// Show spinner while gathering stats (can take a few seconds on large repos)
	var spinner *ui.Spinner
	if !jsonOutput {
		spinner = ui.NewSpinner("Gathering repository statistics")
		spinner.Start()
	}

	stats, err := r.DB.GetRepoStatsFast(ctx)
	if spinner != nil {
		spinner.Stop()
	}
	if err != nil {
		return err
	}

	if jsonOutput {
		return printJSONStats(ctx, r, stats, showXpatch)
	}

	// Display repository overview
	fmt.Println(styles.Boldf("Repository Statistics"))
	fmt.Println()

	fmt.Printf("  Commits:        %s\n", styles.Cyanf("%d", stats.TotalCommits))
	fmt.Printf("  Files tracked:  %s\n", styles.Cyanf("%d", stats.UniqueFiles))
	fmt.Printf("  Blob versions:  %s\n", styles.Cyanf("%d", stats.TotalBlobs))
	if stats.TotalContentSize > 0 {
		fmt.Printf("  Content size:   %s %s\n",
			formatBytes(stats.TotalContentSize),
			styles.Mute("(uncompressed)"))
	}

	// Storage section
	fmt.Println()
	fmt.Println(styles.Boldf("Storage (on disk)"))
	fmt.Println()

	totalStorage := stats.CommitsTableSize + stats.BlobsTableSize
	fmt.Printf("  Commits table:  %s\n", formatBytes(stats.CommitsTableSize))
	fmt.Printf("  Blobs table:    %s\n", formatBytes(stats.BlobsTableSize))
	fmt.Printf("  Indexes:        %s\n", formatBytes(stats.TotalIndexSize))
	fmt.Printf("  ─────────────────────\n")
	fmt.Printf("  Total:          %s\n", styles.SuccessText(formatBytes(totalStorage+stats.TotalIndexSize)))

	// Show compression ratio if we have meaningful content size
	if stats.TotalContentSize > 1024 && stats.BlobsTableSize > 0 {
		ratio := float64(stats.TotalContentSize) / float64(stats.BlobsTableSize)
		savings := (1 - float64(stats.BlobsTableSize)/float64(stats.TotalContentSize)) * 100
		if savings > 0 {
			fmt.Printf("\n  %s %.1fx compression (%.0f%% space saved)\n",
				styles.Successf("→"), ratio, savings)
		}
	}

	// xpatch stats (optional, can be slow)
	if showXpatch {
		fmt.Println()
		fmt.Println(styles.Boldf("pg-xpatch Compression"))

		// Commits xpatch
		fmt.Println()
		fmt.Printf("  %s\n", styles.Mute("pgit_commits:"))
		commitXpatch, err := r.DB.GetXpatchStats(ctx, "pgit_commits")
		if err != nil {
			fmt.Printf("    Unable to get stats: %v\n", styles.Mute(err.Error()))
		} else {
			printXpatchStats(commitXpatch)
		}

		// Blobs xpatch
		fmt.Println()
		fmt.Printf("  %s\n", styles.Mute("pgit_blobs:"))
		blobXpatch, err := r.DB.GetXpatchStats(ctx, "pgit_blobs")
		if err != nil {
			fmt.Printf("    Unable to get stats: %v\n", styles.Mute(err.Error()))
		} else {
			printXpatchStats(blobXpatch)
		}
	} else {
		fmt.Println()
		fmt.Printf("%s Use --xpatch for detailed compression stats\n", styles.Mute("hint:"))
	}

	return nil
}

func printXpatchStats(stats *db.XpatchStats) {
	if stats == nil {
		fmt.Printf("    No stats available\n")
		return
	}

	fmt.Printf("    Rows:         %d\n", stats.TotalRows)
	fmt.Printf("    Groups:       %d\n", stats.TotalGroups)
	fmt.Printf("    Keyframes:    %d\n", stats.KeyframeCount)
	fmt.Printf("    Deltas:       %d\n", stats.DeltaCount)

	if stats.RawSizeBytes > 0 {
		fmt.Printf("    Raw size:     %s\n", formatBytes(stats.RawSizeBytes))
		fmt.Printf("    Compressed:   %s\n", formatBytes(stats.CompressedBytes))

		// Calculate savings
		savings := float64(stats.RawSizeBytes-stats.CompressedBytes) / float64(stats.RawSizeBytes) * 100
		if savings > 0 {
			fmt.Printf("    Ratio:        %.1fx %s\n",
				stats.CompressionRatio,
				styles.Successf("(%.0f%% saved)", savings))
		} else {
			fmt.Printf("    Ratio:        %.2fx\n", stats.CompressionRatio)
		}
	}

	if stats.AvgChainLength > 0 {
		fmt.Printf("    Avg chain:    %.1f\n", stats.AvgChainLength)
	}

	cacheTotal := stats.CacheHits + stats.CacheMisses
	if cacheTotal > 0 {
		hitRate := float64(stats.CacheHits) / float64(cacheTotal) * 100
		fmt.Printf("    Cache hit:    %.1f%%\n", hitRate)
	}
}

// JSONStats represents stats output in JSON format
type JSONStats struct {
	Repository JSONRepoStats    `json:"repository"`
	Storage    JSONStorageStats `json:"storage"`
	Xpatch     *JSONXpatchStats `json:"xpatch,omitempty"`
}

type JSONRepoStats struct {
	Commits          int64 `json:"commits"`
	FilesTracked     int64 `json:"files_tracked"`
	BlobVersions     int64 `json:"blob_versions"`
	ContentSizeBytes int64 `json:"content_size_bytes"`
}

type JSONStorageStats struct {
	CommitsTableBytes int64   `json:"commits_table_bytes"`
	BlobsTableBytes   int64   `json:"blobs_table_bytes"`
	IndexesBytes      int64   `json:"indexes_bytes"`
	TotalBytes        int64   `json:"total_bytes"`
	CompressionRatio  float64 `json:"compression_ratio,omitempty"`
	SpaceSavedPercent float64 `json:"space_saved_percent,omitempty"`
}

type JSONXpatchStats struct {
	Commits *JSONXpatchTableStats `json:"commits,omitempty"`
	Blobs   *JSONXpatchTableStats `json:"blobs,omitempty"`
}

type JSONXpatchTableStats struct {
	TotalRows        int64   `json:"total_rows"`
	TotalGroups      int64   `json:"total_groups"`
	KeyframeCount    int64   `json:"keyframe_count"`
	DeltaCount       int64   `json:"delta_count"`
	RawSizeBytes     int64   `json:"raw_size_bytes"`
	CompressedBytes  int64   `json:"compressed_bytes"`
	CompressionRatio float64 `json:"compression_ratio"`
	AvgChainLength   float64 `json:"avg_chain_length"`
	CacheHitPercent  float64 `json:"cache_hit_percent"`
}

func printJSONStats(ctx context.Context, r *repo.Repository, stats *db.RepoStats, showXpatch bool) error {
	totalStorage := stats.CommitsTableSize + stats.BlobsTableSize + stats.TotalIndexSize

	jsonStats := JSONStats{
		Repository: JSONRepoStats{
			Commits:          stats.TotalCommits,
			FilesTracked:     stats.UniqueFiles,
			BlobVersions:     stats.TotalBlobs,
			ContentSizeBytes: stats.TotalContentSize,
		},
		Storage: JSONStorageStats{
			CommitsTableBytes: stats.CommitsTableSize,
			BlobsTableBytes:   stats.BlobsTableSize,
			IndexesBytes:      stats.TotalIndexSize,
			TotalBytes:        totalStorage,
		},
	}

	if stats.TotalContentSize > 1024 && stats.BlobsTableSize > 0 {
		ratio := float64(stats.TotalContentSize) / float64(stats.BlobsTableSize)
		savings := (1 - float64(stats.BlobsTableSize)/float64(stats.TotalContentSize)) * 100
		if savings > 0 {
			jsonStats.Storage.CompressionRatio = ratio
			jsonStats.Storage.SpaceSavedPercent = savings
		}
	}

	if showXpatch {
		jsonStats.Xpatch = &JSONXpatchStats{}

		commitXpatch, err := r.DB.GetXpatchStats(ctx, "pgit_commits")
		if err == nil && commitXpatch != nil {
			jsonStats.Xpatch.Commits = xpatchToJSON(commitXpatch)
		}

		blobXpatch, err := r.DB.GetXpatchStats(ctx, "pgit_blobs")
		if err == nil && blobXpatch != nil {
			jsonStats.Xpatch.Blobs = xpatchToJSON(blobXpatch)
		}
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(jsonStats)
}

func xpatchToJSON(stats *db.XpatchStats) *JSONXpatchTableStats {
	result := &JSONXpatchTableStats{
		TotalRows:        stats.TotalRows,
		TotalGroups:      stats.TotalGroups,
		KeyframeCount:    stats.KeyframeCount,
		DeltaCount:       stats.DeltaCount,
		RawSizeBytes:     stats.RawSizeBytes,
		CompressedBytes:  stats.CompressedBytes,
		CompressionRatio: stats.CompressionRatio,
		AvgChainLength:   stats.AvgChainLength,
	}

	cacheTotal := stats.CacheHits + stats.CacheMisses
	if cacheTotal > 0 {
		result.CacheHitPercent = float64(stats.CacheHits) / float64(cacheTotal) * 100
	}

	return result
}

func formatBytes(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}
	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(bytes)/float64(div), "KMGTPE"[exp])
}
