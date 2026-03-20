package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"github.com/xdamman/nostr-cli/internal/config"
	"github.com/xdamman/nostr-cli/internal/profile"
)

var profileCmd = &cobra.Command{
	Use:   "profile",
	Short: "View your profile metadata",
	RunE:  runProfile,
}

var profileUpdateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update your profile metadata interactively",
	RunE:  runProfileUpdate,
}

func init() {
	profileCmd.AddCommand(profileUpdateCmd)
	rootCmd.AddCommand(profileCmd)
}

func runProfile(cmd *cobra.Command, args []string) error {
	npub, err := config.LoadResolvedProfile(profileFlag)
	if err != nil {
		return err
	}

	// Try cached first
	meta, _ := profile.LoadCached(npub)

	// Fetch fresh from relays
	relays, relayErr := config.LoadRelays(npub)
	if relayErr == nil && len(relays) > 0 {
		ctx := context.Background()
		fresh, err := profile.FetchFromRelays(ctx, npub, relays)
		if err == nil && fresh != nil {
			meta = fresh
			_ = profile.SaveCached(npub, meta)
		}
	}

	if meta == nil {
		meta = &profile.Metadata{}
	}

	fmt.Printf("npub:         %s\n", npub)
	printField("Name", meta.Name)
	printField("Display Name", meta.DisplayName)
	printField("About", meta.About)
	printField("Picture", meta.Picture)
	printField("NIP-05", meta.NIP05)
	printField("Banner", meta.Banner)
	printField("Website", meta.Website)
	printField("Lightning", meta.LUD16)

	return nil
}

func runProfileUpdate(cmd *cobra.Command, args []string) error {
	npub, err := config.LoadResolvedProfile(profileFlag)
	if err != nil {
		return err
	}

	// Load existing metadata
	meta, _ := profile.LoadCached(npub)
	if meta == nil {
		meta = &profile.Metadata{}
	}

	reader := bufio.NewReader(os.Stdin)

	meta.Name = promptField(reader, "Name", meta.Name)
	meta.DisplayName = promptField(reader, "Display name", meta.DisplayName)
	meta.About = promptField(reader, "About", meta.About)
	meta.Picture = promptField(reader, "Picture URL", meta.Picture)
	meta.NIP05 = promptField(reader, "NIP-05", meta.NIP05)

	// Save locally
	if err := profile.SaveCached(npub, meta); err != nil {
		return fmt.Errorf("failed to save profile: %w", err)
	}

	// Publish to relays
	relays, err := config.LoadRelays(npub)
	if err != nil {
		return err
	}

	fmt.Println("Publishing profile to relays...")
	ctx := context.Background()
	if err := profile.PublishMetadata(ctx, npub, meta, relays); err != nil {
		return fmt.Errorf("failed to publish: %w", err)
	}

	fmt.Println("✓ Profile updated and published")
	return nil
}

func promptField(reader *bufio.Reader, label, current string) string {
	if current != "" {
		fmt.Printf("%s [%s]: ", label, current)
	} else {
		fmt.Printf("%s: ", label)
	}
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)
	if input == "" {
		return current
	}
	return input
}

func printField(label, value string) {
	if value != "" {
		fmt.Printf("%-14s%s\n", label+":", value)
	}
}
