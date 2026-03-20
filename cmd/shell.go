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
	{"follow", "/follow <user>", "Follow a user"},
	{"unfollow", "/unfollow <user>", "Unfollow a user"},
	{"profile", "/profile [user]", "View a profile"},
	{"switch", "/switch [user]", "Switch active profile"},
	{"alias", "/alias <name> <npub>", "Create an alias"},
	{"aliases", "/aliases", "List aliases"},
}

// shellPromptName is the current user's display name for the prompt.
var shellPromptName string

func runShell() error {
	npub, err := config.LoadResolvedProfile(profileFlag)
	if err != nil {
		return fmt.Errorf("no active profile. Run 'nostr login' first")
	}

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

	// Show header immediately
	fmt.Printf("nostr  ")
	dim.Print("connecting...")
	fmt.Println()

	// Fetch follow list
	sp := ui.NewSpinner("Loading feed...")
	ctx := context.Background()
	contacts, err := fetchContactList(ctx, myHex, relays)
	sp.Stop()

	var followedHexes []string
	if err == nil {
		for _, tag := range contacts.Tags {
			if len(tag) >= 2 && tag[0] == "p" {
				followedHexes = append(followedHexes, tag[1])
			}
		}
	}

	// Rewrite header with follow count
	fmt.Print("\033[2A") // up to header
	fmt.Print("\r\033[K")
	fmt.Printf("nostr  ")
	cyan.Printf("following %d", len(followedHexes))
	fmt.Print("  ")
	dim.Print("connecting...")
	fmt.Println()
	fmt.Print("\033[1B") // back down past the spinner-cleared line

	// Start background profile fetcher and queue all followed pubkeys
	startProfileFetcher(npub, relays)
	for _, hex := range followedHexes {
		queueProfileFetch(hex)
	}

	if len(followedHexes) == 0 {
		fmt.Println()
		dim.Println("You're not following anyone yet.")
		dim.Println("  Use /follow <npub|alias|nip05> to follow someone.")
		fmt.Println()
	} else {
		// Fetch recent notes from followed users
		feedLines := showFeed(ctx, npub, myHex, followedHexes, relays, dim)
		_ = feedLines
	}

	// Connect relays in background, update header
	shellCtx, shellCancel := context.WithCancel(context.Background())
	defer shellCancel()

	var connMu sync.Mutex
	var connCount int
	totalRelays := len(relays)

	// Lines from header to current cursor position varies, so we track it.
	// We use save/restore cursor to update the header.
	headerUpdate := func() {
		connMu.Lock()
		count := connCount
		connMu.Unlock()
		fmt.Print("\0337") // save cursor
		// Move to very top — we printed the header as the first line
		fmt.Print("\033[H") // move to row 1, col 1
		fmt.Print("\r\033[K")
		fmt.Printf("nostr  ")
		cyan.Printf("following %d", len(followedHexes))
		fmt.Print("  ")
		if count >= totalRelays {
			green.Printf("%d/%d relays", count, totalRelays)
		} else {
			dim.Printf("%d/%d relays", count, totalRelays)
		}
		fmt.Print("\0338") // restore cursor
	}

	// Deduplication for incoming notes
	var seenMu sync.Mutex
	seen := make(map[string]bool)

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

			if len(followedHexes) == 0 {
				// Nothing to subscribe to, just keep connection for publishing
				<-shellCtx.Done()
				return
			}

			since := nostr.Now()
			filters := nostr.Filters{
				{
					Authors: followedHexes,
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
					seenMu.Lock()
					if seen[ev.ID] {
						seenMu.Unlock()
						continue
					}
					seen[ev.ID] = true
					seenMu.Unlock()

					_ = cache.LogEvent(npub, *ev)
					queueProfileFetch(ev.PubKey)
					authorName := resolveAuthorName(ev.PubKey)
					ts := formatLocalTimestamp(time.Unix(int64(ev.CreatedAt), 0))
					fmt.Print("\r\033[K")
					dim.Printf("%s  ", ts)
					cyan.Printf("%s: ", authorName)
					fmt.Printf("%s\n", truncateNote(ev.Content, 120))
					fmt.Printf("%s> ", color.New(color.FgGreen).Sprint(shellPromptName))
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

func showFeed(ctx context.Context, npub, myHex string, followedHexes []string, relays []string, dim *color.Color) int {
	cyan := color.New(color.FgCyan)

	// Build a set for fast lookup
	followSet := make(map[string]bool, len(followedHexes))
	for _, hex := range followedHexes {
		followSet[hex] = true
	}

	// Load from cache immediately (instant)
	cachedEvents, _ := cache.QueryEvents(npub, func(ev nostr.Event) bool {
		return ev.Kind == nostr.KindTextNote && followSet[ev.PubKey]
	})

	// Deduplicate and sort cached events
	seenIDs := make(map[string]bool, len(cachedEvents))
	var events []nostr.Event
	for _, ev := range cachedEvents {
		if !seenIDs[ev.ID] {
			seenIDs[ev.ID] = true
			events = append(events, ev)
		}
	}

	sort.Slice(events, func(i, j int) bool {
		return events[i].CreatedAt < events[j].CreatedAt
	})

	// Take last 20 from cache
	if len(events) > 20 {
		events = events[len(events)-20:]
	}

	// Display cached feed immediately
	lines := printFeedEvents(events, dim, cyan)

	// Fetch from relays in background, append only new events
	go func() {
		filter := nostr.Filter{
			Authors: followedHexes,
			Kinds:   []int{nostr.KindTextNote},
			Limit:   20,
		}
		fetched, err := internalRelay.FetchEvents(ctx, filter, relays)
		if err != nil || len(fetched) == 0 {
			return
		}
		cache.LogEvents(npub, fetched)

		// Print only events not already shown
		var newEvents []nostr.Event
		for _, ev := range fetched {
			if !seenIDs[ev.ID] {
				seenIDs[ev.ID] = true
				newEvents = append(newEvents, *ev)
			}
		}
		if len(newEvents) == 0 {
			return
		}

		sort.Slice(newEvents, func(i, j int) bool {
			return newEvents[i].CreatedAt < newEvents[j].CreatedAt
		})

		for _, ev := range newEvents {
			queueProfileFetch(ev.PubKey)
			ts := formatLocalTimestamp(time.Unix(int64(ev.CreatedAt), 0))
			name := resolveAuthorName(ev.PubKey)
			content := truncateNote(ev.Content, 120)
			fmt.Print("\r\033[K")
			dim.Printf("%s  ", ts)
			cyan.Printf("%s: ", name)
			fmt.Printf("%s\n", content)
			fmt.Printf("%s> ", color.New(color.FgGreen).Sprint(shellPromptName))
		}
	}()

	return lines
}

func printFeedEvents(events []nostr.Event, dim, cyan *color.Color) int {
	if len(events) == 0 {
		return 0
	}

	// Compute name column width
	nameWidth := 3
	for _, ev := range events {
		name := resolveAuthorName(ev.PubKey)
		if len(name) > nameWidth {
			nameWidth = len(name)
		}
	}

	fmt.Println()
	lines := 1
	for _, ev := range events {
		queueProfileFetch(ev.PubKey)
		ts := formatLocalTimestamp(time.Unix(int64(ev.CreatedAt), 0))
		name := resolveAuthorName(ev.PubKey)
		content := truncateNote(ev.Content, 120)
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

func truncateNote(content string, maxLen int) string {
	// Replace newlines with spaces for compact display
	content = strings.ReplaceAll(content, "\n", " ")
	content = strings.ReplaceAll(content, "\r", "")
	if len(content) > maxLen {
		return content[:maxLen-1] + "…"
	}
	return content
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
		targetNpub, _ := nip19.EncodePublicKey(targetHex)
		color.Green("✓ Now following %s", targetNpub)

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
		// Show list for selection
		cyanFn := color.New(color.FgCyan).SprintFunc()
		dimFn := color.New(color.Faint).SprintFunc()
		for _, e := range entries {
			marker := "  "
			if e.npub == npub {
				marker = "→ "
			}
			if e.name != "" {
				fmt.Printf("%s%s %s\n", marker, cyanFn(e.name), dimFn(e.npub[:20]+"..."))
			} else {
				fmt.Printf("%s%s\n", marker, e.npub)
			}
		}
		fmt.Println()
		color.New(color.Faint).Println("Use /switch <name|npub> to switch.")

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
		aliases, err := resolve.LoadAliases(npub)
		if err != nil {
			color.Red("Error: %v", err)
			return
		}
		aliases[name] = targetNpub
		if err := resolve.SaveAliases(npub, aliases); err != nil {
			color.Red("Error: %v", err)
			return
		}
		_ = config.CreateProfileSymlink(name, targetNpub)
		color.Green("✓ Alias %s → %s", name, targetNpub)

	case "aliases":
		aliases, err := resolve.LoadAliases(npub)
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

	default:
		color.Red("Unknown command: /%s", cmd)
		fmt.Println("Available: /follow /unfollow /dm /profile /switch /alias /aliases")
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

	renderPrompt(buf, showMenu, selected, prevMenuSize)

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
						renderPrompt(buf, showMenu, selected, prevMenuSize)
						prevMenuSize = 0
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

		newMenuSize := 0
		if showMenu {
			newMenuSize = len(cmds)
		}
		renderPrompt(buf, showMenu, selected, prevMenuSize)
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

func renderPrompt(buf []byte, showMenu bool, selected int, prevMenuSize int) {
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
		// Clear any leftover menu lines
		for i := 0; i < prevMenuSize; i++ {
			fmt.Print("\r\n\033[K")
		}
		if prevMenuSize > 0 {
			fmt.Printf("\033[%dA", prevMenuSize)
		}
	}

	// Position cursor at end of input on prompt line
	fmt.Printf("\r\033[%dC", len(promptPrefix)+len(buf))
}

func clearMenu(menuSize int) {
	if menuSize > 0 {
		for i := 0; i < menuSize; i++ {
			fmt.Print("\r\n\033[K")
		}
		fmt.Printf("\033[%dA", menuSize)
	}
}
