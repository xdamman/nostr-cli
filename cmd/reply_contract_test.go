package cmd

// LLM contract tests for: nostr reply
// Source: cmd/reply.go
//
// LLMs use:
//   nostr reply note1abc... "Great post!"
//   nostr reply abc123hex "I agree" --tag t=nostr
//   nostr reply nevent1... "Check this" --tags '[["p","<hex>"]]'
//   nostr reply note1abc... "Test" --dry-run --json

import "testing"

func TestLLM_Reply_Exists(t *testing.T) {
	requireCmd(t, "reply")
}

func TestLLM_Reply_Flags(t *testing.T) {
	cmd := requireCmd(t, "reply")
	t.Run("--tag", func(t *testing.T) { requireFlag(t, cmd, "tag") })
	t.Run("--tags", func(t *testing.T) { requireFlag(t, cmd, "tags") })
	t.Run("--dry-run", func(t *testing.T) { requireFlag(t, cmd, "dry-run") })
}

func TestLLM_Reply_InheritsGlobalFlags(t *testing.T) {
	cmd := requireCmd(t, "reply")
	globals := []string{"json", "jsonl", "raw", "no-color", "account", "timeout"}
	for _, flag := range globals {
		t.Run("--"+flag, func(t *testing.T) { requireFlag(t, cmd, flag) })
	}
}

func TestLLM_Reply_RequiresAtLeastOneArg(t *testing.T) {
	cmd := requireCmd(t, "reply")
	if cmd.Args == nil {
		t.Fatal("reply should have arg validation")
	}
	if err := cmd.Args(cmd, []string{}); err == nil {
		t.Error("reply should reject 0 args (needs eventId)")
	}
	if err := cmd.Args(cmd, []string{"note1abc"}); err != nil {
		t.Error("reply should accept 1 arg (eventId)")
	}
	if err := cmd.Args(cmd, []string{"note1abc", "Great post!"}); err != nil {
		t.Error("reply should accept 2+ args (eventId + message)")
	}
}

func TestLLM_Reply_InSocialGroup(t *testing.T) {
	cmd := requireCmd(t, "reply")
	if cmd.GroupID != "social" {
		t.Errorf("group = %q, want \"social\"", cmd.GroupID)
	}
}
