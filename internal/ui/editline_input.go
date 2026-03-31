package ui

import (
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/knz/bubbline/editline"
	"golang.org/x/term"
)

// EditlineInputConfig configures an editline-based multiline input session.
type EditlineInputConfig struct {
	Prompt     string             // plain-text prompt prefix (no ANSI)
	Hint       string             // hint text shown below input
	Candidates []MentionCandidate // for @ autocomplete
}

// EditlineInputResult holds the result of an editline input session.
type EditlineInputResult struct {
	Text      string
	Mentions  []MentionCandidate
	Cancelled bool
}

type editlineInputModel struct {
	editor *editline.Model
	hint   string

	// result
	value     string
	cancelled bool

	// mention tracking
	mentionCandidates []MentionCandidate
	selectedMentions  []MentionCandidate
	mentionResults    []MentionCandidate
	mentionActive     bool
	mentionIdx        int
	mentionQuery      string

	width  int
	height int
}

func newEditlineInputModel(cfg EditlineInputConfig) editlineInputModel {
	ed := editline.New(0, 0)
	ed.Prompt = cfg.Prompt
	ed.NextPrompt = strings.Repeat(" ", len(cfg.Prompt))
	ed.MaxHeight = 10
	ed.CharLimit = 0
	ed.ShowLineNumbers = false
	ed.Placeholder = ""
	ed.CheckInputComplete = func(entireInput [][]rune, line, col int) bool {
		return true // Enter always submits
	}
	ed.KeyMap.MoreHelp.SetEnabled(false)

	return editlineInputModel{
		editor:            ed,
		hint:              cfg.Hint,
		mentionCandidates: cfg.Candidates,
	}
}

func (m editlineInputModel) Init() tea.Cmd {
	return m.editor.Focus()
}

func (m editlineInputModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.editor.SetSize(msg.Width, msg.Height)
		return m, nil

	case editline.InputCompleteMsg:
		m.value = strings.TrimSpace(m.editor.Value())
		return m, tea.Quit

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
			case tea.KeyTab, tea.KeyEnter:
				if len(m.mentionResults) > 0 && m.mentionIdx < len(m.mentionResults) {
					m.selectedMentions = append(m.selectedMentions, m.mentionResults[m.mentionIdx])
					m.mentionActive = false
					m.mentionResults = nil
				}
				return m, nil
			case tea.KeyEscape:
				m.mentionActive = false
				m.mentionResults = nil
				return m, nil
			}
		}

		if msg.Type == tea.KeyCtrlC || msg.Type == tea.KeyCtrlD || msg.Type == tea.KeyEscape {
			m.cancelled = true
			return m, tea.Quit
		}
	}

	_, cmd := m.editor.Update(msg)

	// Update mention state
	m.updateMentionState()

	return m, cmd
}

func (m editlineInputModel) View() string {
	var b strings.Builder

	// Editor view (strip trailing help line)
	edView := m.editor.View()
	if idx := strings.LastIndex(edView, "\n"); idx >= 0 {
		edView = edView[:idx]
	}
	b.WriteString(edView)
	b.WriteString("\n")

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

	// Hint line
	if m.hint != "" {
		b.WriteString(inlineDimStyle.Render("  " + m.hint))
		b.WriteString("\n")
	}

	return b.String()
}

func (m *editlineInputModel) updateMentionState() {
	if len(m.mentionCandidates) == 0 {
		return
	}
	val := m.editor.Value()
	if val == "" {
		m.mentionActive = false
		return
	}

	textBeforeCursor := val

	atIdx := -1
	for i := len(textBeforeCursor) - 1; i >= 0; i-- {
		if textBeforeCursor[i] == ' ' || textBeforeCursor[i] == '\n' {
			break
		}
		if textBeforeCursor[i] == '@' {
			if i == 0 || textBeforeCursor[i-1] == ' ' || textBeforeCursor[i-1] == '\n' {
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

// RunEditlineInput runs a bubbletea program with editline for multiline input.
// Returns the entered text and any selected mentions.
func RunEditlineInput(cfg EditlineInputConfig) EditlineInputResult {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return EditlineInputResult{Cancelled: true}
	}

	m := newEditlineInputModel(cfg)
	p := tea.NewProgram(m, tea.WithoutSignalHandler())

	finalModel, err := p.Run()
	if err != nil {
		return EditlineInputResult{Cancelled: true}
	}

	result := finalModel.(editlineInputModel)
	return EditlineInputResult{
		Text:      result.value,
		Mentions:  result.selectedMentions,
		Cancelled: result.cancelled,
	}
}
