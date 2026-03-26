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
	feed   feed   // deduplicated, sorted event feed
	status string // dynamic status text (empty = show default hint)
	width  int
	height int

	// Input
	input    textinput.Model
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
	ti := textinput.New()
	ti.Focus()
	ti.CharLimit = 0 // no limit
	ti.Prompt = greenStyle.Render(promptName) + "> "
	ti.Width = 0 // will be set on WindowSizeMsg

	return shellModel{
		feed:       newFeed(maxFeedLines),
		input:      ti,
		npub:       npub,
		myHex:      myHex,
		skHex:      skHex,
		relays:     relays,
		promptName: promptName,
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
		return m, nil

	case batchEventsMsg:
		m.feed.AddEvents(msg.Events)
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

	return m, cmd
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

	// Calculate feed height
	feedHeight := m.height - 2 - menuHeight // 1 for status, 1 for input
	if feedHeight < 1 {
		feedHeight = 1
	}

	// Render feed: take last feedHeight lines
	feed := m.renderFeed(feedHeight)

	// Build the view: feed | input | menu | status bar
	var parts []string
	parts = append(parts, feed)
	parts = append(parts, m.input.View())
	if menuHeight > 0 {
		parts = append(parts, strings.Join(menuLines, "\n"))
	}
	parts = append(parts, statusLine)

	return strings.Join(parts, "\n")
}

func (m shellModel) renderFeed(height int) string {
	rendered := m.feed.Render(m.myHex, m.promptName, m.width)

	if len(rendered) == 0 {
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
