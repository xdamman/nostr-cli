package cmd

import (
	"strings"
	"testing"

	"github.com/fatih/color"
)

func init() {
	// Force color output in tests so we can check for ANSI codes
	color.NoColor = false
}

func TestRenderMentions_NoMentions(t *testing.T) {
	input := "Hello, this is a normal message with no mentions."
	got := renderMentions(input)
	if got != input {
		t.Errorf("expected passthrough, got %q", got)
	}
}

func TestRenderMentions_NpubFallback(t *testing.T) {
	// Use a valid-looking npub (60 chars after npub1)
	npub := "npub1" + strings.Repeat("a", 58)
	input := "Check out nostr:" + npub + " for updates"
	got := renderMentions(input)

	// Should NOT contain the original nostr: prefix
	if strings.Contains(got, "nostr:"+npub) {
		t.Errorf("expected npub to be replaced, got %q", got)
	}

	// Should contain @ (the mention prefix)
	if !strings.Contains(got, "@") {
		t.Errorf("expected @ in output, got %q", got)
	}

	// Should contain truncated form
	if !strings.Contains(got, "npub1aaaaaaa") {
		t.Errorf("expected truncated npub in fallback, got %q", got)
	}
}

func TestRenderMentions_MultipleMentions(t *testing.T) {
	npub1 := "npub1" + strings.Repeat("a", 58)
	npub2 := "npub1" + strings.Repeat("b", 58)
	input := "nostr:" + npub1 + " and nostr:" + npub2
	got := renderMentions(input)

	if strings.Contains(got, "nostr:"+npub1) || strings.Contains(got, "nostr:"+npub2) {
		t.Errorf("expected both npubs to be replaced, got %q", got)
	}

	// Count @ occurrences (stripping ANSI)
	count := strings.Count(got, "@")
	if count < 2 {
		t.Errorf("expected at least 2 @ mentions, got %d in %q", count, got)
	}
}

func TestRenderMentions_NoteReference(t *testing.T) {
	noteID := "note1" + strings.Repeat("x", 58)
	input := "See nostr:" + noteID
	got := renderMentions(input)

	if strings.Contains(got, "nostr:"+noteID) {
		t.Errorf("expected note reference to be replaced, got %q", got)
	}
	if !strings.Contains(got, "📝") {
		t.Errorf("expected 📝 emoji in output, got %q", got)
	}
}

func TestRenderMentions_NeventReference(t *testing.T) {
	neventID := "nevent1" + strings.Repeat("z", 58)
	input := "Check nostr:" + neventID
	got := renderMentions(input)

	if strings.Contains(got, "nostr:"+neventID) {
		t.Errorf("expected nevent reference to be replaced, got %q", got)
	}
	if !strings.Contains(got, "📝") {
		t.Errorf("expected 📝 emoji in output, got %q", got)
	}
}

func TestRenderMentions_MixedContent(t *testing.T) {
	npub := "npub1" + strings.Repeat("c", 58)
	noteID := "note1" + strings.Repeat("d", 58)
	input := "Hey nostr:" + npub + " check this nostr:" + noteID + " cool stuff"
	got := renderMentions(input)

	if strings.Contains(got, "nostr:"+npub) {
		t.Errorf("expected npub to be replaced, got %q", got)
	}
	if strings.Contains(got, "nostr:"+noteID) {
		t.Errorf("expected note to be replaced, got %q", got)
	}
	if !strings.Contains(got, "@") {
		t.Errorf("expected @ in output, got %q", got)
	}
	if !strings.Contains(got, "📝") {
		t.Errorf("expected 📝 in output, got %q", got)
	}
	if !strings.Contains(got, "cool stuff") {
		t.Errorf("expected surrounding text preserved, got %q", got)
	}
}
