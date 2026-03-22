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
	npub, err := config.LoadResolvedProfile(profileFlag)
	if err != nil {
		return fmt.Errorf("no active profile. Run 'nostr login' first")
	}

	// Reset feed name width for this session
	feedNameWidthMu.Lock()
	feedNameWidth = 0
	feedNameWidthMu.Unlock()

	cyan := color.New(color.FgCyan)
	dim := color.New(color.Faint)
	green := color.New(color.FgGreen)

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

	// Show header
	fmt.Printf("nostr  ")
	dim.Print("connecting...")
	fmt.Println()

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

	shellCtx, shellCancel := context.WithCancel(context.Background())
	defer shellCancel()

	var connMu sync.Mutex
	var connCount int
	totalRelays := len(relays)

	headerUpdate := func() {
		connMu.Lock()
		count := connCount
		connMu.Unlock()
		followMu.Lock()
		nFollowing := len(followedHexes)
		followMu.Unlock()
		printMu.Lock()
		fmt.Print("\0337") // save cursor
		fmt.Print("\033[H") // move to row 1, col 1
		fmt.Print("\r\033[K")
		fmt.Printf("nostr  ")
		if nFollowing > 0 {
			cyan.Printf("following %d", nFollowing)
			fmt.Print("  ")
		}
		if count >= totalRelays {
			green.Printf("%d/%d relays", count, totalRelays)
		} else {
			dim.Printf("%d/%d relays", count, totalRelays)
		}
		fmt.Print("\0338") // restore cursor
		printMu.Unlock()
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
		ts := formatLocalTimestamp(time.Unix(int64(ev.CreatedAt), 0))
		name := resolveAuthorName(ev.PubKey)
		nw := updateFeedNameWidth(name)
		prefixLen := 14 + 2 + nw + 2
		content := wrapNoteRaw(ev.Content, prefixLen)
		fmt.Print("\r\033[K")
		dim.Printf("%s  ", ts)
		cyan.Printf("%-*s: ", nw, name)
		fmt.Printf("%s\r\n", content)
		fmt.Printf("\r%s> ", color.New(color.FgGreen).Sprint(shellPromptName))
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

		headerUpdate()

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
				fmt.Printf("\r%s> ", color.New(color.FgGreen).Sprint(shellPromptName))
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
			fmt.Printf("\r%s> ", color.New(color.FgGreen).Sprint(shellPromptName))
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

			connMu.Lock()
			connCount++
			connMu.Unlock()
			headerUpdate()

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
			// Post a note
			go postNoteAsync(npub, myHex, line, relays, statusCh)
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
// It replaces newlines with spaces, then wraps long lines with an indented continuation.
func wrapNote(content string, prefixLen int) string {
	return wrapNoteWithSep(content, prefixLen, "\n")
}

// wrapNoteRaw is like wrapNote but uses \r\n for raw terminal mode.
func wrapNoteRaw(content string, prefixLen int) string {
	return wrapNoteWithSep(content, prefixLen, "\r\n")
}

func wrapNoteWithSep(content string, prefixLen int, newline string) string {
	content = strings.ReplaceAll(content, "\n", " ")
	content = strings.ReplaceAll(content, "\r", "")
	content = strings.TrimSpace(content)

	w := termWidth()
	avail := w - prefixLen
	if avail < 20 {
		avail = 20
	}

	if len(content) <= avail {
		return content
	}

	// Wrap into multiple lines with indent matching the prefix
	indent := strings.Repeat(" ", prefixLen)
	var sb strings.Builder
	for len(content) > 0 {
		lineLen := avail
		if lineLen > len(content) {
			lineLen = len(content)
		}
		// Try to break at a space
		if lineLen < len(content) {
			if idx := strings.LastIndex(content[:lineLen], " "); idx > avail/3 {
				lineLen = idx + 1
			}
		}
		if sb.Len() > 0 {
			sb.WriteString(newline)
			sb.WriteString(indent)
		}
		sb.WriteString(strings.TrimRight(content[:lineLen], " "))
		content = content[lineLen:]
	}
	return sb.String()
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
	if err := internalRelay.PublishEvent(ctx, event, relays); err != nil {
		statusCh <- fmt.Sprintf("✗ %v", err)
		return
	}

	_ = cache.LogEvent(npub, event)
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
		err = internalRelay.PublishEvent(ctx, *contacts, relays)
		sp.Stop()
		if err != nil {
			color.Red("Error: %v", err)
			return
		}
		_ = cache.LogEvent(npub, *contacts)
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
		err = internalRelay.PublishEvent(ctx, *contacts, relays)
		sp.Stop()
		if err != nil {
			color.Red("Error: %v", err)
			return
		}
		_ = cache.LogEvent(npub, *contacts)
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
		fmt.Printf("%s> ", shellPromptName)
		_, err := fmt.Scanln(&line)
		return line, err
	}

	oldState, err := term.MakeRaw(fd)
	if err != nil {
		// Fallback
		var line string
		fmt.Printf("%s> ", shellPromptName)
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

		newMenuSize := 1 // hint line
		if showMenu {
			newMenuSize = len(cmds)
		}
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

func renderPrompt(buf []byte, showMenu bool, selected int, prevMenuSize int, relayCount int) {
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
	promptPrefix := shellPromptName + "> "
	color.New(color.FgGreen).Print(shellPromptName)
	fmt.Print("> ")
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
		fmt.Print("\r\n\033[K")
		if len(buf) == 0 {
			dim.Print("  type / for commands, start typing to post a note, ctrl+c to exit")
		} else if buf[0] != '/' {
			dim.Printf("  press enter to post to %d relays, ctrl+c to exit", relayCount)
		}
		// Move cursor back to prompt line
		fmt.Print("\033[1A")

		// Clear any leftover menu lines (below hint)
		if prevMenuSize > 1 {
			// hint takes 1 line, clear the rest
			fmt.Print("\033[s") // save cursor
			for i := 0; i < prevMenuSize; i++ {
				fmt.Print("\r\n\033[K")
			}
			fmt.Print("\033[u") // restore cursor
		}
	}

	// Position cursor at end of input on prompt line
	fmt.Printf("\r\033[%dC", len(promptPrefix)+len(buf))
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
