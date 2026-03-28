package ui

import (
	"testing"
)

func TestFilterCandidates_PrefixMatch(t *testing.T) {
	candidates := []MentionCandidate{
		{DisplayName: "alice", Npub: "npub1aaa", PubHex: "aaa", Source: "alias"},
		{DisplayName: "bob", Npub: "npub1bbb", PubHex: "bbb", Source: "following"},
		{DisplayName: "alicia", Npub: "npub1ccc", PubHex: "ccc", Source: "cache"},
	}

	results := FilterCandidates(candidates, "ali")
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].DisplayName != "alice" {
		t.Errorf("expected alice first, got %s", results[0].DisplayName)
	}
	if results[1].DisplayName != "alicia" {
		t.Errorf("expected alicia second, got %s", results[1].DisplayName)
	}
}

func TestFilterCandidates_CaseInsensitive(t *testing.T) {
	candidates := []MentionCandidate{
		{DisplayName: "Alice", Npub: "npub1aaa", PubHex: "aaa", Source: "alias"},
		{DisplayName: "BOB", Npub: "npub1bbb", PubHex: "bbb", Source: "following"},
	}

	results := FilterCandidates(candidates, "alice")
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].DisplayName != "Alice" {
		t.Errorf("expected Alice, got %s", results[0].DisplayName)
	}

	results = FilterCandidates(candidates, "bob")
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
}

func TestFilterCandidates_MaxResults(t *testing.T) {
	candidates := make([]MentionCandidate, 20)
	for i := range candidates {
		candidates[i] = MentionCandidate{
			DisplayName: "user",
			Npub:        "npub1xxx",
			PubHex:      "xxx",
			Source:      "cache",
		}
	}

	results := FilterCandidates(candidates, "user")
	if len(results) != 10 {
		t.Fatalf("expected max 10 results, got %d", len(results))
	}
}

func TestFilterCandidates_EmptyQuery(t *testing.T) {
	candidates := []MentionCandidate{
		{DisplayName: "alice", Npub: "npub1aaa", PubHex: "aaa", Source: "alias"},
		{DisplayName: "bob", Npub: "npub1bbb", PubHex: "bbb", Source: "following"},
	}

	results := FilterCandidates(candidates, "")
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
}

func TestFilterCandidates_NoMatch(t *testing.T) {
	candidates := []MentionCandidate{
		{DisplayName: "alice", Npub: "npub1aaa", PubHex: "aaa", Source: "alias"},
		{DisplayName: "bob", Npub: "npub1bbb", PubHex: "bbb", Source: "following"},
	}

	results := FilterCandidates(candidates, "zzz")
	if len(results) != 0 {
		t.Fatalf("expected 0 results, got %d", len(results))
	}
}

func TestFilterCandidates_NpubMatch(t *testing.T) {
	candidates := []MentionCandidate{
		{DisplayName: "alice", Npub: "npub1aaa123", PubHex: "aaa", Source: "alias"},
		{DisplayName: "bob", Npub: "npub1bbb456", PubHex: "bbb", Source: "following"},
	}

	results := FilterCandidates(candidates, "npub1bbb")
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].DisplayName != "bob" {
		t.Errorf("expected bob, got %s", results[0].DisplayName)
	}
}

func TestTruncateNpub(t *testing.T) {
	short := "npub1abc"
	if got := TruncateNpub(short); got != short {
		t.Errorf("short npub: expected %q, got %q", short, got)
	}

	long := "npub1sg6plzptd64u62a878hep2kev88swjh3tw00gjsfl8f237lmu63q0uf63m"
	got := TruncateNpub(long)
	if len(got) > 17 {
		t.Errorf("truncated npub too long: %q", got)
	}
	if got[:5] != "npub1" {
		t.Errorf("should start with npub1: %q", got)
	}
}

func TestReplaceMentionsForEvent(t *testing.T) {
	mentions := []MentionCandidate{
		{DisplayName: "alice", Npub: "npub1aaa", PubHex: "hex_aaa"},
		{DisplayName: "bob", Npub: "npub1bbb", PubHex: "hex_bbb"},
	}

	text := "Hello @alice and @bob!"
	got, tags := ReplaceMentionsForEvent(text, mentions)

	if got != "Hello nostr:npub1aaa and nostr:npub1bbb!" {
		t.Errorf("unexpected text: %q", got)
	}
	if len(tags) != 2 {
		t.Fatalf("expected 2 tags, got %d", len(tags))
	}
	if tags[0][0] != "p" || tags[0][1] != "hex_aaa" {
		t.Errorf("unexpected tag[0]: %v", tags[0])
	}
}

func TestSourcePriority(t *testing.T) {
	if sourcePriority("alias") >= sourcePriority("following") {
		t.Error("alias should have higher priority than following")
	}
	if sourcePriority("following") >= sourcePriority("cache") {
		t.Error("following should have higher priority than cache")
	}
}
