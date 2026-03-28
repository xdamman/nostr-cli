package cmd

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
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
)

var eventsCmd = &cobra.Command{
	Use:     "events",
	Short:   "Query events from relays",
	GroupID: "social",
	Long: `Query events from relays with filters.

Examples:
  nostr events --kinds 4 --since 1h --json          # DMs from last hour
  nostr events --kinds 1 --author npub1... --limit 20 --jsonl
  nostr events --kinds 0,1,3 --since 2024-01-01 --jsonl`,
	RunE: runEvents,
}

func init() {
	eventsCmd.Flags().StringVar(&eventsKinds, "kinds", "", "Event kinds to filter (comma-separated, required)")
	eventsCmd.Flags().StringVar(&eventsSince, "since", "", "Since time: duration (1h, 24h, 7d), unix timestamp, or ISO date")
	eventsCmd.Flags().StringVar(&eventsUntil, "until", "", "Until time: duration (1h, 24h, 7d), unix timestamp, or ISO date")
	eventsCmd.Flags().StringVar(&eventsAuthor, "author", "", "Filter by author (npub, alias, or nip05)")
	eventsCmd.Flags().IntVar(&eventsLimit, "limit", 50, "Maximum events to return")
	eventsCmd.Flags().BoolVar(&eventsDecrypt, "decrypt", false, "Decrypt kind 4 DM content")
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

func runEvents(cmd *cobra.Command, args []string) error {
	npub, err := loadProfile()
	if err != nil {
		return err
	}

	relays, err := config.LoadRelays(npub)
	if err != nil {
		return err
	}

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
			return fmt.Errorf("invalid kind %q: %w", ks, err)
		}
		kinds = append(kinds, k)
	}
	if len(kinds) == 0 {
		return fmt.Errorf("--kinds is required")
	}

	filter := nostr.Filter{
		Kinds: kinds,
		Limit: eventsLimit,
	}

	// Parse --since
	if eventsSince != "" {
		ts, err := parseTimeArg(eventsSince)
		if err != nil {
			return err
		}
		filter.Since = &ts
	}

	// Parse --until
	if eventsUntil != "" {
		ts, err := parseTimeArg(eventsUntil)
		if err != nil {
			return err
		}
		filter.Until = &ts
	}

	// Parse --author
	if eventsAuthor != "" {
		authorHex, err := resolve.Resolve(npub, eventsAuthor)
		if err != nil {
			return fmt.Errorf("cannot resolve author %q: %w", eventsAuthor, err)
		}
		filter.Authors = []string{authorHex}
	}

	// For kind 4 without author, scope to our own DMs
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

	// Fetch events
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

	// Summary to stderr in human mode
	if !rawFlag && !jsonFlag && !jsonlFlag {
		fmt.Fprintf(os.Stderr, "\n%d events\n", len(events))
	}

	return nil
}
