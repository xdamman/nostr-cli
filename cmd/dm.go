package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"sort"
	"strings"
	"sync"
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
	"github.com/xdamman/nostr-cli/internal/profile"
	internalRelay "github.com/xdamman/nostr-cli/internal/relay"
	"github.com/xdamman/nostr-cli/internal/resolve"
	"github.com/xdamman/nostr-cli/internal/ui"
	"golang.org/x/term"
)

var (
	dmWatchFlag  bool
	dmTagFlags   []string
	dmTagsJSON   string
	dmSinceFlag  string
)

var dmCmd = &cobra.Command{
	Use:     "dm [account] [message]",
	Short:   "Send an encrypted direct message",
	GroupID: "social",
	Long: `Send an NIP-04 encrypted direct message to a user.

An <account> can be an npub, alias, or NIP-05 address (e.g. user@domain.com).

Modes:
  nostr dm <account> <message>   Send a one-shot DM
  echo "msg" | nostr dm <account> Send from stdin
  nostr dm <account>             Interactive chat (TUI with message history)
  nostr dm <account> --watch     Stream messages with this user (no send prompt)
  nostr dm --watch               Stream ALL incoming DMs from anyone
  nostr dm                       Show your aliases (quick reference)

Output formats (for one-shot send and --watch):
  (default)  timestamp:sender:message (human-readable)
  --json     Pretty-printed JSON with sender info and event details
  --jsonl    One JSON object per line (ideal for bots and piping)
  --raw      Raw Nostr event JSON (wire format, still encrypted)

Watch mode stderr output:
  Connection errors and subscription failures are logged to stderr.
  A "ready" line is printed to stderr when all relay goroutines are launched.
  Use --since with --watch to catch up on missed events (e.g. --since 1h).

Flags:
  --tag key=value    Add extra tags (repeatable). Semicolons for multi-value:
                     --tag custom="a;b;c" → ["custom","a","b","c"]
  --tags '<json>'    Add extra tags as JSON array of arrays
  --since <duration> Start watching from this time (e.g. 1h, 24h, 7d) — watch mode only

Examples:
  nostr dm alice "Hey, how's it going?"
  echo "Automated alert" | nostr dm alice
  nostr dm alice --watch --jsonl
  nostr dm --watch --jsonl | jq .message
  nostr dm --watch --since 1h --jsonl
  nostr dm alice "Hello" --json
  nostr dm alice "Hello" --tag subject=greeting`,
	RunE: runDM,
}

func init() {
	dmCmd.Flags().BoolVar(&dmWatchFlag, "watch", false, "Listen for DMs without sending")
	dmCmd.Flags().StringArrayVar(&dmTagFlags, "tag", nil, "Extra tags in key=value format (repeatable)")
	dmCmd.Flags().StringVar(&dmTagsJSON, "tags", "", "Extra tags as JSON array of arrays")
	dmCmd.Flags().StringVar(&dmSinceFlag, "since", "", "Start time for --watch: duration (1h, 7d), unix timestamp, or ISO date")
	rootCmd.AddCommand(dmCmd)
}

func runDM(cmd *cobra.Command, args []string) error {
	npub, err := loadAccount()
	if err != nil {
		return err
	}

	// nostr dm --watch: watch all incoming DMs
	if dmWatchFlag && len(args) == 0 {
		return watchAllDMs(npub)
	}

	if len(args) == 0 {
		return showDMAliases(npub)
	}

	targetHex, err := resolve.Resolve(npub, args[0])
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
	myHex, err := crypto.NpubToHex(npub)
	if err != nil {
		return err
	}

	relays, err := config.LoadRelays(npub)
	if err != nil {
		return err
	}

	// One-shot message (from args or piped stdin)
	if len(args) >= 2 {
		message := strings.Join(args[1:], " ")
		return sendDM(npub, skHex, myHex, targetHex, message, relays)
	}

	// Watch mode — don't read stdin
	if dmWatchFlag {
		return watchDM(npub, skHex, myHex, targetHex, args[0], relays)
	}

	// Piped stdin → send as message
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return fmt.Errorf("failed to read stdin: %w", err)
		}
		message := strings.TrimSpace(string(data))
		if message == "" {
			return fmt.Errorf("empty input from pipe")
		}
		return sendDM(npub, skHex, myHex, targetHex, message, relays)
	}

	// Interactive mode
	return interactiveDM(npub, skHex, myHex, targetHex, args[0], relays)
}

func sendDM(npub, skHex, myHex, targetHex, message string, relays []string) error {
	ciphertext, err := nip04.Encrypt(message, generateSharedSecret(skHex, targetHex))
	if err != nil {
		return fmt.Errorf("encryption failed: %w", err)
	}

	dmTags := nostr.Tags{nostr.Tag{"p", targetHex}}

	// Merge extra tags from --tag and --tags flags
	extraTags, err := parseTags(dmTagFlags, dmTagsJSON)
	if err != nil {
		return err
	}
	dmTags = append(dmTags, extraTags...)

	event := nostr.Event{
		PubKey:    myHex,
		CreatedAt: nostr.Now(),
		Kind:      nostr.KindEncryptedDirectMessage,
		Tags:      dmTags,
		Content:   ciphertext,
	}

	if err := event.Sign(skHex); err != nil {
		return fmt.Errorf("failed to sign: %w", err)
	}

	_ = cache.LogDMEvent(npub, targetHex, event)
	targetNpub, _ := nip19.EncodePublicKey(targetHex)
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

	cyan := color.New(color.FgCyan).SprintFunc()

	// Resolve display names
	senderName := resolveProfileName(npub)
	if senderName == "" {
		senderName = npub[:20] + "..."
	}
	recipientName := cache.ResolveNameByHex(targetHex)
	if recipientName == "" {
		recipientName = targetNpub[:20] + "..."
	}
	// Check aliases for better names
	if aliases, aErr := config.LoadGlobalAliases(); aErr == nil {
		for a, n := range aliases {
			if n == npub {
				senderName = a
			}
			if n == targetNpub {
				recipientName = a
			}
		}
	}

	fmt.Printf("Sending encrypted direct message from %s to %s\n", cyan(senderName), cyan(recipientName))
	fmt.Println()
	fmt.Printf("  %s %s\n", cyan(fmt.Sprintf("%-12s", "Signer:")), npub)
	fmt.Printf("  %s %s\n", cyan(fmt.Sprintf("%-12s", "Recipient:")), targetNpub)
	fmt.Printf("  %s %s\n", cyan(fmt.Sprintf("%-12s", "Event ID:")), event.ID)
	fmt.Println()

	_, err = ui.PublishEventToRelays(npub, event, relays, timeout)
	if err != nil {
		return err
	}

	return nil
}

// sendDMAsync sends a DM in the background and reports status to the channel.
func sendDMAsync(npub, skHex, myHex, targetHex, message string, relays []string, statusCh chan<- string) {
	ciphertext, err := nip04.Encrypt(message, generateSharedSecret(skHex, targetHex))
	if err != nil {
		statusCh <- fmt.Sprintf("✗ send failed: %v", err)
		return
	}

	event := nostr.Event{
		PubKey:    myHex,
		CreatedAt: nostr.Now(),
		Kind:      nostr.KindEncryptedDirectMessage,
		Tags:      nostr.Tags{nostr.Tag{"p", targetHex}},
		Content:   ciphertext,
	}

	if err := event.Sign(skHex); err != nil {
		statusCh <- fmt.Sprintf("✗ sign failed: %v", err)
		return
	}

	ctx := context.Background()
	if _, err := internalRelay.PublishEvent(ctx, event, relays); err != nil {
		statusCh <- fmt.Sprintf("✗ %v", err)
		return
	}

	_ = cache.LogDMEvent(npub, targetHex, event)
	_ = cache.LogSentEvent(npub, event)
	statusCh <- "✓"
}


func interactiveDM(npub, skHex, myHex, targetHex, inputName string, relays []string) error {
	return interactiveDMBubbleTea(npub, skHex, myHex, targetHex, inputName, relays)
}

// watchAllDMs subscribes to all incoming DMs for the active account.
// Output: timestamp:sender:message (or JSONL with --json).
func watchAllDMs(npub string) error {
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

	cache.LoadProfileCache(npub)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var seenMu sync.Mutex
	seen := make(map[string]bool)
	var printMu sync.Mutex

	since := nostr.Now()
	if dmSinceFlag != "" {
		ts, err := parseTimeArg(dmSinceFlag)
		if err != nil {
			return fmt.Errorf("invalid --since: %w", err)
		}
		since = ts
	}

	fmt.Fprintf(os.Stderr, "Watching for DMs to %s on %d relays...\n", npub[:20]+"...", len(relays))

	for _, url := range relays {
		go func(url string) {
			connectCtx, connectCancel := context.WithTimeout(ctx, internalRelay.ConnectTimeout)
			defer connectCancel()

			relay, err := nostr.RelayConnect(connectCtx, url)
			if err != nil {
				fmt.Fprintf(os.Stderr, "relay %s: connection failed: %v\n", url, err)
				return
			}
			defer relay.Close()

			// Two separate subscriptions for better relay compatibility
			// Some relays don't handle multi-filter subscriptions correctly
			merged := make(chan *nostr.Event, 20)
			var subsActive sync.WaitGroup

			// Subscription 1: Incoming DMs (p tag = my pubkey)
			inSub, err := relay.Subscribe(ctx, nostr.Filters{{
				Kinds: []int{nostr.KindEncryptedDirectMessage},
				Tags:  nostr.TagMap{"p": []string{myHex}},
				Since: &since,
			}})
			if err != nil {
				fmt.Fprintf(os.Stderr, "relay %s: incoming subscribe failed: %v\n", url, err)
			} else {
				subsActive.Add(1)
				go func() {
					defer subsActive.Done()
					defer inSub.Unsub()
					for ev := range inSub.Events {
						merged <- ev
					}
				}()
			}

			// Subscription 2: Outgoing DMs (author = me)
			outSub, err := relay.Subscribe(ctx, nostr.Filters{{
				Kinds:   []int{nostr.KindEncryptedDirectMessage},
				Authors: []string{myHex},
				Since:   &since,
			}})
			if err != nil {
				fmt.Fprintf(os.Stderr, "relay %s: outgoing subscribe failed: %v\n", url, err)
			} else {
				subsActive.Add(1)
				go func() {
					defer subsActive.Done()
					defer outSub.Unsub()
					for ev := range outSub.Events {
						merged <- ev
					}
				}()
			}

			// Close merged channel when both subs are done
			go func() {
				subsActive.Wait()
				close(merged)
			}()

			for {
				select {
				case <-ctx.Done():
					return
				case ev, ok := <-merged:
					if !ok {
						return
					}

					seenMu.Lock()
					if seen[ev.ID] {
						seenMu.Unlock()
						continue
					}
					seen[ev.ID] = true
					seenMu.Unlock()

					// Determine counterparty for decryption and logging
					counterparty := ev.PubKey
					if counterparty == myHex {
						// Sent by us — counterparty is in p tag
						for _, tag := range ev.Tags {
							if len(tag) >= 2 && tag[0] == "p" {
								counterparty = tag[1]
								break
							}
						}
					}

					_ = cache.LogDMEvent(npub, counterparty, *ev)

					// Decrypt
					sharedSecret := generateSharedSecret(skHex, counterparty)
					plaintext, err := nip04.Decrypt(ev.Content, sharedSecret)
					if err != nil {
						continue
					}

					// Resolve sender name
					senderNpub, _ := nip19.EncodePublicKey(ev.PubKey)
					senderName := cache.ResolveNameByHex(ev.PubKey)
					if senderName == "" {
						senderName = resolveProfileName(senderNpub)
					}
					if senderName == "" {
						if len(senderNpub) > 20 {
							senderName = senderNpub[:20] + "..."
						} else {
							senderName = senderNpub
						}
					}

					ts := time.Unix(int64(ev.CreatedAt), 0)

					printMu.Lock()
					if rawFlag {
						printRaw(ev)
					} else if jsonFlag || jsonlFlag {
						entry := map[string]interface{}{
							"timestamp": ts.Format(time.RFC3339),
							"from":      senderName,
							"from_npub": senderNpub,
							"message":   plaintext,
							"event_id":  ev.ID,
							"pubkey":    ev.PubKey,
						}
						if jsonlFlag {
							printJSONL(entry)
						} else {
							printJSON(entry)
						}
					} else {
						fmt.Printf("%s:%s:%s\n", ts.Format("2006-01-02T15:04:05"), senderName, plaintext)
						os.Stdout.Sync()
					}
					printMu.Unlock()
				}
			}
		}(url)
	}

	fmt.Fprintf(os.Stderr, "ready\n")

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	cancel()
	return nil
}

// watchDM subscribes to DMs with the target user and streams them.
// Output is pipe-friendly: timestamp:name:message (or JSONL with --json).
func watchDM(npub, skHex, myHex, targetHex, inputName string, relays []string) error {
	targetName := inputName
	if targetNpub, err := nip19.EncodePublicKey(targetHex); err == nil {
		if meta, _ := profile.LoadCached(targetNpub); meta != nil {
			if meta.Name != "" {
				targetName = meta.Name
			} else if meta.DisplayName != "" {
				targetName = meta.DisplayName
			}
		}
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var seenMu sync.Mutex
	seen := make(map[string]bool)

	var printMu sync.Mutex

	onConnected := func() {}

	onMessage := func(ev *nostr.Event, plaintext string) {
		ts := time.Unix(int64(ev.CreatedAt), 0)
		senderName := targetName
		if ev.PubKey == myHex {
			senderName = "you"
		}
		senderNpub, _ := nip19.EncodePublicKey(ev.PubKey)

		printMu.Lock()
		defer printMu.Unlock()

		if rawFlag {
			printRaw(ev)
		} else if jsonFlag || jsonlFlag {
			entry := map[string]interface{}{
				"timestamp": ts.Format(time.RFC3339),
				"from":      senderName,
				"from_npub": senderNpub,
				"message":   plaintext,
				"event_id":  ev.ID,
				"pubkey":    ev.PubKey,
			}
			if jsonlFlag {
				printJSONL(entry)
			} else {
				printJSON(entry)
			}
		} else {
			fmt.Printf("%s:%s:%s\n", ts.Format("2006-01-02T15:04:05"), senderName, plaintext)
			os.Stdout.Sync()
		}
	}

	since := nostr.Now()
	if dmSinceFlag != "" {
		ts, err := parseTimeArg(dmSinceFlag)
		if err != nil {
			return fmt.Errorf("invalid --since: %w", err)
		}
		since = ts
	}

	fmt.Fprintf(os.Stderr, "Watching for DMs with %s on %d relays...\n", inputName, len(relays))

	for _, url := range relays {
		go subscribeDMRelay(ctx, npub, url, skHex, myHex, targetHex, &seenMu, seen, onConnected, onMessage, since)
	}

	fmt.Fprintf(os.Stderr, "ready\n")

	// Wait for Ctrl+C
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	cancel()
	return nil
}

// drainStatus prints all pending status messages from the channel.
func drainStatus(ch <-chan string) {
	for {
		select {
		case status := <-ch:
			fmt.Println(status)
		default:
			return
		}
	}
}

// dmEventCallback is called when a new DM event is received and decrypted.
type dmEventCallback func(ev *nostr.Event, plaintext string)

func subscribeDMRelay(ctx context.Context, npub, url, skHex, myHex, targetHex string, seenMu *sync.Mutex, seen map[string]bool, onConnected func(), onMessage dmEventCallback, since nostr.Timestamp) {
	connectCtx, cancel := context.WithTimeout(ctx, internalRelay.ConnectTimeout)
	defer cancel()

	relay, err := nostr.RelayConnect(connectCtx, url)
	if err != nil {
		fmt.Fprintf(os.Stderr, "relay %s: connection failed: %v\n", url, err)
		return
	}
	defer relay.Close()

	onConnected()

	// Two separate subscriptions for better relay compatibility
	merged := make(chan *nostr.Event, 20)
	var subsActive sync.WaitGroup

	// Subscription 1: target→me
	inSub, err := relay.Subscribe(ctx, nostr.Filters{{
		Kinds:   []int{nostr.KindEncryptedDirectMessage},
		Authors: []string{targetHex},
		Tags:    nostr.TagMap{"p": []string{myHex}},
		Since:   &since,
	}})
	if err != nil {
		fmt.Fprintf(os.Stderr, "relay %s: incoming subscribe failed: %v\n", url, err)
	} else {
		subsActive.Add(1)
		go func() {
			defer subsActive.Done()
			defer inSub.Unsub()
			for ev := range inSub.Events {
				merged <- ev
			}
		}()
	}

	// Subscription 2: me→target
	outSub, err := relay.Subscribe(ctx, nostr.Filters{{
		Kinds:   []int{nostr.KindEncryptedDirectMessage},
		Authors: []string{myHex},
		Tags:    nostr.TagMap{"p": []string{targetHex}},
		Since:   &since,
	}})
	if err != nil {
		fmt.Fprintf(os.Stderr, "relay %s: outgoing subscribe failed: %v\n", url, err)
	} else {
		subsActive.Add(1)
		go func() {
			defer subsActive.Done()
			defer outSub.Unsub()
			for ev := range outSub.Events {
				merged <- ev
			}
		}()
	}

	// Close merged channel when both subs are done
	go func() {
		subsActive.Wait()
		close(merged)
	}()

	sharedSecret := generateSharedSecret(skHex, targetHex)
	for {
		select {
		case <-ctx.Done():
			return
		case ev, ok := <-merged:
			if !ok {
				return
			}

			seenMu.Lock()
			if seen[ev.ID] {
				seenMu.Unlock()
				continue
			}
			seen[ev.ID] = true
			seenMu.Unlock()

			_ = cache.LogDMEvent(npub, targetHex, *ev)

			plaintext, err := nip04.Decrypt(ev.Content, sharedSecret)
			if err != nil {
				continue
			}

			onMessage(ev, plaintext)
		}
	}
}



// formatLocalTimestamp formats a time using the system locale convention.
// Uses DD/MM format for non-US locales and MM/DD for US. Only includes
// the year if it differs from the current year.
func formatLocalTimestamp(t time.Time) string {
	usLocale := false
	for _, env := range []string{"LC_TIME", "LC_ALL", "LANG"} {
		val := os.Getenv(env)
		if val == "" {
			continue
		}
		if strings.HasPrefix(val, "en_US") || strings.HasPrefix(val, "C") || strings.HasPrefix(val, "POSIX") {
			usLocale = true
		}
		break
	}

	now := time.Now()
	sameYear := t.Year() == now.Year()

	if usLocale {
		if sameYear {
			return t.Format("01/02 15:04:05")
		}
		return t.Format("01/02/2006 15:04:05")
	}
	if sameYear {
		return t.Format("02/01 15:04:05")
	}
	return t.Format("02/01/2006 15:04:05")
}

func sortEventsByTime(events []nostr.Event) {
	sort.Slice(events, func(i, j int) bool {
		return events[i].CreatedAt < events[j].CreatedAt
	})
}

func generateSharedSecret(skHex, targetHex string) []byte {
	ss, _ := nip04.ComputeSharedSecret(targetHex, skHex)
	return ss
}

func showDMAliases(npub string) error {
	cyan := color.New(color.FgCyan).SprintFunc()
	dim := color.New(color.Faint)
	bold := color.New(color.Bold)

	aliases, err := resolve.LoadAliases(npub)
	if err != nil {
		return err
	}

	bold.Println("Usage: nostr dm <account> [message]")
	fmt.Println()
	dim.Println("An <account> can be an alias, npub, or NIP-05 address (user@domain.com).")
	dim.Println("Without a message, enters interactive chat mode.")
	fmt.Println()

	if len(aliases) > 0 {
		fmt.Println("Your aliases:")
		fmt.Println()
		for _, name := range sortedKeys(aliases) {
			fmt.Printf("  %s → %s\n", cyan(name), aliases[name])
		}
		fmt.Println()

		example := sortedKeys(aliases)[0]
		dim.Printf("Example:  nostr dm %s \"Hey, how are you?\"\n", example)
	}

	fmt.Println()
	dim.Println("To add an alias:")
	dim.Println("  nostr alias <name> <npub|nip05>")

	// Show switch hint only if there are multiple accounts
	entries, _ := listSwitchableProfiles()
	if len(entries) > 1 {
		fmt.Println()
		dim.Println("To switch active account:")
		dim.Println("  nostr switch <account>")
	}

	return nil
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
