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
  nostr post "Test" --dry-run --json`,
	RunE: runPost,
}

func init() {
	postCmd.Flags().StringVar(&postReply, "reply", "", "Event ID to reply to (hex or note1/nevent)")
	postCmd.Flags().StringArrayVar(&postTagFlags, "tag", nil, "Extra tags in key=value format (repeatable)")
	postCmd.Flags().StringVar(&postTagsJSON, "tags", "", "Extra tags as JSON array of arrays")
	postCmd.Flags().BoolVar(&postDryRun, "dry-run", false, "Sign but don't publish — print the signed event")
	rootCmd.AddCommand(postCmd)
}

func runPost(cmd *cobra.Command, args []string) error {
	cyan := color.New(color.FgCyan).SprintFunc()

	npub, err := loadProfile()
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

	// Get message: from args, piped stdin, or interactive prompt
	var message string
	if len(args) > 0 {
		message = strings.Join(args, " ")
	} else if !term.IsTerminal(int(os.Stdin.Fd())) {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return fmt.Errorf("failed to read stdin: %w", err)
		}
		message = strings.TrimSpace(string(data))
	} else {
		prompt := sprintPromptPrefix(promptName)
		promptLen := len(promptName) + 2
		hint := fmt.Sprintf("enter to post a public note to %d relays, ctrl+c to cancel", len(relays))
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
