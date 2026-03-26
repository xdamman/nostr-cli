package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/fatih/color"
	"github.com/nbd-wtf/go-nostr"
	"github.com/xdamman/nostr-cli/internal/cache"
	internalRelay "github.com/xdamman/nostr-cli/internal/relay"
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
	feedLines []string // rendered feed lines with ANSI
	status    string   // dynamic status text (empty = show default hint)
	width     int
	height    int

	// Input
	input    textarea.Model
	showMenu bool
	menuSel  int

	// Shell state
	npub       string
	myHex      string
	skHex      string
	relays     []string
	promptName string

	quitting bool
}

func newShellModel(npub, myHex, skHex string, relays []string, promptName string) shellModel {
	ta := textarea.New()
	ta.Focus()
	ta.CharLimit = 0
	ta.Prompt = greenStyle.Render(promptName) + "> "
	ta.ShowLineNumbers = false
	ta.SetHeight(1) // start as single line, grows with content
	ta.MaxHeight = 10
	ta.KeyMap.InsertNewline.SetEnabled(false) // Enter submits, not newline

	// Remove default borders/padding so it looks like a simple prompt
	ta.FocusedStyle.CursorLine = lipgloss.NewStyle()
	ta.FocusedStyle.Base = lipgloss.NewStyle()
	ta.BlurredStyle.CursorLine = lipgloss.NewStyle()
	ta.BlurredStyle.Base = lipgloss.NewStyle()

	return shellModel{
		input:      ta,
		npub:       npub,
		myHex:      myHex,
		skHex:      skHex,
		relays:     relays,
		promptName: promptName,
	}
}

func (m shellModel) Init() tea.Cmd {
	return textarea.Blink
}

func (m shellModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.input.SetWidth(msg.Width)
		return m, nil

	case tea.KeyMsg:
		return m.handleKey(msg)

	case newEventMsg:
		line := sprintFeedEvent(msg.Event, m.myHex, m.promptName, m.width)
		appendFeed(&m, line)
		return m, nil

	case batchEventsMsg:
		for _, ev := range msg.Events {
			line := sprintFeedEvent(ev, m.myHex, m.promptName, m.width)
			appendFeed(&m, line)
		}
		return m, nil

	case statusMsg:
		m.status = msg.Text
		return m, nil

	case infoMsg:
		if msg.Text != "" {
			for _, line := range strings.Split(msg.Text, "\n") {
				appendFeed(&m, line)
			}
		}
		return m, nil

	case followReadyMsg:
		return m, nil
	}

	// Pass through to textinput
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m shellModel) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case tea.KeyCtrlC, tea.KeyCtrlD:
		m.quitting = true
		return m, tea.Quit

	case tea.KeyEnter:
		line := strings.TrimSpace(m.input.Value())
		m.input.Reset()
		m.input.SetHeight(1)

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
			return m, m.makeSlashCmd(m.npub, m.myHex, m.relays, line)
		}

		// Post a note: sign, show in feed, publish async
		event := nostr.Event{
			PubKey:    m.myHex,
			CreatedAt: nostr.Now(),
			Kind:      nostr.KindTextNote,
			Tags:      nostr.Tags{},
			Content:   line,
		}
		if err := event.Sign(m.skHex); err != nil {
			appendFeed(&m, redStyle.Render("✗ sign failed: "+err.Error()))
			return m, nil
		}
		feedLine := sprintFeedEvent(event, m.myHex, m.promptName, m.width)
		appendFeed(&m, feedLine)
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

	// Let textarea handle the key
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)

	// Auto-grow/shrink textarea height based on content lines
	m.resizeInput()

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

	return m, cmd
}

// resizeInput adjusts the textarea height to fit its content (1..MaxHeight).
func (m *shellModel) resizeInput() {
	lines := m.input.LineCount()
	if lines < 1 {
		lines = 1
	}
	max := m.input.MaxHeight
	if max > 0 && lines > max {
		lines = max
	}
	m.input.SetHeight(lines)
}

// appendFeed adds a line to the feed buffer and caps at maxFeedLines.
// Works on both value and pointer receivers since the caller always
// returns the modified model to Bubble Tea.
func appendFeed(m *shellModel, line string) {
	m.feedLines = append(m.feedLines, line)
	if len(m.feedLines) > maxFeedLines {
		m.feedLines = m.feedLines[len(m.feedLines)-maxFeedLines:]
	}
}

func (m shellModel) View() string {
	if m.quitting {
		return ""
	}
	if m.width == 0 || m.height == 0 {
		return "Loading..."
	}

	// Layout: feed | menu | input (wrapping) | status bar
	menuLines := m.renderMenu()
	menuHeight := len(menuLines)

	statusLine := m.renderStatus()

	// Textarea height grows with content
	inputHeight := m.input.Height()

	// Calculate feed height: total - input - status(1) - menu
	feedHeight := m.height - inputHeight - 1 - menuHeight
	if feedHeight < 1 {
		feedHeight = 1
	}

	// Render feed: take last feedHeight lines
	feed := m.renderFeed(feedHeight)

	// Build the view: feed | menu | input | status bar
	var parts []string
	parts = append(parts, feed)
	if menuHeight > 0 {
		parts = append(parts, strings.Join(menuLines, "\n"))
	}
	parts = append(parts, m.input.View())
	parts = append(parts, statusLine)

	return strings.Join(parts, "\n")
}

func (m shellModel) renderFeed(height int) string {
	if len(m.feedLines) == 0 {
		// Fill with empty lines
		lines := make([]string, height)
		for i := range lines {
			lines[i] = ""
		}
		return strings.Join(lines, "\n")
	}

	// Take last `height` lines
	start := 0
	if len(m.feedLines) > height {
		start = len(m.feedLines) - height
	}
	visible := m.feedLines[start:]

	// Pad with empty lines at the top if fewer lines than height
	padding := height - len(visible)
	lines := make([]string, 0, height)
	for i := 0; i < padding; i++ {
		lines = append(lines, "")
	}
	lines = append(lines, visible...)

	return strings.Join(lines, "\n")
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
			return infoMsg{Text: strings.TrimRight(output, "\n")}
		}
		return nil
	}
}

// sprintFeedEvent formats a single feed event as a string (no raw terminal codes).
func sprintFeedEvent(ev nostr.Event, myHex, promptName string, termW int) string {
	ts := formatLocalTimestamp(time.Unix(int64(ev.CreatedAt), 0))
	name := resolveAuthorName(ev.PubKey)
	if ev.PubKey == myHex && name != promptName && promptName != "" {
		name = promptName
	}
	nw := updateFeedNameWidth(name)
	prefixLen := 14 + 2 + nw + 2

	content := ev.Content
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "")
	content = strings.TrimSpace(content)
	content = renderInlineMarkdown(content)

	// Wrap to terminal width
	avail := termW - prefixLen
	if avail < 20 {
		avail = 20
	}
	if termW <= 0 {
		avail = 60
	}

	indent := strings.Repeat(" ", prefixLen)
	var sb strings.Builder
	paragraphs := strings.Split(content, "\n")
	for pi, para := range paragraphs {
		if pi > 0 {
			sb.WriteString("\n")
			sb.WriteString(indent)
		}
		for len(para) > 0 {
			vis := visibleLen(para)
			if vis <= avail {
				sb.WriteString(para)
				break
			}
			lineLen := visibleIndex(para, avail)
			if lineLen <= 0 {
				lineLen = len(para)
			}
			if lineLen < len(para) {
				cutoff := visibleIndex(para, avail/3)
				if idx := strings.LastIndex(para[:lineLen], " "); idx > cutoff {
					lineLen = idx + 1
				}
			}
			sb.WriteString(strings.TrimRight(para[:lineLen], " "))
			para = para[lineLen:]
			if len(para) > 0 {
				sb.WriteString("\n")
				sb.WriteString(indent)
			}
		}
	}

	tsStr := dimStyle.Render(ts + "  ")
	nameStr := cyanStyle.Render(fmt.Sprintf("%-*s", nw, name)) + ": "

	return tsStr + nameStr + sb.String()
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
