package cmd

import (
	"bufio"
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"github.com/xdamman/nostr-cli/internal/config"
	"github.com/xdamman/nostr-cli/internal/crypto"
	"github.com/xdamman/nostr-cli/internal/nip05"
	"github.com/xdamman/nostr-cli/internal/profile"
	"github.com/xdamman/nostr-cli/internal/resolve"
	"github.com/xdamman/nostr-cli/internal/ui"
)

var profileRefreshFlag bool

var profileCmd = &cobra.Command{
	Use:     "profile [profile]",
	Short:   "Manage and view profiles",
	Long: `View a user's profile metadata (kind 0).

Without arguments, shows your own profile. With a profile argument, looks up
that user. A <profile> can be an npub, alias, or NIP-05 address (user@domain.com).

Use --refresh to force a fresh fetch from relays instead of cache.

Output formats:
  (default)  Human-readable field listing
  --json     Pretty-printed JSON with all profile fields
  --jsonl    Compact single-line JSON
  --raw      Raw JSON

Examples:
  nostr profile
  nostr profile alice
  nostr profile npub1... --json
  nostr profile alice@example.com --refresh`,
	GroupID: "profile",
	RunE:    runProfile,
}

var profileUpdateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update your profile metadata interactively",
	Long: `Interactively update your profile fields (name, display name, about, picture, NIP-05, website).

Changes are saved locally and published to your configured relays.

Examples:
  nostr profile update`,
	RunE: runProfileUpdate,
}

var profileRmCmd = &cobra.Command{
	Use:   "rm [account]",
	Short: "Remove a local account (account: npub, alias, or nip05)",
	Long:  "Remove a local account. Without arguments, select interactively.\nAn <account> can be an npub, alias, or NIP-05 address.",
	RunE:  runProfileRm,
}

func init() {
	profileCmd.Flags().BoolVar(&profileRefreshFlag, "refresh", false, "Fetch fresh profile from relays")
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
	npub, err := loadProfile()
	if err != nil {
		return err
	}

	if rawFlag || jsonFlag || jsonlFlag {
		return showProfileJSON(npub)
	}

	return showProfile(npub, label)
}

func lookupUserProfile(user string, label func(a ...interface{}) string, errColor *color.Color) error {
	activeNpub, _ := config.ActiveProfile()

	npub := user
	if !strings.HasPrefix(user, "npub1") {
		resolved, err := resolve.ResolveToNpub(activeNpub, user)
		if err != nil {
			errColor.Fprintf(os.Stderr, "Error: user %q not found\n", user)
			os.Exit(1)
		}
		npub = resolved
	}

	if rawFlag || jsonFlag || jsonlFlag {
		return showProfileJSON(npub)
	}

	if profileRefreshFlag {
		return fetchAndShowProfile(npub, user, label, errColor)
	}

	// Try cache first
	meta, err := profile.LoadCached(npub)
	if err == nil && meta != nil {
		printProfileFields(npub, meta, label)
		printCacheHint(npub)
		return nil
	}

	// No cache — fetch from relays
	return fetchAndShowProfile(npub, user, label, errColor)
}

func fetchAndShowProfile(npub, user string, label func(a ...interface{}) string, errColor *color.Color) error {
	activeNpub, _ := config.ActiveProfile()

	var relays []string
	if activeNpub != "" {
		relays, _ = config.LoadRelays(activeNpub)
	}
	targetRelays, _ := config.LoadRelaysWithFallback(npub)
	seen := make(map[string]bool, len(relays))
	for _, r := range relays {
		seen[r] = true
	}
	for _, r := range targetRelays {
		if !seen[r] {
			seen[r] = true
			relays = append(relays, r)
		}
	}
	for _, r := range config.DefaultRelays() {
		if !seen[r] {
			relays = append(relays, r)
		}
	}

	ctx := context.Background()

	sp := ui.NewSpinner("Fetching profile from relays...")
	meta, err := profile.FetchFromRelays(ctx, npub, relays)
	sp.Stop()
	if err != nil || meta == nil {
		errColor.Fprintf(os.Stderr, "Error: user %q not found\n", user)
		os.Exit(1)
	}

	_ = profile.SaveCached(npub, meta)

	// Fetch and cache NIP-65 relay list
	sp = ui.NewSpinner("Fetching relay list...")
	pubHex, _ := crypto.NpubToHex(npub)
	fetchedRelays := fetchRelayList(ctx, pubHex, relays)
	sp.Stop()
	if len(fetchedRelays) > 0 {
		_ = config.SaveCachedRelays(npub, fetchedRelays)
	}

	printProfileFields(npub, meta, label)
	return nil
}

// showProfileJSON outputs the cached profile as JSON. No relay fetch.
func showProfileJSON(npub string) error {
	meta, err := profile.LoadCached(npub)
	if err != nil || meta == nil {
		// No cache — fetch from relays and save
		activeNpub, _ := config.ActiveProfile()
		var relays []string
		if activeNpub != "" {
			relays, _ = config.LoadRelays(activeNpub)
		}
		targetRelays, _ := config.LoadRelaysWithFallback(npub)
		seen := make(map[string]bool, len(relays))
		for _, r := range relays {
			seen[r] = true
		}
		for _, r := range targetRelays {
			if !seen[r] {
				relays = append(relays, r)
			}
		}
		for _, r := range config.DefaultRelays() {
			if !seen[r] {
				relays = append(relays, r)
			}
		}
		ctx := context.Background()
		meta, err = profile.FetchFromRelays(ctx, npub, relays)
		if err != nil || meta == nil {
			return fmt.Errorf("profile not found for %s", npub)
		}
		_ = profile.SaveCached(npub, meta)
	}

	obj := map[string]interface{}{
		"npub": npub,
	}
	if meta.Name != "" {
		obj["name"] = meta.Name
	}
	if meta.DisplayName != "" {
		obj["display_name"] = meta.DisplayName
	}
	if meta.About != "" {
		obj["about"] = meta.About
	}
	if meta.Picture != "" {
		obj["picture"] = meta.Picture
	}
	if meta.NIP05 != "" {
		obj["nip05"] = meta.NIP05
	}
	if meta.Banner != "" {
		obj["banner"] = meta.Banner
	}
	if meta.Website != "" {
		obj["website"] = meta.Website
	}
	if meta.LUD16 != "" {
		obj["lud16"] = meta.LUD16
	}

	if rawFlag {
		printRaw(obj)
	} else if jsonlFlag {
		printJSONL(obj)
	} else {
		printJSON(obj)
	}
	return nil
}

func showProfile(npub string, label func(a ...interface{}) string) error {
	errColor := color.New(color.FgRed)

	if profileRefreshFlag {
		return fetchAndShowProfile(npub, npub, label, errColor)
	}

	// Try cache first
	meta, err := profile.LoadCached(npub)
	if err == nil && meta != nil {
		printProfileFields(npub, meta, label)
		printCacheHint(npub)
		return nil
	}

	// No cache — fetch from relays
	return fetchAndShowProfile(npub, npub, label, errColor)
}

func printProfileFields(npub string, meta *profile.Metadata, label func(a ...interface{}) string) {
	dim := color.New(color.Faint)
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

	relays, _ := config.LoadRelaysWithFallback(npub)
	if len(relays) > 0 {
		fmt.Printf("%-14s\n", label("Relays:"))
		for _, r := range relays {
			if u, err := url.Parse(r); err == nil && u.Host != "" {
				dim.Printf("  %s\n", u.Host)
			} else {
				dim.Printf("  %s\n", r)
			}
		}
	}
}

func printCacheHint(npub string) {
	dim := color.New(color.Faint)
	age, err := profile.CacheAge(npub)
	if err != nil {
		return
	}
	fmt.Println()
	dim.Printf("  Last refreshed %s ago. Run with --refresh to fetch from relays.\n", humanDuration(age))
}

func humanDuration(d time.Duration) string {
	if d < time.Minute {
		return "just now"
	}
	if d < time.Hour {
		m := int(d.Minutes())
		if m == 1 {
			return "1 minute"
		}
		return fmt.Sprintf("%d minutes", m)
	}
	if d < 24*time.Hour {
		h := int(d.Hours())
		if h == 1 {
			return "1 hour"
		}
		return fmt.Sprintf("%d hours", h)
	}
	days := int(d.Hours() / 24)
	if days == 1 {
		return "1 day"
	}
	return fmt.Sprintf("%d days", days)
}

func runProfileUpdate(cmd *cobra.Command, args []string) error {
	npub, err := loadProfile()
	if err != nil {
		return err
	}

	// Load existing metadata
	meta, _ := profile.LoadCached(npub)
	if meta == nil {
		meta = &profile.Metadata{}
	}

	reader := bufio.NewReader(os.Stdin)

	meta.Name = promptField(reader, "Username", meta.Name)
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

	// Create and sign the metadata event
	event, err := profile.CreateMetadataEvent(npub, meta)
	if err != nil {
		return fmt.Errorf("failed to create event: %w", err)
	}

	fmt.Println("Publishing profile to relays...")
	timeout := time.Duration(timeoutFlag) * time.Millisecond
	_, err = ui.PublishEventToRelays(npub, event, relays, timeout)
	if err != nil {
		return fmt.Errorf("failed to publish: %w", err)
	}

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

	var npub, displayLabel string

	if len(args) > 0 {
		target := args[0]
		npub = target
		if !strings.HasPrefix(target, "npub1") {
			resolved, err := resolve.ResolveToNpub("", target)
			if err != nil {
				return fmt.Errorf("cannot resolve %q to an account: %w", target, err)
			}
			npub = resolved
		}
		if !config.HasNsec(npub) {
			return fmt.Errorf("no local account found for %s", target)
		}
		displayLabel = target
	} else {
		// Interactive selection
		entries, err := listSwitchableProfiles()
		if err != nil {
			return err
		}
		if len(entries) == 0 {
			fmt.Println("No accounts found.")
			return nil
		}

		chosen, err := interactiveSelect(entries, 0, "")
		if err != nil {
			return err
		}
		if chosen < 0 {
			return nil // user cancelled
		}
		npub = entries[chosen].npub
		displayLabel = npub
	}

	// Confirm
	name := resolveProfileName(npub)
	if name != "" {
		displayLabel = fmt.Sprintf("%s (%s)", name, npub)
	}
	fmt.Printf("Remove account %s? This deletes the local keys and cache. [y/N] ", displayLabel)
	var answer string
	fmt.Scanln(&answer)
	if answer != "y" && answer != "Y" {
		fmt.Println("Cancelled.")
		return nil
	}

	if err := config.RemoveProfile(npub); err != nil {
		return err
	}

	green.Printf("✓ Removed account %s\n", displayLabel)
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
