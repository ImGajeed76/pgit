package cli

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/imgajeed76/pgit/internal/repo"
	"github.com/imgajeed76/pgit/internal/ui/styles"
	"github.com/imgajeed76/pgit/internal/util"
	"github.com/spf13/cobra"
)

func newBlameCmd() *cobra.Command {
	return &cobra.Command{
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
}

func runBlame(cmd *cobra.Command, args []string) error {
	path := args[0]

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

	// Get file history
	history, err := r.DB.GetFileHistory(ctx, path)
	if err != nil {
		return err
	}
	if len(history) == 0 {
		return util.ErrFileNotFound
	}

	// Get current file content
	head, err := r.DB.GetHeadCommit(ctx)
	if err != nil {
		return err
	}
	if head == nil {
		return util.ErrNoCommits
	}

	currentBlob, err := r.DB.GetFileAtCommit(ctx, path, head.ID)
	if err != nil {
		return err
	}
	if currentBlob == nil {
		return util.ErrFileNotFound
	}

	// Split content into lines
	lines := strings.Split(string(currentBlob.Content), "\n")

	// Build blame info for each line
	// This is a simplified implementation - tracks the last commit that changed each line
	type blameInfo struct {
		commitID    string
		authorName  string
		date        time.Time
		lineContent string
	}

	blameLines := make([]blameInfo, len(lines))

	// Initialize all lines to the oldest commit
	for i, line := range lines {
		blameLines[i] = blameInfo{lineContent: line}
	}

	// Process history from oldest to newest
	for i := len(history) - 1; i >= 0; i-- {
		blob := history[i]

		// Get commit info
		commit, err := r.DB.GetCommit(ctx, blob.CommitID)
		if err != nil || commit == nil {
			continue
		}

		// Get lines at this commit
		var commitLines []string
		if blob.Content != nil {
			commitLines = strings.Split(string(blob.Content), "\n")
		}

		// Simple attribution: if a line exists at this commit, attribute it
		for lineIdx := range blameLines {
			if lineIdx < len(commitLines) && commitLines[lineIdx] == blameLines[lineIdx].lineContent {
				blameLines[lineIdx].commitID = commit.ID
				blameLines[lineIdx].authorName = commit.AuthorName
				blameLines[lineIdx].date = commit.CreatedAt
			}
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
