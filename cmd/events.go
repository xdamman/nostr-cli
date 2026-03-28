package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip04"
	"github.com/nbd-wtf/go-nostr/nip19"
	"github.com/spf13/cobra"
	"github.com/xdamman/nostr-cli/internal/cache"
	"github.com/xdamman/nostr-cli/internal/config"
	"github.com/xdamman/nostr-cli/internal/crypto"
	internalRelay "github.com/xdamman/nostr-cli/internal/relay"
	"github.com/xdamman/nostr-cli/internal/resolve"
)

var (
	eventsKinds   string
	eventsSince   string
	eventsUntil   string
	eventsAuthor  string
	eventsLimit   int
	eventsDecrypt bool
	eventsWatch   bool
	eventsFilter  []string
	eventsMe      bool
)

var eventsCmd = &cobra.Command{
	Use:     "events",
	Short:   "Query events from relays",
	GroupID: "social",
	Long: `Query events from relays with flexible filters.

The --kinds flag accepts comma-separated event kind numbers (e.g. 1,4,7).
Common kinds: 0 (profile), 1 (text note), 3 (follow list), 4 (encrypted DM),
7 (reaction), 10002 (relay list).

The --since and --until flags accept:
  • Durations: 1h, 24h, 7d, 2w, 30m
  • Unix timestamps: 1700000000
  • ISO dates: 2024-01-01, 2024-01-01T15:00:00Z

Use --decrypt to decrypt kind 4 DM content (requires your private key).
Use --watch to live-stream events (keeps connection open, Ctrl+C to exit).
  In watch mode, --decrypt works in real-time for kind 4 events.
Use --filter key=value (repeatable) to filter by nostr tags (e.g. p=<pubkey>, t=bitcoin).
Use --me as a shortcut for --filter "p=<your_pubkey>" (requires active account).

Output formats:
  (default)  Human-readable one-line-per-event summary
  --json     Pretty-printed enriched JSON (with author npub, timestamp, etc.)
  --jsonl    One compact JSON object per line (ideal for piping/bots)
  --raw      Raw Nostr event JSON (wire format as relays see it)

Examples:
  nostr events --kinds 1 --since 1h                              # Recent text notes
  nostr events --kinds 4 --since 24h --decrypt --jsonl           # Decrypt DMs, output as JSONL
  nostr events --kinds 1,7 --author npub1... --limit 50          # Notes and reactions by author
  nostr events --kinds 0,1,3 --since 2024-01-01 --json           # Multiple kinds since a date
  nostr events --watch --kinds 4 --decrypt --jsonl               # Stream decrypted DMs
  nostr events --watch --kinds 1 --jsonl                         # Stream all notes
  nostr events --watch --kinds 4 --since 1h --decrypt --jsonl    # Start from 1h ago
  nostr events --watch --kinds 4 --me --decrypt --jsonl          # Only DMs to me, decrypted
  nostr events --kinds 1 --filter "t=bitcoin" --jsonl            # Notes tagged bitcoin`,
	RunE: runEvents,
}

func init() {
	eventsCmd.Flags().StringVar(&eventsKinds, "kinds", "", "Event kinds to filter, comma-separated (e.g. 1,4,7)")
	eventsCmd.Flags().StringVar(&eventsSince, "since", "", "Start time: duration (1h, 7d), unix timestamp, or ISO date (2024-01-01)")
	eventsCmd.Flags().StringVar(&eventsUntil, "until", "", "End time: duration (1h, 7d), unix timestamp, or ISO date (2024-01-01)")
	eventsCmd.Flags().StringVar(&eventsAuthor, "author", "", "Filter by author (npub, alias, or NIP-05 address)")
	eventsCmd.Flags().IntVar(&eventsLimit, "limit", 50, "Maximum number of events to return")
	eventsCmd.Flags().BoolVar(&eventsDecrypt, "decrypt", false, "Decrypt kind 4 DM content (requires private key)")
	eventsCmd.Flags().BoolVar(&eventsWatch, "watch", false, "Live-stream events (keeps connection open, Ctrl+C to exit)")
	eventsCmd.Flags().StringArrayVar(&eventsFilter, "filter", nil, "Tag filter in key=value format, e.g. p=<pubkey> (repeatable)")
	eventsCmd.Flags().BoolVar(&eventsMe, "me", false, "Shortcut for --filter \"p=<your_pubkey>\"")
	_ = eventsCmd.MarkFlagRequired("kinds")
	rootCmd.AddCommand(eventsCmd)
}

// parseTimeArg parses a time argument that can be:
// - a duration: "1h", "24h", "7d", "30m"
// - a unix timestamp: "1700000000"
// - an ISO date: "2024-01-01", "2024-01-01T15:00:00Z"
// Returns a nostr.Timestamp. For durations, returns now-duration.
func parseTimeArg(s string) (nostr.Timestamp, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty time argument")
	}

	// Try duration format: number + suffix
	if len(s) >= 2 {
		suffix := s[len(s)-1]
		numStr := s[:len(s)-1]
		if n, err := strconv.ParseInt(numStr, 10, 64); err == nil {
			var dur time.Duration
			switch suffix {
			case 'm':
				dur = time.Duration(n) * time.Minute
			case 'h':
				dur = time.Duration(n) * time.Hour
			case 'd':
				dur = time.Duration(n) * 24 * time.Hour
			case 'w':
				dur = time.Duration(n) * 7 * 24 * time.Hour
			}
			if dur > 0 {
				return nostr.Timestamp(time.Now().Add(-dur).Unix()), nil
			}
		}
	}

	// Try unix timestamp (all digits)
	if ts, err := strconv.ParseInt(s, 10, 64); err == nil && ts > 1000000000 {
		return nostr.Timestamp(ts), nil
	}

	// Try ISO date formats
	for _, layout := range []string{
		time.RFC3339,
		"2006-01-02T15:04:05",
		"2006-01-02",
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return nostr.Timestamp(t.Unix()), nil
		}
	}

	return 0, fmt.Errorf("cannot parse time %q (use duration like 1h/7d, unix timestamp, or ISO date)", s)
}

func buildEventsFilter(npub string) (nostr.Filter, []int, error) {
	// Parse kinds
	kindStrs := strings.Split(eventsKinds, ",")
	var kinds []int
	for _, ks := range kindStrs {
		ks = strings.TrimSpace(ks)
		if ks == "" {
			continue
		}
		k, err := strconv.Atoi(ks)
		if err != nil {
			return nostr.Filter{}, nil, fmt.Errorf("invalid kind %q: %w", ks, err)
		}
		kinds = append(kinds, k)
	}
	if len(kinds) == 0 {
		return nostr.Filter{}, nil, fmt.Errorf("--kinds is required")
	}

	filter := nostr.Filter{
		Kinds: kinds,
		Limit: eventsLimit,
	}

	// Parse --since
	if eventsSince != "" {
		ts, err := parseTimeArg(eventsSince)
		if err != nil {
			return nostr.Filter{}, nil, err
		}
		filter.Since = &ts
	}

	// Parse --until
	if eventsUntil != "" {
		ts, err := parseTimeArg(eventsUntil)
		if err != nil {
			return nostr.Filter{}, nil, err
		}
		filter.Until = &ts
	}

	// Parse --author
	if eventsAuthor != "" {
		authorHex, err := resolve.Resolve(npub, eventsAuthor)
		if err != nil {
			return nostr.Filter{}, nil, fmt.Errorf("cannot resolve author %q: %w", eventsAuthor, err)
		}
		filter.Authors = []string{authorHex}
	}

	// Parse --me: add p=<my_pubkey> filter
	if eventsMe {
		myHex, err := crypto.NpubToHex(npub)
		if err != nil {
			return nostr.Filter{}, nil, fmt.Errorf("cannot resolve own pubkey: %w", err)
		}
		eventsFilter = append(eventsFilter, "p="+myHex)
	}

	// Parse --filter key=value into TagMap
	if len(eventsFilter) > 0 {
		tagMap := make(nostr.TagMap)
		for _, f := range eventsFilter {
			parts := strings.SplitN(f, "=", 2)
			if len(parts) != 2 {
				return nostr.Filter{}, nil, fmt.Errorf("invalid --filter format %q (expected key=value)", f)
			}
			key := parts[0]
			tagMap[key] = append(tagMap[key], parts[1])
		}
		filter.Tags = tagMap
	}

	return filter, kinds, nil
}

func runEvents(cmd *cobra.Command, args []string) error {
	npub, err := loadProfile()
	if err != nil {
		return err
	}

	relays, err := config.LoadRelays(npub)
	if err != nil {
		return err
	}

	filter, kinds, err := buildEventsFilter(npub)
	if err != nil {
		return err
	}

	// Check if we need decrypt capabilities
	hasKind4 := false
	for _, k := range kinds {
		if k == 4 {
			hasKind4 = true
			break
		}
	}

	var skHex, myHex string
	if hasKind4 && eventsDecrypt {
		nsec, err := config.LoadNsec(npub)
		if err != nil {
			return fmt.Errorf("--decrypt requires access to private key: %w", err)
		}
		skHex, err = crypto.NsecToHex(nsec)
		if err != nil {
			return err
		}
		myHex, err = crypto.NpubToHex(npub)
		if err != nil {
			return err
		}
	}

	if eventsWatch {
		return watchEvents(npub, filter, relays, skHex, myHex)
	}

	// One-shot query
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutFlag)*time.Millisecond+10*time.Second)
	defer cancel()

	events, err := internalRelay.FetchEvents(ctx, filter, relays)
	if err != nil {
		return fmt.Errorf("fetch failed: %w", err)
	}

	// Sort by created_at ascending
	sort.Slice(events, func(i, j int) bool {
		return events[i].CreatedAt < events[j].CreatedAt
	})

	// Cache events
	cache.LogEvents(npub, events)

	// Output
	for _, ev := range events {
		printEvent(*ev, skHex, myHex)
	}

	// Summary to stderr in human mode
	if !rawFlag && !jsonFlag && !jsonlFlag {
		fmt.Fprintf(os.Stderr, "\n%d events\n", len(events))
	}

	return nil
}

// printEvent outputs a single event, decrypting kind 4 if needed.
func printEvent(ev nostr.Event, skHex, myHex string) {
	content := ev.Content

	// Decrypt kind 4 if requested
	if ev.Kind == 4 && eventsDecrypt && skHex != "" {
		counterparty := ev.PubKey
		if counterparty == myHex {
			// Sent by us — counterparty is in p tag
			for _, tag := range ev.Tags {
				if len(tag) >= 2 && tag[0] == "p" {
					counterparty = tag[1]
					break
				}
			}
		}
		ss := generateSharedSecret(skHex, counterparty)
		if plain, err := nip04.Decrypt(ev.Content, ss); err == nil {
			content = plain
		}
	}

	if rawFlag {
		printRaw(ev)
	} else if jsonFlag || jsonlFlag {
		authorNpub, _ := nip19.EncodePublicKey(ev.PubKey)
		entry := map[string]interface{}{
			"id":         ev.ID,
			"pubkey":     ev.PubKey,
			"author":     authorNpub,
			"kind":       ev.Kind,
			"content":    content,
			"created_at": ev.CreatedAt,
			"timestamp":  time.Unix(int64(ev.CreatedAt), 0).Format(time.RFC3339),
			"tags":       ev.Tags,
		}
		if jsonlFlag {
			printJSONL(entry)
		} else {
			printJSON(entry)
		}
	} else {
		// Human-readable: one line per event
		ts := time.Unix(int64(ev.CreatedAt), 0).Format("2006-01-02T15:04:05")
		authorNpub, _ := nip19.EncodePublicKey(ev.PubKey)
		name := cache.ResolveNameByHex(ev.PubKey)
		if name == "" {
			name = resolveProfileName(authorNpub)
		}
		if name == "" {
			if len(authorNpub) > 16 {
				name = authorNpub[:16] + "…"
			} else {
				name = authorNpub
			}
		}
		oneLine := strings.ReplaceAll(content, "\n", " ")
		if len(oneLine) > 120 {
			oneLine = oneLine[:117] + "..."
		}
		fmt.Printf("%s [kind:%d] %s: %s\n", ts, ev.Kind, name, oneLine)
	}
}

// watchEvents live-streams events from relays using subscriptions.
func watchEvents(npub string, filter nostr.Filter, relays []string, skHex, myHex string) error {
	cache.LoadProfileCache(npub)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var seenMu sync.Mutex
	seen := make(map[string]bool)
	var printMu sync.Mutex

	// If no --since specified for watch, default to now
	if filter.Since == nil {
		since := nostr.Now()
		filter.Since = &since
	}

	// Remove limit for streaming
	filter.Limit = 0

	for _, url := range relays {
		go func(url string) {
			connectCtx, connectCancel := context.WithTimeout(ctx, internalRelay.ConnectTimeout)
			defer connectCancel()

			relay, err := nostr.RelayConnect(connectCtx, url)
			if err != nil {
				fmt.Fprintf(os.Stderr, "relay %s: connection failed: %v\n", url, err)
				return
			}
			defer relay.Close()

			sub, err := relay.Subscribe(ctx, nostr.Filters{filter})
			if err != nil {
				fmt.Fprintf(os.Stderr, "relay %s: subscribe failed: %v\n", url, err)
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

					seenMu.Lock()
					if seen[ev.ID] {
						seenMu.Unlock()
						continue
					}
					seen[ev.ID] = true
					seenMu.Unlock()

					cache.LogEvents(npub, []*nostr.Event{ev})

					printMu.Lock()
					printEvent(*ev, skHex, myHex)
					printMu.Unlock()
				}
			}
		}(url)
	}

	fmt.Fprintf(os.Stderr, "Watching for events (kinds: %s) on %d relays...\n", eventsKinds, len(relays))
	fmt.Fprintf(os.Stderr, "ready\n")

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	cancel()
	return nil
}
