package cmd

import (
	"os"
	"testing"

	"golang.org/x/term"
)

func TestPipedStdinIsNotTerminal(t *testing.T) {
	// When stdin is a pipe (as in `echo "hello" | nostr`), IsTerminal must
	// return false so the root command reads stdin instead of launching the
	// interactive Bubble Tea shell.
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()

	if term.IsTerminal(int(r.Fd())) {
		t.Error("pipe fd should not be detected as a terminal")
	}
}

func TestPipedStdinReadContent(t *testing.T) {
	// Simulate `echo "hello world" | nostr`: write to a pipe, then read it
	// back the same way the root command does.
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}

	msg := "hello world\n"
	go func() {
		w.WriteString(msg)
		w.Close()
	}()

	// Save and replace stdin
	origStdin := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = origStdin }()

	if term.IsTerminal(int(os.Stdin.Fd())) {
		t.Fatal("piped stdin should not be a terminal")
	}

	buf := make([]byte, 256)
	n, _ := os.Stdin.Read(buf)
	got := string(buf[:n])
	if got != msg {
		t.Errorf("expected %q, got %q", msg, got)
	}
	r.Close()
}
