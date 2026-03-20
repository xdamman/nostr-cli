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
)

var (
	loginNsec     string
	loginGenerate bool
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
	rootCmd.AddCommand(loginCmd)
}

func runLogin(cmd *cobra.Command, args []string) error {
	green := color.New(color.FgGreen)
	cyan := color.New(color.FgCyan).SprintFunc()

	var nsec, npub string
	var err error

	switch {
	case loginNsec != "":
		nsec = loginNsec
		npub, _, _, err = crypto.NsecToKeys(nsec)
		if err != nil {
			return fmt.Errorf("invalid nsec: %w", err)
		}
	case loginGenerate:
		nsec, npub, _, err = crypto.GenerateKeyPair()
		if err != nil {
			return err
		}
		green.Println("Generated new keypair")
	default:
		fmt.Print("Enter your nsec (leave blank to generate a new keypair): ")
		reader := bufio.NewReader(os.Stdin)
		input, _ := reader.ReadString('\n')
		input = strings.TrimSpace(input)
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

	fmt.Println()
	green.Printf("✓ Logged in as %s\n", npub)
	fmt.Printf("  Profile dir: %s\n", dir)
	return nil
}
