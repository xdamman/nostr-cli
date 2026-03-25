package cmd

import (
	"encoding/json"
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
	postReply   string
	postJSONOut bool
)

var postCmd = &cobra.Command{
	Use:     "post [message]",
	Short:   "Publish a text note to Nostr",
	GroupID: "social",
	Long:  "Publish a kind 1 text note. Pass the message as an argument or enter it interactively.",
	RunE:  runPost,
}

func init() {
	postCmd.Flags().StringVar(&postReply, "reply", "", "Event ID to reply to (hex or note1/nevent)")
	postCmd.Flags().BoolVar(&postJSONOut, "json", false, "Output signed event as JSON without publishing")
	rootCmd.AddCommand(postCmd)
}

func runPost(cmd *cobra.Command, args []string) error {
	cyan := color.New(color.FgCyan).SprintFunc()

	npub, err := config.LoadResolvedProfile(profileFlag)
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
		dim := color.New(color.Faint)
		formatPrompt(promptName)
		fmt.Println()
		dim.Printf("  enter to post a public note to %d relays, ctrl+c to cancel", len(relays))
		fmt.Print("\033[1A") // move cursor back up to prompt line
		fmt.Printf("\r")
		formatPrompt(promptName)
		var line string
		fmt.Scanln(&line)
		message = strings.TrimSpace(line)
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

	// Sign
	if err := event.Sign(skHex); err != nil {
		return fmt.Errorf("failed to sign event: %w", err)
	}

	// --raw: publish and output raw event JSON (wire format)
	if rawFlag {
		timeout := time.Duration(timeoutFlag) * time.Millisecond
		_, err := ui.PublishEventSilent(npub, event, relays, timeout)
		if err != nil {
			// Still output the event even if publish failed
		}
		_ = cache.LogFeedEvent(npub, event)
		ui.PrintRawEvent(event)
		return nil
	}

	// --json: publish and output event + relay results as JSON
	if postJSONOut {
		timeout := time.Duration(timeoutFlag) * time.Millisecond
		result, err := ui.PublishEventSilent(npub, event, relays, timeout)
		if err != nil && result == nil {
			return err
		}
		_ = cache.LogFeedEvent(npub, event)
		data, _ := json.MarshalIndent(result, "", "  ")
		fmt.Println(string(data))
		if err != nil {
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
	nevent, _ := nip19.EncodeEvent(event.ID, relays, event.PubKey)
	if nevent != "" {
		fmt.Printf("  %s %s\n", cyan(fmt.Sprintf("%-10s", "nevent:")), nevent)
	}
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
