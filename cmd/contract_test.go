package cmd

// LLM contract tests — shared helpers.
//
// These tests verify the CLI surface that LLMs and bots depend on.
// If any test fails, the documentation in website/skill.md and
// website/llms.txt is out of sync with the code, and LLMs will
// issue commands that don't work.
//
// Tests are split by command group:
//   contract_test.go          — helpers + global flags + discovery files
//   post_contract_test.go     — nostr post          (cmd/post.go)
//   dm_contract_test.go       — nostr dm            (cmd/dm.go)
//   profile_contract_test.go  — nostr profile/s     (cmd/profile.go, cmd/profiles.go)
//   social_contract_test.go   — nostr follow/unfollow/following (cmd/follow.go)
//   relay_contract_test.go    — nostr relays        (cmd/relays.go)
//   alias_contract_test.go    — nostr alias/aliases  (cmd/alias.go)
//   identity_contract_test.go — nostr login/switch  (cmd/login.go, cmd/switch.go)

import (
	"os"
	"testing"

	"github.com/spf13/cobra"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// findCmd walks the command tree and returns the command at the given path.
// Example: findCmd(rootCmd, "relays", "add") → `nostr relays add`.
func findCmd(root *cobra.Command, path ...string) *cobra.Command {
	cmd := root
	for _, name := range path {
		found := false
		for _, child := range cmd.Commands() {
			if child.Name() == name {
				cmd = child
				found = true
				break
			}
		}
		if !found {
			return nil
		}
	}
	return cmd
}

// hasFlag returns true if cmd (or its parents) exposes the named flag.
func hasFlag(cmd *cobra.Command, name string) bool {
	return cmd.Flags().Lookup(name) != nil || cmd.InheritedFlags().Lookup(name) != nil
}

// requireCmd is a test helper that fails if the command doesn't exist.
func requireCmd(t *testing.T, path ...string) *cobra.Command {
	t.Helper()
	cmd := findCmd(rootCmd, path...)
	if cmd == nil {
		t.Fatalf("command %q not found — LLMs expect this (see skill.md)", path)
	}
	return cmd
}

// requireFlag fails if the command doesn't have the named flag.
func requireFlag(t *testing.T, cmd *cobra.Command, name string) {
	t.Helper()
	if !hasFlag(cmd, name) {
		t.Errorf("missing --%s flag on %q", name, cmd.CommandPath())
	}
}

// ---------------------------------------------------------------------------
// Global flags  (cmd/root.go)
// ---------------------------------------------------------------------------

func TestLLM_GlobalFlags(t *testing.T) {
	t.Run("--profile exists", func(t *testing.T) {
		if rootCmd.PersistentFlags().Lookup("profile") == nil {
			t.Error("missing --profile global flag")
		}
	})
	t.Run("--timeout exists", func(t *testing.T) {
		if rootCmd.PersistentFlags().Lookup("timeout") == nil {
			t.Error("missing --timeout global flag")
		}
	})
	t.Run("--timeout default is 2000", func(t *testing.T) {
		f := rootCmd.PersistentFlags().Lookup("timeout")
		if f != nil && f.DefValue != "2000" {
			t.Errorf("--timeout default = %q, want \"2000\"", f.DefValue)
		}
	})
	t.Run("--no-color exists", func(t *testing.T) {
		f := rootCmd.PersistentFlags().Lookup("no-color")
		if f == nil {
			t.Error("missing --no-color global flag")
		} else if f.DefValue != "false" {
			t.Errorf("--no-color default = %q, want \"false\"", f.DefValue)
		}
	})
}

func TestLLM_GlobalFlagsInherited(t *testing.T) {
	// LLMs use: nostr post "msg" --timeout 5000 --no-color --profile alice
	commands := []string{"post", "dm", "profile", "follow", "unfollow", "following", "relays", "sync"}
	globals := []string{"profile", "timeout", "no-color"}

	for _, name := range commands {
		cmd := findCmd(rootCmd, name)
		if cmd == nil {
			t.Errorf("command %q not found", name)
			continue
		}
		for _, flag := range globals {
			t.Run(name+"/"+flag, func(t *testing.T) {
				requireFlag(t, cmd, flag)
			})
		}
	}
}

// ---------------------------------------------------------------------------
// Root command  (cmd/root.go)
// ---------------------------------------------------------------------------

func TestLLM_Root_AcceptsArbitraryArgs(t *testing.T) {
	// Needed for `echo "msg" | nostr` and `nostr <user>`.
	if rootCmd.Args == nil {
		t.Fatal("rootCmd.Args should be set")
	}
	if err := rootCmd.Args(rootCmd, []string{}); err != nil {
		t.Errorf("rootCmd should accept 0 args (pipe/shell): %v", err)
	}
	if err := rootCmd.Args(rootCmd, []string{"someuser"}); err != nil {
		t.Errorf("rootCmd should accept 1 arg (user lookup): %v", err)
	}
}

// ---------------------------------------------------------------------------
// Discovery files  (website/)
// ---------------------------------------------------------------------------

func TestLLM_DiscoveryFiles(t *testing.T) {
	files := []string{
		"../website/skill/SKILL.md",
		"../website/llms.txt",
		"../AGENTS.md",
	}
	for _, path := range files {
		t.Run(path, func(t *testing.T) {
			if _, err := os.Stat(path); err != nil {
				t.Errorf("LLM discovery file missing: %v", err)
			}
		})
	}
}
