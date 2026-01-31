package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/imgajeed76/pgit/internal/repo"
	"github.com/imgajeed76/pgit/internal/ui/styles"
	"github.com/spf13/cobra"
)

func newCommitCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "commit",
		Short: "Record changes to the repository",
		Long: `Create a new commit containing the current contents of the staging area.

The commit message can be provided with -m. If not provided, an editor
will be opened to write the commit message.

The editor is determined by (in order):
  1. $PGIT_EDITOR environment variable
  2. $VISUAL environment variable
  3. $EDITOR environment variable
  4. First available: vi, vim, nano, notepad (Windows)`,
		RunE: runCommit,
	}

	cmd.Flags().StringP("message", "m", "", "Commit message")
	cmd.Flags().StringP("author", "a", "", "Override author (format: \"Name <email>\")")

	return cmd
}

func runCommit(cmd *cobra.Command, args []string) error {
	message, _ := cmd.Flags().GetString("message")
	authorOverride, _ := cmd.Flags().GetString("author")

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

	// Get staged changes for summary
	staged, err := r.GetStagedChanges(ctx)
	if err != nil {
		return err
	}

	if len(staged) == 0 {
		fmt.Println("nothing to commit, working tree clean")
		return nil
	}

	// If no message provided, open editor
	if message == "" {
		var err error
		message, err = getCommitMessageFromEditor(r, staged)
		if err != nil {
			return err
		}
		if message == "" {
			return fmt.Errorf("aborting commit due to empty commit message")
		}
	}

	// Create commit options
	opts := repo.CommitOptions{
		Message: message,
	}

	// Parse author override if provided
	if authorOverride != "" {
		name, email, err := parseAuthor(authorOverride)
		if err != nil {
			return err
		}
		opts.AuthorName = name
		opts.AuthorEmail = email
	}

	commit, err := r.Commit(ctx, opts)
	if err != nil {
		return err
	}

	// Print commit summary
	// Format: [hash] message
	hash := styles.Hash(commit.ID, true)
	fmt.Printf("[%s] %s\n", hash, firstLine(commit.Message))
	fmt.Println()

	// Count changes by type
	var added, modified, deleted int
	for _, c := range staged {
		switch c.Status {
		case repo.StatusNew:
			added++
		case repo.StatusModified:
			modified++
		case repo.StatusDeleted:
			deleted++
		}
	}

	// Summary line
	total := added + modified + deleted
	fmt.Printf(" %d file(s) changed\n", total)

	// File mode changes
	for _, c := range staged {
		switch c.Status {
		case repo.StatusNew:
			fmt.Printf(" %s %s\n", styles.Green("create"), c.Path)
		case repo.StatusModified:
			fmt.Printf(" %s %s\n", styles.Yellow("modify"), c.Path)
		case repo.StatusDeleted:
			fmt.Printf(" %s %s\n", styles.Red("delete"), c.Path)
		}
	}

	return nil
}

// getCommitMessageFromEditor opens an editor for the user to write a commit message
func getCommitMessageFromEditor(r *repo.Repository, staged []repo.FileChange) (string, error) {
	// Determine editor
	editor, err := findEditor()
	if err != nil {
		return "", err
	}

	// Create temp file with template
	tmpfile, err := os.CreateTemp("", "PGIT_COMMIT_MSG_*.txt")
	if err != nil {
		return "", fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpfile.Name()
	defer os.Remove(tmpPath)

	// Write template
	template := generateCommitTemplate(staged)
	if _, err := tmpfile.WriteString(template); err != nil {
		tmpfile.Close()
		return "", fmt.Errorf("failed to write template: %w", err)
	}
	tmpfile.Close()

	// Open editor
	editorCmd := exec.Command(editor, tmpPath)
	editorCmd.Stdin = os.Stdin
	editorCmd.Stdout = os.Stdout
	editorCmd.Stderr = os.Stderr

	if err := editorCmd.Run(); err != nil {
		return "", fmt.Errorf("editor failed: %w", err)
	}

	// Read the edited file
	content, err := os.ReadFile(tmpPath)
	if err != nil {
		return "", fmt.Errorf("failed to read commit message: %w", err)
	}

	// Parse message (remove comments)
	message := parseCommitMessage(string(content))
	return message, nil
}

// generateCommitTemplate creates the template shown in the editor
func generateCommitTemplate(staged []repo.FileChange) string {
	var sb strings.Builder

	sb.WriteString("\n")
	sb.WriteString("# Please enter the commit message for your changes. Lines starting\n")
	sb.WriteString("# with '#' will be ignored, and an empty message aborts the commit.\n")
	sb.WriteString("#\n")
	sb.WriteString("# Changes to be committed:\n")

	for _, c := range staged {
		var status string
		switch c.Status {
		case repo.StatusNew:
			status = "new file"
		case repo.StatusModified:
			status = "modified"
		case repo.StatusDeleted:
			status = "deleted"
		default:
			status = "changed"
		}
		sb.WriteString(fmt.Sprintf("#\t%s:   %s\n", status, c.Path))
	}

	return sb.String()
}

// parseCommitMessage removes comment lines and trims whitespace
func parseCommitMessage(content string) string {
	var lines []string
	for _, line := range strings.Split(content, "\n") {
		// Skip comment lines
		if strings.HasPrefix(strings.TrimSpace(line), "#") {
			continue
		}
		lines = append(lines, line)
	}

	// Join and trim
	message := strings.Join(lines, "\n")
	message = strings.TrimSpace(message)

	return message
}

// parseAuthor parses "Name <email>" format
func parseAuthor(author string) (name, email string, err error) {
	// Pattern: Name <email>
	re := regexp.MustCompile(`^(.+?)\s*<([^>]+)>$`)
	matches := re.FindStringSubmatch(author)
	if matches == nil {
		return "", "", fmt.Errorf("invalid author format, expected \"Name <email>\"")
	}
	return strings.TrimSpace(matches[1]), strings.TrimSpace(matches[2]), nil
}

// firstLine returns the first line of a string
func firstLine(s string) string {
	for i, c := range s {
		if c == '\n' {
			return s[:i]
		}
	}
	return s
}

// findEditor finds an available text editor
func findEditor() (string, error) {
	// Check environment variables first
	if editor := os.Getenv("PGIT_EDITOR"); editor != "" {
		if path, err := exec.LookPath(editor); err == nil {
			return path, nil
		}
		return "", fmt.Errorf("editor '%s' (from $PGIT_EDITOR) not found", editor)
	}

	if editor := os.Getenv("VISUAL"); editor != "" {
		if path, err := exec.LookPath(editor); err == nil {
			return path, nil
		}
		// Don't error on VISUAL, fall through to EDITOR
	}

	if editor := os.Getenv("EDITOR"); editor != "" {
		if path, err := exec.LookPath(editor); err == nil {
			return path, nil
		}
		// Don't error on EDITOR, fall through to fallbacks
	}

	// Try common editors in order of preference
	fallbacks := []string{"vi", "vim", "nano", "notepad"}
	for _, editor := range fallbacks {
		if path, err := exec.LookPath(editor); err == nil {
			return path, nil
		}
	}

	return "", fmt.Errorf("no editor found. Set $EDITOR or $PGIT_EDITOR environment variable")
}
