package ui

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"golang.org/x/term"
)

// InlineInputResult holds the result of a single-line bubbletea input session.
type InlineInputResult struct {
	Text      string
	Mentions  []MentionCandidate // selected mentions for p-tags
	Cancelled bool
}

// InlineInputConfig configures a single-line bubbletea input session.
type InlineInputConfig struct {
	Prompt     string             // ANSI-colored prompt prefix
	Hint       string             // hint text shown below input
	Candidates []MentionCandidate // for @ autocomplete
}

// RunInlineInput runs a bubbletea program in inline mode for single-line input.
// Returns the entered text and any selected mentions.
// If stdin is not a TTY, returns empty result with Cancelled=true.
func RunInlineInput(cfg InlineInputConfig) InlineInputResult {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return InlineInputResult{Cancelled: true}
	}

	m := newInlineModel(cfg)
	p := tea.NewProgram(m, tea.WithoutSignalHandler())

	finalModel, err := p.Run()
	if err != nil {
		return InlineInputResult{Cancelled: true}
	}

	result := finalModel.(inlineModel)
	return InlineInputResult{
		Text:      result.value,
		Mentions:  result.selectedMentions,
		Cancelled: result.cancelled,
	}
}

var (
	inlineDimStyle  = lipgloss.NewStyle().Faint(true)
	inlineCyanStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
)

// inlineModel is the bubbletea model for single-line input with mention autocomplete.
type inlineModel struct {
	input textinput.Model
	hint  string

	// result
	value            string
	cancelled        bool
	selectedMentions []MentionCandidate

	// mention autocomplete
	mentionCandidates []MentionCandidate
	mentionResults    []MentionCandidate
	mentionActive     bool
	mentionIdx        int
	mentionQuery      string
}

func newInlineModel(cfg InlineInputConfig) inlineModel {
	ti := textinput.New()
	ti.Prompt = cfg.Prompt
	ti.Focus()
	ti.CharLimit = 0 // no limit

	return inlineModel{
		input:             ti,
		hint:              cfg.Hint,
		mentionCandidates: cfg.Candidates,
	}
}

func (m inlineModel) Init() tea.Cmd {
	return nil
}

func (m inlineModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
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
			case tea.KeyTab:
				if len(m.mentionResults) > 0 && m.mentionIdx < len(m.mentionResults) {
					m = m.confirmMention()
				}
				return m, nil
			case tea.KeyEnter:
				if len(m.mentionResults) > 0 && m.mentionIdx < len(m.mentionResults) {
					m = m.confirmMention()
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
			m.cancelled = true
			return m, tea.Quit

		case tea.KeyEscape:
			m.cancelled = true
			return m, tea.Quit

		case tea.KeyEnter:
			m.mentionActive = false
			m.mentionResults = nil
			m.value = strings.TrimSpace(m.input.Value())
			return m, tea.Quit
		}
	}

	// Forward to textinput
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)

	// Update mention state after every keystroke
	m.updateMentionState()

	return m, cmd
}

func (m inlineModel) View() string {
	var b strings.Builder

	// Input line
	b.WriteString(m.input.View())
	b.WriteString("\n")

	// Hint line
	if m.hint != "" {
		b.WriteString(inlineDimStyle.Render("  " + m.hint))
		b.WriteString("\n")
	}

	// Mention dropdown
	if m.mentionActive && len(m.mentionResults) > 0 {
		maxVisible := 5
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

		for i := start; i < end; i++ {
			c := m.mentionResults[i]
			entry := c.DisplayName + " (" + TruncateNpub(c.Npub) + ")"
			if i == m.mentionIdx {
				b.WriteString("  " + inlineCyanStyle.Render("→ "+entry))
			} else {
				b.WriteString("    " + inlineDimStyle.Render(entry))
			}
			b.WriteString("\n")
		}
	}

	return b.String()
}

func (m inlineModel) confirmMention() inlineModel {
	if len(m.mentionResults) == 0 || m.mentionIdx >= len(m.mentionResults) {
		m.mentionActive = false
		return m
	}
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

func (m *inlineModel) updateMentionState() {
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
	results := FilterCandidates(m.mentionCandidates, query)
	if len(results) == 0 {
		m.mentionActive = false
		m.mentionResults = nil
		return
	}
	m.mentionActive = true
	m.mentionResults = results
	m.mentionIdx = 0
}

// ReadLineSimple is a fallback for non-terminal input.
func ReadLineSimple(prompt string) (string, bool) {
	fmt.Print(prompt)
	var line string
	_, err := fmt.Scanln(&line)
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(line), true
}
