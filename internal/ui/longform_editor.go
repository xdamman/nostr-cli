package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textarea"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// LongFormEditorConfig configures the long-form content editor.
type LongFormEditorConfig struct {
	Title       string
	InitialText string // for editing existing content
}

// LongFormEditorResult holds the result of the editor session.
type LongFormEditorResult struct {
	Text      string
	Cancelled bool
}

// RunLongFormEditor runs a full-screen bubbletea textarea editor for long-form content.
func RunLongFormEditor(cfg LongFormEditorConfig) LongFormEditorResult {
	m := newLongFormModel(cfg)
	p := tea.NewProgram(m, tea.WithAltScreen())
	finalModel, err := p.Run()
	if err != nil {
		return LongFormEditorResult{Cancelled: true}
	}
	result := finalModel.(longFormModel)
	return LongFormEditorResult{
		Text:      result.textarea.Value(),
		Cancelled: result.cancelled,
	}
}

var (
	lfTitleStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("6"))
	lfStatusStyle = lipgloss.NewStyle().Faint(true)
	lfCountStyle  = lipgloss.NewStyle().Faint(true).Foreground(lipgloss.Color("8"))
)

type longFormModel struct {
	textarea  textarea.Model
	title     string
	cancelled bool
	width     int
	height    int
}

func newLongFormModel(cfg LongFormEditorConfig) longFormModel {
	ta := textarea.New()
	ta.Placeholder = "Write your article in Markdown..."
	ta.Focus()
	ta.CharLimit = 0 // no limit
	ta.ShowLineNumbers = true

	if cfg.InitialText != "" {
		ta.SetValue(cfg.InitialText)
	}

	return longFormModel{
		textarea: ta,
		title:    cfg.Title,
	}
}

func (m longFormModel) Init() tea.Cmd {
	return nil
}

func (m longFormModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.textarea.SetWidth(msg.Width)
		// Reserve lines for: title (2 if present), status bar (1), padding (1)
		headerLines := 1 // status bar
		if m.title != "" {
			headerLines += 2
		}
		taHeight := msg.Height - headerLines - 1
		if taHeight < 5 {
			taHeight = 5
		}
		m.textarea.SetHeight(taHeight)
		return m, nil

	case tea.KeyMsg:
		switch msg.String() {
		case "ctrl+s":
			// Submit (publish)
			return m, tea.Quit
		case "esc":
			m.cancelled = true
			return m, tea.Quit
		}
	}

	var cmd tea.Cmd
	m.textarea, cmd = m.textarea.Update(msg)
	return m, cmd
}

func (m longFormModel) View() string {
	if m.width == 0 {
		return "Loading..."
	}

	var b strings.Builder

	// Title header
	if m.title != "" {
		b.WriteString(lfTitleStyle.Render("  📝 " + m.title))
		b.WriteString("\n\n")
	}

	// Textarea
	b.WriteString(m.textarea.View())
	b.WriteString("\n")

	// Status bar with counts
	text := m.textarea.Value()
	lines := strings.Count(text, "\n") + 1
	words := len(strings.Fields(text))
	chars := len(text)

	counts := lfCountStyle.Render(fmt.Sprintf("%d lines · %d words · %d chars", lines, words, chars))
	hint := lfStatusStyle.Render("  Ctrl+S to publish · Esc to cancel")

	b.WriteString(hint + "  " + counts)

	return b.String()
}
