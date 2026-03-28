package cmd

// LLM contract tests for: nostr event new
// Source: cmd/event.go
//
// LLMs use:
//   nostr event new --kind 1 --content "Hello world"
//   nostr event new --kind 7 --content "+" --tag e=<eventid> --tag p=<pubkey>
//   nostr event new --kind 1 --content "Test" --dry-run --json

import "testing"

func TestLLM_Event_Exists(t *testing.T) {
	requireCmd(t, "event")
}

func TestLLM_EventNew_Exists(t *testing.T) {
	requireCmd(t, "event", "new")
}

func TestLLM_EventNew_Flags(t *testing.T) {
	cmd := requireCmd(t, "event", "new")
	t.Run("--kind", func(t *testing.T) { requireFlag(t, cmd, "kind") })
	t.Run("--content", func(t *testing.T) { requireFlag(t, cmd, "content") })
	t.Run("--tag", func(t *testing.T) { requireFlag(t, cmd, "tag") })
	t.Run("--tags", func(t *testing.T) { requireFlag(t, cmd, "tags") })
	t.Run("--pow", func(t *testing.T) { requireFlag(t, cmd, "pow") })
	t.Run("--dry-run", func(t *testing.T) { requireFlag(t, cmd, "dry-run") })
}

func TestLLM_EventNew_InheritsGlobalFlags(t *testing.T) {
	cmd := requireCmd(t, "event", "new")
	globals := []string{"json", "jsonl", "raw", "no-color", "profile", "timeout"}
	for _, flag := range globals {
		t.Run("--"+flag, func(t *testing.T) { requireFlag(t, cmd, flag) })
	}
}

func TestLLM_EventNew_KindDefault(t *testing.T) {
	cmd := requireCmd(t, "event", "new")
	f := cmd.Flags().Lookup("kind")
	if f == nil {
		t.Fatal("--kind flag not found")
	}
	if f.DefValue != "-1" {
		t.Errorf("--kind default = %q, want \"-1\"", f.DefValue)
	}
}

func TestLLM_Event_InSocialGroup(t *testing.T) {
	cmd := requireCmd(t, "event")
	if cmd.GroupID != "social" {
		t.Errorf("group = %q, want \"social\"", cmd.GroupID)
	}
}
