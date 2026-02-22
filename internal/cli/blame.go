package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/imgajeed76/pgit/v4/internal/ui/styles"
	"github.com/imgajeed76/pgit/v4/internal/util"
	"github.com/spf13/cobra"
)

func newBlameCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "blame <file>",
		Short: "Show what revision and author last modified each line",
		Long: `Show what revision and author last modified each line of a file.

For each line, shows:
  - Short commit hash
  - Author name
  - Date
  - Line number
  - Line content`,
		Args: cobra.ExactArgs(1),
		RunE: runBlame,
	}
	cmd.Flags().String("remote", "", "Blame file on a remote database (e.g. 'origin')")
	return cmd
}

func runBlame(cmd *cobra.Command, args []string) error {
	path := args[0]

	remoteName, _ := cmd.Flags().GetString("remote")

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	r, err := connectForCommand(ctx, remoteName)
	if err != nil {
		return err
	}
	defer r.Close()

	// Get current file content at HEAD
	headID, err := r.DB.GetHead(ctx)
	if err != nil {
		return err
	}
	if headID == "" {
		return util.ErrNoCommits
	}

	currentBlob, err := r.DB.GetFileAtCommit(ctx, path, headID)
	if err != nil {
		return err
	}
	if currentBlob == nil {
		return util.ErrFileNotFound
	}

	// Get file ref history (metadata only, from pgit_file_refs — normal table, fast).
	// Ordered by commit_id DESC (newest first).
	pathID, groupID, err := r.DB.GetPathIDAndGroupIDByPath(ctx, path)
	if err != nil {
		return err
	}
	if pathID == 0 {
		return util.ErrFileNotFound
	}
	refs, err := r.DB.GetFileRefHistory(ctx, pathID)
	if err != nil {
		return err
	}
	if len(refs) == 0 {
		return util.ErrFileNotFound
	}

	// Split content into lines
	lines := strings.Split(string(currentBlob.Content), "\n")

	// Build blame info for each line
	type blameInfo struct {
		commitID    string
		authorName  string
		date        time.Time
		lineContent string
	}

	blameLines := make([]blameInfo, len(lines))
	pinned := make([]bool, len(lines))
	pinnedCount := 0

	for i, line := range lines {
		blameLines[i] = blameInfo{lineContent: line}
	}

	// Check if the file is binary (determines which content table to query).
	isBinary := false
	for _, ref := range refs {
		if ref.IsBinary {
			isBinary = true
			break
		}
	}

	// Fetch all content versions in a single query, front-to-back (ASC).
	// This is the fastest xpatch access pattern: one Index Scan through the
	// delta chain, decompressing sequentially. We then iterate in reverse in
	// Go for the blame algorithm (newest→oldest, pinning lines as they diverge).
	allContent, err := r.DB.GetAllContentForGroup(ctx, groupID, isBinary)
	if err != nil {
		return err
	}

	// Build version_id → content lookup
	contentByVersion := make(map[int32][]byte, len(allContent))
	for _, cv := range allContent {
		contentByVersion[cv.VersionID] = cv.Content
	}

	// Iterate refs newest→oldest (refs are already ordered by commit_id DESC).
	// Algorithm:
	//   - Start at the newest version: attribute all matching lines to it.
	//   - Move to the previous (older) version: lines that still match get
	//     re-attributed (they existed earlier). Lines that differ get "pinned"
	//     to the newer version (that's where they were introduced).
	//   - Repeat until all lines are pinned or history is exhausted.
	for _, ref := range refs {
		if pinnedCount >= len(lines) {
			break
		}

		if ref.ContentHash == nil {
			// File was deleted at this point — all remaining unpinned lines
			// were introduced after this deletion, pin them.
			for i := range lines {
				if !pinned[i] {
					pinned[i] = true
					pinnedCount++
				}
			}
			break
		}

		content, ok := contentByVersion[ref.VersionID]
		if !ok || content == nil {
			continue
		}
		commitLines := strings.Split(string(content), "\n")

		for i := range lines {
			if pinned[i] {
				continue
			}
			if i < len(commitLines) && commitLines[i] == blameLines[i].lineContent {
				// Line exists at this version — (re-)attribute to this commit
				blameLines[i].commitID = ref.CommitID
			} else {
				// Line doesn't exist at this version — pin to the previously
				// attributed commit (the newer version that introduced it)
				pinned[i] = true
				pinnedCount++
			}
		}
	}

	// Collect unique commit IDs that were attributed, then batch-fetch metadata.
	// This is a small set (typically < 100 unique commits in a blame output).
	commitIDSet := make(map[string]bool)
	for _, bl := range blameLines {
		if bl.commitID != "" {
			commitIDSet[bl.commitID] = true
		}
	}
	uniqueIDs := make([]string, 0, len(commitIDSet))
	for id := range commitIDSet {
		uniqueIDs = append(uniqueIDs, id)
	}

	commitMap, err := r.DB.GetCommitsBatchByRange(ctx, uniqueIDs)
	if err != nil {
		return err
	}

	// Fill in author/date from commit metadata
	for i := range blameLines {
		if c, ok := commitMap[blameLines[i].commitID]; ok && c != nil {
			blameLines[i].authorName = c.AuthorName
			blameLines[i].date = c.AuthoredAt
		}
	}

	// Find max author name length for alignment
	maxAuthorLen := 0
	for _, bl := range blameLines {
		if len(bl.authorName) > maxAuthorLen {
			maxAuthorLen = len(bl.authorName)
		}
	}
	if maxAuthorLen > 20 {
		maxAuthorLen = 20
	}

	// Print blame output
	lineNumWidth := len(fmt.Sprintf("%d", len(blameLines)))

	for i, bl := range blameLines {
		shortHash := "0000000"
		author := "Unknown"
		dateStr := "          "

		if bl.commitID != "" {
			shortHash = util.ShortID(bl.commitID)
			author = bl.authorName
			if len(author) > maxAuthorLen {
				author = author[:maxAuthorLen]
			}
			dateStr = bl.date.Format("2006-01-02")
		}

		// Pad author name
		author = fmt.Sprintf("%-*s", maxAuthorLen, author)

		fmt.Printf("%s %s %s %*d) %s\n",
			styles.Yellow(shortHash),
			styles.Green(author),
			styles.Mute(dateStr),
			lineNumWidth, i+1,
			bl.lineContent)
	}

	return nil
}
