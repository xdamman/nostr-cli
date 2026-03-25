package cmd

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip19"
	"github.com/spf13/cobra"
	"github.com/xdamman/nostr-cli/internal/cache"
	"github.com/xdamman/nostr-cli/internal/config"
	"github.com/xdamman/nostr-cli/internal/crypto"
	internalRelay "github.com/xdamman/nostr-cli/internal/relay"
	"github.com/xdamman/nostr-cli/internal/profile"
	"github.com/xdamman/nostr-cli/internal/resolve"
	"github.com/xdamman/nostr-cli/internal/ui"
)

var followingRefreshFlag bool
var followJSONFlag bool

var followCmd = &cobra.Command{
	Use:     "follow <profile>",
	Short:   "Follow a profile",
	GroupID: "social",
	Args:  exactArgs(1),
	RunE:  runFollow,
}

var unfollowCmd = &cobra.Command{
	Use:     "unfollow <profile>",
	Short:   "Unfollow a profile",
	GroupID: "social",
	Args:    exactArgs(1),
	RunE:    runUnfollow,
}

var followingCmd = &cobra.Command{
	Use:     "following",
	Short:   "List accounts you follow",
	GroupID: "social",
	RunE:    runFollowing,
}

func init() {
	followCmd.Flags().BoolVar(&followJSONFlag, "json", false, "Output as JSON")
	unfollowCmd.Flags().BoolVar(&followJSONFlag, "json", false, "Output as JSON")
	followingCmd.Flags().BoolVar(&followJSONFlag, "json", false, "Output as JSON")
	followingCmd.Flags().BoolVar(&followingRefreshFlag, "refresh", false, "Force refresh from relays")
	rootCmd.AddCommand(followCmd)
	rootCmd.AddCommand(unfollowCmd)
	rootCmd.AddCommand(followingCmd)
}

func runFollow(cmd *cobra.Command, args []string) error {
	green := color.New(color.FgGreen)
	yellow := color.New(color.FgYellow)

	npub, err := config.LoadResolvedProfile(profileFlag)
	if err != nil {
		return err
	}

	targetHex, err := resolve.Resolve(npub, args[0])
	if err != nil {
		return fmt.Errorf("cannot resolve user: %w", err)
	}

	myHex, err := crypto.NpubToHex(npub)
	if err != nil {
		return err
	}

	if targetHex == myHex {
		yellow.Println("⚠ Following yourself")
	}

	relays, err := config.LoadRelays(npub)
	if err != nil {
		return err
	}

	targetNpub, _ := nip19.EncodePublicKey(targetHex)

	sp := ui.NewSpinner("Fetching contact list...")
	ctx := context.Background()
	contacts, err := fetchContactList(ctx, myHex, relays)
	sp.Stop()
	if err != nil {
		return err
	}

	// Check if already following
	for _, tag := range contacts.Tags {
		if len(tag) >= 2 && tag[0] == "p" && tag[1] == targetHex {
			if followJSONFlag {
				result := map[string]interface{}{
					"ok":              false,
					"action":          "follow",
					"user":            targetNpub,
					"error":           "already following",
					"following_count": len(contacts.Tags),
				}
				jsonBytes, _ := json.Marshal(result)
				fmt.Println(string(jsonBytes))
			} else {
				yellow.Printf("Already following %s\n", targetNpub)
			}
			return nil
		}
	}

	// Add to contact list
	contacts.Tags = append(contacts.Tags, nostr.Tag{"p", targetHex})
	contacts.CreatedAt = nostr.Now()
	contacts.ID = ""
	contacts.Sig = ""

	nsec, err := config.LoadNsec(npub)
	if err != nil {
		return err
	}
	skHex, err := crypto.NsecToHex(nsec)
	if err != nil {
		return err
	}

	if err := contacts.Sign(skHex); err != nil {
		return fmt.Errorf("failed to sign: %w", err)
	}

	timeout := time.Duration(timeoutFlag) * time.Millisecond

	if rawFlag {
		_, _ = ui.PublishEventSilent(npub, *contacts, relays, timeout)
		cacheFollowingFromTags(npub, contacts.Tags)
		ui.PrintRawEvent(*contacts)
		return nil
	}

	// Publish using the shared interactive relay publishing
	fmt.Println("Publishing updated contact list...")
	_, err = ui.PublishEventToRelays(npub, *contacts, relays, timeout)
	if err != nil {
		return err
	}

	// Update following cache
	cacheFollowingFromTags(npub, contacts.Tags)

	if followJSONFlag {
		result := map[string]interface{}{
			"ok":              true,
			"action":          "follow",
			"user":            targetNpub,
			"following_count": len(contacts.Tags),
		}
		jsonBytes, _ := json.Marshal(result)
		fmt.Println(string(jsonBytes))
		return nil
	}

	green.Printf("✓ Now following %s\n", targetNpub)

	// Show the target's profile
	fmt.Println()
	label := color.New(color.FgCyan).SprintFunc()
	meta, _ := profile.LoadCached(targetNpub)
	if meta == nil {
		// Try fetching from relays
		meta, _ = profile.FetchFromRelays(ctx, targetNpub, relays)
		if meta != nil {
			_ = profile.SaveCached(targetNpub, meta)
		}
	}
	if meta != nil {
		printProfileFields(targetNpub, meta, label)
		fmt.Println()

		// Prompt for alias — default to profile name
		defaultName := meta.Name
		if defaultName == "" {
			defaultName = meta.DisplayName
		}

		// Check if alias already exists for this npub
		existingAlias := ""
		if aliases, err := config.LoadAliases(npub); err == nil {
			for a, n := range aliases {
				if n == targetNpub {
					existingAlias = a
					break
				}
			}
		}

		if existingAlias != "" {
			dim := color.New(color.Faint)
			dim.Printf("  Alias: %s\n", existingAlias)
		} else {
			defaultAlias := ""
			if defaultName != "" {
				defaultAlias = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(defaultName), " ", "-"))
			}
			if defaultAlias != "" {
				fmt.Printf("Create an alias for this user [%s]: ", defaultAlias)
			} else {
				fmt.Print("Create an alias for this user (enter to skip): ")
			}
			scanner := bufio.NewScanner(os.Stdin)
			if scanner.Scan() {
				alias := strings.TrimSpace(scanner.Text())
				if alias == "" && defaultAlias != "" {
					alias = defaultAlias
				}
				if alias != "" {
					if err := config.SetAlias(npub, alias, targetNpub); err != nil {
						fmt.Fprintf(os.Stderr, "Warning: could not set alias: %v\n", err)
					} else {
						green.Printf("✓ Alias %s → %s\n", alias, targetNpub)
					}
				}
			}
		}
	}

	return nil
}

func runUnfollow(cmd *cobra.Command, args []string) error {
	green := color.New(color.FgGreen)
	yellow := color.New(color.FgYellow)

	npub, err := config.LoadResolvedProfile(profileFlag)
	if err != nil {
		return err
	}

	targetHex, err := resolve.Resolve(npub, args[0])
	if err != nil {
		return fmt.Errorf("cannot resolve user: %w", err)
	}

	myHex, err := crypto.NpubToHex(npub)
	if err != nil {
		return err
	}

	relays, err := config.LoadRelays(npub)
	if err != nil {
		return err
	}

	targetNpub, _ := nip19.EncodePublicKey(targetHex)

	sp := ui.NewSpinner("Fetching contact list...")
	ctx := context.Background()
	contacts, err := fetchContactList(ctx, myHex, relays)
	sp.Stop()
	if err != nil {
		return err
	}

	// Find and remove
	found := false
	var newTags nostr.Tags
	for _, tag := range contacts.Tags {
		if len(tag) >= 2 && tag[0] == "p" && tag[1] == targetHex {
			found = true
			continue
		}
		newTags = append(newTags, tag)
	}

	if !found {
		if followJSONFlag {
			result := map[string]interface{}{
				"ok":              false,
				"action":          "unfollow",
				"user":            targetNpub,
				"error":           "not following",
				"following_count": len(contacts.Tags),
			}
			jsonBytes, _ := json.Marshal(result)
			fmt.Println(string(jsonBytes))
		} else {
			yellow.Printf("Not following %s\n", targetNpub)
		}
		return nil
	}

	contacts.Tags = newTags
	contacts.CreatedAt = nostr.Now()
	contacts.ID = ""
	contacts.Sig = ""

	nsec, err := config.LoadNsec(npub)
	if err != nil {
		return err
	}
	skHex, err := crypto.NsecToHex(nsec)
	if err != nil {
		return err
	}

	if err := contacts.Sign(skHex); err != nil {
		return fmt.Errorf("failed to sign: %w", err)
	}

	timeout := time.Duration(timeoutFlag) * time.Millisecond

	if rawFlag {
		_, _ = ui.PublishEventSilent(npub, *contacts, relays, timeout)
		cacheFollowingFromTags(npub, contacts.Tags)
		ui.PrintRawEvent(*contacts)
		return nil
	}

	// Publish using the shared interactive relay publishing
	fmt.Println("Publishing updated contact list...")
	_, err = ui.PublishEventToRelays(npub, *contacts, relays, timeout)
	if err != nil {
		return err
	}

	// Update following cache
	cacheFollowingFromTags(npub, contacts.Tags)

	if followJSONFlag {
		result := map[string]interface{}{
			"ok":              true,
			"action":          "unfollow",
			"user":            targetNpub,
			"following_count": len(contacts.Tags),
		}
		jsonBytes, _ := json.Marshal(result)
		fmt.Println(string(jsonBytes))
	} else {
		green.Printf("✓ Unfollowed %s\n", targetNpub)
	}
	return nil
}

func runFollowing(cmd *cobra.Command, args []string) error {
	npub, err := config.LoadResolvedProfile(profileFlag)
	if err != nil {
		return err
	}

	myHex, err := crypto.NpubToHex(npub)
	if err != nil {
		return err
	}

	// Load profile cache for name resolution
	cache.LoadProfileCache(npub)

	printActiveProfile(npub)

	cyan := color.New(color.FgCyan).SprintFunc()
	dim := color.New(color.Faint)

	// Try cache first (unless --refresh)
	if !followingRefreshFlag {
		if cached := cache.LoadFollowing(npub); cached != nil && len(cached.Hexes) > 0 {
			if followJSONFlag {
				var following []map[string]string
				for _, hex := range cached.Hexes {
					name := cache.ResolveNameByHex(hex)
					npubStr, _ := nip19.EncodePublicKey(hex)
					following = append(following, map[string]string{
						"npub": npubStr,
						"name": name,
					})
				}
				jsonBytes, _ := json.Marshal(following)
				fmt.Println(string(jsonBytes))
			} else {
				printFollowingList(cached.Hexes, cyan, dim)
				age := time.Since(cached.UpdatedAt).Truncate(time.Second)
				dim.Printf("  (cached %s ago)\n", formatDuration(age))
			}
			return nil
		}
	}

	relays, err := config.LoadRelays(npub)
	if err != nil {
		return err
	}

	sp := ui.NewSpinner("Fetching contact list...")
	ctx := context.Background()
	contacts, err := fetchContactList(ctx, myHex, relays)
	sp.Stop()
	if err != nil {
		return err
	}

	var hexes []string
	for _, tag := range contacts.Tags {
		if len(tag) >= 2 && tag[0] == "p" {
			hexes = append(hexes, tag[1])
		}
	}

	// Cache the result
	_ = cache.SaveFollowing(npub, hexes)

	if len(hexes) == 0 {
		if followJSONFlag {
			fmt.Println("[]")
		} else {
			fmt.Println("You're not following anyone yet.")
		}
		return nil
	}

	if followJSONFlag {
		var following []map[string]string
		for _, hex := range hexes {
			name := cache.ResolveNameByHex(hex)
			npubStr, _ := nip19.EncodePublicKey(hex)
			following = append(following, map[string]string{
				"npub": npubStr,
				"name": name,
			})
		}
		jsonBytes, _ := json.Marshal(following)
		fmt.Println(string(jsonBytes))
	} else {
		printFollowingList(hexes, cyan, dim)
	}
	return nil
}

func printFollowingList(hexes []string, cyan func(a ...interface{}) string, dim *color.Color) {
	dimFn := dim.SprintFunc()
	fmt.Printf("Following %d accounts:\n\n", len(hexes))
	for _, hex := range hexes {
		name := cache.ResolveNameByHex(hex)
		npubStr, _ := nip19.EncodePublicKey(hex)
		shortNpub := npubStr
		if len(shortNpub) > 20 {
			shortNpub = shortNpub[:20] + "..."
		}
		if name != "" {
			fmt.Printf("  %s %s\n", cyan(name), dimFn(shortNpub))
		} else {
			fmt.Printf("  %s\n", shortNpub)
		}
	}
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	return fmt.Sprintf("%dh", int(d.Hours()))
}

// cacheFollowingFromTags extracts hex pubkeys from contact list tags and caches them.
func cacheFollowingFromTags(npub string, tags nostr.Tags) {
	var hexes []string
	for _, tag := range tags {
		if len(tag) >= 2 && tag[0] == "p" {
			hexes = append(hexes, tag[1])
		}
	}
	_ = cache.SaveFollowing(npub, hexes)
}

// fetchContactList fetches the latest kind 3 event for the given pubkey, or returns an empty one.
func fetchContactList(ctx context.Context, pubHex string, relayURLs []string) (*nostr.Event, error) {
	filter := nostr.Filter{
		Authors: []string{pubHex},
		Kinds:   []int{nostr.KindFollowList},
		Limit:   1,
	}

	event, err := internalRelay.FetchEvent(ctx, filter, relayURLs)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch contact list: %w", err)
	}

	if event != nil {
		return event, nil
	}

	// Return empty contact list
	return &nostr.Event{
		PubKey: pubHex,
		Kind:   nostr.KindFollowList,
		Tags:   nostr.Tags{},
	}, nil
}
