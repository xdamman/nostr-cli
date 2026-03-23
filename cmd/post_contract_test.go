package cmd

// LLM contract tests for: nostr post
// Source: cmd/post.go
//
// LLMs use:
//   nostr post "Hello Nostr"
//   nostr post "Reply" --reply <event-id> --json
//   echo "My message" | nostr post
//   nostr post "Message" --json | jq '.id'

import "testing"

func TestLLM_Post_Exists(t *testing.T) {
	requireCmd(t, "post")
}

func TestLLM_Post_Flags(t *testing.T) {
	cmd := requireCmd(t, "post")
	t.Run("--json", func(t *testing.T) { requireFlag(t, cmd, "json") })
	t.Run("--reply", func(t *testing.T) { requireFlag(t, cmd, "reply") })
}

func TestLLM_Post_AcceptsOptionalMessage(t *testing.T) {
	// `nostr post` with no args enters interactive mode or reads stdin.
	// `nostr post "Hello"` passes message as args.
	// Either must be accepted — the command must NOT have strict arg validation.
	cmd := requireCmd(t, "post")
	if cmd.Args != nil {
		if err := cmd.Args(cmd, []string{}); err != nil {
			t.Error("post should accept 0 args (interactive/stdin)")
		}
		if err := cmd.Args(cmd, []string{"Hello Nostr"}); err != nil {
			t.Error("post should accept 1+ args (message)")
		}
	}
}

func TestLLM_Post_InSocialGroup(t *testing.T) {
	cmd := requireCmd(t, "post")
	if cmd.GroupID != "social" {
		t.Errorf("group = %q, want \"social\"", cmd.GroupID)
	}
}
