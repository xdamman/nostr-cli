package cmd

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/nbd-wtf/go-nostr"
	"github.com/xdamman/nostr-cli/internal/cache"
	"github.com/xdamman/nostr-cli/internal/config"
	"github.com/xdamman/nostr-cli/internal/crypto"
	"github.com/xdamman/nostr-cli/internal/ui"
)

// articleFrontmatter holds parsed YAML frontmatter from a markdown file.
type articleFrontmatter struct {
	Title    string
	Summary  string
	Image    string
	Slug     string
	Hashtags []string
	Draft    bool
}

// parseFrontmatter extracts YAML frontmatter from markdown content.
// Returns nil frontmatter if no frontmatter block is found.
func parseFrontmatter(content string) (*articleFrontmatter, string) {
	if !strings.HasPrefix(content, "---\n") && !strings.HasPrefix(content, "---\r\n") {
		return nil, content
	}

	// Find closing ---
	rest := content[4:] // skip opening "---\n"
	var fmBlock string
	var body string
	if strings.HasPrefix(rest, "---\n") || strings.HasPrefix(rest, "---\r\n") {
		// Empty frontmatter block
		fmBlock = ""
		body = strings.TrimLeft(rest[3:], "\r\n")
	} else {
		endIdx := strings.Index(rest, "\n---")
		if endIdx < 0 {
			return nil, content
		}
		fmBlock = rest[:endIdx]
		body = strings.TrimLeft(rest[endIdx+4:], "\r\n")
	}

	fm := &articleFrontmatter{}
	for _, line := range strings.Split(fmBlock, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		colonIdx := strings.Index(line, ":")
		if colonIdx < 0 {
			continue
		}
		key := strings.TrimSpace(line[:colonIdx])
		val := strings.TrimSpace(line[colonIdx+1:])

		switch key {
		case "title":
			fm.Title = unquote(val)
		case "summary":
			fm.Summary = unquote(val)
		case "image":
			fm.Image = unquote(val)
		case "slug":
			fm.Slug = unquote(val)
		case "draft":
			fm.Draft = val == "true"
		case "hashtags":
			fm.Hashtags = parseYAMLArray(val)
		}
	}

	return fm, body
}

// unquote removes surrounding quotes from a YAML value.
func unquote(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

// parseYAMLArray parses a simple YAML inline array like [a, b, c].
func parseYAMLArray(s string) []string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "[") && strings.HasSuffix(s, "]") {
		s = s[1 : len(s)-1]
	}
	parts := strings.Split(s, ",")
	var result []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		p = unquote(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

// slugify converts a title to a URL-friendly slug.
func slugify(title string) string {
	s := strings.ToLower(title)
	s = regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if len(s) > 80 {
		s = s[:80]
	}
	return s
}

// publishLongForm builds and publishes a NIP-23 long-form content event.
func publishLongForm(npub, pubHex, content string, fm *articleFrontmatter, relays []string, mentionPTags [][]string) error {
	cyan := color.New(color.FgCyan).SprintFunc()

	// Resolve values: CLI flags override frontmatter
	title := postTitleFlag
	summary := postSummaryFlag
	image := postImageFlag
	slug := postSlugFlag
	draft := postDraftFlag
	var hashtags []string
	hashtags = append(hashtags, postHashtags...)

	if fm != nil {
		if title == "" {
			title = fm.Title
		}
		if summary == "" {
			summary = fm.Summary
		}
		if image == "" {
			image = fm.Image
		}
		if slug == "" {
			slug = fm.Slug
		}
		if !draft && fm.Draft {
			draft = fm.Draft
		}
		if len(hashtags) == 0 {
			hashtags = fm.Hashtags
		}
	}

	// Generate slug
	if slug == "" && title != "" {
		slug = slugify(title)
	}
	if slug == "" {
		slug = fmt.Sprintf("%d", time.Now().Unix())
	}

	// Build event
	kind := 30023
	if draft {
		kind = 30024
	}

	event := nostr.Event{
		PubKey:    pubHex,
		CreatedAt: nostr.Now(),
		Kind:      kind,
		Content:   content,
		Tags:      nostr.Tags{{"d", slug}},
	}

	if title != "" {
		event.Tags = append(event.Tags, nostr.Tag{"title", title})
	}
	if summary != "" {
		event.Tags = append(event.Tags, nostr.Tag{"summary", summary})
	}
	if image != "" {
		event.Tags = append(event.Tags, nostr.Tag{"image", image})
	}

	event.Tags = append(event.Tags, nostr.Tag{"published_at", fmt.Sprintf("%d", time.Now().Unix())})

	for _, ht := range hashtags {
		event.Tags = append(event.Tags, nostr.Tag{"t", ht})
	}

	// Merge extra tags from --tag and --tags flags
	extraTags, err := parseTags(postTagFlags, postTagsJSON)
	if err != nil {
		return err
	}
	event.Tags = append(event.Tags, extraTags...)

	// Add mention p-tags
	for _, tag := range mentionPTags {
		event.Tags = append(event.Tags, nostr.Tag(tag))
	}

	// Sign
	nsec, err := config.LoadNsec(npub)
	if err != nil {
		return err
	}
	skHex, err := crypto.NsecToHex(nsec)
	if err != nil {
		return err
	}
	if err := event.Sign(skHex); err != nil {
		return fmt.Errorf("failed to sign event: %w", err)
	}

	// Dry run
	if postDryRun {
		if jsonlFlag {
			printJSONL(event)
		} else {
			printJSON(event)
		}
		return nil
	}

	// Machine-readable output modes
	if rawFlag || jsonFlag || jsonlFlag {
		timeout := time.Duration(timeoutFlag) * time.Millisecond
		result, err := ui.PublishEventSilent(npub, event, relays, timeout)
		_ = cache.LogFeedEvent(npub, event)
		if rawFlag {
			printRaw(event)
		} else if jsonlFlag {
			if result != nil {
				printJSONL(result)
			} else {
				printJSONL(event)
			}
		} else {
			if result != nil {
				printJSON(result)
			} else {
				printJSON(event)
			}
		}
		if err != nil && result == nil {
			return err
		}
		return nil
	}

	// Resolve alias for display
	alias := ""
	if aliases, aErr := config.LoadGlobalAliases(); aErr == nil {
		for a, n := range aliases {
			if n == npub {
				alias = a
				break
			}
		}
	}
	postAs := resolveProfileName(npub)
	if postAs == "" {
		postAs = pubHex[:8] + "..."
	}
	if alias != "" {
		postAs = alias
	}

	kindLabel := "article"
	if draft {
		kindLabel = "draft"
	}

	// Header
	fmt.Printf("Publishing %s as %s to %d relays\n", kindLabel, cyan(postAs), len(relays))
	fmt.Println()
	if title != "" {
		fmt.Printf("  %s %s\n", cyan(fmt.Sprintf("%-10s", "Title:")), title)
	}
	fmt.Printf("  %s %s\n", cyan(fmt.Sprintf("%-10s", "Slug:")), slug)
	fmt.Printf("  %s %d\n", cyan(fmt.Sprintf("%-10s", "Kind:")), kind)
	fmt.Printf("  %s %s\n", cyan(fmt.Sprintf("%-10s", "Event ID:")), event.ID)
	fmt.Println()

	// Publish with interactive relay progress
	timeout := time.Duration(timeoutFlag) * time.Millisecond
	_, err = ui.PublishEventToRelays(npub, event, relays, timeout)
	if err != nil {
		return err
	}

	_ = cache.LogFeedEvent(npub, event)

	return nil
}
