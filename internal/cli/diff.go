package cli

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/imgajeed76/pgit/v2/internal/db"
	"github.com/imgajeed76/pgit/v2/internal/repo"
	"github.com/imgajeed76/pgit/v2/internal/ui/styles"
	"github.com/imgajeed76/pgit/v2/internal/util"
	"github.com/spf13/cobra"
)

func newDiffCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "diff [<commit>] [<commit>..<commit>] [--] [<path>...]",
		Short: "Show changes between commits, working tree, etc",
		Long: `Show changes between the working tree and the staging area,
or between commits.

Without arguments, shows unstaged changes.
With --staged, shows changes staged for the next commit.

Commit range syntax:
  pgit diff <commit>           # Changes from commit to working tree
  pgit diff <commit1>..<commit2>  # Changes between two commits
  pgit diff HEAD~3..HEAD       # Changes in last 3 commits

Use -- to separate commits from paths:
  pgit diff HEAD -- file.txt   # Changes to file.txt since HEAD`,
		RunE: runDiff,
	}

	cmd.Flags().Bool("staged", false, "Show staged changes")
	cmd.Flags().Bool("cached", false, "Alias for --staged")
	cmd.Flags().Bool("name-only", false, "Show only names of changed files")
	cmd.Flags().Bool("name-status", false, "Show names and status of changed files")
	cmd.Flags().Bool("stat", false, "Show diffstat summary")
	cmd.Flags().Bool("no-color", false, "Disable colored output")

	return cmd
}

func runDiff(cmd *cobra.Command, args []string) error {
	staged, _ := cmd.Flags().GetBool("staged")
	cached, _ := cmd.Flags().GetBool("cached")
	nameOnly, _ := cmd.Flags().GetBool("name-only")
	nameStatus, _ := cmd.Flags().GetBool("name-status")
	stat, _ := cmd.Flags().GetBool("stat")
	noColor, _ := cmd.Flags().GetBool("no-color")

	if cached {
		staged = true
	}

	r, err := repo.Open()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Connect to database
	if err := r.Connect(ctx); err != nil {
		return err
	}
	defer r.Close()

	// Parse args: look for -- separator and commit..commit syntax
	// Note: cobra passes everything after -- as args, but doesn't include -- itself
	// So we need to use cmd.ArgsLenAtDash() to detect if -- was used
	var commits []string
	var paths []string
	dashAt := cmd.ArgsLenAtDash()
	foundSeparator := dashAt >= 0

	if foundSeparator {
		commits = args[:dashAt]
		paths = args[dashAt:]
	} else {
		commits = args
	}

	// Check for commit range syntax (commit1..commit2)
	var fromCommit, toCommit string
	if len(commits) == 1 && strings.Contains(commits[0], "..") {
		parts := strings.SplitN(commits[0], "..", 2)
		fromCommit = parts[0]
		toCommit = parts[1]
		if toCommit == "" {
			toCommit = "HEAD"
		}
		if fromCommit == "" {
			fromCommit = "HEAD"
		}
	} else if len(commits) == 1 && !foundSeparator {
		// Single commit without -- separator: diff from that commit to working tree
		fromCommit = commits[0]
	} else if len(commits) == 1 && foundSeparator {
		// Single commit with -- separator: diff from that commit, filter by paths
		fromCommit = commits[0]
	} else if len(commits) == 2 {
		// Two commits specified
		fromCommit = commits[0]
		toCommit = commits[1]
	}

	// If we have commit refs, do commit-to-commit diff
	if fromCommit != "" {
		return runCommitDiff(ctx, r, fromCommit, toCommit, paths, nameOnly, nameStatus, stat, noColor)
	}

	// Standard working tree diff
	opts := repo.DiffOptions{
		Staged:     staged,
		NameOnly:   nameOnly,
		NameStatus: nameStatus,
		NoColor:    noColor,
	}

	// Use paths from -- separator if present, otherwise use commits as paths
	if len(paths) > 0 {
		opts.Path = paths[0]
	} else if len(commits) > 0 && !foundSeparator && !strings.Contains(commits[0], "..") {
		// If no -- separator and arg doesn't look like a commit range, treat as path
		opts.Path = commits[0]
	}

	results, err := r.Diff(ctx, opts)
	if err != nil {
		return err
	}

	if len(results) == 0 {
		fmt.Println(styles.Mute("No changes."))
		return nil
	}

	if stat {
		return printDiffStat(results, noColor)
	}

	for _, result := range results {
		if nameOnly {
			fmt.Println(result.Path)
		} else if nameStatus {
			fmt.Printf("%s\t%s\n", result.Status.Symbol(), result.Path)
		} else {
			fmt.Print(repo.FormatDiff(result, noColor))
		}
	}

	return nil
}

// runCommitDiff shows diff between commits or commit to working tree.
// Instead of materializing full trees (which decompresses all xpatch content),
// we identify changed paths first via pgit_file_refs (normal table), then
// fetch content only for those paths using scoped xpatch queries.
func runCommitDiff(ctx context.Context, r *repo.Repository, fromRef, toRef string, paths []string, nameOnly, nameStatus, stat, noColor bool) error {
	// Resolve commit refs
	fromID, err := resolveCommitRef(ctx, r, fromRef)
	if err != nil {
		return fmt.Errorf("cannot resolve '%s': %w", fromRef, err)
	}

	var toID string
	if toRef != "" {
		toID, err = resolveCommitRef(ctx, r, toRef)
		if err != nil {
			return fmt.Errorf("cannot resolve '%s': %w", toRef, err)
		}
	}

	var results []repo.DiffResult

	if toID != "" {
		// Commit-to-commit diff: get only changed paths between the two commits,
		// then fetch old/new content per file via scoped xpatch queries.
		changedMeta, err := r.DB.GetChangedFilesMetadata(ctx, fromID, toID)
		if err != nil {
			return err
		}

		// Deduplicate: a file may be changed in multiple commits between from..to.
		// We only need the unique paths.
		changedPaths := make(map[string]bool)
		for _, b := range changedMeta {
			if len(paths) > 0 && !matchesAnyPath(b.Path, paths) {
				continue
			}
			changedPaths[b.Path] = true
		}

		for path := range changedPaths {
			oldBlob, _ := r.DB.GetFileAtCommit(ctx, path, fromID)
			newBlob, _ := r.DB.GetFileAtCommit(ctx, path, toID)

			var result repo.DiffResult
			result.Path = path

			oldContent := ""
			newContent := ""
			if oldBlob != nil {
				oldContent = string(oldBlob.Content)
			}
			if newBlob != nil {
				newContent = string(newBlob.Content)
			}

			if oldContent == newContent {
				continue // no actual change (e.g., added then reverted)
			}

			result.OldContent = oldContent
			result.NewContent = newContent

			if oldBlob == nil {
				result.Status = repo.StatusNew
			} else if newBlob == nil {
				result.Status = repo.StatusDeleted
			} else {
				result.Status = repo.StatusModified
			}

			if !nameOnly && !nameStatus {
				result.Hunks = repo.GenerateHunks(result.OldContent, result.NewContent, 3)
			}
			results = append(results, result)
		}
	} else {
		// Commit-to-working-tree diff: get tree metadata (no content, fast)
		// then compare hashes against working tree files.
		treeMeta, err := r.DB.GetTreeMetadataAtCommit(ctx, fromID)
		if err != nil {
			return err
		}

		fromMap := make(map[string]*db.Blob)
		for _, b := range treeMeta {
			fromMap[b.Path] = b
		}

		// Check each tracked file against working tree
		for path := range fromMap {
			if len(paths) > 0 && !matchesAnyPath(path, paths) {
				continue
			}
			absPath := r.AbsPath(path)
			wtContent, err := os.ReadFile(absPath)
			if err != nil {
				// File deleted in working tree
				oldBlob, err := r.DB.GetFileAtCommit(ctx, path, fromID)
				if err != nil || oldBlob == nil {
					continue
				}
				result := repo.DiffResult{
					Path:       path,
					Status:     repo.StatusDeleted,
					OldContent: string(oldBlob.Content),
				}
				if !nameOnly && !nameStatus {
					result.Hunks = repo.GenerateHunks(result.OldContent, "", 3)
				}
				results = append(results, result)
				continue
			}

			// Quick hash check to skip unchanged files without fetching content
			wtHash := util.HashBytesBlake3(wtContent)
			if util.ContentHashEqual(wtHash, fromMap[path].ContentHash) {
				continue
			}

			// Content differs — fetch old content
			oldBlob, err := r.DB.GetFileAtCommit(ctx, path, fromID)
			if err != nil || oldBlob == nil {
				continue
			}
			result := repo.DiffResult{
				Path:       path,
				Status:     repo.StatusModified,
				OldContent: string(oldBlob.Content),
				NewContent: string(wtContent),
			}
			if !nameOnly && !nameStatus {
				result.Hunks = repo.GenerateHunks(result.OldContent, result.NewContent, 3)
			}
			results = append(results, result)
		}

		// Check for new files in working directory
		if len(paths) > 0 {
			for _, p := range paths {
				if _, exists := fromMap[p]; !exists {
					absPath := r.AbsPath(p)
					content, err := os.ReadFile(absPath)
					if err == nil {
						result := repo.DiffResult{
							Path:       p,
							Status:     repo.StatusNew,
							NewContent: string(content),
						}
						if !nameOnly && !nameStatus {
							result.Hunks = repo.GenerateHunks("", result.NewContent, 3)
						}
						results = append(results, result)
					}
				}
			}
		}
	}

	if len(results) == 0 {
		fmt.Println(styles.Mute("No changes."))
		return nil
	}

	if stat {
		return printDiffStat(results, noColor)
	}

	for _, result := range results {
		if nameOnly {
			fmt.Println(result.Path)
		} else if nameStatus {
			fmt.Printf("%s\t%s\n", result.Status.Symbol(), result.Path)
		} else {
			fmt.Print(repo.FormatDiff(result, noColor))
		}
	}

	return nil
}

func matchesAnyPath(path string, patterns []string) bool {
	for _, pattern := range patterns {
		if path == pattern || strings.HasPrefix(path, pattern+"/") {
			return true
		}
	}
	return false
}

func printDiffStat(results []repo.DiffResult, noColor bool) error {
	var totalInsertions, totalDeletions int

	for _, result := range results {
		insertions := 0
		deletions := 0

		for _, hunk := range result.Hunks {
			for _, line := range hunk.Lines {
				switch line.Type {
				case repo.DiffLineAdd:
					insertions++
				case repo.DiffLineDelete:
					deletions++
				}
			}
		}

		// Display stat line
		total := insertions + deletions
		barWidth := min(total, 40)
		addWidth := 0
		delWidth := 0
		if total > 0 {
			addWidth = (insertions * barWidth) / total
			delWidth = barWidth - addWidth
		}

		bar := strings.Repeat("+", addWidth) + strings.Repeat("-", delWidth)
		if !noColor {
			bar = styles.Green(strings.Repeat("+", addWidth)) + styles.Red(strings.Repeat("-", delWidth))
		}

		fmt.Printf(" %s | %d %s\n", result.Path, total, bar)
		totalInsertions += insertions
		totalDeletions += deletions
	}

	fmt.Println()
	if noColor {
		fmt.Printf(" %d file(s) changed, %d insertions(+), %d deletions(-)\n",
			len(results), totalInsertions, totalDeletions)
	} else {
		fmt.Printf(" %d file(s) changed, %s, %s\n",
			len(results),
			styles.Green(fmt.Sprintf("%d insertions(+)", totalInsertions)),
			styles.Red(fmt.Sprintf("%d deletions(-)", totalDeletions)))
	}

	return nil
}
