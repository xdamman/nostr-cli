package cmd

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
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
	if meta, _ := profile.LoadCached(npub); meta != nil {
		if meta.Name != "" {
			shellPromptName = meta.Name
		} else if meta.DisplayName != "" {
			shellPromptName = meta.DisplayName
		}
	}

	// Preload feed seen set so FeedSeenID works without re-reading disk
	cache.LoadFeedSeen(npub)

	// Start background profile fetcher
	startProfileFetcher(npub, relays)

	// Load secret key for signing events
	nsec, err := config.LoadNsec(npub)
	if err != nil {
		return err
	}
	skHex, err := crypto.NsecToHex(nsec)
	if err != nil {
		return err
	}

	// Create the Bubble Tea model
	m := newShellModel(npub, myHex, skHex, relays, shellPromptName)

	// Load cached feed into model
	cachedEvents, _ := cache.LoadFeed(npub, 20)
	for _, ev := range cachedEvents {
		queueProfileFetch(ev.PubKey)
		line := sprintFeedEvent(ev, myHex, shellPromptName, 80)
		m.feedLines = append(m.feedLines, line)
	}

	// Create the Bubble Tea program with alt screen
	p := tea.NewProgram(m, tea.WithAltScreen())
	shellProgram = p

	shellCtx, shellCancel := context.WithCancel(context.Background())
	defer shellCancel()

	// Shared followed hexes (updated when contact list arrives)
	var followMu sync.Mutex
	var followedHexes []string
	followReady := make(chan struct{})

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

		_ = cache.SaveFollowing(npub, hexes)

		for _, hex := range hexes {
			queueProfileFetch(hex)
		}

		p.Send(followReadyMsg{Hexes: hexes})

		if len(hexes) == 0 {
			if len(cachedEvents) == 0 {
				p.Send(infoMsg{Text: "You're not following anyone yet.\n  Use /follow <npub|alias|nip05> to follow someone."})
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

		var newEvents []nostr.Event
		for _, ev := range fetched {
			if !cache.FeedSeenID(npub, ev.ID) {
				newEvents = append(newEvents, *ev)
			}
		}

		cache.LogFeedEvents(npub, fetched)

		if len(newEvents) == 0 {
			return
		}

		sort.Slice(newEvents, func(i, j int) bool {
			return newEvents[i].CreatedAt < newEvents[j].CreatedAt
		})

		for _, ev := range newEvents {
			queueProfileFetch(ev.PubKey)
		}

		p.Send(batchEventsMsg{Events: newEvents})
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
					if !cache.FeedSeenID(npub, ev.ID) {
						_ = cache.LogFeedEvent(npub, *ev)
						queueProfileFetch(ev.PubKey)
						p.Send(newEventMsg{Event: *ev})
					}
				}
			}
		}(url)
	}

	// Run the Bubble Tea program (blocks until quit)
	if _, err := p.Run(); err != nil {
		return err
	}
	shellCancel()
	return nil
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
		// List available profiles
		cyanFn := color.New(color.FgCyan).SprintFunc()
		dimFn := color.New(color.Faint).SprintFunc()
		fmt.Println("Available profiles (use /switch <name> to switch):")
		for _, e := range entries {
			active := ""
			if e.npub == npub {
				active = " (active)"
			}
			if e.name != "" {
				short := e.npub
				if len(short) > 20 {
					short = short[:20] + "..."
				}
				fmt.Printf("  %s %s%s\n", cyanFn(e.name), dimFn(short), active)
			} else {
				fmt.Printf("  %s%s\n", e.npub, active)
			}
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

// filterCommands returns slash commands matching the current input prefix.
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
