package cmd

// LLM contract tests for: nostr login, nostr switch, nostr version, nostr update
// Source: cmd/login.go, cmd/switch.go, cmd/version.go
//
// LLMs use:
//   nostr login --new
//   nostr login --nsec nsec1...
//   nostr switch alice
//   nostr version
//   nostr update

import "testing"

func TestLLM_Login_Exists(t *testing.T) {
	requireCmd(t, "login")
}

func TestLLM_Login_Flags(t *testing.T) {
	cmd := requireCmd(t, "login")
	t.Run("--new", func(t *testing.T) { requireFlag(t, cmd, "new") })
	t.Run("--nsec", func(t *testing.T) { requireFlag(t, cmd, "nsec") })
	t.Run("--generate", func(t *testing.T) { requireFlag(t, cmd, "generate") })
}

func TestLLM_Login_InProfileGroup(t *testing.T) {
	cmd := requireCmd(t, "login")
	if cmd.GroupID != "profile" {
		t.Errorf("group = %q, want \"profile\"", cmd.GroupID)
	}
}

func TestLLM_Switch_Exists(t *testing.T) {
	requireCmd(t, "switch")
}

func TestLLM_Switch_InProfileGroup(t *testing.T) {
	cmd := requireCmd(t, "switch")
	if cmd.GroupID != "profile" {
		t.Errorf("group = %q, want \"profile\"", cmd.GroupID)
	}
}

func TestLLM_Version_Exists(t *testing.T) {
	requireCmd(t, "version")
}

func TestLLM_Update_Exists(t *testing.T) {
	requireCmd(t, "update")
}
