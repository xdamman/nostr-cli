package cmd

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip04"
	"github.com/nbd-wtf/go-nostr/nip19"
	"github.com/xdamman/nostr-cli/internal/cache"
	"github.com/xdamman/nostr-cli/internal/crypto"
	"github.com/xdamman/nostr-cli/internal/profile"
	internalRelay "github.com/xdamman/nostr-cli/internal/relay"
	"github.com/xdamman/nostr-cli/internal/ui"
)

// -- DM-specific Bubble Tea messages --

type dmEventMsg struct {
	Events []nostr.Event
}

type dmStatusMsg struct {
	Text string
}

type dmTargetNameMsg struct {
	Name string
}

// typingMsg signals the counterparty is typing.
type typingMsg struct{}

// typingTickMsg fires every second to expire stale typing state.
type typingTickMsg struct{}

// -- DM feed with decryption --

type dmFeedBT struct {
	entries      []nostr.Event
	seen         map[string]bool
	sharedSecret []byte
	skHex        string // for NIP-17 unwrapping
	maxSize      int
	// Cache unwrapped NIP-17 rumor content: eventID → {plaintext, senderPubHex}
	unwrapped map[string]unwrappedRumor
}

type unwrappedRumor struct {
	plaintext    string
	senderPubHex string
}

func newDMFeedBT(sharedSecret []byte, skHex string, maxSize int) dmFeedBT {
	return dmFeedBT{
		seen:         make(map[string]bool),
		sharedSecret: sharedSecret,
		skHex:        skHex,
		maxSize:      maxSize,
		unwrapped:    make(map[string]unwrappedRumor),
	}
}

func (f *dmFeedBT) addEvents(events []nostr.Event) int {
	added := 0
	for i := range events {
		if f.seen[events[i].ID] {
			continue
		}
		f.seen[events[i].ID] = true
		f.entries = append(f.entries, events[i])
		added++
	}
	if added > 0 {
		sort.SliceStable(f.entries, func(i, j int) bool {
			return f.entries[i].CreatedAt < f.entries[j].CreatedAt
		})
		if f.maxSize > 0 && len(f.entries) > f.maxSize {
			removed := f.entries[:len(f.entries)-f.maxSize]
			for _, ev := range removed {
				delete(f.seen, ev.ID)
			}
			f.entries = f.entries[len(f.entries)-f.maxSize:]
		}
	}
	return added
}

func (f *dmFeedBT) render(myHex, myName, targetName string, dimSent map[string]bool, termW int) []string {
	if len(f.entries) == 0 {
		return nil
	}

	nameWidth := len(myName)
	if len(targetName) > nameWidth {
		nameWidth = len(targetName)
	}
	prefixLen := 14 + 2 + nameWidth + 2

	var lines []string
	for _, ev := range f.entries {
		var plaintext string
		var senderPubHex string

		if ev.Kind == nostr.KindGiftWrap {
			// NIP-17 gift wrap — use cached unwrap
			if cached, ok := f.unwrapped[ev.ID]; ok {
				plaintext = cached.plaintext
				senderPubHex = cached.senderPubHex
			} else {
				rumor, err := crypto.UnwrapGiftWrapDM(ev, f.skHex)
				if err != nil {
					continue
				}
				plaintext = rumor.Content
				senderPubHex = rumor.PubKey
				f.unwrapped[ev.ID] = unwrappedRumor{plaintext: plaintext, senderPubHex: senderPubHex}
			}
		} else if ev.Kind == 14 {
			// NIP-17 decrypted rumor (from cache) — content is already plaintext
			plaintext = ev.Content
			senderPubHex = ev.PubKey
		} else {
			// NIP-04 legacy
			var err error
			plaintext, err = nip04.Decrypt(ev.Content, f.sharedSecret)
			if err != nil {
				continue
			}
			senderPubHex = ev.PubKey
		}

		ts := formatLocalTimestamp(time.Unix(int64(ev.CreatedAt), 0))

		// Wrap content
		avail := termW - prefixLen
		if avail < 20 {
			avail = 20
		}
		content := wrapNote(plaintext, prefixLen)

		isDim := dimSent != nil && dimSent[ev.ID]

		var name string
		if senderPubHex == myHex {
			name = myName
		} else {
			name = targetName
		}

		if isDim {
			line := dimStyle.Render(fmt.Sprintf("%s  %-*s: %s", ts, nameWidth, name, content))
			for _, l := range strings.Split(line, "\n") {
				lines = append(lines, l)
			}
		} else {
			tsStr := dimStyle.Render(ts + "  ")
			var nameStr string
			if senderPubHex == myHex {
				nameStr = greenStyle.Render(fmt.Sprintf("%-*s", nameWidth, name)) + ": "
			} else {
				nameStr = cyanStyle.Render(fmt.Sprintf("%-*s", nameWidth, name)) + ": "
			}
			full := tsStr + nameStr + content
			for _, l := range strings.Split(full, "\n") {
				lines = append(lines, l)
			}
		}
	}
	return lines
}

// -- DM Model --

type dmModel struct {
	feed    dmFeedBT
	dimSent map[string]bool // event IDs of unconfirmed sent messages
	status  string
	width      int
	height     int
	input      textinput.Model
	npub       string
	myHex      string
	myName     string
	skHex      string
	targetHex  string
	targetName string
	relays     []string
	quitting   bool
	useNip04   bool   // true = NIP-04, false = NIP-17
	protoLabel string // "NIP-17" or "NIP-04 — peer uses legacy protocol"

	// Mention autocomplete
	mentionCandidates []ui.MentionCandidate
	mentionResults    []ui.MentionCandidate
	mentionActive     bool
	mentionIdx        int
	mentionQuery      string
	selectedMentions  []ui.MentionCandidate

	// Typing indicators
	lastTypingSent  time.Time // throttle outgoing typing events
	counterTyping   bool      // is counterparty typing?
	counterTypingAt time.Time // when we last saw their typing event
}

// dmProgram holds the running tea.Program for DM mode.
var dmProgram *tea.Program

func newDMModel(npub, myHex, myName, skHex, targetHex, targetName string, relays []string, sharedSecret []byte) dmModel {
	ti := textinput.New()
	ti.Focus()
	ti.CharLimit = 0
	ti.Prompt = greenStyle.Render(myName) + "> "

	return dmModel{
		feed:       newDMFeedBT(sharedSecret, skHex, 200),
		dimSent:    make(map[string]bool),
		input:      ti,
		npub:       npub,
		myHex:      myHex,
		myName:     myName,
		skHex:      skHex,
		targetHex:  targetHex,
		targetName: targetName,
		relays:     relays,
	}
}

func (m dmModel) Init() tea.Cmd {
	return tea.Batch(textinput.Blink, typingTickCmd())
}

// typingTickCmd sends a typingTickMsg every second.
func typingTickCmd() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return typingTickMsg{}
	})
}

func (m dmModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.input.Width = msg.Width - 6
		return m, nil

	case tea.KeyMsg:
		return m.handleDMKey(msg)

	case dmEventMsg:
		m.feed.addEvents(msg.Events)
		return m, nil

	case dmStatusMsg:
		m.status = msg.Text
		return m, nil

	case dmTargetNameMsg:
		m.targetName = msg.Name
		return m, nil

	case typingMsg:
		m.counterTyping = true
		m.counterTypingAt = time.Now()
		return m, nil

	case typingTickMsg:
		if m.counterTyping && time.Since(m.counterTypingAt) > 5*time.Second {
			m.counterTyping = false
		}
		return m, typingTickCmd()

	case dmConfirmMsg:
		delete(m.dimSent, msg.EventID)
		return m, nil
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m dmModel) handleDMKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Handle mention autocomplete navigation first
	if m.mentionActive {
		switch msg.Type {
		case tea.KeyUp:
			if m.mentionIdx > 0 {
				m.mentionIdx--
			}
			return m, nil
		case tea.KeyDown:
			if m.mentionIdx < len(m.mentionResults)-1 {
				m.mentionIdx++
			}
			return m, nil
		case tea.KeyTab, tea.KeyEnter:
			if len(m.mentionResults) > 0 && m.mentionIdx < len(m.mentionResults) {
				m = m.confirmDMMention()
			}
			return m, nil
		case tea.KeyEscape:
			m.mentionActive = false
			m.mentionResults = nil
			return m, nil
		}
	}

	switch msg.Type {
	case tea.KeyCtrlC, tea.KeyCtrlD:
		m.quitting = true
		return m, tea.Quit

	case tea.KeyEnter:
		line := strings.TrimSpace(m.input.Value())
		m.input.Reset()
		m.mentionActive = false
		m.mentionResults = nil

		if line == "" {
			return m, nil
		}

		// Process mentions
		content := line
		var extraTags nostr.Tags
		if len(m.selectedMentions) > 0 {
			var mentionTags [][]string
			content, mentionTags = ui.ReplaceMentionsForEvent(line, m.selectedMentions)
			for _, tag := range mentionTags {
				extraTags = append(extraTags, nostr.Tag(tag))
			}
			m.selectedMentions = nil
		}

		if m.useNip04 {
			// Legacy NIP-04 encrypt + kind 4
			sharedSecret := generateSharedSecret(m.skHex, m.targetHex)
			ciphertext, err := nip04.Encrypt(content, sharedSecret)
			if err != nil {
				return m, nil
			}

			tags := nostr.Tags{nostr.Tag{"p", m.targetHex}}
			tags = append(tags, extraTags...)

			event := nostr.Event{
				PubKey:    m.myHex,
				CreatedAt: nostr.Now(),
				Kind:      nostr.KindEncryptedDirectMessage,
				Tags:      tags,
				Content:   ciphertext,
			}
			if err := event.Sign(m.skHex); err != nil {
				return m, nil
			}

			// Add to feed immediately (dim = unconfirmed)
			m.dimSent[event.ID] = true
			m.feed.addEvents([]nostr.Event{event})
			_ = cache.LogDMEvent(m.npub, m.targetHex, event)
			_ = cache.LogSentEvent(m.npub, event)

			// Publish in background
			return m, m.publishDMCmd(event)
		}

		// NIP-17 gift wrap (default)
		forRecipient, forSelf, err := crypto.CreateGiftWrapDM(content, m.skHex, m.myHex, m.targetHex)
		if err != nil {
			return m, nil
		}

		// Cache a synthetic kind 14 rumor with plaintext content
		rumor := nostr.Event{
			Kind:      14,
			Content:   content,
			PubKey:    m.myHex,
			CreatedAt: nostr.Now(),
			Tags:      nostr.Tags{{"p", m.targetHex}},
			ID:        forRecipient.ID,
		}
		_ = cache.LogDMEvent(m.npub, m.targetHex, rumor)

		// Add to feed immediately (dim = unconfirmed)
		m.dimSent[forRecipient.ID] = true
		m.feed.addEvents([]nostr.Event{rumor})

		// Publish both events in background
		return m, m.publishNip17Cmd(forRecipient, forSelf)
	}

	var cmds []tea.Cmd

	// Send typing indicator on keystroke (throttled)
	if msg.Type == tea.KeyRunes || msg.Type == tea.KeySpace {
		if time.Since(m.lastTypingSent) > 3*time.Second {
			m.lastTypingSent = time.Now()
			cmds = append(cmds, m.sendTypingCmd())
		}
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	if cmd != nil {
		cmds = append(cmds, cmd)
	}

	// Check mention trigger
	m.updateDMMentionState()

	return m, tea.Batch(cmds...)
}

// sendTypingCmd publishes an ephemeral typing indicator event.
func (m dmModel) sendTypingCmd() tea.Cmd {
	skHex := m.skHex
	myHex := m.myHex
	targetHex := m.targetHex
	relays := m.relays
	return func() tea.Msg {
		publishTypingIndicator(skHex, myHex, targetHex, relays)
		return nil
	}
}

// publishTypingIndicator sends an ephemeral kind 10003 typing event.
func publishTypingIndicator(skHex, myHex, targetHex string, relays []string) {
	event := nostr.Event{
		Kind:      10003,
		Content:   "",
		Tags:      nostr.Tags{{"p", targetHex}},
		CreatedAt: nostr.Now(),
		PubKey:    myHex,
	}
	event.Sign(skHex)
	ctx := context.Background()
	internalRelay.PublishEventQuiet(ctx, event, relays)
}

func (m dmModel) confirmDMMention() dmModel {
	selected := m.mentionResults[m.mentionIdx]
	val := m.input.Value()

	atIdx := strings.LastIndex(val, "@"+m.mentionQuery)
	if atIdx < 0 {
		atIdx = strings.LastIndex(val, "@")
	}
	if atIdx < 0 {
		m.mentionActive = false
		return m
	}

	before := val[:atIdx]
	after := val[atIdx+1+len(m.mentionQuery):]
	newVal := before + "@" + selected.DisplayName + after
	m.input.SetValue(newVal)
	m.input.SetCursor(len(before) + 1 + len(selected.DisplayName))

	m.selectedMentions = append(m.selectedMentions, selected)
	m.mentionActive = false
	m.mentionResults = nil
	return m
}

func (m *dmModel) updateDMMentionState() {
	if len(m.mentionCandidates) == 0 {
		return
	}
	val := m.input.Value()
	if val == "" {
		m.mentionActive = false
		return
	}

	cursor := m.input.Position()
	textBeforeCursor := val
	if cursor < len(val) {
		textBeforeCursor = val[:cursor]
	}

	atIdx := -1
	for i := len(textBeforeCursor) - 1; i >= 0; i-- {
		if textBeforeCursor[i] == ' ' {
			break
		}
		if textBeforeCursor[i] == '@' {
			if i == 0 || textBeforeCursor[i-1] == ' ' {
				atIdx = i
			}
			break
		}
	}

	if atIdx < 0 {
		m.mentionActive = false
		m.mentionResults = nil
		return
	}

	query := textBeforeCursor[atIdx+1:]
	m.mentionQuery = query
	results := ui.FilterCandidates(m.mentionCandidates, query)
	if len(results) == 0 {
		m.mentionActive = false
		m.mentionResults = nil
		return
	}
	m.mentionActive = true
	m.mentionResults = results
	m.mentionIdx = 0
}

func (m dmModel) publishDMCmd(event nostr.Event) tea.Cmd {
	npub := m.npub
	relays := m.relays
	eventID := event.ID
	return func() tea.Msg {
		total := len(relays)
		timeout := time.Duration(timeoutFlag) * time.Millisecond
		ch := internalRelay.PublishEventWithProgress(context.Background(), event, relays, timeout)

		confirmed := 0
		for res := range ch {
			if res.OK {
				confirmed++
			}
			if dmProgram != nil {
				dmProgram.Send(dmStatusMsg{Text: fmt.Sprintf("Sending... (%d/%d relays)", confirmed, total)})
			}
			// On first confirmation, mark as no longer dim
			if confirmed == 1 && dmProgram != nil {
				dmProgram.Send(dmConfirmMsg{EventID: eventID})
			}
		}
		_ = cache.LogSentEvent(npub, event)
		return dmStatusMsg{Text: ""}
	}
}

func (m dmModel) publishNip17Cmd(forRecipient, forSelf nostr.Event) tea.Cmd {
	npub := m.npub
	relays := m.relays
	return func() tea.Msg {
		total := len(relays)
		timeout := time.Duration(timeoutFlag) * time.Millisecond

		ch := internalRelay.PublishEventWithProgress(context.Background(), forRecipient, relays, timeout)
		ch2 := internalRelay.PublishEventWithProgress(context.Background(), forSelf, relays, timeout)

		recipientOK := make(map[string]bool)
		selfOK := make(map[string]bool)

		for res := range ch {
			if res.OK {
				recipientOK[res.URL] = true
			}
		}
		for res := range ch2 {
			if res.OK {
				selfOK[res.URL] = true
			}
		}

		successCount := 0
		for _, r := range relays {
			if recipientOK[r] && selfOK[r] {
				successCount++
			}
		}

		_ = cache.LogSentEvent(npub, forRecipient)
		_ = cache.LogSentEvent(npub, forSelf)

		return dmStatusMsg{Text: fmt.Sprintf("✓ Published to %d/%d relays (NIP-17)", successCount, total)}
	}
}

type dmConfirmMsg struct {
	EventID string
}

func (m dmModel) View() string {
	if m.quitting {
		return ""
	}
	if m.width == 0 || m.height == 0 {
		return "Loading..."
	}

	statusLine := m.renderDMStatus()
	mentionLines := m.renderDMMentionMenu()
	mentionHeight := len(mentionLines)

	feedHeight := m.height - 2 - mentionHeight // 1 for input, 1 for status
	if feedHeight < 1 {
		feedHeight = 1
	}

	rendered := m.feed.render(m.myHex, m.myName, m.targetName, m.dimSent, m.width)

	// Take last feedHeight lines
	feed := padFeed(rendered, feedHeight)

	var parts []string
	parts = append(parts, feed)
	parts = append(parts, m.input.View())
	if mentionHeight > 0 {
		parts = append(parts, strings.Join(mentionLines, "\n"))
	}
	parts = append(parts, statusLine)
	return strings.Join(parts, "\n")
}

func (m dmModel) renderDMMentionMenu() []string {
	if !m.mentionActive || len(m.mentionResults) == 0 {
		return nil
	}

	maxVisible := 7
	if len(m.mentionResults) < maxVisible {
		maxVisible = len(m.mentionResults)
	}

	start := 0
	if m.mentionIdx >= maxVisible {
		start = m.mentionIdx - maxVisible + 1
	}
	end := start + maxVisible
	if end > len(m.mentionResults) {
		end = len(m.mentionResults)
		start = end - maxVisible
		if start < 0 {
			start = 0
		}
	}

	var lines []string
	for i := start; i < end; i++ {
		c := m.mentionResults[i]
		entry := c.DisplayName + " (" + ui.TruncateNpub(c.Npub) + ")"
		if i == m.mentionIdx {
			lines = append(lines, "  "+cyanStyle.Render("→ "+entry))
		} else {
			lines = append(lines, "    "+dimStyle.Render(entry))
		}
	}
	return lines
}

func (m dmModel) renderDMStatus() string {
	if m.status != "" {
		return dimStyle.Render("  " + m.status)
	}
	if m.counterTyping {
		return dimStyle.Render("  " + m.targetName + " is typing...")
	}
	proto := m.protoLabel
	if proto == "" {
		proto = "NIP-17"
	}
	hint := fmt.Sprintf("Connected to %s (%s) · %d relays · ctrl+c to exit", m.targetName, proto, len(m.relays))
	return dimStyle.Render("  " + hint)
}

// padFeed takes rendered lines and returns a string of exactly `height` lines.
func padFeed(rendered []string, height int) string {
	if len(rendered) == 0 {
		lines := make([]string, height)
		for i := range lines {
			lines[i] = ""
		}
		return strings.Join(lines, "\n")
	}

	start := 0
	if len(rendered) > height {
		start = len(rendered) - height
	}
	visible := rendered[start:]

	padding := height - len(visible)
	lines := make([]string, 0, height)
	for i := 0; i < padding; i++ {
		lines = append(lines, "")
	}
	lines = append(lines, visible...)
	return strings.Join(lines, "\n")
}

// interactiveDMBubbleTea runs the DM chat using Bubble Tea.
func interactiveDMBubbleTea(npub, skHex, myHex, targetHex, inputName string, relays []string) error {
	cache.LoadProfileCache(npub)

	// Resolve own name
	myName := resolveProfileName(npub)
	if myName == "" {
		myName = myHex[:8] + "..."
	}

	// Resolve target name
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

	sharedSecret := generateSharedSecret(skHex, targetHex)

	m := newDMModel(npub, myHex, myName, skHex, targetHex, targetName, relays, sharedSecret)

	// Load mention candidates for @ autocomplete
	m.mentionCandidates = ui.LoadMentionCandidates(npub)

	// Load cached DM history into model
	storedEvents, _ := cache.QueryDMEvents(npub, targetHex)
	if len(storedEvents) > 0 {
		m.feed.addEvents(storedEvents)
	}

	// Auto-detect DM protocol from history
	if dmNip04Flag {
		// Explicit --nip04 flag overrides everything
		m.useNip04 = true
		m.protoLabel = "NIP-04"
	} else {
		// Check last received message's protocol
		detectedNip04 := detectDMProtocol(storedEvents, myHex, targetHex, skHex)
		m.useNip04 = detectedNip04
		if detectedNip04 {
			m.protoLabel = "NIP-04 — peer uses legacy protocol"
		} else {
			m.protoLabel = "NIP-17"
		}
	}

	p := tea.NewProgram(m, tea.WithAltScreen())
	dmProgram = p

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Fetch DM history from relays immediately
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
		// Also fetch NIP-17 gift wraps addressed to me
		filter3 := nostr.Filter{
			Kinds: []int{nostr.KindGiftWrap},
			Tags:  nostr.TagMap{"p": []string{myHex}},
			Since: &since,
			Limit: 50,
		}
		fetchCtx, fetchCancel := context.WithTimeout(ctx, 10*time.Second)
		defer fetchCancel()

		events1, _ := internalRelay.FetchEvents(fetchCtx, filter1, relays)
		events2, _ := internalRelay.FetchEvents(fetchCtx, filter2, relays)
		events3, _ := internalRelay.FetchEvents(fetchCtx, filter3, relays)

		var all []nostr.Event
		for _, evp := range append(append(events1, events2...), events3...) {
			if evp != nil {
				// For gift wraps, filter to only show DMs involving the target
				if evp.Kind == nostr.KindGiftWrap {
					rumor, err := crypto.UnwrapGiftWrapDM(*evp, skHex)
					if err != nil || rumor.Kind != 14 {
						continue
					}
					if rumor.PubKey != targetHex && rumor.PubKey != myHex {
						continue
					}
					_ = cache.LogDMEvent(npub, targetHex, rumor)
				} else {
					_ = cache.LogDMEvent(npub, targetHex, *evp)
				}
				all = append(all, *evp)
			}
		}
		if len(all) > 0 {
			p.Send(dmEventMsg{Events: all})
		}
	}()

	// Resolve target name from relays in background
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
		if newName != "" && newName != targetName {
			p.Send(dmTargetNameMsg{Name: newName})
		}
	}()

	// Subscribe for real-time incoming DMs
	var subSeenMu sync.Mutex
	subSeen := make(map[string]bool)
	// Seed from loaded events
	for _, ev := range storedEvents {
		subSeen[ev.ID] = true
	}

	for _, url := range relays {
		go func(url string) {
			connectCtx, connectCancel := context.WithTimeout(ctx, internalRelay.ConnectTimeout)
			defer connectCancel()

			relay, err := nostr.RelayConnect(connectCtx, url)
			if err != nil {
				return
			}
			defer relay.Close()

			since := nostr.Now()

			// Subscribe to both NIP-04 DMs and NIP-17 gift wraps
			merged := make(chan *nostr.Event, 20)
			var subsActive sync.WaitGroup

			// NIP-04: incoming from target
			nip04Sub, err := relay.Subscribe(ctx, nostr.Filters{{
				Kinds:   []int{nostr.KindEncryptedDirectMessage},
				Authors: []string{targetHex},
				Tags:    nostr.TagMap{"p": []string{myHex}},
				Since:   &since,
			}})
			if err == nil {
				subsActive.Add(1)
				go func() {
					defer subsActive.Done()
					defer nip04Sub.Unsub()
					for ev := range nip04Sub.Events {
						merged <- ev
					}
				}()
			}

			// NIP-17: gift wraps addressed to me
			gwSub, err := relay.Subscribe(ctx, nostr.Filters{{
				Kinds: []int{nostr.KindGiftWrap},
				Tags:  nostr.TagMap{"p": []string{myHex}},
				Since: &since,
			}})
			if err == nil {
				subsActive.Add(1)
				go func() {
					defer subsActive.Done()
					defer gwSub.Unsub()
					for ev := range gwSub.Events {
						merged <- ev
					}
				}()
			}

			// Typing indicators from target (ephemeral kind 10003)
			typingSub, err := relay.Subscribe(ctx, nostr.Filters{{
				Kinds:   []int{10003},
				Authors: []string{targetHex},
				Tags:    nostr.TagMap{"p": []string{myHex}},
				Since:   &since,
			}})
			if err == nil {
				subsActive.Add(1)
				go func() {
					defer subsActive.Done()
					defer typingSub.Unsub()
					for ev := range typingSub.Events {
						// Don't merge into the DM event stream — send typing msg directly
						_ = ev
						p.Send(typingMsg{})
					}
				}()
			}

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
					subSeenMu.Lock()
					if subSeen[ev.ID] {
						subSeenMu.Unlock()
						continue
					}
					subSeen[ev.ID] = true
					subSeenMu.Unlock()

					// Filter NIP-17 events to only show DMs involving the target
					if ev.Kind == nostr.KindGiftWrap {
						rumor, err := crypto.UnwrapGiftWrapDM(*ev, skHex)
						if err != nil || rumor.Kind != 14 {
							continue
						}
						if rumor.PubKey != targetHex && rumor.PubKey != myHex {
							continue
						}
						_ = cache.LogDMEvent(npub, targetHex, rumor)
					} else {
						_ = cache.LogDMEvent(npub, targetHex, *ev)
					}

					p.Send(dmEventMsg{Events: []nostr.Event{*ev}})
				}
			}
		}(url)
	}

	if _, err := p.Run(); err != nil {
		return err
	}
	cancel()
	dmProgram = nil
	return nil
}

// detectDMProtocol examines stored DM events and returns true if the last
// received message from the target used NIP-04 (kind 4). Returns false (NIP-17)
// if the last received message is a gift wrap (kind 1059) or if there's no history.
func detectDMProtocol(events []nostr.Event, myHex, targetHex, skHex string) bool {
	// Sort by CreatedAt descending to find the most recent
	type scored struct {
		kind      int
		createdAt nostr.Timestamp
	}
	var candidates []scored

	for _, ev := range events {
		if ev.Kind == nostr.KindEncryptedDirectMessage {
			// NIP-04: check if it's from the target
			if ev.PubKey == targetHex {
				candidates = append(candidates, scored{kind: 4, createdAt: ev.CreatedAt})
			}
		} else if ev.Kind == nostr.KindGiftWrap {
			// NIP-17: unwrap to check sender
			rumor, err := crypto.UnwrapGiftWrapDM(ev, skHex)
			if err != nil {
				continue
			}
			if rumor.PubKey == targetHex {
				candidates = append(candidates, scored{kind: 1059, createdAt: ev.CreatedAt})
			}
		}
	}

	if len(candidates) == 0 {
		return false // No history → default to NIP-17
	}

	// Find the most recent received message
	latest := candidates[0]
	for _, c := range candidates[1:] {
		if c.createdAt > latest.createdAt {
			latest = c
		}
	}

	return latest.kind == 4 // NIP-04
}
