package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/fatih/color"
	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip19"
	"github.com/xdamman/nostr-cli/internal/cache"
	"github.com/xdamman/nostr-cli/internal/config"
	"github.com/xdamman/nostr-cli/internal/profile"
	internalRelay "github.com/xdamman/nostr-cli/internal/relay"
	"github.com/xdamman/nostr-cli/internal/resolve"
)

var userLimitFlag int

func init() {
	rootCmd.PersistentFlags().IntVar(&userLimitFlag, "limit", 10, "Number of notes to show for user lookup")
}

// runUserLookup handles `nostr [user]` — the catch-all for unrecognized first args.
func runUserLookup(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("no user specified")
	}

	cyan := color.New(color.FgCyan).SprintFunc()
	dim := color.New(color.Faint)
	bold := color.New(color.Bold)

	npub, _ := config.ActiveProfile()

	targetHex, err := resolve.Resolve(npub, args[0])
	if err != nil {
		return fmt.Errorf("cannot resolve %q: %w", args[0], err)
	}

	targetNpub, _ := nip19.EncodePublicKey(targetHex)

	// Get relays
	var relays []string
	if npub != "" {
		relays, _ = config.LoadRelays(npub)
	}

	if len(relays) == 0 {
		return fmt.Errorf("no relays configured")
	}

	ctx := context.Background()

	// Fetch profile (kind 0)
	profileFilter := nostr.Filter{
		Authors: []string{targetHex},
		Kinds:   []int{nostr.KindProfileMetadata},
		Limit:   1,
	}

	profileEvent, _ := internalRelay.FetchEvent(ctx, profileFilter, relays)
	if profileEvent != nil && npub != "" {
		_ = cache.LogEvent(npub, *profileEvent)
	}

	var meta profile.Metadata
	if profileEvent != nil {
		_ = json.Unmarshal([]byte(profileEvent.Content), &meta)
	}

	// Display profile
	fmt.Println()
	if meta.DisplayName != "" {
		bold.Println(meta.DisplayName)
	} else if meta.Name != "" {
		bold.Println(meta.Name)
	}
	fmt.Printf("%s %s\n", cyan("npub:"), targetNpub)
	if meta.Name != "" {
		fmt.Printf("%s %s\n", cyan("Name:"), meta.Name)
	}
	if meta.NIP05 != "" {
		fmt.Printf("%s %s\n", cyan("NIP-05:"), meta.NIP05)
	}
	if meta.About != "" {
		fmt.Printf("%s %s\n", cyan("About:"), meta.About)
	}
	if meta.Website != "" {
		fmt.Printf("%s %s\n", cyan("Web:"), meta.Website)
	}
	if meta.LUD16 != "" {
		fmt.Printf("%s %s\n", cyan("Lightning:"), meta.LUD16)
	}

	// Fetch notes (kind 1)
	limit := userLimitFlag
	if limit <= 0 {
		limit = 10
	}

	notesFilter := nostr.Filter{
		Authors: []string{targetHex},
		Kinds:   []int{nostr.KindTextNote},
		Limit:   limit,
	}

	events, err := internalRelay.FetchEvents(ctx, notesFilter, relays)
	if err != nil {
		return nil // Silently skip if notes fail
	}

	if npub != "" {
		cache.LogEvents(npub, events)
	}

	if len(events) > 0 {
		// Sort by time descending
		sort.Slice(events, func(i, j int) bool {
			return events[i].CreatedAt > events[j].CreatedAt
		})

		// Trim to limit
		if len(events) > limit {
			events = events[:limit]
		}

		fmt.Println()
		dim.Printf("─── Last %d notes ───\n", len(events))
		fmt.Println()

		for _, ev := range events {
			ts := time.Unix(int64(ev.CreatedAt), 0).Format("2006-01-02 15:04")
			dim.Printf("  %s\n", ts)
			fmt.Printf("  %s\n\n", ev.Content)
		}
	}

	return nil
}
