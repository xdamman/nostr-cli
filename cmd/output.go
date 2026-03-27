package cmd

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"
)

// printRaw outputs a value as compact single-line JSON (wire format).
func printRaw(v interface{}) {
	data, _ := json.Marshal(v)
	fmt.Println(string(data))
}

// printJSON outputs a value as pretty-printed JSON.
// When stdout is a TTY, adds syntax coloring.
func printJSON(v interface{}) {
	data, _ := json.MarshalIndent(v, "", "  ")
	if term.IsTerminal(int(os.Stdout.Fd())) && !noColorFlag {
		fmt.Println(colorizeJSON(string(data)))
	} else {
		fmt.Println(string(data))
	}
}

// printJSONL outputs a value as a single compact JSON line (JSON Lines format).
func printJSONL(v interface{}) {
	data, _ := json.Marshal(v)
	fmt.Println(string(data))
}

// colorizeJSON adds ANSI color codes to JSON output.
func colorizeJSON(s string) string {
	var b strings.Builder
	inString := false
	isKey := false
	escaped := false

	const (
		reset  = "\033[0m"
		cyan   = "\033[36m"  // keys
		green  = "\033[32m"  // strings
		yellow = "\033[33m"  // numbers
		red    = "\033[31m"  // booleans/null
	)

	for i := 0; i < len(s); i++ {
		c := s[i]

		if escaped {
			b.WriteByte(c)
			escaped = false
			continue
		}

		if c == '\\' && inString {
			b.WriteByte(c)
			escaped = true
			continue
		}

		if c == '"' {
			if !inString {
				inString = true
				// Determine if this is a key (next non-ws char after closing quote is ':')
				isKey = isJSONKey(s, i)
				if isKey {
					b.WriteString(cyan)
				} else {
					b.WriteString(green)
				}
				b.WriteByte(c)
			} else {
				b.WriteByte(c)
				b.WriteString(reset)
				inString = false
			}
			continue
		}

		if inString {
			b.WriteByte(c)
			continue
		}

		// Numbers
		if (c >= '0' && c <= '9') || c == '-' {
			b.WriteString(yellow)
			for i < len(s) && ((s[i] >= '0' && s[i] <= '9') || s[i] == '.' || s[i] == '-' || s[i] == 'e' || s[i] == 'E' || s[i] == '+') {
				b.WriteByte(s[i])
				i++
			}
			b.WriteString(reset)
			i--
			continue
		}

		// true/false/null
		for _, kw := range []string{"true", "false", "null"} {
			if i+len(kw) <= len(s) && s[i:i+len(kw)] == kw {
				b.WriteString(red)
				b.WriteString(kw)
				b.WriteString(reset)
				i += len(kw) - 1
				goto next
			}
		}

		b.WriteByte(c)
	next:
	}
	return b.String()
}

// isJSONKey checks if the quote at position i starts a JSON key.
func isJSONKey(s string, i int) bool {
	// Find the closing quote
	j := i + 1
	for j < len(s) {
		if s[j] == '\\' {
			j += 2
			continue
		}
		if s[j] == '"' {
			break
		}
		j++
	}
	// Look for ':' after closing quote
	for k := j + 1; k < len(s); k++ {
		if s[k] == ' ' || s[k] == '\t' || s[k] == '\n' || s[k] == '\r' {
			continue
		}
		return s[k] == ':'
	}
	return false
}
