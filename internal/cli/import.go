package cli

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/imgajeed76/pgit/internal/db"
	"github.com/imgajeed76/pgit/internal/repo"
	"github.com/imgajeed76/pgit/internal/ui"
	"github.com/imgajeed76/pgit/internal/ui/styles"
	"github.com/imgajeed76/pgit/internal/util"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var (
	importWorkers int
	importDryRun  bool
	importForce   bool
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

File histories are extracted in parallel for better performance.
The current directory must be a pgit repository (run 'pgit init' first).`,
		Args: cobra.MaximumNArgs(1),
		RunE: runImport,
	}

	cmd.Flags().IntVarP(&importWorkers, "workers", "w", 0, "Number of parallel workers (default: number of CPUs)")
	cmd.Flags().BoolVarP(&importDryRun, "dry-run", "n", false, "Show what would be imported without actually importing")
	cmd.Flags().BoolVarP(&importForce, "force", "f", false, "Overwrite existing data in database")
	cmd.Flags().StringP("branch", "b", "", "Branch to import (default: current branch, or interactive picker)")

	return cmd
}

// gitCommit represents a git commit
type gitCommit struct {
	Hash        string
	ParentHash  string
	AuthorName  string
	AuthorEmail string
	AuthorDate  time.Time
	Message     string
}

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

	// Make absolute
	gitPath, err = filepath.Abs(gitPath)
	if err != nil {
		return err
	}

	// Check if it's a git repo
	gitDir := filepath.Join(gitPath, ".git")
	if _, err := os.Stat(gitDir); os.IsNotExist(err) {
		return util.NewError("Not a git repository").
			WithContext(fmt.Sprintf("No .git directory found in '%s'", gitPath)).
			WithSuggestion("pgit import /path/to/git/repo  # Specify correct path")
	}

	// Set worker count
	if importWorkers <= 0 {
		importWorkers = runtime.NumCPU()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
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
	fmt.Printf("Workers: %d\n", importWorkers)

	if importDryRun {
		fmt.Println(styles.Yellow("Dry run mode - no changes will be made"))
	}

	// Check if database already has commits
	var commitCount int
	_ = r.DB.QueryRow(ctx, "SELECT COUNT(*) FROM pgit_commits").Scan(&commitCount)
	if commitCount > 0 && !importForce {
		return util.NewError("Database not empty").
			WithMessage(fmt.Sprintf("Database already contains %d commits", commitCount)).
			WithSuggestion("pgit import --force  # Overwrite existing data")
	}

	// Determine which branch to import
	branchFlag, _ := cmd.Flags().GetString("branch")
	selectedBranch := branchFlag

	if selectedBranch == "" {
		// Check if we're in a TTY and have multiple branches
		branches, err := getGitBranches(gitPath)
		if err != nil {
			return fmt.Errorf("failed to list branches: %w", err)
		}

		if len(branches) == 0 {
			return fmt.Errorf("no branches found in git repository")
		}

		if len(branches) == 1 {
			// Only one branch, use it
			selectedBranch = branches[0].Name
		} else if term.IsTerminal(int(os.Stdout.Fd())) && !styles.IsAccessible() {
			// Multiple branches and TTY (non-accessible) - show picker
			fmt.Println()
			picked, err := runBranchPicker(branches)
			if err != nil {
				return err
			}
			selectedBranch = picked
			fmt.Printf("\033[2K\r") // Clear line
		} else {
			// Non-interactive or accessible mode, use current branch
			selectedBranch = getCurrentBranch(gitPath)
			if styles.IsAccessible() {
				fmt.Printf("Using current branch (accessible mode): %s\n", selectedBranch)
				fmt.Println("Tip: Use --branch to specify a different branch")
			}
		}
	}

	fmt.Printf("Branch: %s\n", styles.Branch(selectedBranch))

	// Step 1: Get all commits in topological order (oldest first)
	fmt.Println("\nExtracting commit history...")
	commits, err := getGitCommits(gitPath, selectedBranch)
	if err != nil {
		return fmt.Errorf("failed to get commits: %w", err)
	}
	fmt.Printf("Found %d commits\n", len(commits))

	if len(commits) == 0 {
		fmt.Println("No commits to import")
		return nil
	}

	// Step 2: Get all file paths that ever existed
	fmt.Println("\nFinding all files...")
	allPaths, err := getAllFilePaths(gitPath)
	if err != nil {
		return fmt.Errorf("failed to get file paths: %w", err)
	}
	fmt.Printf("Found %d unique file paths\n", len(allPaths))

	if importDryRun {
		fmt.Println("\nDry run complete. Would import:")
		fmt.Printf("  - %d commits\n", len(commits))
		fmt.Printf("  - %d file paths\n", len(allPaths))
		if commitCount > 0 && importForce {
			fmt.Printf("  - Would clear existing %d commits (--force)\n", commitCount)
		}
		return nil
	}

	// Clear existing data if --force was specified
	if commitCount > 0 && importForce {
		fmt.Println("\nClearing existing data...")
		if err := r.DB.DropSchema(ctx); err != nil {
			return fmt.Errorf("failed to clear database: %w", err)
		}
		if err := r.DB.InitSchema(ctx); err != nil {
			return fmt.Errorf("failed to reinit database: %w", err)
		}
	}

	// Step 3: Create commit ID mapping (git hash -> ULID)
	fmt.Println("\nGenerating commit IDs...")
	commitMap := make(map[string]string) // git hash -> ULID
	pgitCommits := make([]*db.Commit, len(commits))

	// Track last timestamp to ensure strictly increasing order
	// (git has 1-second resolution, xpatch needs unique timestamps)
	var lastTime time.Time

	for i, gc := range commits {
		commitTime := gc.AuthorDate
		// Ensure strictly increasing timestamps
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
			TreeHash:    gc.Hash[:8], // Use git hash prefix as tree hash
			Message:     util.ToValidUTF8(gc.Message),
			AuthorName:  util.ToValidUTF8(gc.AuthorName),
			AuthorEmail: util.ToValidUTF8(gc.AuthorEmail),
			CreatedAt:   commitTime,
		}
	}

	// Step 4: Insert commits
	fmt.Println("\nImporting commits...")
	commitProgress := ui.NewProgress("  Commits", len(pgitCommits))
	for i, commit := range pgitCommits {
		if err := r.DB.CreateCommit(ctx, commit); err != nil {
			fmt.Println()
			return fmt.Errorf("failed to create commit %s: %w", util.ShortID(commit.ID), err)
		}
		commitProgress.Update(i + 1)
	}
	commitProgress.Done()

	// Step 5: Extract and import file contents in parallel
	fmt.Println("\nImporting file contents...")

	// Create work channel
	type workItem struct {
		path    string
		commits []gitCommit
	}
	workChan := make(chan workItem, len(allPaths))
	resultChan := make(chan error, len(allPaths))

	// Start workers
	var wg sync.WaitGroup
	for i := 0; i < importWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for work := range workChan {
				err := importFileHistory(ctx, r.DB, gitPath, work.path, work.commits, commitMap)
				resultChan <- err
			}
		}()
	}

	// Send work
	for _, path := range allPaths {
		workChan <- workItem{path: path, commits: commits}
	}
	close(workChan)

	// Wait for completion and collect errors
	go func() {
		wg.Wait()
		close(resultChan)
	}()

	fileProgress := ui.NewProgress("  Files  ", len(allPaths))
	var errCount int
	var completed int
	for err := range resultChan {
		completed++
		if err != nil {
			errCount++
		}
		fileProgress.Update(completed)
	}
	fileProgress.Done()

	if errCount > 0 {
		fmt.Printf(styles.Red("\n%d errors occurred during import\n"), errCount)
	}

	// Step 6: Set HEAD to latest commit
	latestCommit := pgitCommits[len(pgitCommits)-1]
	if err := r.DB.SetHead(ctx, latestCommit.ID); err != nil {
		return fmt.Errorf("failed to set HEAD: %w", err)
	}

	fmt.Printf("\n%s Imported %d commits from git repository\n",
		styles.Green("Success!"), len(commits))
	fmt.Printf("HEAD is now at %s %s\n",
		styles.Yellow(util.ShortID(latestCommit.ID)),
		firstLine(latestCommit.Message))

	return nil
}

// getGitCommits returns all commits in topological order (oldest first)
// If branch is empty, uses the current branch
func getGitCommits(gitPath, branch string) ([]gitCommit, error) {
	// Format: hash|parent|author name|author email|author date|subject
	args := []string{"log", "--reverse", "--format=%H|%P|%an|%ae|%aI|%s"}
	if branch != "" {
		args = append(args, branch)
	}
	cmd := exec.Command("git", args...)
	cmd.Dir = gitPath

	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	var commits []gitCommit
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.SplitN(line, "|", 6)
		if len(parts) < 6 {
			continue
		}

		// Parse parent (may be empty or have multiple for merges - take first)
		parentHash := ""
		if parts[1] != "" {
			parents := strings.Fields(parts[1])
			if len(parents) > 0 {
				parentHash = parents[0]
			}
		}

		// Parse date
		authorDate, err := time.Parse(time.RFC3339, parts[4])
		if err != nil {
			authorDate = time.Now()
		}

		commits = append(commits, gitCommit{
			Hash:        parts[0],
			ParentHash:  parentHash,
			AuthorName:  parts[2],
			AuthorEmail: parts[3],
			AuthorDate:  authorDate,
			Message:     parts[5],
		})
	}

	return commits, nil
}

// getAllFilePaths returns all file paths that ever existed in the repo
func getAllFilePaths(gitPath string) ([]string, error) {
	cmd := exec.Command("git", "log", "--all", "--pretty=format:", "--name-only", "--diff-filter=ACMRT")
	cmd.Dir = gitPath

	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	// Deduplicate paths
	pathSet := make(map[string]bool)
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		path := strings.TrimSpace(scanner.Text())
		if path != "" {
			pathSet[path] = true
		}
	}

	paths := make([]string, 0, len(pathSet))
	for path := range pathSet {
		paths = append(paths, path)
	}

	return paths, nil
}

// importFileHistory imports the history of a single file
func importFileHistory(ctx context.Context, database *db.DB, gitPath, filePath string, commits []gitCommit, commitMap map[string]string) error {
	// Get all commits that touched this file
	cmd := exec.Command("git", "log", "--follow", "--format=%H", "--", filePath)
	cmd.Dir = gitPath

	output, err := cmd.Output()
	if err != nil {
		// File might not exist in some branches
		return nil
	}

	// Parse commit hashes
	fileCommits := make(map[string]bool)
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	for scanner.Scan() {
		hash := strings.TrimSpace(scanner.Text())
		if hash != "" {
			fileCommits[hash] = true
		}
	}

	if len(fileCommits) == 0 {
		return nil
	}

	// For each commit that touched this file, get the content
	var blobs []*db.Blob
	var lastContent []byte

	for _, gc := range commits {
		if !fileCommits[gc.Hash] {
			continue
		}

		pgitCommitID := commitMap[gc.Hash]

		// Get file content at this commit
		content, mode, isSymlink, target, err := getFileAtCommit(gitPath, gc.Hash, filePath)
		if err != nil {
			// File might be deleted at this commit
			continue
		}

		// Skip if content is same as last (shouldn't happen but be safe)
		if string(content) == string(lastContent) {
			continue
		}
		lastContent = content

		contentHash := util.HashBytes(content)

		blob := &db.Blob{
			Path:        filePath,
			CommitID:    pgitCommitID,
			Content:     content,
			ContentHash: &contentHash,
			Mode:        mode,
			IsSymlink:   isSymlink,
		}
		if isSymlink {
			blob.SymlinkTarget = &target
		}

		blobs = append(blobs, blob)
	}

	if len(blobs) == 0 {
		return nil
	}

	// Insert blobs
	return database.CreateBlobs(ctx, blobs)
}

// gitBranch represents a git branch with metadata
type gitBranch struct {
	Name        string
	CommitCount int
	IsCurrent   bool
}

// getGitBranches returns all branches in a git repo with commit counts
func getGitBranches(gitPath string) ([]gitBranch, error) {
	// Get all branches
	cmd := exec.Command("git", "branch", "-a", "--format=%(refname:short)%(if)%(HEAD)%(then)*%(end)")
	cmd.Dir = gitPath
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("failed to list branches: %w", err)
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

		// Skip remote tracking branches that duplicate local ones
		if strings.HasPrefix(name, "origin/") {
			localName := strings.TrimPrefix(name, "origin/")
			if seen[localName] {
				continue
			}
			// Also skip origin/HEAD
			if localName == "HEAD" {
				continue
			}
		}

		if seen[name] {
			continue
		}
		seen[name] = true

		// Get commit count for this branch
		countCmd := exec.Command("git", "rev-list", "--count", name)
		countCmd.Dir = gitPath
		countOutput, err := countCmd.Output()
		count := 0
		if err == nil {
			count, _ = strconv.Atoi(strings.TrimSpace(string(countOutput)))
		}

		branches = append(branches, gitBranch{
			Name:        name,
			CommitCount: count,
			IsCurrent:   isCurrent,
		})
	}

	// Sort: current branch first, then by name
	sort.Slice(branches, func(i, j int) bool {
		if branches[i].IsCurrent != branches[j].IsCurrent {
			return branches[i].IsCurrent
		}
		return branches[i].Name < branches[j].Name
	})

	return branches, nil
}

// getCurrentBranch returns the current git branch name
func getCurrentBranch(gitPath string) string {
	cmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	cmd.Dir = gitPath
	output, err := cmd.Output()
	if err != nil {
		return "main" // fallback
	}
	return strings.TrimSpace(string(output))
}

// ═══════════════════════════════════════════════════════════════════════════
// Branch Picker TUI (single-select)
// ═══════════════════════════════════════════════════════════════════════════

type branchPickerModel struct {
	branches []gitBranch
	cursor   int
	selected string
	width    int
	height   int
	quit     bool
}

func newBranchPicker(branches []gitBranch) branchPickerModel {
	width, height, _ := term.GetSize(int(os.Stdout.Fd()))
	if width == 0 {
		width = 80
	}
	if height == 0 {
		height = 24
	}

	return branchPickerModel{
		branches: branches,
		cursor:   0, // Start at first (current branch if sorted)
		width:    width,
		height:   height,
	}
}

func (m branchPickerModel) Init() tea.Cmd {
	return nil
}

func (m branchPickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, branchPickerKeys.Up):
			if m.cursor > 0 {
				m.cursor--
			}
		case key.Matches(msg, branchPickerKeys.Down):
			if m.cursor < len(m.branches)-1 {
				m.cursor++
			}
		case key.Matches(msg, branchPickerKeys.Select):
			m.selected = m.branches[m.cursor].Name
			return m, tea.Quit
		case key.Matches(msg, branchPickerKeys.Quit):
			m.quit = true
			return m, tea.Quit
		}
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	}
	return m, nil
}

func (m branchPickerModel) View() string {
	var b strings.Builder

	b.WriteString(styles.Boldf("Select branch to import:"))
	b.WriteString("\n\n")

	// Show branches
	maxVisible := m.height - 6 // Leave room for header and help
	if maxVisible < 3 {
		maxVisible = 3
	}

	start := 0
	if m.cursor >= maxVisible {
		start = m.cursor - maxVisible + 1
	}
	end := start + maxVisible
	if end > len(m.branches) {
		end = len(m.branches)
	}

	for i := start; i < end; i++ {
		branch := m.branches[i]

		cursor := "  "
		if i == m.cursor {
			cursor = styles.Cyan("> ")
		}

		name := branch.Name
		if branch.IsCurrent {
			name = styles.Green(name) + styles.Mute(" (current)")
		} else if i == m.cursor {
			name = styles.Cyan(name)
		}

		commits := styles.Mutef("(%d commits)", branch.CommitCount)

		b.WriteString(fmt.Sprintf("%s%s %s\n", cursor, name, commits))
	}

	// Scroll indicator
	if len(m.branches) > maxVisible {
		b.WriteString(styles.Mutef("\n  ... %d branches total", len(m.branches)))
	}

	b.WriteString("\n\n")
	b.WriteString(styles.Mute("  ↑/↓ navigate  enter select  q cancel"))

	return b.String()
}

// Key bindings for branch picker
var branchPickerKeys = struct {
	Up     key.Binding
	Down   key.Binding
	Select key.Binding
	Quit   key.Binding
}{
	Up:     key.NewBinding(key.WithKeys("up", "k")),
	Down:   key.NewBinding(key.WithKeys("down", "j")),
	Select: key.NewBinding(key.WithKeys("enter")),
	Quit:   key.NewBinding(key.WithKeys("q", "esc", "ctrl+c")),
}

// runBranchPicker shows the TUI and returns the selected branch
func runBranchPicker(branches []gitBranch) (string, error) {
	m := newBranchPicker(branches)
	p := tea.NewProgram(m)

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

// getFileAtCommit returns the content of a file at a specific commit
func getFileAtCommit(gitPath, commitHash, filePath string) (content []byte, mode int, isSymlink bool, target string, err error) {
	// First check the file mode
	cmd := exec.Command("git", "ls-tree", commitHash, "--", filePath)
	cmd.Dir = gitPath
	output, err := cmd.Output()
	if err != nil || len(output) == 0 {
		return nil, 0, false, "", fmt.Errorf("file not found")
	}

	// Parse mode from ls-tree output: "mode type hash\tpath"
	parts := strings.Fields(string(output))
	if len(parts) < 3 {
		return nil, 0, false, "", fmt.Errorf("invalid ls-tree output")
	}

	modeStr := parts[0]
	modeInt, _ := strconv.ParseInt(modeStr, 8, 32)
	mode = int(modeInt)

	// Check if symlink (mode 120000)
	if modeStr == "120000" {
		// For symlinks, git show returns the target path
		cmd = exec.Command("git", "show", fmt.Sprintf("%s:%s", commitHash, filePath))
		cmd.Dir = gitPath
		output, err = cmd.Output()
		if err != nil {
			return nil, 0, false, "", err
		}
		return nil, mode, true, string(output), nil
	}

	// Get file content
	cmd = exec.Command("git", "show", fmt.Sprintf("%s:%s", commitHash, filePath))
	cmd.Dir = gitPath
	content, err = cmd.Output()
	if err != nil {
		return nil, 0, false, "", err
	}

	return content, mode, false, "", nil
}
