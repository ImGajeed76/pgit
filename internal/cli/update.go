package cli

import (
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"strings"
	"time"

	"github.com/imgajeed76/pgit/v3/internal/ui/styles"
	"github.com/spf13/cobra"
)

const (
	githubRepo    = "imgajeed76/pgit"
	releaseAPIURL = "https://api.github.com/repos/" + githubRepo + "/releases/latest"
)

func newUpdateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update",
		Short: "Check for pgit updates",
		Long: `Check if a newer version of pgit is available on GitHub.

If an update is available, shows the new version and instructions
for how to update.`,
		RunE: runUpdate,
	}

	cmd.Flags().Bool("check", false, "Only check, don't show install instructions")

	return cmd
}

type githubRelease struct {
	TagName     string `json:"tag_name"`
	Name        string `json:"name"`
	PublishedAt string `json:"published_at"`
	HTMLURL     string `json:"html_url"`
	Body        string `json:"body"`
}

func runUpdate(cmd *cobra.Command, args []string) error {
	checkOnly, _ := cmd.Flags().GetBool("check")

	fmt.Printf("Current version: %s\n", styles.Cyan(Version))
	fmt.Println("Checking for updates...")

	release, err := getLatestRelease()
	if err != nil {
		return fmt.Errorf("failed to check for updates: %w", err)
	}

	latestVersion := strings.TrimPrefix(release.TagName, "v")
	currentVersion := strings.TrimPrefix(Version, "v")

	// Handle dev version - this happens with `go install ...@latest` since ldflags aren't set
	if currentVersion == "dev" || currentVersion == "" {
		fmt.Println()
		fmt.Printf("Latest release: %s\n", styles.Green(release.TagName))
		fmt.Println(styles.Mute("(Version info unavailable - installed via 'go install')"))
		fmt.Println()
		fmt.Printf("%s To check if you have the latest code, re-run:\n", styles.Green("Tip:"))
		fmt.Printf("  %s\n", styles.Cyan("go install github.com/imgajeed76/pgit/v3/cmd/pgit@latest"))
		return nil
	}

	if latestVersion == currentVersion {
		fmt.Println()
		fmt.Printf("%s You are running the latest version!\n", styles.Green("✓"))
		return nil
	}

	// Compare versions (simple string compare works for semver)
	if latestVersion > currentVersion {
		fmt.Println()
		fmt.Printf("%s New version available: %s → %s\n",
			styles.Yellow("!"),
			styles.Mute(currentVersion),
			styles.Green(latestVersion))

		if release.PublishedAt != "" {
			if t, err := time.Parse(time.RFC3339, release.PublishedAt); err == nil {
				fmt.Printf("Released: %s\n", t.Format("Jan 2, 2006"))
			}
		}

		if !checkOnly {
			printUpdateInstructions(release)
		}
	} else {
		fmt.Println()
		fmt.Printf("%s You are running a newer version than the latest release.\n", styles.Cyan("!"))
	}

	return nil
}

func getLatestRelease() (*githubRelease, error) {
	client := &http.Client{Timeout: 10 * time.Second}

	req, err := http.NewRequest("GET", releaseAPIURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("User-Agent", "pgit/"+Version)

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API returned status %d", resp.StatusCode)
	}

	var release githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, err
	}

	return &release, nil
}

func printUpdateInstructions(release *githubRelease) {
	fmt.Println()
	fmt.Println(styles.SectionHeader("UPDATE INSTRUCTIONS"))
	fmt.Println()

	// Go install (recommended)
	fmt.Println(styles.Boldf("Using Go (recommended):"))
	fmt.Println()
	fmt.Printf("  %s\n", styles.Cyan("go install github.com/imgajeed76/pgit/v3/cmd/pgit@latest"))
	fmt.Println()

	// Binary download
	fmt.Println(styles.Boldf("Download binary:"))
	fmt.Println()
	fmt.Printf("  %s\n", styles.Cyan(release.HTMLURL))
	fmt.Println()

	// Platform-specific hints
	os := runtime.GOOS
	arch := runtime.GOARCH

	var binaryName string
	switch os {
	case "darwin":
		binaryName = fmt.Sprintf("pgit_%s_darwin_%s.tar.gz", strings.TrimPrefix(release.TagName, "v"), arch)
	case "linux":
		binaryName = fmt.Sprintf("pgit_%s_linux_%s.tar.gz", strings.TrimPrefix(release.TagName, "v"), arch)
	case "windows":
		binaryName = fmt.Sprintf("pgit_%s_windows_%s.zip", strings.TrimPrefix(release.TagName, "v"), arch)
	}

	if binaryName != "" {
		fmt.Printf("  Your platform: %s/%s\n", os, arch)
		fmt.Printf("  Download: %s\n", styles.Mute(binaryName))
	}
}
