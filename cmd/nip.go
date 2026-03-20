package cmd

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/charmbracelet/glamour"
	"github.com/fatih/color"
	"github.com/spf13/cobra"
)

var nipCmd = &cobra.Command{
	Use:   "nip [number]",
	Short: "View a NIP specification",
	Long:  "Fetch and display a NIP (Nostr Implementation Possibility) in the terminal.\nExamples: nostr nip 01, nostr nip 44, nostr nip01",
	Args:  cobra.ExactArgs(1),
	RunE:  runNIP,
}

func init() {
	rootCmd.AddCommand(nipCmd)
}

func runNIP(cmd *cobra.Command, args []string) error {
	return fetchAndDisplayNIP(args[0])
}

func fetchAndDisplayNIP(input string) error {
	dim := color.New(color.Faint)

	// Parse NIP number: accept "01", "1", "nip01", "nip1", "NIP-01", etc.
	re := regexp.MustCompile(`(?i)^(?:nip[- ]?)?(\d+)$`)
	matches := re.FindStringSubmatch(input)
	if matches == nil {
		return fmt.Errorf("invalid NIP number: %q\nUsage: nostr nip 01, nostr nip 44", input)
	}

	numStr := matches[1]
	// Pad to 2 digits
	if len(numStr) == 1 {
		numStr = "0" + numStr
	}

	cacheDir := nipCacheDir()
	cachePath := filepath.Join(cacheDir, numStr+".md")

	// Try cache first
	content, err := loadNIPCache(cachePath)
	if err != nil {
		// Fetch from GitHub
		url := fmt.Sprintf("https://raw.githubusercontent.com/nostr-protocol/nips/master/%s.md", numStr)
		dim.Printf("Fetching NIP-%s...\n", numStr)

		client := &http.Client{Timeout: 15 * time.Second}
		resp, err := client.Get(url)
		if err != nil {
			// Try cache even if stale
			stale, staleErr := os.ReadFile(cachePath)
			if staleErr == nil {
				dim.Println("(offline - showing cached version)")
				return renderMarkdown(string(stale))
			}
			return fmt.Errorf("failed to fetch NIP-%s: %w", numStr, err)
		}
		defer resp.Body.Close()

		if resp.StatusCode == 404 {
			return fmt.Errorf("NIP-%s not found", numStr)
		}
		if resp.StatusCode != 200 {
			return fmt.Errorf("failed to fetch NIP-%s: HTTP %d", numStr, resp.StatusCode)
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return fmt.Errorf("failed to read NIP-%s: %w", numStr, err)
		}
		content = string(body)

		// Cache it
		_ = saveNIPCache(cachePath, content)
	} else {
		dim.Printf("NIP-%s (cached)\n", numStr)
	}

	return renderMarkdown(content)
}

func nipCacheDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "/tmp/nostr-nip-cache"
	}
	return filepath.Join(home, ".nostr", "cache", "nips")
}

func loadNIPCache(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	// Consider cache valid for 24 hours
	if time.Since(info.ModTime()) > 24*time.Hour {
		return "", fmt.Errorf("cache expired")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func saveNIPCache(path, content string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0644)
}

func renderMarkdown(content string) error {
	style := "dark"
	if os.Getenv("GLAMOUR_STYLE") != "" {
		style = os.Getenv("GLAMOUR_STYLE")
	}

	renderer, err := glamour.NewTermRenderer(
		glamour.WithStylePath(style),
		glamour.WithWordWrap(100),
	)
	if err != nil {
		// Fallback: print raw
		fmt.Println(content)
		return nil
	}

	out, err := renderer.Render(content)
	if err != nil {
		fmt.Println(content)
		return nil
	}

	out = strings.TrimRight(out, "\n")
	fmt.Println(out)
	return nil
}
