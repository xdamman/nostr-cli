package cmd

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
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
	Use:     "version",
	Short:   "Print version information",
	GroupID: "app",
	Run:     runVersion,
}

var updateCmd = &cobra.Command{
	Use:     "update",
	Short:   "Check for updates and install the latest version",
	GroupID: "app",
	RunE:    runUpdate,
}

func init() {
	updateCmd.Flags().BoolVarP(&updateYesFlag, "yes", "y", false, "Update without confirmation")
	rootCmd.AddCommand(versionCmd)
	rootCmd.AddCommand(updateCmd)
}

func runVersion(cmd *cobra.Command, args []string) {
	if rawFlag || jsonFlag || jsonlFlag {
		info := map[string]string{
			"version": Version,
			"commit":  CommitSHA,
			"date":    CommitDate,
			"message": CommitMsg,
			"os":      runtime.GOOS,
			"arch":    runtime.GOARCH,
		}
		if rawFlag {
			printRaw(info)
		} else if jsonlFlag {
			printJSONL(info)
		} else {
			printJSON(info)
		}
		return
	}
	label := color.New(color.FgCyan).SprintFunc()
	fmt.Printf("nostr %s\n", Version)
	fmt.Printf("%s %s\n", label("Commit:"), CommitSHA)
	fmt.Printf("%s %s\n", label("Date:  "), CommitDate)
	fmt.Printf("%s %s\n", label("Message:"), CommitMsg)
}

// ghRelease holds the fields we need from the GitHub releases API.
type ghRelease struct {
	TagName     string `json:"tag_name"`
	Name        string `json:"name"`
	PublishedAt string `json:"published_at"`
	Body        string `json:"body"`
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

	// Fetch latest release
	if !jsonFlag && !jsonlFlag && !rawFlag {
		fmt.Println("\nChecking for updates...")
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get("https://api.github.com/repos/xdamman/nostr-cli/releases/latest")
	if err != nil {
		return fmt.Errorf("failed to check for updates: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("GitHub API returned %d", resp.StatusCode)
	}

	var latest ghRelease
	if err := json.NewDecoder(resp.Body).Decode(&latest); err != nil {
		return fmt.Errorf("failed to parse release info: %w", err)
	}

	latestVersion := strings.TrimPrefix(latest.TagName, "v")
	currentVersion := strings.TrimPrefix(Version, "v")

	if latestVersion == currentVersion {
		if jsonFlag || jsonlFlag || rawFlag {
			result := map[string]interface{}{
				"current_version": Version,
				"latest_version":  latest.TagName,
				"up_to_date":      true,
			}
			if rawFlag {
				printRaw(result)
			} else if jsonlFlag {
				printJSONL(result)
			} else {
				printJSON(result)
			}
			return nil
		}
		green.Println("\nYou're up to date.")
		return nil
	}

	// Determine OS and arch
	goos := runtime.GOOS
	goarch := runtime.GOARCH
	tarball := fmt.Sprintf("nostr_%s_%s.tar.gz", goos, goarch)
	downloadURL := fmt.Sprintf("https://github.com/xdamman/nostr-cli/releases/download/%s/%s", latest.TagName, tarball)

	// HEAD request to get size
	var sizeMB float64
	headResp, headErr := client.Head(downloadURL)
	if headErr == nil && headResp.StatusCode == 200 && headResp.ContentLength > 0 {
		sizeMB = float64(headResp.ContentLength) / (1024 * 1024)
	}
	if headResp != nil {
		headResp.Body.Close()
	}

	// JSON mode: auto-confirm and output result
	if jsonFlag || jsonlFlag || rawFlag {
		updateYesFlag = true
	}

	// Show latest version
	if !jsonFlag && !jsonlFlag && !rawFlag {
		fmt.Println()
		fmt.Println(label("Latest version:"))
		fmt.Printf("  Version:  %s\n", latest.TagName)
		fmt.Printf("  Date:     %s\n", latest.PublishedAt)
		if latest.Name != "" {
			fmt.Printf("  Name:     %s\n", latest.Name)
		}
		fmt.Printf("  File:     %s\n", tarball)
		if sizeMB > 0 {
			fmt.Printf("  Size:     %.1f MB\n", sizeMB)
		}
	}

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

	fmt.Println("\nDownloading...")

	resp, err = client.Get(downloadURL)
	if err != nil {
		return fmt.Errorf("download failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("download failed: HTTP %d (no binary for %s/%s?)", resp.StatusCode, goos, goarch)
	}

	// Extract the binary from the tarball
	gr, err := gzip.NewReader(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to decompress: %w", err)
	}
	defer gr.Close()

	newBinary, err := extractTarBinary(gr, "nostr")
	if err != nil {
		return fmt.Errorf("failed to extract binary: %w", err)
	}

	// Find where the current binary lives
	execPath, err := os.Executable()
	if err != nil {
		return fmt.Errorf("cannot determine executable path: %w", err)
	}
	// Resolve symlinks
	execPath, err = resolveSymlinks(execPath)
	if err != nil {
		return fmt.Errorf("cannot resolve executable path: %w", err)
	}

	// Write to a temp file next to the binary, then rename (atomic on same FS)
	tmpPath := execPath + ".tmp"
	if err := os.WriteFile(tmpPath, newBinary, 0755); err != nil {
		// Try with sudo hint
		os.Remove(tmpPath)
		return fmt.Errorf("cannot write to %s (try running with sudo): %w", execPath, err)
	}

	if err := os.Rename(tmpPath, execPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("cannot replace binary: %w", err)
	}

	if jsonFlag || jsonlFlag || rawFlag {
		result := map[string]interface{}{
			"current_version": Version,
			"latest_version":  latest.TagName,
			"up_to_date":      false,
			"updated":         true,
			"file":            tarball,
			"size_mb":         fmt.Sprintf("%.1f", sizeMB),
		}
		if rawFlag {
			printRaw(result)
		} else if jsonlFlag {
			printJSONL(result)
		} else {
			printJSON(result)
		}
		return nil
	}

	green.Printf("\nUpdated to %s\n", latest.TagName)
	return nil
}

// extractTarBinary reads a tar stream and returns the contents of the named file.
func extractTarBinary(r io.Reader, name string) ([]byte, error) {
	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}
		if hdr.Name == name || strings.HasSuffix(hdr.Name, "/"+name) {
			return io.ReadAll(tr)
		}
	}
	return nil, fmt.Errorf("binary %q not found in archive", name)
}

// resolveSymlinks resolves a path through symlinks to the real file.
func resolveSymlinks(path string) (string, error) {
	resolved, err := os.Readlink(path)
	if err != nil {
		// Not a symlink
		return path, nil
	}
	if !strings.HasPrefix(resolved, "/") {
		// Relative symlink — resolve relative to parent dir
		dir := path[:strings.LastIndex(path, "/")+1]
		resolved = dir + resolved
	}
	return resolveSymlinks(resolved)
}
