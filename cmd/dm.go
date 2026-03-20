package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/fatih/color"
	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip04"
	"github.com/nbd-wtf/go-nostr/nip19"
	"github.com/spf13/cobra"
	"github.com/xdamman/nostr-cli/internal/cache"
	"github.com/xdamman/nostr-cli/internal/config"
	"github.com/xdamman/nostr-cli/internal/crypto"
	internalRelay "github.com/xdamman/nostr-cli/internal/relay"
	"github.com/xdamman/nostr-cli/internal/resolve"
)

var dmCmd = &cobra.Command{
	Use:   "dm [user] [message]",
	Short: "Send an encrypted direct message",
	Long:  "Send a DM to a user. Without a message, enters interactive chat mode.",
	Args:  cobra.MinimumNArgs(1),
	RunE:  runDM,
}

func init() {
	rootCmd.AddCommand(dmCmd)
}

func runDM(cmd *cobra.Command, args []string) error {
	npub, err := config.LoadResolvedProfile(profileFlag)
	if err != nil {
		return err
	}

	targetHex, err := resolve.Resolve(npub, args[0])
	if err != nil {
		return fmt.Errorf("cannot resolve user: %w", err)
	}

	nsec, err := config.LoadNsec(npub)
	if err != nil {
		return err
	}
	skHex, err := crypto.NsecToHex(nsec)
	if err != nil {
		return err
	}
	myHex, err := crypto.NpubToHex(npub)
	if err != nil {
		return err
	}

	relays, err := config.LoadRelays(npub)
	if err != nil {
		return err
	}

	if len(args) >= 2 {
		// One-shot message
		message := strings.Join(args[1:], " ")
		return sendDM(npub, skHex, myHex, targetHex, message, relays)
	}

	// Interactive mode
	return interactiveDM(npub, skHex, myHex, targetHex, relays)
}

func sendDM(npub, skHex, myHex, targetHex, message string, relays []string) error {
	green := color.New(color.FgGreen)

	ciphertext, err := nip04.Encrypt(message, generateSharedSecret(skHex, targetHex))
	if err != nil {
		return fmt.Errorf("encryption failed: %w", err)
	}

	event := nostr.Event{
		PubKey:    myHex,
		CreatedAt: nostr.Now(),
		Kind:      nostr.KindEncryptedDirectMessage,
		Tags:      nostr.Tags{nostr.Tag{"p", targetHex}},
		Content:   ciphertext,
	}

	if err := event.Sign(skHex); err != nil {
		return fmt.Errorf("failed to sign: %w", err)
	}

	ctx := context.Background()
	if err := internalRelay.PublishEvent(ctx, event, relays); err != nil {
		return err
	}

	_ = cache.LogEvent(npub, event)

	targetNpub, _ := nip19.EncodePublicKey(targetHex)
	green.Printf("✓ DM sent to %s\n", targetNpub)
	return nil
}

func interactiveDM(npub, skHex, myHex, targetHex string, relays []string) error {
	cyan := color.New(color.FgCyan)
	dim := color.New(color.Faint)
	targetNpub, _ := nip19.EncodePublicKey(targetHex)

	fmt.Printf("Chat with %s\n", targetNpub)
	dim.Println("Type messages and press Enter. Ctrl+C to exit.")
	fmt.Println()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Subscribe for incoming DMs in background
	go func() {
		for _, url := range relays {
			go subscribeRelay(ctx, npub, url, skHex, myHex, targetHex, cyan)
		}
	}()

	// Handle Ctrl+C
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println("\nExiting chat.")
		cancel()
		os.Exit(0)
	}()

	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Print("> ")
		line, err := reader.ReadString('\n')
		if err != nil {
			break
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		if err := sendDM(npub, skHex, myHex, targetHex, line, relays); err != nil {
			color.Red("Error: %v", err)
		}
	}
	return nil
}

func subscribeRelay(ctx context.Context, npub, url, skHex, myHex, targetHex string, cyan *color.Color) {
	connectCtx, cancel := context.WithTimeout(ctx, internalRelay.ConnectTimeout)
	defer cancel()

	relay, err := nostr.RelayConnect(connectCtx, url)
	if err != nil {
		return
	}
	defer relay.Close()

	since := nostr.Now()
	filters := nostr.Filters{
		{
			Kinds:   []int{nostr.KindEncryptedDirectMessage},
			Authors: []string{targetHex},
			Tags:    nostr.TagMap{"p": []string{myHex}},
			Since:   &since,
		},
	}

	sub, err := relay.Subscribe(ctx, filters)
	if err != nil {
		return
	}
	defer sub.Unsub()

	sharedSecret := generateSharedSecret(skHex, targetHex)
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-sub.Events:
			if !ok {
				return
			}
			_ = cache.LogEvent(npub, *ev)

			plaintext, err := nip04.Decrypt(ev.Content, sharedSecret)
			if err != nil {
				continue
			}

			ts := time.Unix(int64(ev.CreatedAt), 0).Format("15:04")
			fmt.Printf("\r%s %s: %s\n> ", ts, cyan.Sprint("them"), plaintext)
		}
	}
}

func generateSharedSecret(skHex, targetHex string) []byte {
	ss, _ := nip04.ComputeSharedSecret(targetHex, skHex)
	return ss
}
