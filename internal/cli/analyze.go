package cli

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/imgajeed76/pgit/v3/internal/repo"
	"github.com/imgajeed76/pgit/v3/internal/ui"
	"github.com/imgajeed76/pgit/v3/internal/ui/table"
	"github.com/spf13/cobra"
)

// ═══════════════════════════════════════════════════════════════════════════
// Analyze Command
// ═══════════════════════════════════════════════════════════════════════════

func newAnalyzeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "analyze",
		Short: "Run pre-built analyses on your repository",
		Long: `Analyze your repository with optimized, pre-built queries.

These commands wrap complex SQL patterns into simple one-liners.
All queries are optimized for pg-xpatch storage — you don't need to
think about delta chains, heap tables, or query access patterns.

Results are displayed in the interactive table viewer (same as pgit sql).

Available analyses:
  churn       Most frequently modified files
  coupling    Files that are always changed together
  hotspots    Churn aggregated by directory
  authors     Commits per author
  activity    Commit activity over time
  bus-factor  Files with fewest distinct authors (knowledge silos)`,
	}

	cmd.AddCommand(
		newAnalyzeChurnCmd(),
		newAnalyzeCouplingCmd(),
		newAnalyzeHotspotsCmd(),
		newAnalyzeAuthorsCmd(),
		newAnalyzeActivityCmd(),
		newAnalyzeBusFactorCmd(),
	)

	return cmd
}

// ═══════════════════════════════════════════════════════════════════════════
// Shared Helpers
// ═══════════════════════════════════════════════════════════════════════════

// analyzeFlags holds the common flags for all analyze subcommands.
type analyzeFlags struct {
	limit    int
	pathGlob string
	json     bool
	raw      bool
	noPager  bool
}

// addAnalyzeFlags adds the shared flags to a cobra.Command.
func addAnalyzeFlags(cmd *cobra.Command) {
	cmd.Flags().IntP("limit", "n", 25, "Maximum number of rows to display")
	cmd.Flags().StringP("path", "p", "", "Glob pattern to filter file paths (e.g. 'src/**/*.go')")
	cmd.Flags().Bool("json", false, "Output as JSON")
	cmd.Flags().Bool("raw", false, "Output as tab-separated values (for piping)")
	cmd.Flags().Bool("no-pager", false, "Plain table output, no interactive viewer")
}

// parseAnalyzeFlags reads the shared flags from a cobra.Command.
func parseAnalyzeFlags(cmd *cobra.Command) analyzeFlags {
	limit, _ := cmd.Flags().GetInt("limit")
	pathGlob, _ := cmd.Flags().GetString("path")
	jsonOutput, _ := cmd.Flags().GetBool("json")
	raw, _ := cmd.Flags().GetBool("raw")
	noPager, _ := cmd.Flags().GetBool("no-pager")
	return analyzeFlags{
		limit:    limit,
		pathGlob: pathGlob,
		json:     jsonOutput,
		raw:      raw,
		noPager:  noPager,
	}
}

// displayOpts converts analyzeFlags to table.DisplayOptions.
func (f analyzeFlags) displayOpts() table.DisplayOptions {
	return table.DisplayOptions{
		JSON:    f.json,
		Raw:     f.raw,
		NoPager: f.noPager,
	}
}

// matchPath checks if a file path matches the glob pattern.
// Supports ** for recursive matching by splitting on path segments.
func matchPath(pattern, path string) bool {
	if pattern == "" {
		return true
	}
	// Handle ** patterns by checking if any suffix of the path matches
	if strings.Contains(pattern, "**") {
		// Try matching the full path first
		if matched, _ := filepath.Match(pattern, path); matched {
			return true
		}
		// For ** patterns, try matching each suffix of the path
		// e.g., pattern "src/**/*.go" against "src/foo/bar/baz.go"
		parts := strings.Split(path, "/")
		for i := range parts {
			suffix := strings.Join(parts[i:], "/")
			// Replace ** with * for this segment match
			simplePattern := strings.ReplaceAll(pattern, "/**/", "/*/")
			simplePattern = strings.ReplaceAll(simplePattern, "/**", "/*")
			if matched, _ := filepath.Match(simplePattern, suffix); matched {
				return true
			}
		}
		// Also try: if pattern starts with **, match any path with the suffix
		if strings.HasPrefix(pattern, "**") {
			suffixPattern := strings.TrimPrefix(pattern, "**/")
			suffixPattern = strings.TrimPrefix(suffixPattern, "**")
			for i := range parts {
				suffix := strings.Join(parts[i:], "/")
				if matched, _ := filepath.Match(suffixPattern, suffix); matched {
					return true
				}
			}
		}
		return false
	}
	matched, _ := filepath.Match(pattern, path)
	return matched
}

// connectRepo opens and connects to the repository database.
func connectRepo(ctx context.Context) (*repo.Repository, error) {
	r, err := repo.Open()
	if err != nil {
		return nil, err
	}
	if err := r.Connect(ctx); err != nil {
		return nil, err
	}
	return r, nil
}

// ═══════════════════════════════════════════════════════════════════════════
// churn — Most frequently modified files
// ═══════════════════════════════════════════════════════════════════════════

func newAnalyzeChurnCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "churn",
		Short: "Most frequently modified files",
		Long: `Rank files by how many commits touched them.

Files that change often are maintenance hotspots — they tend to contain
bugs, have complex logic, or suffer from unclear responsibilities.

This query runs entirely on heap tables (no xpatch decompression).`,
		RunE: runAnalyzeChurn,
	}
	addAnalyzeFlags(cmd)
	return cmd
}

func runAnalyzeChurn(cmd *cobra.Command, args []string) error {
	flags := parseAnalyzeFlags(cmd)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	r, err := connectRepo(ctx)
	if err != nil {
		return err
	}
	defer r.Close()

	spinner := ui.NewSpinner("Analyzing file churn")
	spinner.Start()

	// Query: count file refs per path (heap tables only, no xpatch)
	rows, err := r.DB.Query(ctx, `
		SELECT p.path, COUNT(*) as versions
		FROM pgit_file_refs r
		JOIN pgit_paths p ON p.group_id = r.group_id
		GROUP BY p.path
		ORDER BY versions DESC
	`)
	if err != nil {
		spinner.Stop()
		return err
	}

	type churnEntry struct {
		path     string
		versions int64
	}

	var results []churnEntry
	for rows.Next() {
		var e churnEntry
		if err := rows.Scan(&e.path, &e.versions); err != nil {
			rows.Close()
			spinner.Stop()
			return err
		}
		if matchPath(flags.pathGlob, e.path) {
			results = append(results, e)
		}
	}
	rows.Close()

	spinner.Stop()

	// Apply limit
	if flags.limit > 0 && len(results) > flags.limit {
		results = results[:flags.limit]
	}

	// Build table data
	columns := []string{"path", "versions"}
	tableRows := make([][]string, len(results))
	for i, e := range results {
		tableRows[i] = []string{e.path, strconv.FormatInt(e.versions, 10)}
	}

	title := fmt.Sprintf("churn: top %d files", len(results))
	return table.DisplayResults(title, columns, tableRows, flags.displayOpts())
}

// ═══════════════════════════════════════════════════════════════════════════
// coupling — Files changed together
// ═══════════════════════════════════════════════════════════════════════════

func newAnalyzeCouplingCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "coupling",
		Short: "Files that are always changed together",
		Long: `Find file pairs that are most frequently modified in the same commit.

High coupling between unrelated files reveals hidden architectural
dependencies. If changing one file always requires changing another,
they may need to be merged, or an abstraction is missing.

Commits touching more than --max-files are skipped (bulk reformats
produce noise, not signal). The computation is done in Go to avoid
expensive SQL self-joins.`,
		RunE: runAnalyzeCoupling,
	}
	addAnalyzeFlags(cmd)
	cmd.Flags().Int("min", 2, "Minimum co-change count to include a pair")
	cmd.Flags().Int("max-files", 100, "Skip commits touching more than this many files")
	return cmd
}

func runAnalyzeCoupling(cmd *cobra.Command, args []string) error {
	flags := parseAnalyzeFlags(cmd)
	minCount, _ := cmd.Flags().GetInt("min")
	maxFiles, _ := cmd.Flags().GetInt("max-files")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	r, err := connectRepo(ctx)
	if err != nil {
		return err
	}
	defer r.Close()

	spinner := ui.NewSpinner("Analyzing file coupling")
	spinner.Start()

	// Step 1: Fetch all (commit_id, group_id) pairs from heap table
	rows, err := r.DB.Query(ctx, `
		SELECT commit_id, group_id
		FROM pgit_file_refs
		ORDER BY commit_id
	`)
	if err != nil {
		spinner.Stop()
		return err
	}

	// Build commit -> []group_id map
	commitFiles := make(map[string][]int32)
	for rows.Next() {
		var commitID string
		var groupID int32
		if err := rows.Scan(&commitID, &groupID); err != nil {
			rows.Close()
			spinner.Stop()
			return err
		}
		commitFiles[commitID] = append(commitFiles[commitID], groupID)
	}
	rows.Close()

	// Step 2: Fetch group_id -> path mapping
	pathRows, err := r.DB.Query(ctx, `SELECT group_id, path FROM pgit_paths`)
	if err != nil {
		spinner.Stop()
		return err
	}

	pathMap := make(map[int32]string)
	for pathRows.Next() {
		var groupID int32
		var path string
		if err := pathRows.Scan(&groupID, &path); err != nil {
			pathRows.Close()
			spinner.Stop()
			return err
		}
		pathMap[groupID] = path
	}
	pathRows.Close()

	// Step 3: Count pairs in Go (avoids O(n^2) SQL self-join)
	type pair struct {
		a, b int32 // a < b always
	}
	pairCounts := make(map[pair]int)

	for _, groupIDs := range commitFiles {
		if len(groupIDs) < 2 || len(groupIDs) > maxFiles {
			continue
		}
		// Sort for consistent pair ordering
		sort.Slice(groupIDs, func(i, j int) bool { return groupIDs[i] < groupIDs[j] })
		// Generate all pairs
		for i := 0; i < len(groupIDs); i++ {
			for j := i + 1; j < len(groupIDs); j++ {
				p := pair{groupIDs[i], groupIDs[j]}
				pairCounts[p]++
			}
		}
	}

	// Step 4: Filter and sort
	type couplingEntry struct {
		pathA string
		pathB string
		count int
	}

	var results []couplingEntry
	for p, count := range pairCounts {
		if count < minCount {
			continue
		}
		pathA := pathMap[p.a]
		pathB := pathMap[p.b]
		// Apply path filter: at least one file must match
		if flags.pathGlob != "" && !matchPath(flags.pathGlob, pathA) && !matchPath(flags.pathGlob, pathB) {
			continue
		}
		results = append(results, couplingEntry{pathA, pathB, count})
	}

	sort.Slice(results, func(i, j int) bool {
		return results[i].count > results[j].count
	})

	spinner.Stop()

	// Apply limit
	if flags.limit > 0 && len(results) > flags.limit {
		results = results[:flags.limit]
	}

	// Build table data
	columns := []string{"file_a", "file_b", "commits_together"}
	tableRows := make([][]string, len(results))
	for i, e := range results {
		tableRows[i] = []string{e.pathA, e.pathB, strconv.Itoa(e.count)}
	}

	title := fmt.Sprintf("coupling: top %d pairs", len(results))
	return table.DisplayResults(title, columns, tableRows, flags.displayOpts())
}

// ═══════════════════════════════════════════════════════════════════════════
// hotspots — Churn aggregated by directory
// ═══════════════════════════════════════════════════════════════════════════

func newAnalyzeHotspotsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "hotspots",
		Short: "Churn aggregated by directory",
		Long: `Aggregate file modification counts by directory prefix.

For large repos, per-file churn is noisy. Hotspots shows which
directories (subsystems) accumulate the most changes, helping you
identify problem areas at a higher level.

Use --depth to control how many directory levels to aggregate.`,
		RunE: runAnalyzeHotspots,
	}
	addAnalyzeFlags(cmd)
	cmd.Flags().Int("depth", 1, "Directory depth to aggregate at (1 = top-level)")
	return cmd
}

func runAnalyzeHotspots(cmd *cobra.Command, args []string) error {
	flags := parseAnalyzeFlags(cmd)
	depth, _ := cmd.Flags().GetInt("depth")
	if depth < 1 {
		depth = 1
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	r, err := connectRepo(ctx)
	if err != nil {
		return err
	}
	defer r.Close()

	spinner := ui.NewSpinner("Analyzing directory hotspots")
	spinner.Start()

	// Same base query as churn
	rows, err := r.DB.Query(ctx, `
		SELECT p.path, COUNT(*) as versions
		FROM pgit_file_refs r
		JOIN pgit_paths p ON p.group_id = r.group_id
		GROUP BY p.path
	`)
	if err != nil {
		spinner.Stop()
		return err
	}

	// Aggregate by directory prefix in Go
	type dirStats struct {
		files    int
		versions int64
	}
	dirMap := make(map[string]*dirStats)

	for rows.Next() {
		var path string
		var versions int64
		if err := rows.Scan(&path, &versions); err != nil {
			rows.Close()
			spinner.Stop()
			return err
		}
		if !matchPath(flags.pathGlob, path) {
			continue
		}
		dir := extractDirPrefix(path, depth)
		stats, ok := dirMap[dir]
		if !ok {
			stats = &dirStats{}
			dirMap[dir] = stats
		}
		stats.files++
		stats.versions += versions
	}
	rows.Close()

	spinner.Stop()

	// Sort by total versions descending
	type hotspotEntry struct {
		dir      string
		files    int
		versions int64
		avg      float64
	}
	var results []hotspotEntry
	for dir, stats := range dirMap {
		avg := float64(stats.versions) / float64(stats.files)
		results = append(results, hotspotEntry{dir, stats.files, stats.versions, avg})
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].versions > results[j].versions
	})

	// Apply limit
	if flags.limit > 0 && len(results) > flags.limit {
		results = results[:flags.limit]
	}

	// Build table data
	columns := []string{"directory", "files", "total_versions", "avg_versions"}
	tableRows := make([][]string, len(results))
	for i, e := range results {
		tableRows[i] = []string{
			e.dir,
			strconv.Itoa(e.files),
			strconv.FormatInt(e.versions, 10),
			fmt.Sprintf("%.1f", e.avg),
		}
	}

	title := fmt.Sprintf("hotspots: top %d directories (depth %d)", len(results), depth)
	return table.DisplayResults(title, columns, tableRows, flags.displayOpts())
}

// extractDirPrefix returns the directory prefix at the given depth.
// depth=1: "src/foo/bar.go" -> "src/"
// depth=2: "src/foo/bar.go" -> "src/foo/"
// Files at root level with no directory return "(root)".
func extractDirPrefix(path string, depth int) string {
	parts := strings.Split(path, "/")
	if len(parts) <= depth {
		// File is at or above the requested depth, use its directory
		if len(parts) == 1 {
			return "(root)"
		}
		return strings.Join(parts[:len(parts)-1], "/") + "/"
	}
	return strings.Join(parts[:depth], "/") + "/"
}

// ═══════════════════════════════════════════════════════════════════════════
// authors — Commits per author
// ═══════════════════════════════════════════════════════════════════════════

func newAnalyzeAuthorsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "authors",
		Short: "Commits per author",
		Long: `Rank contributors by commit count.

Shows each author's name, email, total commits, and the dates of their
first and last commit. Useful for understanding contributor distribution
and identifying core maintainers vs occasional contributors.

The query uses a front-to-back sequential scan of pgit_commits, which
is the optimal access pattern for xpatch delta-compressed tables.`,
		RunE: runAnalyzeAuthors,
	}
	addAnalyzeFlags(cmd)
	return cmd
}

func runAnalyzeAuthors(cmd *cobra.Command, args []string) error {
	flags := parseAnalyzeFlags(cmd)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	r, err := connectRepo(ctx)
	if err != nil {
		return err
	}
	defer r.Close()

	spinner := ui.NewSpinner("Analyzing commit authors")
	spinner.Start()

	// Front-to-back sequential scan — optimal xpatch access pattern.
	// ORDER BY authored_at ASC decompresses the delta chain in natural order,
	// each row reusing the previous row's cached decompression result.
	rows, err := r.DB.Query(ctx, `
		SELECT author_name, author_email, authored_at
		FROM pgit_commits
		ORDER BY authored_at ASC
	`)
	if err != nil {
		spinner.Stop()
		return err
	}

	type authorKey struct {
		name  string
		email string
	}
	type authorStats struct {
		commits int
		first   time.Time
		last    time.Time
	}
	statsMap := make(map[authorKey]*authorStats)

	for rows.Next() {
		var name, email string
		var authoredAt time.Time
		if err := rows.Scan(&name, &email, &authoredAt); err != nil {
			rows.Close()
			spinner.Stop()
			return err
		}
		k := authorKey{name, email}
		s, ok := statsMap[k]
		if !ok {
			s = &authorStats{first: authoredAt}
			statsMap[k] = s
		}
		s.commits++
		s.last = authoredAt
	}
	rows.Close()

	spinner.Stop()

	// Sort by commit count descending
	type authorEntry struct {
		name    string
		email   string
		commits int
		first   time.Time
		last    time.Time
	}
	var results []authorEntry
	for k, s := range statsMap {
		results = append(results, authorEntry{k.name, k.email, s.commits, s.first, s.last})
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].commits > results[j].commits
	})

	// Apply limit
	if flags.limit > 0 && len(results) > flags.limit {
		results = results[:flags.limit]
	}

	// Build table data
	columns := []string{"author", "email", "commits", "first_commit", "last_commit"}
	tableRows := make([][]string, len(results))
	for i, e := range results {
		tableRows[i] = []string{
			e.name,
			e.email,
			strconv.Itoa(e.commits),
			e.first.Format("2006-01-02"),
			e.last.Format("2006-01-02"),
		}
	}

	title := fmt.Sprintf("authors: %d contributors", len(results))
	return table.DisplayResults(title, columns, tableRows, flags.displayOpts())
}

// ═══════════════════════════════════════════════════════════════════════════
// activity — Commits over time
// ═══════════════════════════════════════════════════════════════════════════

func newAnalyzeActivityCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "activity",
		Short: "Commit activity over time",
		Long: `Show commit counts bucketed by time period.

Useful for understanding velocity trends, identifying slowdowns or
acceleration, and seeing the effect of team changes on output.

Periods with zero commits are included so the timeline is continuous.`,
		RunE: runAnalyzeActivity,
	}
	addAnalyzeFlags(cmd)
	cmd.Flags().String("period", "month", "Time bucket: week, month, quarter, year")
	return cmd
}

func runAnalyzeActivity(cmd *cobra.Command, args []string) error {
	flags := parseAnalyzeFlags(cmd)
	period, _ := cmd.Flags().GetString("period")

	// Validate period
	switch period {
	case "week", "month", "quarter", "year":
	default:
		return fmt.Errorf("invalid period %q: must be week, month, quarter, or year", period)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	r, err := connectRepo(ctx)
	if err != nil {
		return err
	}
	defer r.Close()

	spinner := ui.NewSpinner("Analyzing commit activity")
	spinner.Start()

	// Front-to-back sequential scan — optimal xpatch access pattern
	rows, err := r.DB.Query(ctx, `
		SELECT authored_at
		FROM pgit_commits
		ORDER BY authored_at ASC
	`)
	if err != nil {
		spinner.Stop()
		return err
	}

	// Collect all timestamps
	var timestamps []time.Time
	for rows.Next() {
		var t time.Time
		if err := rows.Scan(&t); err != nil {
			rows.Close()
			spinner.Stop()
			return err
		}
		timestamps = append(timestamps, t)
	}
	rows.Close()

	spinner.Stop()

	if len(timestamps) == 0 {
		columns := []string{"period", "commits"}
		return table.DisplayResults("activity: 0 commits", columns, nil, flags.displayOpts())
	}

	// Bucket timestamps by period
	bucketKey := func(t time.Time) string {
		switch period {
		case "week":
			year, week := t.ISOWeek()
			return fmt.Sprintf("%d-W%02d", year, week)
		case "month":
			return t.Format("2006-01")
		case "quarter":
			q := (t.Month()-1)/3 + 1
			return fmt.Sprintf("%d-Q%d", t.Year(), q)
		case "year":
			return fmt.Sprintf("%d", t.Year())
		}
		return ""
	}

	// Count per bucket (maintaining order)
	bucketCounts := make(map[string]int)
	for _, t := range timestamps {
		k := bucketKey(t)
		bucketCounts[k]++
	}

	// Generate all periods between first and last to fill gaps
	allPeriods := generatePeriodRange(timestamps[0], timestamps[len(timestamps)-1], period)

	// Build table data
	columns := []string{"period", "commits"}
	var tableRows [][]string
	for _, p := range allPeriods {
		count := bucketCounts[p]
		tableRows = append(tableRows, []string{p, strconv.Itoa(count)})
	}

	title := fmt.Sprintf("activity: %d %ss", len(tableRows), period)
	return table.DisplayResults(title, columns, tableRows, flags.displayOpts())
}

// generatePeriodRange generates all period keys between start and end (inclusive).
func generatePeriodRange(start, end time.Time, period string) []string {
	var periods []string
	seen := make(map[string]bool)

	current := start
	for !current.After(end) {
		var key string
		switch period {
		case "week":
			year, week := current.ISOWeek()
			key = fmt.Sprintf("%d-W%02d", year, week)
			current = current.AddDate(0, 0, 7)
		case "month":
			key = current.Format("2006-01")
			current = current.AddDate(0, 1, 0)
		case "quarter":
			q := (current.Month()-1)/3 + 1
			key = fmt.Sprintf("%d-Q%d", current.Year(), q)
			current = current.AddDate(0, 3, 0)
		case "year":
			key = fmt.Sprintf("%d", current.Year())
			current = current.AddDate(1, 0, 0)
		}
		if !seen[key] {
			periods = append(periods, key)
			seen[key] = true
		}
	}

	return periods
}

// ═══════════════════════════════════════════════════════════════════════════
// bus-factor — Distinct authors per file
// ═══════════════════════════════════════════════════════════════════════════

func newAnalyzeBusFactorCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bus-factor",
		Short: "Files with fewest distinct authors (knowledge silos)",
		Long: `For each file, count how many distinct authors have touched it.

Files with only 1 author are knowledge silos — if that person leaves,
nobody else knows the code. This is a bus-factor risk.

Results are sorted by author count ascending (most vulnerable first).

This uses a two-step optimized query: first a front-to-back scan of
pgit_commits to build a commit→author map, then a scan of pgit_file_refs
(heap table) to resolve authors per file. This avoids the expensive JOIN
onto xpatch tables that the naive SQL approach would require.`,
		RunE: runAnalyzeBusFactor,
	}
	addAnalyzeFlags(cmd)
	cmd.Flags().Int("max-authors", 0, "Only show files with at most this many authors (0 = all)")
	return cmd
}

func runAnalyzeBusFactor(cmd *cobra.Command, args []string) error {
	flags := parseAnalyzeFlags(cmd)
	maxAuthors, _ := cmd.Flags().GetInt("max-authors")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	r, err := connectRepo(ctx)
	if err != nil {
		return err
	}
	defer r.Close()

	spinner := ui.NewSpinner("Analyzing bus factor")
	spinner.Start()

	// Step 1: Build commit_id -> author_name map from pgit_commits.
	// Front-to-back sequential scan (ORDER BY authored_at ASC) is the
	// optimal xpatch access pattern — each row reuses the previous
	// row's cached decompression result.
	commitRows, err := r.DB.Query(ctx, `
		SELECT id, author_name
		FROM pgit_commits
		ORDER BY authored_at ASC
	`)
	if err != nil {
		spinner.Stop()
		return err
	}

	commitAuthor := make(map[string]string)
	for commitRows.Next() {
		var id, author string
		if err := commitRows.Scan(&id, &author); err != nil {
			commitRows.Close()
			spinner.Stop()
			return err
		}
		commitAuthor[id] = author
	}
	commitRows.Close()

	// Step 2: Scan file_refs (heap table, fast) and resolve authors
	refRows, err := r.DB.Query(ctx, `
		SELECT group_id, commit_id
		FROM pgit_file_refs
	`)
	if err != nil {
		spinner.Stop()
		return err
	}

	// group_id -> set of authors
	fileAuthors := make(map[int32]map[string]bool)
	for refRows.Next() {
		var groupID int32
		var commitID string
		if err := refRows.Scan(&groupID, &commitID); err != nil {
			refRows.Close()
			spinner.Stop()
			return err
		}
		author, ok := commitAuthor[commitID]
		if !ok {
			continue
		}
		if fileAuthors[groupID] == nil {
			fileAuthors[groupID] = make(map[string]bool)
		}
		fileAuthors[groupID][author] = true
	}
	refRows.Close()

	// Step 3: Fetch group_id -> path mapping
	pathRows, err := r.DB.Query(ctx, `SELECT group_id, path FROM pgit_paths`)
	if err != nil {
		spinner.Stop()
		return err
	}

	pathMap := make(map[int32]string)
	for pathRows.Next() {
		var groupID int32
		var path string
		if err := pathRows.Scan(&groupID, &path); err != nil {
			pathRows.Close()
			spinner.Stop()
			return err
		}
		pathMap[groupID] = path
	}
	pathRows.Close()

	spinner.Stop()

	// Build results
	type busFactorEntry struct {
		path       string
		authors    int
		authorList string
	}
	var results []busFactorEntry
	for groupID, authors := range fileAuthors {
		path, ok := pathMap[groupID]
		if !ok {
			continue
		}
		if !matchPath(flags.pathGlob, path) {
			continue
		}
		if maxAuthors > 0 && len(authors) > maxAuthors {
			continue
		}
		// Build sorted author list
		var names []string
		for name := range authors {
			names = append(names, name)
		}
		sort.Strings(names)
		results = append(results, busFactorEntry{
			path:       path,
			authors:    len(authors),
			authorList: strings.Join(names, ", "),
		})
	}

	// Sort by author count ascending (most vulnerable first)
	sort.Slice(results, func(i, j int) bool {
		if results[i].authors != results[j].authors {
			return results[i].authors < results[j].authors
		}
		return results[i].path < results[j].path
	})

	// Apply limit
	if flags.limit > 0 && len(results) > flags.limit {
		results = results[:flags.limit]
	}

	// Build table data
	columns := []string{"path", "authors", "author_list"}
	tableRows := make([][]string, len(results))
	for i, e := range results {
		tableRows[i] = []string{
			e.path,
			strconv.Itoa(e.authors),
			e.authorList,
		}
	}

	title := fmt.Sprintf("bus-factor: %d files", len(results))
	return table.DisplayResults(title, columns, tableRows, flags.displayOpts())
}
