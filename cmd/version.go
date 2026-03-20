package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
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

var updateYesFlag bool

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	Run:   runVersion,
}

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Check for updates and install the latest version",
	RunE:  runUpdate,
}

func init() {
	updateCmd.Flags().BoolVarP(&updateYesFlag, "yes", "y", false, "Update without confirmation")
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

// ghCommit holds the fields we need from the GitHub commits API.
type ghCommit struct {
	SHA    string `json:"sha"`
	Commit struct {
		Message string `json:"message"`
		Author  struct {
			Date string `json:"date"`
		} `json:"author"`
	} `json:"commit"`
}

func runUpdate(cmd *cobra.Command, args []string) error {
	label := color.New(color.FgCyan).SprintFunc()
	green := color.New(color.FgGreen)
	yellow := color.New(color.FgYellow)

	// Show current version
	fmt.Println(label("Current version:"))
	fmt.Printf("  Version: %s\n", Version)
	fmt.Printf("  Commit:  %s\n", CommitSHA)
	fmt.Printf("  Date:    %s\n", CommitDate)
	fmt.Printf("  Message: %s\n", CommitMsg)

	// Fetch latest commit from main
	fmt.Println("\nChecking for updates...")
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get("https://api.github.com/repos/xdamman/nostr-cli/commits/main")
	if err != nil {
		return fmt.Errorf("failed to check for updates: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("GitHub API returned %d", resp.StatusCode)
	}

	var latest ghCommit
	if err := json.NewDecoder(resp.Body).Decode(&latest); err != nil {
		return fmt.Errorf("failed to parse commit info: %w", err)
	}

	shortSHA := latest.SHA
	if len(shortSHA) > 7 {
		shortSHA = shortSHA[:7]
	}
	// Take only the first line of the commit message
	latestMsg := strings.SplitN(latest.Commit.Message, "\n", 2)[0]
	latestDate := latest.Commit.Author.Date

	if shortSHA == CommitSHA {
		green.Println("\nYou're up to date.")
		return nil
	}

	// Show latest version
	fmt.Println()
	fmt.Println(label("Latest version:"))
	fmt.Printf("  Commit:  %s\n", shortSHA)
	fmt.Printf("  Date:    %s\n", latestDate)
	fmt.Printf("  Message: %s\n", latestMsg)

	if !updateYesFlag {
		yellow.Print("\nUpdate to latest version? [Y/n] ")
		reader := bufio.NewReader(os.Stdin)
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(strings.ToLower(input))
		if input != "" && input != "y" && input != "yes" {
			fmt.Println("Update cancelled.")
			return nil
		}
	}

	fmt.Println("\nUpdating...")
	install := exec.Command("go", "install", "github.com/xdamman/nostr-cli@latest")
	install.Stdout = os.Stdout
	install.Stderr = os.Stderr
	if err := install.Run(); err != nil {
		return fmt.Errorf("update failed: %w", err)
	}

	green.Println("Updated successfully.")
	return nil
}
