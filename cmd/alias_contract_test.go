package cmd

// LLM contract tests for: nostr alias, nostr aliases
// Source: cmd/alias.go
//
// LLMs use:
//   nostr alias alice npub1...
//   nostr alias bob alice@example.com
//   nostr aliases --json
//   nostr alias rm alice

import "testing"

func TestLLM_Alias_Exists(t *testing.T) {
	requireCmd(t, "alias")
}

func TestLLM_Aliases_Exists(t *testing.T) {
	requireCmd(t, "aliases")
}

func TestLLM_Alias_RmExists(t *testing.T) {
	requireCmd(t, "alias", "rm")
}

func TestLLM_Alias_RmRequiresOneArg(t *testing.T) {
	cmd := requireCmd(t, "alias", "rm")
	if cmd.Args == nil {
		t.Error("alias rm should require exactly 1 argument (<name>)")
	}
}

func TestLLM_Alias_InInfraGroup(t *testing.T) {
	for _, name := range []string{"alias", "aliases"} {
		t.Run(name, func(t *testing.T) {
			cmd := requireCmd(t, name)
			if cmd.GroupID != "infra" {
				t.Errorf("group = %q, want \"infra\"", cmd.GroupID)
			}
		})
	}
}
