package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"github.com/xdamman/nostr-cli/internal/config"
	"github.com/xdamman/nostr-cli/internal/crypto"
	"github.com/xdamman/nostr-cli/internal/profile"
	"golang.org/x/term"
)

var (
	loginNsec     string
	loginGenerate bool
	loginNew      bool
)

var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "Create a new profile or import an existing one",
	Long:  "Login with an existing nsec or generate a new keypair. Creates a profile in ~/.nostr/profiles/<npub>/.",
	RunE:  runLogin,
}

func init() {
	loginCmd.Flags().StringVar(&loginNsec, "nsec", "", "Import an existing nsec (non-interactive)")
	loginCmd.Flags().BoolVar(&loginGenerate, "generate", false, "Generate a new keypair (skip prompt)")
	loginCmd.Flags().BoolVar(&loginNew, "new", false, "Generate a new keypair (alias for --generate)")
	rootCmd.AddCommand(loginCmd)
}

func runLogin(cmd *cobra.Command, args []string) error {
	green := color.New(color.FgGreen)
	cyan := color.New(color.FgCyan).SprintFunc()

	var nsec, npub string
	var err error
	interactive := false

	switch {
	case loginNsec != "":
		nsec = loginNsec
		npub, _, _, err = crypto.NsecToKeys(nsec)
		if err != nil {
			return fmt.Errorf("invalid nsec: %w", err)
		}
	case loginGenerate || loginNew:
		nsec, npub, _, err = crypto.GenerateKeyPair()
		if err != nil {
			return err
		}
		green.Println("Generated new keypair")
	default:
		interactive = true
		fmt.Print("Enter your nsec (leave blank to generate a new keypair): ")
		input, _ := readMaskedInput()
		fmt.Println()
		if input == "" {
			nsec, npub, _, err = crypto.GenerateKeyPair()
			if err != nil {
				return err
			}
			green.Println("Generated new keypair")
		} else {
			nsec = input
			npub, _, _, err = crypto.NsecToKeys(nsec)
			if err != nil {
				return fmt.Errorf("invalid nsec: %w", err)
			}
		}
	}

	// Check if profile already exists
	dir, err := config.ProfileDir(npub)
	if err != nil {
		return err
	}
	if _, statErr := os.Stat(dir); statErr == nil {
		fmt.Printf("Profile %s already exists. Switching to it.\n", npub)
		return config.SetActiveProfile(npub)
	}

	// Create profile directory
	if _, err := config.EnsureProfileDir(npub); err != nil {
		return err
	}

	// Save nsec
	if err := config.SaveNsec(npub, nsec); err != nil {
		return fmt.Errorf("failed to save nsec: %w", err)
	}

	// Save default relays
	if err := config.SaveDefaultRelays(npub); err != nil {
		return fmt.Errorf("failed to save default relays: %w", err)
	}

	// Set as active profile
	if err := config.SetActiveProfile(npub); err != nil {
		return fmt.Errorf("failed to set active profile: %w", err)
	}

	// Try to fetch profile from relays
	relays, _ := config.LoadRelays(npub)
	if len(relays) > 0 {
		fmt.Println("Fetching profile from relays...")
		ctx := context.Background()
		meta, err := profile.FetchFromRelays(ctx, npub, relays)
		if err == nil && meta != nil {
			if err := profile.SaveCached(npub, meta); err == nil {
				if meta.Name != "" {
					fmt.Printf("%s %s\n", cyan("Found profile:"), meta.Name)
				}
				if meta.DisplayName != "" {
					fmt.Printf("%s %s\n", cyan("Display name:"), meta.DisplayName)
				}
			}
		} else {
			fmt.Println("No profile found on relays (new identity?)")
		}
	}

	// Prompt for alias (interactive mode only)
	if interactive && term.IsTerminal(int(os.Stdin.Fd())) {
		fmt.Print("\nChoose an alias for this profile (enter to skip): ")
		scanner := bufio.NewScanner(os.Stdin)
		if scanner.Scan() {
			alias := strings.TrimSpace(scanner.Text())
			if alias != "" {
				if err := config.SetGlobalAlias(alias, npub); err != nil {
					fmt.Fprintf(os.Stderr, "Warning: could not set alias: %v\n", err)
				} else {
					green.Printf("✓ Alias %s → %s\n", alias, npub)
				}
			}
		}
	}

	fmt.Println()
	green.Printf("✓ Logged in as %s\n", npub)
	fmt.Printf("  Profile dir: %s\n", dir)
	return nil
}

// readMaskedInput reads input character by character in raw mode,
// displaying • for all characters except the last 4.
func readMaskedInput() (string, error) {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		// Fallback: read without masking
		var line string
		fmt.Scanln(&line)
		return strings.TrimSpace(line), nil
	}

	oldState, err := term.MakeRaw(fd)
	if err != nil {
		var line string
		fmt.Scanln(&line)
		return strings.TrimSpace(line), nil
	}
	defer term.Restore(fd, oldState)

	var buf []byte
	b := make([]byte, 1)

	for {
		_, err := os.Stdin.Read(b)
		if err != nil {
			return string(buf), err
		}

		switch b[0] {
		case 13, 10: // Enter
			return string(buf), nil
		case 3: // Ctrl-C
			return "", fmt.Errorf("interrupted")
		case 127, 8: // Backspace
			if len(buf) > 0 {
				buf = buf[:len(buf)-1]
				redrawMasked(buf)
			}
		case 22: // Ctrl-V (paste) — just continue reading chars
			continue
		default:
			if b[0] >= 32 {
				buf = append(buf, b[0])
				redrawMasked(buf)
			}
		}
	}
}

// redrawMasked redraws the masked input: •••••last4
func redrawMasked(buf []byte) {
	display := maskSecret(string(buf))
	// Clear the line from cursor start, reprint prompt + masked value
	fmt.Printf("\r\033[K")
	fmt.Printf("Enter your nsec (leave blank to generate a new keypair): %s", display)
}

// maskSecret returns a masked version showing only the last 4 characters.
func maskSecret(s string) string {
	if len(s) <= 4 {
		return s
	}
	masked := strings.Repeat("•", len(s)-4)
	return masked + s[len(s)-4:]
}
