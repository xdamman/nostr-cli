package cmd

import (
	"strings"
	"testing"

	"github.com/nbd-wtf/go-nostr"
)

func TestPlural(t *testing.T) {
	tests := []struct {
		n    int
		want string
	}{
		{0, "s"},
		{1, ""},
		{2, "s"},
		{10, "s"},
	}
	for _, tt := range tests {
		got := plural(tt.n)
		if got != tt.want {
			t.Errorf("plural(%d) = %q, want %q", tt.n, got, tt.want)
		}
	}
}

func TestSyncChecklistRender_ConstantLineCount(t *testing.T) {
	relays := []string{
		"wss://relay.damus.io",
		"wss://nos.lol",
		"wss://relay.nostr.band",
		"wss://purplepag.es",
	}
	expectedLines := len(relays) + 2 // relay lines + blank + hint

	checked := make([]bool, len(relays))
	for i := range checked {
		checked[i] = true
	}

	// No fetch state yet
	fetchStates := map[string]*relayFetchState{}

	for cursor := 0; cursor < len(relays); cursor++ {
		var buf strings.Builder
		n := syncChecklistRenderTo(&buf, relays, checked, cursor, fetchStates)
		if n != expectedLines {
			t.Errorf("cursor=%d, no fetch: expected %d lines, got %d", cursor, expectedLines, n)
		}
		crlfs := strings.Count(buf.String(), "\r\n")
		if crlfs != expectedLines {
			t.Errorf("cursor=%d, no fetch: expected %d \\r\\n, got %d", cursor, expectedLines, crlfs)
		}
	}
}

func TestSyncChecklistRender_ConstantLineCount_WithFetchResults(t *testing.T) {
	relays := []string{
		"wss://relay.damus.io",
		"wss://nos.lol",
		"wss://relay.nostr.band",
		"wss://purplepag.es",
	}
	expectedLines := len(relays) + 2

	checked := make([]bool, len(relays))
	for i := range checked {
		checked[i] = true
	}

	fetchStates := map[string]*relayFetchState{
		"wss://relay.damus.io": {done: true, ok: true, count: 42, missing: 3, eventIDs: map[string]bool{}},
		"wss://nos.lol":        {done: true, ok: true, count: 38, missing: 0, eventIDs: map[string]bool{}},
		"wss://purplepag.es":   {done: true, ok: false, eventIDs: map[string]bool{}},
	}

	for cursor := 0; cursor < len(relays); cursor++ {
		var buf strings.Builder
		n := syncChecklistRenderTo(&buf, relays, checked, cursor, fetchStates)
		if n != expectedLines {
			t.Errorf("cursor=%d, mixed fetch: expected %d lines, got %d", cursor, expectedLines, n)
		}
		crlfs := strings.Count(buf.String(), "\r\n")
		if crlfs != expectedLines {
			t.Errorf("cursor=%d, mixed fetch: expected %d \\r\\n, got %d", cursor, expectedLines, crlfs)
		}
	}
}

func TestSyncChecklistRender_ConstantLineCount_AllInSync(t *testing.T) {
	relays := []string{
		"wss://relay.damus.io",
		"wss://nos.lol",
	}
	expectedLines := len(relays) + 2

	checked := []bool{false, false}

	fetchStates := map[string]*relayFetchState{
		"wss://relay.damus.io": {done: true, ok: true, count: 10, missing: 0, eventIDs: map[string]bool{}},
		"wss://nos.lol":        {done: true, ok: true, count: 20, missing: 0, eventIDs: map[string]bool{}},
	}

	var buf strings.Builder
	n := syncChecklistRenderTo(&buf, relays, checked, 0, fetchStates)
	if n != expectedLines {
		t.Errorf("all in sync: expected %d lines, got %d", expectedLines, n)
	}
	crlfs := strings.Count(buf.String(), "\r\n")
	if crlfs != expectedLines {
		t.Errorf("all in sync: expected %d \\r\\n, got %d", expectedLines, crlfs)
	}
}

func TestSyncChecklistRender_SelectedCount(t *testing.T) {
	relays := []string{
		"wss://relay.damus.io",
		"wss://nos.lol",
		"wss://relay.nostr.band",
	}

	checked := []bool{true, false, true}
	fetchStates := map[string]*relayFetchState{}

	var buf strings.Builder
	syncChecklistRenderTo(&buf, relays, checked, 0, fetchStates)
	output := buf.String()

	if !strings.Contains(output, "2 selected") {
		t.Errorf("expected '2 selected' in output, got: %s", output)
	}
}

func TestSyncChecklistRender_SingularPlural(t *testing.T) {
	// 1 relay selected should say "1 selected" (test render uses "hint line (N selected)")
	relays := []string{"wss://nos.lol"}
	checked := []bool{true}
	fetchStates := map[string]*relayFetchState{}

	var buf strings.Builder
	syncChecklistRenderTo(&buf, relays, checked, 0, fetchStates)
	output := buf.String()

	if !strings.Contains(output, "1 selected") {
		t.Errorf("expected '1 selected' in output, got: %s", output)
	}
}

func TestSyncChecklistRender_FetchStatusContent(t *testing.T) {
	relays := []string{"wss://nos.lol", "wss://relay.damus.io"}
	checked := []bool{true, true}
	fetchStates := map[string]*relayFetchState{
		"wss://nos.lol":        {done: true, ok: true, count: 1, missing: 0, eventIDs: map[string]bool{}},
		"wss://relay.damus.io": {done: true, ok: true, count: 42, missing: 3, eventIDs: map[string]bool{}},
	}

	var buf strings.Builder
	syncChecklistRenderTo(&buf, relays, checked, 0, fetchStates)
	output := buf.String()

	// Singular: "1 event, in sync"
	if !strings.Contains(output, "1 event, in sync") {
		t.Errorf("expected '1 event, in sync' (singular), got: %s", output)
	}
	// Plural: "42 events, 3 to push"
	if !strings.Contains(output, "42 events, 3 to push") {
		t.Errorf("expected '42 events, 3 to push' (plural), got: %s", output)
	}
}

func TestSyncableEvents_SkipsEphemeral(t *testing.T) {
	events := []nostr.Event{
		{ID: "a", Kind: 1, CreatedAt: 100},
		{ID: "b", Kind: 20000, CreatedAt: 200}, // ephemeral
		{ID: "c", Kind: 1, CreatedAt: 300},
	}
	syncable, skipped := syncableEvents(events)
	if len(syncable) != 2 {
		t.Errorf("expected 2 syncable, got %d", len(syncable))
	}
	if skipped != 1 {
		t.Errorf("expected 1 skipped, got %d", skipped)
	}
}

func TestSyncableEvents_KeepsLatestReplaceable(t *testing.T) {
	events := []nostr.Event{
		{ID: "old-profile", Kind: 0, CreatedAt: 100},
		{ID: "new-profile", Kind: 0, CreatedAt: 200},
		{ID: "note", Kind: 1, CreatedAt: 150},
	}
	syncable, skipped := syncableEvents(events)
	if len(syncable) != 2 {
		t.Errorf("expected 2 syncable (latest profile + note), got %d", len(syncable))
	}
	if skipped != 1 {
		t.Errorf("expected 1 skipped (old profile), got %d", skipped)
	}
	// The latest profile should be the one kept
	for _, ev := range syncable {
		if ev.Kind == 0 && ev.ID != "new-profile" {
			t.Errorf("expected new-profile to be kept, got %s", ev.ID)
		}
	}
}

func TestSyncableEvents_KeepsLatestAddressable(t *testing.T) {
	events := []nostr.Event{
		{ID: "old", Kind: 30023, CreatedAt: 100, Tags: nostr.Tags{{"d", "article-1"}}},
		{ID: "new", Kind: 30023, CreatedAt: 200, Tags: nostr.Tags{{"d", "article-1"}}},
		{ID: "other", Kind: 30023, CreatedAt: 150, Tags: nostr.Tags{{"d", "article-2"}}},
	}
	syncable, skipped := syncableEvents(events)
	if len(syncable) != 2 {
		t.Errorf("expected 2 syncable (latest per d-tag), got %d", len(syncable))
	}
	if skipped != 1 {
		t.Errorf("expected 1 skipped, got %d", skipped)
	}
}
