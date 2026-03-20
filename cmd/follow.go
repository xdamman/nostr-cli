package cmd

import (
	"context"
	"fmt"

	"github.com/fatih/color"
	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip19"
	"github.com/spf13/cobra"
	"github.com/xdamman/nostr-cli/internal/cache"
	"github.com/xdamman/nostr-cli/internal/config"
	"github.com/xdamman/nostr-cli/internal/crypto"
	internalRelay "github.com/xdamman/nostr-cli/internal/relay"
	"github.com/xdamman/nostr-cli/internal/resolve"
	"github.com/xdamman/nostr-cli/internal/ui"
)

var followCmd = &cobra.Command{
	Use:   "follow [npub|nip05|alias]",
	Short: "Follow a user",
	Args:  cobra.ExactArgs(1),
	RunE:  runFollow,
}

var unfollowCmd = &cobra.Command{
	Use:   "unfollow [npub|nip05|alias]",
	Short: "Unfollow a user",
	Args:  cobra.ExactArgs(1),
	RunE:  runUnfollow,
}

func init() {
	rootCmd.AddCommand(followCmd)
	rootCmd.AddCommand(unfollowCmd)
}

func runFollow(cmd *cobra.Command, args []string) error {
	green := color.New(color.FgGreen)
	yellow := color.New(color.FgYellow)

	npub, err := config.LoadResolvedProfile(profileFlag)
	if err != nil {
		return err
	}

	targetHex, err := resolve.Resolve(npub, args[0])
	if err != nil {
		return fmt.Errorf("cannot resolve user: %w", err)
	}

	myHex, err := crypto.NpubToHex(npub)
	if err != nil {
		return err
	}

	if targetHex == myHex {
		yellow.Println("⚠ Following yourself")
	}

	relays, err := config.LoadRelays(npub)
	if err != nil {
		return err
	}

	targetNpub, _ := nip19.EncodePublicKey(targetHex)

	sp := ui.NewSpinner("Fetching contact list...")
	ctx := context.Background()
	contacts, err := fetchContactList(ctx, myHex, relays)
	sp.Stop()
	if err != nil {
		return err
	}

	// Check if already following
	for _, tag := range contacts.Tags {
		if len(tag) >= 2 && tag[0] == "p" && tag[1] == targetHex {
			yellow.Printf("Already following %s\n", targetNpub)
			return nil
		}
	}

	// Add to contact list
	contacts.Tags = append(contacts.Tags, nostr.Tag{"p", targetHex})
	contacts.CreatedAt = nostr.Now()
	contacts.ID = ""
	contacts.Sig = ""

	nsec, err := config.LoadNsec(npub)
	if err != nil {
		return err
	}
	skHex, err := crypto.NsecToHex(nsec)
	if err != nil {
		return err
	}

	if err := contacts.Sign(skHex); err != nil {
		return fmt.Errorf("failed to sign: %w", err)
	}

	sp = ui.NewSpinner("Publishing...")
	err = internalRelay.PublishEvent(ctx, *contacts, relays)
	sp.Stop()
	if err != nil {
		return err
	}

	_ = cache.LogEvent(npub, *contacts)

	green.Printf("✓ Now following %s\n", targetNpub)
	return nil
}

func runUnfollow(cmd *cobra.Command, args []string) error {
	green := color.New(color.FgGreen)
	yellow := color.New(color.FgYellow)

	npub, err := config.LoadResolvedProfile(profileFlag)
	if err != nil {
		return err
	}

	targetHex, err := resolve.Resolve(npub, args[0])
	if err != nil {
		return fmt.Errorf("cannot resolve user: %w", err)
	}

	myHex, err := crypto.NpubToHex(npub)
	if err != nil {
		return err
	}

	relays, err := config.LoadRelays(npub)
	if err != nil {
		return err
	}

	targetNpub, _ := nip19.EncodePublicKey(targetHex)

	sp := ui.NewSpinner("Fetching contact list...")
	ctx := context.Background()
	contacts, err := fetchContactList(ctx, myHex, relays)
	sp.Stop()
	if err != nil {
		return err
	}

	// Find and remove
	found := false
	var newTags nostr.Tags
	for _, tag := range contacts.Tags {
		if len(tag) >= 2 && tag[0] == "p" && tag[1] == targetHex {
			found = true
			continue
		}
		newTags = append(newTags, tag)
	}

	if !found {
		yellow.Printf("Not following %s\n", targetNpub)
		return nil
	}

	contacts.Tags = newTags
	contacts.CreatedAt = nostr.Now()
	contacts.ID = ""
	contacts.Sig = ""

	nsec, err := config.LoadNsec(npub)
	if err != nil {
		return err
	}
	skHex, err := crypto.NsecToHex(nsec)
	if err != nil {
		return err
	}

	if err := contacts.Sign(skHex); err != nil {
		return fmt.Errorf("failed to sign: %w", err)
	}

	sp = ui.NewSpinner("Publishing...")
	err = internalRelay.PublishEvent(ctx, *contacts, relays)
	sp.Stop()
	if err != nil {
		return err
	}

	_ = cache.LogEvent(npub, *contacts)

	green.Printf("✓ Unfollowed %s\n", targetNpub)
	return nil
}

// fetchContactList fetches the latest kind 3 event for the given pubkey, or returns an empty one.
func fetchContactList(ctx context.Context, pubHex string, relayURLs []string) (*nostr.Event, error) {
	filter := nostr.Filter{
		Authors: []string{pubHex},
		Kinds:   []int{nostr.KindFollowList},
		Limit:   1,
	}

	event, err := internalRelay.FetchEvent(ctx, filter, relayURLs)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch contact list: %w", err)
	}

	if event != nil {
		return event, nil
	}

	// Return empty contact list
	return &nostr.Event{
		PubKey: pubHex,
		Kind:   nostr.KindFollowList,
		Tags:   nostr.Tags{},
	}, nil
}
