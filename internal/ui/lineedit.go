package ui

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"unicode"

	"github.com/fatih/color"
	"golang.org/x/term"
)

// LineEditor provides a raw-terminal line editor with a hint line below.
// It supports cursor movement, word navigation, and dynamic hint updates.
type LineEditor struct {
	Prompt    string // ANSI-colored prompt prefix (e.g. "name> ")
	PromptLen int    // visible length of prompt (excluding ANSI)
	hint      string
	hintMu    sync.Mutex
	fd        int
	buf       []rune
	pos       int // cursor position in buf (rune index)
	termWidth int

	// Mention autocomplete
	MentionCandidates []MentionCandidate // set before ReadLine
	SelectedMentions  []MentionCandidate // populated on confirm, read by caller

	// internal mention state
	inMention      bool
	mentionStart   int    // rune index where '@' was typed
	mentionQuery   string
	mentionResults []MentionCandidate
	mentionIdx     int
	mentionLines   int // number of dropdown lines rendered (for cleanup)
}

// NewLineEditor creates a line editor. Returns nil if stdin is not a terminal.
func NewLineEditor(prompt string, promptLen int, hint string) *LineEditor {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return nil
	}
	w := 80
	if tw, _, err := term.GetSize(fd); err == nil && tw > 0 {
		w = tw
	}
	return &LineEditor{
		Prompt:    prompt,
		PromptLen: promptLen,
		hint:      hint,
		fd:        fd,
		termWidth: w,
	}
}

// ReadLine reads a single line of input with full editing support.
// Returns the entered text, or empty string if cancelled (Ctrl+C/Ctrl+D).
func (e *LineEditor) ReadLine() (string, bool) {
	oldState, err := term.MakeRaw(e.fd)
	if err != nil {
		return "", false
	}
	defer term.Restore(e.fd, oldState)

	e.buf = nil
	e.pos = 0
	e.render()

	b := make([]byte, 6)
	for {
		n, err := os.Stdin.Read(b)
		if err != nil || n == 0 {
			return "", false
		}

		// If in mention mode, intercept navigation keys
		if e.inMention {
			handled := false
			switch {
			case n == 3 && b[0] == 27 && b[1] == '[' && b[2] == 'A': // Up arrow
				if e.mentionIdx > 0 {
					e.mentionIdx--
				}
				e.render()
				handled = true

			case n == 3 && b[0] == 27 && b[1] == '[' && b[2] == 'B': // Down arrow
				if e.mentionIdx < len(e.mentionResults)-1 {
					e.mentionIdx++
				}
				e.render()
				handled = true

			case n == 1 && b[0] == 9: // Tab — confirm selection
				e.confirmMention()
				e.render()
				handled = true

			case n == 1 && b[0] == 13: // Enter — confirm selection if dropdown open
				if len(e.mentionResults) > 0 {
					e.confirmMention()
					e.render()
					handled = true
				}

			case n == 1 && b[0] == 27: // Escape — close dropdown
				e.closeMentionDropdown()
				e.render()
				handled = true
			}
			if handled {
				continue
			}
		}

		switch {
		case n == 1 && b[0] == 13: // Enter
			e.closeMentionDropdown()
			e.clearHint()
			fmt.Print("\r\n")
			return string(e.buf), true

		case n == 1 && b[0] == 3: // Ctrl+C
			e.closeMentionDropdown()
			e.clearHint()
			fmt.Print("\r\n")
			return "", false

		case n == 1 && b[0] == 4: // Ctrl+D
			if len(e.buf) == 0 {
				e.closeMentionDropdown()
				e.clearHint()
				fmt.Print("\r\n")
				return "", false
			}

		case n == 1 && b[0] == 1: // Ctrl+A — beginning of line
			e.pos = 0
			e.closeMentionDropdown()
			e.render()

		case n == 1 && b[0] == 5: // Ctrl+E — end of line
			e.pos = len(e.buf)
			e.render()

		case n == 1 && b[0] == 21: // Ctrl+U — clear line
			e.buf = nil
			e.pos = 0
			e.closeMentionDropdown()
			e.render()

		case n == 1 && b[0] == 11: // Ctrl+K — kill to end of line
			e.buf = e.buf[:e.pos]
			e.closeMentionDropdown()
			e.render()

		case n == 1 && (b[0] == 127 || b[0] == 8): // Backspace
			if e.pos > 0 {
				e.buf = append(e.buf[:e.pos-1], e.buf[e.pos:]...)
				e.pos--
				e.updateMentionState()
				e.render()
			}

		case n == 3 && b[0] == 27 && b[1] == '[' && b[2] == 'D': // Left arrow
			if e.pos > 0 {
				e.pos--
				e.updateMentionState()
				e.render()
			}

		case n == 3 && b[0] == 27 && b[1] == '[' && b[2] == 'C': // Right arrow
			if e.pos < len(e.buf) {
				e.pos++
				e.updateMentionState()
				e.render()
			}

		case n == 3 && b[0] == 27 && b[1] == '[' && b[2] == 'H': // Home
			e.pos = 0
			e.closeMentionDropdown()
			e.render()

		case n == 3 && b[0] == 27 && b[1] == '[' && b[2] == 'F': // End
			e.pos = len(e.buf)
			e.render()

		case n >= 2 && b[0] == 27 && b[1] == 'b': // Alt+Left — word left
			e.pos = e.wordLeft()
			e.updateMentionState()
			e.render()

		case n >= 2 && b[0] == 27 && b[1] == 'f': // Alt+Right — word right
			e.pos = e.wordRight()
			e.updateMentionState()
			e.render()

		case n >= 6 && b[0] == 27 && b[1] == '[' && b[2] == '1' && b[3] == ';' && b[4] == '3' && b[5] == 'D':
			e.pos = e.wordLeft()
			e.updateMentionState()
			e.render()

		case n >= 6 && b[0] == 27 && b[1] == '[' && b[2] == '1' && b[3] == ';' && b[4] == '3' && b[5] == 'C':
			e.pos = e.wordRight()
			e.updateMentionState()
			e.render()

		case n == 1 && b[0] >= 32 && b[0] < 127: // Printable ASCII
			ch := rune(b[0])
			e.insert(ch)
			if ch == '@' && len(e.MentionCandidates) > 0 {
				// Check if @ is at start of line or preceded by space
				if e.pos == 1 || (e.pos > 1 && e.buf[e.pos-2] == ' ') {
					e.inMention = true
					e.mentionStart = e.pos - 1 // index of '@'
					e.mentionQuery = ""
					e.mentionIdx = 0
					e.mentionResults = FilterCandidates(e.MentionCandidates, "")
				}
			} else if e.inMention {
				if ch == ' ' {
					e.closeMentionDropdown()
				} else {
					e.mentionQuery = string(e.buf[e.mentionStart+1 : e.pos])
					e.mentionResults = FilterCandidates(e.MentionCandidates, e.mentionQuery)
					e.mentionIdx = 0
					if len(e.mentionResults) == 0 {
						e.closeMentionDropdown()
					}
				}
			}
			e.render()

		case n >= 2 && b[0] >= 0xC0: // UTF-8 multi-byte
			r := decodeUTF8(b[:n])
			if r != 0 {
				e.insert(r)
				if e.inMention {
					e.mentionQuery = string(e.buf[e.mentionStart+1 : e.pos])
					e.mentionResults = FilterCandidates(e.MentionCandidates, e.mentionQuery)
					e.mentionIdx = 0
					if len(e.mentionResults) == 0 {
						e.closeMentionDropdown()
					}
				}
				e.render()
			}
		}
	}
}

// SetHint updates the hint text and redraws it. Safe to call from any goroutine.
func (e *LineEditor) SetHint(hint string) {
	e.hintMu.Lock()
	e.hint = hint
	e.hintMu.Unlock()
	// Save cursor, move to hint line, clear and redraw, restore cursor
	fmt.Print("\0337")
	lines := e.inputLines()
	down := lines - e.cursorLine()
	if down > 0 {
		fmt.Printf("\033[%dB", down)
	}
	fmt.Print("\r\n\033[K")
	e.hintMu.Lock()
	h := e.hint
	e.hintMu.Unlock()
	if h != "" {
		color.New(color.Faint).Print("  " + h)
	}
	fmt.Print("\0338")
}

func (e *LineEditor) getHint() string {
	e.hintMu.Lock()
	defer e.hintMu.Unlock()
	return e.hint
}

func (e *LineEditor) insert(r rune) {
	e.buf = append(e.buf, 0)
	copy(e.buf[e.pos+1:], e.buf[e.pos:])
	e.buf[e.pos] = r
	e.pos++
}

func (e *LineEditor) wordLeft() int {
	p := e.pos
	for p > 0 && unicode.IsSpace(e.buf[p-1]) {
		p--
	}
	for p > 0 && !unicode.IsSpace(e.buf[p-1]) {
		p--
	}
	return p
}

func (e *LineEditor) wordRight() int {
	p := e.pos
	for p < len(e.buf) && !unicode.IsSpace(e.buf[p]) {
		p++
	}
	for p < len(e.buf) && unicode.IsSpace(e.buf[p]) {
		p++
	}
	return p
}

func (e *LineEditor) inputLines() int {
	totalChars := e.PromptLen + len(e.buf)
	if totalChars == 0 {
		return 1
	}
	return (totalChars-1)/e.termWidth + 1
}

func (e *LineEditor) cursorLine() int {
	cursorCol := e.PromptLen + e.pos
	return cursorCol / e.termWidth
}

func (e *LineEditor) render() {
	curLine := e.cursorLine()
	if curLine > 0 {
		fmt.Printf("\033[%dA", curLine)
	}
	fmt.Print("\r")

	// Calculate total lines to clear: input + hint + previous dropdown
	lines := e.inputLines()
	clearLines := lines + 1 + e.mentionLines // input + hint + dropdown
	for i := 0; i < clearLines; i++ {
		fmt.Print("\033[K")
		if i < clearLines-1 {
			fmt.Print("\r\n")
		}
	}
	if clearLines > 1 {
		fmt.Printf("\033[%dA", clearLines-1)
	}
	fmt.Print("\r")

	fmt.Print(e.Prompt)
	fmt.Print(string(e.buf))

	// Hint line
	fmt.Print("\r\n\033[K")
	hint := e.getHint()
	if hint != "" {
		color.New(color.Faint).Print("  " + hint)
	}

	// Mention dropdown below hint
	dropdownLines := 0
	if e.inMention && len(e.mentionResults) > 0 {
		maxVisible := 7
		if len(e.mentionResults) < maxVisible {
			maxVisible = len(e.mentionResults)
		}

		// Calculate scroll window
		start := 0
		if e.mentionIdx >= maxVisible {
			start = e.mentionIdx - maxVisible + 1
		}
		end := start + maxVisible
		if end > len(e.mentionResults) {
			end = len(e.mentionResults)
			start = end - maxVisible
			if start < 0 {
				start = 0
			}
		}

		// Find max width for the box
		maxW := 0
		for i := start; i < end; i++ {
			c := e.mentionResults[i]
			entry := c.DisplayName + " (" + TruncateNpub(c.Npub) + ")"
			if len(entry)+4 > maxW {
				maxW = len(entry) + 4
			}
		}
		if maxW < 20 {
			maxW = 20
		}

		// Top border
		fmt.Print("\r\n\033[K")
		fmt.Print("  ┌" + strings.Repeat("─", maxW) + "┐")
		dropdownLines++

		for i := start; i < end; i++ {
			c := e.mentionResults[i]
			entry := c.DisplayName + " (" + TruncateNpub(c.Npub) + ")"

			fmt.Print("\r\n\033[K")
			if i == e.mentionIdx {
				// Highlighted: bold/inverse
				fmt.Print("  │ \033[1;7m→ " + entry + "\033[0m")
				pad := maxW - len(entry) - 4
				if pad > 0 {
					fmt.Print(strings.Repeat(" ", pad))
				}
				fmt.Print(" │")
			} else {
				fmt.Print("  │   " + entry)
				pad := maxW - len(entry) - 4
				if pad > 0 {
					fmt.Print(strings.Repeat(" ", pad))
				}
				fmt.Print(" │")
			}
			dropdownLines++
		}

		// Bottom border
		fmt.Print("\r\n\033[K")
		fmt.Print("  └" + strings.Repeat("─", maxW) + "┘")
		dropdownLines++
	}
	e.mentionLines = dropdownLines

	// Position cursor back to input line
	cursorAbsCol := e.PromptLen + e.pos
	targetLine := cursorAbsCol / e.termWidth
	targetCol := cursorAbsCol % e.termWidth

	up := (lines - targetLine - 1) + 1 + dropdownLines
	if up > 0 {
		fmt.Printf("\033[%dA", up)
	}
	fmt.Print("\r")
	if targetCol > 0 {
		fmt.Printf("\033[%dC", targetCol)
	}
}

func (e *LineEditor) confirmMention() {
	if len(e.mentionResults) == 0 || e.mentionIdx >= len(e.mentionResults) {
		e.closeMentionDropdown()
		return
	}
	selected := e.mentionResults[e.mentionIdx]

	// Replace @query with @DisplayName in the buffer
	replacement := []rune("@" + selected.DisplayName)
	newBuf := make([]rune, 0, len(e.buf)+len(replacement))
	newBuf = append(newBuf, e.buf[:e.mentionStart]...)
	newBuf = append(newBuf, replacement...)
	if e.pos < len(e.buf) {
		newBuf = append(newBuf, e.buf[e.pos:]...)
	}
	e.buf = newBuf
	e.pos = e.mentionStart + len(replacement)

	// Track the selected mention
	e.SelectedMentions = append(e.SelectedMentions, selected)

	e.closeMentionDropdown()
}

func (e *LineEditor) closeMentionDropdown() {
	e.inMention = false
	e.mentionQuery = ""
	e.mentionResults = nil
	e.mentionIdx = 0
}

func (e *LineEditor) updateMentionState() {
	if !e.inMention {
		return
	}
	// If cursor moved before the mention start, close
	if e.pos <= e.mentionStart {
		e.closeMentionDropdown()
		return
	}
	// Update query from mentionStart+1 to pos
	e.mentionQuery = string(e.buf[e.mentionStart+1 : e.pos])
	e.mentionResults = FilterCandidates(e.MentionCandidates, e.mentionQuery)
	e.mentionIdx = 0
	if len(e.mentionResults) == 0 {
		e.closeMentionDropdown()
	}
}

func (e *LineEditor) clearHint() {
	lines := e.inputLines()
	curLine := e.cursorLine()
	down := (lines - curLine - 1) + 1
	if down > 0 {
		fmt.Printf("\033[%dB", down)
	}
	// Clear hint + any dropdown lines
	totalClear := 1 + e.mentionLines
	for i := 0; i < totalClear; i++ {
		fmt.Print("\r\033[K")
		if i < totalClear-1 {
			fmt.Print("\n")
		}
	}
	if totalClear > 1 {
		fmt.Printf("\033[%dA", totalClear-1)
	}
	if down > 0 {
		fmt.Printf("\033[%dA", down)
	}
	e.mentionLines = 0
}

func decodeUTF8(b []byte) rune {
	s := string(b)
	for _, r := range s {
		return r
	}
	return 0
}

// ReadLineSimple is a fallback for non-terminal input.
func ReadLineSimple(prompt string) (string, bool) {
	fmt.Print(prompt)
	var line string
	_, err := fmt.Scanln(&line)
	if err != nil && err != io.EOF {
		return "", false
	}
	return strings.TrimSpace(line), true
}
