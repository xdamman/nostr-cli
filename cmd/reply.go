package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"golang.org/x/term"

	"github.com/fatih/color"
	"github.com/nbd-wtf/go-nostr"
	"github.com/spf13/cobra"
	"github.com/xdamman/nostr-cli/internal/cache"
	"github.com/xdamman/nostr-cli/internal/config"
	"github.com/xdamman/nostr-cli/internal/crypto"
	internalRelay "github.com/xdamman/nostr-cli/internal/relay"
	"github.com/xdamman/nostr-cli/internal/ui"
)

var (
	replyTagFlags  []string
	replyTagsJSON  string
	replyDryRun    bool
)

var replyCmd = &cobra.Command{
	Use:     "reply <eventId> [message]",
	Short:   "Reply to a Nostr event",
	GroupID: "social",
	Long: `Reply to an existing Nostr event (kind 1 text note).

The event ID can be in hex, note1..., or nevent1... format.
The referenced event is fetched from relays to determine the author
and thread structure (NIP-10 compliant reply tags).

The message can come from:
  • Command argument: nostr reply note1abc... "Great post!"
  • Piped stdin:      echo "I agree" | nostr reply note1abc...
  • Interactive:      nostr reply note1abc... (prompts for input)

Flags:
  --tag key=value    Add extra tags (repeatable). Use semicolons for
                     multi-value: --tag custom="a;b;c" → ["custom","a","b","c"]
  --tags '<json>'    Add extra tags as JSON array:
                     --tags '[["t","bitcoin"],["p","<hex>"]]'
  --dry-run          Sign but don't publish, output JSON

Output formats:
  (default)  Human-readable relay-by-relay progress
  --json     Pretty-printed JSON with event + relay results
  --jsonl    Compact single-line JSON (for piping)
  --raw      Raw Nostr event JSON (wire format)

Examples:
  nostr reply note1abc... "Great post!"
  nostr reply abc123hex "I agree" --tag t=nostr
  nostr reply nevent1... "Check this" --tags '[["p","<hex>"]]'
  echo "Nice work" | nostr reply note1abc...
  nostr reply note1abc... "Test" --dry-run --json`,
	Args: cobra.MinimumNArgs(1),
	RunE: runReply,
}

func init() {
	replyCmd.Flags().StringArrayVar(&replyTagFlags, "tag", nil, "Extra tags in key=value format (repeatable)")
	replyCmd.Flags().StringVar(&replyTagsJSON, "tags", "", "Extra tags as JSON array of arrays")
	replyCmd.Flags().BoolVar(&replyDryRun, "dry-run", false, "Sign but don't publish — print the signed event")
	rootCmd.AddCommand(replyCmd)
}

func runReply(cmd *cobra.Command, args []string) error {
	cyan := color.New(color.FgCyan).SprintFunc()

	npub, err := loadProfile()
	if err != nil {
		return err
	}

	pubHex, err := crypto.NpubToHex(npub)
	if err != nil {
		return err
	}

	// Resolve the event ID to reply to
	replyToID, err := resolveEventID(args[0])
	if err != nil {
		return fmt.Errorf("invalid event ID: %w", err)
	}

	// Load relays
	relays, err := config.LoadRelays(npub)
	if err != nil {
		return err
	}

	// Fetch the referenced event from relays
	ctx := context.Background()
	filter := nostr.Filter{
		IDs:   []string{replyToID},
		Limit: 1,
	}
	referencedEvent, err := internalRelay.FetchEvent(ctx, filter, relays)
	if err != nil {
		return fmt.Errorf("failed to fetch referenced event: %w", err)
	}
	if referencedEvent == nil {
		return fmt.Errorf("event %s not found on any relay", replyToID)
	}

	// Get message: from args, piped stdin, or interactive prompt
	var message string
	if len(args) > 1 {
		message = strings.Join(args[1:], " ")
	} else if !term.IsTerminal(int(os.Stdin.Fd())) {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return fmt.Errorf("failed to read stdin: %w", err)
		}
		message = strings.TrimSpace(string(data))
	} else {
		promptName := resolveProfileName(npub)
		if promptName == "" {
			promptName = pubHex[:8] + "..."
		}
		prompt := sprintPromptPrefix(promptName)
		promptLen := len(promptName) + 2
		hint := fmt.Sprintf("enter to reply to %d relays, ctrl+c to cancel", len(relays))
		editor := ui.NewLineEditor(prompt, promptLen, hint)
		if editor != nil {
			line, ok := editor.ReadLine()
			if !ok {
				return nil
			}
			message = strings.TrimSpace(line)
		} else {
			message, _ = ui.ReadLineSimple(prompt)
		}
	}

	if message == "" {
		return fmt.Errorf("message cannot be empty")
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

	// Build NIP-10 compliant reply tags
	tags := buildReplyTags(referencedEvent, relays)

	// Parse extra tags from --tag and --tags flags
	extraTags, err := parseTags(replyTagFlags, replyTagsJSON)
	if err != nil {
		return err
	}
	tags = append(tags, extraTags...)

	// Build event
	event := nostr.Event{
		PubKey:    pubHex,
		CreatedAt: nostr.Now(),
		Kind:      nostr.KindTextNote,
		Content:   message,
		Tags:      tags,
	}

	// Sign
	if err := event.Sign(skHex); err != nil {
		return fmt.Errorf("failed to sign event: %w", err)
	}

	// Dry run: just output the event
	if replyDryRun {
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

	// Header
	fmt.Printf("Replying as %s to %d relays\n", cyan(postAs), len(relays))
	fmt.Println()
	fmt.Printf("  %s %s\n", cyan(fmt.Sprintf("%-12s", "Signer:")), npub)
	fmt.Printf("  %s %s\n", cyan(fmt.Sprintf("%-12s", "Event ID:")), event.ID)
	fmt.Printf("  %s %s\n", cyan(fmt.Sprintf("%-12s", "Reply to:")), replyToID)
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

// buildReplyTags constructs NIP-10 compliant e and p tags for a reply.
func buildReplyTags(referenced *nostr.Event, relays []string) nostr.Tags {
	var tags nostr.Tags

	relayHint := ""
	if len(relays) > 0 {
		relayHint = relays[0]
	}

	// Find existing root tag in the referenced event
	var rootID string
	for _, tag := range referenced.Tags {
		if len(tag) >= 4 && tag[0] == "e" && tag[3] == "root" {
			rootID = tag[1]
			if tag[2] != "" {
				relayHint = tag[2]
			}
			break
		}
	}

	if rootID != "" {
		// Referenced event is itself a reply — preserve thread structure
		// Keep the original root
		tags = append(tags, nostr.Tag{"e", rootID, relayHint, "root"})
		// Our reply points to the referenced event
		tags = append(tags, nostr.Tag{"e", referenced.ID, relayHint, "reply"})
	} else {
		// Referenced event is a top-level post — it becomes the root
		tags = append(tags, nostr.Tag{"e", referenced.ID, relayHint, "root"})
	}

	// Tag the author of the referenced event
	tags = append(tags, nostr.Tag{"p", referenced.PubKey})

	// Also tag any p-tagged users from the referenced event (for thread notifications)
	seen := map[string]bool{referenced.PubKey: true}
	for _, tag := range referenced.Tags {
		if len(tag) >= 2 && tag[0] == "p" && !seen[tag[1]] {
			tags = append(tags, nostr.Tag{"p", tag[1]})
			seen[tag[1]] = true
		}
	}

	return tags
}
