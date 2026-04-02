package cmd

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/knz/bubbline/editline"
	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip04"
	"github.com/nbd-wtf/go-nostr/nip19"
	"github.com/xdamman/nostr-cli/internal/cache"
	"github.com/xdamman/nostr-cli/internal/config"
	"github.com/xdamman/nostr-cli/internal/crypto"
	"github.com/xdamman/nostr-cli/internal/profile"
	internalRelay "github.com/xdamman/nostr-cli/internal/relay"
	"github.com/xdamman/nostr-cli/internal/ui"
)

// KindTypingIndicator is the event kind for typing indicators in DMs.
// It uses the ephemeral range (20000-29999) per NIP-01, meaning relays
// MUST NOT store these events — they only forward them to connected subscribers.
// In NIP-04 mode, sent as a plain ephemeral event with a "p" tag.
// In NIP-17 mode, the kind 20003 rumor is gift-wrapped so the relay only sees
// a random outer pubkey + recipient — no sender leak.
const KindTypingIndicator = 20003

// -- DM-specific Bubble Tea messages --

type dmEventMsg struct {
	Events []nostr.Event
}

type dmStatusMsg struct {
	Text string
}

// dmCmdOutputMsg carries multiline output from a slash command to display in the feed area.
type dmCmdOutputMsg struct {
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
	input      *editline.Model
	npub       string
	myHex      string
	myName     string
	skHex      string
	targetHex  string
	targetName string
	relays       []string
	quitting     bool
	backToPicker bool   // true when user pressed backspace on empty input to go back
	useNip04     bool   // true = NIP-04, false = NIP-17
	protoLabel   string // "NIP-17" or "NIP-04 — peer uses legacy protocol"

	// Mention candidates (used for p-tag extraction at send time)
	mentionCandidates []ui.MentionCandidate

	// Slash command menu
	showMenu  bool
	menuSel   int
	cmdOutput string // multiline output from a slash command (shown in feed area, dismissed on keystroke)

	// Typing indicators
	lastTypingSent  time.Time // throttle outgoing typing events
	counterTyping   bool      // is counterparty typing?
	counterTypingAt time.Time // when we last saw their typing event
}

// dmProgram holds the running tea.Program for DM mode.
var dmProgram *tea.Program

func newDMModel(npub, myHex, myName, skHex, targetHex, targetName string, relays []string, sharedSecret []byte) dmModel {
	ed := editline.New(0, 0)
	// Use plain-text prompt so bubbline calculates correct padding.
	// Colorize in View() post-processing.
	ed.Prompt = myName + ">" + targetName + "> "
	ed.NextPrompt = strings.Repeat(" ", len(myName)+1+len(targetName)+2)

	// Disable the help bar below the input
	ed.KeyMap.MoreHelp.SetEnabled(false)

	// Shift+Enter (Alt+Enter in terminals) inserts newline; disable Ctrl+O
	ed.KeyMap.AlwaysNewline = key.NewBinding(key.WithKeys("alt+enter", "alt+\r"))
	ed.KeyMap.AlwaysComplete.SetEnabled(false)

	// Wire @ mention autocomplete (via Tab key)
	// Candidates are set later in interactiveDMBubbleTea after loading.
	ed.MaxHeight = 5
	ed.CharLimit = 0
	ed.ShowLineNumbers = false
	ed.Placeholder = ""
	// Enter always sends (single-line default); Ctrl+O for newline
	ed.CheckInputComplete = func(entireInput [][]rune, line, col int) bool {
		return true // Enter always submits
	}

	return dmModel{
		feed:       newDMFeedBT(sharedSecret, skHex, 200),
		dimSent:    make(map[string]bool),
		input:      ed,
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
	return tea.Batch(typingTickCmd(), m.input.Focus())
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
		m.input.SetSize(msg.Width, msg.Height)
		return m, nil

	case editline.InputCompleteMsg:
		return m.handleSend()

	case tea.KeyMsg:
		return m.handleDMKey(msg)

	case dmEventMsg:
		m.feed.addEvents(msg.Events)
		return m, nil

	case dmStatusMsg:
		m.status = msg.Text
		return m, nil

	case dmCmdOutputMsg:
		m.cmdOutput = msg.Text
		m.status = "press any key to dismiss"
		return m, nil

	case typeStringMsg:
		s := string(msg)
		if len(s) == 0 {
			return m, nil
		}
		r := []rune(s)
		keyMsg := tea.KeyMsg{Type: tea.KeyRunes, Runes: r[:1]}
		_, cmd := m.input.Update(keyMsg)
		var cmds []tea.Cmd
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
		if len(r) > 1 {
			cmds = append(cmds, typeString(string(r[1:])))
		}
		return m, tea.Batch(cmds...)

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

	_, cmd := m.input.Update(msg)
	return m, cmd
}

func (m dmModel) handleDMKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Dismiss command output on any keystroke
	if m.cmdOutput != "" {
		m.cmdOutput = ""
		m.status = ""
		return m, nil
	}

	// Ctrl+C: bubbline clears input if non-empty, or sends ErrInterrupted if empty.
	// We intercept Ctrl+C on empty input to quit.
	if msg.Type == tea.KeyCtrlC {
		if strings.TrimSpace(m.input.Value()) == "" {
			m.quitting = true
			return m, tea.Quit
		}
		// Let bubbline handle clearing
	}
	// Ctrl+D quits
	if msg.Type == tea.KeyCtrlD {
		m.quitting = true
		return m, tea.Quit
	}

	// Backspace on empty input → go back to picker
	if (msg.Type == tea.KeyBackspace || msg.Type == tea.KeyDelete) && strings.TrimSpace(m.input.Value()) == "" {
		m.backToPicker = true
		return m, tea.Quit
	}

	// Slash menu navigation
	if m.showMenu {
		switch msg.Type {
		case tea.KeyUp:
			if m.menuSel > 0 {
				m.menuSel--
			}
			return m, nil
		case tea.KeyDown:
			cmds := filterCommands([]byte(m.input.Value()), dmSlashCommands)
			if m.menuSel < len(cmds)-1 {
				m.menuSel++
			}
			return m, nil
		case tea.KeyTab:
			// Autocomplete with selected command
			cmds := filterCommands([]byte(m.input.Value()), dmSlashCommands)
			if m.menuSel >= 0 && m.menuSel < len(cmds) {
				replacement := "/" + cmds[m.menuSel].name + " "
				m.input.Reset()
				m.showMenu = false
				return m, typeString(replacement)
			}
			return m, nil
		case tea.KeyEscape:
			m.showMenu = false
			return m, nil
		}
	}

	var cmds []tea.Cmd

	// Send typing indicator on keystroke (throttled every 5s)
	if msg.Type == tea.KeyRunes || msg.Type == tea.KeySpace {
		if time.Since(m.lastTypingSent) > 5*time.Second {
			m.lastTypingSent = time.Now()
			cmds = append(cmds, m.sendTypingCmd())
		}
	}

	// Forward to editline (handles multiline, history, autocomplete, Enter→InputCompleteMsg)
	_, cmd := m.input.Update(msg)
	if cmd != nil {
		cmds = append(cmds, cmd)
	}

	// Check if we should show/hide slash menu
	val := m.input.Value()
	if len(val) > 0 && val[0] == '/' && !strings.Contains(val, " ") {
		filtered := filterCommands([]byte(val), dmSlashCommands)
		m.showMenu = len(filtered) > 0
		if m.menuSel >= len(filtered) {
			m.menuSel = 0
		}
	} else {
		m.showMenu = false
	}

	// Auto-trigger autocomplete when typing inside an @mention
	if (msg.Type == tea.KeyRunes || msg.Type == tea.KeySpace) && len(m.mentionCandidates) > 0 {
		if isInMentionContext(m.input.Value()) {
			cmds = append(cmds, autoTriggerTab())
		}
	}

	return m, tea.Batch(cmds...)
}

// handleSend is called when editline signals InputCompleteMsg (Enter pressed).
func (m dmModel) handleSend() (tea.Model, tea.Cmd) {
	line := strings.TrimSpace(m.input.Value())

	// Slash menu selection: if menu is visible, use selected command
	if m.showMenu {
		cmds := filterCommands([]byte(line), dmSlashCommands)
		if m.menuSel >= 0 && m.menuSel < len(cmds) {
			line = "/" + cmds[m.menuSel].name
		}
	}
	m.showMenu = false

	// Add to history before resetting
	if line != "" {
		m.input.AddHistoryEntry(line)
	}

	m.input.Reset()

	if line == "" {
		return m, m.input.Focus()
	}

	// Handle /protocol command to toggle between NIP-04 and NIP-17
	if line == "/protocol" {
		m.useNip04 = !m.useNip04
		if m.useNip04 {
			m.protoLabel = "NIP-04"
		} else {
			m.protoLabel = "NIP-17"
		}
		return m, m.input.Focus()
	}

	// Handle /alias <name> — create alias for current conversation partner
	if line == "/alias" || strings.HasPrefix(line, "/alias ") {
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			aliasName := parts[1]
			targetNpub, _ := nip19.EncodePublicKey(m.targetHex)
			if err := config.SetAlias(m.npub, aliasName, targetNpub); err != nil {
				m.status = fmt.Sprintf("alias error: %v", err)
			} else {
				m.status = fmt.Sprintf("alias '%s' created for %s", aliasName, m.targetName)
			}
		} else {
			m.status = "usage: /alias <name>"
		}
		return m, m.input.Focus()
	}

	// Handle /dm — go back to conversation picker
	if line == "/dm" || strings.HasPrefix(line, "/dm ") {
		m.backToPicker = true
		return m, tea.Quit
	}

	// Delegate other slash commands to the shared command executor
	if strings.HasPrefix(line, "/") {
		return m, tea.Batch(m.makeDMSlashCmd(line), m.input.Focus())
	}

	// Extract mentions from text (bubbline autocomplete inserts @DisplayName)
	content := line
	var extraTags nostr.Tags
	mentions := ui.ExtractMentionsFromText(line, m.mentionCandidates)
	if len(mentions) > 0 {
		var mentionTags [][]string
		content, mentionTags = ui.ReplaceMentionsForEvent(line, mentions)
		for _, tag := range mentionTags {
			extraTags = append(extraTags, nostr.Tag(tag))
		}
	}

	if m.useNip04 {
		// Legacy NIP-04 encrypt + kind 4
		sharedSecret := generateSharedSecret(m.skHex, m.targetHex)
		ciphertext, err := nip04.Encrypt(content, sharedSecret)
		if err != nil {
			return m, m.input.Focus()
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
			return m, m.input.Focus()
		}

		// Add to feed immediately (dim = unconfirmed)
		m.dimSent[event.ID] = true
		m.feed.addEvents([]nostr.Event{event})
		_ = cache.LogDMEvent(m.npub, m.targetHex, event)
		_ = cache.LogSentEvent(m.npub, event)

		// Publish in background
		return m, tea.Batch(m.publishDMCmd(event), m.input.Focus())
	}

	// NIP-17 gift wrap (default)
	forRecipient, forSelf, err := crypto.CreateGiftWrapDM(content, m.skHex, m.myHex, m.targetHex)
	if err != nil {
		return m, m.input.Focus()
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
	return m, tea.Batch(m.publishNip17Cmd(forRecipient, forSelf), m.input.Focus())
}

// isInMentionContext checks if the cursor (assumed at end) is inside an @word.
func isInMentionContext(text string) bool {
	if text == "" {
		return false
	}
	// Walk backwards from end to find @ preceded by space/newline/start
	for i := len(text) - 1; i >= 0; i-- {
		c := text[i]
		if c == ' ' || c == '\n' || c == '\t' {
			return false
		}
		if c == '@' {
			return i == 0 || text[i-1] == ' ' || text[i-1] == '\n' || text[i-1] == '\t'
		}
	}
	return false
}

// autoTriggerTab sends a synthetic Tab key to trigger bubbline's autocomplete.
func autoTriggerTab() tea.Cmd {
	return func() tea.Msg {
		return tea.KeyMsg{Type: tea.KeyTab}
	}
}

// typeString sends a sequence of synthetic key presses to type a string into editline.
func typeString(s string) tea.Cmd {
	return func() tea.Msg {
		return typeStringMsg(s)
	}
}

// typeStringMsg is processed one character at a time by Update.
type typeStringMsg string

// sendTypingCmd publishes an ephemeral typing indicator event.
func (m dmModel) sendTypingCmd() tea.Cmd {
	skHex := m.skHex
	myHex := m.myHex
	targetHex := m.targetHex
	relays := m.relays
	useNip04 := m.useNip04
	return func() tea.Msg {
		publishTypingIndicator(skHex, myHex, targetHex, relays, useNip04)
		return nil
	}
}

// publishTypingIndicator sends an ephemeral typing indicator event (kind 20003).
// Kind 20003 is in the ephemeral range (20000-29999) per NIP-01, so relays
// MUST NOT store it — they only forward it to connected subscribers.
// In NIP-04 mode it publishes a plain ephemeral event.
// In NIP-17 mode it gift-wraps the kind 20003 rumor so the relay
// only sees random pubkey + recipient — no sender leak.
func publishTypingIndicator(skHex, myHex, targetHex string, relays []string, useNip04 bool) {
	ctx := context.Background()

	if useNip04 {
		event := nostr.Event{
			Kind:      KindTypingIndicator,
			Content:   "",
			Tags:      nostr.Tags{{"p", targetHex}},
			CreatedAt: nostr.Now(),
			PubKey:    myHex,
		}
		event.Sign(skHex)
		internalRelay.PublishEventQuiet(ctx, event, relays)
		return
	}

	// NIP-17: gift-wrap the typing indicator
	rumor := nostr.Event{
		Kind:      KindTypingIndicator,
		Content:   "",
		Tags:      nostr.Tags{{"p", targetHex}},
		CreatedAt: nostr.Now(),
		PubKey:    myHex,
	}
	wrapped, err := crypto.CreateGiftWrapEvent(rumor, skHex, targetHex)
	if err != nil {
		return
	}
	internalRelay.PublishEventQuiet(ctx, wrapped, relays)
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
	eventID := forRecipient.ID
	return func() tea.Msg {
		total := len(relays)
		timeout := time.Duration(timeoutFlag) * time.Millisecond

		ch := internalRelay.PublishEventWithProgress(context.Background(), forRecipient, relays, timeout)
		ch2 := internalRelay.PublishEventWithProgress(context.Background(), forSelf, relays, timeout)

		recipientOK := make(map[string]bool)
		selfOK := make(map[string]bool)
		confirmed := false

		for res := range ch {
			if res.OK {
				recipientOK[res.URL] = true
				// Un-dim on first relay confirmation
				if !confirmed && dmProgram != nil {
					confirmed = true
					dmProgram.Send(dmConfirmMsg{EventID: eventID})
				}
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
	menuLines := m.renderDMMenu()
	menuHeight := len(menuLines)

	// Editline renders its own view including prompt and a trailing help line.
	// Strip the help/search line (last line after the final \n).
	inputView := m.input.View()
	if idx := strings.LastIndex(inputView, "\n"); idx >= 0 {
		inputView = inputView[:idx]
	}
	// Colorize the plain-text prompt (first occurrence on first line)
	plainPrompt := m.myName + ">" + m.targetName + "> "
	colorPrompt := greenStyle.Render(m.myName) + ">" + pickerCyanStyle.Render(m.targetName) + "> "
	inputView = strings.Replace(inputView, plainPrompt, colorPrompt, 1)
	inputHeight := strings.Count(inputView, "\n") + 1

	feedHeight := m.height - 1 - inputHeight - menuHeight // 1 for status
	if feedHeight < 1 {
		feedHeight = 1
	}

	var feed string
	if m.cmdOutput != "" {
		// Show command output in the feed area
		outputLines := strings.Split(m.cmdOutput, "\n")
		feed = padFeed(outputLines, feedHeight)
	} else {
		rendered := m.feed.render(m.myHex, m.myName, m.targetName, m.dimSent, m.width)
		feed = padFeed(rendered, feedHeight)
	}

	var parts []string
	parts = append(parts, feed)
	parts = append(parts, inputView)
	parts = append(parts, menuLines...)
	parts = append(parts, statusLine)
	return strings.Join(parts, "\n")
}

// renderDMMenu renders the slash command autocomplete menu for DMs.
func (m dmModel) renderDMMenu() []string {
	if !m.showMenu {
		return nil
	}
	val := m.input.Value()
	cmds := filterCommands([]byte(val), dmSlashCommands)
	if len(cmds) == 0 {
		return nil
	}

	var lines []string
	for i, cmd := range cmds {
		if i == m.menuSel {
			line := "  " + cyanStyle.Render("> "+cmd.name) + "  " + dimStyle.Render(cmd.desc)
			lines = append(lines, line)
		} else {
			line := "    " + dimStyle.Render(cmd.name+"  "+cmd.desc)
			lines = append(lines, line)
		}
	}
	return lines
}

// makeDMSlashCmd runs a slash command (shared with the shell) in the background
// and sends the output as a dmCmdOutputMsg (shown in feed area, dismissed on keystroke).
func (m dmModel) makeDMSlashCmd(line string) tea.Cmd {
	npub := m.npub
	myHex := m.myHex
	relays := m.relays
	return func() tea.Msg {
		output := captureOutput(func() {
			statusCh := make(chan string, 16)
			go func() {
				for s := range statusCh {
					if dmProgram != nil {
						dmProgram.Send(dmStatusMsg{Text: s})
					}
				}
			}()
			executeSlashCommand(npub, myHex, line, relays, statusCh)
			close(statusCh)
		})
		output = strings.TrimRight(output, "\n")
		if output != "" {
			return dmCmdOutputMsg{Text: output}
		}
		return dmStatusMsg{Text: ""}
	}
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
	// Show contextual hint depending on whether input has text
	if strings.TrimSpace(m.input.Value()) != "" {
		hint := fmt.Sprintf("enter to send to %s over %d relays (%s) · ctrl+c to clear", m.targetName, len(m.relays), proto)
		return dimStyle.Render("  " + hint)
	}
	hint := fmt.Sprintf("%s · %d relays · %s · type / for commands · backspace to go back", m.targetName, len(m.relays), proto)
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

	// Load mention candidates for @ autocomplete (via Tab in bubbline)
	m.mentionCandidates = ui.LoadMentionCandidates(npub)
	if len(m.mentionCandidates) > 0 {
		m.input.AutoComplete = ui.MentionAutoComplete(m.mentionCandidates)
	}

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
					if err != nil {
						continue
					}
					// Skip typing indicators from history
					if rumor.Kind == KindTypingIndicator {
						continue
					}
					if rumor.Kind != 14 {
						continue
					}
					// Filter: only messages between me and the target
					if rumor.PubKey == targetHex {
						// Incoming from target — OK
					} else if rumor.PubKey == myHex {
						// Sent by me — verify recipient is the target
						if rumor.Tags.GetFirst([]string{"p", targetHex}) == nil {
							continue
						}
					} else {
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

			// Typing indicators from target (ephemeral kind 20003)
			typingSub, err := relay.Subscribe(ctx, nostr.Filters{{
				Kinds:   []int{KindTypingIndicator},
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
						if err != nil {
							continue
						}
						// Gift-wrapped typing indicator
						if rumor.Kind == KindTypingIndicator && rumor.PubKey == targetHex {
							p.Send(typingMsg{})
							continue
						}
						if rumor.Kind != 14 {
							continue
						}
						// Filter: only messages between me and the target
						if rumor.PubKey == targetHex {
							// Incoming from target — OK
						} else if rumor.PubKey == myHex {
							// Sent by me — verify recipient is the target
							if rumor.Tags.GetFirst([]string{"p", targetHex}) == nil {
								continue
							}
						} else {
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

	finalModel, err := p.Run()
	cancel()
	dmProgram = nil
	if err != nil {
		return err
	}
	if result, ok := finalModel.(dmModel); ok && result.backToPicker {
		return errBackToPicker
	}
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
