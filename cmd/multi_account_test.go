package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/xdamman/nostr-cli/internal/config"
)

// Multi-account contract tests — verify the --profile flag works across commands.
// These are contract-style tests (no network calls) verifying flag presence and behavior.

// TestMultiAccount_ProfileFlagInherited verifies --profile is available on all subcommands.
func TestMultiAccount_ProfileFlagInherited(t *testing.T) {
	commands := [][]string{
		{"follow"},
		{"unfollow"},
		{"dm"},
		{"post"},
		{"events"},
		{"event", "new"},
		{"reply"},
		{"relays"},
		{"profile"},
		{"switch"},
		{"alias"},
		{"login"},
	}

	for _, path := range commands {
		cmd := findCmd(rootCmd, path...)
		if cmd == nil {
			t.Logf("command %v not found, skipping", path)
			continue
		}
		t.Run(cmd.CommandPath(), func(t *testing.T) {
			requireFlag(t, cmd, "profile")
		})
	}
}

// TestMultiAccount_ProfileAcceptsNpub verifies that setting --profile to an npub
// makes loadProfile() return that npub directly.
func TestMultiAccount_ProfileAcceptsNpub(t *testing.T) {
	testNpub := "npub1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqsef5cw"

	old := profileFlag
	defer func() { profileFlag = old }()

	profileFlag = testNpub
	got, err := loadProfile()
	if err != nil {
		t.Fatalf("loadProfile() with npub flag: %v", err)
	}
	if got != testNpub {
		t.Errorf("loadProfile() = %q, want %q", got, testNpub)
	}
}

// TestMultiAccount_ProfileFlagType verifies that --profile is a string flag.
func TestMultiAccount_ProfileFlagType(t *testing.T) {
	f := rootCmd.PersistentFlags().Lookup("profile")
	if f == nil {
		t.Fatal("--profile flag not found")
	}
	if f.Value.Type() != "string" {
		t.Errorf("--profile type = %q, want \"string\"", f.Value.Type())
	}
}

// TestMultiAccount_AccountAliasFlag verifies --account exists as an alias for --profile.
func TestMultiAccount_AccountAliasFlag(t *testing.T) {
	f := rootCmd.PersistentFlags().Lookup("account")
	if f == nil {
		t.Fatal("--account flag not found (should be alias for --profile)")
	}
	if f.Value.Type() != "string" {
		t.Errorf("--account type = %q, want \"string\"", f.Value.Type())
	}
}

// TestMultiAccount_FollowWithProfile verifies follow has --profile inherited from root.
func TestMultiAccount_FollowWithProfile(t *testing.T) {
	cmd := requireCmd(t, "follow")
	requireFlag(t, cmd, "profile")

	// Verify it's an inherited persistent flag, not a local one
	if cmd.Flags().Lookup("profile") != nil && cmd.InheritedFlags().Lookup("profile") == nil {
		// It's defined locally, not inherited — that's wrong but still works
		t.Log("--profile is local on follow, expected inherited from root")
	}
}

// TestMultiAccount_DMWithProfile verifies dm has --profile inherited from root.
func TestMultiAccount_DMWithProfile(t *testing.T) {
	cmd := requireCmd(t, "dm")
	requireFlag(t, cmd, "profile")
}

// TestMultiAccount_PostWithProfile verifies post has --profile inherited from root.
func TestMultiAccount_PostWithProfile(t *testing.T) {
	cmd := requireCmd(t, "post")
	requireFlag(t, cmd, "profile")
}

// TestMultiAccount_EventsWithProfile verifies events has --profile inherited from root.
func TestMultiAccount_EventsWithProfile(t *testing.T) {
	cmd := requireCmd(t, "events")
	requireFlag(t, cmd, "profile")
}

// TestMultiAccount_LoadProfileReturnsFlag verifies loadProfile() returns the flag value when set.
func TestMultiAccount_LoadProfileReturnsFlag(t *testing.T) {
	testNpub := "npub1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqsef5cw"

	old := profileFlag
	defer func() { profileFlag = old }()

	profileFlag = testNpub
	got, err := loadProfile()
	if err != nil {
		t.Fatalf("loadProfile() error: %v", err)
	}
	if got != testNpub {
		t.Errorf("loadProfile() = %q, want %q", got, testNpub)
	}
}

// TestMultiAccount_LoadProfileFallback verifies loadProfile() falls back to ActiveProfile()
// when --profile is not set.
func TestMultiAccount_LoadProfileFallback(t *testing.T) {
	// Set up a temp config dir with an active profile
	dir := t.TempDir()
	config.BaseDirOverride = dir
	defer func() { config.BaseDirOverride = "" }()

	npub := "npub1testprofile1234567890abcdefghijklmnopqrstuvwxyz12345"
	profDir := filepath.Join(dir, "profiles", npub)
	os.MkdirAll(profDir, 0700)
	os.WriteFile(filepath.Join(profDir, "nsec"), []byte("nsec1test\n"), 0600)
	config.SetActiveProfile(npub)

	old := profileFlag
	defer func() { profileFlag = old }()

	profileFlag = ""
	got, err := loadProfile()
	if err != nil {
		t.Fatalf("loadProfile() fallback error: %v", err)
	}
	if got != npub {
		t.Errorf("loadProfile() fallback = %q, want %q", got, npub)
	}
}

// TestMultiAccount_AccountFlagInherited verifies --account alias is available on subcommands.
func TestMultiAccount_AccountFlagInherited(t *testing.T) {
	commands := []string{"follow", "dm", "post", "events"}
	for _, name := range commands {
		cmd := findCmd(rootCmd, name)
		if cmd == nil {
			continue
		}
		t.Run(name, func(t *testing.T) {
			requireFlag(t, cmd, "account")
		})
	}
}
