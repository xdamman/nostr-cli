package ui

import "testing"

func TestInlineInputConfigConstruction(t *testing.T) {
	cfg := InlineInputConfig{
		Prompt: "alice> ",
		Hint:   "enter to post, ctrl+c to cancel",
		Candidates: []MentionCandidate{
			{DisplayName: "bob", Npub: "npub1abc", PubHex: "abc123", Source: "alias"},
		},
	}
	if cfg.Prompt != "alice> " {
		t.Errorf("expected prompt 'alice> ', got %q", cfg.Prompt)
	}
	if cfg.Hint != "enter to post, ctrl+c to cancel" {
		t.Errorf("unexpected hint: %q", cfg.Hint)
	}
	if len(cfg.Candidates) != 1 {
		t.Errorf("expected 1 candidate, got %d", len(cfg.Candidates))
	}
}

func TestRunInlineInputNonTTY(t *testing.T) {
	// In test environment, stdin is not a TTY, so RunInlineInput should return cancelled
	result := RunInlineInput(InlineInputConfig{
		Prompt: "test> ",
		Hint:   "testing",
	})
	if !result.Cancelled {
		t.Error("expected Cancelled=true when stdin is not a TTY")
	}
}
