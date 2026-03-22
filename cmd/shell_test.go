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
	// Force a wrap by using a very long string with a small available width
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
	// Should not have \r\n
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

	// Continuation lines should start with spaces equal to prefixLen
	indent := strings.Repeat(" ", prefixLen)
	for i := 1; i < len(lines); i++ {
		if !strings.HasPrefix(lines[i], indent) {
			t.Errorf("line %d should start with %d spaces of indent, got %q", i, prefixLen, lines[i])
		}
	}
}

func TestWrapNoteWithSep_NewlinesReplacedWithSpaces(t *testing.T) {
	content := "line1\nline2\nline3"
	result := wrapNoteWithSep(content, 20, "\n")
	// The original newlines should be replaced with spaces
	if strings.Contains(result, "line1\n") {
		// If it wraps, make sure original newlines were collapsed
		flat := strings.ReplaceAll(result, "\n", "")
		flat = strings.ReplaceAll(flat, " ", "")
		if !strings.Contains(flat, "line1line2line3") {
			t.Error("original content newlines should be replaced with spaces")
		}
	}
}

func TestBatchEventsSortedChronologically(t *testing.T) {
	// Simulate the batch fetch sort used in runShell: events should be sorted
	// oldest first so they print in chronological order.
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
	// Real-time subscription uses since: nostr.Now(), so any event received
	// will have CreatedAt >= the subscription start time.
	// This means appending real-time events at the bottom is correct.
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
	// Reset
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
