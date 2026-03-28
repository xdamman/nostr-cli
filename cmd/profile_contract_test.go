package cmd

// LLM contract tests for: nostr profile, nostr accounts (alias: profiles)
// Source: cmd/profile.go, cmd/profiles.go
//
// LLMs use:
//   nostr profile alice --json
//   nostr profile npub1... --refresh --json
//   nostr accounts --json
//   nostr accounts rm [name]

import "testing"

func TestLLM_Profile_Exists(t *testing.T) {
	requireCmd(t, "profile")
}

func TestLLM_Profile_UpdateExists(t *testing.T) {
	requireCmd(t, "profile", "update")
}

func TestLLM_Profile_Flags(t *testing.T) {
	cmd := requireCmd(t, "profile")
	t.Run("--json", func(t *testing.T) { requireFlag(t, cmd, "json") })
	t.Run("--refresh", func(t *testing.T) { requireFlag(t, cmd, "refresh") })
}

func TestLLM_Profile_InProfileGroup(t *testing.T) {
	cmd := requireCmd(t, "profile")
	if cmd.GroupID != "profile" {
		t.Errorf("group = %q, want \"profile\"", cmd.GroupID)
	}
}

func TestLLM_Accounts_Exists(t *testing.T) {
	requireCmd(t, "accounts")
}

func TestLLM_Accounts_ProfilesAlias(t *testing.T) {
	// "profiles" should still work as an alias
	requireCmd(t, "profiles")
}

func TestLLM_Accounts_RmExists(t *testing.T) {
	requireCmd(t, "accounts", "rm")
}

func TestLLM_Accounts_Flags(t *testing.T) {
	cmd := requireCmd(t, "accounts")
	t.Run("--json", func(t *testing.T) { requireFlag(t, cmd, "json") })
}

func TestLLM_Accounts_InProfileGroup(t *testing.T) {
	cmd := requireCmd(t, "accounts")
	if cmd.GroupID != "profile" {
		t.Errorf("group = %q, want \"profile\"", cmd.GroupID)
	}
}
