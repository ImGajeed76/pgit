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
	"github.com/imgajeed76/pgit/v3/internal/config"
	"github.com/imgajeed76/pgit/v3/internal/db"
	"github.com/imgajeed76/pgit/v3/internal/repo"
	"github.com/imgajeed76/pgit/v3/internal/ui"
	"github.com/imgajeed76/pgit/v3/internal/ui/styles"
	"github.com/imgajeed76/pgit/v3/internal/util"
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

The import uses git fast-export for correct handling of merges, renames,
and full commit messages. A parallel worker pool imports blob content
with progress visualization.

The current directory must be a pgit repository (run 'pgit init' first).`,
		Args: cobra.MaximumNArgs(1),
		RunE: runImport,
	}

	cmd.Flags().IntP("workers", "w", 0, "Number of parallel workers (default 4, max 16)")
	cmd.Flags().BoolP("dry-run", "n", false, "Show what would be imported without actually importing")
	cmd.Flags().BoolP("force", "f", false, "Overwrite existing data in database")
	cmd.Flags().StringP("branch", "b", "", "Branch to import (default: current branch, or interactive picker)")
	cmd.Flags().String("remote", "", "Import directly into a remote database (e.g. 'origin'), skipping local container")
	cmd.Flags().Bool("resume", false, "Resume a previously interrupted import")
	cmd.Flags().String("fastexport", "", "Use a pre-generated git fast-export file instead of re-exporting")
	cmd.Flags().Duration("timeout", 24*time.Hour, "Maximum time for the import operation (e.g. 2h, 30m, 48h)")

	return cmd
}

// ═══════════════════════════════════════════════════════════════════════════
// Data structures for fast-export parsing
// ═══════════════════════════════════════════════════════════════════════════

// blobEntry records where a blob's content lives in the temp file.
type blobEntry struct {
	Mark       int    // :N mark number
	Offset     int64  // byte offset in temp file where content starts (after "data <size>\n")
	Size       int    // byte size of content
	OriginalID string // git SHA (from original-oid line), empty if not present
}

// commitEntry records parsed commit metadata from fast-export.
type commitEntry struct {
	Mark               int
	OriginalID         string // git SHA
	AuthorName         string
	AuthorEmail        string
	AuthorTimestamp    int64 // unix seconds
	AuthorTZ           string
	CommitterName      string
	CommitterEmail     string
	CommitterTimestamp int64
	CommitterTZ        string
	MessageOffset      int64 // byte offset of message in temp file
	MessageSize        int   // message byte count
	FromMark           int   // parent commit mark (0 = root commit)
	MergeMark          int   // merge parent mark (0 = not a merge, only first merge tracked)
	FileOps            []fileOp
}

// fileOp represents a file operation in a commit.
type fileOp struct {
	Type     byte   // 'M' (modify/add), 'D' (delete)
	Mode     int    // file mode (100644, 100755, 120000)
	BlobMark int    // mark reference for M ops (0 for D ops)
	Path     string // file path (unquoted)
}

// pathOp groups a commit's operation on a specific path.
type pathOp struct {
	CommitID string // pgit ULID
	BlobMark int    // mark -> can get offset+size from blobIndex
	Mode     int
	IsDelete bool
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
	resume, _ := cmd.Flags().GetBool("resume")
	fastExportPath, _ := cmd.Flags().GetString("fastexport")

	remoteName, _ := cmd.Flags().GetString("remote")
	isRemote := remoteName != ""

	timeout, _ := cmd.Flags().GetDuration("timeout")
	if timeout <= 0 {
		timeout = 24 * time.Hour
	}

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Connect to database
	if isRemote {
		// Remote mode: connect directly to remote, skip container
		remote, exists := r.Config.GetRemote(remoteName)
		if !exists {
			return util.RemoteNotFoundError(remoteName)
		}

		remoteDB, err := r.ConnectTo(ctx, remote.URL)
		if err != nil {
			return util.DatabaseConnectionError(remote.URL, err)
		}
		defer remoteDB.Close()

		r.DB = remoteDB

		// Initialize schema if needed
		schemaExists, err := remoteDB.SchemaExists(ctx)
		if err != nil {
			return err
		}
		if !schemaExists {
			fmt.Println("Initializing remote schema...")
			if err := remoteDB.InitSchema(ctx); err != nil {
				return err
			}
		}
	} else {
		// Local mode (existing behavior)
		if err := r.StartContainer(); err != nil {
			return err
		}
		if err := r.Connect(ctx); err != nil {
			return err
		}
		defer r.Close()
	}

	// Set session-level GUCs for import performance
	if err := r.DB.SetImportGUCs(ctx); err != nil {
		fmt.Printf("Warning: failed to set import GUCs: %v\n", err)
	}
	defer r.DB.ResetImportGUCs(ctx)

	fmt.Printf("Importing from: %s\n", styles.Cyan(gitPath))
	fmt.Printf("Workers: %d\n", workers)

	if dryRun {
		fmt.Println(styles.Yellow("Dry run mode - no changes will be made"))
	}

	// Check if database already has commits and determine resume state
	var existingCommits int
	_ = r.DB.QueryRow(ctx, "SELECT COUNT(*) FROM pgit_commits").Scan(&existingCommits)

	resumeFromBlobs := false

	if existingCommits > 0 && force {
		// --force always wipes, handled below
	} else if existingCommits > 0 && resume {
		// --resume: validate we have a resumable state
		importState, _ := r.DB.GetMetadata(ctx, "import_state")
		importBranch, _ := r.DB.GetMetadata(ctx, "import_branch")
		switch importState {
		case "commits_done":
			resumeFromBlobs = true
			fmt.Printf("Resuming interrupted import (%s commits already inserted)\n",
				ui.FormatCount(existingCommits))
			if importBranch != "" {
				fmt.Printf("Original import branch: %s\n", styles.Branch(importBranch))
			}
		case "complete":
			return util.NewError("Import already complete").
				WithMessage(fmt.Sprintf("Database contains %d commits from a completed import", existingCommits)).
				WithSuggestion("pgit import --force  # Wipe and re-import from scratch")
		default:
			// No import_state — either partial commit phase or old version crash.
			// Either way: skip already-inserted commits, continue from where we left off.
			resumeFromBlobs = true
			importBranch, _ := r.DB.GetMetadata(ctx, "import_branch")
			fmt.Printf("Resuming interrupted import (%s commits found in database)\n",
				ui.FormatCount(existingCommits))
			if importBranch != "" {
				fmt.Printf("Original import branch: %s\n", styles.Branch(importBranch))
			}
		}
	} else if existingCommits > 0 {
		// No --force, no --resume: tell user what to do
		importState, _ := r.DB.GetMetadata(ctx, "import_state")
		switch importState {
		case "complete":
			return util.NewError("Database not empty").
				WithMessage(fmt.Sprintf("Database already contains %d commits from a completed import", existingCommits)).
				WithSuggestion("pgit import --force  # Wipe and re-import from scratch")
		case "commits_done":
			return util.NewError("Interrupted import detected").
				WithMessage(fmt.Sprintf("Database contains %d commits but blob import is incomplete", existingCommits)).
				WithSuggestions(
					"pgit import --resume  # Resume blob import",
					"pgit import --force   # Wipe and start over",
				)
		default:
			return util.NewError("Incomplete import detected").
				WithMessage(fmt.Sprintf("Database contains %d commits but import did not finish", existingCommits)).
				WithSuggestions(
					"pgit import --resume  # Resume from where it left off",
					"pgit import --force   # Wipe and start over",
				)
		}
	}

	// Determine branch
	branchFlag, _ := cmd.Flags().GetString("branch")
	selectedBranch, err := selectBranch(gitPath, branchFlag)
	if err != nil {
		return err
	}
	fmt.Printf("Branch: %s\n", styles.Branch(selectedBranch))

	// ═══════════════════════════════════════════════════════════════════════
	// Step 1: Export to temp file via git fast-export (or use provided file)
	// ═══════════════════════════════════════════════════════════════════════

	var tmpPath string
	ownsTmpFile := false // whether we should clean up the temp file

	if fastExportPath != "" {
		// Use pre-generated fast-export file
		info, err := os.Stat(fastExportPath)
		if err != nil {
			return util.NewError("Cannot read fast-export file").
				WithMessage(fmt.Sprintf("File not found or not readable: %s", fastExportPath)).
				WithSuggestion("Provide a valid fast-export file generated with:\n  git fast-export --reencode=yes --show-original-ids <branch> > export.stream")
		}
		tmpPath = fastExportPath
		fmt.Printf("Using fast-export file: %s (%s)\n", fastExportPath, formatBytes(info.Size()))
	} else {
		spinner := ui.NewSpinner("Exporting git history")
		spinner.Start()

		var exportSize int64
		tmpPath, exportSize, err = exportToFile(gitPath, selectedBranch)
		spinner.Stop()

		if err != nil {
			// Clean up temp file if it was created
			if tmpPath != "" {
				os.Remove(tmpPath)
			}
			// Check if the branch doesn't exist
			if strings.Contains(err.Error(), "exit status 128") {
				branches, branchErr := getGitBranches(gitPath)
				if branchErr == nil && len(branches) > 0 {
					var branchNames []string
					for _, b := range branches {
						branchNames = append(branchNames, b.Name)
					}
					return util.NewError(fmt.Sprintf("Branch '%s' not found", selectedBranch)).
						WithMessage(fmt.Sprintf("Available branches: %s", strings.Join(branchNames, ", "))).
						WithSuggestion(fmt.Sprintf("pgit import %s --branch %s", gitPath, branchNames[0]))
				}
			}
			return fmt.Errorf("failed to export git history: %w", err)
		}
		ownsTmpFile = true
		fmt.Printf("Exported %s fast-export stream\n", formatBytes(exportSize))
		fmt.Printf("Temp file: %s\n", styles.Mute(tmpPath))
	}

	// Note: we do NOT defer os.Remove(tmpPath) here. If the import crashes,
	// the temp file is preserved so the user can resume with --fastexport.
	// It is cleaned up explicitly on successful completion at the end.

	// ═══════════════════════════════════════════════════════════════════════
	// Step 2: Index the fast-export stream (single pass)
	// ═══════════════════════════════════════════════════════════════════════

	idxSpinner := ui.NewSpinner("Indexing fast-export stream")
	idxSpinner.Start()

	commitEntries, blobIndex, err := indexFastExport(tmpPath)
	idxSpinner.Stop()

	if err != nil {
		return fmt.Errorf("failed to index fast-export: %w", err)
	}

	// Count total file ops
	totalFileOps := 0
	for _, ce := range commitEntries {
		totalFileOps += len(ce.FileOps)
	}

	fmt.Printf("Found %s commits, %s file changes, %s blobs\n",
		ui.FormatCount(len(commitEntries)),
		ui.FormatCount(totalFileOps),
		ui.FormatCount(len(blobIndex)))

	if len(commitEntries) == 0 {
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
	// Step 3: Prepare commits (ULID assignment, commit objects, path grouping)
	// ═══════════════════════════════════════════════════════════════════════

	fmt.Println("\nPreparing commits...")

	tmpFile, err := os.Open(tmpPath)
	if err != nil {
		return fmt.Errorf("failed to reopen temp file: %w", err)
	}
	defer tmpFile.Close()

	pgitCommits, markToULID, pathOps := prepareCommits(commitEntries, blobIndex, tmpFile)

	// ═══════════════════════════════════════════════════════════════════════
	// Step 3b + 4: Resume-aware commit handling
	// ═══════════════════════════════════════════════════════════════════════

	if resumeFromBlobs {
		// Some or all commits are already in the DB. Read them back and
		// rebuild the markToULID mapping with the real ULIDs (freshly
		// generated ones have different random entropy).

		dbCommitIDs, err := r.DB.GetAllCommitIDsOrdered(ctx)
		if err != nil {
			return fmt.Errorf("failed to read existing commits for resume: %w", err)
		}

		if len(dbCommitIDs) > len(commitEntries) {
			return util.NewError("Cannot resume — commit count mismatch").
				WithMessage(fmt.Sprintf("Database has %d commits but fast-export has %d commits",
					len(dbCommitIDs), len(commitEntries))).
				WithSuggestion("The repository may have changed since the original import.\n  pgit import --force  # Wipe and start fresh")
		}

		// Rebuild markToULID: for already-inserted commits use real DB ULIDs,
		// for remaining commits use the freshly generated ones from prepareCommits.
		alreadyInserted := len(dbCommitIDs)
		for i := 0; i < alreadyInserted; i++ {
			markToULID[commitEntries[i].Mark] = dbCommitIDs[i]
		}
		// markToULID[i >= alreadyInserted] keeps the freshly generated ULIDs
		// from prepareCommits — these are for commits we still need to insert.

		// Rebuild pathOps with the corrected ULIDs
		pathOps = make(map[string][]pathOp)
		for _, ce := range commitEntries {
			ulid := markToULID[ce.Mark]
			for _, op := range ce.FileOps {
				po := pathOp{
					CommitID: ulid,
					BlobMark: op.BlobMark,
					Mode:     op.Mode,
					IsDelete: op.Type == 'D',
				}
				pathOps[op.Path] = append(pathOps[op.Path], po)
			}
		}

		// Update pgitCommits to use the real ULIDs (needed for HEAD/checkout)
		for i := 0; i < alreadyInserted; i++ {
			pgitCommits[i].ID = dbCommitIDs[i]
			if pgitCommits[i].ParentID != nil {
				parentMark := commitEntries[i].FromMark
				if parentMark > 0 {
					realParentID := markToULID[parentMark]
					pgitCommits[i].ParentID = &realParentID
				}
			}
		}
		// For commits after alreadyInserted, fix their parent IDs too
		// (the parent of commit alreadyInserted might be in the DB)
		for i := alreadyInserted; i < len(pgitCommits); i++ {
			if pgitCommits[i].ParentID != nil {
				parentMark := commitEntries[i].FromMark
				if parentMark > 0 {
					realParentID := markToULID[parentMark]
					pgitCommits[i].ParentID = &realParentID
				}
			}
		}

		remaining := len(commitEntries) - alreadyInserted
		fmt.Printf("Rebuilt commit mapping: %s already inserted", ui.FormatCount(alreadyInserted))
		if remaining > 0 {
			fmt.Printf(", %s remaining\n", ui.FormatCount(remaining))
		} else {
			fmt.Println()
		}

		// Insert remaining commits if any
		if remaining > 0 {
			fmt.Println("\nImporting remaining commits...")

			remainingCommits := pgitCommits[alreadyInserted:]
			batchSize := 1000
			commitProgress := ui.NewProgress("Commits", len(remainingCommits))

			for i := 0; i < len(remainingCommits); i += batchSize {
				end := i + batchSize
				if end > len(remainingCommits) {
					end = len(remainingCommits)
				}
				batch := remainingCommits[i:end]

				if err := r.DB.CreateCommitsBatch(ctx, batch); err != nil {
					fmt.Println()
					return fmt.Errorf("failed to insert commits batch: %w", err)
				}
				commitProgress.Update(end)
			}
			commitProgress.Done()
		}

		// Mark commits as done (idempotent if already set)
		_ = r.DB.SetMetadata(ctx, "import_state", "commits_done")
	} else {
		// Fresh import: insert all commits
		_ = r.DB.SetMetadata(ctx, "import_branch", selectedBranch)
		_ = r.DB.SetMetadata(ctx, "import_expected_commits", fmt.Sprintf("%d", len(pgitCommits)))

		fmt.Println("\nImporting commits...")

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

		// Mark commits as done — this is the resume checkpoint
		_ = r.DB.SetMetadata(ctx, "import_state", "commits_done")
	}

	// ═══════════════════════════════════════════════════════════════════════
	// Step 5: Parallel blob import via ReadAt on temp file
	// ═══════════════════════════════════════════════════════════════════════

	// When resuming, filter out already-imported paths
	if resumeFromBlobs {
		importedPaths, err := r.DB.GetImportedPaths(ctx)
		if err != nil {
			return fmt.Errorf("failed to query imported paths: %w", err)
		}

		if len(importedPaths) > 0 {
			// Count ops being skipped
			skippedOps := 0
			for path, ops := range pathOps {
				if importedPaths[path] {
					skippedOps += len(ops)
					delete(pathOps, path)
				}
			}

			// Recalculate totalFileOps for remaining paths
			totalFileOps = 0
			for _, ops := range pathOps {
				totalFileOps += len(ops)
			}

			fmt.Printf("\nResuming: %s paths already imported, %s paths remaining\n",
				ui.FormatCount(len(importedPaths)),
				ui.FormatCount(len(pathOps)))
		}
	}

	fmt.Printf("\nImporting %s file versions across %s paths...\n",
		ui.FormatCount(totalFileOps), ui.FormatCount(len(pathOps)))

	if totalFileOps > 0 {
		err = importBlobsParallel(ctx, r.DB, tmpPath, pathOps, blobIndex, markToULID, workers, resumeFromBlobs)
		if err != nil {
			return err
		}
	}

	// ═══════════════════════════════════════════════════════════════════════
	// Step 6: Set HEAD
	// ═══════════════════════════════════════════════════════════════════════

	latestCommit := pgitCommits[len(pgitCommits)-1]
	if err := r.DB.SetHead(ctx, latestCommit.ID); err != nil {
		return fmt.Errorf("failed to set HEAD: %w", err)
	}

	// ═══════════════════════════════════════════════════════════════════════
	// Step 7: Checkout working tree (local only)
	// ═══════════════════════════════════════════════════════════════════════

	if !isRemote {
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
	}

	// Mark import as complete
	_ = r.DB.SetMetadata(ctx, "import_state", "complete")

	// Clean up temp file on success (preserved on crash for --fastexport reuse)
	if ownsTmpFile {
		os.Remove(tmpPath)
	}

	if isRemote {
		fmt.Printf("\n%s Imported %s commits to remote '%s'\n",
			styles.Green("Success!"),
			ui.FormatCount(len(commitEntries)),
			remoteName)
	} else {
		fmt.Printf("\n%s Imported %s commits from git repository\n",
			styles.Green("Success!"),
			ui.FormatCount(len(commitEntries)))
	}
	fmt.Printf("HEAD is now at %s %s\n",
		styles.Hash(latestCommit.ID, true),
		firstLine(latestCommit.Message))

	return nil
}

// ═══════════════════════════════════════════════════════════════════════════
// Phase 1: Export to temp file
// ═══════════════════════════════════════════════════════════════════════════

// exportToFile runs git fast-export and writes the stream to a temp file.
// Returns the temp file path, total bytes written, and error.
func exportToFile(gitPath, branch string) (string, int64, error) {
	cmd := exec.Command("git", "fast-export", "--reencode=yes", "--show-original-ids", branch)
	cmd.Dir = gitPath

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", 0, err
	}

	if err := cmd.Start(); err != nil {
		return "", 0, err
	}

	tmpFile, err := os.CreateTemp("", "pgit-import-*.fastexport")
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return "", 0, err
	}
	tmpPath := tmpFile.Name()

	n, copyErr := io.Copy(tmpFile, stdout)
	tmpFile.Close()

	if waitErr := cmd.Wait(); waitErr != nil {
		if copyErr != nil {
			return tmpPath, n, fmt.Errorf("git fast-export failed: %w (copy error: %v)", waitErr, copyErr)
		}
		return tmpPath, n, fmt.Errorf("git fast-export failed: %w", waitErr)
	}
	if copyErr != nil {
		return tmpPath, n, fmt.Errorf("failed to write export stream: %w", copyErr)
	}

	return tmpPath, n, nil
}

// ═══════════════════════════════════════════════════════════════════════════
// Phase 2: Index the fast-export stream
// ═══════════════════════════════════════════════════════════════════════════

// indexFastExport does a single-pass scan of the temp file and builds
// the blob index and commit entry list.
func indexFastExport(tmpPath string) ([]commitEntry, map[int]*blobEntry, error) {
	f, err := os.Open(tmpPath)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()

	reader := bufio.NewReaderSize(f, 4*1024*1024) // 4MB buffer

	var commits []commitEntry
	blobIdx := make(map[int]*blobEntry)

	// Track byte offset in the stream
	var offset int64

	// readLine reads a line and tracks offset. Returns line without trailing \n.
	readLine := func() (string, error) {
		line, err := reader.ReadString('\n')
		offset += int64(len(line))
		if err != nil {
			return strings.TrimSuffix(line, "\n"), err
		}
		return strings.TrimSuffix(line, "\n"), nil
	}

	// skipData reads and skips exactly n bytes of data content plus optional trailing LF.
	// Returns the byte offset where the data starts.
	skipData := func(n int) (int64, error) {
		dataOffset := offset
		// Skip n bytes
		remaining := n
		for remaining > 0 {
			skipped, err := reader.Discard(min(remaining, 4*1024*1024))
			offset += int64(skipped)
			remaining -= skipped
			if err != nil {
				return dataOffset, err
			}
		}
		// Skip optional trailing LF
		b, err := reader.ReadByte()
		if err == nil {
			offset++
			if b != '\n' {
				_ = reader.UnreadByte()
				offset--
			}
		}
		return dataOffset, nil
	}

	for {
		line, err := readLine()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, nil, fmt.Errorf("read error at offset %d: %w", offset, err)
		}

		switch {
		case line == "blob":
			be := &blobEntry{}
			// Read blob metadata lines until data
			for {
				bline, err := readLine()
				if err != nil {
					return nil, nil, fmt.Errorf("unexpected EOF in blob at offset %d", offset)
				}
				if strings.HasPrefix(bline, "mark :") {
					be.Mark, _ = strconv.Atoi(bline[6:])
				} else if strings.HasPrefix(bline, "original-oid ") {
					be.OriginalID = bline[13:]
				} else if strings.HasPrefix(bline, "data ") {
					size, _ := strconv.Atoi(bline[5:])
					be.Size = size
					dataOffset, err := skipData(size)
					if err != nil {
						return nil, nil, fmt.Errorf("error skipping blob data at offset %d: %w", offset, err)
					}
					be.Offset = dataOffset
					break
				}
			}
			if be.Mark > 0 {
				blobIdx[be.Mark] = be
			}

		case strings.HasPrefix(line, "commit "):
			ce := commitEntry{}
			// Read commit metadata
			for {
				cline, err := readLine()
				if err != nil {
					return nil, nil, fmt.Errorf("unexpected EOF in commit at offset %d", offset)
				}

				if strings.HasPrefix(cline, "mark :") {
					ce.Mark, _ = strconv.Atoi(cline[6:])
				} else if strings.HasPrefix(cline, "original-oid ") {
					ce.OriginalID = cline[13:]
				} else if strings.HasPrefix(cline, "author ") {
					ce.AuthorName, ce.AuthorEmail, ce.AuthorTimestamp, ce.AuthorTZ = parseIdentity(cline[7:])
				} else if strings.HasPrefix(cline, "committer ") {
					ce.CommitterName, ce.CommitterEmail, ce.CommitterTimestamp, ce.CommitterTZ = parseIdentity(cline[10:])
				} else if strings.HasPrefix(cline, "data ") {
					size, _ := strconv.Atoi(cline[5:])
					ce.MessageSize = size
					dataOffset, err := skipData(size)
					if err != nil {
						return nil, nil, fmt.Errorf("error skipping commit message at offset %d: %w", offset, err)
					}
					ce.MessageOffset = dataOffset
					// After commit message, read file ops and from/merge
					break
				}
			}

			// Read from, merge, and file ops
			for {
				cline, err := readLine()
				if err == io.EOF {
					break
				}
				if err != nil {
					return nil, nil, fmt.Errorf("error reading commit ops at offset %d: %w", offset, err)
				}

				if cline == "" {
					// Empty line signals end of commit
					break
				}

				if strings.HasPrefix(cline, "from :") {
					ce.FromMark, _ = strconv.Atoi(cline[6:])
				} else if strings.HasPrefix(cline, "merge :") {
					if ce.MergeMark == 0 {
						ce.MergeMark, _ = strconv.Atoi(cline[7:])
					}
					// Additional merge parents are ignored (pgit tracks single parent)
				} else if strings.HasPrefix(cline, "M ") {
					op := parseFileModify(cline)
					if op.Path != "" {
						ce.FileOps = append(ce.FileOps, op)
					}
				} else if strings.HasPrefix(cline, "D ") {
					path := unquotePath(cline[2:])
					if path != "" {
						ce.FileOps = append(ce.FileOps, fileOp{
							Type: 'D',
							Path: path,
						})
					}
				} else if strings.HasPrefix(cline, "R ") {
					// Rename: decompose to D old + M new
					// Shouldn't appear without -M flag, but handle gracefully
					parts := strings.SplitN(cline[2:], " ", 2)
					if len(parts) == 2 {
						oldPath := unquotePath(parts[0])
						newPath := unquotePath(parts[1])
						ce.FileOps = append(ce.FileOps, fileOp{Type: 'D', Path: oldPath})
						// Note: R doesn't carry a blob mark — the new path gets the old blob
						// This shouldn't happen without -M, but if it does we can't resolve it here
						_ = newPath
					}
				} else if cline == "deleteall" { //nolint:staticcheck // deleteall: rare in practice, skip for now
				}
			}

			commits = append(commits, ce)

		case strings.HasPrefix(line, "reset "):
			// reset line — skip
			continue

		case strings.HasPrefix(line, "progress "):
			// progress line — skip
			continue

		case line == "done":
			break

		case line == "":
			continue
		}
	}

	return commits, blobIdx, nil
}

// parseIdentity parses a fast-export author/committer line.
// Format: "Name <email> timestamp tz"
// Example: "John Doe <john@example.com> 1234567890 +0100"
func parseIdentity(s string) (name, email string, timestamp int64, tz string) {
	// Find < and >
	ltIdx := strings.LastIndex(s, " <")
	gtIdx := strings.LastIndex(s, "> ")
	if ltIdx < 0 || gtIdx < 0 || gtIdx <= ltIdx {
		return s, "", 0, ""
	}

	name = s[:ltIdx]
	email = s[ltIdx+2 : gtIdx]

	rest := s[gtIdx+2:]
	parts := strings.Fields(rest)
	if len(parts) >= 1 {
		timestamp, _ = strconv.ParseInt(parts[0], 10, 64)
	}
	if len(parts) >= 2 {
		tz = parts[1]
	}

	return name, email, timestamp, tz
}

// parseFileModify parses "M <mode> :<mark> <path>" line.
func parseFileModify(line string) fileOp {
	// "M 100644 :5 path/to/file"
	// "M 100644 :5 "quoted path""
	parts := strings.SplitN(line, " ", 4)
	if len(parts) < 4 {
		return fileOp{}
	}

	mode, _ := strconv.ParseInt(parts[1], 8, 32)
	markStr := parts[2]
	mark := 0
	if strings.HasPrefix(markStr, ":") {
		mark, _ = strconv.Atoi(markStr[1:])
	}
	path := unquotePath(parts[3])

	return fileOp{
		Type:     'M',
		Mode:     int(mode),
		BlobMark: mark,
		Path:     path,
	}
}

// unquotePath handles C-style quoted paths from fast-export.
// If the path starts with ", parse escape sequences.
func unquotePath(s string) string {
	if len(s) < 2 || s[0] != '"' {
		return s // unquoted path
	}
	// Strip outer quotes
	if s[len(s)-1] != '"' {
		return s // malformed, return as-is
	}
	inner := s[1 : len(s)-1]

	var buf strings.Builder
	buf.Grow(len(inner))

	i := 0
	for i < len(inner) {
		if inner[i] == '\\' && i+1 < len(inner) {
			i++
			switch inner[i] {
			case '\\':
				buf.WriteByte('\\')
			case '"':
				buf.WriteByte('"')
			case 'n':
				buf.WriteByte('\n')
			case 't':
				buf.WriteByte('\t')
			case 'a':
				buf.WriteByte('\a')
			case 'b':
				buf.WriteByte('\b')
			case 'f':
				buf.WriteByte('\f')
			case 'r':
				buf.WriteByte('\r')
			case 'v':
				buf.WriteByte('\v')
			default:
				// Octal: \NNN (1-3 digits)
				if inner[i] >= '0' && inner[i] <= '7' {
					oct := string(inner[i])
					for j := 1; j < 3 && i+j < len(inner) && inner[i+j] >= '0' && inner[i+j] <= '7'; j++ {
						oct += string(inner[i+j])
					}
					val, _ := strconv.ParseInt(oct, 8, 32)
					buf.WriteByte(byte(val))
					i += len(oct) - 1 // -1 because the loop will i++
				} else {
					buf.WriteByte('\\')
					buf.WriteByte(inner[i])
				}
			}
		} else {
			buf.WriteByte(inner[i])
		}
		i++
	}

	return buf.String()
}

// ═══════════════════════════════════════════════════════════════════════════
// Phase 3: Prepare commits
// ═══════════════════════════════════════════════════════════════════════════

// prepareCommits assigns ULIDs, builds db.Commit objects, and groups file ops by path.
func prepareCommits(
	commitEntries []commitEntry,
	blobIndex map[int]*blobEntry,
	tmpFile *os.File,
) ([]*db.Commit, map[int]string, map[string][]pathOp) {

	markToULID := make(map[int]string, len(commitEntries))
	pgitCommits := make([]*db.Commit, 0, len(commitEntries))

	var lastTime time.Time

	for _, ce := range commitEntries {
		authorTime := time.Unix(ce.AuthorTimestamp, 0)
		if !authorTime.After(lastTime) {
			authorTime = lastTime.Add(time.Millisecond)
		}
		lastTime = authorTime

		ulid := util.NewULIDWithTime(authorTime)
		markToULID[ce.Mark] = ulid

		var parentID *string
		if ce.FromMark > 0 {
			if pid, ok := markToULID[ce.FromMark]; ok {
				parentID = &pid
			}
		}

		// Read message from temp file
		message := readBytesAt(tmpFile, ce.MessageOffset, ce.MessageSize)

		committedTime := time.Unix(ce.CommitterTimestamp, 0)

		pgitCommits = append(pgitCommits, &db.Commit{
			ID:             ulid,
			ParentID:       parentID,
			TreeHash:       ce.OriginalID[:min(8, len(ce.OriginalID))],
			Message:        util.ToValidUTF8(string(message)),
			AuthorName:     util.ToValidUTF8(ce.AuthorName),
			AuthorEmail:    util.ToValidUTF8(ce.AuthorEmail),
			AuthoredAt:     authorTime,
			CommitterName:  util.ToValidUTF8(ce.CommitterName),
			CommitterEmail: util.ToValidUTF8(ce.CommitterEmail),
			CommittedAt:    committedTime,
		})
	}

	// Group file ops by path (ops are already in correct order since we iterate in stream order)
	pathOpsMap := make(map[string][]pathOp)
	for _, ce := range commitEntries {
		ulid := markToULID[ce.Mark]
		for _, op := range ce.FileOps {
			po := pathOp{
				CommitID: ulid,
				BlobMark: op.BlobMark,
				Mode:     op.Mode,
				IsDelete: op.Type == 'D',
			}
			pathOpsMap[op.Path] = append(pathOpsMap[op.Path], po)
		}
	}

	return pgitCommits, markToULID, pathOpsMap
}

// readBytesAt reads exactly n bytes from f at the given offset.
func readBytesAt(f *os.File, offset int64, n int) []byte {
	if n <= 0 {
		return nil
	}
	buf := make([]byte, n)
	_, err := f.ReadAt(buf, offset)
	if err != nil {
		return nil
	}
	return buf
}

// ═══════════════════════════════════════════════════════════════════════════
// Phase 5: Parallel blob import
// ═══════════════════════════════════════════════════════════════════════════

// pathBatch groups multiple paths for batched DB insertion.
type pathBatch struct {
	paths []string
}

// importBlobsParallel imports blobs grouped by path using parallel workers.
// Each worker processes batches of paths for fewer, larger transactions.
// Content is read from the temp file via ReadAt (safe for concurrent access).
//
// Optimizations over the naive per-path approach:
//   - Pre-registers all paths upfront (1 transaction instead of N)
//   - Batches multiple paths per transaction (fewer COMMITs = less WAL)
//   - Skips MAX(version_id) query for fresh imports (starts at 0)
//   - Sorts paths by first blob offset for sequential I/O on temp file
//   - Uses sync.Pool for byte buffer reuse to reduce GC pressure
func importBlobsParallel(
	ctx context.Context,
	database *db.DB,
	tmpFilePath string,
	pathOpsMap map[string][]pathOp,
	blobIndex map[int]*blobEntry,
	markToULID map[int]string,
	workers int,
	isResume bool,
) error {
	// Open temp file for concurrent reads
	tmpFile, err := os.Open(tmpFilePath)
	if err != nil {
		return fmt.Errorf("failed to open temp file for reading: %w", err)
	}
	defer tmpFile.Close()

	// Collect paths and sort by version count descending (heaviest first),
	// then interleave so heavy and light paths alternate. This prevents
	// the "tail stall" where one worker gets stuck on a huge path (e.g.
	// Makefile with 3,681 versions) while all other workers sit idle.
	//
	// Strategy: sort desc by version count, split into odds/evens,
	// reverse the evens, then zip them back together. Result: heavy
	// paths are spread across the batch stream so workers pick them
	// up early and in parallel.
	paths := make([]string, 0, len(pathOpsMap))
	for path := range pathOpsMap {
		paths = append(paths, path)
	}
	sort.Slice(paths, func(i, j int) bool {
		return len(pathOpsMap[paths[i]]) > len(pathOpsMap[paths[j]])
	})
	paths = interleavePaths(paths)

	// Pre-register all paths in one bulk operation
	pathToGroupID, err := database.PreRegisterPaths(ctx, paths)
	if err != nil {
		return fmt.Errorf("failed to pre-register paths: %w", err)
	}

	// Initialize version counters for each group
	// For fresh imports: all start at 0 (CreateBlobsBatchFast increments before use)
	// For resume: query existing max version_ids
	versionCounters := make(map[int32]*int32, len(pathToGroupID))
	if isResume {
		// Query existing max version_ids
		groupIDs := make([]int32, 0, len(pathToGroupID))
		for _, gid := range pathToGroupID {
			groupIDs = append(groupIDs, gid)
		}
		// Batch query in chunks to avoid huge ANY() arrays
		maxVersions := make(map[int32]int32)
		const chunkSize = 5000
		for i := 0; i < len(groupIDs); i += chunkSize {
			end := i + chunkSize
			if end > len(groupIDs) {
				end = len(groupIDs)
			}
			chunk := groupIDs[i:end]
			rows, err := database.Query(ctx,
				"SELECT group_id, COALESCE(MAX(version_id), 0) FROM pgit_file_refs WHERE group_id = ANY($1) GROUP BY group_id",
				chunk,
			)
			if err != nil {
				return fmt.Errorf("failed to query max versions: %w", err)
			}
			for rows.Next() {
				var gid, maxV int32
				if err := rows.Scan(&gid, &maxV); err != nil {
					rows.Close()
					return err
				}
				maxVersions[gid] = maxV
			}
			rows.Close()
		}
		for _, gid := range groupIDs {
			v := maxVersions[gid] // 0 if not found
			versionCounters[gid] = &v
		}
	} else {
		// Fresh import: all groups start at 0
		for _, gid := range pathToGroupID {
			v := int32(0)
			versionCounters[gid] = &v
		}
	}

	// Count total ops for progress
	totalOps := 0
	for _, ops := range pathOpsMap {
		totalOps += len(ops)
	}

	progress := ui.NewProgress("Blobs", totalOps)
	var imported atomic.Int64

	// Create batches of paths, capped by total blob count.
	// Small batches = more parallelism. Large batches = fewer commits.
	// Target: ~100 blobs per transaction — enough to amortize commit overhead
	// without blocking a connection too long on delta encoding.
	const maxBlobsPerBatch = 100
	var batches []pathBatch
	var currentBatch pathBatch
	currentBlobCount := 0

	for _, p := range paths {
		opCount := len(pathOpsMap[p])
		// If a single path exceeds the limit, it gets its own batch
		if currentBlobCount > 0 && currentBlobCount+opCount > maxBlobsPerBatch {
			batches = append(batches, currentBatch)
			currentBatch = pathBatch{}
			currentBlobCount = 0
		}
		currentBatch.paths = append(currentBatch.paths, p)
		currentBlobCount += opCount
	}
	if len(currentBatch.paths) > 0 {
		batches = append(batches, currentBatch)
	}

	batchChan := make(chan pathBatch, len(batches))
	for _, b := range batches {
		batchChan <- b
	}
	close(batchChan)

	var firstErr atomic.Pointer[error]
	var wg sync.WaitGroup

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for batch := range batchChan {
				if firstErr.Load() != nil {
					return
				}

				// Flatten all ops for this batch with their path, preserving order.
				// We stream content in sub-batches to avoid loading everything
				// into memory at once (a path with 3,681 versions * 128KB = 460MB).
				type flatOp struct {
					path string
					op   pathOp
				}
				var flatOps []flatOp
				for _, path := range batch.paths {
					for _, op := range pathOpsMap[path] {
						flatOps = append(flatOps, flatOp{path: path, op: op})
					}
				}

				// Process in sub-batches of 200 — read content, insert, update progress
				const subBatchSize = 200
				for i := 0; i < len(flatOps); i += subBatchSize {
					if firstErr.Load() != nil {
						return
					}
					end := i + subBatchSize
					if end > len(flatOps) {
						end = len(flatOps)
					}

					chunk := make([]*db.Blob, 0, end-i)
					for _, fo := range flatOps[i:end] {
						if fo.op.IsDelete {
							chunk = append(chunk, &db.Blob{
								Path:        fo.path,
								CommitID:    fo.op.CommitID,
								Content:     []byte{},
								ContentHash: nil,
								Mode:        0,
								IsSymlink:   false,
								IsBinary:    false,
							})
							continue
						}

						be, ok := blobIndex[fo.op.BlobMark]
						if !ok {
							continue
						}

						content := make([]byte, be.Size)
						if be.Size > 0 {
							_, err := tmpFile.ReadAt(content, be.Offset)
							if err != nil {
								readErr := fmt.Errorf("failed to read blob content at offset %d: %w", be.Offset, err)
								firstErr.CompareAndSwap(nil, &readErr)
								return
							}
						}

						isBinary := util.DetectBinary(content)
						contentHash := util.HashBytesBlake3(content)

						blob := &db.Blob{
							Path:        fo.path,
							CommitID:    fo.op.CommitID,
							Content:     content,
							ContentHash: contentHash,
							Mode:        fo.op.Mode,
							IsSymlink:   fo.op.Mode == 0120000,
							IsBinary:    isBinary,
						}

						if blob.IsSymlink {
							target := string(content)
							blob.SymlinkTarget = &target
						}

						chunk = append(chunk, blob)
					}

					if len(chunk) > 0 {
						if err := database.CreateBlobsBatchFast(ctx, chunk, pathToGroupID, versionCounters); err != nil {
							firstErr.CompareAndSwap(nil, &err)
							return
						}
					}

					count := imported.Add(int64(len(chunk)))
					progress.Update(int(count))
				}
			}
		}()
	}

	wg.Wait()
	progress.Done()

	if errPtr := firstErr.Load(); errPtr != nil {
		return *errPtr
	}

	return nil
}

// interleavePaths takes a list sorted by weight (heaviest first) and
// interleaves so that heavy and light paths alternate. This ensures
// workers get a balanced mix and no single worker gets stuck with all
// the heavy paths at the end.
//
// Example: [A B C D E F G H] (sorted heaviest first)
//
//	odds  = [A C E G]       (indices 0,2,4,6)
//	evens = [B D F H]       (indices 1,3,5,7) -> reversed: [H F D B]
//	result = [A H C F E D G B]
func interleavePaths(paths []string) []string {
	if len(paths) <= 2 {
		return paths
	}

	var odds, evens []string
	for i, p := range paths {
		if i%2 == 0 {
			odds = append(odds, p)
		} else {
			evens = append(evens, p)
		}
	}

	// Reverse evens
	for i, j := 0, len(evens)-1; i < j; i, j = i+1, j-1 {
		evens[i], evens[j] = evens[j], evens[i]
	}

	// Zip together
	result := make([]string, 0, len(paths))
	oi, ei := 0, 0
	for oi < len(odds) || ei < len(evens) {
		if oi < len(odds) {
			result = append(result, odds[oi])
			oi++
		}
		if ei < len(evens) {
			result = append(result, evens[ei])
			ei++
		}
	}

	return result
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
