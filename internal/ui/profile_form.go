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

// ProfileFormConfig configures the multi-field profile form.
type ProfileFormConfig struct {
	Title  string
	Fields []ProfileField
}

// ProfileField describes a single field in the profile form.
type ProfileField struct {
	Label       string // "Username", "Display name", etc.
	Key         string // "name", "display_name", etc.
	Value       string // current value (for editing)
	Hint        string // helper text shown dimmed below the field
	Placeholder string // shown when empty
}

// ProfileFormResult holds the result of the profile form.
type ProfileFormResult struct {
	Values    map[string]string // key → value
	Cancelled bool
}

// RunProfileForm runs an inline bubbletea form with multiple text fields.
// Returns the filled values or Cancelled=true if the user pressed ESC/Ctrl+C.
func RunProfileForm(cfg ProfileFormConfig) ProfileFormResult {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return ProfileFormResult{Cancelled: true}
	}

	m := newProfileFormModel(cfg)
	p := tea.NewProgram(m, tea.WithOutput(os.Stderr))
	finalModel, err := p.Run()
	if err != nil {
		return ProfileFormResult{Cancelled: true}
	}

	fm := finalModel.(profileFormModel)
	if fm.cancelled {
		return ProfileFormResult{Cancelled: true}
	}

	values := make(map[string]string, len(cfg.Fields))
	for i, f := range cfg.Fields {
		values[f.Key] = fm.inputs[i].Value()
	}
	return ProfileFormResult{Values: values}
}

// --- bubbletea model ---

type profileFormModel struct {
	title     string
	fields    []ProfileField
	inputs    []textinput.Model
	focused   int
	submitted bool
	cancelled bool
	maxLabel  int // length of longest label for alignment
}

func newProfileFormModel(cfg ProfileFormConfig) profileFormModel {
	inputs := make([]textinput.Model, len(cfg.Fields))
	maxLabel := 0
	for _, f := range cfg.Fields {
		if len(f.Label) > maxLabel {
			maxLabel = len(f.Label)
		}
	}

	for i, f := range cfg.Fields {
		ti := textinput.New()
		ti.Prompt = ""
		ti.CharLimit = 500
		if f.Value != "" {
			ti.SetValue(f.Value)
		}
		if f.Placeholder != "" {
			ti.Placeholder = f.Placeholder
		}
		ti.PlaceholderStyle = lipgloss.NewStyle().Faint(true)
		if i == 0 {
			ti.Focus()
		}
		inputs[i] = ti
	}

	return profileFormModel{
		title:    cfg.Title,
		fields:   cfg.Fields,
		inputs:   inputs,
		focused:  0,
		maxLabel: maxLabel,
	}
}

func (m profileFormModel) Init() tea.Cmd {
	return nil
}

func (m profileFormModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			m.cancelled = true
			return m, tea.Quit

		case tea.KeyCtrlS:
			m.submitted = true
			return m, tea.Quit

		case tea.KeyEnter:
			if m.focused == len(m.inputs)-1 {
				// Last field → submit
				m.submitted = true
				return m, tea.Quit
			}
			// Otherwise move to next field
			return m.focusNext()

		case tea.KeyTab, tea.KeyDown:
			return m.focusNext()

		case tea.KeyShiftTab, tea.KeyUp:
			return m.focusPrev()
		}
	}

	// Update only the focused input
	var cmd tea.Cmd
	m.inputs[m.focused], cmd = m.inputs[m.focused].Update(msg)
	return m, cmd
}

func (m profileFormModel) focusNext() (tea.Model, tea.Cmd) {
	m.inputs[m.focused].Blur()
	m.focused = (m.focused + 1) % len(m.inputs)
	return m, m.inputs[m.focused].Focus()
}

func (m profileFormModel) focusPrev() (tea.Model, tea.Cmd) {
	m.inputs[m.focused].Blur()
	m.focused = (m.focused - 1 + len(m.inputs)) % len(m.inputs)
	return m, m.inputs[m.focused].Focus()
}

func (m profileFormModel) View() string {
	var b strings.Builder

	titleStyle := lipgloss.NewStyle().Bold(true)
	activeLabelStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Bold(true) // cyan bold
	inactiveLabelStyle := lipgloss.NewStyle()
	hintStyle := lipgloss.NewStyle().Faint(true)
	footerStyle := lipgloss.NewStyle().Faint(true)

	// Title
	b.WriteString("\n")
	b.WriteString(titleStyle.Render(m.title))
	b.WriteString("\n\n")

	// Fields
	for i, f := range m.fields {
		// Right-align label
		padding := m.maxLabel - len(f.Label)
		paddedLabel := strings.Repeat(" ", padding) + f.Label + ":"

		var labelStr string
		if i == m.focused {
			labelStr = activeLabelStyle.Render(paddedLabel)
		} else {
			labelStr = inactiveLabelStyle.Render(paddedLabel)
		}

		b.WriteString(fmt.Sprintf("  %s %s\n", labelStr, m.inputs[i].View()))

		// Hint (only for the focused field, if it has one)
		if f.Hint != "" && i == m.focused {
			hintPad := strings.Repeat(" ", m.maxLabel+2)
			b.WriteString(fmt.Sprintf("  %s%s\n", hintPad, hintStyle.Render("ℹ "+f.Hint)))
		}
	}

	// Footer
	b.WriteString("\n")
	b.WriteString(footerStyle.Render("  Tab/↓ next field · Enter to save · ESC to cancel"))
	b.WriteString("\n")

	return b.String()
}
