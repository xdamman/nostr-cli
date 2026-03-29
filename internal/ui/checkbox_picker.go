package ui

import (
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"golang.org/x/term"
)

// CheckboxItem represents a single item in a checkbox picker.
type CheckboxItem struct {
	Label    string
	Sublabel string // e.g. ping result, shown dimmed
	Active   bool   // e.g. mark current/active item
}

// CheckboxPickerConfig configures the checkbox picker.
type CheckboxPickerConfig struct {
	Title   string
	Items   []CheckboxItem
	OnReady func(index int, statusCh chan<- string) // async status updates per item (e.g. ping)
}

// CheckboxPickerResult holds the result of a checkbox picker session.
type CheckboxPickerResult struct {
	Selected  []int // indices of selected items
	Cancelled bool
}

// statusUpdateMsg delivers an async status update for a specific item.
type statusUpdateMsg struct {
	index  int
	status string
}

// checkboxModel is the bubbletea model for the checkbox picker.
type checkboxModel struct {
	title     string
	items     []CheckboxItem
	checked   []bool
	sublabels []string
	cursor    int
	cancelled bool

	// channels for async status updates, one per item
	statusChs []chan string
}

func newCheckboxModel(cfg CheckboxPickerConfig) checkboxModel {
	n := len(cfg.Items)
	checked := make([]bool, n)
	sublabels := make([]string, n)
	statusChs := make([]chan string, n)

	for i, item := range cfg.Items {
		sublabels[i] = item.Sublabel
		statusChs[i] = make(chan string, 1)
	}

	return checkboxModel{
		title:     cfg.Title,
		items:     cfg.Items,
		checked:   checked,
		sublabels: sublabels,
		cursor:    0,
		statusChs: statusChs,
	}
}

// waitForStatus returns a tea.Cmd that waits on a status channel for a given index.
func waitForStatus(index int, ch <-chan string) tea.Cmd {
	return func() tea.Msg {
		status, ok := <-ch
		if !ok {
			return nil
		}
		return statusUpdateMsg{index: index, status: status}
	}
}

func (m checkboxModel) Init() tea.Cmd {
	// Start listening on all status channels
	cmds := make([]tea.Cmd, len(m.statusChs))
	for i, ch := range m.statusChs {
		cmds[i] = waitForStatus(i, ch)
	}
	return tea.Batch(cmds...)
}

func (m checkboxModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case statusUpdateMsg:
		m.sublabels[msg.index] = msg.status
		return m, nil

	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyEscape:
			m.cancelled = true
			return m, tea.Quit

		case tea.KeyEnter:
			return m, tea.Quit

		case tea.KeyUp:
			if m.cursor > 0 {
				m.cursor--
			}
			return m, nil

		case tea.KeyDown:
			if m.cursor < len(m.items)-1 {
				m.cursor++
			}
			return m, nil

		case tea.KeySpace:
			m.checked[m.cursor] = !m.checked[m.cursor]
			return m, nil

		default:
			switch msg.String() {
			case "k":
				if m.cursor > 0 {
					m.cursor--
				}
				return m, nil
			case "j":
				if m.cursor < len(m.items)-1 {
					m.cursor++
				}
				return m, nil
			}
		}
	}
	return m, nil
}

var (
	cbDimStyle   = lipgloss.NewStyle().Faint(true)
	cbCyanStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
	cbGreenStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
)

func (m checkboxModel) View() string {
	var b strings.Builder

	if m.title != "" {
		b.WriteString(m.title + "\n")
	}

	// Find max label length for alignment
	maxLen := 0
	for _, item := range m.items {
		if len(item.Label) > maxLen {
			maxLen = len(item.Label)
		}
	}

	for i, item := range m.items {
		// Cursor
		cursor := "  "
		if i == m.cursor {
			cursor = "→ "
		}

		// Checkbox
		var check string
		if m.checked[i] {
			check = cbGreenStyle.Render("[✓]")
		} else {
			check = cbDimStyle.Render("[ ]")
		}

		// Label — highlighted if current
		label := item.Label
		padded := label + strings.Repeat(" ", maxLen-len(label))
		var labelStr string
		if i == m.cursor {
			labelStr = cbCyanStyle.Render(padded)
		} else {
			labelStr = cbDimStyle.Render(padded)
		}

		// Sublabel
		sublabel := ""
		if m.sublabels[i] != "" {
			sublabel = "  " + cbDimStyle.Render(m.sublabels[i])
		}

		b.WriteString(fmt.Sprintf("  %s%s %s%s\n", cursor, check, labelStr, sublabel))
	}

	// Hint
	selectedCount := 0
	for _, c := range m.checked {
		if c {
			selectedCount++
		}
	}
	hint := "  ↑/↓ navigate, space toggle, enter confirm, esc cancel"
	b.WriteString("\n" + cbDimStyle.Render(hint) + "\n")

	return b.String()
}

// RunCheckboxPicker runs the checkbox picker in inline mode.
func RunCheckboxPicker(cfg CheckboxPickerConfig) CheckboxPickerResult {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return CheckboxPickerResult{Cancelled: true}
	}

	m := newCheckboxModel(cfg)

	// Trigger OnReady callbacks to start async work
	if cfg.OnReady != nil {
		for i := range cfg.Items {
			cfg.OnReady(i, m.statusChs[i])
		}
	}

	p := tea.NewProgram(m, tea.WithoutSignalHandler())

	finalModel, err := p.Run()
	if err != nil {
		return CheckboxPickerResult{Cancelled: true}
	}

	result := finalModel.(checkboxModel)
	if result.cancelled {
		return CheckboxPickerResult{Cancelled: true}
	}

	var selected []int
	for i, c := range result.checked {
		if c {
			selected = append(selected, i)
		}
	}
	return CheckboxPickerResult{Selected: selected}
}
