package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/fatih/color"
	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip19"
	"github.com/xdamman/nostr-cli/internal/cache"
	"github.com/xdamman/nostr-cli/internal/config"
	"github.com/xdamman/nostr-cli/internal/crypto"
	"github.com/xdamman/nostr-cli/internal/profile"
	internalRelay "github.com/xdamman/nostr-cli/internal/relay"
	"github.com/xdamman/nostr-cli/internal/resolve"
	"github.com/xdamman/nostr-cli/internal/ui"
	"golang.org/x/term"
)

type slashCmd struct {
	name  string
	usage string
	desc  string
}

var slashCommands = []slashCmd{
	{"dm", "/dm <user> <message>", "Send a direct message"},
	{"post", "/post <message>", "Post a note"},
	{"follow", "/follow <user>", "Follow a user"},
	{"following", "/following", "List accounts you follow"},
	{"unfollow", "/unfollow <user>", "Unfollow a user"},
	{"profile", "/profile [user]", "View a profile"},
	{"edit-profile", "/edit-profile", "Edit your profile metadata"},
	{"switch", "/switch [user]", "Switch active profile"},
	{"relays", "/relays", "List relays"},
	{"alias", "/alias <name> <npub>", "Create an alias"},
	{"aliases", "/aliases", "List aliases"},
	{"nip", "/nip <number>", "View a NIP specification"},
	{"version", "/version", "Show version info"},
	{"update", "/update", "Check for updates"},
}

// shellPromptName is the current user's display name for the prompt.
var shellPromptName string
var shellRelayCount int

// shellHintOverride, when non-empty, replaces the default hint text.
// Used by background goroutines (e.g. publish progress) to show status.
var shellHintOverride string
var shellHintMu sync.Mutex

// setShellHint sets the hint override and redraws the hint line.
// Pass "" to clear the override and revert to the default hint.
// Safe to call from any goroutine while readShellLine is running.
func setShellHint(hint string) {
	shellHintMu.Lock()
	shellHintOverride = hint
	shellHintMu.Unlock()

	// Only redraw if stdout is a terminal (ANSI escape codes)
	if !term.IsTerminal(int(os.Stdout.Fd())) {
		return
	}

	dim := color.New(color.Faint)
	var text string
	if hint != "" {
		text = "  " + hint
	} else {
		text = defaultHintText(nil, shellRelayCount)
	}
	// Move to hint line (1 below prompt), clear and rewrite, move back
	fmt.Print("\0337")          // save cursor
	fmt.Print("\033[B\r\033[K") // move down, clear line
	if text != "" {
		dim.Print(text)
	}
	fmt.Print("\0338") // restore cursor
}

// formatPrompt prints "name> " in green and returns the visible length.
func formatPrompt(name string) int {
	color.New(color.FgGreen).Print(name)
	fmt.Print("> ")
	return len(name) + 2
}

// sprintPromptPrefix returns the "name> " prompt string with ANSI colors.
func sprintPromptPrefix(name string) string {
	return color.New(color.FgGreen).Sprint(name) + "> "
}

// feedNameWidth tracks the widest author name seen, for uniform column alignment.
var feedNameWidth int
var feedNameWidthMu sync.Mutex

func updateFeedNameWidth(name string) int {
	feedNameWidthMu.Lock()
	defer feedNameWidthMu.Unlock()
	if len(name) > feedNameWidth {
		feedNameWidth = len(name)
	}
	return feedNameWidth
}

func runShell() error {
	npub, err := loadProfile()
	if err != nil {
		return fmt.Errorf("no active profile. Run 'nostr login' first")
	}

	// Reset feed name width for this session
	feedNameWidthMu.Lock()
	feedNameWidth = 0
	feedNameWidthMu.Unlock()

	cyan := color.New(color.FgCyan)
	dim := color.New(color.Faint)

	myHex, err := crypto.NpubToHex(npub)
	if err != nil {
		return err
	}

	relays, err := config.LoadRelays(npub)
	if err != nil {
		return err
	}
	shellRelayCount = len(relays)

	// Load profile cache for instant name resolution
	cache.LoadProfileCache(npub)

	// Resolve own username for prompt
	shellPromptName = resolveAuthorName(myHex)
	// Try cached profile metadata if resolveAuthorName returned truncated npub
	if meta, _ := profile.LoadCached(npub); meta != nil {
		if meta.Name != "" {
			shellPromptName = meta.Name
		} else if meta.DisplayName != "" {
			shellPromptName = meta.DisplayName
		}
	}

	// Display cached feed immediately (no relay needed)
	cachedEvents, _ := cache.LoadFeed(npub, 20)
	if len(cachedEvents) > 0 {
		printFeedEvents(cachedEvents, dim, cyan)
	}

	// Preload feed seen set so FeedSeenID works without re-reading disk
	cache.LoadFeedSeen(npub)

	// Start background profile fetcher
	startProfileFetcher(npub, relays)

	// Mutex for all terminal output from background goroutines
	var printMu sync.Mutex

	// Shared followed hexes (updated when contact list arrives)
	var followMu sync.Mutex
	var followedHexes []string
	followReady := make(chan struct{})

	// Load secret key for signing events in the main loop
	nsec, err := config.LoadNsec(npub)
	if err != nil {
		return err
	}
	skHex, err := crypto.NsecToHex(nsec)
	if err != nil {
		return err
	}

	shellCtx, shellCancel := context.WithCancel(context.Background())
	defer shellCancel()


	// printFeedEventRaw prints a single event in the feed area (raw terminal mode).
	// If dimmed is true, the entire line is printed in faint style.
	// Caller must hold printMu.
	printFeedEventRaw := func(ev nostr.Event, dimmed bool) {
		ts := formatLocalTimestamp(time.Unix(int64(ev.CreatedAt), 0))
		name := resolveAuthorName(ev.PubKey)
		// Use shellPromptName for our own events if cache missed
		if ev.PubKey == myHex && name != shellPromptName && shellPromptName != "" {
			name = shellPromptName
		}
		nw := updateFeedNameWidth(name)
		prefixLen := 14 + 2 + nw + 2
		content := wrapNoteRaw(ev.Content, prefixLen)
		fmt.Print("\r\033[K")
		if dimmed {
			dim.Printf("%s  %-*s: %s\r\n", ts, nw, name, content)
		} else {
			dim.Printf("%s  ", ts)
			cyan.Printf("%-*s: ", nw, name)
			fmt.Printf("%s\r\n", content)
		}
	}

	// printNewEvent deduplicates, caches, and prints a new feed event.
	// Returns true if the event was new and printed.
	printNewEvent := func(ev nostr.Event) bool {
		if cache.FeedSeenID(npub, ev.ID) {
			return false
		}
		_ = cache.LogFeedEvent(npub, ev)
		queueProfileFetch(ev.PubKey)
		printMu.Lock()
		printFeedEventRaw(ev, false)
		fmt.Print("\r")
		formatPrompt(shellPromptName)
		printMu.Unlock()
		return true
	}

	// Fetch contact list and new feed events in background
	go func() {
		defer close(followReady)

		ctx := context.Background()
		contacts, err := fetchContactList(ctx, myHex, relays)
		if err != nil {
			return
		}

		followMu.Lock()
		for _, tag := range contacts.Tags {
			if len(tag) >= 2 && tag[0] == "p" {
				followedHexes = append(followedHexes, tag[1])
			}
		}
		hexes := make([]string, len(followedHexes))
		copy(hexes, followedHexes)
		followMu.Unlock()

		// Cache the following list
		_ = cache.SaveFollowing(npub, hexes)

		// Queue profile fetches for all followed users
		for _, hex := range hexes {
			queueProfileFetch(hex)
		}

		if len(hexes) == 0 {
			if len(cachedEvents) == 0 {
				printMu.Lock()
				fmt.Print("\r\033[K")
				dim.Print("You're not following anyone yet.\r\n")
				dim.Print("  Use /follow <npub|alias|nip05> to follow someone.\r\n")
				fmt.Print("\r\n")
				fmt.Print("\r")
				formatPrompt(shellPromptName)
				printMu.Unlock()
			}
			return
		}

		// Fetch recent notes from relays
		filter := nostr.Filter{
			Authors: hexes,
			Kinds:   []int{nostr.KindTextNote},
			Limit:   20,
		}
		fetched, err := internalRelay.FetchEvents(ctx, filter, relays)
		if err != nil || len(fetched) == 0 {
			return
		}

		// Collect only genuinely new events
		var newEvents []*nostr.Event
		for _, ev := range fetched {
			if !cache.FeedSeenID(npub, ev.ID) {
				newEvents = append(newEvents, ev)
			}
		}

		// Cache all fetched (LogFeedEvents deduplicates internally)
		cache.LogFeedEvents(npub, fetched)

		if len(newEvents) == 0 {
			return
		}

		sort.Slice(newEvents, func(i, j int) bool {
			return newEvents[i].CreatedAt < newEvents[j].CreatedAt
		})

		for _, ev := range newEvents {
			queueProfileFetch(ev.PubKey)
			printMu.Lock()
			ts := formatLocalTimestamp(time.Unix(int64(ev.CreatedAt), 0))
			name := resolveAuthorName(ev.PubKey)
			nw := updateFeedNameWidth(name)
			prefixLen := 14 + 2 + nw + 2
			content := wrapNoteRaw(ev.Content, prefixLen)
			fmt.Print("\r\033[K")
			dim.Printf("%s  ", ts)
			cyan.Printf("%-*s: ", nw, name)
			fmt.Printf("%s\r\n", content)
			fmt.Print("\r")
			formatPrompt(shellPromptName)
			printMu.Unlock()
		}
	}()

	// Connect relays and subscribe to real-time events
	for _, url := range relays {
		go func(url string) {
			connectCtx, cancel := context.WithTimeout(shellCtx, internalRelay.ConnectTimeout)
			defer cancel()

			relay, err := nostr.RelayConnect(connectCtx, url)
			if err != nil {
				return
			}
			defer relay.Close()

			// Wait for follow list to be available
			select {
			case <-shellCtx.Done():
				return
			case <-followReady:
			}

			followMu.Lock()
			hexes := make([]string, len(followedHexes))
			copy(hexes, followedHexes)
			followMu.Unlock()

			if len(hexes) == 0 {
				<-shellCtx.Done()
				return
			}

			since := nostr.Now()
			filters := nostr.Filters{
				{
					Authors: hexes,
					Kinds:   []int{nostr.KindTextNote},
					Since:   &since,
				},
			}

			sub, err := relay.Subscribe(shellCtx, filters)
			if err != nil {
				return
			}
			defer sub.Unsub()

			for {
				select {
				case <-shellCtx.Done():
					return
				case ev, ok := <-sub.Events:
					if !ok {
						return
					}
					printNewEvent(*ev)
				}
			}
		}(url)
	}

	// Handle Ctrl+C
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println()
		shellCancel()
		os.Exit(0)
	}()

	// Async post status channel
	statusCh := make(chan string, 16)

	// Interactive loop
	for {
		// Drain pending statuses
		drainShellStatus(statusCh)

		line, err := readShellLine()
		if err != nil {
			break
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "/") {
			executeSlashCommand(npub, myHex, line, relays, statusCh)
		} else {
			// Create and sign event synchronously
			event := nostr.Event{
				PubKey:    myHex,
				CreatedAt: nostr.Now(),
				Kind:      nostr.KindTextNote,
				Tags:      nostr.Tags{},
				Content:   line,
			}
			if signErr := event.Sign(skHex); signErr != nil {
				color.Red("✗ sign failed: %v", signErr)
				continue
			}

			// Show event in feed immediately and mark as seen
			// so it won't print again when it arrives from relay subscriptions
			_ = cache.LogFeedEvent(npub, event)
			_ = cache.LogSentEvent(npub, event)
			printMu.Lock()
			printFeedEventRaw(event, false)
			fmt.Print("\r")
			printMu.Unlock()

			// Publish with per-relay progress — updates the hint in real time
			total := len(relays)
			timeout := time.Duration(timeoutFlag) * time.Millisecond
			pubCh := internalRelay.PublishEventWithProgress(context.Background(), event, relays, timeout)

			go func() {
				confirmed := 0
				for res := range pubCh {
					if res.OK {
						confirmed++
					}
					setShellHint(fmt.Sprintf("Posting... (%d/%d relays)", confirmed, total))
				}
				setShellHint("")
			}()
		}
	}

	return nil
}


func printFeedEvents(events []nostr.Event, dim, cyan *color.Color) int {
	if len(events) == 0 {
		return 0
	}

	// Compute name column width and seed the global feedNameWidth
	nameWidth := 3
	for _, ev := range events {
		name := resolveAuthorName(ev.PubKey)
		if len(name) > nameWidth {
			nameWidth = len(name)
		}
	}
	feedNameWidthMu.Lock()
	if nameWidth > feedNameWidth {
		feedNameWidth = nameWidth
	}
	feedNameWidthMu.Unlock()

	fmt.Println()
	lines := 1
	for _, ev := range events {
		queueProfileFetch(ev.PubKey)
		ts := formatLocalTimestamp(time.Unix(int64(ev.CreatedAt), 0))
		name := resolveAuthorName(ev.PubKey)
		// Prefix: "03/20 14:19:32  Name: " — timestamp(14) + 2 spaces + name(padded) + ": "
		prefixLen := 14 + 2 + nameWidth + 2
		content := wrapNote(ev.Content, prefixLen)
		dim.Printf("%s  ", ts)
		cyan.Printf("%-*s: ", nameWidth, name)
		fmt.Printf("%s\n", content)
		lines++
	}
	fmt.Println()
	lines++
	return lines
}

func resolveAuthorName(pubHex string) string {
	// Fast path: in-memory profile cache
	if name := cache.ResolveNameByHex(pubHex); name != "" {
		return name
	}
	// Fallback: truncated npub
	npub, err := nip19.EncodePublicKey(pubHex)
	if err != nil {
		return pubHex[:8] + "..."
	}
	return npub[:16] + "..."
}

// profileFetchQueue and related vars manage background profile fetching.
var (
	profileFetchMu      sync.Mutex
	profileFetchPending = make(map[string]bool)
	profileFetchCh      chan string
)

// startProfileFetcher starts a background goroutine that fetches kind 0
// profiles for unknown pubkeys and caches them.
func startProfileFetcher(npub string, relays []string) {
	profileFetchCh = make(chan string, 64)

	go func() {
		for pubHex := range profileFetchCh {
			fetchAndCacheProfile(npub, pubHex, relays)
		}
	}()
}

// queueProfileFetch enqueues a pubkey for background profile fetching
// if it's not already cached or pending.
func queueProfileFetch(pubHex string) {
	if cache.GetProfile(pubHex) != nil {
		return
	}
	profileFetchMu.Lock()
	if profileFetchPending[pubHex] {
		profileFetchMu.Unlock()
		return
	}
	profileFetchPending[pubHex] = true
	profileFetchMu.Unlock()

	select {
	case profileFetchCh <- pubHex:
	default:
		// Channel full, skip
	}
}

func fetchAndCacheProfile(npub, pubHex string, relays []string) {
	targetNpub, err := nip19.EncodePublicKey(pubHex)
	if err != nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	merged := mergeWithDefaults(relays)
	meta, err := profile.FetchFromRelays(ctx, targetNpub, merged)
	if err != nil || meta == nil {
		return
	}

	_ = cache.PutProfile(npub, &cache.CachedProfile{
		PubKey:      pubHex,
		Name:        meta.Name,
		DisplayName: meta.DisplayName,
		About:       meta.About,
		Picture:     meta.Picture,
		NIP05:       meta.NIP05,
		Banner:      meta.Banner,
		Website:     meta.Website,
		LUD16:       meta.LUD16,
	})
}

// termWidth returns the current terminal width, defaulting to 80.
func termWidth() int {
	w, _, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || w <= 0 {
		return 80
	}
	return w
}

// wrapNote wraps content to fit within the available columns after the prefix.
// It preserves newlines and wraps long lines with an indented continuation.
func wrapNote(content string, prefixLen int) string {
	return wrapNoteWithSep(content, prefixLen, "\n")
}

// wrapNoteRaw is like wrapNote but uses \r\n for raw terminal mode.
func wrapNoteRaw(content string, prefixLen int) string {
	return wrapNoteWithSep(content, prefixLen, "\r\n")
}

func wrapNoteWithSep(content string, prefixLen int, newline string) string {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "")
	content = strings.TrimSpace(content)

	// Apply basic markdown rendering
	content = renderInlineMarkdown(content)

	w := termWidth()
	avail := w - prefixLen
	if avail < 20 {
		avail = 20
	}

	indent := strings.Repeat(" ", prefixLen)
	var sb strings.Builder

	// Process each paragraph line separately to preserve newlines
	paragraphs := strings.Split(content, "\n")
	for pi, para := range paragraphs {
		if pi > 0 {
			sb.WriteString(newline)
			sb.WriteString(indent)
		}
		// Wrap this line within avail width
		// Use visible length (excluding ANSI escape codes) for wrapping
		for len(para) > 0 {
			vis := visibleLen(para)
			if vis <= avail {
				sb.WriteString(para)
				break
			}
			// Find a break point at avail visible characters
			lineLen := visibleIndex(para, avail)
			if lineLen <= 0 {
				lineLen = len(para)
			}
			// Try to break at a space
			if lineLen < len(para) {
				cutoff := visibleIndex(para, avail/3)
				if idx := strings.LastIndex(para[:lineLen], " "); idx > cutoff {
					lineLen = idx + 1
				}
			}
			sb.WriteString(strings.TrimRight(para[:lineLen], " "))
			para = para[lineLen:]
			if len(para) > 0 {
				sb.WriteString(newline)
				sb.WriteString(indent)
			}
		}
	}
	return sb.String()
}

// renderInlineMarkdown applies basic inline markdown formatting using ANSI escape codes.
// Supports: **bold**, *italic*, __underline__, ~~strikethrough~~
func renderInlineMarkdown(s string) string {
	// Process in order: bold before italic to avoid conflicts
	s = applyInlineStyle(s, "**", "\033[1m", "\033[22m")       // bold
	s = applyInlineStyle(s, "__", "\033[4m", "\033[24m")        // underline
	s = applyInlineStyle(s, "~~", "\033[9m", "\033[29m")        // strikethrough
	s = applyInlineStyle(s, "*", "\033[3m", "\033[23m")         // italic
	return s
}

// applyInlineStyle finds pairs of the given marker and wraps the content in ANSI codes.
func applyInlineStyle(s, marker, ansiOn, ansiOff string) string {
	var sb strings.Builder
	for {
		start := strings.Index(s, marker)
		if start == -1 {
			sb.WriteString(s)
			break
		}
		end := strings.Index(s[start+len(marker):], marker)
		if end == -1 {
			sb.WriteString(s)
			break
		}
		end += start + len(marker) // absolute index of closing marker
		inner := s[start+len(marker) : end]
		// Skip empty markers or markers spanning newlines
		if inner == "" || strings.Contains(inner, "\n") {
			sb.WriteString(s[:end+len(marker)])
			s = s[end+len(marker):]
			continue
		}
		sb.WriteString(s[:start])
		sb.WriteString(ansiOn)
		sb.WriteString(inner)
		sb.WriteString(ansiOff)
		s = s[end+len(marker):]
	}
	return sb.String()
}

// visibleLen returns the length of a string excluding ANSI escape sequences.
func visibleLen(s string) int {
	n := 0
	inEsc := false
	for _, r := range s {
		if r == '\033' {
			inEsc = true
			continue
		}
		if inEsc {
			if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') {
				inEsc = false
			}
			continue
		}
		n++
	}
	return n
}

// visibleIndex returns the byte index in s where the visible character count reaches n.
func visibleIndex(s string, n int) int {
	vis := 0
	inEsc := false
	for i, r := range s {
		if vis >= n {
			return i
		}
		if r == '\033' {
			inEsc = true
			continue
		}
		if inEsc {
			if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') {
				inEsc = false
			}
			continue
		}
		vis++
	}
	return len(s)
}

func postNoteAsync(npub, myHex, message string, relays []string, statusCh chan<- string) {
	nsec, err := config.LoadNsec(npub)
	if err != nil {
		statusCh <- fmt.Sprintf("✗ %v", err)
		return
	}
	skHex, err := crypto.NsecToHex(nsec)
	if err != nil {
		statusCh <- fmt.Sprintf("✗ %v", err)
		return
	}

	event := nostr.Event{
		PubKey:    myHex,
		CreatedAt: nostr.Now(),
		Kind:      nostr.KindTextNote,
		Tags:      nostr.Tags{},
		Content:   message,
	}
	if err := event.Sign(skHex); err != nil {
		statusCh <- fmt.Sprintf("✗ %v", err)
		return
	}

	ctx := context.Background()
	if _, err := internalRelay.PublishEvent(ctx, event, relays); err != nil {
		statusCh <- fmt.Sprintf("✗ %v", err)
		return
	}

	_ = cache.LogSentEvent(npub, event)
	_ = cache.LogFeedEvent(npub, event)
	statusCh <- "✓ posted"
}

func executeSlashCommand(npub, myHex, line string, relays []string, statusCh chan<- string) {
	// Parse: /command arg1 arg2 ...
	parts := strings.Fields(line)
	if len(parts) == 0 {
		return
	}
	cmd := strings.TrimPrefix(parts[0], "/")
	args := parts[1:]

	switch cmd {
	case "follow":
		if len(args) == 0 {
			color.Red("Usage: /follow <npub|alias|nip05>")
			return
		}
		targetHex, err := resolve.Resolve(npub, args[0])
		if err != nil {
			color.Red("Cannot resolve %q: %v", args[0], err)
			return
		}
		sp := ui.NewSpinner("Fetching contact list...")
		ctx := context.Background()
		contacts, err := fetchContactList(ctx, myHex, relays)
		sp.Stop()
		if err != nil {
			color.Red("Error: %v", err)
			return
		}
		for _, tag := range contacts.Tags {
			if len(tag) >= 2 && tag[0] == "p" && tag[1] == targetHex {
				targetNpub, _ := nip19.EncodePublicKey(targetHex)
				color.Yellow("Already following %s", targetNpub)
				return
			}
		}
		contacts.Tags = append(contacts.Tags, nostr.Tag{"p", targetHex})
		contacts.CreatedAt = nostr.Now()
		contacts.ID = ""
		contacts.Sig = ""
		nsec, err := config.LoadNsec(npub)
		if err != nil {
			color.Red("Error: %v", err)
			return
		}
		skHex, err := crypto.NsecToHex(nsec)
		if err != nil {
			color.Red("Error: %v", err)
			return
		}
		if err := contacts.Sign(skHex); err != nil {
			color.Red("Error: %v", err)
			return
		}
		sp = ui.NewSpinner("Publishing...")
		_, err = internalRelay.PublishEvent(ctx, *contacts, relays)
		sp.Stop()
		if err != nil {
			color.Red("Error: %v", err)
			return
		}
		_ = cache.LogSentEvent(npub, *contacts)
		cacheFollowingFromTags(npub, contacts.Tags)
		targetNpub, _ := nip19.EncodePublicKey(targetHex)
		color.Green("✓ Now following %s", targetNpub)

	case "following":
		// Try cache first
		cyanFn := color.New(color.FgCyan).SprintFunc()
		dimColor := color.New(color.Faint)
		refresh := len(args) > 0 && args[0] == "--refresh"
		if !refresh {
			if cached := cache.LoadFollowing(npub); cached != nil && len(cached.Hexes) > 0 {
				printFollowingList(cached.Hexes, cyanFn, dimColor)
				for _, hex := range cached.Hexes {
					queueProfileFetch(hex)
				}
				return
			}
		}
		sp := ui.NewSpinner("Fetching contact list...")
		ctx := context.Background()
		contacts, err := fetchContactList(ctx, myHex, relays)
		sp.Stop()
		if err != nil {
			color.Red("Error: %v", err)
			return
		}
		var hexes []string
		for _, tag := range contacts.Tags {
			if len(tag) >= 2 && tag[0] == "p" {
				hexes = append(hexes, tag[1])
			}
		}
		_ = cache.SaveFollowing(npub, hexes)
		if len(hexes) == 0 {
			fmt.Println("You're not following anyone yet.")
			return
		}
		printFollowingList(hexes, cyanFn, dimColor)
		for _, hex := range hexes {
			queueProfileFetch(hex)
		}

	case "unfollow":
		if len(args) == 0 {
			color.Red("Usage: /unfollow <npub|alias|nip05>")
			return
		}
		targetHex, err := resolve.Resolve(npub, args[0])
		if err != nil {
			color.Red("Cannot resolve %q: %v", args[0], err)
			return
		}
		sp := ui.NewSpinner("Fetching contact list...")
		ctx := context.Background()
		contacts, err := fetchContactList(ctx, myHex, relays)
		sp.Stop()
		if err != nil {
			color.Red("Error: %v", err)
			return
		}
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
			targetNpub, _ := nip19.EncodePublicKey(targetHex)
			color.Yellow("Not following %s", targetNpub)
			return
		}
		contacts.Tags = newTags
		contacts.CreatedAt = nostr.Now()
		contacts.ID = ""
		contacts.Sig = ""
		nsec, err := config.LoadNsec(npub)
		if err != nil {
			color.Red("Error: %v", err)
			return
		}
		skHex, err := crypto.NsecToHex(nsec)
		if err != nil {
			color.Red("Error: %v", err)
			return
		}
		if err := contacts.Sign(skHex); err != nil {
			color.Red("Error: %v", err)
			return
		}
		sp = ui.NewSpinner("Publishing...")
		_, err = internalRelay.PublishEvent(ctx, *contacts, relays)
		sp.Stop()
		if err != nil {
			color.Red("Error: %v", err)
			return
		}
		_ = cache.LogSentEvent(npub, *contacts)
		cacheFollowingFromTags(npub, contacts.Tags)
		targetNpub, _ := nip19.EncodePublicKey(targetHex)
		color.Green("✓ Unfollowed %s", targetNpub)

	case "dm":
		if len(args) < 2 {
			color.Red("Usage: /dm <user> <message>")
			return
		}
		targetHex, err := resolve.Resolve(npub, args[0])
		if err != nil {
			color.Red("Cannot resolve %q: %v", args[0], err)
			return
		}
		nsec, err := config.LoadNsec(npub)
		if err != nil {
			color.Red("Error: %v", err)
			return
		}
		skHex, err := crypto.NsecToHex(nsec)
		if err != nil {
			color.Red("Error: %v", err)
			return
		}
		message := strings.Join(args[1:], " ")
		go sendDMAsync(npub, skHex, myHex, targetHex, message, relays, statusCh)

	case "profile":
		user := npub
		if len(args) > 0 {
			resolved, err := resolve.ResolveToNpub(npub, args[0])
			if err != nil {
				color.Red("Cannot resolve %q: %v", args[0], err)
				return
			}
			user = resolved
		}
		label := color.New(color.FgCyan).SprintFunc()

		// Try cache first for immediate display
		userHex, _ := crypto.NpubToHex(user)
		cached := cache.GetProfile(userHex)
		if cached != nil {
			fmt.Printf("%s %s\n", label("npub:"), user)
			printColorField(label, "Name", cached.Name)
			printColorField(label, "Display Name", cached.DisplayName)
			printColorField(label, "About", cached.About)
			printColorField(label, "Website", cached.Website)
			// Refresh in background if stale
			if cache.IsProfileStale(userHex, 1*time.Hour) {
				queueProfileFetch(userHex)
			}
			return
		}

		// Not cached — fetch from relays
		mergedRelays := mergeWithDefaults(relays)
		ctx := context.Background()
		sp := ui.NewSpinner("Fetching profile...")
		meta, err := profile.FetchFromRelays(ctx, user, mergedRelays)
		sp.Stop()
		if err != nil || meta == nil {
			color.Red("Profile not found")
			return
		}
		// Cache the result
		_ = cache.PutProfile(npub, &cache.CachedProfile{
			PubKey:      userHex,
			Name:        meta.Name,
			DisplayName: meta.DisplayName,
			About:       meta.About,
			Picture:     meta.Picture,
			NIP05:       meta.NIP05,
			Banner:      meta.Banner,
			Website:     meta.Website,
			LUD16:       meta.LUD16,
		})
		fmt.Printf("%s %s\n", label("npub:"), user)
		printColorField(label, "Name", meta.Name)
		printColorField(label, "Display Name", meta.DisplayName)
		printColorField(label, "About", meta.About)
		printColorField(label, "Website", meta.Website)

	case "edit-profile":
		if err := runProfileUpdate(nil, nil); err != nil {
			color.Red("Error: %v", err)
		}

	case "switch":
		entries, err := listSwitchableProfiles()
		if err != nil {
			color.Red("Error: %v", err)
			return
		}
		if len(entries) == 0 {
			fmt.Println("No other profiles found. Run 'nostr login' to add one.")
			return
		}
		if len(args) > 0 {
			// Direct switch
			if err := switchToTarget(args[0], npub, color.New(color.FgGreen)); err != nil {
				color.Red("Error: %v", err)
				return
			}
			// Update shell prompt name
			newNpub, _ := config.ActiveProfile()
			newHex, _ := crypto.NpubToHex(newNpub)
			if name := cache.ResolveNameByHex(newHex); name != "" {
				shellPromptName = name
			} else if meta, _ := profile.LoadCached(newNpub); meta != nil && meta.Name != "" {
				shellPromptName = meta.Name
			} else {
				shellPromptName = newNpub[:16] + "..."
			}
			return
		}
		// Interactive selection
		selected := 0
		for i, e := range entries {
			if e.npub == npub {
				selected = i
				break
			}
		}
		chosen := shellInteractiveSelect(entries, selected)
		if chosen < 0 {
			return
		}
		target := entries[chosen]
		if target.npub == npub {
			fmt.Println("Already on this profile.")
			return
		}
		if err := config.SetActiveProfile(target.npub); err != nil {
			color.Red("Error: %v", err)
			return
		}
		// Update shell prompt name
		newNpub := target.npub
		newHex, _ := crypto.NpubToHex(newNpub)
		if name := cache.ResolveNameByHex(newHex); name != "" {
			shellPromptName = name
		} else if meta, _ := profile.LoadCached(newNpub); meta != nil && meta.Name != "" {
			shellPromptName = meta.Name
		} else {
			shellPromptName = newNpub[:16] + "..."
		}
		if target.name != "" {
			color.New(color.FgGreen).Printf("Switched to %s (%s)\n", target.name, target.npub)
		} else {
			color.New(color.FgGreen).Printf("Switched to %s\n", target.npub)
		}

	case "alias":
		if len(args) < 2 {
			color.Red("Usage: /alias <name> <npub|nip05>")
			return
		}
		name := args[0]
		target := args[1]
		targetNpub, err := resolve.ResolveToNpub(npub, target)
		if err != nil {
			color.Red("Cannot resolve %q: %v", target, err)
			return
		}
		if err := config.SetGlobalAlias(name, targetNpub); err != nil {
			color.Red("Error: %v", err)
			return
		}
		color.Green("✓ Alias %s → %s", name, targetNpub)

	case "aliases":
		aliases, err := config.LoadGlobalAliases()
		if err != nil {
			color.Red("Error: %v", err)
			return
		}
		if len(aliases) == 0 {
			fmt.Println("No aliases configured.")
			return
		}
		cyanFn := color.New(color.FgCyan).SprintFunc()
		names := make([]string, 0, len(aliases))
		for n := range aliases {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			fmt.Printf("  %s → %s\n", cyanFn(n), aliases[n])
		}

	case "post":
		if len(args) == 0 {
			color.Red("Usage: /post <message>")
			return
		}
		message := strings.Join(args, " ")
		go postNoteAsync(npub, myHex, message, relays, statusCh)

	case "relays":
		currentRelays, err := config.LoadRelays(npub)
		if err != nil {
			color.Red("Error: %v", err)
			return
		}
		cyanFn := color.New(color.FgCyan).SprintFunc()
		fmt.Printf("Relays (%d):\n\n", len(currentRelays))
		for i, r := range currentRelays {
			fmt.Printf("  %s %s\n", cyanFn(fmt.Sprintf("%d.", i+1)), r)
		}

	case "nip":
		if len(args) == 0 {
			color.Red("Usage: /nip <number>")
			return
		}
		if err := fetchAndDisplayNIP(args[0]); err != nil {
			color.Red("Error: %v", err)
		}

	case "version":
		runVersion(nil, nil)

	case "update":
		if err := runUpdate(nil, nil); err != nil {
			color.Red("Error: %v", err)
		}

	default:
		color.Red("Unknown command: /%s", cmd)
	}
}

func mergeWithDefaults(relays []string) []string {
	seen := make(map[string]bool, len(relays))
	for _, r := range relays {
		seen[r] = true
	}
	merged := append([]string{}, relays...)
	for _, r := range config.DefaultRelays() {
		if !seen[r] {
			merged = append(merged, r)
		}
	}
	return merged
}

func drainShellStatus(ch <-chan string) {
	for {
		select {
		case status := <-ch:
			fmt.Println(status)
		default:
			return
		}
	}
}

// --- Raw terminal prompt with slash command autocomplete ---

func readShellLine() (string, error) {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		// Fallback for non-TTY
		var line string
		formatPrompt(shellPromptName)
		_, err := fmt.Scanln(&line)
		return line, err
	}

	oldState, err := term.MakeRaw(fd)
	if err != nil {
		// Fallback
		var line string
		formatPrompt(shellPromptName)
		_, err := fmt.Scanln(&line)
		return line, err
	}
	defer term.Restore(fd, oldState)

	var buf []byte
	selected := 0
	showMenu := false
	prevMenuSize := 0

	renderPrompt(buf, showMenu, selected, prevMenuSize, shellRelayCount)

	b := make([]byte, 3)
	for {
		n, err := os.Stdin.Read(b)
		if err != nil {
			return "", err
		}

		if n == 1 {
			switch b[0] {
			case 3: // Ctrl-C
				clearMenu(prevMenuSize)
				fmt.Print("\r\n")
				return "", fmt.Errorf("interrupted")

			case 4: // Ctrl-D
				clearMenu(prevMenuSize)
				fmt.Print("\r\n")
				return "", fmt.Errorf("exit")

			case 13: // Enter
				if showMenu {
					cmds := filterCommands(buf)
					if selected >= 0 && selected < len(cmds) {
						buf = []byte("/" + cmds[selected].name + " ")
						showMenu = false
						renderPrompt(buf, showMenu, selected, prevMenuSize, shellRelayCount)
						prevMenuSize = 1 // hint line
						continue
					}
				}
				clearMenu(prevMenuSize)
				fmt.Print("\r\n")
				return string(buf), nil

			case 9: // Tab
				if showMenu {
					cmds := filterCommands(buf)
					if selected >= 0 && selected < len(cmds) {
						buf = []byte("/" + cmds[selected].name + " ")
						showMenu = false
					}
				}

			case 127, 8: // Backspace
				if len(buf) > 0 {
					buf = buf[:len(buf)-1]
				}
				if len(buf) == 0 {
					showMenu = false
				} else if buf[0] == '/' && !strings.Contains(string(buf), " ") {
					showMenu = true
					selected = 0
				} else {
					showMenu = false
				}

			default:
				if b[0] >= 32 { // printable
					buf = append(buf, b[0])
					if len(buf) == 1 && buf[0] == '/' {
						showMenu = true
						selected = 0
					} else if len(buf) > 1 && buf[0] == '/' && strings.Contains(string(buf), " ") {
						showMenu = false
					} else if showMenu {
						// Re-filter, reset selection
						cmds := filterCommands(buf)
						if selected >= len(cmds) {
							selected = 0
						}
					}
				}
			}
		}

		if n == 3 && b[0] == 27 && b[1] == '[' {
			if showMenu {
				cmds := filterCommands(buf)
				switch b[2] {
				case 'A': // Up
					if selected > 0 {
						selected--
					}
				case 'B': // Down
					if selected < len(cmds)-1 {
						selected++
					}
				}
			}
		}

		// Handle bare Escape (n==1, b[0]==27) — cancel menu
		if n == 1 && b[0] == 27 {
			showMenu = false
		}

		cmds := filterCommands(buf)
		if showMenu && len(cmds) == 0 {
			showMenu = false
		}

		newMenuSize := hintLinesForInput(buf, showMenu, cmds, shellRelayCount)
		renderPrompt(buf, showMenu, selected, prevMenuSize, shellRelayCount)
		prevMenuSize = newMenuSize
	}
}

func filterCommands(buf []byte) []slashCmd {
	if len(buf) == 0 || buf[0] != '/' {
		return nil
	}
	prefix := strings.TrimPrefix(string(buf), "/")
	if strings.Contains(prefix, " ") {
		return nil
	}
	var result []slashCmd
	for _, cmd := range slashCommands {
		if strings.HasPrefix(cmd.name, prefix) {
			result = append(result, cmd)
		}
	}
	return result
}

func renderPrompt(buf []byte, showMenu bool, selected int, prevMenuSize int, totalRelays int) {
	dim := color.New(color.Faint)
	cyan := color.New(color.FgCyan)

	// Clear the prompt line and any previous menu lines below
	fmt.Print("\r\033[K") // clear current line
	if prevMenuSize > 0 {
		for i := 0; i < prevMenuSize; i++ {
			fmt.Print("\r\n\033[K") // move down and clear
		}
		// Move back up
		fmt.Printf("\033[%dA", prevMenuSize)
	}

	// Draw prompt
	promptPrefixLen := formatPrompt(shellPromptName)
	fmt.Print(string(buf))

	if showMenu {
		cmds := filterCommands(buf)
		for i, cmd := range cmds {
			fmt.Print("\r\n\033[K")
			if i == selected {
				fmt.Print("  ")
				cyan.Printf("> %-12s", cmd.name)
				dim.Printf(" %s", cmd.desc)
			} else {
				fmt.Print("    ")
				dim.Printf("%-12s %s", cmd.name, cmd.desc)
			}
		}
		// Move cursor back to prompt line
		if len(cmds) > 0 {
			fmt.Printf("\033[%dA", len(cmds))
		}
	} else {
		// Show hint below prompt
		hint := hintTextForInput(buf, totalRelays)
		hintLines := hintLineCount(hint, termWidth())
		if hintLines == 0 {
			hintLines = 1
		}
		fmt.Print("\r\n\033[K")
		if hint != "" {
			dim.Print(hint)
		}
		// Clear any extra lines from previous render that extend beyond current hint
		if prevMenuSize > hintLines {
			for i := 0; i < prevMenuSize-hintLines; i++ {
				fmt.Print("\r\n\033[K")
			}
			// Move back up to end of hint
			fmt.Printf("\033[%dA", prevMenuSize-hintLines)
		}
		// Move cursor back to prompt line (up past all hint lines)
		fmt.Printf("\033[%dA", hintLines)
	}

	// Position cursor at end of input on prompt line
	fmt.Printf("\r\033[%dC", promptPrefixLen+len(buf))
}

// shellInteractiveSelect shows an interactive profile picker using arrow keys.
// Returns the chosen index, or -1 if cancelled.
func shellInteractiveSelect(entries []profileEntry, selected int) int {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		// Non-TTY fallback: just list them
		cyanFn := color.New(color.FgCyan).SprintFunc()
		dimFn := color.New(color.Faint).SprintFunc()
		for _, e := range entries {
			if e.name != "" {
				fmt.Printf("  %s %s\n", cyanFn(e.name), dimFn(e.npub[:20]+"..."))
			} else {
				fmt.Printf("  %s\n", e.npub)
			}
		}
		return -1
	}

	oldState, err := term.MakeRaw(fd)
	if err != nil {
		return -1
	}
	defer term.Restore(fd, oldState)

	cyan := color.New(color.FgCyan).SprintFunc()
	dim := color.New(color.Faint).SprintFunc()
	activeNpub, _ := config.ActiveProfile()

	render := func() {
		fmt.Print("\r\033[J") // clear from cursor to end of screen
		fmt.Print("Select a profile (↑↓ to move, enter to select, q to cancel):\r\n\r\n")
		for i, e := range entries {
			label := e.npub
			short := e.npub
			if len(short) > 20 {
				short = short[:20] + "..."
			}
			if e.name != "" {
				label = fmt.Sprintf("%s (%s)", cyan(e.name), short)
			}
			active := ""
			if e.npub == activeNpub {
				active = " (active)"
			}
			if i == selected {
				fmt.Printf("  > %s%s\r\n", label, active)
			} else {
				if e.name != "" {
					fmt.Printf("    %s\r\n", dim(fmt.Sprintf("%s (%s)%s", e.name, short, active)))
				} else {
					fmt.Printf("    %s\r\n", dim(fmt.Sprintf("%s%s", e.npub, active)))
				}
			}
		}
	}

	render()

	b := make([]byte, 1)
	for {
		if _, err := os.Stdin.Read(b); err != nil {
			return -1
		}

		switch b[0] {
		case 13: // enter
			fmt.Print("\r\033[J")
			return selected
		case 'q', 3: // q or Ctrl-C
			fmt.Print("\r\033[J")
			return -1
		case 27: // ESC — could be arrow key or bare Esc
			// Read next two bytes to check for arrow sequence
			seq := make([]byte, 2)
			n, _ := os.Stdin.Read(seq)
			if n == 2 && seq[0] == '[' {
				switch seq[1] {
				case 'A': // up arrow
					if selected > 0 {
						selected--
					}
				case 'B': // down arrow
					if selected < len(entries)-1 {
						selected++
					}
				}
			} else if n == 1 && seq[0] == '[' {
				// Got '[', read one more for the letter
				if _, err := os.Stdin.Read(seq[:1]); err == nil {
					switch seq[0] {
					case 'A':
						if selected > 0 {
							selected--
						}
					case 'B':
						if selected < len(entries)-1 {
							selected++
						}
					}
				}
			} else {
				// Bare Esc
				fmt.Print("\r\033[J")
				return -1
			}
		case 'k': // vim up
			if selected > 0 {
				selected--
			}
		case 'j': // vim down
			if selected < len(entries)-1 {
				selected++
			}
		}

		// Re-render: move cursor up to overwrite
		lines := len(entries) + 2 // header + blank + entries
		fmt.Printf("\033[%dA", lines)
		render()
	}
}

func clearMenu(menuSize int) {
	if menuSize > 0 {
		for i := 0; i < menuSize; i++ {
			fmt.Print("\r\n\033[K")
		}
		fmt.Printf("\033[%dA", menuSize)
	}
}

// hintLineCount returns the number of terminal lines a hint string occupies,
// accounting for wrapping at the given terminal width.
func hintLineCount(hint string, tw int) int {
	if hint == "" || tw <= 0 {
		return 0
	}
	n := len(hint)
	return (n + tw - 1) / tw
}

// hintTextForInput returns the hint text that would be shown below the prompt
// for the given input buffer state. If shellHintOverride is set, it takes priority.
func hintTextForInput(buf []byte, totalRelays int) string {
	shellHintMu.Lock()
	override := shellHintOverride
	shellHintMu.Unlock()
	if override != "" {
		return "  " + override
	}
	if len(buf) == 0 {
		return fmt.Sprintf("  type / for commands, enter to post a public note to %d relays, ctrl+c to exit", totalRelays)
	}
	if buf[0] != '/' {
		return fmt.Sprintf("  enter to post a public note to %d relays, ctrl+c to exit", totalRelays)
	}
	return ""
}

// defaultHintText returns the default hint (ignoring override) for a given input state.
func defaultHintText(buf []byte, totalRelays int) string {
	if len(buf) == 0 {
		return fmt.Sprintf("  type / for commands, enter to post a public note to %d relays, ctrl+c to exit", totalRelays)
	}
	if buf[0] != '/' {
		return fmt.Sprintf("  enter to post a public note to %d relays, ctrl+c to exit", totalRelays)
	}
	return ""
}

// hintLinesForInput returns the number of terminal lines occupied by the hint
// or menu below the prompt.
func hintLinesForInput(buf []byte, showMenu bool, cmds []slashCmd, totalRelays int) int {
	if showMenu {
		return len(cmds)
	}
	hint := hintTextForInput(buf, totalRelays)
	tw := termWidth()
	lines := hintLineCount(hint, tw)
	if lines == 0 {
		return 1 // at minimum reserve 1 line for the hint area
	}
	return lines
}
