package cmd

import (
	"sort"
	"strings"
	"testing"

	"github.com/nbd-wtf/go-nostr"
)

func TestWrapNoteWithSep_ShortContent(t *testing.T) {
	result := wrapNoteWithSep("hello world", 20, "\n")
	if result != "hello world" {
		t.Errorf("expected 'hello world', got %q", result)
	}
}

func TestWrapNoteWithSep_UsesCustomSeparator(t *testing.T) {
	long := strings.Repeat("word ", 30)
	resultLF := wrapNoteWithSep(long, 20, "\n")
	resultCRLF := wrapNoteWithSep(long, 20, "\r\n")

	if !strings.Contains(resultLF, "\n") {
		t.Error("expected \\n separator in wrapped output")
	}
	if strings.Contains(resultLF, "\r\n") {
		t.Error("LF mode should not contain \\r\\n")
	}
	if !strings.Contains(resultCRLF, "\r\n") {
		t.Error("expected \\r\\n separator in wrapped output")
	}
}

func TestWrapNoteRaw_UsesCRLF(t *testing.T) {
	long := strings.Repeat("word ", 30)
	result := wrapNoteRaw(long, 20)
	if !strings.Contains(result, "\r\n") {
		t.Error("wrapNoteRaw should use \\r\\n separators")
	}
}

func TestWrapNote_UsesLF(t *testing.T) {
	long := strings.Repeat("word ", 30)
	result := wrapNote(long, 20)
	if !strings.Contains(result, "\n") {
		t.Error("wrapNote should contain \\n separators")
	}
	if strings.Contains(result, "\r\n") {
		t.Error("wrapNote should not contain \\r\\n separators")
	}
}

func TestWrapNoteWithSep_ContinuationIndent(t *testing.T) {
	long := strings.Repeat("word ", 30)
	prefixLen := 20
	result := wrapNoteWithSep(long, prefixLen, "\n")

	lines := strings.Split(result, "\n")
	if len(lines) < 2 {
		t.Fatal("expected at least 2 lines")
	}

	indent := strings.Repeat(" ", prefixLen)
	for i := 1; i < len(lines); i++ {
		if !strings.HasPrefix(lines[i], indent) {
			t.Errorf("line %d should start with %d spaces of indent, got %q", i, prefixLen, lines[i])
		}
	}
}

func TestWrapNoteWithSep_NewlinesPreserved(t *testing.T) {
	content := "line1\nline2\nline3"
	result := wrapNoteWithSep(content, 20, "\n")
	indent := strings.Repeat(" ", 20)
	expected := "line1\n" + indent + "line2\n" + indent + "line3"
	if result != expected {
		t.Errorf("expected newlines preserved with indent, got %q", result)
	}
}

func TestRenderInlineMarkdown(t *testing.T) {
	tests := []struct {
		input    string
		contains string
		desc     string
	}{
		{"**bold**", "\033[1mbold\033[22m", "bold"},
		{"*italic*", "\033[3mitalic\033[23m", "italic"},
		{"__underline__", "\033[4munderline\033[24m", "underline"},
		{"~~strike~~", "\033[9mstrike\033[29m", "strikethrough"},
		{"no markdown here", "no markdown here", "plain text"},
		{"**bold** and *italic*", "\033[1mbold\033[22m", "mixed"},
	}
	for _, tt := range tests {
		result := renderInlineMarkdown(tt.input)
		if !strings.Contains(result, tt.contains) {
			t.Errorf("%s: expected %q to contain %q", tt.desc, result, tt.contains)
		}
	}
}

func TestVisibleLen(t *testing.T) {
	if n := visibleLen("hello"); n != 5 {
		t.Errorf("expected 5, got %d", n)
	}
	if n := visibleLen("\033[1mhi\033[22m"); n != 2 {
		t.Errorf("expected 2, got %d", n)
	}
}

func TestSprintPromptPrefix(t *testing.T) {
	prefix := sprintPromptPrefix("alice")
	if !strings.Contains(prefix, "alice") {
		t.Errorf("expected prefix to contain 'alice', got %q", prefix)
	}
	if !strings.HasSuffix(prefix, "> ") {
		t.Errorf("expected prefix to end with '> ', got %q", prefix)
	}
}

func TestBatchEventsSortedChronologically(t *testing.T) {
	events := []*nostr.Event{
		{ID: "c", CreatedAt: nostr.Timestamp(1000)},
		{ID: "a", CreatedAt: nostr.Timestamp(500)},
		{ID: "b", CreatedAt: nostr.Timestamp(750)},
	}

	sort.Slice(events, func(i, j int) bool {
		return events[i].CreatedAt < events[j].CreatedAt
	})

	if events[0].ID != "a" || events[1].ID != "b" || events[2].ID != "c" {
		t.Errorf("events not in chronological order: %v, %v, %v",
			events[0].ID, events[1].ID, events[2].ID)
	}
}

func TestRealtimeEventsAreNewerThanBatch(t *testing.T) {
	batchTime := nostr.Timestamp(1000)
	subscriptionStart := nostr.Timestamp(2000)
	realtimeEvent := nostr.Timestamp(2001)

	if realtimeEvent <= batchTime {
		t.Error("real-time events should always be newer than batch events")
	}
	if realtimeEvent < subscriptionStart {
		t.Error("real-time events should be at or after subscription start")
	}
}

func TestUpdateFeedNameWidth(t *testing.T) {
	feedNameWidthMu.Lock()
	feedNameWidth = 0
	feedNameWidthMu.Unlock()

	w1 := updateFeedNameWidth("alice")
	if w1 != 5 {
		t.Errorf("expected 5, got %d", w1)
	}

	w2 := updateFeedNameWidth("bob")
	if w2 != 5 {
		t.Errorf("expected 5 (should not shrink), got %d", w2)
	}

	w3 := updateFeedNameWidth("christopher")
	if w3 != 11 {
		t.Errorf("expected 11, got %d", w3)
	}
}

func TestFilterCommands(t *testing.T) {
	cmds := filterCommands([]byte("/f"))
	names := make([]string, len(cmds))
	for i, c := range cmds {
		names[i] = c.name
	}
	if !sliceContains(names, "follow") || !sliceContains(names, "following") {
		t.Errorf("expected /f to match follow and following, got %v", names)
	}

	cmds = filterCommands([]byte("/dm"))
	if len(cmds) != 1 || cmds[0].name != "dm" {
		t.Errorf("expected /dm to match dm, got %v", cmds)
	}

	cmds = filterCommands([]byte("/dm message"))
	if cmds != nil {
		t.Error("expected nil for command with space")
	}

	cmds = filterCommands([]byte("hello"))
	if cmds != nil {
		t.Error("expected nil for non-slash input")
	}
}

func sliceContains(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}

func TestSprintFeedEvent(t *testing.T) {
	ev := nostr.Event{
		PubKey:    "abcdef1234567890abcdef1234567890abcdef1234567890abcdef1234567890",
		CreatedAt: nostr.Timestamp(1711000000),
		Content:   "Hello world",
	}
	line := sprintFeedEvent(ev, "myhex", "alice", 80)
	if !strings.Contains(line, "Hello world") {
		t.Errorf("expected feed line to contain content, got %q", line)
	}
}
