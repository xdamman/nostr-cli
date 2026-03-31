package cmd

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/xdamman/nostr-cli/internal/config"
	"github.com/xdamman/nostr-cli/internal/crypto"
)

var (
	nip05Address string
	nip05Npub    string
)

var generateCmd = &cobra.Command{
	Use:     "generate",
	Short:   "Generate Nostr resources",
	GroupID: "infra",
}

var generateNip05Cmd = &cobra.Command{
	Use:   "nip05",
	Short: "Generate a NIP-05 nostr.json file",
	Long: `Generate a .well-known/nostr.json file for NIP-05 identity verification.

Examples:
  nostr generate nip05                                    # Interactive mode
  nostr generate nip05 --address user@domain.com          # Use active account's pubkey
  nostr generate nip05 --address user@domain.com --npub npub1...
  nostr generate nip05 --address user@domain.com --json   # Output JSON to stdout`,
	RunE: runGenerateNip05,
}

func init() {
	generateNip05Cmd.Flags().StringVar(&nip05Address, "address", "", "NIP-05 address (user@domain)")
	generateNip05Cmd.Flags().StringVar(&nip05Npub, "npub", "", "Public key (npub1..., defaults to active account)")
	generateCmd.AddCommand(generateNip05Cmd)
	rootCmd.AddCommand(generateCmd)
}

func runGenerateNip05(cmd *cobra.Command, args []string) error {
	address := nip05Address
	npub := nip05Npub

	// Interactive mode if no address provided
	if address == "" {
		reader := bufio.NewReader(os.Stdin)
		fmt.Print("NIP-05 address (user@domain): ")
		line, err := reader.ReadString('\n')
		if err != nil {
			return fmt.Errorf("failed to read input: %w", err)
		}
		address = strings.TrimSpace(line)
		if address == "" {
			return fmt.Errorf("address is required")
		}

		fmt.Print("npub (leave blank to use active account): ")
		line, err = reader.ReadString('\n')
		if err != nil && err != io.EOF {
			return fmt.Errorf("failed to read input: %w", err)
		}
		npub = strings.TrimSpace(line)
	}

	// Validate address format
	if !strings.Contains(address, "@") {
		return fmt.Errorf("invalid NIP-05 address: must be in user@domain format")
	}
	parts := strings.SplitN(address, "@", 2)
	user := parts[0]
	domain := parts[1]
	if user == "" || domain == "" {
		return fmt.Errorf("invalid NIP-05 address: must be in user@domain format")
	}

	// Resolve npub to hex
	var hexPubkey string
	if npub == "" {
		// Use active account
		activeNpub, err := config.ActiveProfile()
		if err != nil {
			return fmt.Errorf("no active account found. Provide --npub or run 'nostr login' first")
		}
		hex, err := crypto.NpubToHex(activeNpub)
		if err != nil {
			return fmt.Errorf("failed to convert active npub to hex: %w", err)
		}
		hexPubkey = hex
	} else {
		if !strings.HasPrefix(npub, "npub1") {
			return fmt.Errorf("invalid npub: must start with npub1")
		}
		hex, err := crypto.NpubToHex(npub)
		if err != nil {
			return fmt.Errorf("failed to convert npub to hex: %w", err)
		}
		hexPubkey = hex
	}

	// Try to fetch existing nostr.json from the domain
	names := make(map[string]string)
	existingURL := fmt.Sprintf("https://%s/.well-known/nostr.json", domain)
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(existingURL)
	if err == nil && resp.StatusCode == 200 {
		defer resp.Body.Close()
		var existing struct {
			Names map[string]string `json:"names"`
		}
		if json.NewDecoder(resp.Body).Decode(&existing) == nil && existing.Names != nil {
			names = existing.Names
		}
	}

	// Add/update the entry
	names[user] = hexPubkey

	nostrJSON := map[string]interface{}{
		"names": names,
	}

	output, err := json.MarshalIndent(nostrJSON, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}

	// If --json flag, output to stdout only
	if jsonFlag {
		fmt.Println(string(output))
		return nil
	}

	// Save to current directory
	if err := os.WriteFile("nostr.json", append(output, '\n'), 0644); err != nil {
		return fmt.Errorf("failed to write nostr.json: %w", err)
	}

	fmt.Printf("✓ Generated nostr.json in current directory\n\n")
	fmt.Printf("To complete NIP-05 setup:\n")
	fmt.Printf("  1. Upload nostr.json to your web server:\n")
	fmt.Printf("     scp nostr.json yourserver:/var/www/html/.well-known/nostr.json\n\n")
	fmt.Printf("  2. Make sure your web server serves it with CORS headers:\n")
	fmt.Printf("     Access-Control-Allow-Origin: *\n\n")
	fmt.Printf("  3. Verify it works:\n")
	fmt.Printf("     curl https://%s/.well-known/nostr.json\n\n", domain)
	fmt.Printf("  More info: https://nostrcli.sh/nip05\n")

	return nil
}
