package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip19"
	"github.com/spf13/cobra"
	"github.com/xdamman/nostr-cli/internal/config"
	"github.com/xdamman/nostr-cli/internal/crypto"
	internalRelay "github.com/xdamman/nostr-cli/internal/relay"
)

var postReply string

var postCmd = &cobra.Command{
	Use:   "post [message]",
	Short: "Publish a text note to Nostr",
	Long:  "Publish a kind 1 text note. Pass the message as an argument or enter it interactively.",
	RunE:  runPost,
}

func init() {
	postCmd.Flags().StringVar(&postReply, "reply", "", "Event ID to reply to (hex or note1/nevent)")
	rootCmd.AddCommand(postCmd)
}

func runPost(cmd *cobra.Command, args []string) error {
	npub, err := config.LoadResolvedProfile(profileFlag)
	if err != nil {
		return err
	}

	// Get message
	var message string
	if len(args) > 0 {
		message = strings.Join(args, " ")
	} else {
		fmt.Print("Enter your note: ")
		reader := bufio.NewReader(os.Stdin)
		message, _ = reader.ReadString('\n')
		message = strings.TrimSpace(message)
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
	pubHex, err := crypto.NpubToHex(npub)
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

	// Publish
	relays, err := config.LoadRelays(npub)
	if err != nil {
		return err
	}

	fmt.Println("Publishing...")
	ctx := context.Background()
	if err := internalRelay.PublishEvent(ctx, event, relays); err != nil {
		return err
	}

	// Encode nevent
	nevent, _ := nip19.EncodeEvent(event.ID, relays, pubHex)

	fmt.Println("✓ Published!")
	fmt.Printf("  Event ID: %s\n", event.ID)
	if nevent != "" {
		fmt.Printf("  nevent:   %s\n", nevent)
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
