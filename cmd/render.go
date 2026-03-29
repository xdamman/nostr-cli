package cmd

import (
	"context"
	"regexp"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip19"
	"github.com/xdamman/nostr-cli/internal/cache"
	"github.com/xdamman/nostr-cli/internal/config"
	"github.com/xdamman/nostr-cli/internal/crypto"
	internalRelay "github.com/xdamman/nostr-cli/internal/relay"
)

var (
	npubRe   = regexp.MustCompile(`nostr:npub1[a-z0-9]{58}`)
	noteRe   = regexp.MustCompile(`nostr:(note1[a-z0-9]{58})`)
	neventRe = regexp.MustCompile(`nostr:(nevent1[a-z0-9]+)`)
)

// renderMentions replaces nostr:npub1... with @name in colored text,
// and nostr:note1.../nostr:nevent1... with shortened dimmed references.
// Only use for human-readable terminal output — not JSON/JSONL.
func renderMentions(content string) string {
	mentionColor := color.New(color.FgMagenta, color.Bold)
	mentionFallback := color.New(color.FgMagenta)
	dimStyle := color.New(color.Faint)

	// Replace npub mentions
	content = npubRe.ReplaceAllStringFunc(content, func(match string) string {
		npub := strings.TrimPrefix(match, "nostr:")

		// Try alias first
		if aliases, err := config.LoadGlobalAliases(); err == nil {
			for name, aliasNpub := range aliases {
				if aliasNpub == npub {
					return mentionColor.Sprintf("@%s", name)
				}
			}
		}

		// Try cached profile name
		if hex, err := crypto.NpubToHex(npub); err == nil {
			if name := cache.ResolveNameByHex(hex); name != "" {
				return mentionColor.Sprintf("@%s", name)
			}
		}

		// Fallback: truncated npub
		short := npub[:12] + "…" + npub[len(npub)-4:]
		return mentionFallback.Sprintf("@%s", short)
	})

	// Replace note references with quoted content
	content = noteRe.ReplaceAllStringFunc(content, func(match string) string {
		id := strings.TrimPrefix(match, "nostr:")
		return renderNoteRef(id, dimStyle)
	})

	// Replace nevent references with quoted content
	content = neventRe.ReplaceAllStringFunc(content, func(match string) string {
		id := strings.TrimPrefix(match, "nostr:")
		return renderNoteRef(id, dimStyle)
	})

	return content
}

// renderNoteRef renders a note1 or nevent1 reference as a quoted block if the event
// can be found in cache or fetched from relays. Falls back to dimmed short ID.
func renderNoteRef(bech32ID string, dimStyle *color.Color) string {
	short := bech32ID[:12] + "…" + bech32ID[len(bech32ID)-4:]
	fallback := dimStyle.Sprintf("📝%s", short)

	// Decode the bech32 identifier to get the event ID hex
	eventIDHex, relayHints := decodeNoteRef(bech32ID)
	if eventIDHex == "" {
		return fallback
	}

	// Try to find the event: cache first, then relay fetch
	ev := findEvent(eventIDHex, relayHints)
	if ev == nil {
		return fallback
	}

	return formatQuotedEvent(ev, bech32ID, dimStyle)
}

// decodeNoteRef decodes a note1 or nevent1 bech32 string into an event ID hex
// and optional relay hints.
func decodeNoteRef(bech32ID string) (string, []string) {
	prefix, data, err := nip19.Decode(bech32ID)
	if err != nil {
		return "", nil
	}

	switch prefix {
	case "note":
		if hex, ok := data.(string); ok {
			return hex, nil
		}
	case "nevent":
		if ep, ok := data.(nostr.EventPointer); ok {
			return ep.ID, ep.Relays
		}
	}
	return "", nil
}

// findEvent looks up an event by ID, first in cache then via relay fetch.
func findEvent(eventIDHex string, relayHints []string) *nostr.Event {
	// Check the active user's event cache
	activeNpub, _ := config.ActiveProfile()
	if activeNpub != "" {
		events, _ := cache.QueryEvents(activeNpub, func(ev nostr.Event) bool {
			return ev.ID == eventIDHex
		})
		if len(events) > 0 {
			return &events[0]
		}
	}

	// Build relay list for fetching
	var relays []string
	if len(relayHints) > 0 {
		relays = append(relays, relayHints...)
	}
	if activeNpub != "" {
		userRelays, _ := config.LoadRelays(activeNpub)
		seen := make(map[string]bool)
		for _, r := range relays {
			seen[r] = true
		}
		for _, r := range userRelays {
			if !seen[r] {
				relays = append(relays, r)
			}
		}
	}
	if len(relays) == 0 {
		relays = config.DefaultRelays()
	}

	// Fetch with short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	filter := nostr.Filter{
		IDs:   []string{eventIDHex},
		Limit: 1,
	}
	ev, _ := internalRelay.FetchEvent(ctx, filter, relays)
	if ev != nil && activeNpub != "" {
		_ = cache.LogEvent(activeNpub, *ev)
	}
	return ev
}

// formatQuotedEvent formats a found event as a quoted block for display.
func formatQuotedEvent(ev *nostr.Event, bech32ID string, dimStyle *color.Color) string {
	quoteColor := color.New(color.Faint)
	short := bech32ID[:12] + "…" + bech32ID[len(bech32ID)-4:]

	// Resolve author name
	authorName := cache.ResolveNameByHex(ev.PubKey)
	if authorName == "" {
		npub, err := nip19.EncodePublicKey(ev.PubKey)
		if err == nil {
			authorName = npub[:12] + "…"
		} else {
			authorName = ev.PubKey[:8] + "…"
		}
	}

	// Truncate content to 140 chars, single line
	content := strings.ReplaceAll(ev.Content, "\n", " ")
	content = strings.ReplaceAll(ev.Content, "\r", "")
	content = strings.TrimSpace(content)
	content = strings.ReplaceAll(content, "\n", " ")
	if len(content) > 140 {
		content = content[:137] + "…"
	}

	// Format as quoted block
	border := quoteColor.Sprint("┃")
	line1 := "\n  " + border + " " + dimStyle.Sprintf("@%s: %s", authorName, content)
	line2 := "\n  " + border + " " + dimStyle.Sprintf("📝 %s", short)

	return line1 + line2
}
