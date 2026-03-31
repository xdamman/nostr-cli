package ui

import (
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/knz/bubbline/complete"
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

// -- Mention completions for bubbline --

// mentionCompletions implements editline.Completions for @ mentions.
type mentionCompletions struct {
	candidates []MentionCandidate
	start      int // start column of the @word on the line
	end        int // end column (cursor position)
	cursor     int // cursor column
}

func (mc *mentionCompletions) NumCategories() int            { return 1 }
func (mc *mentionCompletions) CategoryTitle(_ int) string    { return "mentions" }
func (mc *mentionCompletions) NumEntries(_ int) int          { return len(mc.candidates) }
func (mc *mentionCompletions) Entry(_, i int) complete.Entry { return mentionEntry{mc, i} }
func (mc *mentionCompletions) Candidate(e complete.Entry) editline.Candidate {
	return e.(mentionEntry)
}

type mentionEntry struct {
	mc *mentionCompletions
	i  int
}

func (e mentionEntry) Title() string       { return "@" + e.mc.candidates[e.i].DisplayName }
func (e mentionEntry) Description() string { return TruncateNpub(e.mc.candidates[e.i].Npub) }
func (e mentionEntry) Replacement() string { return "@" + e.mc.candidates[e.i].DisplayName }
func (e mentionEntry) MoveRight() int      { return e.mc.end - e.mc.cursor }
func (e mentionEntry) DeleteLeft() int     { return e.mc.end - e.mc.start }

// MentionAutoComplete builds the AutoCompleteFn for bubbline.
// Exported so DM and other commands can wire it into their own editline models.
func MentionAutoComplete(candidates []MentionCandidate) editline.AutoCompleteFn {
	return func(entireInput [][]rune, line, col int) (string, editline.Completions) {
		if len(candidates) == 0 || line >= len(entireInput) {
			return "", nil
		}

		lineRunes := entireInput[line]
		if col > len(lineRunes) {
			col = len(lineRunes)
		}

		// Find @ before cursor on this line
		atIdx := -1
		for i := col - 1; i >= 0; i-- {
			r := lineRunes[i]
			if r == ' ' || r == '\n' || r == '\t' {
				break
			}
			if r == '@' {
				// @ must be at start of line or preceded by space
				if i == 0 || lineRunes[i-1] == ' ' || lineRunes[i-1] == '\n' || lineRunes[i-1] == '\t' {
					atIdx = i
				}
				break
			}
		}

		if atIdx < 0 {
			return "", nil
		}

		// Extract query (after @, before cursor)
		query := string(lineRunes[atIdx+1 : col])

		// Find end of the @word (past cursor)
		endIdx := col
		for endIdx < len(lineRunes) {
			r := lineRunes[endIdx]
			if r == ' ' || r == '\n' || r == '\t' {
				break
			}
			endIdx++
		}

		results := FilterCandidates(candidates, query)
		if len(results) == 0 {
			return "", nil
		}

		return "", &mentionCompletions{
			candidates: results,
			start:      atIdx,
			end:        endIdx,
			cursor:     col,
		}
	}
}

// -- Editline input model --

type editlineInputModel struct {
	editor *editline.Model
	hint   string

	// result
	value     string
	cancelled bool

	// mention tracking (for p-tag generation at send time)
	mentionCandidates []MentionCandidate
	selectedMentions  []MentionCandidate

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

	// Wire @ mention autocomplete via Tab
	if len(cfg.Candidates) > 0 {
		ed.AutoComplete = MentionAutoComplete(cfg.Candidates)
	}

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
		// Collect selected mentions from the final text
		m.selectedMentions = ExtractMentionsFromText(m.value, m.mentionCandidates)
		return m, tea.Quit

	case tea.KeyMsg:
		if msg.Type == tea.KeyCtrlC || msg.Type == tea.KeyCtrlD || msg.Type == tea.KeyEscape {
			m.cancelled = true
			return m, tea.Quit
		}
	}

	_, cmd := m.editor.Update(msg)
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

	// Hint line
	if m.hint != "" {
		b.WriteString(inlineDimStyle.Render("  " + m.hint))
		b.WriteString("\n")
	}

	return b.String()
}

// ExtractMentionsFromText scans the final text for @DisplayName patterns
// and returns matching MentionCandidates for p-tag generation.
func ExtractMentionsFromText(text string, candidates []MentionCandidate) []MentionCandidate {
	var found []MentionCandidate
	seen := make(map[string]bool)
	for _, c := range candidates {
		tag := "@" + c.DisplayName
		if strings.Contains(text, tag) && !seen[c.PubHex] {
			seen[c.PubHex] = true
			found = append(found, c)
		}
	}
	return found
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
