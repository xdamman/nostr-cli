package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/fatih/color"
	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip19"
	"github.com/spf13/cobra"
	"github.com/xdamman/nostr-cli/internal/cache"
	"github.com/xdamman/nostr-cli/internal/config"
	"github.com/xdamman/nostr-cli/internal/crypto"
	"github.com/xdamman/nostr-cli/internal/nip05"
	"github.com/xdamman/nostr-cli/internal/profile"
	internalRelay "github.com/xdamman/nostr-cli/internal/relay"
	"github.com/xdamman/nostr-cli/internal/resolve"
	"github.com/xdamman/nostr-cli/internal/ui"
)

var (
	profileRefreshFlag bool
	profileEventsFlag  int
	profileKindsFlag   string
	profileWatchFlag   bool
)

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
	profileCmd.Flags().IntVarP(&profileEventsFlag, "events", "n", 0, "Number of past events to show")
	profileCmd.Flags().StringVar(&profileKindsFlag, "kinds", "1", "Event kinds to show (comma-separated)")
	profileCmd.Flags().BoolVar(&profileWatchFlag, "watch", false, "Watch for new events in real-time")
	profileCmd.AddCommand(profileUpdateCmd)
	rootCmd.AddCommand(profileCmd)
}

func runProfile(cmd *cobra.Command, args []string) error {
	label := color.New(color.FgCyan).SprintFunc()
	errColor := color.New(color.FgRed)

	// If --watch is set and -n is not explicitly set, default to 5
	if profileWatchFlag && !cmd.Flags().Changed("events") {
		profileEventsFlag = 5
	}

	var targetNpub string

	if len(args) > 0 {
		// User specified a target — look them up, do NOT fall back to current user
		userArg := strings.TrimPrefix(args[0], "@")

		// For JSON output with events, we need to handle differently
		if (rawFlag || jsonFlag || jsonlFlag) && (profileEventsFlag > 0 || profileWatchFlag) {
			// Resolve npub first
			activeNpub, _ := config.ActiveProfile()
			npub := userArg
			if !strings.HasPrefix(userArg, "npub1") {
				resolved, err := resolve.ResolveToNpub(activeNpub, userArg)
				if err != nil {
					errColor.Fprintf(os.Stderr, "Error: user %q not found\n", userArg)
					os.Exit(1)
				}
				npub = resolved
			}
			return showProfileWithEvents(npub)
		}

		if err := lookupUserProfile(userArg, label, errColor); err != nil {
			return err
		}

		// Resolve npub for events
		activeNpub, _ := config.ActiveProfile()
		npub := userArg
		if !strings.HasPrefix(userArg, "npub1") {
			resolved, err := resolve.ResolveToNpub(activeNpub, userArg)
			if err != nil {
				return nil // profile was shown, events fail silently
			}
			npub = resolved
		}
		targetNpub = npub
	} else {
		// No args — show current user's profile
		npub, err := loadAccount()
		if err != nil {
			return err
		}

		if rawFlag || jsonFlag || jsonlFlag {
			if profileEventsFlag > 0 || profileWatchFlag {
				return showProfileWithEvents(npub)
			}
			return showProfileJSON(npub)
		}

		if err := showProfile(npub, label); err != nil {
			return err
		}
		targetNpub = npub
	}

	// Show events if requested
	if profileEventsFlag > 0 || profileWatchFlag {
		return showProfileEvents(targetNpub)
	}

	return nil
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

	// Use cached relays only — no network fetch for display
	relays, err := config.LoadRelays(npub)
	if err != nil {
		relays, _ = config.LoadCachedRelays(npub)
	}
	if len(relays) > 0 {
		fmt.Printf("%-14s\n", label("Relays:"))
		for _, r := range relays {
			if u, err := url.Parse(r); err == nil && u.Host != "" {
				dim.Printf("  %s\n", u.Host)
			} else {
				dim.Printf("  %s\n", r)
			}
		}
	} else if !profileRefreshFlag {
		dim.Printf("%-14s %s\n", label("Relays:"), "No relays cached (use --refresh to fetch)")
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
	npub, err := loadAccount()
	if err != nil {
		return err
	}

	// Load existing metadata
	meta, _ := profile.LoadCached(npub)
	if meta == nil {
		meta = &profile.Metadata{}
	}

	result := ui.RunProfileForm(ui.ProfileFormConfig{
		Title: "Update your Nostr profile",
		Fields: []ui.ProfileField{
			{Label: "Username", Key: "name", Value: meta.Name, Hint: "A short name for @mentions"},
			{Label: "Display name", Key: "display_name", Value: meta.DisplayName},
			{Label: "About", Key: "about", Value: meta.About},
			{Label: "Picture URL", Key: "picture", Value: meta.Picture},
			{Label: "NIP-05", Key: "nip05", Value: meta.NIP05, Hint: "user@domain.com for verification"},
			{Label: "Website", Key: "website", Value: meta.Website},
		},
	})
	if result.Cancelled {
		fmt.Println("Cancelled.")
		return nil
	}
	meta.Name = result.Values["name"]
	meta.DisplayName = result.Values["display_name"]
	meta.About = result.Values["about"]
	meta.Picture = result.Values["picture"]
	meta.NIP05 = result.Values["nip05"]
	meta.Website = result.Values["website"]

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
// Uses cached verification result when available (< 24h old).
// Only verifies on --refresh; otherwise shows the address without ✓/✗.
func printNIP05Field(label func(a ...interface{}) string, nip05Addr string, pubHex string) {
	if nip05Addr == "" {
		return
	}
	green := color.New(color.FgGreen).SprintFunc()
	red := color.New(color.FgRed).SprintFunc()
	dim := color.New(color.Faint).SprintFunc()

	// Derive npub for cache lookup
	npubForCache := ""
	if npub, err := nip19.EncodePublicKey(pubHex); err == nil {
		npubForCache = npub
	}

	if profileRefreshFlag {
		// Force verify and cache the result
		verified := nip05.Verify(nip05Addr, pubHex)
		if npubForCache != "" {
			_ = cache.SaveNIP05Cache(npubForCache, nip05Addr, verified)
		}
		if verified {
			fmt.Printf("%-14s %s %s\n", label("NIP-05:"), nip05Addr, green("✓"))
		} else {
			fmt.Printf("%-14s %s %s\n", label("NIP-05:"), nip05Addr, red("✗"))
		}
		return
	}

	// Try cached result (< 24h)
	if npubForCache != "" {
		if cached := cache.LoadNIP05Cache(npubForCache, 24*time.Hour); cached != nil && cached.NIP05 == nip05Addr {
			if cached.Verified {
				fmt.Printf("%-14s %s %s\n", label("NIP-05:"), nip05Addr, green("✓"))
			} else {
				fmt.Printf("%-14s %s %s\n", label("NIP-05:"), nip05Addr, red("✗"))
			}
			return
		}
	}

	// No cached result and not refreshing — show without verification
	fmt.Printf("%-14s %s %s\n", label("NIP-05:"), nip05Addr, dim("(unverified)"))
}

// parseKinds parses a comma-separated list of event kinds into []int.
func parseKinds(s string) ([]int, error) {
	parts := strings.Split(s, ",")
	kinds := make([]int, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		k, err := strconv.Atoi(p)
		if err != nil {
			return nil, fmt.Errorf("invalid kind %q: %w", p, err)
		}
		kinds = append(kinds, k)
	}
	if len(kinds) == 0 {
		return []int{1}, nil
	}
	return kinds, nil
}

// getRelaysForProfile returns merged relays for fetching events.
func getRelaysForProfile(npub string) []string {
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
	return relays
}

// formatEventLine formats a single event for human-readable display.
func formatEventLine(ev *nostr.Event) string {
	ts := time.Unix(int64(ev.CreatedAt), 0).Format("2006-01-02 15:04")
	kindLabel := fmt.Sprintf("[kind %d]", ev.Kind)

	content := ev.Content
	// For articles (kind 30023), show title tag if present
	if ev.Kind == 30023 {
		for _, tag := range ev.Tags {
			if len(tag) >= 2 && tag[0] == "title" {
				content = tag[1] + " (article)"
				break
			}
		}
	}

	// Render mentions and inline markdown
	content = renderMentions(content)
	content = renderInlineMarkdown(content)

	// Truncate to ~200 chars
	if len(content) > 200 {
		content = content[:200] + "..."
	}

	// Replace newlines with spaces for single-line display
	content = strings.ReplaceAll(content, "\n", " ")

	return fmt.Sprintf("  %s  %s %s", ts, kindLabel, content)
}

// showProfileEvents fetches and displays events for a profile (human-readable mode).
func showProfileEvents(npub string) error {
	pubHex, err := crypto.NpubToHex(npub)
	if err != nil {
		return fmt.Errorf("invalid npub: %w", err)
	}

	kinds, err := parseKinds(profileKindsFlag)
	if err != nil {
		return err
	}

	relays := getRelaysForProfile(npub)
	if len(relays) == 0 {
		return fmt.Errorf("no relays available")
	}

	dim := color.New(color.Faint)
	ctx := context.Background()

	if profileEventsFlag > 0 {
		filter := nostr.Filter{
			Authors: []string{pubHex},
			Kinds:   kinds,
			Limit:   profileEventsFlag,
		}

		events, err := internalRelay.FetchEvents(ctx, filter, relays)
		if err != nil {
			return fmt.Errorf("failed to fetch events: %w", err)
		}

		if len(events) > 0 {
			sort.Slice(events, func(i, j int) bool {
				return events[i].CreatedAt > events[j].CreatedAt
			})
			if len(events) > profileEventsFlag {
				events = events[:profileEventsFlag]
			}

			fmt.Println()
			dim.Printf("─── Last %d events ───\n", len(events))
			for _, ev := range events {
				fmt.Println(formatEventLine(ev))
			}
		}
	}

	if profileWatchFlag {
		return watchProfileEvents(npub, pubHex, kinds, relays, dim)
	}

	return nil
}

// showProfileWithEvents handles JSON/JSONL/raw output with events included.
func showProfileWithEvents(npub string) error {
	pubHex, err := crypto.NpubToHex(npub)
	if err != nil {
		return fmt.Errorf("invalid npub: %w", err)
	}

	kinds, err := parseKinds(profileKindsFlag)
	if err != nil {
		return err
	}

	relays := getRelaysForProfile(npub)

	// Load profile metadata
	meta, err := profile.LoadCached(npub)
	if err != nil || meta == nil {
		ctx := context.Background()
		meta, err = profile.FetchFromRelays(ctx, npub, relays)
		if err != nil || meta == nil {
			return fmt.Errorf("profile not found for %s", npub)
		}
		_ = profile.SaveCached(npub, meta)
	}

	profileObj := map[string]interface{}{
		"npub": npub,
	}
	if meta.Name != "" {
		profileObj["name"] = meta.Name
	}
	if meta.DisplayName != "" {
		profileObj["display_name"] = meta.DisplayName
	}
	if meta.About != "" {
		profileObj["about"] = meta.About
	}
	if meta.Picture != "" {
		profileObj["picture"] = meta.Picture
	}
	if meta.NIP05 != "" {
		profileObj["nip05"] = meta.NIP05
	}
	if meta.Banner != "" {
		profileObj["banner"] = meta.Banner
	}
	if meta.Website != "" {
		profileObj["website"] = meta.Website
	}
	if meta.LUD16 != "" {
		profileObj["lud16"] = meta.LUD16
	}

	ctx := context.Background()

	// Fetch events
	var events []*nostr.Event
	if profileEventsFlag > 0 {
		filter := nostr.Filter{
			Authors: []string{pubHex},
			Kinds:   kinds,
			Limit:   profileEventsFlag,
		}
		events, _ = internalRelay.FetchEvents(ctx, filter, relays)
		if len(events) > 0 {
			sort.Slice(events, func(i, j int) bool {
				return events[i].CreatedAt > events[j].CreatedAt
			})
			if len(events) > profileEventsFlag {
				events = events[:profileEventsFlag]
			}
		}
	}

	if rawFlag || jsonlFlag {
		// JSONL: profile line first, then event lines
		profileLine, _ := json.Marshal(profileObj)
		fmt.Println(string(profileLine))
		for _, ev := range events {
			evJSON, _ := json.Marshal(ev)
			fmt.Println(string(evJSON))
		}
	} else if jsonFlag {
		// JSON: object with profile and events
		output := map[string]interface{}{
			"profile": profileObj,
			"events":  events,
		}
		printJSON(output)
	}

	// Watch mode for JSONL/raw streaming
	if profileWatchFlag {
		return watchProfileEventsJSON(pubHex, kinds, relays)
	}

	return nil
}

// watchProfileEvents streams new events in human-readable format.
func watchProfileEvents(npub, pubHex string, kinds []int, relays []string, dim *color.Color) error {
	fmt.Println()
	dim.Println("─── Watching for new events (Ctrl+C to exit) ───")
	fmt.Println()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println("\nStopped watching.")
		cancel()
		os.Exit(0)
	}()

	since := nostr.Now()

	for _, relayURL := range relays {
		go func(url string) {
			connectCtx, connectCancel := context.WithTimeout(ctx, internalRelay.ConnectTimeout)
			defer connectCancel()

			relay, err := nostr.RelayConnect(connectCtx, url)
			if err != nil {
				return
			}
			defer relay.Close()

			filters := nostr.Filters{
				{
					Authors: []string{pubHex},
					Kinds:   kinds,
					Since:   &since,
				},
			}

			sub, err := relay.Subscribe(ctx, filters)
			if err != nil {
				return
			}
			defer sub.Unsub()

			for {
				select {
				case <-ctx.Done():
					return
				case ev, ok := <-sub.Events:
					if !ok {
						return
					}
					fmt.Println(formatEventLine(ev))
				}
			}
		}(relayURL)
	}

	<-ctx.Done()
	return nil
}

// watchProfileEventsJSON streams new events in JSON format.
func watchProfileEventsJSON(pubHex string, kinds []int, relays []string) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
		os.Exit(0)
	}()

	since := nostr.Now()

	for _, relayURL := range relays {
		go func(url string) {
			connectCtx, connectCancel := context.WithTimeout(ctx, internalRelay.ConnectTimeout)
			defer connectCancel()

			relay, err := nostr.RelayConnect(connectCtx, url)
			if err != nil {
				return
			}
			defer relay.Close()

			filters := nostr.Filters{
				{
					Authors: []string{pubHex},
					Kinds:   kinds,
					Since:   &since,
				},
			}

			sub, err := relay.Subscribe(ctx, filters)
			if err != nil {
				return
			}
			defer sub.Unsub()

			for {
				select {
				case <-ctx.Done():
					return
				case ev, ok := <-sub.Events:
					if !ok {
						return
					}
					evJSON, _ := json.Marshal(ev)
					fmt.Println(string(evJSON))
				}
			}
		}(relayURL)
	}

	<-ctx.Done()
	return nil
}
