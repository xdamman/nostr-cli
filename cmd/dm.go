package cmd

import (
	"bufio"
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

var dmCmd = &cobra.Command{
	Use:     "dm [profile] [message]",
	Short:   "Send an encrypted direct message",
	GroupID: "social",
	Long:  "Send a DM to a profile (npub, alias, or nip05). Without a message, enters interactive chat mode.\nWithout arguments, shows your aliases.",
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

	dim := color.New(color.Faint)
	name := resolveProfileName(npub)
	short := npub
	if len(short) > 20 {
		short = short[:20] + "..."
	}
	if name != "" {
		dim.Printf("Sending direct message as %s (%s)\n", name, short)
	} else {
		dim.Printf("Sending direct message as %s\n", short)
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

	if len(args) >= 2 {
		// One-shot message
		message := strings.Join(args[1:], " ")
		return sendDM(npub, skHex, myHex, targetHex, message, relays)
	}

	// If stdin is piped, read message from it
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

	// Interactive mode — pass the original input as initial display name
	return interactiveDM(npub, skHex, myHex, targetHex, args[0], relays)
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

	targetNpub, _ := nip19.EncodePublicKey(targetHex)

	sp := ui.NewSpinner("Sending...")
	ctx := context.Background()
	err = internalRelay.PublishEvent(ctx, event, relays)
	sp.Stop()
	if err != nil {
		return err
	}

	_ = cache.LogEvent(npub, event)

	green.Printf("✓ DM sent to %s\n", targetNpub)
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
	if err := internalRelay.PublishEvent(ctx, event, relays); err != nil {
		statusCh <- fmt.Sprintf("✗ %v", err)
		return
	}

	_ = cache.LogEvent(npub, event)
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

	fmt.Printf("Chat with %s  ", cyan.Sprint(target.get()))
	dim.Print("connecting...")
	fmt.Println()

	// Show recent DM history from cache
	historyLines := showDMHistory(npub, myHex, targetHex, target.get(), cyan, dim)

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

	// Track relay connection status
	var connectedCount int32
	var connMu sync.Mutex
	totalRelays := len(relays)

	// Lines below the header: history + blank line + prompt
	// We need to jump back this many lines to rewrite the header
	linesBelow := historyLines + 2 // blank line + "> " prompt line

	onConnected := func() {
		connMu.Lock()
		connectedCount++
		count := connectedCount
		connMu.Unlock()

		// Save cursor, jump to header line, rewrite it, restore cursor
		fmt.Printf("\0337")                    // save cursor
		fmt.Printf("\033[%dA", linesBelow)     // move up to header
		fmt.Printf("\r\033[K")                 // clear line
		fmt.Printf("Chat with %s  ", cyan.Sprint(target.get()))
		if int(count) >= totalRelays {
			color.New(color.FgGreen).Printf("%d/%d relays", count, totalRelays)
		} else {
			dim.Printf("%d/%d relays", count, totalRelays)
		}
		fmt.Printf("\0338") // restore cursor
	}

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
			fmt.Print("> ")
		}
	}()

	// Subscribe for incoming DMs in background
	for _, url := range relays {
		go subscribeRelayWithStatus(ctx, npub, url, skHex, myHex, targetHex, target, cyan, &seenMu, seen, onConnected)
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
		fmt.Print("> ")
		line, err := reader.ReadString('\n')
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

func subscribeRelayWithStatus(ctx context.Context, npub, url, skHex, myHex, targetHex string, target *dmTargetName, cyan *color.Color, seenMu *sync.Mutex, seen map[string]bool, onConnected func()) {
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

			// Deduplicate across relays
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
			fmt.Printf("%s\n> ", content)
		}
	}
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
