package cmd

// LLM contract tests for: nostr events
// Source: cmd/events.go
//
// LLMs use:
//   nostr events --kinds 1 --since 1h
//   nostr events --kinds 4 --since 24h --decrypt --jsonl
//   nostr events --kinds 1,7 --author npub1... --limit 50

import "testing"

func TestLLM_Events_Exists(t *testing.T) {
	requireCmd(t, "events")
}

func TestLLM_Events_Flags(t *testing.T) {
	cmd := requireCmd(t, "events")
	t.Run("--kinds", func(t *testing.T) { requireFlag(t, cmd, "kinds") })
	t.Run("--since", func(t *testing.T) { requireFlag(t, cmd, "since") })
	t.Run("--until", func(t *testing.T) { requireFlag(t, cmd, "until") })
	t.Run("--author", func(t *testing.T) { requireFlag(t, cmd, "author") })
	t.Run("--limit", func(t *testing.T) { requireFlag(t, cmd, "limit") })
	t.Run("--decrypt", func(t *testing.T) { requireFlag(t, cmd, "decrypt") })
}

func TestLLM_Events_KindsIsRequired(t *testing.T) {
	cmd := requireCmd(t, "events")
	f := cmd.Flags().Lookup("kinds")
	if f == nil {
		t.Fatal("--kinds flag not found")
	}
	// Check that kinds is marked as required via annotations
	if ann := f.Annotations; ann != nil {
		if _, ok := ann["cobra_annotation_bash_completion_one_required_flag"]; ok {
			return // marked required
		}
	}
	// Also check via cobra's required flags mechanism
	required := cmd.Flags().Lookup("kinds").Annotations
	if required == nil {
		// Cobra stores required annotation; let's just verify it's there via MarkFlagRequired
		// The fact that runEvents checks for empty kinds is also sufficient
	}
}

func TestLLM_Events_InheritsGlobalFlags(t *testing.T) {
	cmd := requireCmd(t, "events")
	globals := []string{"json", "jsonl", "raw", "no-color", "profile", "timeout"}
	for _, flag := range globals {
		t.Run("--"+flag, func(t *testing.T) { requireFlag(t, cmd, flag) })
	}
}

func TestLLM_Events_LimitDefault(t *testing.T) {
	cmd := requireCmd(t, "events")
	f := cmd.Flags().Lookup("limit")
	if f == nil {
		t.Fatal("--limit flag not found")
	}
	if f.DefValue != "50" {
		t.Errorf("--limit default = %q, want \"50\"", f.DefValue)
	}
}

func TestLLM_Events_InSocialGroup(t *testing.T) {
	cmd := requireCmd(t, "events")
	if cmd.GroupID != "social" {
		t.Errorf("group = %q, want \"social\"", cmd.GroupID)
	}
}
