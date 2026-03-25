package cmd

import (
	"bufio"
	"context"
	"encoding/json"
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
	dmJSONFlag  bool
	dmWatchFlag bool
)

var dmCmd = &cobra.Command{
	Use:     "dm [profile] [message]",
	Short:   "Send an encrypted direct message",
	GroupID: "social",
	Long:  "Send a DM to a profile (npub, alias, or nip05). Without a message, enters interactive chat mode.\nWith --watch, listens for incoming DMs without a send prompt.\nWithout arguments, shows your aliases.",
	RunE:  runDM,
}

func init() {
	dmCmd.Flags().BoolVar(&dmJSONFlag, "json", false, "Output event and relay results as JSON")
	dmCmd.Flags().BoolVar(&dmWatchFlag, "watch", false, "Listen for DMs without sending")
	rootCmd.AddCommand(dmCmd)
}

func runDM(cmd *cobra.Command, args []string) error {
	npub, err := config.LoadResolvedProfile(profileFlag)
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

	// One-shot message (from args or piped stdin)
	if len(args) >= 2 {
		message := strings.Join(args[1:], " ")
		return sendDM(npub, skHex, myHex, targetHex, message, relays)
	}
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

	if dmWatchFlag {
		return watchDM(npub, skHex, myHex, targetHex, args[0], relays)
	}

	// Interactive mode
	return interactiveDM(npub, skHex, myHex, targetHex, args[0], relays)
}

func sendDM(npub, skHex, myHex, targetHex, message string, relays []string) error {
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

	targetNpub, _ := nip19.EncodePublicKey(targetHex)
	timeout := time.Duration(timeoutFlag) * time.Millisecond

	if rawFlag {
		_, _ = ui.PublishEventSilent(npub, event, relays, timeout)
		ui.PrintRawEvent(event)
		return nil
	}

	if dmJSONFlag {
		result, err := ui.PublishEventSilent(npub, event, relays, timeout)
		if err != nil && result == nil {
			return err
		}
		data, _ := json.MarshalIndent(result, "", "  ")
		fmt.Println(string(data))
		if err != nil {
			return err
		}
		return nil
	}

	fmt.Printf("Sending DM to %s...\n", targetNpub)
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

	_ = cache.LogSentEvent(npub, event)
	statusCh <- "✓"
}

// dmTargetName holds the resolved target display name, updated asynchronously.
type dmTargetName struct {
	mu   sync.Mutex
	name string
}

func (t *dmTargetName) get() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.name
}

func (t *dmTargetName) set(name string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.name = name
}

func interactiveDM(npub, skHex, myHex, targetHex, inputName string, relays []string) error {
	cyan := color.New(color.FgCyan)
	dim := color.New(color.Faint)
	// Resolve own name for prompt
	promptName := resolveProfileName(npub)
	if promptName == "" {
		promptName = myHex[:8] + "..."
	}

	// Use alias/input as initial name — show prompt immediately
	target := &dmTargetName{name: inputName}

	// Try to upgrade from cache synchronously (instant, no I/O to relays)
	if targetNpub, err := nip19.EncodePublicKey(targetHex); err == nil {
		if meta, _ := profile.LoadCached(targetNpub); meta != nil {
			if meta.Name != "" {
				target.set(meta.Name)
			} else if meta.DisplayName != "" {
				target.set(meta.DisplayName)
			}
		}
	}

	// Show recent DM history from cache
	showDMHistory(npub, myHex, targetHex, target.get(), cyan, dim)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Deduplication: track seen event IDs (seed with cached history)
	var seenMu sync.Mutex
	seen := make(map[string]bool)
	cachedEvents, _ := cache.QueryEvents(npub, func(ev nostr.Event) bool {
		if ev.Kind != nostr.KindEncryptedDirectMessage {
			return false
		}
		if ev.PubKey == myHex {
			for _, tag := range ev.Tags {
				if len(tag) >= 2 && tag[0] == "p" && tag[1] == targetHex {
					return true
				}
			}
		}
		if ev.PubKey == targetHex {
			for _, tag := range ev.Tags {
				if len(tag) >= 2 && tag[0] == "p" && tag[1] == myHex {
					return true
				}
			}
		}
		return false
	})
	for _, ev := range cachedEvents {
		seen[ev.ID] = true
	}

	totalRelays := len(relays)

	getPromptPrefix := func() string {
		return sprintPromptPrefix(promptName)
	}

	onConnected := func() {}

	// Resolve full name from relays in background
	go func() {
		targetNpub, err := nip19.EncodePublicKey(targetHex)
		if err != nil {
			return
		}
		rctx, rcancel := context.WithTimeout(ctx, 5*time.Second)
		defer rcancel()
		fresh, err := profile.FetchFromRelays(rctx, targetNpub, relays)
		if err != nil || fresh == nil {
			return
		}
		newName := fresh.Name
		if newName == "" {
			newName = fresh.DisplayName
		}
		if newName != "" && newName != target.get() {
			target.set(newName)
		}
	}()


	// Fetch recent DM history from relays (fills the gap between cache and real-time)
	go func() {
		since := nostr.Timestamp(time.Now().Add(-7 * 24 * time.Hour).Unix())
		// Messages they sent to me
		filter1 := nostr.Filter{
			Kinds:   []int{nostr.KindEncryptedDirectMessage},
			Authors: []string{targetHex},
			Tags:    nostr.TagMap{"p": []string{myHex}},
			Since:   &since,
			Limit:   50,
		}
		// Messages I sent to them
		filter2 := nostr.Filter{
			Kinds:   []int{nostr.KindEncryptedDirectMessage},
			Authors: []string{myHex},
			Tags:    nostr.TagMap{"p": []string{targetHex}},
			Since:   &since,
			Limit:   50,
		}
		fetchCtx, fetchCancel := context.WithTimeout(ctx, 10*time.Second)
		defer fetchCancel()

		events1, _ := internalRelay.FetchEvents(fetchCtx, filter1, relays)
		events2, _ := internalRelay.FetchEvents(fetchCtx, filter2, relays)

		var allNew []nostr.Event
		sharedSecret := generateSharedSecret(skHex, targetHex)

		for _, evp := range append(events1, events2...) {
			if evp == nil {
				continue
			}
			seenMu.Lock()
			alreadySeen := seen[evp.ID]
			if !alreadySeen {
				seen[evp.ID] = true
			}
			seenMu.Unlock()
			if alreadySeen {
				continue
			}
			_ = cache.LogEvent(npub, *evp)
			allNew = append(allNew, *evp)
		}

		if len(allNew) == 0 {
			return
		}

		// Sort chronologically
		sortEventsByTime(allNew)

		// Compute column width
		nameWidth := len("you")
		tName := target.get()
		if len(tName) > nameWidth {
			nameWidth = len(tName)
		}

		youColor := color.New(color.FgGreen)

		prefixLen := 14 + 2 + nameWidth + 2
		for _, ev := range allNew {
			plaintext, err := nip04.Decrypt(ev.Content, sharedSecret)
			if err != nil {
				continue
			}
			ts := formatLocalTimestamp(time.Unix(int64(ev.CreatedAt), 0))
			content := wrapNote(plaintext, prefixLen)
			fmt.Print("\r\033[K")
			dim.Printf("%s  ", ts)
			if ev.PubKey == myHex {
				youColor.Printf("%-*s: ", nameWidth, "you")
			} else {
				cyan.Printf("%-*s: ", nameWidth, tName)
			}
			dim.Printf("%s\n", content)
			fmt.Print(getPromptPrefix())
		}
	}()

	// Subscribe for incoming DMs in background
	for _, url := range relays {
		go subscribeRelayWithStatus(ctx, npub, url, skHex, myHex, targetHex, target, cyan, getPromptPrefix, &seenMu, seen, onConnected)
	}

	fmt.Println() // blank line after header

	// Handle Ctrl+C
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println()
		cancel()
		os.Exit(0)
	}()

	// Channel for async send status (buffered to avoid blocking senders)
	statusCh := make(chan string, 16)

	reader := bufio.NewReader(os.Stdin)
	for {
		// Show pending status from previous sends before the prompt
		drainStatus(statusCh)
		fmt.Print(getPromptPrefix())
		fmt.Println()
		dim.Printf("  enter to send an encrypted message to %s over %d relays, ctrl+c to exit", target.get(), totalRelays)
		fmt.Print("\033[1A") // move cursor back up to prompt line
		fmt.Printf("\r")
		fmt.Print(getPromptPrefix())
		line, err := reader.ReadString('\n')
		fmt.Print("\033[K") // clear hint line remnants
		if err != nil {
			break
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Send in background, show prompt immediately
		go sendDMAsync(npub, skHex, myHex, targetHex, line, relays, statusCh)
	}
	return nil
}

// watchAllDMs subscribes to all incoming DMs for the active profile.
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

	for _, url := range relays {
		go func(url string) {
			connectCtx, connectCancel := context.WithTimeout(ctx, internalRelay.ConnectTimeout)
			defer connectCancel()

			relay, err := nostr.RelayConnect(connectCtx, url)
			if err != nil {
				return
			}
			defer relay.Close()

			sub, err := relay.Subscribe(ctx, nostr.Filters{
				{
					Kinds: []int{nostr.KindEncryptedDirectMessage},
					Tags:  nostr.TagMap{"p": []string{myHex}},
					Since: &since,
				},
			})
			if err != nil {
				return
			}
			defer sub.Unsub()

			for {
				select {
				case <-ctx.Done():
					return
				case ev, ok := <-sub.Events:
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

					_ = cache.LogEvent(npub, *ev)

					// Decrypt
					sharedSecret := generateSharedSecret(skHex, ev.PubKey)
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
						data, _ := json.Marshal(ev)
						fmt.Println(string(data))
					} else if dmJSONFlag {
						entry := map[string]interface{}{
							"timestamp": ts.Format(time.RFC3339),
							"from":      senderName,
							"from_npub": senderNpub,
							"message":   plaintext,
							"event_id":  ev.ID,
							"pubkey":    ev.PubKey,
						}
						data, _ := json.Marshal(entry)
						fmt.Println(string(data))
					} else {
						fmt.Printf("%s:%s:%s\n", ts.Format("2006-01-02T15:04:05"), senderName, plaintext)
					}
					printMu.Unlock()
				}
			}
		}(url)
	}

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

		printMu.Lock()
		defer printMu.Unlock()

		if rawFlag {
			data, _ := json.Marshal(ev)
			fmt.Println(string(data))
		} else if dmJSONFlag {
			entry := map[string]interface{}{
				"timestamp": ts.Format(time.RFC3339),
				"from":      senderName,
				"message":   plaintext,
				"event_id":  ev.ID,
				"pubkey":    ev.PubKey,
			}
			data, _ := json.Marshal(entry)
			fmt.Println(string(data))
		} else {
			fmt.Printf("%s:%s:%s\n", ts.Format("2006-01-02T15:04:05"), senderName, plaintext)
		}
	}

	for _, url := range relays {
		go subscribeDMRelay(ctx, npub, url, skHex, myHex, targetHex, &seenMu, seen, onConnected, onMessage)
	}

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

func subscribeDMRelay(ctx context.Context, npub, url, skHex, myHex, targetHex string, seenMu *sync.Mutex, seen map[string]bool, onConnected func(), onMessage dmEventCallback) {
	connectCtx, cancel := context.WithTimeout(ctx, internalRelay.ConnectTimeout)
	defer cancel()

	relay, err := nostr.RelayConnect(connectCtx, url)
	if err != nil {
		return
	}
	defer relay.Close()

	onConnected()

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

			seenMu.Lock()
			if seen[ev.ID] {
				seenMu.Unlock()
				continue
			}
			seen[ev.ID] = true
			seenMu.Unlock()

			_ = cache.LogEvent(npub, *ev)

			plaintext, err := nip04.Decrypt(ev.Content, sharedSecret)
			if err != nil {
				continue
			}

			onMessage(ev, plaintext)
		}
	}
}

// subscribeRelayWithStatus wraps subscribeDMRelay with interactive formatting.
func subscribeRelayWithStatus(ctx context.Context, npub, url, skHex, myHex, targetHex string, target *dmTargetName, cyan *color.Color, getPromptPrefix func() string, seenMu *sync.Mutex, seen map[string]bool, onConnected func()) {
	subscribeDMRelay(ctx, npub, url, skHex, myHex, targetHex, seenMu, seen, onConnected, func(ev *nostr.Event, plaintext string) {
		ts := formatLocalTimestamp(time.Unix(int64(ev.CreatedAt), 0))
		dim := color.New(color.Faint)
		name := target.get()
		nameWidth := len(name)
		if nameWidth < 3 {
			nameWidth = 3
		}
		prefixLen := 14 + 2 + nameWidth + 2
		content := wrapNote(plaintext, prefixLen)
		fmt.Print("\r")
		dim.Printf("%s  ", ts)
		cyan.Printf("%-*s: ", nameWidth, name)
		fmt.Printf("%s\n%s", content, getPromptPrefix())
	})
}

func showDMHistory(npub, myHex, targetHex, targetName string, cyan, dim *color.Color) int {
	events, err := cache.QueryEvents(npub, func(ev nostr.Event) bool {
		if ev.Kind != nostr.KindEncryptedDirectMessage {
			return false
		}
		// Messages I sent to them
		if ev.PubKey == myHex {
			for _, tag := range ev.Tags {
				if len(tag) >= 2 && tag[0] == "p" && tag[1] == targetHex {
					return true
				}
			}
		}
		// Messages they sent to me
		if ev.PubKey == targetHex {
			for _, tag := range ev.Tags {
				if len(tag) >= 2 && tag[0] == "p" && tag[1] == myHex {
					return true
				}
			}
		}
		return false
	})
	if err != nil || len(events) == 0 {
		return 0
	}

	// Sort by timestamp
	sortEventsByTime(events)

	// Take last 10
	if len(events) > 10 {
		events = events[len(events)-10:]
	}

	nsec, err := config.LoadNsec(npub)
	if err != nil {
		return 0
	}
	skHex, err := crypto.NsecToHex(nsec)
	if err != nil {
		return 0
	}
	sharedSecret := generateSharedSecret(skHex, targetHex)

	// Compute column width from the longest name
	nameWidth := len("you")
	if len(targetName) > nameWidth {
		nameWidth = len(targetName)
	}

	youColor := color.New(color.FgGreen)
	themColor := cyan

	fmt.Println()
	lines := 1 // the blank line above
	for _, ev := range events {
		plaintext, err := nip04.Decrypt(ev.Content, sharedSecret)
		if err != nil {
			continue
		}
		ts := formatLocalTimestamp(time.Unix(int64(ev.CreatedAt), 0))
		prefixLen := 14 + 2 + nameWidth + 2
		content := wrapNote(plaintext, prefixLen)
		dim.Printf("%s  ", ts)
		if ev.PubKey == myHex {
			youColor.Printf("%-*s: ", nameWidth, "you")
		} else {
			themColor.Printf("%-*s: ", nameWidth, targetName)
		}
		dim.Printf("%s\n", content)
		lines++
	}
	return lines
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

	bold.Println("Usage: nostr dm <profile> [message]")
	fmt.Println()
	dim.Println("A <profile> can be an alias, npub, or NIP-05 address (user@domain.com).")
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

	// Show switch hint only if there are multiple profiles
	entries, _ := listSwitchableProfiles()
	if len(entries) > 1 {
		fmt.Println()
		dim.Println("To switch active profile:")
		dim.Println("  nostr switch <profile>")
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
