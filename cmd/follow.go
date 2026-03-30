package cmd

import (
	"bufio"
	"context"
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
	"golang.org/x/term"
)

var (
	followingRefreshFlag bool
	followAliasFlag      string
)

var followCmd = &cobra.Command{
	Use:     "follow <account>",
	Short:   "Follow a user",
	GroupID: "social",
	Long: `Follow a user by adding them to your contact list (kind 3).

An <account> can be an npub, alias, or NIP-05 address.
After following, you'll be prompted to create an alias for quick access.

Output formats:
  --json/--jsonl  Structured result with follow count

Examples:
  nostr follow npub1...
  nostr follow alice@example.com
  nostr follow alice --json`,
	Args: exactArgs(1),
	RunE: runFollow,
}

var unfollowCmd = &cobra.Command{
	Use:     "unfollow <account>",
	Short:   "Unfollow a user",
	GroupID: "social",
	Long: `Remove a user from your contact list (kind 3).

Examples:
  nostr unfollow npub1...
  nostr unfollow alice`,
	Args: exactArgs(1),
	RunE: runUnfollow,
}

var followingCmd = &cobra.Command{
	Use:     "following",
	Short:   "List accounts you follow",
	GroupID: "social",
	Long: `List all accounts in your contact list (kind 3).

Use --refresh to fetch fresh data from relays instead of cache.

Output formats:
  --json   Pretty-printed JSON array of {npub, name} objects
  --jsonl  Compact single-line JSON

Examples:
  nostr following
  nostr following --json
  nostr following --refresh`,
	RunE: runFollowing,
}

func init() {
	followCmd.Flags().StringVar(&followAliasFlag, "alias", "", "Set a local alias for this user")
	followingCmd.Flags().BoolVar(&followingRefreshFlag, "refresh", false, "Force refresh from relays")
	rootCmd.AddCommand(followCmd)
	rootCmd.AddCommand(unfollowCmd)
	rootCmd.AddCommand(followingCmd)
}

func runFollow(cmd *cobra.Command, args []string) error {
	green := color.New(color.FgGreen)
	yellow := color.New(color.FgYellow)

	npub, err := loadAccount()
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

	ctx := context.Background()
	var contacts *nostr.Event
	if rawFlag || jsonFlag || jsonlFlag {
		contacts, err = fetchContactList(ctx, myHex, relays)
	} else {
		sp := ui.NewSpinner("Fetching contact list...")
		contacts, err = fetchContactList(ctx, myHex, relays)
		sp.Stop()
	}
	if err != nil {
		return err
	}

	// Check if already following
	for _, tag := range contacts.Tags {
		if len(tag) >= 2 && tag[0] == "p" && tag[1] == targetHex {
			if jsonFlag || jsonlFlag {
				result := map[string]interface{}{
					"ok":              false,
					"action":          "follow",
					"user":            targetNpub,
					"error":           "already following",
					"following_count": len(contacts.Tags),
				}
				if jsonlFlag {
					printJSONL(result)
				} else {
					printJSON(result)
				}
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

	if rawFlag || jsonFlag || jsonlFlag {
		_, pubErr := ui.PublishEventSilent(npub, *contacts, relays, timeout)
		cacheFollowingFromTags(npub, contacts.Tags)
		if rawFlag {
			printRaw(contacts)
		} else {
			result := map[string]interface{}{
				"ok":              pubErr == nil,
				"action":          "follow",
				"user":            targetNpub,
				"following_count": len(contacts.Tags),
				"event_id":        contacts.ID,
				"event":           contacts,
				"relays":          relays,
			}
			if pubErr != nil {
				result["error"] = pubErr.Error()
			}
			if jsonlFlag {
				printJSONL(result)
			} else {
				printJSON(result)
			}
		}
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
			if !rawFlag && !jsonFlag && !jsonlFlag {
				dim := color.New(color.Faint)
				dim.Printf("  Alias: %s\n", existingAlias)
			}
		} else if followAliasFlag != "" {
			// Explicit --alias flag
			if err := config.SetAlias(npub, followAliasFlag, targetNpub); err != nil {
				fmt.Fprintf(os.Stderr, "Warning: could not set alias: %v\n", err)
			} else if !rawFlag && !jsonFlag && !jsonlFlag {
				green.Printf("✓ Alias %s → %s\n", followAliasFlag, targetNpub)
			}
		} else if term.IsTerminal(int(os.Stdin.Fd())) && !rawFlag && !jsonFlag && !jsonlFlag {
			// Interactive: prompt for alias
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
			// Non-interactive without --alias: skip alias silently
		}
	}

	return nil
}

func runUnfollow(cmd *cobra.Command, args []string) error {
	green := color.New(color.FgGreen)
	yellow := color.New(color.FgYellow)

	npub, err := loadAccount()
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

	ctx := context.Background()
	var contacts *nostr.Event
	if rawFlag || jsonFlag || jsonlFlag {
		contacts, err = fetchContactList(ctx, myHex, relays)
	} else {
		sp := ui.NewSpinner("Fetching contact list...")
		contacts, err = fetchContactList(ctx, myHex, relays)
		sp.Stop()
	}
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
		if jsonFlag || jsonlFlag {
			result := map[string]interface{}{
				"ok":              false,
				"action":          "unfollow",
				"user":            targetNpub,
				"error":           "not following",
				"following_count": len(contacts.Tags),
			}
			if jsonlFlag {
				printJSONL(result)
			} else {
				printJSON(result)
			}
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

	if jsonFlag || jsonlFlag {
		result := map[string]interface{}{
			"ok":              true,
			"action":          "unfollow",
			"user":            targetNpub,
			"following_count": len(contacts.Tags),
		}
		if jsonlFlag {
			printJSONL(result)
		} else {
			printJSON(result)
		}
	} else {
		green.Printf("✓ Unfollowed %s\n", targetNpub)
	}
	return nil
}

func runFollowing(cmd *cobra.Command, args []string) error {
	npub, err := loadAccount()
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
			if jsonFlag || jsonlFlag {
				var following []map[string]string
				for _, hex := range cached.Hexes {
					name := cache.ResolveNameByHex(hex)
					npubStr, _ := nip19.EncodePublicKey(hex)
					following = append(following, map[string]string{
						"npub": npubStr,
						"name": name,
					})
				}
				if jsonlFlag {
					printJSONL(following)
				} else {
					printJSON(following)
				}
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

	ctx := context.Background()
	var contacts *nostr.Event
	if rawFlag || jsonFlag || jsonlFlag {
		contacts, err = fetchContactList(ctx, myHex, relays)
	} else {
		sp := ui.NewSpinner("Fetching contact list...")
		contacts, err = fetchContactList(ctx, myHex, relays)
		sp.Stop()
	}
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
		if jsonFlag || jsonlFlag {
			fmt.Println("[]")
		} else {
			fmt.Println("You're not following anyone yet.")
		}
		return nil
	}

	if jsonFlag || jsonlFlag {
		var following []map[string]string
		for _, hex := range hexes {
			name := cache.ResolveNameByHex(hex)
			npubStr, _ := nip19.EncodePublicKey(hex)
			following = append(following, map[string]string{
				"npub": npubStr,
				"name": name,
			})
		}
		if jsonlFlag {
			printJSONL(following)
		} else {
			printJSON(following)
		}
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
