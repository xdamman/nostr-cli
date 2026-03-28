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

// -- DM feed with decryption --

type dmFeedBT struct {
	entries      []nostr.Event
	seen         map[string]bool
	sharedSecret []byte
	maxSize      int
}

func newDMFeedBT(sharedSecret []byte, maxSize int) dmFeedBT {
	return dmFeedBT{
		seen:         make(map[string]bool),
		sharedSecret: sharedSecret,
		maxSize:      maxSize,
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
		plaintext, err := nip04.Decrypt(ev.Content, f.sharedSecret)
		if err != nil {
			continue
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
		if ev.PubKey == myHex {
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
			if ev.PubKey == myHex {
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

	// Mention autocomplete
	mentionCandidates []ui.MentionCandidate
	mentionResults    []ui.MentionCandidate
	mentionActive     bool
	mentionIdx        int
	mentionQuery      string
	selectedMentions  []ui.MentionCandidate
}

// dmProgram holds the running tea.Program for DM mode.
var dmProgram *tea.Program

func newDMModel(npub, myHex, myName, skHex, targetHex, targetName string, relays []string, sharedSecret []byte) dmModel {
	ti := textinput.New()
	ti.Focus()
	ti.CharLimit = 0
	ti.Prompt = greenStyle.Render(myName) + "> "

	return dmModel{
		feed:       newDMFeedBT(sharedSecret, 200),
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
	return textinput.Blink
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

		// Encrypt and sign
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

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)

	// Check mention trigger
	m.updateDMMentionState()

	return m, cmd
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
	hint := fmt.Sprintf("enter to send encrypted message to %s over %d relays, ctrl+c to exit", m.targetName, len(m.relays))
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
		fetchCtx, fetchCancel := context.WithTimeout(ctx, 10*time.Second)
		defer fetchCancel()

		events1, _ := internalRelay.FetchEvents(fetchCtx, filter1, relays)
		events2, _ := internalRelay.FetchEvents(fetchCtx, filter2, relays)

		var all []nostr.Event
		for _, evp := range append(events1, events2...) {
			if evp != nil {
				// Store each event to disk
				_ = cache.LogDMEvent(npub, targetHex, *evp)
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
			sub, err := relay.Subscribe(ctx, nostr.Filters{
				{
					Kinds:   []int{nostr.KindEncryptedDirectMessage},
					Authors: []string{targetHex},
					Tags:    nostr.TagMap{"p": []string{myHex}},
					Since:   &since,
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
					subSeenMu.Lock()
					if subSeen[ev.ID] {
						subSeenMu.Unlock()
						continue
					}
					subSeen[ev.ID] = true
					subSeenMu.Unlock()

					_ = cache.LogDMEvent(npub, targetHex, *ev)
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
