package cmd

// LLM contract tests for: nostr dm
// Source: cmd/dm.go
//
// LLMs use:
//   nostr dm alice "Hello"
//   nostr dm npub1... "Message"
//   echo "Content" | nostr dm alice

import "testing"

func TestLLM_DM_Exists(t *testing.T) {
	requireCmd(t, "dm")
}

func TestLLM_DM_AcceptsUserAndOptionalMessage(t *testing.T) {
	// `nostr dm` with no args shows alias help.
	// `nostr dm alice` enters interactive chat.
	// `nostr dm alice "msg"` sends one-shot DM.
	cmd := requireCmd(t, "dm")
	if cmd.Args != nil {
		if err := cmd.Args(cmd, []string{}); err != nil {
			t.Error("dm should accept 0 args (show aliases)")
		}
		if err := cmd.Args(cmd, []string{"alice"}); err != nil {
			t.Error("dm should accept 1 arg (interactive chat)")
		}
		if err := cmd.Args(cmd, []string{"alice", "Hello"}); err != nil {
			t.Error("dm should accept 2 args (one-shot DM)")
		}
	}
}

func TestLLM_DM_Flags(t *testing.T) {
	cmd := requireCmd(t, "dm")
	t.Run("--json", func(t *testing.T) { requireFlag(t, cmd, "json") })
	t.Run("--tag", func(t *testing.T) { requireFlag(t, cmd, "tag") })
	t.Run("--tags", func(t *testing.T) { requireFlag(t, cmd, "tags") })
	t.Run("--watch", func(t *testing.T) { requireFlag(t, cmd, "watch") })
}

func TestLLM_DM_InSocialGroup(t *testing.T) {
	cmd := requireCmd(t, "dm")
	if cmd.GroupID != "social" {
		t.Errorf("group = %q, want \"social\"", cmd.GroupID)
	}
}
