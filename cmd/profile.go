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
	"github.com/xdamman/nostr-cli/internal/profile"
)

var profileCmd = &cobra.Command{
	Use:   "profile [user]",
	Short: "View a profile (yours by default, or specify a user)",
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
	label := color.New(color.FgCyan).SprintFunc()
	errColor := color.New(color.FgRed)

	if len(args) > 0 {
		// User specified a target — look them up, do NOT fall back to current user
		userArg := strings.TrimPrefix(args[0], "@")
		return lookupUserProfile(userArg, label, errColor)
	}

	// No args — show current user's profile
	npub, err := config.LoadResolvedProfile(profileFlag)
	if err != nil {
		return err
	}

	return showProfile(npub, label)
}

func lookupUserProfile(user string, label func(a ...interface{}) string, errColor *color.Color) error {
	// If it looks like an npub, use directly
	npub := user
	if !strings.HasPrefix(user, "npub1") {
		// TODO: resolve username/alias to npub
		errColor.Fprintf(os.Stderr, "Error: user %q not found\n", user)
		os.Exit(1)
	}

	// Try fetching from relays
	// Use current user's relays as a starting point
	activeNpub, _ := config.ActiveProfile()
	var relays []string
	if activeNpub != "" {
		relays, _ = config.LoadRelays(activeNpub)
	}

	if len(relays) > 0 {
		ctx := context.Background()
		meta, err := profile.FetchFromRelays(ctx, npub, relays)
		if err != nil || meta == nil {
			errColor.Fprintf(os.Stderr, "Error: user %q not found\n", user)
			os.Exit(1)
		}

		fmt.Printf("%s %s\n", label("npub:"), npub)
		printColorField(label, "Name", meta.Name)
		printColorField(label, "Display Name", meta.DisplayName)
		printColorField(label, "About", meta.About)
		printColorField(label, "Picture", meta.Picture)
		printColorField(label, "NIP-05", meta.NIP05)
		printColorField(label, "Banner", meta.Banner)
		printColorField(label, "Website", meta.Website)
		printColorField(label, "Lightning", meta.LUD16)
		return nil
	}

	errColor.Fprintf(os.Stderr, "Error: user %q not found (no relays configured)\n", user)
	os.Exit(1)
	return nil
}

func showProfile(npub string, label func(a ...interface{}) string) error {
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

	fmt.Printf("%s %s\n", label("npub:"), npub)
	printColorField(label, "Name", meta.Name)
	printColorField(label, "Display Name", meta.DisplayName)
	printColorField(label, "About", meta.About)
	printColorField(label, "Picture", meta.Picture)
	printColorField(label, "NIP-05", meta.NIP05)
	printColorField(label, "Banner", meta.Banner)
	printColorField(label, "Website", meta.Website)
	printColorField(label, "Lightning", meta.LUD16)

	return nil
}

func runProfileUpdate(cmd *cobra.Command, args []string) error {
	green := color.New(color.FgGreen)

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

	green.Println("✓ Profile updated and published")
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

func printColorField(label func(a ...interface{}) string, name, value string) {
	if value != "" {
		fmt.Printf("%-14s %s\n", label(name+":"), value)
	}
}
