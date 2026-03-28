package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/fatih/color"
	"github.com/nbd-wtf/go-nostr"
	"github.com/xdamman/nostr-cli/internal/cache"
	internalRelay "github.com/xdamman/nostr-cli/internal/relay"
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

type shellModel struct {
	// Display
	feed   feed   // deduplicated, sorted event feed
	status string // dynamic status text (empty = show default hint)
	width  int
	height int

	// Input
	input    textinput.Model
	showMenu bool
	menuSel  int

	// Mention autocomplete
	mentionCandidates []ui.MentionCandidate
	mentionResults    []ui.MentionCandidate
	mentionActive     bool
	mentionIdx        int
	mentionQuery      string
	selectedMentions  []ui.MentionCandidate

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

	quitting bool
}

func newShellModel(npub, myHex, skHex string, relays []string, promptName string) shellModel {
	ti := textinput.New()
	ti.Focus()
	ti.CharLimit = 0 // no limit
	ti.Prompt = greenStyle.Render(promptName) + "> "
	ti.Width = 0 // will be set on WindowSizeMsg

	return shellModel{
		feed:        newFeed(maxFeedLines),
		input:       ti,
		npub:        npub,
		myHex:       myHex,
		skHex:       skHex,
		relays:      relays,
		promptName:  promptName,
		showWelcome: true,
	}
}

func (m shellModel) Init() tea.Cmd {
	return textinput.Blink
}

func (m shellModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.input.Width = msg.Width - len(m.promptName) - 3 // "name> " + margin
		return m, nil

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
	}

	// Pass through to textinput
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m shellModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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
				m = m.confirmShellMention()
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

		if m.showMenu {
			cmds := filterCommands([]byte(line))
			if m.menuSel >= 0 && m.menuSel < len(cmds) {
				m.input.SetValue("/" + cmds[m.menuSel].name + " ")
				m.input.CursorEnd()
				m.showMenu = false
				return m, nil
			}
		}
		m.showMenu = false

		if strings.HasPrefix(line, "/") {
			m.selectedMentions = nil
			return m, m.makeSlashCmd(m.npub, m.myHex, m.relays, line)
		}

		// Process mentions in the message
		content := line
		var tags nostr.Tags
		if len(m.selectedMentions) > 0 {
			var mentionTags [][]string
			content, mentionTags = ui.ReplaceMentionsForEvent(line, m.selectedMentions)
			for _, tag := range mentionTags {
				tags = append(tags, nostr.Tag(tag))
			}
			m.selectedMentions = nil
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
			return m, nil
		}
		m.feed.AddEvent(event)
		_ = cache.LogFeedEvent(m.npub, event)
		_ = cache.LogSentEvent(m.npub, event)

		return m, publishNoteCmd(m.npub, m.relays, event)

	case tea.KeyTab:
		if m.showMenu {
			val := m.input.Value()
			cmds := filterCommands([]byte(val))
			if m.menuSel >= 0 && m.menuSel < len(cmds) {
				m.input.SetValue("/" + cmds[m.menuSel].name + " ")
				m.input.CursorEnd()
				m.showMenu = false
			}
			return m, nil
		}

	case tea.KeyEscape:
		if m.showMenu {
			m.showMenu = false
			return m, nil
		}

	case tea.KeyUp:
		if m.showMenu {
			if m.menuSel > 0 {
				m.menuSel--
			}
			return m, nil
		}

	case tea.KeyDown:
		if m.showMenu {
			cmds := filterCommands([]byte(m.input.Value()))
			if m.menuSel < len(cmds)-1 {
				m.menuSel++
			}
			return m, nil
		}
	}

	// Let textinput handle the key
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)

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

	// Check mention trigger
	m.updateShellMentionState()

	return m, cmd
}

func (m shellModel) confirmShellMention() shellModel {
	selected := m.mentionResults[m.mentionIdx]
	val := m.input.Value()

	// Find the last '@' that started this mention
	atIdx := strings.LastIndex(val, "@"+m.mentionQuery)
	if atIdx < 0 {
		atIdx = strings.LastIndex(val, "@")
	}
	if atIdx < 0 {
		m.mentionActive = false
		return m
	}

	// Replace @query with @DisplayName
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

func (m *shellModel) updateShellMentionState() {
	if len(m.mentionCandidates) == 0 {
		return
	}
	val := m.input.Value()
	if val == "" {
		m.mentionActive = false
		return
	}

	// Find the last '@' not followed by a space
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
			// Valid if at start or preceded by space
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


func (m shellModel) View() string {
	if m.quitting {
		return ""
	}
	if m.width == 0 || m.height == 0 {
		return "Loading..."
	}

	// Layout: feed area | status line | input line
	// If menu is shown, it sits between status and input
	menuLines := m.renderMenu()
	menuHeight := len(menuLines)

	statusLine := m.renderStatus()

	// Mention dropdown
	mentionLines := m.renderMentionMenu()
	mentionHeight := len(mentionLines)

	// Calculate feed height
	feedHeight := m.height - 2 - menuHeight - mentionHeight // 1 for status, 1 for input
	if feedHeight < 1 {
		feedHeight = 1
	}

	// Render feed: take last feedHeight lines
	feed := m.renderFeed(feedHeight)

	// Build the view: feed | input | menu | mention | status bar
	var parts []string
	parts = append(parts, feed)
	parts = append(parts, m.input.View())
	if menuHeight > 0 {
		parts = append(parts, strings.Join(menuLines, "\n"))
	}
	if mentionHeight > 0 {
		parts = append(parts, strings.Join(mentionLines, "\n"))
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
	hint := fmt.Sprintf("type / for commands, enter to post to %d relays, ctrl+c to exit", len(m.relays))
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

func (m shellModel) renderMentionMenu() []string {
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
			return infoMsg{Text: strings.TrimRight(output, "\n")}
		}
		return nil
	}
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
