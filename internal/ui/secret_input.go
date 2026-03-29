package ui

import (
	"os"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"golang.org/x/term"
)

// SecretInputConfig configures a masked secret input.
type SecretInputConfig struct {
	Prompt string
	Hint   string
}

// SecretInputResult holds the result of a secret input session.
type SecretInputResult struct {
	Value     string
	Cancelled bool
}

type secretModel struct {
	input     textinput.Model
	hint      string
	value     string
	cancelled bool
}

func newSecretModel(cfg SecretInputConfig) secretModel {
	ti := textinput.New()
	ti.Prompt = cfg.Prompt
	ti.EchoMode = textinput.EchoPassword
	ti.EchoCharacter = '•'
	ti.Focus()
	ti.CharLimit = 0

	return secretModel{
		input: ti,
		hint:  cfg.Hint,
	}
}

func (m secretModel) Init() tea.Cmd {
	return textinput.Blink
}

func (m secretModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyCtrlD, tea.KeyEscape:
			m.cancelled = true
			return m, tea.Quit
		case tea.KeyEnter:
			m.value = strings.TrimSpace(m.input.Value())
			return m, tea.Quit
		}
	}

	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m secretModel) View() string {
	var b strings.Builder
	b.WriteString(m.input.View())
	b.WriteString("\n")
	if m.hint != "" {
		b.WriteString(inlineDimStyle.Render("  " + m.hint))
		b.WriteString("\n")
	}
	return b.String()
}

// RunSecretInput runs a masked secret input in inline mode.
func RunSecretInput(cfg SecretInputConfig) SecretInputResult {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return SecretInputResult{Cancelled: true}
	}

	m := newSecretModel(cfg)
	p := tea.NewProgram(m, tea.WithoutSignalHandler())

	finalModel, err := p.Run()
	if err != nil {
		return SecretInputResult{Cancelled: true}
	}

	result := finalModel.(secretModel)
	if result.cancelled {
		return SecretInputResult{Cancelled: true}
	}
	return SecretInputResult{Value: result.value}
}
