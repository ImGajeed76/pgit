package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/imgajeed76/pgit/v4/internal/ui/styles"
	"golang.org/x/term"
)

// ═══════════════════════════════════════════════════════════════════════════
// pgit-bench: Fair compression benchmark between git and pgit
//
// Usage:
//   pgit-bench <github-url> [<url>...] [options]
//   pgit-bench --file repos.txt [options]
//
// Clones repositories, measures git storage (normal + aggressive gc),
// imports into pgit, measures pgit storage (on-disk + actual data),
// and presents a detailed comparison with optional markdown report.
// ═══════════════════════════════════════════════════════════════════════════

// ═══════════════════════════════════════════════════════════════════════════
// Terminal: TTY detection and lipgloss styles
// ═══════════════════════════════════════════════════════════════════════════

// isTTY is true when stdout is a terminal and accessibility mode is off
var isTTY bool

func init() {
	isTTY = term.IsTerminal(int(os.Stdout.Fd())) && !styles.IsAccessible()
}

// Lipgloss styles using pgit's color palette
var (
	stBold    = lipgloss.NewStyle().Bold(true)
	stDim     = lipgloss.NewStyle().Foreground(styles.Muted)
	stAccent  = lipgloss.NewStyle().Foreground(styles.Accent)
	stSuccess = lipgloss.NewStyle().Foreground(styles.Success)
	stWarning = lipgloss.NewStyle().Foreground(styles.Warning)
	stError   = lipgloss.NewStyle().Foreground(styles.Error)
	stInfo    = lipgloss.NewStyle().Foreground(styles.Info)

	// Progress bar styles
	stBarFull  = lipgloss.NewStyle().Foreground(styles.Success)
	stBarEmpty = lipgloss.NewStyle().Foreground(styles.Muted)
)

// render applies a lipgloss style, respecting NoColor
func render(s lipgloss.Style, text string) string {
	if styles.NoColor() {
		return text
	}
	return s.Render(text)
}

func main() {
	args := parseArgs()
	if len(args.repoURLs) == 0 {
		printUsage()
		os.Exit(1)
	}

	multiRepo := len(args.repoURLs) > 1

	// ─── Single repo mode (backward compatible) ───────────────────────
	if !multiRepo && !args.jsonMode && args.reportPath == "" {
		result, err := benchmarkRepoInteractive(args.repoURLs[0], args.branch)
		if err != nil {
			fatalMsg("Benchmark failed: %v", err)
		}
		// Print paths
		fmt.Println()
		sectionHeader("Paths")
		fmt.Printf("  Git repo:    %s\n", result.GitDir)
		fmt.Printf("  pgit repo:   %s\n", result.PgitDir)
		fmt.Printf("  Log file:    %s\n", result.LogFile)
		fmt.Println()
		return
	}

	// ─── Single repo JSON-only mode ───────────────────────────────────
	if !multiRepo && args.jsonMode && args.reportPath == "" {
		result, err := benchmarkRepoQuiet(args.repoURLs[0], args.branch)
		if err != nil {
			fatalMsg("Benchmark failed: %v", err)
		}
		writeJSONOutput([]benchResult{*result}, args.jsonPath)
		return
	}

	// ─── Multi-repo / report mode ─────────────────────────────────────
	results := runMultiRepo(args)

	// JSON output
	if args.jsonMode {
		writeJSONOutput(results, args.jsonPath)
	}

	// Markdown report
	if args.reportPath != "" {
		if err := writeMarkdownReport(args.reportPath, results); err != nil {
			fatalMsg("Failed to write report: %v", err)
		}
		if !args.jsonMode {
			fmt.Printf("\n  %s\n\n", styles.SuccessMsg(fmt.Sprintf("Report written to %s", args.reportPath)))
		}
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// Data types
// ═══════════════════════════════════════════════════════════════════════════

type gitStats struct {
	packfileBytes int64
	idxBytes      int64
	otherBytes    int64
	totalBytes    int64
	commitCount   int64
}

type gitRawSize struct {
	totalBytes  int64
	blobBytes   int64
	treeBytes   int64
	commitBytes int64
	tagBytes    int64
	blobCount   int64
	treeCount   int64
	commitCount int64
	tagCount    int64
}

type pgitTableStats struct {
	name       string
	heapBytes  int64
	toastBytes int64
	totalBytes int64
}

type pgitXpatchStats struct {
	tableName        string
	totalRows        int64
	totalGroups      int64
	keyframeCount    int64
	deltaCount       int64
	rawSizeBytes     int64
	compressedBytes  int64
	compressionRatio float64
	avgChainLength   float64
}

type pgitNormalTableStats struct {
	tableName    string
	totalBytes   int64
	rawDataBytes int64
	rowCount     int64
}

type pgitStats struct {
	tables       []pgitTableStats
	xpatch       []pgitXpatchStats
	normalTables []pgitNormalTableStats
	indexBytes   int64
	commitCount  int64
	fileCount    int64
}

func (p *pgitStats) totalOnDisk() int64 {
	var total int64
	for _, t := range p.tables {
		total += t.totalBytes
	}
	return total
}

func (p *pgitStats) totalActualData() int64 {
	var total int64
	for _, x := range p.xpatch {
		total += x.compressedBytes
	}
	for _, n := range p.normalTables {
		total += n.rawDataBytes
	}
	return total
}

func (p *pgitStats) totalRawUncompressed() int64 {
	var total int64
	for _, x := range p.xpatch {
		total += x.rawSizeBytes
	}
	for _, n := range p.normalTables {
		total += n.rawDataBytes
	}
	return total
}

func (p *pgitStats) totalOverhead() int64 {
	return p.totalOnDisk() - p.totalActualData()
}

// benchResult is the complete result for one repository benchmark
type benchResult struct {
	Repo      string `json:"repo"`
	RepoName  string `json:"repo_name"`
	Branch    string `json:"branch"`
	Timestamp string `json:"timestamp"`
	Duration  string `json:"duration_seconds"`

	Commits int64 `json:"commits"`
	Files   int64 `json:"files"`

	RawUncompressedBytes int64 `json:"raw_uncompressed_bytes"`

	GitNormalPackfileBytes     int64 `json:"git_normal_packfile_bytes"`
	GitAggressivePackfileBytes int64 `json:"git_aggressive_packfile_bytes"`

	PgitOnDiskBytes     int64 `json:"pgit_on_disk_bytes"`
	PgitActualDataBytes int64 `json:"pgit_actual_data_bytes"`
	PgitOverheadBytes   int64 `json:"pgit_overhead_bytes"`
	PgitIndexBytes      int64 `json:"pgit_index_bytes"`

	CompressionRatioGitNormal     float64 `json:"compression_ratio_git_normal"`
	CompressionRatioGitAggressive float64 `json:"compression_ratio_git_aggressive"`
	CompressionRatioOnDisk        float64 `json:"compression_ratio_on_disk"`
	CompressionRatioData          float64 `json:"compression_ratio_actual_data"`

	RatioVsNormalOnDisk         float64 `json:"ratio_vs_normal_on_disk"`
	RatioVsAggressiveOnDisk     float64 `json:"ratio_vs_aggressive_on_disk"`
	RatioVsNormalActualData     float64 `json:"ratio_vs_normal_actual_data"`
	RatioVsAggressiveActualData float64 `json:"ratio_vs_aggressive_actual_data"`

	OverheadPercent float64 `json:"overhead_percent"`

	GitDir  string `json:"git_dir"`
	PgitDir string `json:"pgit_dir"`
	LogFile string `json:"log_file"`

	Error string `json:"error,omitempty"`

	// Detailed stats for report generation (not in JSON)
	gitNormal  gitStats   `json:"-"`
	gitAgg     gitStats   `json:"-"`
	gitRaw     gitRawSize `json:"-"`
	pgit       pgitStats  `json:"-"`
	cloneSecs  float64    `json:"-"`
	gcAggrSecs float64    `json:"-"`
	importSecs float64    `json:"-"`
}

// ═══════════════════════════════════════════════════════════════════════════
// Argument parsing
// ═══════════════════════════════════════════════════════════════════════════

type cliArgs struct {
	repoURLs   []string
	branch     string
	logPath    string
	jsonMode   bool
	jsonPath   string // "" = stdout
	reportPath string
	filePath   string
	parallel   int
}

// jsonMode controls whether terminal output is suppressed
var jsonMode bool

func parseArgs() cliArgs {
	args := cliArgs{parallel: 2}
	osArgs := os.Args[1:]

	for i := 0; i < len(osArgs); i++ {
		switch osArgs[i] {
		case "--branch", "-b":
			if i+1 < len(osArgs) {
				i++
				args.branch = osArgs[i]
			}
		case "--log", "-l":
			if i+1 < len(osArgs) {
				i++
				args.logPath = osArgs[i]
			}
		case "--json", "-j":
			args.jsonMode = true
			jsonMode = true
			// Check if next arg is a path (not a flag, not a URL)
			if i+1 < len(osArgs) && !strings.HasPrefix(osArgs[i+1], "-") && !strings.HasPrefix(osArgs[i+1], "http") {
				i++
				args.jsonPath = osArgs[i]
			}
		case "--report", "-r":
			if i+1 < len(osArgs) {
				i++
				args.reportPath = osArgs[i]
			}
		case "--file", "-f":
			if i+1 < len(osArgs) {
				i++
				args.filePath = osArgs[i]
			}
		case "--parallel", "-p":
			if i+1 < len(osArgs) {
				i++
				n, err := strconv.Atoi(osArgs[i])
				if err != nil || n < 1 {
					fatalMsg("--parallel requires a positive integer, got: %s", osArgs[i])
				}
				args.parallel = n
			}
		case "--no-color":
			styles.SetNoColor(true)
		case "--help", "-h":
			printUsage()
			os.Exit(0)
		default:
			if !strings.HasPrefix(osArgs[i], "-") {
				args.repoURLs = append(args.repoURLs, osArgs[i])
			}
		}
	}

	// Load repos from file if specified
	if args.filePath != "" {
		fileURLs, err := readRepoFile(args.filePath)
		if err != nil {
			fatalMsg("Failed to read repo file: %v", err)
		}
		args.repoURLs = append(args.repoURLs, fileURLs...)
	}

	return args
}

func readRepoFile(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var urls []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Fields(line)
		urls = append(urls, parts[0])
	}
	return urls, scanner.Err()
}

func printUsage() {
	b := render(stBold, "")
	r := "" // reset handled by lipgloss
	_ = b
	_ = r

	fmt.Printf(`%s - Fair compression benchmark between git and pgit

%s
  pgit-bench <github-url> [<url>...] [options]
  pgit-bench --file repos.txt [options]

%s
  --branch, -b <name>     Branch to benchmark (default: auto-detect)
  --parallel, -p <n>      Max concurrent benchmarks (default: 2)
  --report, -r <path>     Generate markdown report with charts
  --json, -j [path]       JSON output (file path or stdout if omitted)
  --file, -f <path>       Read repo URLs from file (one per line)
  --log, -l <path>        Log file path (single-repo mode only)
  --no-color              Disable colored output
  --help, -h              Show this help

%s
  # Single repo
  pgit-bench https://github.com/tokio-rs/tokio

  # Multiple repos with report
  pgit-bench --report bench.md \
    https://github.com/tokio-rs/tokio \
    https://github.com/serde-rs/serde \
    https://github.com/BurntSushi/ripgrep

  # From file, parallel, with JSON + report
  pgit-bench --file repos.txt --parallel 3 --report bench.md --json results.json

  # JSON to stdout
  pgit-bench --json https://github.com/golang/go

%s
  # Lines starting with # are comments
  https://github.com/tokio-rs/tokio
  https://github.com/serde-rs/serde
  https://github.com/golang/go

%s
  - git and pgit must be on PATH
  - pgit local container must be running (pgit local start)

`,
		render(stBold, "pgit-bench"),
		render(stBold, "Usage:"),
		render(stBold, "Options:"),
		render(stBold, "Examples:"),
		render(stBold, "File format:"),
		render(stBold, "Requirements:"))
}

// ═══════════════════════════════════════════════════════════════════════════
// Core benchmark pipeline (single repo)
// ═══════════════════════════════════════════════════════════════════════════

func benchmarkRepoQuiet(repoURL, branch string) (*benchResult, error) {
	return benchmarkRepo(repoURL, branch, false, nil)
}

func benchmarkRepoInteractive(repoURL, branch string) (*benchResult, error) {
	return benchmarkRepo(repoURL, branch, true, nil)
}

type statusCallback func(phase string)

func benchmarkRepo(repoURL, branch string, interactive bool, onStatus statusCallback) (*benchResult, error) {
	suffix := randomSuffix()
	repoName := extractRepoName(repoURL)
	gitDir := filepath.Join("/tmp", fmt.Sprintf("pgit-bench-git-%s-%s", repoName, suffix))
	pgitDir := filepath.Join("/tmp", fmt.Sprintf("pgit-bench-pgit-%s-%s", repoName, suffix))
	logPath := filepath.Join("/tmp", fmt.Sprintf("pgit-bench-%s-%s.log", repoName, suffix))

	logFile, err := os.Create(logPath)
	if err != nil {
		return nil, fmt.Errorf("create log file: %w", err)
	}
	defer logFile.Close()
	log := newLogger(logFile)

	if interactive {
		printHeader(repoURL, repoName)
	}
	log.header(repoURL, repoName, gitDir, pgitDir)

	benchStart := time.Now()
	report := func(status string) {
		if onStatus != nil {
			onStatus(status)
		}
	}

	// ── Phase 1: Clone ──
	report("cloning")
	if interactive {
		phaseHeader("1", "Cloning git repository")
	}
	log.phase("1", "Cloning git repository")

	if branch == "" {
		branch = detectDefaultBranch(repoURL)
	}
	if interactive {
		infoMsg("Branch: %s", branch)
	}
	log.info("Branch: %s", branch)

	cloneStart := time.Now()
	if err := runQuietErr("git", "clone", "--single-branch", "--branch", branch, repoURL, gitDir); err != nil {
		return nil, fmt.Errorf("git clone failed: %w", err)
	}
	cloneDuration := time.Since(cloneStart)

	if interactive {
		successMsg("Cloned in %s", formatDuration(cloneDuration))
	}
	log.info("Clone completed in %s", formatDuration(cloneDuration))

	// ── Phase 2: Git stats (normal) ──
	report("git gc")
	if interactive {
		phaseHeader("2", "Measuring git storage (normal clone)")
	}
	log.phase("2", "Measuring git storage (normal clone)")

	_ = runQuietInDirErr(gitDir, "git", "gc", "--quiet")
	gitNormal := collectGitStats(gitDir)
	if interactive {
		printGitStats("Normal clone", gitNormal)
	}
	log.gitStats("Normal clone", gitNormal)

	// ── Phase 3: Git stats (aggressive) ──
	report("git gc --aggressive")
	if interactive {
		phaseHeader("3", "Measuring git storage (aggressive gc)")
		infoMsg("Running git gc --aggressive (this may take a while)...")
	}
	log.phase("3", "Measuring git storage (aggressive gc)")

	aggressiveStart := time.Now()
	_ = runQuietInDirErr(gitDir, "git", "gc", "--aggressive", "--quiet")
	aggressiveDuration := time.Since(aggressiveStart)
	if interactive {
		infoMsg("Aggressive gc completed in %s", formatDuration(aggressiveDuration))
	}
	log.info("Aggressive gc completed in %s", formatDuration(aggressiveDuration))

	gitAggressive := collectGitStats(gitDir)
	if interactive {
		printGitStats("Aggressive gc", gitAggressive)
	}
	log.gitStats("Aggressive gc", gitAggressive)

	// Raw object sizes
	gitRaw := collectGitRawSize(gitDir)
	if interactive {
		infoMsg("  Raw uncompressed:  %s  %s", formatBytes(gitRaw.totalBytes), render(stDim, "(sum of all object sizes)"))
		infoMsg("    Blobs:           %s  %s", formatBytes(gitRaw.blobBytes), render(stDim, fmt.Sprintf("(%s objects)", formatCount(gitRaw.blobCount))))
		infoMsg("    Trees:           %s  %s", formatBytes(gitRaw.treeBytes), render(stDim, fmt.Sprintf("(%s objects)", formatCount(gitRaw.treeCount))))
		infoMsg("    Commits:         %s  %s", formatBytes(gitRaw.commitBytes), render(stDim, fmt.Sprintf("(%s objects)", formatCount(gitRaw.commitCount))))
		if gitRaw.tagBytes > 0 {
			infoMsg("    Tags:            %s  %s", formatBytes(gitRaw.tagBytes), render(stDim, fmt.Sprintf("(%s objects)", formatCount(gitRaw.tagCount))))
		}
	}
	log.info("Raw uncompressed: %d bytes (%s)", gitRaw.totalBytes, formatBytes(gitRaw.totalBytes))

	// ── Phase 4: Import into pgit ──
	report("importing")
	if interactive {
		phaseHeader("4", "Importing into pgit")
	}
	log.phase("4", "Importing into pgit")

	if err := os.MkdirAll(pgitDir, 0755); err != nil {
		return nil, fmt.Errorf("create pgit dir: %w", err)
	}
	if err := runQuietInDirErr(pgitDir, "pgit", "init"); err != nil {
		return nil, fmt.Errorf("pgit init failed: %w", err)
	}
	if interactive {
		successMsg("Initialized pgit repo at %s", pgitDir)
	}
	log.info("Initialized pgit repo at %s", pgitDir)

	importStart := time.Now()
	if interactive {
		runInDirPassthrough(pgitDir, "pgit", "import", "--branch", branch, gitDir)
	} else {
		if err := runQuietInDirErr(pgitDir, "pgit", "import", "--branch", branch, gitDir); err != nil {
			return nil, fmt.Errorf("pgit import failed: %w", err)
		}
	}
	importDuration := time.Since(importStart)
	if interactive {
		successMsg("Imported in %s", formatDuration(importDuration))
	}
	log.info("Import completed in %s", formatDuration(importDuration))

	// ── Phase 5: pgit stats ──
	report("collecting stats")
	if interactive {
		phaseHeader("5", "Measuring pgit storage")
	}
	log.phase("5", "Measuring pgit storage")

	pgitSt := collectPgitStats(pgitDir)
	if interactive {
		printPgitStats(pgitSt)
	}
	log.pgitStats(pgitSt)

	// ── Phase 6: Comparison ──
	if interactive {
		phaseHeader("6", "Comparison")
		printComparison(gitNormal, gitAggressive, gitRaw, pgitSt)
	}
	log.phase("6", "Comparison")
	log.comparison(gitNormal, gitAggressive, gitRaw, pgitSt)

	benchDuration := time.Since(benchStart)
	log.info("Total benchmark time: %s", formatDuration(benchDuration))

	// Build result
	pgitOnDisk := pgitSt.totalOnDisk()
	pgitActual := pgitSt.totalActualData()
	raw := gitRaw.totalBytes

	result := &benchResult{
		Repo:      repoURL,
		RepoName:  repoName,
		Branch:    branch,
		Timestamp: time.Now().Format(time.RFC3339),
		Duration:  fmt.Sprintf("%.1f", benchDuration.Seconds()),

		Commits: pgitSt.commitCount,
		Files:   pgitSt.fileCount,

		RawUncompressedBytes: raw,

		GitNormalPackfileBytes:     gitNormal.packfileBytes,
		GitAggressivePackfileBytes: gitAggressive.packfileBytes,

		PgitOnDiskBytes:     pgitOnDisk,
		PgitActualDataBytes: pgitActual,
		PgitOverheadBytes:   pgitSt.totalOverhead(),
		PgitIndexBytes:      pgitSt.indexBytes,

		CompressionRatioGitNormal:     safeRatio(raw, gitNormal.packfileBytes),
		CompressionRatioGitAggressive: safeRatio(raw, gitAggressive.packfileBytes),
		CompressionRatioOnDisk:        safeRatio(raw, pgitOnDisk),
		CompressionRatioData:          safeRatio(raw, pgitActual),

		RatioVsNormalOnDisk:         safeRatioF(pgitOnDisk, gitNormal.packfileBytes),
		RatioVsAggressiveOnDisk:     safeRatioF(pgitOnDisk, gitAggressive.packfileBytes),
		RatioVsNormalActualData:     safeRatioF(pgitActual, gitNormal.packfileBytes),
		RatioVsAggressiveActualData: safeRatioF(pgitActual, gitAggressive.packfileBytes),

		OverheadPercent: safePercent(pgitSt.totalOverhead(), pgitOnDisk),

		GitDir:  gitDir,
		PgitDir: pgitDir,
		LogFile: logPath,

		gitNormal:  gitNormal,
		gitAgg:     gitAggressive,
		gitRaw:     gitRaw,
		pgit:       pgitSt,
		cloneSecs:  cloneDuration.Seconds(),
		gcAggrSecs: aggressiveDuration.Seconds(),
		importSecs: importDuration.Seconds(),
	}

	return result, nil
}

// ═══════════════════════════════════════════════════════════════════════════
// Multi-repo orchestration with progress
// ═══════════════════════════════════════════════════════════════════════════

type repoState int

const (
	stateQueued repoState = iota
	stateRunning
	stateDone
	stateFailed
)

type repoProgress struct {
	mu        sync.Mutex
	repos     []string // short names
	states    []repoState
	phases    []string // current phase for running repos
	results   []*benchResult
	errors    []error
	total     int
	done      int
	startTime time.Time
}

func newRepoProgress(urls []string) *repoProgress {
	names := make([]string, len(urls))
	for i, u := range urls {
		names[i] = shortName(extractRepoName(u))
	}
	return &repoProgress{
		repos:     names,
		states:    make([]repoState, len(urls)),
		phases:    make([]string, len(urls)),
		results:   make([]*benchResult, len(urls)),
		errors:    make([]error, len(urls)),
		total:     len(urls),
		startTime: time.Now(),
	}
}

func shortName(repoName string) string {
	// repoName is "org-repo" from extractRepoName (e.g. "serde-rs-serde")
	// The repo is always the last URL path segment, which is the part after
	// the last "-" in our org-repo format.
	if idx := strings.LastIndex(repoName, "-"); idx > 0 {
		return repoName[idx+1:]
	}
	return repoName
}

func (p *repoProgress) setRunning(idx int, phase string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.states[idx] = stateRunning
	p.phases[idx] = phase
}

func (p *repoProgress) setPhase(idx int, phase string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.phases[idx] = phase
}

func (p *repoProgress) setDone(idx int, result *benchResult) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.states[idx] = stateDone
	p.results[idx] = result
	p.done++
}

func (p *repoProgress) setFailed(idx int, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.states[idx] = stateFailed
	p.errors[idx] = err
	p.done++
}

// renderTTY renders a single-line progress bar for TTY mode.
// Uses \r\033[K to update in place (pgit pattern).
func (p *repoProgress) renderTTY() string {
	p.mu.Lock()
	defer p.mu.Unlock()

	barWidth := 30
	filled := 0
	if p.total > 0 {
		filled = (p.done * barWidth) / p.total
	}
	empty := barWidth - filled

	bar := render(stBarFull, repeatStr("█", filled)) +
		render(stBarEmpty, repeatStr("░", empty))

	pct := 0
	if p.total > 0 {
		pct = (p.done * 100) / p.total
	}

	elapsed := time.Since(p.startTime)

	// Build per-repo status inline
	var parts []string
	for i := 0; i < len(p.repos); i++ {
		switch p.states[i] {
		case stateQueued:
			// skip — don't clutter the line
		case stateRunning:
			parts = append(parts, fmt.Sprintf("%s%s", render(stInfo, p.repos[i]), render(stDim, "("+p.phases[i]+")")))
		case stateDone:
			parts = append(parts, render(stSuccess, p.repos[i]+" "+styles.SymbolSuccess))
		case stateFailed:
			parts = append(parts, render(stError, p.repos[i]+" "+styles.SymbolError))
		}
	}

	status := ""
	if len(parts) > 0 {
		status = "  " + strings.Join(parts, " ")
	}

	return fmt.Sprintf("[%d/%d] %s %3d%% %s%s", p.done, p.total, bar, pct, formatDuration(elapsed), status)
}

// renderNonTTY returns a milestone line for non-TTY mode (only called on state changes)
func (p *repoProgress) renderNonTTY(idx int) string {
	p.mu.Lock()
	defer p.mu.Unlock()

	elapsed := time.Since(p.startTime)
	switch p.states[idx] {
	case stateDone:
		return fmt.Sprintf("[%d/%d] %s: done (%s)", p.done, p.total, p.repos[idx], formatDuration(elapsed))
	case stateFailed:
		return fmt.Sprintf("[%d/%d] %s: FAILED - %v (%s)", p.done, p.total, p.repos[idx], p.errors[idx], formatDuration(elapsed))
	case stateRunning:
		return fmt.Sprintf("[%d/%d] %s: started", p.done, p.total, p.repos[idx])
	default:
		return ""
	}
}

func runMultiRepo(args cliArgs) []benchResult {
	total := len(args.repoURLs)

	if !jsonMode {
		fmt.Println()
		fmt.Printf("%s %s\n", render(stAccent.Bold(true), "pgit-bench"), render(stDim, "- multi-repo compression benchmark"))
		fmt.Println(render(stDim, "════════════════════════════════════════════════════════════"))
		fmt.Printf("  Repositories:  %d\n", total)
		fmt.Printf("  Parallel:      %d\n", args.parallel)
		fmt.Printf("  Date:          %s\n", time.Now().Format("2006-01-02 15:04:05"))
		fmt.Println(render(stDim, "════════════════════════════════════════════════════════════"))
		fmt.Println()
	}

	progress := newRepoProgress(args.repoURLs)

	// Start progress display goroutine (TTY only)
	var progressDone chan struct{}
	if !jsonMode && isTTY {
		progressDone = make(chan struct{})
		go func() {
			ticker := time.NewTicker(500 * time.Millisecond)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					// Single-line update: \r\033[K (carriage return + clear line)
					fmt.Printf("\r\033[K%s", progress.renderTTY())
				case <-progressDone:
					// Final render with newline
					fmt.Printf("\r\033[K%s\n", progress.renderTTY())
					return
				}
			}
		}()
	}

	// Worker pool with semaphore
	sem := make(chan struct{}, args.parallel)
	var wg sync.WaitGroup

	for i, repoURL := range args.repoURLs {
		wg.Add(1)
		go func(idx int, url string) {
			defer wg.Done()

			sem <- struct{}{}
			defer func() { <-sem }()

			progress.setRunning(idx, "starting")

			// Non-TTY: print "started" line
			if !jsonMode && !isTTY {
				fmt.Println(progress.renderNonTTY(idx))
			}

			result, err := benchmarkRepo(url, args.branch, false, func(phase string) {
				progress.setPhase(idx, phase)
			})
			if err != nil {
				progress.setFailed(idx, err)
				progress.mu.Lock()
				progress.results[idx] = &benchResult{
					Repo:     url,
					RepoName: extractRepoName(url),
					Error:    err.Error(),
				}
				progress.mu.Unlock()
			} else {
				progress.setDone(idx, result)
			}

			// Non-TTY: print completion/failure line
			if !jsonMode && !isTTY {
				fmt.Println(progress.renderNonTTY(idx))
			}
		}(i, repoURL)
	}

	wg.Wait()

	if !jsonMode && isTTY {
		close(progressDone)
		time.Sleep(100 * time.Millisecond) // let final render complete
	}

	// Collect results
	results := make([]benchResult, total)
	for i := 0; i < total; i++ {
		if progress.results[i] != nil {
			results[i] = *progress.results[i]
		}
	}

	// Print summary table to terminal
	if !jsonMode {
		fmt.Println()
		printSummaryTable(results)
	}

	return results
}

func printSummaryTable(results []benchResult) {
	sectionHeader("Summary")
	fmt.Println()

	fmt.Printf("  %-16s %8s %10s %10s %10s %10s %8s %8s\n",
		"Repository", "Commits", "Raw", "git", "git-aggr", "pgit", "Ratio", "Time")
	fmt.Printf("  %s\n", render(stDim, strings.Repeat("─", 96)))

	for _, r := range results {
		if r.Error != "" {
			fmt.Printf("  %-16s %s\n", truncate(shortName(r.RepoName), 16), render(stError, r.Error))
			continue
		}
		dur, _ := strconv.ParseFloat(r.Duration, 64)
		fmt.Printf("  %-16s %8s %10s %10s %10s %10s %7.1fx %7.0fs\n",
			truncate(shortName(r.RepoName), 16),
			formatCount(r.Commits),
			formatBytes(r.RawUncompressedBytes),
			formatBytes(r.GitNormalPackfileBytes),
			formatBytes(r.GitAggressivePackfileBytes),
			formatBytes(r.PgitActualDataBytes),
			r.CompressionRatioData,
			dur)
	}
	fmt.Println()
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}

// ═══════════════════════════════════════════════════════════════════════════
// JSON output
// ═══════════════════════════════════════════════════════════════════════════

func writeJSONOutput(results []benchResult, path string) {
	var w *os.File
	if path == "" {
		w = os.Stdout
	} else {
		var err error
		w, err = os.Create(path)
		if err != nil {
			fatalMsg("Failed to create JSON file: %v", err)
		}
		defer w.Close()
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")

	if len(results) == 1 {
		_ = enc.Encode(results[0])
	} else {
		_ = enc.Encode(results)
	}
}

// ═══════════════════════════════════════════════════════════════════════════
// Markdown report generation
// ═══════════════════════════════════════════════════════════════════════════

func writeMarkdownReport(path string, results []benchResult) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	w := bufio.NewWriter(f)
	defer w.Flush()

	p := func(format string, args ...any) {
		fmt.Fprintf(w, format+"\n", args...)
	}

	// Filter out failed results for charts/tables
	var good []benchResult
	for _, r := range results {
		if r.Error == "" {
			good = append(good, r)
		}
	}

	// ── Title ──
	p("# pgit-bench Compression Report")
	p("")
	p("**Date:** %s", time.Now().Format("2006-01-02 15:04:05"))
	p("**Repositories:** %d", len(results))
	if len(good) < len(results) {
		p("**Failed:** %d", len(results)-len(good))
	}
	p("")

	// ── Summary table ──
	p("## Summary")
	p("")
	p("| Repository | Commits | Raw Size | git (normal) | git (aggressive) | pgit (on-disk) | pgit (actual) | PG Overhead | Best Ratio | Duration |")
	p("|:-----------|--------:|---------:|-------------:|-----------------:|---------------:|--------------:|------------:|-----------:|---------:|")

	for _, r := range results {
		if r.Error != "" {
			p("| %s | - | - | - | - | - | - | - | - | FAILED: %s |", r.RepoName, r.Error)
			continue
		}
		dur, _ := strconv.ParseFloat(r.Duration, 64)

		bestLabel := fmt.Sprintf("%.1fx (pgit)", r.CompressionRatioData)
		if r.CompressionRatioGitAggressive > r.CompressionRatioData {
			bestLabel = fmt.Sprintf("%.1fx (git)", r.CompressionRatioGitAggressive)
		}

		p("| %s | %s | %s | %s | %s | %s | %s | %s (%.0f%%) | %s | %.0fs |",
			shortName(r.RepoName),
			formatCount(r.Commits),
			formatBytes(r.RawUncompressedBytes),
			formatBytes(r.GitNormalPackfileBytes),
			formatBytes(r.GitAggressivePackfileBytes),
			formatBytes(r.PgitOnDiskBytes),
			formatBytes(r.PgitActualDataBytes),
			formatBytes(r.PgitOverheadBytes), r.OverheadPercent,
			bestLabel,
			dur)
	}
	p("")

	// ── Charts: QuickChart.io grouped bar charts ──
	if len(good) > 0 {
		p("## Compression: git aggressive vs pgit")
		p("")

		// Chart 1: Stored size comparison (MB)
		p("### Stored Size")
		p("")
		sizeChart := buildQuickChart(good,
			"Stored Size (MB) — lower is better",
			func(r benchResult) float64 { return float64(r.GitAggressivePackfileBytes) / (1024 * 1024) },
			func(r benchResult) float64 { return float64(r.PgitActualDataBytes) / (1024 * 1024) },
			"%.1f",
		)
		p("![Stored Size Comparison](%s)", sizeChart)
		p("")

		// Chart 2: Compression ratio comparison
		p("### Compression Ratio")
		p("")
		p("Higher is better (raw uncompressed / stored size).")
		p("")
		ratioChart := buildQuickChart(good,
			"Compression Ratio — higher is better",
			func(r benchResult) float64 { return r.CompressionRatioGitAggressive },
			func(r benchResult) float64 { return r.CompressionRatioData },
			"%.1f",
		)
		p("![Compression Ratio Comparison](%s)", ratioChart)
		p("")
	}

	// ── Per-repo details ──
	p("## Per-Repository Details")
	p("")

	for _, r := range results {
		if r.Error != "" {
			p("### %s", shortName(r.RepoName))
			p("")
			p("**Error:** %s", r.Error)
			p("")
			continue
		}

		p("### %s", shortName(r.RepoName))
		p("")
		p("**URL:** %s  ", r.Repo)
		p("**Branch:** %s  ", r.Branch)
		p("**Commits:** %s | **Files:** %s  ", formatCount(r.Commits), formatCount(r.Files))
		dur, _ := strconv.ParseFloat(r.Duration, 64)
		p("**Duration:** %.0fs  ", dur)
		p("")

		p("| Metric | git (normal) | git (aggressive) | pgit (on-disk) | pgit (actual data) |")
		p("|:-------|-------------:|-----------------:|---------------:|-------------------:|")
		p("| Stored size | %s | %s | %s | %s |",
			formatBytes(r.GitNormalPackfileBytes),
			formatBytes(r.GitAggressivePackfileBytes),
			formatBytes(r.PgitOnDiskBytes),
			formatBytes(r.PgitActualDataBytes))
		p("| Compression ratio | %.1fx | %.1fx | %.1fx | %.1fx |",
			r.CompressionRatioGitNormal,
			r.CompressionRatioGitAggressive,
			r.CompressionRatioOnDisk,
			r.CompressionRatioData)
		p("| vs git normal | - | - | %.2fx | %.2fx |",
			r.RatioVsNormalOnDisk,
			r.RatioVsNormalActualData)
		p("| vs git aggressive | - | - | %.2fx | %.2fx |",
			r.RatioVsAggressiveOnDisk,
			r.RatioVsAggressiveActualData)
		p("")

		p("**PostgreSQL overhead:** %s (%.1f%% of on-disk)",
			formatBytes(r.PgitOverheadBytes), r.OverheadPercent)
		p("")

		if r.CompressionRatioData > r.CompressionRatioGitAggressive {
			improvement := ((r.CompressionRatioData / r.CompressionRatioGitAggressive) - 1) * 100
			p("> **pgit wins** on actual data compression: %.1fx vs git aggressive %.1fx (%.0f%% better ratio)",
				r.CompressionRatioData, r.CompressionRatioGitAggressive, improvement)
		} else {
			improvement := ((r.CompressionRatioGitAggressive / r.CompressionRatioData) - 1) * 100
			p("> **git aggressive wins** on compression: %.1fx vs pgit actual %.1fx (%.0f%% better ratio)",
				r.CompressionRatioGitAggressive, r.CompressionRatioData, improvement)
		}
		p("")
		p("---")
		p("")
	}

	// ── PostgreSQL Overhead Table ──
	p("## PostgreSQL Overhead")
	p("")
	p("| Repository | On-disk | Actual Data | Overhead | Overhead %% |")
	p("|:-----------|--------:|------------:|---------:|-----------:|")

	for _, r := range good {
		p("| %s | %s | %s | %s | %.1f%% |",
			shortName(r.RepoName),
			formatBytes(r.PgitOnDiskBytes),
			formatBytes(r.PgitActualDataBytes),
			formatBytes(r.PgitOverheadBytes),
			r.OverheadPercent)
	}
	p("")

	if len(good) > 1 {
		sorted := make([]benchResult, len(good))
		copy(sorted, good)
		sort.Slice(sorted, func(i, j int) bool {
			return sorted[i].OverheadPercent < sorted[j].OverheadPercent
		})
		p("Overhead ranges from %.1f%% (%s) to %.1f%% (%s).",
			sorted[0].OverheadPercent, shortName(sorted[0].RepoName),
			sorted[len(sorted)-1].OverheadPercent, shortName(sorted[len(sorted)-1].RepoName))
		p("")
	}
	p("PostgreSQL overhead includes: tuple headers (23 bytes/row), TOAST chunk metadata, page headers, alignment padding.")
	p("")

	// ── Methodology ──
	p("## Methodology")
	p("")
	p("### Raw Uncompressed Size")
	p("- `git cat-file --batch-all-objects --batch-check='%%(objecttype) %%(objectsize)'` -- sum of all object sizes")
	p("- Same number used as the numerator for all compression ratios")
	p("")
	p("### Git Storage")
	p("- **Normal:** `git gc` then measure `.pack` file size")
	p("- **Aggressive:** `git gc --aggressive` then measure `.pack` file size")
	p("- Only the packfile is counted (`.idx`, `.rev`, `.bitmap` are reconstructable)")
	p("")
	p("### pgit Storage")
	p("- **On-disk:** `pg_table_size()` sum across all `pgit_*` tables (heap + TOAST, no indexes)")
	p("- **Actual data:** `xpatch.stats()` `compressed_size_bytes` for xpatch tables + `SUM(octet_length(columns))` for normal tables")
	p("- Strips PostgreSQL overhead (tuple headers, TOAST chunk metadata, page headers, alignment)")
	p("- Indexes excluded (reconstructable, same as git)")
	p("")
	p("### Compression Ratio")
	p("- `raw_uncompressed / stored_size` -- same numerator for all methods")
	p("- Higher is better")
	p("")
	p("---")
	p("*Generated by pgit-bench*")

	return nil
}

// buildQuickChart generates a QuickChart.io URL for a grouped bar chart
// comparing git aggressive vs pgit actual data.
func buildQuickChart(results []benchResult, title string,
	gitFn, pgitFn func(benchResult) float64, valueFmt string) string {

	var labels []string
	var gitData []string
	var pgitData []string

	for _, r := range results {
		labels = append(labels, "'"+shortName(r.RepoName)+"'")
		gitData = append(gitData, fmt.Sprintf(valueFmt, gitFn(r)))
		pgitData = append(pgitData, fmt.Sprintf(valueFmt, pgitFn(r)))
	}

	// Build Chart.js config (shorthand syntax supported by QuickChart)
	config := fmt.Sprintf(
		"{type:'bar',data:{labels:[%s],datasets:[{label:'git aggressive',data:[%s],backgroundColor:'#3B82F6'},{label:'pgit actual data',data:[%s],backgroundColor:'#7C3AED'}]},options:{title:{display:true,text:'%s'},plugins:{datalabels:{display:true,anchor:'end',align:'top',font:{size:8}}}}}",
		strings.Join(labels, ","),
		strings.Join(gitData, ","),
		strings.Join(pgitData, ","),
		title,
	)

	return fmt.Sprintf("https://quickchart.io/chart?w=900&h=400&c=%s", url.PathEscape(config))
}

// ═══════════════════════════════════════════════════════════════════════════
// Git operations
// ═══════════════════════════════════════════════════════════════════════════

func detectDefaultBranch(repoURL string) string {
	out, err := runCapture("git", "ls-remote", "--symref", repoURL, "HEAD")
	if err == nil {
		for _, line := range strings.Split(out, "\n") {
			if !strings.HasPrefix(line, "ref:") {
				continue
			}
			tabParts := strings.SplitN(line, "\t", 2)
			if len(tabParts) < 1 {
				continue
			}
			ref := strings.TrimSpace(tabParts[0])
			branch := strings.TrimPrefix(ref, "ref: refs/heads/")
			if branch != ref {
				return branch
			}
		}
	}
	return "main"
}

func extractRepoName(url string) string {
	url = strings.TrimSuffix(url, ".git")
	parts := strings.Split(url, "/")
	if len(parts) >= 2 {
		return parts[len(parts)-2] + "-" + parts[len(parts)-1]
	}
	if len(parts) >= 1 {
		return parts[len(parts)-1]
	}
	return "unknown"
}

func collectGitStats(dir string) gitStats {
	stats := gitStats{}

	out, err := runCaptureInDir(dir, "git", "rev-list", "--count", "HEAD")
	if err == nil {
		stats.commitCount, _ = strconv.ParseInt(strings.TrimSpace(out), 10, 64)
	}

	objectsDir := filepath.Join(dir, ".git", "objects")

	packDir := filepath.Join(objectsDir, "pack")
	entries, err := os.ReadDir(packDir)
	if err == nil {
		for _, e := range entries {
			fi, err := e.Info()
			if err != nil {
				continue
			}
			size := fi.Size()
			switch {
			case strings.HasSuffix(e.Name(), ".pack"):
				stats.packfileBytes += size
			case strings.HasSuffix(e.Name(), ".idx"):
				stats.idxBytes += size
			default:
				stats.otherBytes += size
			}
		}
	}

	for i := 0; i < 256; i++ {
		fanout := filepath.Join(objectsDir, fmt.Sprintf("%02x", i))
		entries, err := os.ReadDir(fanout)
		if err != nil {
			continue
		}
		for _, e := range entries {
			fi, err := e.Info()
			if err != nil {
				continue
			}
			stats.otherBytes += fi.Size()
		}
	}

	infoDir := filepath.Join(objectsDir, "info")
	entries, err = os.ReadDir(infoDir)
	if err == nil {
		for _, e := range entries {
			fi, err := e.Info()
			if err != nil {
				continue
			}
			stats.otherBytes += fi.Size()
		}
	}

	stats.totalBytes = stats.packfileBytes + stats.idxBytes + stats.otherBytes
	return stats
}

func collectGitRawSize(dir string) gitRawSize {
	raw := gitRawSize{}

	out, err := runCaptureInDir(dir, "git", "cat-file", "--batch-all-objects", "--batch-check=%(objecttype) %(objectsize)")
	if err != nil {
		return raw
	}

	scanner := bufio.NewScanner(strings.NewReader(out))
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.Fields(line)
		if len(parts) != 2 {
			continue
		}
		size, err := strconv.ParseInt(parts[1], 10, 64)
		if err != nil {
			continue
		}

		raw.totalBytes += size
		switch parts[0] {
		case "blob":
			raw.blobBytes += size
			raw.blobCount++
		case "tree":
			raw.treeBytes += size
			raw.treeCount++
		case "commit":
			raw.commitBytes += size
			raw.commitCount++
		case "tag":
			raw.tagBytes += size
			raw.tagCount++
		}
	}

	return raw
}

// ═══════════════════════════════════════════════════════════════════════════
// pgit operations
// ═══════════════════════════════════════════════════════════════════════════

func collectPgitStats(dir string) pgitStats {
	stats := pgitStats{}

	out := pgitSQL(dir, `
		SELECT
			c.relname,
			pg_relation_size(c.oid),
			COALESCE(pg_relation_size(c.reltoastrelid), 0),
			pg_table_size(c.oid)
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = 'public'
		  AND c.relkind = 'r'
		  AND c.relname LIKE 'pgit_%'
		ORDER BY pg_table_size(c.oid) DESC
	`)
	for _, line := range nonEmptyLines(out) {
		cols := strings.Split(line, "\t")
		if len(cols) != 4 {
			continue
		}
		t := pgitTableStats{name: cols[0]}
		t.heapBytes, _ = strconv.ParseInt(cols[1], 10, 64)
		t.toastBytes, _ = strconv.ParseInt(cols[2], 10, 64)
		t.totalBytes, _ = strconv.ParseInt(cols[3], 10, 64)
		stats.tables = append(stats.tables, t)
	}

	for _, tableName := range []string{"pgit_text_content", "pgit_commits", "pgit_binary_content"} {
		out = pgitSQL(dir, fmt.Sprintf(`
			SELECT
				total_rows,
				total_groups,
				keyframe_count,
				delta_count,
				raw_size_bytes,
				compressed_size_bytes,
				compression_ratio::float8,
				avg_compression_depth::float8
			FROM xpatch.stats('%s'::regclass)
		`, tableName))
		for _, line := range nonEmptyLines(out) {
			cols := strings.Split(line, "\t")
			if len(cols) != 8 {
				continue
			}
			x := pgitXpatchStats{tableName: tableName}
			x.totalRows, _ = strconv.ParseInt(cols[0], 10, 64)
			x.totalGroups, _ = strconv.ParseInt(cols[1], 10, 64)
			x.keyframeCount, _ = strconv.ParseInt(cols[2], 10, 64)
			x.deltaCount, _ = strconv.ParseInt(cols[3], 10, 64)
			x.rawSizeBytes, _ = strconv.ParseInt(cols[4], 10, 64)
			x.compressedBytes, _ = strconv.ParseInt(cols[5], 10, 64)
			x.compressionRatio, _ = strconv.ParseFloat(cols[6], 64)
			x.avgChainLength, _ = strconv.ParseFloat(cols[7], 64)
			stats.xpatch = append(stats.xpatch, x)
		}
	}

	// Normal table: pgit_file_refs
	out = pgitSQL(dir, `
		SELECT
			COUNT(*),
			SUM(
				octet_length(commit_id) + 4 + 4 +
				octet_length(content_hash) + 4 + 1 + 1 +
				COALESCE(octet_length(symlink_target), 0)
			)
		FROM pgit_file_refs
	`)
	for _, line := range nonEmptyLines(out) {
		cols := strings.Split(line, "\t")
		if len(cols) != 2 {
			continue
		}
		n := pgitNormalTableStats{tableName: "pgit_file_refs"}
		n.rowCount, _ = strconv.ParseInt(cols[0], 10, 64)
		n.rawDataBytes, _ = strconv.ParseInt(cols[1], 10, 64)
		for _, t := range stats.tables {
			if t.name == "pgit_file_refs" {
				n.totalBytes = t.totalBytes
				break
			}
		}
		stats.normalTables = append(stats.normalTables, n)
	}

	// Normal table: pgit_paths
	out = pgitSQL(dir, `
		SELECT COUNT(*), SUM(4 + octet_length(path)) FROM pgit_paths
	`)
	for _, line := range nonEmptyLines(out) {
		cols := strings.Split(line, "\t")
		if len(cols) != 2 {
			continue
		}
		n := pgitNormalTableStats{tableName: "pgit_paths"}
		n.rowCount, _ = strconv.ParseInt(cols[0], 10, 64)
		n.rawDataBytes, _ = strconv.ParseInt(cols[1], 10, 64)
		for _, t := range stats.tables {
			if t.name == "pgit_paths" {
				n.totalBytes = t.totalBytes
				break
			}
		}
		stats.normalTables = append(stats.normalTables, n)
	}

	// Normal table: pgit_refs
	out = pgitSQL(dir, `
		SELECT COUNT(*), SUM(octet_length(name) + octet_length(commit_id)) FROM pgit_refs
	`)
	for _, line := range nonEmptyLines(out) {
		cols := strings.Split(line, "\t")
		if len(cols) != 2 {
			continue
		}
		n := pgitNormalTableStats{tableName: "pgit_refs"}
		n.rowCount, _ = strconv.ParseInt(cols[0], 10, 64)
		rawBytes := cols[1]
		if rawBytes == "" {
			n.rawDataBytes = 0
		} else {
			n.rawDataBytes, _ = strconv.ParseInt(rawBytes, 10, 64)
		}
		for _, t := range stats.tables {
			if t.name == "pgit_refs" {
				n.totalBytes = t.totalBytes
				break
			}
		}
		stats.normalTables = append(stats.normalTables, n)
	}

	// Normal table: pgit_metadata
	out = pgitSQL(dir, `
		SELECT COUNT(*), SUM(octet_length(key) + octet_length(value)) FROM pgit_metadata
	`)
	for _, line := range nonEmptyLines(out) {
		cols := strings.Split(line, "\t")
		if len(cols) != 2 {
			continue
		}
		n := pgitNormalTableStats{tableName: "pgit_metadata"}
		n.rowCount, _ = strconv.ParseInt(cols[0], 10, 64)
		rawBytes := cols[1]
		if rawBytes == "" {
			n.rawDataBytes = 0
		} else {
			n.rawDataBytes, _ = strconv.ParseInt(rawBytes, 10, 64)
		}
		for _, t := range stats.tables {
			if t.name == "pgit_metadata" {
				n.totalBytes = t.totalBytes
				break
			}
		}
		stats.normalTables = append(stats.normalTables, n)
	}

	// Index size
	out = pgitSQL(dir, `
		SELECT COALESCE(SUM(pg_relation_size(indexrelid)), 0)::bigint
		FROM pg_index
		WHERE indrelid IN (
			'pgit_commits'::regclass,
			'pgit_paths'::regclass,
			'pgit_file_refs'::regclass,
			'pgit_text_content'::regclass,
			'pgit_binary_content'::regclass,
			'pgit_refs'::regclass,
			'pgit_sync_state'::regclass
		)
	`)
	for _, line := range nonEmptyLines(out) {
		stats.indexBytes, _ = strconv.ParseInt(strings.TrimSpace(line), 10, 64)
	}

	// Counts
	out = pgitSQL(dir, `SELECT total_rows FROM xpatch.stats('pgit_commits'::regclass)`)
	for _, line := range nonEmptyLines(out) {
		stats.commitCount, _ = strconv.ParseInt(strings.TrimSpace(line), 10, 64)
	}

	out = pgitSQL(dir, `SELECT COUNT(*) FROM pgit_paths`)
	for _, line := range nonEmptyLines(out) {
		stats.fileCount, _ = strconv.ParseInt(strings.TrimSpace(line), 10, 64)
	}

	return stats
}

func pgitSQL(dir, query string) string {
	out, err := runCaptureInDir(dir, "pgit", "sql", query, "--raw", "--timeout", "120")
	if err != nil {
		return ""
	}
	return out
}

// ═══════════════════════════════════════════════════════════════════════════
// Display: git stats (interactive single-repo mode)
// ═══════════════════════════════════════════════════════════════════════════

func printGitStats(label string, s gitStats) {
	fmt.Printf("  %s\n", render(stDim, label))
	fmt.Printf("    Commits:         %s\n", formatCount(s.commitCount))
	fmt.Printf("    Packfile:        %s\n", formatBytes(s.packfileBytes))
	fmt.Printf("    Index (.idx):    %s\n", formatBytes(s.idxBytes))
	if s.otherBytes > 0 {
		fmt.Printf("    Other:           %s  %s\n", formatBytes(s.otherBytes), render(stDim, "(.rev, .bitmap, loose)"))
	}
	fmt.Printf("    %s\n", render(stDim, "───────────────────"))
	fmt.Printf("    Total:           %s  %s\n", render(stBold, formatBytes(s.totalBytes)), render(stDim, "(.git/objects)"))
	fmt.Printf("    Essential:       %s  %s\n", render(stBold, formatBytes(s.packfileBytes)), render(stDim, "(packfile only)"))
	fmt.Println()
}

// ═══════════════════════════════════════════════════════════════════════════
// Display: pgit stats (interactive single-repo mode)
// ═══════════════════════════════════════════════════════════════════════════

func printPgitStats(s pgitStats) {
	fmt.Printf("  %s\n", render(stDim, fmt.Sprintf("Commits: %s  |  Files: %s", formatCount(s.commitCount), formatCount(s.fileCount))))
	fmt.Println()

	fmt.Printf("  %s\n", render(stDim, "On-disk (pg_table_size, no indexes):"))
	for _, t := range s.tables {
		if t.totalBytes == 0 {
			continue
		}
		toast := ""
		if t.toastBytes > 0 {
			toast = fmt.Sprintf("  %s", render(stDim, fmt.Sprintf("(heap: %s + toast: %s)", formatBytes(t.heapBytes), formatBytes(t.toastBytes))))
		}
		fmt.Printf("    %-22s %10s%s\n", t.name, formatBytes(t.totalBytes), toast)
	}
	fmt.Printf("    %-22s %10s\n", "indexes", formatBytes(s.indexBytes))
	fmt.Printf("    %s\n", render(stDim, "──────────────────────────────"))
	fmt.Printf("    %-22s %10s  %s\n", "Total (no indexes)", render(stBold, formatBytes(s.totalOnDisk())), render(stDim, "(what you pay)"))
	fmt.Printf("    %-22s %10s  %s\n", "Total (with indexes)", render(stBold, formatBytes(s.totalOnDisk()+s.indexBytes)), render(stDim, "(full footprint)"))
	fmt.Println()

	fmt.Printf("  %s\n", render(stDim, "xpatch compression:"))
	for _, x := range s.xpatch {
		sn := strings.TrimPrefix(x.tableName, "pgit_")
		fmt.Printf("    %s  rows: %-8s  groups: %-6s  raw: %-10s  compressed: %-10s  ratio: %.1fx\n",
			render(stDim, fmt.Sprintf("%-18s", sn)),
			formatCount(x.totalRows), formatCount(x.totalGroups),
			formatBytes(x.rawSizeBytes), formatBytes(x.compressedBytes),
			x.compressionRatio)
	}
	fmt.Println()

	fmt.Printf("  %s\n", render(stDim, "Normal tables (uncompressed column data):"))
	for _, n := range s.normalTables {
		if n.rawDataBytes == 0 && n.rowCount == 0 {
			continue
		}
		sn := strings.TrimPrefix(n.tableName, "pgit_")
		overheadPct := float64(0)
		if n.totalBytes > 0 && n.rawDataBytes > 0 {
			overheadPct = float64(n.totalBytes-n.rawDataBytes) / float64(n.totalBytes) * 100
		}
		fmt.Printf("    %-18s  rows: %-8s  data: %-10s  on-disk: %-10s  overhead: %.0f%%\n",
			sn,
			formatCount(n.rowCount), formatBytes(n.rawDataBytes),
			formatBytes(n.totalBytes), overheadPct)
	}
	fmt.Println()

	fmt.Printf("  %s\n", render(stDim, "Actual data (compressed xpatch + raw normal):"))
	var xpatchCompTotal int64
	for _, x := range s.xpatch {
		xpatchCompTotal += x.compressedBytes
	}
	var normalRawTotal int64
	for _, n := range s.normalTables {
		normalRawTotal += n.rawDataBytes
	}
	fmt.Printf("    xpatch compressed:   %10s\n", formatBytes(xpatchCompTotal))
	fmt.Printf("    normal table data:   %10s\n", formatBytes(normalRawTotal))
	fmt.Printf("    %s\n", render(stDim, "──────────────────────────────"))
	fmt.Printf("    %-22s %10s\n", "Total actual data", render(stBold, formatBytes(s.totalActualData())))
	fmt.Printf("    %-22s %10s  %s\n", "PG overhead",
		render(stWarning, formatBytes(s.totalOverhead())),
		render(stDim, fmt.Sprintf("(%.1f%% of on-disk)", float64(s.totalOverhead())/float64(s.totalOnDisk())*100)))
	fmt.Println()
}

// ═══════════════════════════════════════════════════════════════════════════
// Display: comparison (interactive single-repo mode)
// ═══════════════════════════════════════════════════════════════════════════

func printComparison(gitNormal, gitAggressive gitStats, gitRaw gitRawSize, pgit pgitStats) {
	pgitOnDisk := pgit.totalOnDisk()
	pgitActual := pgit.totalActualData()
	gitNormalPack := gitNormal.packfileBytes
	gitAggressivePack := gitAggressive.packfileBytes
	rawUncompressed := gitRaw.totalBytes

	sectionHeader("Head-to-head")
	fmt.Printf("  %s\n", render(stDim, fmt.Sprintf("Repository contains %s of uncompressed data", render(stBold, formatBytes(rawUncompressed)))))
	fmt.Println()

	fmt.Printf("  %-26s %12s %16s %12s\n",
		"", "git (normal)", "git (aggressive)", "pgit")
	fmt.Printf("  %s\n", render(stDim, "──────────────────────────────────────────────────────────────────────"))

	fmt.Printf("  %-26s %12s %16s %12s  %s\n",
		"Stored on disk",
		formatBytes(gitNormalPack),
		formatBytes(gitAggressivePack),
		formatBytes(pgitOnDisk),
		render(stDim, "(packfile vs pg_table_size)"))

	fmt.Printf("  %-26s %12s %16s %12s  %s\n",
		"Actual data",
		formatBytes(gitNormalPack),
		formatBytes(gitAggressivePack),
		formatBytes(pgitActual),
		render(stDim, "(minus PG overhead)"))

	fmt.Printf("  %s\n", render(stDim, "──────────────────────────────────────────────────────────────────────"))

	gitNormalRatio := safeRatio(rawUncompressed, gitNormalPack)
	gitAggressiveRatio := safeRatio(rawUncompressed, gitAggressivePack)
	pgitOnDiskRatio := safeRatio(rawUncompressed, pgitOnDisk)
	pgitActualRatio := safeRatio(rawUncompressed, pgitActual)

	fmt.Printf("  %-26s %11.1fx %15.1fx %11.1fx  %s\n",
		"Compression ratio",
		gitNormalRatio,
		gitAggressiveRatio,
		pgitOnDiskRatio,
		render(stDim, "(raw / stored)"))

	fmt.Printf("  %-26s %12s %16s %11.1fx  %s\n",
		"Compression ratio (data)",
		"", "",
		pgitActualRatio,
		render(stDim, "(raw / actual data)"))

	fmt.Println()

	sectionHeader("Relative size")
	fmt.Println()

	comparePair := func(gitLabel string, gitBytes, pgitBytes int64) {
		ratio := float64(pgitBytes) / float64(gitBytes)
		var indicator string
		if ratio < 1.0 {
			pctSmaller := (1.0 - ratio) * 100
			indicator = render(stSuccess, fmt.Sprintf("%.0f%% smaller", pctSmaller))
		} else if ratio > 1.0 {
			pctLarger := (ratio - 1.0) * 100
			indicator = render(stError, fmt.Sprintf("%.0f%% larger", pctLarger))
		} else {
			indicator = "identical"
		}
		fmt.Printf("  pgit vs %-24s  %s / %s = %s  (%s)\n",
			gitLabel,
			formatBytes(pgitBytes), formatBytes(gitBytes),
			render(stBold, fmt.Sprintf("%.2fx", ratio)),
			indicator)
	}

	comparePair("git normal (on-disk)", gitNormalPack, pgitOnDisk)
	comparePair("git aggressive (on-disk)", gitAggressivePack, pgitOnDisk)
	comparePair("git normal (actual data)", gitNormalPack, pgitActual)
	comparePair("git aggressive (actual data)", gitAggressivePack, pgitActual)
	fmt.Println()

	sectionHeader("PostgreSQL overhead")
	fmt.Printf("  Total overhead:    %s  (%.1f%% of on-disk)\n",
		render(stWarning, formatBytes(pgit.totalOverhead())),
		float64(pgit.totalOverhead())/float64(pgitOnDisk)*100)
	fmt.Printf("  This includes:     tuple headers, TOAST chunk metadata, page headers, alignment\n")
	fmt.Printf("  Could be reduced:  by pg-xpatch improvements (custom large-value storage)\n")
	fmt.Println()
}

// ═══════════════════════════════════════════════════════════════════════════
// Logger
// ═══════════════════════════════════════════════════════════════════════════

type logger struct {
	f *os.File
}

func newLogger(f *os.File) *logger {
	return &logger{f: f}
}

func (l *logger) write(format string, args ...any) {
	fmt.Fprintf(l.f, format+"\n", args...)
}

func (l *logger) header(repoURL, repoName, gitDir, pgitDir string) {
	l.write("════════════════════════════════════════════════════════════════")
	l.write("pgit-bench compression benchmark")
	l.write("Date:      %s", time.Now().Format(time.RFC3339))
	l.write("Repo:      %s", repoURL)
	l.write("Name:      %s", repoName)
	l.write("Git dir:   %s", gitDir)
	l.write("pgit dir:  %s", pgitDir)
	l.write("════════════════════════════════════════════════════════════════")
	l.write("")

	meta := map[string]string{
		"repo_url":  repoURL,
		"repo_name": repoName,
		"git_dir":   gitDir,
		"pgit_dir":  pgitDir,
		"timestamp": time.Now().Format(time.RFC3339),
	}
	jsonBytes, _ := json.MarshalIndent(meta, "", "  ")
	l.write("META_JSON_START")
	l.write(string(jsonBytes))
	l.write("META_JSON_END")
	l.write("")
}

func (l *logger) phase(num, title string) {
	l.write("── Phase %s: %s ──", num, title)
}

func (l *logger) info(format string, args ...any) {
	l.write("  "+format, args...)
}

func (l *logger) gitStats(label string, s gitStats) {
	l.write("  [%s]", label)
	l.write("    commits:       %d", s.commitCount)
	l.write("    packfile:      %d bytes (%s)", s.packfileBytes, formatBytes(s.packfileBytes))
	l.write("    idx:           %d bytes (%s)", s.idxBytes, formatBytes(s.idxBytes))
	l.write("    other:         %d bytes (%s)", s.otherBytes, formatBytes(s.otherBytes))
	l.write("    total:         %d bytes (%s)", s.totalBytes, formatBytes(s.totalBytes))
	l.write("    essential:     %d bytes (%s)", s.packfileBytes, formatBytes(s.packfileBytes))
}

func (l *logger) pgitStats(s pgitStats) {
	l.write("  commits: %d", s.commitCount)
	l.write("  files:   %d", s.fileCount)
	l.write("")

	l.write("  [On-disk table sizes]")
	for _, t := range s.tables {
		l.write("    %-22s  total: %12d  heap: %12d  toast: %12d", t.name, t.totalBytes, t.heapBytes, t.toastBytes)
	}
	l.write("    %-22s  total: %12d", "indexes", s.indexBytes)
	l.write("    %-22s  total: %12d", "TOTAL (no indexes)", s.totalOnDisk())
	l.write("")

	l.write("  [xpatch compression]")
	for _, x := range s.xpatch {
		l.write("    %-22s  rows: %8d  groups: %6d  raw: %12d  compressed: %12d  ratio: %.2f",
			x.tableName, x.totalRows, x.totalGroups, x.rawSizeBytes, x.compressedBytes, x.compressionRatio)
	}
	l.write("")

	l.write("  [Normal tables]")
	for _, n := range s.normalTables {
		l.write("    %-22s  rows: %8d  raw_data: %12d  on_disk: %12d",
			n.tableName, n.rowCount, n.rawDataBytes, n.totalBytes)
	}
	l.write("")

	l.write("  [Summary]")
	l.write("    total_on_disk:    %d bytes (%s)", s.totalOnDisk(), formatBytes(s.totalOnDisk()))
	l.write("    total_actual:     %d bytes (%s)", s.totalActualData(), formatBytes(s.totalActualData()))
	l.write("    total_overhead:   %d bytes (%s) (%.1f%%)", s.totalOverhead(), formatBytes(s.totalOverhead()),
		float64(s.totalOverhead())/float64(s.totalOnDisk())*100)

	type jsonPgit struct {
		Commits       int64 `json:"commits"`
		Files         int64 `json:"files"`
		OnDiskBytes   int64 `json:"on_disk_bytes"`
		ActualBytes   int64 `json:"actual_data_bytes"`
		OverheadBytes int64 `json:"overhead_bytes"`
		IndexBytes    int64 `json:"index_bytes"`
		RawBytes      int64 `json:"raw_uncompressed_bytes"`
	}
	jp := jsonPgit{
		Commits:       s.commitCount,
		Files:         s.fileCount,
		OnDiskBytes:   s.totalOnDisk(),
		ActualBytes:   s.totalActualData(),
		OverheadBytes: s.totalOverhead(),
		IndexBytes:    s.indexBytes,
		RawBytes:      s.totalRawUncompressed(),
	}
	jsonBytes, _ := json.MarshalIndent(jp, "  ", "  ")
	l.write("")
	l.write("PGIT_JSON_START")
	l.write("  " + string(jsonBytes))
	l.write("PGIT_JSON_END")
}

func (l *logger) comparison(gitNormal, gitAggressive gitStats, gitRaw gitRawSize, pgit pgitStats) {
	pgitOnDisk := pgit.totalOnDisk()
	pgitActual := pgit.totalActualData()
	rawUncompressed := gitRaw.totalBytes

	l.write("  [Comparison]")
	l.write("    raw_uncompressed:          %12d bytes (%s)", rawUncompressed, formatBytes(rawUncompressed))
	l.write("    git_normal_packfile:       %12d bytes (%s)", gitNormal.packfileBytes, formatBytes(gitNormal.packfileBytes))
	l.write("    git_aggressive_packfile:   %12d bytes (%s)", gitAggressive.packfileBytes, formatBytes(gitAggressive.packfileBytes))
	l.write("    pgit_on_disk:              %12d bytes (%s)", pgitOnDisk, formatBytes(pgitOnDisk))
	l.write("    pgit_actual_data:          %12d bytes (%s)", pgitActual, formatBytes(pgitActual))
	l.write("    pgit_overhead:             %12d bytes (%s)", pgit.totalOverhead(), formatBytes(pgit.totalOverhead()))
	l.write("")

	l.write("  [Git raw breakdown]")
	l.write("    blobs:     %12d bytes (%s) [%d objects]", gitRaw.blobBytes, formatBytes(gitRaw.blobBytes), gitRaw.blobCount)
	l.write("    trees:     %12d bytes (%s) [%d objects]", gitRaw.treeBytes, formatBytes(gitRaw.treeBytes), gitRaw.treeCount)
	l.write("    commits:   %12d bytes (%s) [%d objects]", gitRaw.commitBytes, formatBytes(gitRaw.commitBytes), gitRaw.commitCount)
	l.write("    tags:      %12d bytes (%s) [%d objects]", gitRaw.tagBytes, formatBytes(gitRaw.tagBytes), gitRaw.tagCount)
	l.write("")

	ratioNormal := float64(pgitOnDisk) / float64(gitNormal.packfileBytes)
	ratioAggressive := float64(pgitOnDisk) / float64(gitAggressive.packfileBytes)
	ratioNormalActual := float64(pgitActual) / float64(gitNormal.packfileBytes)
	ratioAggressiveActual := float64(pgitActual) / float64(gitAggressive.packfileBytes)

	l.write("  [Ratios - pgit / git]")
	l.write("    vs_normal_on_disk:       %.4f (pgit is %.1f%% %s)", ratioNormal, absF((ratioNormal-1)*100), largerSmaller(ratioNormal))
	l.write("    vs_aggressive_on_disk:   %.4f (pgit is %.1f%% %s)", ratioAggressive, absF((ratioAggressive-1)*100), largerSmaller(ratioAggressive))
	l.write("    vs_normal_actual:        %.4f (pgit is %.1f%% %s)", ratioNormalActual, absF((ratioNormalActual-1)*100), largerSmaller(ratioNormalActual))
	l.write("    vs_aggressive_actual:    %.4f (pgit is %.1f%% %s)", ratioAggressiveActual, absF((ratioAggressiveActual-1)*100), largerSmaller(ratioAggressiveActual))

	type jsonComp struct {
		RawUncompressed   int64   `json:"raw_uncompressed_bytes"`
		GitNormalPack     int64   `json:"git_normal_packfile_bytes"`
		GitAggressivePack int64   `json:"git_aggressive_packfile_bytes"`
		PgitOnDisk        int64   `json:"pgit_on_disk_bytes"`
		PgitActual        int64   `json:"pgit_actual_data_bytes"`
		PgitOverhead      int64   `json:"pgit_overhead_bytes"`
		RatioVsNormal     float64 `json:"ratio_vs_normal_on_disk"`
		RatioVsAggressive float64 `json:"ratio_vs_aggressive_on_disk"`
		RatioVsNormalData float64 `json:"ratio_vs_normal_actual_data"`
		RatioVsAggrData   float64 `json:"ratio_vs_aggressive_actual_data"`
	}
	jc := jsonComp{
		RawUncompressed:   rawUncompressed,
		GitNormalPack:     gitNormal.packfileBytes,
		GitAggressivePack: gitAggressive.packfileBytes,
		PgitOnDisk:        pgitOnDisk,
		PgitActual:        pgitActual,
		PgitOverhead:      pgit.totalOverhead(),
		RatioVsNormal:     ratioNormal,
		RatioVsAggressive: ratioAggressive,
		RatioVsNormalData: ratioNormalActual,
		RatioVsAggrData:   ratioAggressiveActual,
	}
	jsonBytes, _ := json.MarshalIndent(jc, "  ", "  ")
	l.write("")
	l.write("COMPARISON_JSON_START")
	l.write("  " + string(jsonBytes))
	l.write("COMPARISON_JSON_END")
}

// ═══════════════════════════════════════════════════════════════════════════
// Shell execution
// ═══════════════════════════════════════════════════════════════════════════

func runQuietErr(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg != "" {
			return fmt.Errorf("%w: %s", err, lastLine(msg))
		}
		return err
	}
	return nil
}

func runQuietInDirErr(dir, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg != "" {
			return fmt.Errorf("%w: %s", err, lastLine(msg))
		}
		return err
	}
	return nil
}

// lastLine returns the last non-empty line from a string (typically the real error message)
func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line != "" {
			return line
		}
	}
	return s
}

func runInDirPassthrough(dir, name string, args ...string) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		fatalMsg("Command failed in %s: %s %s\n%v", dir, name, strings.Join(args, " "), err)
	}
}

func runCapture(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	out, err := cmd.Output()
	return string(out), err
}

func runCaptureInDir(dir, name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

// ═══════════════════════════════════════════════════════════════════════════
// Formatting helpers
// ═══════════════════════════════════════════════════════════════════════════

func formatBytes(bytes int64) string {
	if bytes < 0 {
		return "0 B"
	}
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

func formatCount(n int64) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	if n < 1000000 {
		return fmt.Sprintf("%d,%03d", n/1000, n%1000)
	}
	return fmt.Sprintf("%d,%03d,%03d", n/1000000, (n/1000)%1000, n%1000)
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%.1fs", d.Seconds())
	}
	if d < time.Hour {
		m := int(d.Minutes())
		s := int(d.Seconds()) % 60
		return fmt.Sprintf("%dm%ds", m, s)
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	return fmt.Sprintf("%dh%dm", h, m)
}

func randomSuffix() string {
	const chars = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, 8)
	for i := range b {
		b[i] = chars[rand.Intn(len(chars))]
	}
	return string(b)
}

func nonEmptyLines(s string) []string {
	var result []string
	for _, line := range strings.Split(strings.TrimSpace(s), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			result = append(result, line)
		}
	}
	return result
}

func repeatStr(s string, n int) string {
	if n <= 0 {
		return ""
	}
	result := make([]byte, len(s)*n)
	for i := 0; i < n; i++ {
		copy(result[i*len(s):], s)
	}
	return string(result)
}

func absF(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}

func largerSmaller(ratio float64) string {
	if ratio > 1.0 {
		return "larger"
	}
	return "smaller"
}

func safeRatio(numerator, denominator int64) float64 {
	if denominator <= 0 {
		return 0
	}
	return float64(numerator) / float64(denominator)
}

func safeRatioF(numerator, denominator int64) float64 {
	if denominator <= 0 {
		return 0
	}
	return float64(numerator) / float64(denominator)
}

func safePercent(part, whole int64) float64 {
	if whole <= 0 {
		return 0
	}
	return float64(part) / float64(whole) * 100
}

// ═══════════════════════════════════════════════════════════════════════════
// Terminal output helpers — using lipgloss + pgit styles
// ═══════════════════════════════════════════════════════════════════════════

func printHeader(repoURL, repoName string) {
	fmt.Println()
	fmt.Printf("%s %s\n", render(stAccent.Bold(true), "pgit-bench"), render(stDim, "- compression benchmark"))
	fmt.Println(render(stDim, "════════════════════════════════════════════════════════════"))
	fmt.Printf("  Repository:  %s\n", repoURL)
	fmt.Printf("  Name:        %s\n", repoName)
	fmt.Printf("  Date:        %s\n", time.Now().Format("2006-01-02 15:04:05"))
	fmt.Println(render(stDim, "════════════════════════════════════════════════════════════"))
	fmt.Println()
}

func phaseHeader(num, title string) {
	fmt.Printf("\n%s Phase %s: %s %s\n\n",
		render(stAccent, "──"),
		render(stBold, num),
		render(stBold, title),
		render(stAccent, "──"))
}

func sectionHeader(title string) {
	fmt.Printf("  %s\n", render(stBold, title))
}

func infoMsg(format string, args ...any) {
	fmt.Printf("  %s\n", fmt.Sprintf(format, args...))
}

func successMsg(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	fmt.Printf("  %s\n", styles.SuccessMsg(msg))
}

func fatalMsg(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintf(os.Stderr, "\n  %s\n\n", styles.ErrorMsg(msg))
	os.Exit(1)
}
