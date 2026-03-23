package cmd

// LLM contract tests for: nostr follow, nostr unfollow, nostr following
// Source: cmd/follow.go
//
// LLMs use:
//   nostr follow alice
//   nostr follow npub1... --json
//   nostr unfollow alice --json
//   nostr following --json
//   nostr following --refresh

import "testing"

func TestLLM_Follow_Exists(t *testing.T) {
	requireCmd(t, "follow")
}

func TestLLM_Unfollow_Exists(t *testing.T) {
	requireCmd(t, "unfollow")
}

func TestLLM_Following_Exists(t *testing.T) {
	requireCmd(t, "following")
}

func TestLLM_Follow_RequiresOneArg(t *testing.T) {
	cmd := requireCmd(t, "follow")
	if cmd.Args == nil {
		t.Error("follow should require exactly 1 argument (<user>)")
	}
}

func TestLLM_Unfollow_RequiresOneArg(t *testing.T) {
	cmd := requireCmd(t, "unfollow")
	if cmd.Args == nil {
		t.Error("unfollow should require exactly 1 argument (<user>)")
	}
}

func TestLLM_Follow_JSONFlag(t *testing.T) {
	for _, name := range []string{"follow", "unfollow", "following"} {
		t.Run(name, func(t *testing.T) {
			requireFlag(t, requireCmd(t, name), "json")
		})
	}
}

func TestLLM_Following_RefreshFlag(t *testing.T) {
	requireFlag(t, requireCmd(t, "following"), "refresh")
}

func TestLLM_Social_InSocialGroup(t *testing.T) {
	for _, name := range []string{"follow", "unfollow", "following"} {
		t.Run(name, func(t *testing.T) {
			cmd := requireCmd(t, name)
			if cmd.GroupID != "social" {
				t.Errorf("group = %q, want \"social\"", cmd.GroupID)
			}
		})
	}
}
