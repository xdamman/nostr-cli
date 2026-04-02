package cmd

import (
	"errors"
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

// errBackToPicker is a sentinel error returned by interactiveDM when the user
// presses backspace on empty input to go back to the conversation picker.
var errBackToPicker = errors.New("back to picker")

// dmPickerEntry represents a selectable conversation in the picker.
type dmPickerEntry struct {
	Label            string // display name or alias
	Sublabel         string // npub or extra info
	ShortNpub        string // shortened npub (npub1...xxxx)
	CounterpartyHex  string
	CounterpartyNpub string
	LastMessageAt    nostr.Timestamp
	LastMessageText  string
	IsIncoming       bool // true if this is an incoming request (we never sent a message)
}

// pickerResult describes what the user selected in the picker.
type pickerResult int

const (
	pickerResultCancelled pickerResult = iota
	pickerResultConversation
	pickerResultIncoming // user selected the "+N incoming requests" row
)

type dmPickerModel struct {
	entries          []dmPickerEntry
	filtered         []int // indices into entries
	cursor           int
	filter           string
	cancelled        bool
	width            int
	height           int
	accountName      string       // current account display name for prompt
	title            string       // optional title shown above entries (e.g. "Incoming Requests")
	incomingRequests int          // count of incoming DM requests (never replied to)
	result           pickerResult
	backOnEmpty      bool         // if true, backspace on empty filter cancels (go back)
}

func newDMPickerModel(entries []dmPickerEntry, accountName string, incomingRequests int) dmPickerModel {
	filtered := make([]int, len(entries))
	for i := range entries {
		filtered[i] = i
	}
	return dmPickerModel{
		entries:          entries,
		filtered:         filtered,
		accountName:      accountName,
		incomingRequests: incomingRequests,
	}
}

// totalRows returns the number of selectable rows (entries + incoming row if present).
func (m dmPickerModel) totalRows() int {
	n := len(m.filtered)
	if m.incomingRequests > 0 && m.filter == "" {
		n++ // extra row for incoming requests
	}
	return n
}

// isOnIncomingRow returns true if the cursor is on the incoming requests row.
func (m dmPickerModel) isOnIncomingRow() bool {
	return m.incomingRequests > 0 && m.filter == "" && m.cursor == len(m.filtered)
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
			if m.isOnIncomingRow() {
				m.result = pickerResultIncoming
				return m, tea.Quit
			}
			if len(m.filtered) > 0 {
				m.result = pickerResultConversation
				return m, tea.Quit
			}
			return m, nil
		case tea.KeyUp:
			if m.cursor > 0 {
				m.cursor--
			}
			return m, nil
		case tea.KeyDown:
			if m.cursor < m.totalRows()-1 {
				m.cursor++
			}
			return m, nil
		case tea.KeyBackspace, tea.KeyDelete:
			if len(m.filter) > 0 {
				m.filter = m.filter[:len(m.filter)-1]
				m.applyFilter()
			} else if m.backOnEmpty {
				m.cancelled = true
				return m, tea.Quit
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

	// Show account prompt at the top
	if m.accountName != "" {
		b.WriteString(pickerGreenStyle.Render(m.accountName) + "> ")
	}
	if m.filter != "" {
		b.WriteString(pickerYellowStyle.Render(m.filter))
	}
	b.WriteString("\n")
	if m.title != "" {
		b.WriteString(pickerDimStyle.Render(m.title))
	}
	b.WriteString("\n")

	total := m.totalRows()
	if total == 0 {
		if m.filter != "" {
			b.WriteString(pickerDimStyle.Render("No matches for \""+m.filter+"\"") + "\n")
		} else {
			b.WriteString(pickerDimStyle.Render("No conversations yet. Start one with: nostr dm <npub|alias>") + "\n")
		}
	} else {
		// Calculate how many entries fit. Entries take 1 or 2 lines depending on preview.
		// Reserve lines for header (prompt + title), footer (hints), padding.
		overhead := 5
		if m.incomingRequests > 0 && m.filter == "" {
			overhead += 2
		}
		availLines := m.height - overhead
		if availLines < 3 {
			availLines = 3
		}

		// Count how many entries fit starting from a given position
		countFit := func(from int) int {
			lines := 0
			count := 0
			for i := from; i < total; i++ {
				entryLines := 1
				if i < len(m.filtered) {
					idx := m.filtered[i]
					if truncateString(m.entries[idx].LastMessageText, 60) != "" {
						entryLines = 2
					}
				} else {
					entryLines = 2 // incoming requests row
				}
				if lines+entryLines > availLines && count > 0 {
					break
				}
				lines += entryLines
				count++
			}
			return count
		}

		// Determine scroll window around cursor
		maxVisible := countFit(0)
		if maxVisible > total {
			maxVisible = total
		}

		start := 0
		if m.cursor >= maxVisible {
			// Scroll so cursor is visible: find start where cursor fits in window
			start = m.cursor - maxVisible + 1
			// Recount from new start since entry heights may differ
			maxVisible = countFit(start)
			for start > 0 && m.cursor >= start+maxVisible {
				start++
				maxVisible = countFit(start)
			}
		}
		end := start + maxVisible
		if end > total {
			end = total
			start = end - maxVisible
			if start < 0 {
				start = 0
			}
		}

		for vi := start; vi < end; vi++ {
			selected := vi == m.cursor

			// Check if this is the incoming requests row (last row, after all entries)
			if vi == len(m.filtered) && m.incomingRequests > 0 && m.filter == "" {
				cursor := "  "
				if selected {
					cursor = "→ "
				}
				label := fmt.Sprintf("+%d incoming request", m.incomingRequests)
				if m.incomingRequests > 1 {
					label += "s"
				}
				var style lipgloss.Style
				if selected {
					style = pickerCyanBoldStyle
				} else {
					style = pickerYellowStyle
				}
				b.WriteString("\n" + cursor + style.Render(label) + "\n")
				continue
			}

			idx := m.filtered[vi]
			e := m.entries[idx]

			cursor := "  "
			if selected {
				cursor = "→ "
			}

			timeAgo := formatTimeAgo(e.LastMessageAt)

			// Name styling: cyan bold when selected, white when not
			var nameStyle lipgloss.Style
			if selected {
				nameStyle = pickerCyanBoldStyle
			} else {
				nameStyle = lipgloss.NewStyle().Bold(true)
			}

			// Build first line: cursor + name + shortNpub + right-aligned timestamp
			line1 := cursor + nameStyle.Render(e.Label)
			if e.ShortNpub != "" {
				line1 += " " + pickerDimStyle.Render(e.ShortNpub)
			}

			if timeAgo != "" {
				timeStr := pickerDimStyle.Render(timeAgo)
				// Calculate visible length for spacing (approximate)
				nameLen := 2 + len(e.Label)
				if e.ShortNpub != "" {
					nameLen += 1 + len(e.ShortNpub)
				}
				if m.width > 0 && nameLen < m.width-len(timeAgo)-2 {
					spaces := strings.Repeat(" ", m.width-nameLen-len(timeAgo))
					line1 += spaces + timeStr
				} else {
					line1 += "  " + timeStr
				}
			}

			b.WriteString(line1 + "\n")

			// Show last message preview only if there is one
			preview := truncateString(e.LastMessageText, 60)
			if preview != "" {
				b.WriteString("    " + pickerDimStyle.Render(preview) + "\n")
			}
		}
	}

	b.WriteString("\n")
	b.WriteString(pickerDimStyle.Render("↑/↓ navigate · type to filter · enter select · esc/ctrl+c quit") + "\n")

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

// buildPickerEntries builds the list of picker entries from conversations and aliases.
// If domainUsers is non-nil, those users are merged into the list.
// Returns (conversations, incomingRequests) — incoming requests are DMs from people we never replied to.
func buildPickerEntries(npub string, myHex string, domainUsers map[string]string, domain string) ([]dmPickerEntry, []dmPickerEntry) {
	// Load conversations from cache
	convos, _ := cache.ListDMConversations(npub, myHex)

	// Load aliases for reverse lookup
	aliases, _ := resolve.LoadAliases(npub)
	aliasReverse := make(map[string]string) // npub → alias
	for name, target := range aliases {
		aliasReverse[target] = name
	}

	// Track which hex pubkeys we've already added (for dedup with domain users)
	seenHex := make(map[string]bool)

	// Build picker entries from conversations, split into conversations and incoming requests
	var conversations []dmPickerEntry
	var incoming []dmPickerEntry
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

		entry := dmPickerEntry{
			Label:            label,
			Sublabel:         sublabel,
			ShortNpub:        shortenNpub(c.CounterpartyNpub),
			CounterpartyHex:  c.CounterpartyHex,
			CounterpartyNpub: c.CounterpartyNpub,
			LastMessageAt:    c.LastMessageAt,
			LastMessageText:  preview,
			IsIncoming:       !c.HasSentMessage,
		}

		if c.CounterpartyHex != "" {
			seenHex[c.CounterpartyHex] = true
		}

		if !c.HasSentMessage {
			incoming = append(incoming, entry)
		} else {
			conversations = append(conversations, entry)
		}
	}

	// Also add aliases that don't have conversations yet
	for name, target := range aliases {
		found := false
		for _, e := range conversations {
			if e.CounterpartyNpub == target {
				found = true
				break
			}
		}
		if !found {
			for _, e := range incoming {
				if e.CounterpartyNpub == target {
					found = true
					break
				}
			}
		}
		if !found {
			// Resolve hex for dedup tracking
			hex, _ := resolve.Resolve(npub, target)
			if hex != "" {
				seenHex[hex] = true
			}
			conversations = append(conversations, dmPickerEntry{
				Label:            name,
				ShortNpub:        shortenNpub(target),
				CounterpartyNpub: target,
				CounterpartyHex:  hex,
			})
		}
	}

	// Merge domain users if provided
	if domainUsers != nil {
		for name, hex := range domainUsers {
			if seenHex[hex] {
				continue
			}
			seenHex[hex] = true
			label := fmt.Sprintf("%s@%s", name, domain)
			conversations = append(conversations, dmPickerEntry{
				Label:           label,
				CounterpartyHex: hex,
			})
		}
	}

	return conversations, incoming
}

// shortenNpub returns "npub1...xxxx" from a full npub, or empty string if too short.
func shortenNpub(npub string) string {
	if len(npub) < 12 {
		return npub
	}
	return npub[:5] + "..." + npub[len(npub)-4:]
}

// runIncomingPicker shows the incoming requests list. Returns the selected entry
// or nil if cancelled/backspaced. The second return value is true if the user
// pressed backspace on empty filter (go back to main picker).
func runIncomingPicker(incoming []dmPickerEntry, accountName string) (*dmPickerEntry, bool) {
	m := newDMPickerModel(incoming, accountName, 0)
	m.title = "incoming requests"
	m.backOnEmpty = true
	p := tea.NewProgram(m)
	finalModel, err := p.Run()
	if err != nil {
		return nil, false
	}
	result := finalModel.(dmPickerModel)
	if result.cancelled || len(result.filtered) == 0 || result.result != pickerResultConversation {
		return nil, true
	}
	selected := result.entries[result.filtered[result.cursor]]
	return &selected, false
}

// runDMPicker shows an interactive conversation picker and launches the selected DM.
// If domain is non-empty, domain users from NIP-05 are merged into the list.
func runDMPicker(npub string, domain string) error {
	cache.LoadProfileCache(npub)

	accountName := resolveProfileName(npub)
	if accountName == "" {
		if len(npub) > 16 {
			accountName = npub[:16] + "..."
		} else {
			accountName = npub
		}
	}

	// Load credentials once for the session
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

	// Fetch domain users if a domain was specified
	var domainUsers map[string]string
	if domain != "" {
		domainUsers, err = fetchDomainNostrJSON(domain)
		if err != nil {
			return showDomainError(domain)
		}
	}

	// Loop: picker → DM → picker (on backspace from empty input)
	for {
		var conversations []dmPickerEntry
		var incomingReqs []dmPickerEntry

		if domain != "" {
			// Domain mode: only show users from that domain, no incoming requests
			for name, hex := range domainUsers {
				label := fmt.Sprintf("%s@%s", name, domain)
				conversations = append(conversations, dmPickerEntry{
					Label:           label,
					CounterpartyHex: hex,
				})
			}
		} else {
			conversations, incomingReqs = buildPickerEntries(npub, myHex, nil, "")
		}

		if len(conversations) == 0 && len(incomingReqs) == 0 {
			return showDMAliases(npub)
		}

		m := newDMPickerModel(conversations, accountName, len(incomingReqs))
		if domain != "" {
			m.title = "Select a user to start a conversation with:"
		} else {
			m.title = "Select a conversation:"
		}
		p := tea.NewProgram(m)
		finalModel, err := p.Run()
		if err != nil {
			return err
		}

		result := finalModel.(dmPickerModel)
		if result.cancelled {
			return nil
		}

		if result.result == pickerResultIncoming {
			// Show incoming requests sub-picker
			selected, back := runIncomingPicker(incomingReqs, accountName)
			if back || selected == nil {
				continue // go back to main picker
			}
			targetHex := selected.CounterpartyHex
			if targetHex == "" {
				targetHex, err = resolve.Resolve(npub, selected.CounterpartyNpub)
				if err != nil {
					return err
				}
			}
			err = interactiveDM(npub, skHex, myHex, targetHex, selected.Label, relays)
			if errors.Is(err, errBackToPicker) {
				continue
			}
			return err
		}

		if result.result != pickerResultConversation || len(result.filtered) == 0 {
			return nil
		}

		selected := result.entries[result.filtered[result.cursor]]

		// Resolve target hex
		targetHex := selected.CounterpartyHex
		if targetHex == "" {
			targetHex, err = resolve.Resolve(npub, selected.CounterpartyNpub)
			if err != nil {
				return err
			}
		}

		err = interactiveDM(npub, skHex, myHex, targetHex, selected.Label, relays)
		if errors.Is(err, errBackToPicker) {
			continue // go back to picker
		}
		return err
	}
}


