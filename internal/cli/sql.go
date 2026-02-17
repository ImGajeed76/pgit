package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/imgajeed76/pgit/v3/internal/db"
	"github.com/imgajeed76/pgit/v3/internal/repo"
	"github.com/imgajeed76/pgit/v3/internal/ui"
	"github.com/imgajeed76/pgit/v3/internal/ui/styles"
	"github.com/imgajeed76/pgit/v3/internal/ui/table"
	"github.com/spf13/cobra"
)

func newSQLCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sql [query]",
		Short: "Execute SQL queries on the repository database",
		Long: `Execute SQL queries directly on the repository database.

By default, only read-only queries (SELECT) are allowed.
Use --write to enable INSERT, UPDATE, DELETE operations.

Interactive mode shows results in a navigable table.
Use --raw for plain output suitable for piping.

Subcommands:
  pgit sql schema [table]  Show database schema documentation
  pgit sql tables          List all pgit tables
  pgit sql examples        Show example SQL queries

Use with caution - this can corrupt your repository!`,
		Args: cobra.MaximumNArgs(1),
		RunE: runSQL,
	}

	cmd.Flags().Bool("write", false, "Allow write operations (INSERT, UPDATE, DELETE)")
	cmd.Flags().Bool("raw", false, "Output raw values without formatting (for piping)")
	cmd.Flags().Bool("json", false, "Output results as JSON array")
	cmd.Flags().Bool("no-pager", false, "Disable interactive table view")
	cmd.Flags().Int("timeout", 60, "Query timeout in seconds")

	// Add subcommands
	cmd.AddCommand(newSQLSchemaCmd())
	cmd.AddCommand(newSQLTablesCmd())
	cmd.AddCommand(newSQLExamplesCmd())

	return cmd
}

// ═══════════════════════════════════════════════════════════════════════════
// SQL Schema Command
// ═══════════════════════════════════════════════════════════════════════════

// schemaInfo describes a table in the pgit schema
type schemaInfo struct {
	Name        string
	Description string
	Columns     []columnInfo
}

type columnInfo struct {
	Name        string
	Type        string
	Description string
}

var pgitSchema = []schemaInfo{
	{
		Name:        "pgit_commits",
		Description: "Stores commit metadata (author, message, timestamp, parent relationship)",
		Columns: []columnInfo{
			{"id", "TEXT PRIMARY KEY", "ULID commit identifier (time-sortable)"},
			{"parent_id", "TEXT", "Parent commit ID (NULL for root commit)"},
			{"tree_hash", "TEXT", "Hash identifying the file tree state"},
			{"message", "TEXT", "Commit message"},
			{"author_name", "TEXT", "Author's name"},
			{"author_email", "TEXT", "Author's email address"},
			{"authored_at", "TIMESTAMPTZ", "Author timestamp"},
			{"committer_name", "TEXT", "Committer's name"},
			{"committer_email", "TEXT", "Committer's email address"},
			{"committed_at", "TIMESTAMPTZ", "Committer timestamp"},
		},
	},
	{
		Name:        "pgit_paths",
		Description: "File paths with group IDs for delta compression grouping",
		Columns: []columnInfo{
			{"group_id", "INTEGER PRIMARY KEY", "Unique group ID for this path (auto-generated)"},
			{"path", "TEXT UNIQUE", "File path relative to repository root"},
		},
	},
	{
		Name:        "pgit_file_refs",
		Description: "Links commits to file versions (which files exist in which commits)",
		Columns: []columnInfo{
			{"group_id", "INTEGER", "Reference to pgit_paths.group_id (part of PK)"},
			{"commit_id", "TEXT", "Reference to pgit_commits.id (part of PK)"},
			{"version_id", "INTEGER", "Version number within the path group"},
			{"content_hash", "BYTEA", "BLAKE3 hash of file content (NULL = deleted)"},
			{"mode", "INTEGER", "Unix file mode (permissions)"},
			{"is_symlink", "BOOLEAN", "Whether this is a symlink"},
			{"symlink_target", "TEXT", "Symlink target path (if symlink)"},
			{"is_binary", "BOOLEAN", "Whether the file content is binary"},
		},
	},
	{
		Name:        "pgit_text_content",
		Description: "Text file content, delta-compressed by pg-xpatch",
		Columns: []columnInfo{
			{"group_id", "INTEGER", "Reference to pgit_paths.group_id (part of PK)"},
			{"version_id", "INTEGER", "Version number within the group (part of PK)"},
			{"content", "TEXT", "Text file content (auto delta-compressed)"},
		},
	},
	{
		Name:        "pgit_binary_content",
		Description: "Binary file content, delta-compressed by pg-xpatch",
		Columns: []columnInfo{
			{"group_id", "INTEGER", "Reference to pgit_paths.group_id (part of PK)"},
			{"version_id", "INTEGER", "Version number within the group (part of PK)"},
			{"content", "BYTEA", "Binary file content (auto delta-compressed)"},
		},
	},
	{
		Name:        "pgit_refs",
		Description: "Named references (branches, tags) pointing to commits",
		Columns: []columnInfo{
			{"name", "TEXT PRIMARY KEY", "Reference name (e.g., 'HEAD', 'main')"},
			{"commit_id", "TEXT", "Reference to pgit_commits.id"},
		},
	},
	{
		Name:        "pgit_metadata",
		Description: "Repository metadata and configuration",
		Columns: []columnInfo{
			{"key", "TEXT PRIMARY KEY", "Metadata key"},
			{"value", "TEXT", "Metadata value"},
		},
	},
	{
		Name:        "pgit_sync_state",
		Description: "Tracks synchronization state with remote repositories",
		Columns: []columnInfo{
			{"remote_name", "TEXT PRIMARY KEY", "Remote repository name"},
			{"last_commit_id", "TEXT", "Last synchronized commit ID"},
			{"synced_at", "TIMESTAMPTZ", "Last sync timestamp"},
		},
	},
}

var exampleQueries = []struct {
	Title       string
	Description string
	Query       string
}{
	{
		Title:       "Recent commits",
		Description: "Show the 10 most recent commits",
		Query:       "SELECT id, author_name, message, authored_at\nFROM pgit_commits\nORDER BY authored_at DESC\nLIMIT 10;",
	},
	{
		Title:       "Most changed files",
		Description: "Files with the most versions (see also: pgit analyze churn)",
		Query:       "SELECT p.path, COUNT(*) as versions\nFROM pgit_file_refs r\nJOIN pgit_paths p ON p.group_id = r.group_id\nGROUP BY p.path\nORDER BY versions DESC\nLIMIT 10;",
	},
	{
		Title:       "Files changed together",
		Description: "File pairs frequently modified in the same commit (see also: pgit analyze coupling)",
		Query:       "SELECT pa.path, pb.path, COUNT(*) as times_together\nFROM pgit_file_refs a\nJOIN pgit_paths pa ON pa.group_id = a.group_id\nJOIN pgit_file_refs b ON a.commit_id = b.commit_id AND a.group_id < b.group_id\nJOIN pgit_paths pb ON pb.group_id = b.group_id\nGROUP BY pa.path, pb.path\nORDER BY times_together DESC\nLIMIT 10;",
	},
	{
		Title:       "Commits by author",
		Description: "Full table scan on pgit_commits (slow on large repos, use pgit analyze authors instead)",
		Query:       "SELECT author_name, author_email, COUNT(*) as commits\nFROM pgit_commits\nGROUP BY author_name, author_email\nORDER BY commits DESC;",
	},
	{
		Title:       "Commits by day of week",
		Description: "Full table scan on pgit_commits (see also: pgit analyze activity for time-series)",
		Query:       "SELECT TO_CHAR(authored_at, 'Day') as day_of_week, COUNT(*) as commits\nFROM pgit_commits\nGROUP BY TO_CHAR(authored_at, 'Day'), EXTRACT(DOW FROM authored_at)\nORDER BY EXTRACT(DOW FROM authored_at);",
	},
	{
		Title:       "Deleted files",
		Description: "Files that were removed (content_hash is NULL when deleted)",
		Query:       "SELECT DISTINCT p.path\nFROM pgit_file_refs r\nJOIN pgit_paths p ON p.group_id = r.group_id\nWHERE r.content_hash IS NULL\nORDER BY p.path;",
	},
	{
		Title:       "Search commit messages",
		Description: "Find commits by message text (scans full pgit_commits table)",
		Query:       "SELECT id, author_name, message, authored_at\nFROM pgit_commits\nWHERE message ILIKE '%fix%' OR message ILIKE '%bug%'\nORDER BY authored_at DESC\nLIMIT 20;",
	},
	{
		Title:       "Files by extension",
		Description: "Count files grouped by extension",
		Query:       "SELECT\n  COALESCE(NULLIF(SUBSTRING(path FROM '\\.([^.]+)$'), ''), '(no ext)') as extension,\n  COUNT(*) as file_count\nFROM pgit_paths\nGROUP BY extension\nORDER BY file_count DESC\nLIMIT 15;",
	},
}

func newSQLSchemaCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "schema [table]",
		Short: "Show database schema documentation",
		Long: `Display the pgit database schema with descriptions.

Without arguments, shows all tables and their purposes.
With a table name, shows detailed column information.

Examples:
  pgit sql schema              # Show all tables
  pgit sql schema pgit_commits # Show pgit_commits table details`,
		Args: cobra.MaximumNArgs(1),
		RunE: runSQLSchema,
	}
}

func runSQLSchema(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		// Show all tables overview
		fmt.Println(styles.SectionHeader("PGIT DATABASE SCHEMA"))
		fmt.Println()
		fmt.Println("Tables:")
		fmt.Println()

		for _, t := range pgitSchema {
			fmt.Printf("  %s\n", styles.Cyan(t.Name))
			fmt.Printf("    %s\n", styles.Mute(t.Description))
			fmt.Println()
		}

		fmt.Println(styles.Mute("Use 'pgit sql schema <table>' for detailed column information."))
		fmt.Println(styles.Mute("Use 'pgit sql examples' for example queries."))
		return nil
	}

	// Show specific table
	tableName := strings.ToLower(args[0])
	if !strings.HasPrefix(tableName, "pgit_") {
		tableName = "pgit_" + tableName
	}

	for _, t := range pgitSchema {
		if t.Name == tableName {
			fmt.Printf("%s %s\n", styles.SectionHeader("TABLE:"), styles.Cyan(t.Name))
			fmt.Println()
			fmt.Printf("%s\n", t.Description)
			fmt.Println()
			fmt.Println(styles.Boldf("Columns:"))
			fmt.Println()

			// Calculate column widths
			maxNameLen := 0
			maxTypeLen := 0
			for _, col := range t.Columns {
				if len(col.Name) > maxNameLen {
					maxNameLen = len(col.Name)
				}
				if len(col.Type) > maxTypeLen {
					maxTypeLen = len(col.Type)
				}
			}

			for _, col := range t.Columns {
				fmt.Printf("  %-*s  %-*s  %s\n",
					maxNameLen, styles.Cyan(col.Name),
					maxTypeLen, styles.Mute(col.Type),
					col.Description)
			}

			fmt.Println()
			fmt.Println(styles.Boldf("Example query:"))
			fmt.Println()
			fmt.Printf("  %s\n", styles.Mute(fmt.Sprintf("SELECT * FROM %s LIMIT 10;", t.Name)))
			return nil
		}
	}

	return fmt.Errorf("unknown table: %s\n\nAvailable tables: pgit_commits, pgit_paths, pgit_file_refs, pgit_text_content, pgit_binary_content, pgit_refs, pgit_metadata, pgit_sync_state", args[0])
}

func newSQLTablesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "tables",
		Short: "List all pgit tables",
		Long:  `Display a quick list of all pgit database tables.`,
		Args:  cobra.NoArgs,
		RunE:  runSQLTables,
	}
}

func runSQLTables(cmd *cobra.Command, args []string) error {
	fmt.Println(styles.SectionHeader("PGIT TABLES"))
	fmt.Println()
	for _, t := range pgitSchema {
		fmt.Printf("  %s\n", t.Name)
	}
	fmt.Println()
	fmt.Println(styles.Mute("Use 'pgit sql schema <table>' for details."))
	return nil
}

func newSQLExamplesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "examples",
		Short: "Show example SQL queries",
		Long:  `Display useful example SQL queries for analyzing your repository.`,
		Args:  cobra.NoArgs,
		RunE:  runSQLExamples,
	}
}

func runSQLExamples(cmd *cobra.Command, args []string) error {
	fmt.Println(styles.SectionHeader("EXAMPLE SQL QUERIES"))
	fmt.Println()

	for i, example := range exampleQueries {
		if i > 0 {
			fmt.Println()
			fmt.Println(strings.Repeat("─", 60))
			fmt.Println()
		}

		fmt.Printf("%s\n", styles.Boldf("%d. %s", i+1, example.Title))
		fmt.Printf("%s\n", styles.Mute(example.Description))
		fmt.Println()

		// Print query with syntax highlighting-like formatting
		for _, line := range strings.Split(example.Query, "\n") {
			fmt.Printf("  %s\n", styles.Cyan(line))
		}
	}

	fmt.Println()
	fmt.Println(styles.Mute("Copy any query and run it with: pgit sql \"<query>\""))
	return nil
}

// ═══════════════════════════════════════════════════════════════════════════
// SQL Execution
// ═══════════════════════════════════════════════════════════════════════════

func runSQL(cmd *cobra.Command, args []string) error {
	// If no args, show help
	if len(args) == 0 {
		return cmd.Help()
	}

	query := args[0]
	allowWrite, _ := cmd.Flags().GetBool("write")
	raw, _ := cmd.Flags().GetBool("raw")
	jsonOutput, _ := cmd.Flags().GetBool("json")
	noPager, _ := cmd.Flags().GetBool("no-pager")
	timeout, _ := cmd.Flags().GetInt("timeout")

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
		fmt.Println(styles.WarningText("WARNING: Direct SQL writes bypass pgit's safety checks and can"))
		fmt.Println(styles.WarningText("corrupt your repository! Only use if you know what you're doing."))
		fmt.Println()
		fmt.Println("If you're sure, run again with:")
		fmt.Printf("  %s\n", styles.Cyan("pgit sql --write \""+query+"\""))
		return fmt.Errorf("write operation not allowed")
	}

	r, err := repo.Open()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
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

	// Collect all rows
	var allRows [][]string
	for rows.Next() {
		values, err := rows.Values()
		if err != nil {
			return err
		}

		strValues := make([]string, len(values))
		for i, v := range values {
			strValues[i] = formatSQLValue(v)
		}
		allRows = append(allRows, strValues)
	}

	if err := rows.Err(); err != nil {
		return err
	}

	// Display results using the shared table viewer
	return table.DisplayResults("pgit sql", colNames, allRows, table.DisplayOptions{
		JSON:    jsonOutput,
		Raw:     raw,
		NoPager: noPager,
	})
}

// formatSQLValue formats a SQL value for display
func formatSQLValue(v interface{}) string {
	if v == nil {
		return "NULL"
	}

	switch val := v.(type) {
	case []byte:
		// For byte arrays, check if it's printable text
		if len(val) == 0 {
			return ""
		}
		isPrintable := true
		for _, b := range val {
			if b < 32 && b != '\n' && b != '\r' && b != '\t' {
				isPrintable = false
				break
			}
		}
		if isPrintable {
			s := string(val)
			s = strings.ReplaceAll(s, "\n", "\\n")
			s = strings.ReplaceAll(s, "\r", "\\r")
			s = strings.ReplaceAll(s, "\t", "\\t")
			return s
		}
		return fmt.Sprintf("[%d bytes]", len(val))
	case string:
		s := val
		s = strings.ReplaceAll(s, "\n", "\\n")
		s = strings.ReplaceAll(s, "\r", "\\r")
		s = strings.ReplaceAll(s, "\t", "\\t")
		return s
	case time.Time:
		return val.Format("2006-01-02 15:04:05")
	default:
		return fmt.Sprintf("%v", v)
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// Stats Command
// ═══════════════════════════════════════════════════════════════════════════

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

	// Calculate total for all data tables
	contentTableSize := stats.TextContentTableSize + stats.BinaryContentTableSize
	totalDataStorage := stats.CommitsTableSize + stats.PathsTableSize + stats.FileRefsTableSize + contentTableSize
	fmt.Printf("  Commits table:    %s\n", formatBytes(stats.CommitsTableSize))
	fmt.Printf("  Paths table:      %s\n", formatBytes(stats.PathsTableSize))
	fmt.Printf("  File refs table:  %s\n", formatBytes(stats.FileRefsTableSize))
	fmt.Printf("  Text content:     %s\n", formatBytes(stats.TextContentTableSize))
	fmt.Printf("  Binary content:   %s\n", formatBytes(stats.BinaryContentTableSize))
	fmt.Printf("  Indexes:          %s\n", formatBytes(stats.TotalIndexSize))
	fmt.Printf("  ─────────────────────\n")
	fmt.Printf("  Total:            %s\n", styles.SuccessText(formatBytes(totalDataStorage+stats.TotalIndexSize)))

	// Show compression ratio if we have meaningful content size
	if stats.TotalContentSize > 1024 && contentTableSize > 0 {
		ratio := float64(stats.TotalContentSize) / float64(contentTableSize)
		savings := (1 - float64(contentTableSize)/float64(stats.TotalContentSize)) * 100
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

		// Text content xpatch
		fmt.Println()
		fmt.Printf("  %s\n", styles.Mute("pgit_text_content:"))
		textXpatch, err := r.DB.GetXpatchStats(ctx, "pgit_text_content")
		if err != nil {
			fmt.Printf("    Unable to get stats: %v\n", styles.Mute(err.Error()))
		} else {
			printXpatchStats(textXpatch)
		}

		// Binary content xpatch
		fmt.Println()
		fmt.Printf("  %s\n", styles.Mute("pgit_binary_content:"))
		binaryXpatch, err := r.DB.GetXpatchStats(ctx, "pgit_binary_content")
		if err != nil {
			fmt.Printf("    Unable to get stats: %v\n", styles.Mute(err.Error()))
		} else {
			printXpatchStats(binaryXpatch)
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
	CommitsTableBytes       int64   `json:"commits_table_bytes"`
	PathsTableBytes         int64   `json:"paths_table_bytes"`
	FileRefsTableBytes      int64   `json:"file_refs_table_bytes"`
	TextContentTableBytes   int64   `json:"text_content_table_bytes"`
	BinaryContentTableBytes int64   `json:"binary_content_table_bytes"`
	IndexesBytes            int64   `json:"indexes_bytes"`
	TotalBytes              int64   `json:"total_bytes"`
	CompressionRatio        float64 `json:"compression_ratio,omitempty"`
	SpaceSavedPercent       float64 `json:"space_saved_percent,omitempty"`
}

type JSONXpatchStats struct {
	Commits       *JSONXpatchTableStats `json:"commits,omitempty"`
	TextContent   *JSONXpatchTableStats `json:"text_content,omitempty"`
	BinaryContent *JSONXpatchTableStats `json:"binary_content,omitempty"`
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
	contentTableSize := stats.TextContentTableSize + stats.BinaryContentTableSize
	totalStorage := stats.CommitsTableSize + stats.PathsTableSize + stats.FileRefsTableSize + contentTableSize + stats.TotalIndexSize

	jsonStats := JSONStats{
		Repository: JSONRepoStats{
			Commits:          stats.TotalCommits,
			FilesTracked:     stats.UniqueFiles,
			BlobVersions:     stats.TotalBlobs,
			ContentSizeBytes: stats.TotalContentSize,
		},
		Storage: JSONStorageStats{
			CommitsTableBytes:       stats.CommitsTableSize,
			PathsTableBytes:         stats.PathsTableSize,
			FileRefsTableBytes:      stats.FileRefsTableSize,
			TextContentTableBytes:   stats.TextContentTableSize,
			BinaryContentTableBytes: stats.BinaryContentTableSize,
			IndexesBytes:            stats.TotalIndexSize,
			TotalBytes:              totalStorage,
		},
	}

	if stats.TotalContentSize > 1024 && contentTableSize > 0 {
		ratio := float64(stats.TotalContentSize) / float64(contentTableSize)
		savings := (1 - float64(contentTableSize)/float64(stats.TotalContentSize)) * 100
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

		textXpatch, err := r.DB.GetXpatchStats(ctx, "pgit_text_content")
		if err == nil && textXpatch != nil {
			jsonStats.Xpatch.TextContent = xpatchToJSON(textXpatch)
		}

		binaryXpatch, err := r.DB.GetXpatchStats(ctx, "pgit_binary_content")
		if err == nil && binaryXpatch != nil {
			jsonStats.Xpatch.BinaryContent = xpatchToJSON(binaryXpatch)
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
