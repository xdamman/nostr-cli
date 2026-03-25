package cmd

// LLM contract tests for: nostr relays
// Source: cmd/relays.go
//
// LLMs use:
//   nostr relays --json
//   nostr relays add wss://relay.example.com
//   nostr relays rm 1
//   nostr relays rm wss://relay.example.com

import "testing"

func TestLLM_Relays_Exists(t *testing.T) {
	requireCmd(t, "relays")
}

func TestLLM_Relays_AddExists(t *testing.T) {
	requireCmd(t, "relays", "add")
}

func TestLLM_Relays_RmExists(t *testing.T) {
	requireCmd(t, "relays", "rm")
}

func TestLLM_Relays_JSONFlag(t *testing.T) {
	requireFlag(t, requireCmd(t, "relays"), "json")
}

func TestLLM_Relays_AddRequiresOneArg(t *testing.T) {
	cmd := requireCmd(t, "relays", "add")
	if cmd.Args == nil {
		t.Error("relays add should require exactly 1 argument (<url>)")
	}
}

func TestLLM_Relays_RmRequiresOneArg(t *testing.T) {
	cmd := requireCmd(t, "relays", "rm")
	if cmd.Args == nil {
		t.Error("relays rm should require exactly 1 argument (<url|number>)")
	}
}

func TestLLM_Relays_RelayFlag(t *testing.T) {
	requireFlag(t, requireCmd(t, "relays"), "relay")
}

func TestLLM_Relays_RmYesFlag(t *testing.T) {
	cmd := requireCmd(t, "relays", "rm")
	requireFlag(t, cmd, "yes")
	// Check short flag -y
	f := cmd.Flags().ShorthandLookup("y")
	if f == nil {
		t.Error("relays rm missing -y short flag")
	}
}

func TestLLM_Relays_InInfraGroup(t *testing.T) {
	cmd := requireCmd(t, "relays")
	if cmd.GroupID != "infra" {
		t.Errorf("group = %q, want \"infra\"", cmd.GroupID)
	}
}
