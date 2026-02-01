package cli

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/imgajeed76/pgit/internal/config"
	"github.com/imgajeed76/pgit/internal/db"
	"github.com/imgajeed76/pgit/internal/repo"
	"github.com/imgajeed76/pgit/internal/ui"
	"github.com/imgajeed76/pgit/internal/ui/styles"
	"github.com/imgajeed76/pgit/internal/util"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func newImportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "import [git-repo-path]",
		Short: "Import a git repository into pgit",
		Long: `Import an existing git repository into pgit.

This command extracts the commit history and file contents from a git
repository and stores them in the pgit database with delta compression.

By default, imports the current branch. Use --branch to specify a different
branch, or an interactive picker will be shown if multiple branches exist.

The import uses a streaming architecture with progress visualization
showing git extraction and pgit import rates separately.

The current directory must be a pgit repository (run 'pgit init' first).`,
		Args: cobra.MaximumNArgs(1),
		RunE: runImport,
	}

	cmd.Flags().IntP("workers", "w", 0, "Number of parallel workers (default 4, max 16)")
	cmd.Flags().BoolP("dry-run", "n", false, "Show what would be imported without actually importing")
	cmd.Flags().BoolP("force", "f", false, "Overwrite existing data in database")
	cmd.Flags().StringP("branch", "b", "", "Branch to import (default: current branch, or interactive picker)")

	return cmd
}

// ═══════════════════════════════════════════════════════════════════════════
// Data structures
// ═══════════════════════════════════════════════════════════════════════════

// gitCommitInfo contains commit metadata from git log --raw
type gitCommitInfo struct {
	Hash        string
	ParentHash  string
	AuthorName  string
	AuthorEmail string
	AuthorDate  time.Time
	Message     string
}

// gitFileChange represents a file change in a commit
type gitFileChange struct {
	CommitHash string
	Path       string
	BlobHash   string
	Mode       int
	ChangeType byte // 'A'dd, 'M'odify, 'D'elete
}

// blobWork represents work for the blob importer
type blobWork struct {
	Path      string
	CommitID  string // pgit ULID
	BlobHash  string
	Mode      int
	IsSymlink bool
	IsDeleted bool // true for deletions (no content to fetch)
}

// ═══════════════════════════════════════════════════════════════════════════
// Main import function
// ═══════════════════════════════════════════════════════════════════════════

func runImport(cmd *cobra.Command, args []string) error {
	// Open pgit repository
	r, err := repo.Open()
	if err != nil {
		return util.NotARepoError()
	}

	// Determine git repo path
	gitPath := "."
	if len(args) > 0 {
		gitPath = args[0]
	}

	gitPath, err = filepath.Abs(gitPath)
	if err != nil {
		return err
	}

	// Check if it's a git repo
	gitDir := filepath.Join(gitPath, ".git")
	if _, err := os.Stat(gitDir); os.IsNotExist(err) {
		return util.NewError("Not a git repository").
			WithContext(fmt.Sprintf("No .git directory found in '%s'", gitPath)).
			WithSuggestion("pgit import /path/to/git/repo")
	}

	// Get flags
	workers, _ := cmd.Flags().GetInt("workers")
	if workers <= 0 {
		// Use global config default, or fall back to 4
		if globalCfg, err := config.LoadGlobal(); err == nil && globalCfg.Import.Workers > 0 {
			workers = globalCfg.Import.Workers
		} else {
			workers = 4
		}
	}
	// Cap at 16 (pg-xpatch insert cache slot limit)
	if workers > 16 {
		workers = 16
	}

	dryRun, _ := cmd.Flags().GetBool("dry-run")
	force, _ := cmd.Flags().GetBool("force")

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Hour)
	defer cancel()

	// Connect to database
	if err := r.StartContainer(); err != nil {
		return err
	}
	if err := r.Connect(ctx); err != nil {
		return err
	}
	defer r.Close()

	fmt.Printf("Importing from: %s\n", styles.Cyan(gitPath))
	fmt.Printf("Workers: %d\n", workers)

	if dryRun {
		fmt.Println(styles.Yellow("Dry run mode - no changes will be made"))
	}

	// Check if database already has commits
	var existingCommits int
	_ = r.DB.QueryRow(ctx, "SELECT COUNT(*) FROM pgit_commits").Scan(&existingCommits)
	if existingCommits > 0 && !force {
		return util.NewError("Database not empty").
			WithMessage(fmt.Sprintf("Database already contains %d commits", existingCommits)).
			WithSuggestion("pgit import --force  # Overwrite existing data")
	}

	// Determine branch
	branchFlag, _ := cmd.Flags().GetString("branch")
	selectedBranch, err := selectBranch(gitPath, branchFlag)
	if err != nil {
		return err
	}
	fmt.Printf("Branch: %s\n", styles.Branch(selectedBranch))

	// ═══════════════════════════════════════════════════════════════════════
	// Step 1: Parse git history with single command
	// ═══════════════════════════════════════════════════════════════════════

	spinner := ui.NewSpinner("Parsing git history")
	spinner.Start()

	commits, fileChanges, err := parseGitHistory(gitPath, selectedBranch)
	spinner.Stop()

	if err != nil {
		return fmt.Errorf("failed to parse git history: %w", err)
	}

	fmt.Printf("Found %s commits, %s file changes\n",
		ui.FormatCount(len(commits)),
		ui.FormatCount(len(fileChanges)))

	if len(commits) == 0 {
		fmt.Println("No commits to import")
		return nil
	}

	if dryRun {
		fmt.Println("\nDry run complete.")
		return nil
	}

	// Clear existing data if --force
	if existingCommits > 0 && force {
		fmt.Println("\nClearing existing data...")
		if err := r.DB.DropSchema(ctx); err != nil {
			return fmt.Errorf("failed to clear database: %w", err)
		}
		if err := r.DB.InitSchema(ctx); err != nil {
			return fmt.Errorf("failed to reinit database: %w", err)
		}
	}

	// ═══════════════════════════════════════════════════════════════════════
	// Step 2: Create commit mapping and prepare pgit commits
	// ═══════════════════════════════════════════════════════════════════════

	fmt.Println("\nPreparing commits...")

	commitMap := make(map[string]string) // git hash -> pgit ULID
	pgitCommits := make([]*db.Commit, len(commits))

	var lastTime time.Time
	for i, gc := range commits {
		commitTime := gc.AuthorDate
		if !commitTime.After(lastTime) {
			commitTime = lastTime.Add(time.Millisecond)
		}
		lastTime = commitTime

		ulid := util.NewULIDWithTime(commitTime)
		commitMap[gc.Hash] = ulid

		var parentID *string
		if gc.ParentHash != "" {
			if pid, ok := commitMap[gc.ParentHash]; ok {
				parentID = &pid
			}
		}

		pgitCommits[i] = &db.Commit{
			ID:          ulid,
			ParentID:    parentID,
			TreeHash:    gc.Hash[:8],
			Message:     util.ToValidUTF8(gc.Message),
			AuthorName:  util.ToValidUTF8(gc.AuthorName),
			AuthorEmail: util.ToValidUTF8(gc.AuthorEmail),
			CreatedAt:   commitTime,
		}
	}

	// ═══════════════════════════════════════════════════════════════════════
	// Step 3: Insert commits (batch for speed)
	// ═══════════════════════════════════════════════════════════════════════

	fmt.Println("\nImporting commits...")

	// Use batch insert with COPY for speed
	batchSize := 1000
	commitProgress := ui.NewProgress("Commits", len(pgitCommits))

	for i := 0; i < len(pgitCommits); i += batchSize {
		end := i + batchSize
		if end > len(pgitCommits) {
			end = len(pgitCommits)
		}
		batch := pgitCommits[i:end]

		if err := r.DB.CreateCommitsBatch(ctx, batch); err != nil {
			fmt.Println()
			return fmt.Errorf("failed to insert commits batch: %w", err)
		}
		commitProgress.Update(end)
	}
	commitProgress.Done()

	// ═══════════════════════════════════════════════════════════════════════
	// Step 4: Import blobs with dual progress
	// ═══════════════════════════════════════════════════════════════════════

	// Collect all file changes (additions, modifications, AND deletions)
	var blobsToImport []blobWork
	for _, fc := range fileChanges {
		pgitCommitID, ok := commitMap[fc.CommitHash]
		if !ok {
			continue
		}

		if fc.ChangeType == 'D' {
			// Deletion - no content to fetch, will be handled in importBlobsOptimized
			blobsToImport = append(blobsToImport, blobWork{
				Path:      fc.Path,
				CommitID:  pgitCommitID,
				BlobHash:  "",
				Mode:      0,
				IsSymlink: false,
				IsDeleted: true,
			})
			continue
		}

		if fc.BlobHash == "" || fc.BlobHash == "0000000000000000000000000000000000000000" {
			continue
		}

		blobsToImport = append(blobsToImport, blobWork{
			Path:      fc.Path,
			CommitID:  pgitCommitID,
			BlobHash:  fc.BlobHash,
			Mode:      fc.Mode,
			IsSymlink: fc.Mode == 0120000,
			IsDeleted: false,
		})
	}

	fmt.Printf("\nImporting %s file versions...\n", ui.FormatCount(len(blobsToImport)))

	if len(blobsToImport) > 0 {
		err = importBlobsOptimized(ctx, r.DB, gitPath, blobsToImport, workers)
		if err != nil {
			return err
		}
	}

	// ═══════════════════════════════════════════════════════════════════════
	// Step 5: Set HEAD
	// ═══════════════════════════════════════════════════════════════════════

	latestCommit := pgitCommits[len(pgitCommits)-1]
	if err := r.DB.SetHead(ctx, latestCommit.ID); err != nil {
		return fmt.Errorf("failed to set HEAD: %w", err)
	}

	// ═══════════════════════════════════════════════════════════════════════
	// Step 6: Checkout working tree
	// ═══════════════════════════════════════════════════════════════════════

	fmt.Printf("\nChecking out files...\n")
	tree, err := r.DB.GetTreeAtCommit(ctx, latestCommit.ID)
	if err != nil {
		return fmt.Errorf("failed to get tree: %w", err)
	}

	for _, blob := range tree {
		absPath := r.AbsPath(blob.Path)

		// Ensure directory exists
		if err := os.MkdirAll(filepath.Dir(absPath), 0755); err != nil {
			continue
		}

		// Write file
		if blob.IsSymlink && blob.SymlinkTarget != nil {
			os.Remove(absPath)
			_ = os.Symlink(*blob.SymlinkTarget, absPath)
		} else {
			_ = os.WriteFile(absPath, blob.Content, os.FileMode(blob.Mode))
		}
	}

	fmt.Printf("\n%s Imported %s commits from git repository\n",
		styles.Green("Success!"),
		ui.FormatCount(len(commits)))
	fmt.Printf("HEAD is now at %s %s\n",
		styles.Hash(latestCommit.ID, true),
		firstLine(latestCommit.Message))

	return nil
}

// ═══════════════════════════════════════════════════════════════════════════
// Git history parsing (single command)
// ═══════════════════════════════════════════════════════════════════════════

func parseGitHistory(gitPath, branch string) ([]gitCommitInfo, []gitFileChange, error) {
	// git log --raw --reverse gives us commits with their file changes
	// Format: hash|parent|author|email|date|subject
	// Then raw diff lines: :oldmode newmode oldhash newhash status\tpath
	cmd := exec.Command("git", "log", "--raw", "--reverse",
		"--format=%H|%P|%an|%ae|%aI|%s", branch)
	cmd.Dir = gitPath

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, err
	}

	if err := cmd.Start(); err != nil {
		return nil, nil, err
	}

	var commits []gitCommitInfo
	var fileChanges []gitFileChange
	seenChanges := make(map[string]bool) // Dedupe merge commit duplicates

	scanner := bufio.NewScanner(stdout)
	// Increase buffer size for long lines
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)

	var currentCommit *gitCommitInfo

	for scanner.Scan() {
		line := scanner.Text()

		if line == "" {
			continue
		}

		// Check if it's a commit line (starts with hash)
		if len(line) >= 40 && !strings.HasPrefix(line, ":") {
			parts := strings.SplitN(line, "|", 6)
			if len(parts) >= 6 && len(parts[0]) == 40 {
				// Parse commit
				parentHash := ""
				if parts[1] != "" {
					parents := strings.Fields(parts[1])
					if len(parents) > 0 {
						parentHash = parents[0]
					}
				}

				authorDate, _ := time.Parse(time.RFC3339, parts[4])

				commit := gitCommitInfo{
					Hash:        parts[0],
					ParentHash:  parentHash,
					AuthorName:  parts[2],
					AuthorEmail: parts[3],
					AuthorDate:  authorDate,
					Message:     parts[5],
				}
				commits = append(commits, commit)
				currentCommit = &commits[len(commits)-1]
				continue
			}
		}

		// Check if it's a raw diff line
		if strings.HasPrefix(line, ":") && currentCommit != nil {
			// Format: :oldmode newmode oldhash newhash status\tpath
			// Example: :100644 100644 abc123 def456 M	path/to/file
			tabIdx := strings.Index(line, "\t")
			if tabIdx == -1 {
				continue
			}

			path := line[tabIdx+1:]
			fields := strings.Fields(line[1:tabIdx])
			if len(fields) < 5 {
				continue
			}

			newMode, _ := strconv.ParseInt(fields[1], 8, 32)
			newHash := fields[3]
			status := fields[4][0] // First char: A, M, D, R, C, etc.

			// For renames (R) and copies (C), the path might have two parts
			if status == 'R' || status == 'C' {
				pathParts := strings.Split(path, "\t")
				if len(pathParts) == 2 {
					path = pathParts[1] // Use destination path
				}
			}

			// Deduplicate: merge commits can list the same file change multiple times
			// (once for each parent). We only want the first one.
			changeKey := currentCommit.Hash + "\x00" + path
			if seenChanges[changeKey] {
				continue
			}
			seenChanges[changeKey] = true

			fc := gitFileChange{
				CommitHash: currentCommit.Hash,
				Path:       path,
				BlobHash:   newHash,
				Mode:       int(newMode),
				ChangeType: status,
			}
			fileChanges = append(fileChanges, fc)
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, nil, fmt.Errorf("error reading git output: %w", err)
	}

	if err := cmd.Wait(); err != nil {
		return nil, nil, fmt.Errorf("git log failed: %w", err)
	}

	return commits, fileChanges, nil
}

// ═══════════════════════════════════════════════════════════════════════════
// Blob import - optimized for pg-xpatch delta compression
// ═══════════════════════════════════════════════════════════════════════════

// importBlobsOptimized imports blobs grouped by path for optimal delta compression.
// pg-xpatch compresses blobs within the same path, so we:
// 1. Group all blobs by path
// 2. Sort each path's blobs by commit_id (chronological order)
// 3. Process paths in parallel (different paths are independent)
// 4. Insert all versions of a path together in order
//
// Each worker gets its own `git cat-file --batch` process for true parallelism.
func importBlobsOptimized(
	ctx context.Context,
	database *db.DB,
	gitPath string,
	blobs []blobWork,
	workers int,
) error {
	// ───────────────────────────────────────────────────────────────────────
	// Step 1: Group blobs by path and sort by commit_id
	// ───────────────────────────────────────────────────────────────────────

	pathGroups := make(map[string][]blobWork)
	for _, b := range blobs {
		pathGroups[b.Path] = append(pathGroups[b.Path], b)
	}

	// Sort each group by commit_id (ULID = chronological)
	for path := range pathGroups {
		group := pathGroups[path]
		sort.Slice(group, func(i, j int) bool {
			return group[i].CommitID < group[j].CommitID
		})
	}

	// Create list of paths to process
	paths := make([]string, 0, len(pathGroups))
	for path := range pathGroups {
		paths = append(paths, path)
	}

	// ───────────────────────────────────────────────────────────────────────
	// Step 2: Process paths with worker pool (each worker has own git process)
	// ───────────────────────────────────────────────────────────────────────

	totalBlobs := len(blobs)
	progress := ui.NewProgress("Blobs", totalBlobs)
	var imported atomic.Int64

	// Path work channel
	pathChan := make(chan string, len(paths))
	for _, p := range paths {
		pathChan <- p
	}
	close(pathChan)

	// Error handling
	var firstErr atomic.Value
	var wg sync.WaitGroup

	// Start workers - each with its own git cat-file process
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			// Start this worker's git cat-file --batch process
			catFile := exec.CommandContext(ctx, "git", "cat-file", "--batch")
			catFile.Dir = gitPath

			catStdin, err := catFile.StdinPipe()
			if err != nil {
				firstErr.CompareAndSwap(nil, fmt.Errorf("failed to create cat-file stdin: %w", err))
				return
			}

			catStdout, err := catFile.StdoutPipe()
			if err != nil {
				firstErr.CompareAndSwap(nil, fmt.Errorf("failed to create cat-file stdout: %w", err))
				return
			}

			if err := catFile.Start(); err != nil {
				firstErr.CompareAndSwap(nil, fmt.Errorf("failed to start cat-file: %w", err))
				return
			}
			defer func() { _ = catFile.Wait() }()
			defer catStdin.Close()

			catReader := bufio.NewReaderSize(catStdout, 1024*1024)

			// fetchBlobContent fetches content for a single blob from this worker's git process
			fetchBlobContent := func(blobHash string) ([]byte, error) {
				// Request blob
				if _, err := fmt.Fprintf(catStdin, "%s\n", blobHash); err != nil {
					return nil, err
				}

				// Read header
				header, err := catReader.ReadString('\n')
				if err != nil {
					return nil, err
				}

				parts := strings.Fields(header)
				if len(parts) < 3 || parts[1] == "missing" {
					return nil, fmt.Errorf("blob not found: %s", blobHash)
				}

				size, err := strconv.Atoi(parts[2])
				if err != nil {
					return nil, err
				}

				// Read content
				content := make([]byte, size)
				if _, err := io.ReadFull(catReader, content); err != nil {
					return nil, err
				}

				// Read trailing newline
				_, _ = catReader.ReadByte()

				return content, nil
			}

			// Process paths assigned to this worker
			for path := range pathChan {
				// Check for previous errors
				if firstErr.Load() != nil {
					return
				}

				group := pathGroups[path]
				dbBlobs := make([]*db.Blob, 0, len(group))

				// Fetch content for all blobs in this path (in order)
				for _, bw := range group {
					if bw.IsDeleted {
						// Deletion - no content to fetch
						dbBlobs = append(dbBlobs, &db.Blob{
							Path:        bw.Path,
							CommitID:    bw.CommitID,
							Content:     []byte{},
							ContentHash: nil, // nil = deleted
							Mode:        0,
							IsSymlink:   false,
						})
						continue
					}

					content, err := fetchBlobContent(bw.BlobHash)
					if err != nil {
						// Skip missing blobs (may have been garbage collected)
						continue
					}

					// Compute BLAKE3 hash (16 bytes)
					contentHash := util.HashBytesBlake3(content)

					blob := &db.Blob{
						Path:        bw.Path,
						CommitID:    bw.CommitID,
						Content:     content,
						ContentHash: contentHash,
						Mode:        bw.Mode,
						IsSymlink:   bw.IsSymlink,
					}

					if bw.IsSymlink {
						target := string(content)
						blob.SymlinkTarget = &target
					}

					dbBlobs = append(dbBlobs, blob)
				}

				// Insert all blobs for this path (in chronological order)
				if len(dbBlobs) > 0 {
					if err := database.CreateBlobs(ctx, dbBlobs); err != nil {
						firstErr.CompareAndSwap(nil, err)
						return
					}
				}

				// Update progress
				count := imported.Add(int64(len(dbBlobs)))
				progress.Update(int(count))
			}
		}()
	}

	wg.Wait()
	progress.Done()

	if err := firstErr.Load(); err != nil {
		return err.(error)
	}

	return nil
}

// ═══════════════════════════════════════════════════════════════════════════
// Branch selection
// ═══════════════════════════════════════════════════════════════════════════

func selectBranch(gitPath, branchFlag string) (string, error) {
	if branchFlag != "" {
		return branchFlag, nil
	}

	branches, err := getGitBranches(gitPath)
	if err != nil {
		return "", fmt.Errorf("failed to list branches: %w", err)
	}

	if len(branches) == 0 {
		return "", fmt.Errorf("no branches found")
	}

	if len(branches) == 1 {
		return branches[0].Name, nil
	}

	// Multiple branches - show picker if TTY
	if term.IsTerminal(int(os.Stdout.Fd())) && !styles.IsAccessible() {
		fmt.Println()
		return runBranchPicker(branches)
	}

	// Non-interactive: use current branch
	return getCurrentBranch(gitPath), nil
}

type gitBranch struct {
	Name        string
	CommitCount int
	IsCurrent   bool
}

func getGitBranches(gitPath string) ([]gitBranch, error) {
	cmd := exec.Command("git", "branch", "-a", "--format=%(refname:short)%(if)%(HEAD)%(then)*%(end)")
	cmd.Dir = gitPath
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var branches []gitBranch
	seen := make(map[string]bool)

	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		isCurrent := strings.HasSuffix(line, "*")
		name := strings.TrimSuffix(line, "*")

		// Skip remote duplicates
		if strings.HasPrefix(name, "origin/") {
			localName := strings.TrimPrefix(name, "origin/")
			if seen[localName] || localName == "HEAD" {
				continue
			}
		}

		if seen[name] {
			continue
		}
		seen[name] = true

		// Get commit count
		countCmd := exec.Command("git", "rev-list", "--count", name)
		countCmd.Dir = gitPath
		countOutput, _ := countCmd.Output()
		count, _ := strconv.Atoi(strings.TrimSpace(string(countOutput)))

		branches = append(branches, gitBranch{
			Name:        name,
			CommitCount: count,
			IsCurrent:   isCurrent,
		})
	}

	sort.Slice(branches, func(i, j int) bool {
		if branches[i].IsCurrent != branches[j].IsCurrent {
			return branches[i].IsCurrent
		}
		return branches[i].Name < branches[j].Name
	})

	return branches, nil
}

func getCurrentBranch(gitPath string) string {
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = gitPath
	output, err := cmd.Output()
	if err != nil {
		return "main"
	}
	return strings.TrimSpace(string(output))
}

// ═══════════════════════════════════════════════════════════════════════════
// Branch Picker TUI
// ═══════════════════════════════════════════════════════════════════════════

type branchPickerModel struct {
	branches []gitBranch
	cursor   int
	selected string
	quit     bool
	height   int // terminal height
	offset   int // scroll offset
	ready    bool
}

func (m branchPickerModel) Init() tea.Cmd {
	return nil
}

func (m branchPickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.height = msg.Height
		m.ready = true
		return m, nil

	case tea.KeyMsg:
		switch {
		case key.Matches(msg, key.NewBinding(key.WithKeys("up", "k"))):
			if m.cursor > 0 {
				m.cursor--
				// Scroll up if cursor goes above visible area
				if m.cursor < m.offset {
					m.offset = m.cursor
				}
			}
		case key.Matches(msg, key.NewBinding(key.WithKeys("down", "j"))):
			if m.cursor < len(m.branches)-1 {
				m.cursor++
				// Scroll down if cursor goes below visible area
				_, end := m.visibleRange()
				if m.cursor >= end {
					m.offset++
				}
			}
		case key.Matches(msg, key.NewBinding(key.WithKeys("enter"))):
			m.selected = m.branches[m.cursor].Name
			return m, tea.Quit
		case key.Matches(msg, key.NewBinding(key.WithKeys("q", "esc", "ctrl+c"))):
			m.quit = true
			return m, tea.Quit
		}
	}
	return m, nil
}

// maxVisibleLines returns the maximum branch lines that can fit
// accounting for header (2 lines), footer (2 lines), and scroll indicators (2 lines)
func (m branchPickerModel) maxVisibleLines() int {
	if m.height <= 7 {
		return 1
	}
	return m.height - 7 // header(2) + footer(2) + top indicator(1) + bottom indicator(1) + buffer(1)
}

// visibleRange returns the start/end indices of visible branches
func (m branchPickerModel) visibleRange() (start, end int) {
	start = m.offset
	end = start + m.maxVisibleLines()
	if end > len(m.branches) {
		end = len(m.branches)
	}
	return start, end
}

func (m branchPickerModel) View() string {
	if !m.ready {
		return "Loading..."
	}

	var b strings.Builder
	b.WriteString(styles.Boldf("Select branch to import:"))
	b.WriteString("\n\n")

	start, end := m.visibleRange()

	// Always show scroll indicator areas (reserved space)
	if start > 0 {
		b.WriteString(styles.Mute(fmt.Sprintf("  ↑ %d more above", start)))
	}
	b.WriteString("\n")

	for i := start; i < end; i++ {
		branch := m.branches[i]
		var cursor string
		if i == m.cursor {
			cursor = styles.Cyan(">") + " "
		} else {
			cursor = "  "
		}

		name := branch.Name
		if branch.IsCurrent {
			name = styles.Green(name) + styles.Mute(" (current)")
		} else if i == m.cursor {
			name = styles.Cyan(name)
		}

		b.WriteString(cursor)
		b.WriteString(name)
		b.WriteString(" ")
		b.WriteString(styles.Mutef("(%d commits)", branch.CommitCount))
		b.WriteString("\n")
	}

	// Always show scroll indicator area (reserved space)
	if end < len(m.branches) {
		b.WriteString(styles.Mute(fmt.Sprintf("  ↓ %d more below", len(m.branches)-end)))
	}
	b.WriteString("\n")

	b.WriteString("\n")
	b.WriteString(styles.Mute("  ↑/↓ navigate  enter select  q cancel"))
	return b.String()
}

func runBranchPicker(branches []gitBranch) (string, error) {
	m := branchPickerModel{branches: branches}
	p := tea.NewProgram(m, tea.WithAltScreen())

	finalModel, err := p.Run()
	if err != nil {
		return "", err
	}

	result := finalModel.(branchPickerModel)
	if result.quit {
		return "", fmt.Errorf("cancelled")
	}

	return result.selected, nil
}
