package cmd

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/nbd-wtf/go-nostr"
	"github.com/spf13/cobra"
	"github.com/xdamman/nostr-cli/internal/cache"
	"github.com/xdamman/nostr-cli/internal/config"
	"github.com/xdamman/nostr-cli/internal/crypto"
	"github.com/xdamman/nostr-cli/internal/ui"
)

var (
	eventNewKind     int
	eventNewContent  string
	eventNewTags     []string
	eventNewTagsJSON string
	eventNewPow      int
	eventNewDryRun   bool
)

var eventCmd = &cobra.Command{
	Use:     "event",
	Short:   "Event operations",
	GroupID: "social",
	Long:    "Commands for creating and inspecting raw Nostr events of any kind.",
}

var eventNewCmd = &cobra.Command{
	Use:   "new",
	Short: "Create and publish a new event",
	Long: `Create, sign, and publish a Nostr event of any kind.

Flags:
  --kind      Event kind number (required). Common kinds: 0 (profile), 1 (text note),
              3 (follow list), 5 (deletion), 7 (reaction), 30023 (long-form article).
  --content   Event content string (required). Use '-' to read from stdin.
  --tag       Tags in key=value format (repeatable). E.g. --tag e=<eventid> --tag p=<pubkey>
              Use semicolons for multi-value: --tag custom="a;b;c" → ["custom","a","b","c"]
  --tags      Extra tags as JSON array: --tags '[["t","bitcoin"],["p","<hex>"]]'
  --pow       Proof of work difficulty (leading zero bits in event ID).
  --dry-run   Sign the event but don't publish. Outputs the signed event JSON.

With --dry-run, or when --json/--jsonl is set and --dry-run is used, the signed
event is printed without publishing — useful for inspection or piping to other tools.

Output formats:
  --json      Pretty-printed JSON (event + relay results, or just event with --dry-run)
  --jsonl     Compact single-line JSON (ideal for piping)
  --raw       Raw Nostr event JSON (wire format)

Examples:
  nostr event new --kind 1 --content "Hello world"
  nostr event new --kind 1 --content "Reply" --tag e=abc123
  nostr event new --kind 7 --content "+" --tag e=<eventid> --tag p=<pubkey>
  nostr event new --kind 0 --content '{"name":"bot","about":"I am a bot"}'
  echo "Hello" | nostr event new --kind 1 --content -
  nostr event new --kind 1 --content "Test" --dry-run --json
  nostr event new --kind 1 --content "Mined" --pow 16
  nostr event new --kind 1 --content "Hello" --tag t=nostr --tags '[["r","https://example.com"]]'`,
	RunE: runEventNew,
}

func init() {
	eventNewCmd.Flags().IntVar(&eventNewKind, "kind", -1, "Event kind number (required, e.g. 1 for text note)")
	eventNewCmd.Flags().StringVar(&eventNewContent, "content", "", "Event content (use '-' to read from stdin)")
	eventNewCmd.Flags().StringArrayVar(&eventNewTags, "tag", nil, "Tags in key=value format, repeatable (e.g. --tag e=abc --tag p=def)")
	eventNewCmd.Flags().StringVar(&eventNewTagsJSON, "tags", "", "Extra tags as JSON array of arrays")
	eventNewCmd.Flags().IntVar(&eventNewPow, "pow", 0, "Proof of work difficulty (leading zero bits)")
	eventNewCmd.Flags().BoolVar(&eventNewDryRun, "dry-run", false, "Sign but don't publish — print the signed event")
	_ = eventNewCmd.MarkFlagRequired("kind")
	_ = eventNewCmd.MarkFlagRequired("content")

	eventCmd.AddCommand(eventNewCmd)
	rootCmd.AddCommand(eventCmd)
}

func runEventNew(cmd *cobra.Command, args []string) error {
	if eventNewKind < 0 {
		return fmt.Errorf("--kind is required")
	}

	npub, err := loadAccount()
	if err != nil {
		return err
	}

	nsec, err := config.LoadNsec(npub)
	if err != nil {
		return err
	}
	skHex, err := crypto.NsecToHex(nsec)
	if err != nil {
		return err
	}
	pubHex, err := crypto.NpubToHex(npub)
	if err != nil {
		return err
	}

	// Read content from stdin if "-"
	content := eventNewContent
	if content == "-" {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return fmt.Errorf("failed to read stdin: %w", err)
		}
		content = strings.TrimSpace(string(data))
	}
	if content == "" {
		return fmt.Errorf("content cannot be empty")
	}

	// Parse tags
	tags, err := parseTags(eventNewTags, eventNewTagsJSON)
	if err != nil {
		return err
	}

	event := nostr.Event{
		PubKey:    pubHex,
		CreatedAt: nostr.Now(),
		Kind:      eventNewKind,
		Content:   content,
		Tags:      tags,
	}

	// POW
	if eventNewPow > 0 {
		event = doPOW(event, eventNewPow)
	}

	if err := event.Sign(skHex); err != nil {
		return fmt.Errorf("failed to sign: %w", err)
	}

	// Dry run: just output the event
	if eventNewDryRun {
		if jsonlFlag {
			printJSONL(event)
		} else {
			printJSON(event)
		}
		return nil
	}

	relays, err := config.LoadRelays(npub)
	if err != nil {
		return err
	}

	timeout := time.Duration(timeoutFlag) * time.Millisecond

	if rawFlag || jsonFlag || jsonlFlag {
		result, err := ui.PublishEventSilent(npub, event, relays, timeout)
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

	// Human-readable output
	fmt.Printf("Publishing kind %d event to %d relays\n", eventNewKind, len(relays))
	fmt.Printf("  Event ID: %s\n", event.ID)
	fmt.Println()

	_, err = ui.PublishEventToRelays(npub, event, relays, timeout)
	if err != nil {
		return err
	}

	_ = cache.LogSentEvent(npub, event)
	return nil
}

// doPOW does simple proof-of-work by iterating nonce tags until
// the event ID has the required number of leading zero bits.
func doPOW(event nostr.Event, difficulty int) nostr.Event {
	target := fmt.Sprintf("%d", difficulty)
	baseTags := filterNonceTags(event.Tags)
	for nonce := 0; nonce < 10_000_000; nonce++ {
		event.Tags = append(baseTags[:len(baseTags):len(baseTags)],
			nostr.Tag{"nonce", fmt.Sprintf("%d", nonce), target})
		event.CreatedAt = nostr.Now()
		id := event.GetID()
		if countLeadingZeroBits(id) >= difficulty {
			return event
		}
	}
	return event
}

func filterNonceTags(tags nostr.Tags) nostr.Tags {
	var result nostr.Tags
	for _, t := range tags {
		if len(t) > 0 && t[0] != "nonce" {
			result = append(result, t)
		}
	}
	return result
}

func countLeadingZeroBits(hexStr string) int {
	bits := 0
	for _, c := range hexStr {
		switch {
		case c == '0':
			bits += 4
		case c == '1':
			bits += 3
			return bits
		case c >= '2' && c <= '3':
			bits += 2
			return bits
		case c >= '4' && c <= '7':
			bits += 1
			return bits
		default:
			return bits
		}
	}
	return bits
}
