package cmd

import (
	"regexp"
	"strings"

	"github.com/fatih/color"
	"github.com/xdamman/nostr-cli/internal/cache"
	"github.com/xdamman/nostr-cli/internal/config"
	"github.com/xdamman/nostr-cli/internal/crypto"
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

	// Replace note references
	content = noteRe.ReplaceAllStringFunc(content, func(match string) string {
		id := strings.TrimPrefix(match, "nostr:")
		short := id[:12] + "…" + id[len(id)-4:]
		return dimStyle.Sprintf("📝%s", short)
	})

	// Replace nevent references
	content = neventRe.ReplaceAllStringFunc(content, func(match string) string {
		id := strings.TrimPrefix(match, "nostr:")
		short := id[:12] + "…" + id[len(id)-4:]
		return dimStyle.Sprintf("📝%s", short)
	})

	return content
}
