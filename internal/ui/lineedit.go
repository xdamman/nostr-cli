package ui

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/reeflective/readline"
	"golang.org/x/term"
)

// LineEditor provides a line editor with a hint line below the prompt.
// It wraps reeflective/readline for robust terminal handling.
type LineEditor struct {
	shell *readline.Shell
}

// NewLineEditor creates a line editor. Returns nil if stdin is not a terminal.
func NewLineEditor(prompt string, promptLen int, hint string) *LineEditor {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return nil
	}

	rl := readline.NewShell()
	rl.Prompt.Primary(func() string { return prompt })
	rl.Hint.Set("  " + hint)

	return &LineEditor{shell: rl}
}

// ReadLine reads a single line of input with full editing support.
// Returns the entered text, or empty string if cancelled (Ctrl+C/Ctrl+D).
// The hint can be updated concurrently via SetHint.
func (e *LineEditor) ReadLine() (string, bool) {
	line, err := e.shell.Readline()
	if err != nil {
		return "", false
	}
	return line, true
}

// SetHint updates the hint text and redraws it. Safe to call from any goroutine.
func (e *LineEditor) SetHint(hint string) {
	e.shell.Hint.Set("  " + hint)
	e.shell.Display.Refresh()
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
