package ui

import (
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"golang.org/x/term"
)

// ListPickerItem represents a single item in a list picker.
type ListPickerItem struct {
	Label    string
	Sublabel string
	Active   bool // marks current/active item (e.g. currently selected account)
}

// ListPickerConfig configures the list picker.
type ListPickerConfig struct {
	Title  string
	Items  []ListPickerItem
	Footer string
}

// ListPickerResult holds the result of a list picker session.
type ListPickerResult struct {
	Selected  int // index of selected item, -1 if cancelled
	Cancelled bool
}

type listPickerModel struct {
	title     string
	items     []ListPickerItem
	footer    string
	cursor    int
	cancelled bool
}

func newListPickerModel(cfg ListPickerConfig) listPickerModel {
	return listPickerModel{
		title:  cfg.Title,
		items:  cfg.Items,
		footer: cfg.Footer,
		cursor: 0,
	}
}

func (m listPickerModel) Init() tea.Cmd {
	return nil
}

func (m listPickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
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
		default:
			switch msg.String() {
			case "k":
				if m.cursor > 0 {
					m.cursor--
				}
			case "j":
				if m.cursor < len(m.items)-1 {
					m.cursor++
				}
			case "q":
				m.cancelled = true
				return m, tea.Quit
			}
		}
	}
	return m, nil
}

var (
	lpDimStyle   = lipgloss.NewStyle().Faint(true)
	lpCyanStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
	lpBoldStyle  = lipgloss.NewStyle().Bold(true)
)

func (m listPickerModel) View() string {
	var b strings.Builder

	if m.title != "" {
		b.WriteString(m.title + "\n\n")
	}

	for i, item := range m.items {
		cursor := "   "
		if i == m.cursor {
			cursor = " → "
		}

		label := item.Label
		if item.Sublabel != "" {
			label += "  " + item.Sublabel
		}

		var line string
		if i == m.cursor {
			line = lpCyanStyle.Render(label)
		} else {
			line = lpDimStyle.Render(label)
		}

		active := ""
		if item.Active {
			active = " " + lpBoldStyle.Render("(active)")
		}

		b.WriteString(fmt.Sprintf("%s%s%s\n", cursor, line, active))
	}

	if m.footer != "" {
		b.WriteString("\n  " + lpDimStyle.Render(m.footer) + "\n")
	}

	b.WriteString("\n" + lpDimStyle.Render("  ↑/↓ navigate, enter select, esc cancel") + "\n")

	return b.String()
}

// RunListPicker runs the list picker in inline mode.
func RunListPicker(cfg ListPickerConfig) ListPickerResult {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return ListPickerResult{Selected: -1, Cancelled: true}
	}

	m := newListPickerModel(cfg)
	p := tea.NewProgram(m, tea.WithoutSignalHandler())

	finalModel, err := p.Run()
	if err != nil {
		return ListPickerResult{Selected: -1, Cancelled: true}
	}

	result := finalModel.(listPickerModel)
	if result.cancelled {
		return ListPickerResult{Selected: -1, Cancelled: true}
	}
	return ListPickerResult{Selected: result.cursor}
}
