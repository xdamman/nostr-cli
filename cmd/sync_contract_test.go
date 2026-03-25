package cmd

// LLM contract tests for: nostr sync
// Source: cmd/sync.go
//
// LLMs use:
//   nostr sync --json
//   nostr sync --relay nos.lol --json
//   nostr sync --relay wss://nos.lol --json

import "testing"

func TestLLM_Sync_Exists(t *testing.T) {
	requireCmd(t, "sync")
}

func TestLLM_Sync_JSONFlag(t *testing.T) {
	requireFlag(t, requireCmd(t, "sync"), "json")
}

func TestLLM_Sync_RelayFlag(t *testing.T) {
	requireFlag(t, requireCmd(t, "sync"), "relay")
}

func TestLLM_Sync_InInfraGroup(t *testing.T) {
	cmd := requireCmd(t, "sync")
	if cmd.GroupID != "infra" {
		t.Errorf("group = %q, want \"infra\"", cmd.GroupID)
	}
}
