package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"golang.org/x/term"

	"github.com/fatih/color"
	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip19"
	"github.com/spf13/cobra"
	"github.com/xdamman/nostr-cli/internal/cache"
	"github.com/xdamman/nostr-cli/internal/config"
	"github.com/xdamman/nostr-cli/internal/crypto"
	"github.com/xdamman/nostr-cli/internal/ui"
)

var (
	postReply    string
	postTagFlags []string
	postTagsJSON string
	postDryRun   bool

	// NIP-23 long-form content flags
	postFileFlag    string   // -f / --file
	postLongFlag    bool     // --long (opens editor)
	postTitleFlag   string   // --title
	postSummaryFlag string   // --summary
	postImageFlag   string   // --image
	postSlugFlag    string   // --slug (d tag)
	postDraftFlag   bool     // --draft (kind 30024)
	postHashtags    []string // --hashtag (repeatable, t tags)
)

var postCmd = &cobra.Command{
	Use:     "post [message]",
	Short:   "Publish a text note to Nostr",
	GroupID: "social",
	Long: `Publish a kind 1 text note to your configured relays.

The message can come from:
  • Command argument: nostr post "Hello world"
  • Piped stdin:      echo "Hello" | nostr post
  • Interactive:      nostr post (prompts for input)

Flags:
  --reply <event-id>  Reply to a specific event (hex, note1, or nevent)
  --tag key=value     Add extra tags (repeatable). Semicolons for multi-value:
                      --tag custom="a;b;c" → ["custom","a","b","c"]
  --tags '<json>'     Add extra tags as JSON array of arrays
  --dry-run           Sign but don't publish, output JSON

Long-form content (NIP-23):
  -f, --file <path>   Read content from a markdown file
  --long               Open multi-line editor for long-form content
  --title <string>     Article title
  --summary <string>   Article summary
  --image <url>        Header image URL
  --slug <string>      Article identifier for updates (d tag)
  --draft              Publish as draft (kind 30024 instead of 30023)
  --hashtag <string>   Hashtag topics (repeatable, t tags)

  Using --file, --long, --title, or --slug activates long-form mode (kind 30023).
  Files with YAML frontmatter (---) auto-extract title, summary, image, slug, hashtags.
  CLI flags override frontmatter values.
  Reusing the same --slug updates an existing article (addressable/replaceable event).

Output formats:
  (default)  Human-readable relay-by-relay progress
  --json     Pretty-printed JSON with event + relay results
  --jsonl    Compact single-line JSON (for piping)
  --raw      Raw Nostr event JSON (wire format)

Examples:
  nostr post "Hello Nostr!"
  echo "Automated post" | nostr post
  nostr post "Great thread!" --reply note1abc...
  nostr post "Hello" --json | jq .id
  echo "Bot message" | nostr post --jsonl
  nostr post "Tagged post" --tag t=nostr --tag t=bitcoin
  nostr post "Custom" --tags '[["t","nostr"],["r","https://example.com"]]'
  nostr post "Test" --dry-run --json

Long-form content (NIP-23):
  nostr post -f article.md --title "My Article"
  nostr post -f article.md --slug my-article --title "My Article" --summary "Great read"
  nostr post --long --title "Quick Thoughts"
  nostr post -f article.md --draft
  nostr post -f updated.md --slug my-article    # Updates existing article`,
	RunE: runPost,
}

func init() {
	postCmd.Flags().StringVar(&postReply, "reply", "", "Event ID to reply to (hex or note1/nevent)")
	postCmd.Flags().StringArrayVar(&postTagFlags, "tag", nil, "Extra tags in key=value format (repeatable)")
	postCmd.Flags().StringVar(&postTagsJSON, "tags", "", "Extra tags as JSON array of arrays")
	postCmd.Flags().BoolVar(&postDryRun, "dry-run", false, "Sign but don't publish — print the signed event")

	// NIP-23 long-form content flags
	postCmd.Flags().StringVarP(&postFileFlag, "file", "f", "", "Read content from a markdown file")
	postCmd.Flags().BoolVar(&postLongFlag, "long", false, "Open multi-line editor for long-form content")
	postCmd.Flags().StringVar(&postTitleFlag, "title", "", "Article title (NIP-23)")
	postCmd.Flags().StringVar(&postSummaryFlag, "summary", "", "Article summary (NIP-23)")
	postCmd.Flags().StringVar(&postImageFlag, "image", "", "Header image URL (NIP-23)")
	postCmd.Flags().StringVar(&postSlugFlag, "slug", "", "Article identifier for updates (d tag, NIP-23)")
	postCmd.Flags().BoolVar(&postDraftFlag, "draft", false, "Publish as draft (kind 30024)")
	postCmd.Flags().StringArrayVar(&postHashtags, "hashtag", nil, "Hashtag topics (repeatable, NIP-23 t tags)")

	rootCmd.AddCommand(postCmd)
}

func runPost(cmd *cobra.Command, args []string) error {
	cyan := color.New(color.FgCyan).SprintFunc()

	npub, err := loadAccount()
	if err != nil {
		return err
	}

	// Resolve display name for prompt (same as shell)
	pubHex, err := crypto.NpubToHex(npub)
	if err != nil {
		return err
	}
	promptName := resolveProfileName(npub)
	if promptName == "" {
		promptName = pubHex[:8] + "..."
	}

	// Load relays early (needed for hint and publishing)
	relays, err := config.LoadRelays(npub)
	if err != nil {
		return err
	}

	// Detect long-form mode
	isLongForm := postFileFlag != "" || postLongFlag || postTitleFlag != "" || postSlugFlag != ""

	// Get message: from file, editor, args, piped stdin, or interactive prompt
	var message string
	var mentionPTags [][]string
	var frontmatter *articleFrontmatter

	if postFileFlag != "" {
		data, err := os.ReadFile(postFileFlag)
		if err != nil {
			return fmt.Errorf("failed to read file: %w", err)
		}
		content := string(data)
		fm, body := parseFrontmatter(content)
		frontmatter = fm
		message = body
	} else if postLongFlag {
		result := ui.RunLongFormEditor(ui.LongFormEditorConfig{
			Title: postTitleFlag,
		})
		if result.Cancelled {
			return nil
		}
		message = result.Text
	} else if len(args) > 0 {
		message = strings.Join(args, " ")
	} else if !term.IsTerminal(int(os.Stdin.Fd())) {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return fmt.Errorf("failed to read stdin: %w", err)
		}
		message = strings.TrimSpace(string(data))
	} else {
		prompt := promptName + "> "
		hint := fmt.Sprintf("enter to post to %d relays · ctrl+o for newline · ctrl+c to cancel", len(relays))
		cache.LoadProfileCache(npub)
		result := ui.RunEditlineInput(ui.EditlineInputConfig{
			Prompt:     prompt,
			Hint:       hint,
			Candidates: ui.LoadMentionCandidates(npub),
		})
		if result.Cancelled {
			return nil
		}
		message = strings.TrimSpace(result.Text)
		if len(result.Mentions) > 0 {
			message, mentionPTags = ui.ReplaceMentionsForEvent(message, result.Mentions)
		}
	}

	if message == "" {
		return fmt.Errorf("message cannot be empty")
	}

	// Long-form content: build kind 30023/30024 event
	if isLongForm {
		return publishLongForm(npub, pubHex, message, frontmatter, relays, mentionPTags)
	}

	// Load keys
	nsec, err := config.LoadNsec(npub)
	if err != nil {
		return err
	}
	skHex, err := crypto.NsecToHex(nsec)
	if err != nil {
		return err
	}

	// Build event
	event := nostr.Event{
		PubKey:    pubHex,
		CreatedAt: nostr.Now(),
		Kind:      nostr.KindTextNote,
		Content:   message,
		Tags:      nostr.Tags{},
	}

	// Handle reply
	if postReply != "" {
		replyID, err := resolveEventID(postReply)
		if err != nil {
			return fmt.Errorf("invalid reply event ID: %w", err)
		}
		event.Tags = append(event.Tags, nostr.Tag{"e", replyID, "", "reply"})
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
	if err := event.Sign(skHex); err != nil {
		return fmt.Errorf("failed to sign event: %w", err)
	}

	// Dry run: just output the event
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
	postAs := promptName
	if alias != "" {
		postAs = alias
	}

	// Header
	fmt.Printf("Posting as %s to %d relays\n", cyan(postAs), len(relays))
	fmt.Println()
	fmt.Printf("  %s %s\n", cyan(fmt.Sprintf("%-10s", "Signer:")), npub)
	fmt.Printf("  %s %s\n", cyan(fmt.Sprintf("%-10s", "Event ID:")), event.ID)
	fmt.Println()

	// Publish with interactive relay progress
	timeout := time.Duration(timeoutFlag) * time.Millisecond
	_, err = ui.PublishEventToRelays(npub, event, relays, timeout)
	if err != nil {
		return err
	}

	// Also cache in feed
	_ = cache.LogFeedEvent(npub, event)

	return nil
}

// resolveEventID takes a hex, note1, or nevent ID and returns the hex event ID.
func resolveEventID(input string) (string, error) {
	if strings.HasPrefix(input, "note1") || strings.HasPrefix(input, "nevent1") {
		prefix, value, err := nip19.Decode(input)
		if err != nil {
			return "", err
		}
		switch prefix {
		case "note":
			return value.(string), nil
		case "nevent":
			ep := value.(nostr.EventPointer)
			return ep.ID, nil
		default:
			return "", fmt.Errorf("unexpected prefix: %s", prefix)
		}
	}
	// Assume hex
	if len(input) != 64 {
		return "", fmt.Errorf("expected 64 character hex event ID, got %d", len(input))
	}
	return input, nil
}
