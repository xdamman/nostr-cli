package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
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
	internalRelay "github.com/xdamman/nostr-cli/internal/relay"
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
	green := color.New(color.FgGreen)
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
		color.New(color.FgGreen).Printf("%s> ", promptName)
		fmt.Println()
		dim.Printf("  type your message then hit enter to send to %d relays, ctrl+c to cancel", len(relays))
		fmt.Print("\033[1A") // move cursor back up to prompt line
		fmt.Printf("\r")
		color.New(color.FgGreen).Printf("%s> ", promptName)
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

	// --json: output signed event without publishing
	if postJSONOut {
		data, err := json.MarshalIndent(event, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal event: %w", err)
		}
		fmt.Println(string(data))
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

	dim := color.New(color.Faint)

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

	// Build relay host labels and print initial spinner state
	type relayLine struct {
		host string
		url  string
	}
	rl := make([]relayLine, len(relays))
	for i, r := range relays {
		host := r
		if u, uErr := url.Parse(r); uErr == nil && u.Host != "" {
			host = u.Host
		}
		rl[i] = relayLine{host: host, url: r}
		fmt.Printf("  %s %s\n", dim.Sprint(ui.SpinnerFrames[0]), dim.Sprint(host))
	}

	// Publish with per-relay progress
	ctx := context.Background()
	timeout := time.Duration(timeoutFlag) * time.Millisecond
	ch := internalRelay.PublishEventWithProgress(ctx, event, relays, timeout)

	// Track results by URL
	results := make(map[string]internalRelay.RelayResult)
	var successRelays []string

	greenFn := color.New(color.FgGreen).SprintFunc()
	redFn := color.New(color.FgRed).SprintFunc()

	// Render function for relay lines
	renderRelays := func(frame int) {
		fmt.Printf("\033[%dA", len(rl))
		for _, l := range rl {
			fmt.Print("\r\033[K")
			if r, ok := results[l.url]; ok {
				ms := r.Duration.Milliseconds()
				if r.OK {
					fmt.Printf("  %s %s  %s\n", greenFn("✓"), l.host, dim.Sprintf("%dms", ms))
				} else {
					fmt.Printf("  %s %s  %s\n", redFn("✗"), l.host, dim.Sprintf("%dms", ms))
				}
			} else {
				f := ui.SpinnerFrames[frame%len(ui.SpinnerFrames)]
				fmt.Printf("  %s %s\n", dim.Sprint(f), dim.Sprint(l.host))
			}
		}
	}

	// Animate spinners while waiting for results
	ticker := time.NewTicker(80 * time.Millisecond)
	defer ticker.Stop()
	frame := 0
	done := false

	for !done {
		select {
		case res, ok := <-ch:
			if !ok {
				done = true
				break
			}
			results[res.URL] = res
			if res.OK {
				successRelays = append(successRelays, res.URL)
			}
			renderRelays(frame)
		case <-ticker.C:
			// Only animate if there are still pending relays
			if len(results) < len(rl) {
				frame++
				renderRelays(frame)
			}
		}
	}

	// Final render to ensure all results shown
	renderRelays(frame)

	if len(successRelays) == 0 {
		return fmt.Errorf("failed to publish to any relay")
	}

	// Save sent event to profile-level events.jsonl (for backup)
	_ = cache.LogSentEvent(npub, event)
	// Also cache in feed
	_ = cache.LogFeedEvent(npub, event)

	fmt.Println()
	green.Printf("✓ Published to %d/%d relays\n", len(successRelays), len(relays))
	eventsPath := cache.SentEventsPath(npub)
	if eventsPath != "" {
		home, _ := os.UserHomeDir()
		if home != "" {
			eventsPath = strings.Replace(eventsPath, home, "~", 1)
		}
		dim.Printf("  Saved locally in %s\n", eventsPath)
	}
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
