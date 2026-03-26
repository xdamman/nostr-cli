package cmd

import (
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
	npub, err := loadProfile()
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

	_ = cache.LogDMEvent(npub, targetHex, event)
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

	_ = cache.LogDMEvent(npub, targetHex, event)
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

// dmFeed manages the displayed DM conversation as a sorted, deduplicated list.
type dmFeed struct {
	mu           sync.Mutex
	events       []nostr.Event   // sorted by CreatedAt ascending
	seenIDs      map[string]bool // dedup by event ID
	npub         string
	myHex        string
	targetHex    string
	targetName   func() string // dynamic target name
	skHex        string
	sharedSecret []byte
	cyan         *color.Color
	dim          *color.Color
	renderedLines int // how many terminal lines the feed occupies
}

func newDMFeed(npub, myHex, targetHex, skHex string, targetName func() string, cyan, dim *color.Color) *dmFeed {
	return &dmFeed{
		seenIDs:      make(map[string]bool),
		npub:         npub,
		myHex:        myHex,
		targetHex:    targetHex,
		targetName:   targetName,
		skHex:        skHex,
		sharedSecret: generateSharedSecret(skHex, targetHex),
		cyan:         cyan,
		dim:          dim,
	}
}

// addEvents adds events to the feed, deduplicates, stores new ones, and returns true if any were new.
func (f *dmFeed) addEvents(events []nostr.Event) bool {
	f.mu.Lock()
	defer f.mu.Unlock()

	added := false
	for _, ev := range events {
		if f.seenIDs[ev.ID] {
			continue
		}
		f.seenIDs[ev.ID] = true
		f.events = append(f.events, ev)
		_ = cache.LogDMEvent(f.npub, f.targetHex, ev)
		added = true
	}
	if added {
		sort.Slice(f.events, func(i, j int) bool {
			return f.events[i].CreatedAt < f.events[j].CreatedAt
		})
	}
	return added
}

// addEventDirect adds a single event (e.g. user's own sent message) without storing to disk.
func (f *dmFeed) addEventDirect(ev nostr.Event) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.seenIDs[ev.ID] {
		return
	}
	f.seenIDs[ev.ID] = true
	f.events = append(f.events, ev)
	sort.Slice(f.events, func(i, j int) bool {
		return f.events[i].CreatedAt < f.events[j].CreatedAt
	})
}

// render clears the previous feed output and reprints all messages.
// Takes the number of extra lines below the feed (prompt + hint) to clear.
func (f *dmFeed) render(extraLines int, dimSent map[string]bool) {
	f.mu.Lock()
	defer f.mu.Unlock()

	// Clear previous feed + extra lines (prompt + hint)
	totalClear := f.renderedLines + extraLines
	if totalClear > 0 {
		// Move to top of feed
		fmt.Printf("\033[%dA", totalClear)
	}
	// Clear all lines
	for i := 0; i < totalClear; i++ {
		fmt.Print("\r\033[K\n")
	}
	// Move back to top
	if totalClear > 0 {
		fmt.Printf("\033[%dA", totalClear)
	}

	tName := f.targetName()
	nameWidth := len("you")
	if len(tName) > nameWidth {
		nameWidth = len(tName)
	}
	prefixLen := 14 + 2 + nameWidth + 2

	youColor := color.New(color.FgGreen)
	youColorDim := color.New(color.FgGreen, color.Faint)

	lineCount := 0

	// Only show last 20 messages
	events := f.events
	if len(events) > 20 {
		events = events[len(events)-20:]
	}

	for _, ev := range events {
		plaintext, err := nip04.Decrypt(ev.Content, f.sharedSecret)
		if err != nil {
			continue
		}
		ts := formatLocalTimestamp(time.Unix(int64(ev.CreatedAt), 0))
		content := wrapNote(plaintext, prefixLen)
		isDim := dimSent != nil && dimSent[ev.ID]

		if isDim {
			f.dim.Printf("%s  ", ts)
			youColorDim.Printf("%-*s: ", nameWidth, "you")
			f.dim.Printf("%s\n", content)
		} else {
			f.dim.Printf("%s  ", ts)
			if ev.PubKey == f.myHex {
				youColor.Printf("%-*s: ", nameWidth, "you")
			} else {
				f.cyan.Printf("%-*s: ", nameWidth, tName)
			}
			fmt.Printf("%s\n", content)
		}
		lineCount += 1 + strings.Count(content, "\n")
	}

	f.renderedLines = lineCount
}

func interactiveDM(npub, skHex, myHex, targetHex, inputName string, relays []string) error {
	cyan := color.New(color.FgCyan)
	dim := color.New(color.Faint)
	promptName := resolveProfileName(npub)
	if promptName == "" {
		promptName = myHex[:8] + "..."
	}

	target := &dmTargetName{name: inputName}
	if targetNpub, err := nip19.EncodePublicKey(targetHex); err == nil {
		if meta, _ := profile.LoadCached(targetNpub); meta != nil {
			if meta.Name != "" {
				target.set(meta.Name)
			} else if meta.DisplayName != "" {
				target.set(meta.DisplayName)
			}
		}
	}

	totalRelays := len(relays)
	feed := newDMFeed(npub, myHex, targetHex, skHex, target.get, cyan, dim)

	// Seed feed from local storage
	storedEvents, _ := cache.QueryDMEvents(npub, targetHex)
	if len(storedEvents) > 0 {
		feed.addEvents(storedEvents)
	}

	// Track which sent messages are unconfirmed (shown dim)
	var dimSentMu sync.Mutex
	dimSent := make(map[string]bool)

	getPromptPrefix := func() string {
		return sprintPromptPrefix(promptName)
	}

	prompt := getPromptPrefix()
	promptLen := len(promptName) + 2
	defaultHint := fmt.Sprintf("enter to send an encrypted message to %s over %d relays, ctrl+c to exit", target.get(), totalRelays)

	// Current editor (may be nil between inputs)
	var currentEditor *ui.LineEditor
	var editorMu sync.Mutex

	// renderFeed re-renders the entire feed + prompt + hint.
	renderFeed := func() {
		dimSentMu.Lock()
		ds := make(map[string]bool, len(dimSent))
		for k, v := range dimSent {
			ds[k] = v
		}
		dimSentMu.Unlock()
		editorMu.Lock()
		extraLines := 2 // prompt + hint
		editorMu.Unlock()
		feed.render(extraLines, ds)
		// Redraw prompt + hint (editor will be recreated in the loop)
		fmt.Print(prompt)
		fmt.Print("\r\n")
		dim.Printf("  %s", defaultHint)
		fmt.Print("\033[1A\r")
		fmt.Print(prompt)
	}

	// Initial render
	fmt.Println() // blank line before feed
	feed.renderedLines = 0
	renderFeed()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

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

	// Fetch recent DM history from relays
	go func() {
		since := nostr.Timestamp(time.Now().Add(-7 * 24 * time.Hour).Unix())
		filter1 := nostr.Filter{
			Kinds:   []int{nostr.KindEncryptedDirectMessage},
			Authors: []string{targetHex},
			Tags:    nostr.TagMap{"p": []string{myHex}},
			Since:   &since,
			Limit:   50,
		}
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

		var all []nostr.Event
		for _, evp := range append(events1, events2...) {
			if evp != nil {
				all = append(all, *evp)
			}
		}
		if feed.addEvents(all) {
			renderFeed()
		}
	}()

	// Subscribe for incoming DMs — use feed.addEvents + renderFeed
	var subSeenMu sync.Mutex
	subSeen := make(map[string]bool)
	// Seed subscription seen set from feed
	feed.mu.Lock()
	for id := range feed.seenIDs {
		subSeen[id] = true
	}
	feed.mu.Unlock()
	for _, url := range relays {
		go subscribeDMRelay(ctx, npub, url, skHex, myHex, targetHex, &subSeenMu, subSeen, func() {}, func(ev *nostr.Event, plaintext string) {
			if feed.addEvents([]nostr.Event{*ev}) {
				renderFeed()
			}
		})
	}

	// Handle Ctrl+C
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println()
		cancel()
		os.Exit(0)
	}()

	for {
		editor := ui.NewLineEditor(prompt, promptLen, defaultHint)
		if editor == nil {
			break
		}
		editorMu.Lock()
		currentEditor = editor
		editorMu.Unlock()

		line, ok := editor.ReadLine()
		editorMu.Lock()
		currentEditor = nil
		editorMu.Unlock()

		if !ok {
			break
		}
		line = strings.TrimSpace(line)
		if line == "" {
			renderFeed()
			continue
		}

		// Encrypt and sign
		ciphertext, encErr := nip04.Encrypt(line, generateSharedSecret(skHex, targetHex))
		if encErr != nil {
			renderFeed()
			continue
		}
		event := nostr.Event{
			PubKey:    myHex,
			CreatedAt: nostr.Now(),
			Kind:      nostr.KindEncryptedDirectMessage,
			Tags:      nostr.Tags{nostr.Tag{"p", targetHex}},
			Content:   ciphertext,
		}
		if signErr := event.Sign(skHex); signErr != nil {
			renderFeed()
			continue
		}

		// Add to feed immediately (dim = unconfirmed)
		dimSentMu.Lock()
		dimSent[event.ID] = true
		dimSentMu.Unlock()
		_ = cache.LogDMEvent(npub, targetHex, event)
		_ = cache.LogSentEvent(npub, event)
		feed.addEventDirect(event)
		renderFeed()

		// Publish with progress — update hint on next editor
		timeout := time.Duration(timeoutFlag) * time.Millisecond
		pubCh := internalRelay.PublishEventWithProgress(context.Background(), event, relays, timeout)

		go func(eventID string) {
			confirmed := 0
			total := len(relays)
			for res := range pubCh {
				if res.OK {
					confirmed++
				}
				if confirmed == 1 {
					dimSentMu.Lock()
					delete(dimSent, eventID)
					dimSentMu.Unlock()
					renderFeed()
				}
				editorMu.Lock()
				if currentEditor != nil {
					currentEditor.SetHint(fmt.Sprintf("Posting... (%d/%d relays)", confirmed, total))
				}
				editorMu.Unlock()
			}
			editorMu.Lock()
			if currentEditor != nil {
				currentEditor.SetHint(defaultHint)
			}
			editorMu.Unlock()
		}(event.ID)
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

					_ = cache.LogDMEvent(npub, ev.PubKey, *ev)

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
