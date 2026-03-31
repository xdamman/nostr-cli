package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/fatih/color"
	"github.com/knz/bubbline/editline"
	"github.com/nbd-wtf/go-nostr"
	"github.com/xdamman/nostr-cli/internal/cache"
	"github.com/xdamman/nostr-cli/internal/config"
	"github.com/xdamman/nostr-cli/internal/crypto"
	internalRelay "github.com/xdamman/nostr-cli/internal/relay"
	"github.com/xdamman/nostr-cli/internal/resolve"
	"github.com/xdamman/nostr-cli/internal/ui"
)

// -- Bubble Tea messages sent from background goroutines --

// newEventMsg delivers a single real-time event from a relay subscription.
type newEventMsg struct {
	Event nostr.Event
}

// batchEventsMsg delivers multiple events at once (e.g. initial feed fetch).
type batchEventsMsg struct {
	Events []nostr.Event
}

// statusMsg updates the status/hint line (e.g. "Posting... (3/11 relays)").
// An empty Text clears the status back to the default hint.
type statusMsg struct {
	Text string
}

// infoMsg appends arbitrary text lines to the feed area
// (e.g. output from /profile, /following, /relays).
type infoMsg struct {
	Text string
}

// followReadyMsg signals that the contact list has been fetched.
type followReadyMsg struct {
	Hexes []string
}

// dmStartMsg signals the user wants to enter DM mode with a target.
type dmStartMsg struct {
	Target string
}

// switchStartMsg signals the user wants to interactively pick an account.
type switchStartMsg struct{}

// dmSelectMsg signals the user wants to interactively pick a DM target.
type dmSelectMsg struct{}

// -- Styles --

var (
	dimStyle   = lipgloss.NewStyle().Faint(true)
	cyanStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
	greenStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	redStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("1"))
)

// shellProgram holds the running tea.Program reference so background
// goroutines and tea.Cmd functions can send messages to it.
var shellProgram *tea.Program

// -- Model --

const maxFeedLines = 1000

// Shell interaction modes
const (
	modeNormal    = iota
	modeSwitch    // Interactive account picker
	modeDMSelect  // DM target autocomplete picker
	modeDMCompose // DM message composition after target selected
)

type shellModel struct {
	// Display
	feed   feed   // deduplicated, sorted event feed
	status string // dynamic status text (empty = show default hint)
	width  int
	height int

	// Input
	input    *editline.Model
	showMenu bool
	menuSel  int

	// Mention candidates (for autocomplete and p-tag extraction)
	mentionCandidates []ui.MentionCandidate

	// Shell state
	npub       string
	myHex      string
	skHex      string
	relays     []string
	promptName string

	// Welcome message shown until first event arrives
	showWelcome bool

	// DM mode: when set, shell quits and launches DM with this target
	dmTarget string

	// Interactive mode state
	mode int

	// Switch mode state
	switchEntries []profileEntry
	switchIdx     int

	// DM select mode state
	dmSelectInput      textinput.Model
	dmSelectCandidates []ui.MentionCandidate
	dmSelectResults    []ui.MentionCandidate
	dmSelectIdx        int

	// DM compose mode state
	dmComposeInput  textinput.Model
	dmComposeTarget ui.MentionCandidate

	quitting bool
}

func newShellModel(npub, myHex, skHex string, relays []string, promptName string) shellModel {
	ed := editline.New(0, 0)
	ed.Prompt = promptName + "> "
	ed.NextPrompt = strings.Repeat(" ", len(promptName)+2)
	ed.MaxHeight = 5
	ed.CharLimit = 0
	ed.ShowLineNumbers = false
	ed.Placeholder = ""
	ed.CheckInputComplete = func(entireInput [][]rune, line, col int) bool {
		return true // Enter always submits
	}
	ed.KeyMap.MoreHelp.SetEnabled(false)
	// Shift+Enter (Alt+Enter in terminals) inserts newline
	ed.KeyMap.AlwaysNewline = key.NewBinding(key.WithKeys("alt+enter", "alt+\r"))
	ed.KeyMap.AlwaysComplete.SetEnabled(false)

	return shellModel{
		feed:        newFeed(maxFeedLines),
		input:       ed,
		npub:        npub,
		myHex:       myHex,
		skHex:       skHex,
		relays:      relays,
		promptName:  promptName,
		showWelcome: true,
	}
}

func (m shellModel) Init() tea.Cmd {
	return m.input.Focus()
}

func (m shellModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.input.SetSize(msg.Width, msg.Height)
		return m, nil

	case editline.InputCompleteMsg:
		return m.handleSubmit()

	case tea.KeyMsg:
		return m.handleKey(msg)

	case newEventMsg:
		m.feed.AddEvent(msg.Event)
		m.showWelcome = false
		return m, nil

	case batchEventsMsg:
		m.feed.AddEvents(msg.Events)
		if len(msg.Events) > 0 {
			m.showWelcome = false
		}
		return m, nil

	case statusMsg:
		m.status = msg.Text
		return m, nil

	case infoMsg:
		if msg.Text != "" {
			m.feed.AddInfo(msg.Text)
		}
		return m, nil

	case followReadyMsg:
		return m, nil

	case dmStartMsg:
		m.dmTarget = msg.Target
		return m, tea.Quit

	case switchStartMsg:
		entries, err := listSwitchableProfiles()
		if err != nil || len(entries) == 0 {
			m.feed.AddInfo(redStyle.Render("No other accounts found. Run 'nostr login' to add one."))
			return m, nil
		}
		m.mode = modeSwitch
		m.switchEntries = entries
		// Start cursor on active account
		m.switchIdx = 0
		for i, e := range entries {
			if e.npub == m.npub {
				m.switchIdx = i
				break
			}
		}
		return m, nil

	case dmSelectMsg:
		m.mode = modeDMSelect
		m.dmSelectCandidates = m.mentionCandidates
		m.dmSelectResults = ui.FilterCandidates(m.dmSelectCandidates, "")
		m.dmSelectIdx = 0
		ti := textinput.New()
		ti.Focus()
		ti.CharLimit = 0
		ti.Prompt = cyanStyle.Render("DM to: ")
		ti.Width = m.width - 10
		m.dmSelectInput = ti
		return m, nil
	}

	// Pass through to editline
	_, cmd := m.input.Update(msg)
	return m, cmd
}

func (m shellModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Dispatch to mode-specific handlers
	switch m.mode {
	case modeSwitch:
		return m.handleSwitchKey(msg)
	case modeDMSelect:
		return m.handleDMSelectKey(msg)
	case modeDMCompose:
		return m.handleDMComposeKey(msg)
	}

	// Ctrl+C on empty input or Ctrl+D quits
	if msg.Type == tea.KeyCtrlC {
		if strings.TrimSpace(m.input.Value()) == "" {
			m.quitting = true
			return m, tea.Quit
		}
		// Let bubbline handle clearing
	}
	if msg.Type == tea.KeyCtrlD {
		m.quitting = true
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
			cmds := filterCommands([]byte(m.input.Value()))
			if m.menuSel < len(cmds)-1 {
				m.menuSel++
			}
			return m, nil
		case tea.KeyEscape:
			m.showMenu = false
			return m, nil
		}
	}

	// Forward to editline
	_, cmd := m.input.Update(msg)

	// Check if we should show/hide slash menu
	val := m.input.Value()
	if len(val) > 0 && val[0] == '/' && !strings.Contains(val, " ") {
		cmds := filterCommands([]byte(val))
		m.showMenu = len(cmds) > 0
		if m.menuSel >= len(cmds) {
			m.menuSel = 0
		}
	} else {
		m.showMenu = false
	}

	// Auto-trigger @ mention autocomplete
	if (msg.Type == tea.KeyRunes || msg.Type == tea.KeySpace) && len(m.mentionCandidates) > 0 {
		if isInMentionContext(m.input.Value()) {
			return m, tea.Batch(cmd, autoTriggerTab())
		}
	}

	return m, cmd
}

// handleSubmit is called when editline signals InputCompleteMsg (Enter pressed).
func (m shellModel) handleSubmit() (tea.Model, tea.Cmd) {
	line := strings.TrimSpace(m.input.Value())

	// Add to history before resetting
	if line != "" {
		m.input.AddHistoryEntry(line)
	}

	m.input.Reset()

	if line == "" {
		return m, m.input.Focus()
	}

	// Slash menu selection
	if m.showMenu {
		cmds := filterCommands([]byte(line))
		if m.menuSel >= 0 && m.menuSel < len(cmds) {
			// Can't SetValue on editline — just execute the selected command
			line = "/" + cmds[m.menuSel].name
		}
	}
	m.showMenu = false

	if strings.HasPrefix(line, "/") {
		return m, tea.Batch(m.makeSlashCmd(m.npub, m.myHex, m.relays, line), m.input.Focus())
	}

	// Extract mentions from text
	content := line
	var tags nostr.Tags
	mentions := ui.ExtractMentionsFromText(line, m.mentionCandidates)
	if len(mentions) > 0 {
		var mentionTags [][]string
		content, mentionTags = ui.ReplaceMentionsForEvent(line, mentions)
		for _, tag := range mentionTags {
			tags = append(tags, nostr.Tag(tag))
		}
	}

	// Post a note: sign, show in feed, publish async
	event := nostr.Event{
		PubKey:    m.myHex,
		CreatedAt: nostr.Now(),
		Kind:      nostr.KindTextNote,
		Tags:      tags,
		Content:   content,
	}
	if err := event.Sign(m.skHex); err != nil {
		m.feed.AddInfo(redStyle.Render("✗ sign failed: " + err.Error()))
		return m, m.input.Focus()
	}
	m.feed.AddEvent(event)
	_ = cache.LogFeedEvent(m.npub, event)
	_ = cache.LogSentEvent(m.npub, event)

	return m, tea.Batch(publishNoteCmd(m.npub, m.relays, event), m.input.Focus())
}

func (m shellModel) View() string {
	if m.quitting {
		return ""
	}
	if m.width == 0 || m.height == 0 {
		return "Loading..."
	}

	switch m.mode {
	case modeSwitch:
		return m.renderSwitchView()
	case modeDMSelect:
		return m.renderDMSelectView()
	case modeDMCompose:
		return m.renderDMComposeView()
	}

	// Layout: feed area | input | menu | status
	menuLines := m.renderMenu()
	menuHeight := len(menuLines)

	statusLine := m.renderStatus()

	// Editline renders its own view; strip trailing help line
	inputView := m.input.View()
	if idx := strings.LastIndex(inputView, "\n"); idx >= 0 {
		inputView = inputView[:idx]
	}
	// Colorize the plain-text prompt
	plainPrompt := m.promptName + "> "
	colorPrompt := greenStyle.Render(m.promptName) + "> "
	inputView = strings.Replace(inputView, plainPrompt, colorPrompt, 1)
	inputHeight := strings.Count(inputView, "\n") + 1

	// Calculate feed height
	feedHeight := m.height - 1 - inputHeight - menuHeight // 1 for status
	if feedHeight < 1 {
		feedHeight = 1
	}

	// Render feed: take last feedHeight lines
	feed := m.renderFeed(feedHeight)

	var parts []string
	parts = append(parts, feed)
	parts = append(parts, inputView)
	if menuHeight > 0 {
		parts = append(parts, strings.Join(menuLines, "\n"))
	}
	parts = append(parts, statusLine)

	return strings.Join(parts, "\n")
}

func (m shellModel) renderFeed(height int) string {
	rendered := m.feed.Render(m.myHex, m.promptName, m.width)

	if len(rendered) == 0 {
		if m.showWelcome {
			return m.renderWelcome(height)
		}
		lines := make([]string, height)
		for i := range lines {
			lines[i] = ""
		}
		return strings.Join(lines, "\n")
	}

	// Take last `height` lines
	start := 0
	if len(rendered) > height {
		start = len(rendered) - height
	}
	visible := rendered[start:]

	// Pad with empty lines at the top if fewer lines than height
	padding := height - len(visible)
	lines := make([]string, 0, height)
	for i := 0; i < padding; i++ {
		lines = append(lines, "")
	}
	lines = append(lines, visible...)

	return strings.Join(lines, "\n")
}

func (m shellModel) renderWelcome(height int) string {
	welcome := []string{
		"",
		"  " + cyanStyle.Render("Welcome to Nostr"),
		"",
		dimStyle.Render("  Nostr is an open protocol for censorship-resistant social networking."),
		dimStyle.Render("  Your identity is a cryptographic key pair — no accounts, no servers you depend on."),
		"",
		"  " + greenStyle.Render("Getting started:"),
		"",
		"    " + cyanStyle.Render("/follow <user>") + "   Follow someone and see their posts",
		"    " + cyanStyle.Render("/dm [user]") + "       Start a DM conversation",
		"    " + cyanStyle.Render("/help") + "            Show all commands",
		"",
		dimStyle.Render("  Just type text and press enter to post a public note."),
		dimStyle.Render("  A <user> can be an npub, alias, or NIP-05 (user@domain.com)."),
		"",
	}

	// Pad to fill height
	if len(welcome) < height {
		padding := make([]string, height-len(welcome))
		for i := range padding {
			padding[i] = ""
		}
		welcome = append(padding, welcome...)
	}
	if len(welcome) > height {
		welcome = welcome[len(welcome)-height:]
	}
	return strings.Join(welcome, "\n")
}

func (m shellModel) renderStatus() string {
	if m.status != "" {
		return dimStyle.Render("  " + m.status)
	}
	hint := fmt.Sprintf("type / for commands, shift+enter for newline, enter to post to %d relays", len(m.relays))
	return dimStyle.Render("  " + hint)
}

func (m shellModel) renderMenu() []string {
	if !m.showMenu {
		return nil
	}
	val := m.input.Value()
	cmds := filterCommands([]byte(val))
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

// publishNoteCmd returns a tea.Cmd that publishes the event to relays
// with progress updates via shellProgram.Send.
func publishNoteCmd(npub string, relays []string, event nostr.Event) tea.Cmd {
	return func() tea.Msg {
		total := len(relays)
		timeout := time.Duration(timeoutFlag) * time.Millisecond
		ch := internalRelay.PublishEventWithProgress(context.Background(), event, relays, timeout)

		confirmed := 0
		for res := range ch {
			if res.OK {
				confirmed++
			}
			if shellProgram != nil {
				shellProgram.Send(statusMsg{Text: fmt.Sprintf("Posting... (%d/%d relays)", confirmed, total)})
			}
		}
		_ = cache.LogSentEvent(npub, event)
		return statusMsg{Text: ""}
	}
}

// makeSlashCmd returns a tea.Cmd that runs a slash command and captures its output.
func (m shellModel) makeSlashCmd(npub, myHex string, relays []string, line string) tea.Cmd {
	return func() tea.Msg {
		output := captureOutput(func() {
			statusCh := make(chan string, 16)
			go func() {
				for s := range statusCh {
					if shellProgram != nil {
						shellProgram.Send(infoMsg{Text: s})
					}
				}
			}()
			executeSlashCommand(npub, myHex, line, relays, statusCh)
			close(statusCh)
		})
		if output != "" {
			// Check for DM start signal
			if strings.HasPrefix(output, "__DM_START__:") {
				target := strings.TrimPrefix(output, "__DM_START__:")
				return dmStartMsg{Target: target}
			}
			if output == "__SWITCH_START__" {
				return switchStartMsg{}
			}
			if output == "__DM_SELECT__" {
				return dmSelectMsg{}
			}
			return infoMsg{Text: strings.TrimRight(output, "\n")}
		}
		return nil
	}
}


// -- Switch mode key handling --

func (m shellModel) handleSwitchKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyUp:
		if m.switchIdx > 0 {
			m.switchIdx--
		}
		return m, nil
	case tea.KeyDown:
		if m.switchIdx < len(m.switchEntries)-1 {
			m.switchIdx++
		}
		return m, nil
	case tea.KeyEnter:
		if m.switchIdx >= 0 && m.switchIdx < len(m.switchEntries) {
			entry := m.switchEntries[m.switchIdx]
			if entry.npub == m.npub {
				// Already active, just exit switch mode
				m.mode = modeNormal
				return m, nil
			}
			if err := switchToTarget(entry.npub, m.npub, color.New(color.FgGreen)); err != nil {
				m.feed.AddInfo(redStyle.Render("✗ " + err.Error()))
				m.mode = modeNormal
				return m, nil
			}
			// Update shell state for new account
			newNpub, _ := config.ActiveProfile()
			newHex, _ := crypto.NpubToHex(newNpub)
			newName := entry.name
			if newName == "" {
				newName = newNpub[:16] + "..."
			}
			m.npub = newNpub
			m.myHex = newHex
			m.promptName = newName
			shellPromptName = newName
			// Prompt is rendered separately in View()

			// Reload nsec for new account
			if nsec, err := config.LoadNsec(newNpub); err == nil {
				if sk, err := crypto.NsecToHex(nsec); err == nil {
					m.skHex = sk
				}
			}
			// Reload relays
			if relays, err := config.LoadRelays(newNpub); err == nil {
				m.relays = relays
			}

			// Reload mention candidates and autocomplete
			m.mentionCandidates = ui.LoadMentionCandidates(newNpub)
			if len(m.mentionCandidates) > 0 {
				m.input.AutoComplete = ui.MentionAutoComplete(m.mentionCandidates)
			}

			// Update editline prompt for new account
			m.input.Prompt = newName + "> "
			m.input.NextPrompt = strings.Repeat(" ", len(newName)+2)

			m.feed.AddInfo(greenStyle.Render("✓ Switched to " + newName))
		}
		m.mode = modeNormal
		return m, nil
	case tea.KeyEscape, tea.KeyCtrlC:
		m.mode = modeNormal
		return m, nil
	}
	return m, nil
}

func (m shellModel) renderSwitchView() string {
	// Feed area (same as normal)
	listHeight := len(m.switchEntries) + 2 // header + entries + blank
	statusHeight := 1
	inputHeight := 1 // not shown but reserve space
	overlayHeight := listHeight + statusHeight + inputHeight
	feedHeight := m.height - overlayHeight
	if feedHeight < 1 {
		feedHeight = 1
	}
	feed := m.renderFeed(feedHeight)

	// Switch picker
	var pickerLines []string
	pickerLines = append(pickerLines, cyanStyle.Render("Switch account:"))
	for i, e := range m.switchEntries {
		name := e.name
		if name == "" {
			name = ui.TruncateNpub(e.npub)
		} else {
			name = name + " (" + ui.TruncateNpub(e.npub) + ")"
		}
		active := ""
		if e.npub == m.npub {
			active = dimStyle.Render(" ← active")
		}
		if i == m.switchIdx {
			pickerLines = append(pickerLines, "  "+cyanStyle.Render("→ "+name)+active)
		} else {
			pickerLines = append(pickerLines, "    "+dimStyle.Render(name)+active)
		}
	}

	statusLine := dimStyle.Render("  ↑/↓ navigate, enter to switch, esc to cancel")

	var parts []string
	parts = append(parts, feed)
	parts = append(parts, strings.Join(pickerLines, "\n"))
	parts = append(parts, statusLine)
	return strings.Join(parts, "\n")
}

// -- DM Select mode key handling --

func (m shellModel) handleDMSelectKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyUp:
		if m.dmSelectIdx > 0 {
			m.dmSelectIdx--
		}
		return m, nil
	case tea.KeyDown:
		if m.dmSelectIdx < len(m.dmSelectResults)-1 {
			m.dmSelectIdx++
		}
		return m, nil
	case tea.KeyTab, tea.KeyEnter:
		val := strings.TrimSpace(m.dmSelectInput.Value())
		if len(m.dmSelectResults) > 0 && m.dmSelectIdx < len(m.dmSelectResults) {
			// Select from dropdown
			selected := m.dmSelectResults[m.dmSelectIdx]
			m.dmComposeTarget = selected
			m.mode = modeDMCompose
			ti := textinput.New()
			ti.Focus()
			ti.CharLimit = 0
			ti.Prompt = cyanStyle.Render("DM to "+selected.DisplayName+": ")
			ti.Width = m.width - len("DM to "+selected.DisplayName+": ") - 3
			m.dmComposeInput = ti
			return m, nil
		} else if val != "" {
			// User typed a raw npub or name, try to use it directly
			// Create a candidate from raw input
			m.dmComposeTarget = ui.MentionCandidate{
				DisplayName: val,
				Npub:        val, // might be npub or name, resolve later
				PubHex:      "",
			}
			m.mode = modeDMCompose
			ti := textinput.New()
			ti.Focus()
			ti.CharLimit = 0
			ti.Prompt = cyanStyle.Render("DM to "+val+": ")
			ti.Width = m.width - len("DM to "+val+": ") - 3
			m.dmComposeInput = ti
			return m, nil
		}
		return m, nil
	case tea.KeyEscape, tea.KeyCtrlC:
		m.mode = modeNormal
		return m, nil
	}

	// Let textinput handle the key
	var cmd tea.Cmd
	m.dmSelectInput, cmd = m.dmSelectInput.Update(msg)

	// Filter candidates based on input
	query := m.dmSelectInput.Value()
	m.dmSelectResults = ui.FilterCandidates(m.dmSelectCandidates, query)
	m.dmSelectIdx = 0

	return m, cmd
}

func (m shellModel) renderDMSelectView() string {
	// Feed area
	dropdownHeight := len(m.dmSelectResults)
	if dropdownHeight > 7 {
		dropdownHeight = 7
	}
	overlayHeight := 1 + dropdownHeight + 1 // input + dropdown + status
	feedHeight := m.height - overlayHeight
	if feedHeight < 1 {
		feedHeight = 1
	}
	feed := m.renderFeed(feedHeight)

	// Dropdown
	var dropdownLines []string
	maxVisible := 7
	if len(m.dmSelectResults) < maxVisible {
		maxVisible = len(m.dmSelectResults)
	}
	start := 0
	if m.dmSelectIdx >= maxVisible {
		start = m.dmSelectIdx - maxVisible + 1
	}
	end := start + maxVisible
	if end > len(m.dmSelectResults) {
		end = len(m.dmSelectResults)
		start = end - maxVisible
		if start < 0 {
			start = 0
		}
	}
	for i := start; i < end; i++ {
		c := m.dmSelectResults[i]
		entry := c.DisplayName + " (" + ui.TruncateNpub(c.Npub) + ")"
		if i == m.dmSelectIdx {
			dropdownLines = append(dropdownLines, "  "+cyanStyle.Render("→ "+entry))
		} else {
			dropdownLines = append(dropdownLines, "    "+dimStyle.Render(entry))
		}
	}

	statusLine := dimStyle.Render("  ↑/↓ navigate, enter to select, esc to cancel")

	var parts []string
	parts = append(parts, feed)
	parts = append(parts, m.dmSelectInput.View())
	if len(dropdownLines) > 0 {
		parts = append(parts, strings.Join(dropdownLines, "\n"))
	}
	parts = append(parts, statusLine)
	return strings.Join(parts, "\n")
}

// -- DM Compose mode key handling --

func (m shellModel) handleDMComposeKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyEnter:
		message := strings.TrimSpace(m.dmComposeInput.Value())
		target := m.dmComposeTarget

		if message == "" {
			// Empty enter → enter interactive DM conversation
			// Use DisplayName or Npub as the target identifier
			identifier := target.DisplayName
			if identifier == "" {
				identifier = target.Npub
			}
			m.mode = modeNormal
			return m, func() tea.Msg {
				return dmStartMsg{Target: identifier}
			}
		}

		// Non-empty → send DM immediately and return to shell
		identifier := target.DisplayName
		if identifier == "" {
			identifier = target.Npub
		}
		m.mode = modeNormal
		m.feed.AddInfo(dimStyle.Render("Sending DM to " + identifier + "..."))

		npub := m.npub
		myHex := m.myHex
		relays := m.relays

		return m, func() tea.Msg {
			// Resolve target
			targetHex, err := resolve.Resolve(npub, identifier)
			if err != nil {
				return infoMsg{Text: redStyle.Render("✗ Cannot resolve " + identifier + ": " + err.Error())}
			}

			nsec, err := config.LoadNsec(npub)
			if err != nil {
				return infoMsg{Text: redStyle.Render("✗ " + err.Error())}
			}
			skHex, err := crypto.NsecToHex(nsec)
			if err != nil {
				return infoMsg{Text: redStyle.Render("✗ " + err.Error())}
			}

			statusCh := make(chan string, 16)
			go func() {
				for s := range statusCh {
					if shellProgram != nil {
						shellProgram.Send(infoMsg{Text: s})
					}
				}
			}()
			sendDMAsync(npub, skHex, myHex, targetHex, message, relays, statusCh)
			close(statusCh)
			return infoMsg{Text: greenStyle.Render("✓ DM sent to " + identifier)}
		}

	case tea.KeyEscape, tea.KeyCtrlC:
		m.mode = modeNormal
		return m, nil
	}

	// Let textinput handle the key
	var cmd tea.Cmd
	m.dmComposeInput, cmd = m.dmComposeInput.Update(msg)
	return m, cmd
}

func (m shellModel) renderDMComposeView() string {
	feedHeight := m.height - 2 // input + status
	if feedHeight < 1 {
		feedHeight = 1
	}
	feed := m.renderFeed(feedHeight)

	statusLine := dimStyle.Render("  enter to send, empty enter for conversation, esc to cancel")

	var parts []string
	parts = append(parts, feed)
	parts = append(parts, m.dmComposeInput.View())
	parts = append(parts, statusLine)
	return strings.Join(parts, "\n")
}

// captureOutput runs fn and captures anything written to os.Stdout.
// It forces color output since the pipe would otherwise disable it.
func captureOutput(fn func()) string {
	r, w, err := os.Pipe()
	if err != nil {
		fn()
		return ""
	}

	origStdout := os.Stdout
	os.Stdout = w

	// Force color output even though stdout is now a pipe
	color.NoColor = false

	done := make(chan string)
	go func() {
		var buf strings.Builder
		_, _ = io.Copy(&buf, r)
		done <- buf.String()
	}()

	fn()

	os.Stdout = origStdout
	w.Close()
	result := <-done
	r.Close()
	return result
}
