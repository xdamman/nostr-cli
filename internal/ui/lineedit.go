package ui

import (
	"fmt"
	"os"
	"strings"
	"unicode"

	"github.com/fatih/color"
	"golang.org/x/term"
)

// LineEditor provides a raw-terminal line editor with a hint line below.
// It supports cursor movement, word navigation, and dynamic hint updates.
type LineEditor struct {
	Prompt     string // ANSI-colored prompt prefix (e.g. "name> ")
	PromptLen  int    // visible length of prompt (excluding ANSI)
	Hint       string // hint text shown below the prompt line
	fd         int
	oldState   *term.State
	buf        []rune
	pos        int // cursor position in buf (rune index)
	termWidth  int
	hintDirty  bool
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
		Hint:      hint,
		fd:        fd,
		termWidth: w,
	}
}

// ReadLine reads a single line of input with full editing support.
// Returns the entered text, or empty string if cancelled (Ctrl+C/Ctrl+D).
// The hint can be updated concurrently via SetHint.
func (e *LineEditor) ReadLine() (string, bool) {
	oldState, err := term.MakeRaw(e.fd)
	if err != nil {
		return "", false
	}
	e.oldState = oldState
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

		switch {
		case n == 1 && b[0] == 13: // Enter
			e.clearHint()
			fmt.Print("\r\n")
			return string(e.buf), true

		case n == 1 && b[0] == 3: // Ctrl+C
			e.clearHint()
			fmt.Print("\r\n")
			return "", false

		case n == 1 && b[0] == 4: // Ctrl+D
			if len(e.buf) == 0 {
				e.clearHint()
				fmt.Print("\r\n")
				return "", false
			}

		case n == 1 && b[0] == 1: // Ctrl+A — beginning of line
			e.pos = 0
			e.render()

		case n == 1 && b[0] == 5: // Ctrl+E — end of line
			e.pos = len(e.buf)
			e.render()

		case n == 1 && b[0] == 21: // Ctrl+U — clear line
			e.buf = nil
			e.pos = 0
			e.render()

		case n == 1 && b[0] == 11: // Ctrl+K — kill to end of line
			e.buf = e.buf[:e.pos]
			e.render()

		case n == 1 && (b[0] == 127 || b[0] == 8): // Backspace
			if e.pos > 0 {
				e.buf = append(e.buf[:e.pos-1], e.buf[e.pos:]...)
				e.pos--
				e.render()
			}

		case n == 3 && b[0] == 27 && b[1] == '[' && b[2] == 'D': // Left arrow
			if e.pos > 0 {
				e.pos--
				e.render()
			}

		case n == 3 && b[0] == 27 && b[1] == '[' && b[2] == 'C': // Right arrow
			if e.pos < len(e.buf) {
				e.pos++
				e.render()
			}

		case n == 3 && b[0] == 27 && b[1] == '[' && b[2] == 'H': // Home
			e.pos = 0
			e.render()

		case n == 3 && b[0] == 27 && b[1] == '[' && b[2] == 'F': // End
			e.pos = len(e.buf)
			e.render()

		case n >= 3 && b[0] == 27 && b[1] == 'b': // Alt+Left — word left
			e.pos = e.wordLeft()
			e.render()

		case n >= 3 && b[0] == 27 && b[1] == 'f': // Alt+Right — word right
			e.pos = e.wordRight()
			e.render()

		// Alt+Left as ESC [ 1 ; 3 D (6 bytes)
		case n >= 6 && b[0] == 27 && b[1] == '[' && b[2] == '1' && b[3] == ';' && b[4] == '3' && b[5] == 'D':
			e.pos = e.wordLeft()
			e.render()

		// Alt+Right as ESC [ 1 ; 3 C (6 bytes)
		case n >= 6 && b[0] == 27 && b[1] == '[' && b[2] == '1' && b[3] == ';' && b[4] == '3' && b[5] == 'C':
			e.pos = e.wordRight()
			e.render()

		case n == 1 && b[0] >= 32 && b[0] < 127: // Printable ASCII
			e.insert(rune(b[0]))
			e.render()

		case n >= 2 && b[0] >= 0xC0: // UTF-8 multi-byte
			r := decodeUTF8(b[:n])
			if r != 0 {
				e.insert(r)
				e.render()
			}
		}
	}
}

// SetHint updates the hint text and redraws it. Safe to call from any goroutine.
func (e *LineEditor) SetHint(hint string) {
	e.Hint = hint
	// Save cursor, move to hint line, clear and redraw, restore cursor
	fmt.Print("\0337")
	lines := e.inputLines()
	down := lines - e.cursorLine()
	if down > 0 {
		fmt.Printf("\033[%dB", down)
	}
	fmt.Print("\r\n\033[K")
	if hint != "" {
		color.New(color.Faint).Print("  " + hint)
	}
	fmt.Print("\0338")
}

func (e *LineEditor) insert(r rune) {
	e.buf = append(e.buf, 0)
	copy(e.buf[e.pos+1:], e.buf[e.pos:])
	e.buf[e.pos] = r
	e.pos++
}

func (e *LineEditor) wordLeft() int {
	p := e.pos
	// Skip spaces
	for p > 0 && unicode.IsSpace(e.buf[p-1]) {
		p--
	}
	// Skip word chars
	for p > 0 && !unicode.IsSpace(e.buf[p-1]) {
		p--
	}
	return p
}

func (e *LineEditor) wordRight() int {
	p := e.pos
	// Skip word chars
	for p < len(e.buf) && !unicode.IsSpace(e.buf[p]) {
		p++
	}
	// Skip spaces
	for p < len(e.buf) && unicode.IsSpace(e.buf[p]) {
		p++
	}
	return p
}

// inputLines returns how many terminal lines the prompt+input occupies.
func (e *LineEditor) inputLines() int {
	totalChars := e.PromptLen + len(e.buf)
	if totalChars == 0 {
		return 1
	}
	return (totalChars-1)/e.termWidth + 1
}

// cursorLine returns which line (0-based) the cursor is on.
func (e *LineEditor) cursorLine() int {
	cursorCol := e.PromptLen + e.pos
	return cursorCol / e.termWidth
}

func (e *LineEditor) render() {
	// Move to start of input region (first line of prompt)
	curLine := e.cursorLine()
	if curLine > 0 {
		fmt.Printf("\033[%dA", curLine)
	}
	fmt.Print("\r")

	// Calculate how many lines we need
	lines := e.inputLines()

	// Clear all lines (input + hint)
	for i := 0; i < lines+1; i++ { // +1 for hint
		fmt.Print("\033[K")
		if i < lines {
			fmt.Print("\r\n")
		}
	}

	// Move back to first line
	if lines > 0 {
		fmt.Printf("\033[%dA", lines)
	}
	fmt.Print("\r")

	// Print prompt + input
	fmt.Print(e.Prompt)
	fmt.Print(string(e.buf))

	// Print hint on next line
	fmt.Print("\r\n\033[K")
	if e.Hint != "" {
		color.New(color.Faint).Print("  " + e.Hint)
	}

	// Position cursor at the right place
	// Move back to prompt line(s) and position cursor
	cursorAbsCol := e.PromptLen + e.pos
	targetLine := cursorAbsCol / e.termWidth
	targetCol := cursorAbsCol % e.termWidth

	// We're currently at the end of the hint line
	// Need to move up (lines - targetLine) + 1 (for hint line)
	up := (lines - targetLine - 1) + 1
	if up > 0 {
		fmt.Printf("\033[%dA", up)
	}
	fmt.Print("\r")
	if targetCol > 0 {
		fmt.Printf("\033[%dC", targetCol)
	}
}

func (e *LineEditor) clearHint() {
	// Move to hint line and clear it
	lines := e.inputLines()
	curLine := e.cursorLine()
	down := (lines - curLine - 1) + 1
	if down > 0 {
		fmt.Printf("\033[%dB", down)
	}
	fmt.Print("\r\033[K")
	// Move back
	if down > 0 {
		fmt.Printf("\033[%dA", down)
	}
}

// decodeUTF8 decodes the first rune from a byte slice.
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
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(line), true
}
