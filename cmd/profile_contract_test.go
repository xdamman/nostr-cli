package cmd

// LLM contract tests for: nostr profile, nostr profiles
// Source: cmd/profile.go, cmd/profiles.go
//
// LLMs use:
//   nostr profile alice --json
//   nostr profile npub1... --refresh --json
//   nostr profiles --json
//   nostr profiles rm [name]

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

func TestLLM_Profiles_Exists(t *testing.T) {
	requireCmd(t, "profiles")
}

func TestLLM_Profiles_RmExists(t *testing.T) {
	requireCmd(t, "profiles", "rm")
}

func TestLLM_Profiles_Flags(t *testing.T) {
	cmd := requireCmd(t, "profiles")
	t.Run("--json", func(t *testing.T) { requireFlag(t, cmd, "json") })
}

func TestLLM_Profiles_InProfileGroup(t *testing.T) {
	cmd := requireCmd(t, "profiles")
	if cmd.GroupID != "profile" {
		t.Errorf("group = %q, want \"profile\"", cmd.GroupID)
	}
}
