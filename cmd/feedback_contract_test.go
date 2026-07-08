package cmd

// LLM contract tests for: nostr feedback
// Source: cmd/feedback.go
//
// LLMs use:
//   nostr feedback "Love the CLI"
//   echo "Found a bug" | nostr feedback --jsonl
//   nostr feedback "Test" --dry-run --json

import (
	"strings"
	"testing"
)

func TestLLM_Feedback_Exists(t *testing.T) {
	requireCmd(t, "feedback")
}

func TestLLM_Feedback_Flags(t *testing.T) {
	cmd := requireCmd(t, "feedback")
	t.Run("--dry-run", func(t *testing.T) { requireFlag(t, cmd, "dry-run") })
}

func TestLLM_Feedback_AcceptsOptionalMessage(t *testing.T) {
	// `nostr feedback` with no args enters interactive mode or reads stdin.
	cmd := requireCmd(t, "feedback")
	if cmd.Args != nil {
		if err := cmd.Args(cmd, []string{}); err != nil {
			t.Error("feedback should accept 0 args (interactive/stdin)")
		}
		if err := cmd.Args(cmd, []string{"Great tool"}); err != nil {
			t.Error("feedback should accept 1+ args (message)")
		}
	}
}

func TestLLM_Feedback_InSocialGroup(t *testing.T) {
	cmd := requireCmd(t, "feedback")
	if cmd.GroupID != "social" {
		t.Errorf("group = %q, want \"social\"", cmd.GroupID)
	}
}

func TestLLM_Feedback_HelpSaysPublic(t *testing.T) {
	// The command must make clear feedback is a PUBLIC note mentioning @nostrcli.
	cmd := requireCmd(t, "feedback")
	if !strings.Contains(cmd.Long, "PUBLIC") {
		t.Error("feedback help must state the note is PUBLIC")
	}
	if !strings.Contains(cmd.Long, "@nostrcli") {
		t.Error("feedback help must mention @nostrcli")
	}
}

func TestLLM_Feedback_NostrcliNpubIsValid(t *testing.T) {
	if !strings.HasPrefix(nostrcliNpub, "npub1") {
		t.Errorf("nostrcliNpub should be an npub, got %q", nostrcliNpub)
	}
}
