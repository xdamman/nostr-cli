package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"sort"
	"syscall"
	"time"

	"github.com/fatih/color"
	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip19"
	"github.com/xdamman/nostr-cli/internal/cache"
	"github.com/xdamman/nostr-cli/internal/config"
	"github.com/xdamman/nostr-cli/internal/nip05"
	"github.com/xdamman/nostr-cli/internal/profile"
	internalRelay "github.com/xdamman/nostr-cli/internal/relay"
	"github.com/xdamman/nostr-cli/internal/resolve"
)

var (
	userLimitFlag int
	userWatchFlag bool
)

func init() {
	rootCmd.PersistentFlags().IntVar(&userLimitFlag, "limit", 10, "Number of notes to show for user lookup")
	rootCmd.PersistentFlags().BoolVar(&userWatchFlag, "watch", false, "Live-stream new notes (Ctrl+C to exit)")
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

	// Check profile cache first
	var meta profile.Metadata
	cachedMeta, _ := profile.LoadCachedWithTime(targetNpub)
	usedCache := false

	if cachedMeta != nil && profile.IsCacheFresh(targetNpub) {
		meta = *cachedMeta
		usedCache = true
	}

	// Fetch profile (kind 0) from relays
	profileFilter := nostr.Filter{
		Authors: []string{targetHex},
		Kinds:   []int{nostr.KindProfileMetadata},
		Limit:   1,
	}

	profileEvent, _ := internalRelay.FetchEvent(ctx, profileFilter, relays)
	if profileEvent != nil {
		if npub != "" {
			_ = cache.LogEvent(npub, *profileEvent)
		}
		_ = json.Unmarshal([]byte(profileEvent.Content), &meta)
		// Update cache for this target
		_ = profile.SaveCached(targetNpub, &meta)
		usedCache = false
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
		green := color.New(color.FgGreen).SprintFunc()
		red := color.New(color.FgRed).SprintFunc()
		verified := nip05.Verify(meta.NIP05, targetHex)
		if verified {
			fmt.Printf("%s %s %s\n", cyan("NIP-05:"), meta.NIP05, green("✓"))
		} else {
			fmt.Printf("%s %s %s\n", cyan("NIP-05:"), meta.NIP05, red("✗"))
		}
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
	if usedCache {
		dim.Println("  (cached)")
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
		if !userWatchFlag {
			return nil // Silently skip if notes fail
		}
		events = nil
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

	// --watch: keep subscription open for new notes
	if userWatchFlag {
		return watchUserNotes(npub, targetHex, relays, dim)
	}

	return nil
}

func watchUserNotes(npub, targetHex string, relays []string, dim *color.Color) error {
	fmt.Println()
	dim.Println("─── Watching for new notes (Ctrl+C to exit) ───")
	fmt.Println()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle Ctrl+C
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println("\nStopped watching.")
		cancel()
		os.Exit(0)
	}()

	since := nostr.Now()

	for _, url := range relays {
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
					Authors: []string{targetHex},
					Kinds:   []int{nostr.KindTextNote},
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
					if npub != "" {
						_ = cache.LogEvent(npub, *ev)
					}
					ts := time.Unix(int64(ev.CreatedAt), 0).Format("2006-01-02 15:04")
					dim.Printf("  %s\n", ts)
					fmt.Printf("  %s\n\n", ev.Content)
				}
			}
		}(url)
	}

	// Block until context is cancelled
	<-ctx.Done()
	return nil
}
