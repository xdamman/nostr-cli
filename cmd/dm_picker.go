package cmd

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/nbd-wtf/go-nostr"
	"github.com/xdamman/nostr-cli/internal/cache"
	"github.com/xdamman/nostr-cli/internal/config"
	"github.com/xdamman/nostr-cli/internal/crypto"
	"github.com/xdamman/nostr-cli/internal/resolve"
)

// dmPickerEntry represents a selectable conversation in the picker.
type dmPickerEntry struct {
	Label            string // display name or alias
	Sublabel         string // npub or extra info
	CounterpartyHex  string
	CounterpartyNpub string
	LastMessageAt    nostr.Timestamp
	LastMessageText  string
}

type dmPickerModel struct {
	entries   []dmPickerEntry
	filtered  []int // indices into entries
	cursor    int
	filter    string
	cancelled bool
	width     int
	height    int
}

func newDMPickerModel(entries []dmPickerEntry) dmPickerModel {
	filtered := make([]int, len(entries))
	for i := range entries {
		filtered[i] = i
	}
	return dmPickerModel{
		entries:  entries,
		filtered: filtered,
	}
}

func (m dmPickerModel) Init() tea.Cmd {
	return nil
}

func (m dmPickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyEscape:
			m.cancelled = true
			return m, tea.Quit
		case tea.KeyEnter:
			if len(m.filtered) > 0 {
				return m, tea.Quit
			}
			return m, nil
		case tea.KeyUp:
			if m.cursor > 0 {
				m.cursor--
			}
			return m, nil
		case tea.KeyDown:
			if m.cursor < len(m.filtered)-1 {
				m.cursor++
			}
			return m, nil
		case tea.KeyBackspace, tea.KeyDelete:
			if len(m.filter) > 0 {
				m.filter = m.filter[:len(m.filter)-1]
				m.applyFilter()
			}
			return m, nil
		default:
			if msg.Type == tea.KeyRunes || msg.Type == tea.KeySpace {
				m.filter += msg.String()
				m.applyFilter()
				return m, nil
			}
		}
	}
	return m, nil
}

func (m *dmPickerModel) applyFilter() {
	if m.filter == "" {
		m.filtered = make([]int, len(m.entries))
		for i := range m.entries {
			m.filtered[i] = i
		}
	} else {
		q := strings.ToLower(m.filter)
		m.filtered = nil
		for i, e := range m.entries {
			if strings.Contains(strings.ToLower(e.Label), q) ||
				strings.Contains(strings.ToLower(e.Sublabel), q) ||
				strings.Contains(strings.ToLower(e.CounterpartyNpub), q) {
				m.filtered = append(m.filtered, i)
			}
		}
	}
	m.cursor = 0
}

var (
	pickerDimStyle      = lipgloss.NewStyle().Faint(true)
	pickerCyanStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
	pickerCyanBoldStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("6")).Bold(true)
	pickerGreenStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("2"))
	pickerYellowStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("3"))
)

func (m dmPickerModel) View() string {
	var b strings.Builder

	b.WriteString("Direct Messages\n\n")

	if m.filter != "" {
		b.WriteString(pickerDimStyle.Render("Filter: ") + pickerYellowStyle.Render(m.filter) + "\n\n")
	}

	if len(m.filtered) == 0 {
		if m.filter != "" {
			b.WriteString(pickerDimStyle.Render("No matches for \""+m.filter+"\"") + "\n")
		} else {
			b.WriteString(pickerDimStyle.Render("No conversations yet. Start one with: nostr dm <npub|alias>") + "\n")
		}
	} else {
		// How many items can we show
		maxVisible := m.height - 8 // header + footer + padding
		if maxVisible < 3 {
			maxVisible = 3
		}
		if maxVisible > len(m.filtered) {
			maxVisible = len(m.filtered)
		}

		// Scroll window
		start := 0
		if m.cursor >= maxVisible {
			start = m.cursor - maxVisible + 1
		}
		end := start + maxVisible
		if end > len(m.filtered) {
			end = len(m.filtered)
			start = end - maxVisible
			if start < 0 {
				start = 0
			}
		}

		for vi := start; vi < end; vi++ {
			idx := m.filtered[vi]
			e := m.entries[idx]

			cursor := "  "
			if vi == m.cursor {
				cursor = "→ "
			}

			timeAgo := formatTimeAgo(e.LastMessageAt)
			preview := truncateString(e.LastMessageText, 50)

			// One account per line with cursor, name, and timestamp
			var nameStyle lipgloss.Style
			if vi == m.cursor {
				nameStyle = pickerCyanBoldStyle
			} else {
				nameStyle = pickerDimStyle
			}

			// Format: cursor + name + spaces + timestamp
			nameWithCursor := cursor + nameStyle.Render(e.Label)
			if timeAgo != "" {
				timeStr := pickerDimStyle.Render(timeAgo)
				// Calculate spacing to right-align timestamp (approximate)
				nameLength := len(e.Label) + 2 // cursor + name
				if m.width > 0 && nameLength < m.width-15 {
					spaces := strings.Repeat(" ", m.width-nameLength-len(timeAgo))
					nameWithCursor += spaces + timeStr
				} else {
					nameWithCursor += "  " + timeStr
				}
			}

			b.WriteString(nameWithCursor + "\n")

			// Show last message preview below, indented, with timestamp
			if preview != "" {
				ts := ""
				if e.LastMessageAt > 0 {
					t := time.Unix(int64(e.LastMessageAt), 0)
					ts = pickerDimStyle.Render(t.Format("15:04")) + " "
				}
				b.WriteString("    " + ts + pickerDimStyle.Render(preview) + "\n")
			}
		}
	}

	b.WriteString("\n")
	b.WriteString(pickerDimStyle.Render("↑/↓ navigate · type to filter · enter select · esc quit") + "\n")

	return b.String()
}

func formatTimeAgo(ts nostr.Timestamp) string {
	if ts == 0 {
		return ""
	}
	t := time.Unix(int64(ts), 0)
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		m := int(d.Minutes())
		if m == 1 {
			return "1m ago"
		}
		return fmt.Sprintf("%dm ago", m)
	case d < 24*time.Hour:
		h := int(d.Hours())
		if h == 1 {
			return "1h ago"
		}
		return fmt.Sprintf("%dh ago", h)
	case d < 7*24*time.Hour:
		days := int(d.Hours() / 24)
		if days == 1 {
			return "yesterday"
		}
		return fmt.Sprintf("%dd ago", days)
	default:
		return t.Format("02 Jan")
	}
}

func truncateString(s string, maxLen int) string {
	// Replace newlines with spaces for preview
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.TrimSpace(s)
	if len(s) > maxLen {
		return s[:maxLen] + "…"
	}
	return s
}

// runDMPicker shows an interactive conversation picker and launches the selected DM.
func runDMPicker(npub string) error {
	cache.LoadProfileCache(npub)

	// Load conversations from cache
	convos, err := cache.ListDMConversations(npub)
	if err != nil {
		return fmt.Errorf("failed to list conversations: %w", err)
	}

	// Load aliases for reverse lookup
	aliases, _ := resolve.LoadAliases(npub)
	aliasReverse := make(map[string]string) // npub → alias
	for name, target := range aliases {
		aliasReverse[target] = name
	}

	// Build picker entries
	var entries []dmPickerEntry
	for _, c := range convos {
		// Determine display name: alias > cached profile name > truncated npub
		label := ""
		if alias, ok := aliasReverse[c.CounterpartyNpub]; ok {
			label = alias
		}
		if label == "" && c.CounterpartyHex != "" {
			label = cache.ResolveNameByHex(c.CounterpartyHex)
		}
		if label == "" {
			if len(c.CounterpartyNpub) > 20 {
				label = c.CounterpartyNpub[:20] + "..."
			} else {
				label = c.CounterpartyNpub
			}
		}

		sublabel := ""
		if alias, ok := aliasReverse[c.CounterpartyNpub]; ok && alias != label {
			sublabel = alias
		}

		// Decrypt last message preview if possible (best effort)
		preview := c.LastMessageText
		// For NIP-04 encrypted content, it will look like ciphertext — skip
		if strings.Contains(preview, "?iv=") {
			preview = ""
		}

		entries = append(entries, dmPickerEntry{
			Label:            label,
			Sublabel:         sublabel,
			CounterpartyHex:  c.CounterpartyHex,
			CounterpartyNpub: c.CounterpartyNpub,
			LastMessageAt:    c.LastMessageAt,
			LastMessageText:  preview,
		})
	}

	// Also add aliases that don't have conversations yet
	for name, target := range aliases {
		found := false
		for _, e := range entries {
			if e.CounterpartyNpub == target {
				found = true
				break
			}
		}
		if !found {
			entries = append(entries, dmPickerEntry{
				Label:            name,
				CounterpartyNpub: target,
			})
		}
	}

	if len(entries) == 0 {
		return showDMAliases(npub)
	}

	m := newDMPickerModel(entries)
	p := tea.NewProgram(m)
	finalModel, err := p.Run()
	if err != nil {
		return err
	}

	result := finalModel.(dmPickerModel)
	if result.cancelled || len(result.filtered) == 0 {
		return nil
	}

	selected := result.entries[result.filtered[result.cursor]]

	// Now launch the DM session with the selected counterparty
	targetHex := selected.CounterpartyHex
	if targetHex == "" {
		targetHex, err = resolve.Resolve(npub, selected.CounterpartyNpub)
		if err != nil {
			return err
		}
	}

	nsec, err := config.LoadNsec(npub)
	if err != nil {
		return err
	}
	skHex, err := crypto.NsecToHex(nsec)
	if err != nil {
		return err
	}
	myHex, err := crypto.NpubToHex(npub)
	if err != nil {
		return err
	}

	relays, err := config.LoadRelays(npub)
	if err != nil {
		return err
	}

	return interactiveDM(npub, skHex, myHex, targetHex, selected.Label, relays)
}


