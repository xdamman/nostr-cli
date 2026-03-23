package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/fatih/color"
	"github.com/nbd-wtf/go-nostr"
	"github.com/spf13/cobra"
	"github.com/xdamman/nostr-cli/internal/config"
	"github.com/xdamman/nostr-cli/internal/crypto"
	"github.com/xdamman/nostr-cli/internal/nip05"
	"github.com/xdamman/nostr-cli/internal/profile"
	internalRelay "github.com/xdamman/nostr-cli/internal/relay"
	"github.com/xdamman/nostr-cli/internal/resolve"
)

var profileJSONFlag bool

var profileCmd = &cobra.Command{
	Use:     "profile [profile]",
	Short:   "Manage and view profiles",
	Long:    "View a profile. A <profile> can be an npub, alias, or NIP-05 address (e.g. user@domain.com).",
	GroupID: "profile",
	RunE:    runProfile,
}

var profileUpdateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update your profile metadata interactively",
	RunE:  runProfileUpdate,
}

var profileRmCmd = &cobra.Command{
	Use:   "rm <profile>",
	Short: "Remove a local profile (profile: npub, alias, or nip05)",
	Args:  exactArgs(1),
	RunE:  runProfileRm,
}

func init() {
	profileCmd.Flags().BoolVar(&profileJSONFlag, "json", false, "Output raw kind 0 event as JSON")
	profileCmd.AddCommand(profileUpdateCmd)
	profileCmd.AddCommand(profileRmCmd)
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

	if profileJSONFlag {
		return showProfileJSON(npub)
	}

	return showProfile(npub, label)
}

func lookupUserProfile(user string, label func(a ...interface{}) string, errColor *color.Color) error {
	activeNpub, _ := config.ActiveProfile()

	npub := user
	if !strings.HasPrefix(user, "npub1") {
		// Try resolve via alias/nip05 (aliases are global, no active profile needed)
		resolved, err := resolve.ResolveToNpub(activeNpub, user)
		if err != nil {
			errColor.Fprintf(os.Stderr, "Error: user %q not found\n", user)
			os.Exit(1)
		}
		npub = resolved
	}

	if profileJSONFlag {
		return showProfileJSON(npub)
	}

	// Merge current user's relays with defaults for broader coverage
	var relays []string
	if activeNpub != "" {
		relays, _ = config.LoadRelays(activeNpub)
	}
	// Always include default relays when looking up other users
	seen := make(map[string]bool, len(relays))
	for _, r := range relays {
		seen[r] = true
	}
	for _, r := range config.DefaultRelays() {
		if !seen[r] {
			relays = append(relays, r)
		}
	}

	ctx := context.Background()
	meta, err := profile.FetchFromRelays(ctx, npub, relays)
	if err != nil || meta == nil {
		errColor.Fprintf(os.Stderr, "Error: user %q not found\n", user)
		os.Exit(1)
	}

	pubHex, _ := crypto.NpubToHex(npub)
	fmt.Printf("%s %s\n", label("npub:"), npub)
	printColorField(label, "Name", meta.Name)
	printColorField(label, "Display Name", meta.DisplayName)
	printColorField(label, "About", meta.About)
	printColorField(label, "Picture", meta.Picture)
	printNIP05Field(label, meta.NIP05, pubHex)
	printColorField(label, "Banner", meta.Banner)
	printColorField(label, "Website", meta.Website)
	printColorField(label, "Lightning", meta.LUD16)
	return nil
}

// showProfileJSON fetches the raw kind 0 event and prints it as pretty JSON.
func showProfileJSON(npub string) error {
	activeNpub, _ := config.ActiveProfile()
	var relays []string
	if activeNpub != "" {
		relays, _ = config.LoadRelays(activeNpub)
	}
	// Also try the target npub's relays
	targetRelays, _ := config.LoadRelays(npub)
	for _, r := range targetRelays {
		found := false
		for _, existing := range relays {
			if existing == r {
				found = true
				break
			}
		}
		if !found {
			relays = append(relays, r)
		}
	}
	// Include default relays
	seen := make(map[string]bool, len(relays))
	for _, r := range relays {
		seen[r] = true
	}
	for _, r := range config.DefaultRelays() {
		if !seen[r] {
			relays = append(relays, r)
		}
	}

	if len(relays) == 0 {
		return fmt.Errorf("no relays configured")
	}

	pubHex, err := crypto.NpubToHex(npub)
	if err != nil {
		return err
	}

	ctx := context.Background()
	filter := nostr.Filter{
		Authors: []string{pubHex},
		Kinds:   []int{nostr.KindProfileMetadata},
		Limit:   1,
	}

	event, err := internalRelay.FetchEvent(ctx, filter, relays)
	if err != nil {
		return fmt.Errorf("failed to fetch profile: %w", err)
	}
	if event == nil {
		return fmt.Errorf("profile not found")
	}

	data, err := json.MarshalIndent(event, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(data))
	return nil
}

func showProfile(npub string, label func(a ...interface{}) string) error {
	dim := color.New(color.Faint)

	// Profile caching: check cache first
	meta, cachedErr := profile.LoadCachedWithTime(npub)
	showedCached := false

	if cachedErr == nil && meta != nil && profile.IsCacheFresh(npub) {
		// Cache is fresh (< 1 hour), show it with indicator
		showedCached = true
		pubHex, _ := crypto.NpubToHex(npub)
		fmt.Printf("%s %s\n", label("npub:"), npub)
		printColorField(label, "Name", meta.Name)
		printColorField(label, "Display Name", meta.DisplayName)
		printColorField(label, "About", meta.About)
		printColorField(label, "Picture", meta.Picture)
		printNIP05Field(label, meta.NIP05, pubHex)
		printColorField(label, "Banner", meta.Banner)
		printColorField(label, "Website", meta.Website)
		printColorField(label, "Lightning", meta.LUD16)
		dim.Println("  (cached)")
	}

	// Fetch fresh from relays
	relays, relayErr := config.LoadRelays(npub)
	if relayErr == nil && len(relays) > 0 {
		ctx := context.Background()
		fresh, err := profile.FetchFromRelays(ctx, npub, relays)
		if err == nil && fresh != nil {
			meta = fresh
			_ = profile.SaveCached(npub, meta)

			if !showedCached {
				pubHex, _ := crypto.NpubToHex(npub)
				fmt.Printf("%s %s\n", label("npub:"), npub)
				printColorField(label, "Name", meta.Name)
				printColorField(label, "Display Name", meta.DisplayName)
				printColorField(label, "About", meta.About)
				printColorField(label, "Picture", meta.Picture)
				printNIP05Field(label, meta.NIP05, pubHex)
				printColorField(label, "Banner", meta.Banner)
				printColorField(label, "Website", meta.Website)
				printColorField(label, "Lightning", meta.LUD16)
			}
			return nil
		}
	}

	// If we didn't show cached and couldn't fetch, show whatever we have
	if !showedCached {
		if meta == nil {
			meta = &profile.Metadata{}
		}
		pubHex, _ := crypto.NpubToHex(npub)
		fmt.Printf("%s %s\n", label("npub:"), npub)
		printColorField(label, "Name", meta.Name)
		printColorField(label, "Display Name", meta.DisplayName)
		printColorField(label, "About", meta.About)
		printColorField(label, "Picture", meta.Picture)
		printNIP05Field(label, meta.NIP05, pubHex)
		printColorField(label, "Banner", meta.Banner)
		printColorField(label, "Website", meta.Website)
		printColorField(label, "Lightning", meta.LUD16)
		if cachedErr == nil {
			dim.Println("  (from cache - relays unreachable)")
		}
	}

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
	meta.Website = promptField(reader, "Website", meta.Website)

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

// printNIP05Field prints the NIP-05 field with verification status.
func runProfileRm(cmd *cobra.Command, args []string) error {
	green := color.New(color.FgGreen)
	target := args[0]

	// Resolve to npub
	npub := target
	if !strings.HasPrefix(target, "npub1") {
		resolved, err := resolve.ResolveToNpub("", target)
		if err != nil {
			return fmt.Errorf("cannot resolve %q to a profile: %w", target, err)
		}
		npub = resolved
	}

	// Check it exists locally
	if !config.HasNsec(npub) {
		return fmt.Errorf("no local profile found for %s", target)
	}

	// Confirm
	name := resolveProfileName(npub)
	if name != "" {
		fmt.Printf("Remove profile %s (%s)? This deletes the local keys and cache. [y/N] ", name, npub)
	} else {
		fmt.Printf("Remove profile %s? This deletes the local keys and cache. [y/N] ", npub)
	}
	var answer string
	fmt.Scanln(&answer)
	if answer != "y" && answer != "Y" {
		fmt.Println("Cancelled.")
		return nil
	}

	if err := config.RemoveProfile(npub); err != nil {
		return err
	}

	if name != "" {
		green.Printf("✓ Removed profile %s (%s)\n", name, npub)
	} else {
		green.Printf("✓ Removed profile %s\n", npub)
	}
	return nil
}

// printNIP05Field prints the NIP-05 field with verification status.
func printNIP05Field(label func(a ...interface{}) string, nip05Addr string, pubHex string) {
	if nip05Addr == "" {
		return
	}
	green := color.New(color.FgGreen).SprintFunc()
	red := color.New(color.FgRed).SprintFunc()

	verified := nip05.Verify(nip05Addr, pubHex)
	if verified {
		fmt.Printf("%-14s %s %s\n", label("NIP-05:"), nip05Addr, green("✓"))
	} else {
		fmt.Printf("%-14s %s %s\n", label("NIP-05:"), nip05Addr, red("✗"))
	}
}
