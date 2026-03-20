package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

// Build-time variables injected via ldflags
var (
	Version    string = "dev"
	CommitSHA  string = "unknown"
	CommitDate string = "unknown"
	CommitMsg  string = "unknown"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	Run:   runVersion,
}

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Check for updates",
	RunE:  runUpdate,
}

func init() {
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(updateCmd)
}

func runVersion(cmd *cobra.Command, args []string) {
	label := color.New(color.FgCyan).SprintFunc()
	fmt.Printf("nostr-cli %s\n", Version)
	fmt.Printf("%s %s\n", label("Commit:"), CommitSHA)
	fmt.Printf("%s %s\n", label("Date:  "), CommitDate)
	fmt.Printf("%s %s\n", label("Message:"), CommitMsg)
}

func runUpdate(cmd *cobra.Command, args []string) error {
	green := color.New(color.FgGreen).SprintFunc()
	cyan := color.New(color.FgCyan).SprintFunc()
	yellow := color.New(color.FgYellow).SprintFunc()

	fmt.Printf("Current: nostr-cli %s (%s)\n", Version, CommitSHA)
	fmt.Println("Checking for updates...")

	client := &http.Client{Timeout: 10 * time.Second}

	// Try releases first
	resp, err := client.Get("https://api.github.com/repos/xdamman/nostr-cli/releases/latest")
	if err != nil {
		return fmt.Errorf("failed to check for updates: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 200 {
		var release struct {
			TagName     string `json:"tag_name"`
			PublishedAt string `json:"published_at"`
			HTMLURL     string `json:"html_url"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&release); err == nil {
			fmt.Printf("\n%s %s (released %s)\n", cyan("Latest release:"), release.TagName, release.PublishedAt)
			if release.TagName != Version {
				fmt.Printf("%s A newer version is available!\n", yellow("→"))
				fmt.Printf("  Run: %s\n", green("go install github.com/xdamman/nostr-cli@latest"))
			} else {
				fmt.Printf("%s You're up to date.\n", green("✓"))
			}
			return nil
		}
	}

	// No releases — check latest commit
	resp2, err := client.Get("https://api.github.com/repos/xdamman/nostr-cli/commits/main")
	if err != nil {
		return fmt.Errorf("failed to fetch latest commit: %w", err)
	}
	defer resp2.Body.Close()

	if resp2.StatusCode != 200 {
		return fmt.Errorf("GitHub API returned %d", resp2.StatusCode)
	}

	var commit struct {
		SHA    string `json:"sha"`
		Commit struct {
			Message string `json:"message"`
			Author  struct {
				Date string `json:"date"`
			} `json:"author"`
		} `json:"commit"`
	}
	if err := json.NewDecoder(resp2.Body).Decode(&commit); err != nil {
		return fmt.Errorf("failed to parse commit info: %w", err)
	}

	shortSHA := commit.SHA
	if len(shortSHA) > 7 {
		shortSHA = shortSHA[:7]
	}

	fmt.Printf("\n%s %s (%s)\n", cyan("Latest commit:"), shortSHA, commit.Commit.Author.Date)
	fmt.Printf("  %s\n", commit.Commit.Message)

	if shortSHA != CommitSHA {
		fmt.Printf("\n%s A newer version may be available.\n", yellow("→"))
		fmt.Printf("  Run: %s\n", green("go install github.com/xdamman/nostr-cli@latest"))
		fmt.Println("  Or:  git pull && make install")
	} else {
		fmt.Printf("\n%s You're up to date.\n", green("✓"))
	}

	return nil
}
