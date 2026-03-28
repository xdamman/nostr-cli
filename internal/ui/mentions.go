package ui

import (
	"sort"
	"strings"

	"github.com/nbd-wtf/go-nostr/nip19"
	"github.com/xdamman/nostr-cli/internal/cache"
	"github.com/xdamman/nostr-cli/internal/config"
	"github.com/xdamman/nostr-cli/internal/crypto"
)

// MentionCandidate represents one autocomplete entry.
type MentionCandidate struct {
	DisplayName string // "fiatjaf", "alice", etc.
	Npub        string // npub1...
	PubHex      string // hex pubkey
	Source      string // "alias", "following", "cache"
}

// LoadMentionCandidates builds the autocomplete list from all available sources.
func LoadMentionCandidates(npub string) []MentionCandidate {
	seen := make(map[string]*MentionCandidate) // keyed by hex pubkey

	// 1. Global aliases (highest priority)
	if aliases, err := config.LoadGlobalAliases(); err == nil {
		for name, target := range aliases {
			hex, err := crypto.NpubToHex(target)
			if err != nil {
				continue
			}
			seen[hex] = &MentionCandidate{
				DisplayName: name,
				Npub:        target,
				PubHex:      hex,
				Source:      "alias",
			}
		}
	}

	// 2. Following list
	if fc := cache.LoadFollowing(npub); fc != nil {
		for _, hex := range fc.Hexes {
			if _, exists := seen[hex]; exists {
				continue
			}
			npubStr, err := nip19.EncodePublicKey(hex)
			if err != nil {
				continue
			}
			name := cache.ResolveNameByHex(hex)
			if name == "" {
				name = npubStr[:20] + "..."
			}
			seen[hex] = &MentionCandidate{
				DisplayName: name,
				Npub:        npubStr,
				PubHex:      hex,
				Source:      "following",
			}
		}
	}

	// 3. Cached profiles
	for hex, prof := range cache.GetAllProfiles() {
		if _, exists := seen[hex]; exists {
			continue
		}
		name := prof.BestName()
		if name == "" {
			continue // skip profiles without names
		}
		npubStr, err := nip19.EncodePublicKey(hex)
		if err != nil {
			continue
		}
		seen[hex] = &MentionCandidate{
			DisplayName: name,
			Npub:        npubStr,
			PubHex:      hex,
			Source:      "cache",
		}
	}

	// Collect and sort: aliases first, then following, then cache
	result := make([]MentionCandidate, 0, len(seen))
	for _, c := range seen {
		result = append(result, *c)
	}
	sort.Slice(result, func(i, j int) bool {
		pi := sourcePriority(result[i].Source)
		pj := sourcePriority(result[j].Source)
		if pi != pj {
			return pi < pj
		}
		return strings.ToLower(result[i].DisplayName) < strings.ToLower(result[j].DisplayName)
	})

	return result
}

func sourcePriority(s string) int {
	switch s {
	case "alias":
		return 0
	case "following":
		return 1
	case "cache":
		return 2
	default:
		return 3
	}
}

// FilterCandidates returns candidates matching the query (case-insensitive prefix match).
// Returns up to 10 matches.
func FilterCandidates(candidates []MentionCandidate, query string) []MentionCandidate {
	if query == "" {
		if len(candidates) > 10 {
			return candidates[:10]
		}
		return candidates
	}

	q := strings.ToLower(query)
	var results []MentionCandidate
	for _, c := range candidates {
		if strings.HasPrefix(strings.ToLower(c.DisplayName), q) ||
			strings.HasPrefix(c.Npub, q) {
			results = append(results, c)
			if len(results) >= 10 {
				break
			}
		}
	}
	return results
}

// TruncateNpub returns a shortened npub for display: npub1abc...wxyz
func TruncateNpub(npub string) string {
	if len(npub) <= 16 {
		return npub
	}
	return npub[:10] + "..." + npub[len(npub)-4:]
}

// ReplaceMentionsForEvent takes terminal-display text with @name mentions and
// a list of selected mentions, and returns the event content with nostr:npub1...
// format and the p-tags to add.
func ReplaceMentionsForEvent(text string, mentions []MentionCandidate) (string, [][]string) {
	var tags [][]string
	for _, m := range mentions {
		// Replace @displayname with nostr:npub1...
		text = strings.ReplaceAll(text, "@"+m.DisplayName, "nostr:"+m.Npub)
		tags = append(tags, []string{"p", m.PubHex})
	}
	return text, tags
}
