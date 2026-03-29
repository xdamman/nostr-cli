package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/xdamman/nostr-cli/internal/config"
)

// Multi-account contract tests — verify the --account flag works across commands.
// These are contract-style tests (no network calls) verifying flag presence and behavior.

// TestMultiAccount_AccountFlagInherited verifies --account is available on all subcommands.
func TestMultiAccount_AccountFlagInherited(t *testing.T) {
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
			requireFlag(t, cmd, "account")
		})
	}
}

// TestMultiAccount_AccountAcceptsNpub verifies that setting --account to an npub
// makes loadAccount() return that npub directly.
func TestMultiAccount_AccountAcceptsNpub(t *testing.T) {
	testNpub := "npub1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqsef5cw"

	old := accountFlag
	defer func() { accountFlag = old }()

	accountFlag = testNpub
	got, err := loadAccount()
	if err != nil {
		t.Fatalf("loadAccount() with npub flag: %v", err)
	}
	if got != testNpub {
		t.Errorf("loadAccount() = %q, want %q", got, testNpub)
	}
}

// TestMultiAccount_AccountFlagType verifies that --account is a string flag.
func TestMultiAccount_AccountFlagType(t *testing.T) {
	f := rootCmd.PersistentFlags().Lookup("account")
	if f == nil {
		t.Fatal("--account flag not found")
	}
	if f.Value.Type() != "string" {
		t.Errorf("--account type = %q, want \"string\"", f.Value.Type())
	}
}

// TestMultiAccount_FollowWithAccount verifies follow has --account inherited from root.
func TestMultiAccount_FollowWithAccount(t *testing.T) {
	cmd := requireCmd(t, "follow")
	requireFlag(t, cmd, "account")
}

// TestMultiAccount_DMWithAccount verifies dm has --account inherited from root.
func TestMultiAccount_DMWithAccount(t *testing.T) {
	cmd := requireCmd(t, "dm")
	requireFlag(t, cmd, "account")
}

// TestMultiAccount_PostWithAccount verifies post has --account inherited from root.
func TestMultiAccount_PostWithAccount(t *testing.T) {
	cmd := requireCmd(t, "post")
	requireFlag(t, cmd, "account")
}

// TestMultiAccount_EventsWithAccount verifies events has --account inherited from root.
func TestMultiAccount_EventsWithAccount(t *testing.T) {
	cmd := requireCmd(t, "events")
	requireFlag(t, cmd, "account")
}

// TestMultiAccount_LoadAccountReturnsFlag verifies loadAccount() returns the flag value when set.
func TestMultiAccount_LoadAccountReturnsFlag(t *testing.T) {
	testNpub := "npub1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqsef5cw"

	old := accountFlag
	defer func() { accountFlag = old }()

	accountFlag = testNpub
	got, err := loadAccount()
	if err != nil {
		t.Fatalf("loadAccount() error: %v", err)
	}
	if got != testNpub {
		t.Errorf("loadAccount() = %q, want %q", got, testNpub)
	}
}

// TestMultiAccount_LoadAccountFallback verifies loadAccount() falls back to ActiveProfile()
// when --account is not set.
func TestMultiAccount_LoadAccountFallback(t *testing.T) {
	// Set up a temp config dir with an active profile
	dir := t.TempDir()
	config.BaseDirOverride = dir
	defer func() { config.BaseDirOverride = "" }()

	npub := "npub1testprofile1234567890abcdefghijklmnopqrstuvwxyz12345"
	profDir := filepath.Join(dir, "accounts", npub)
	os.MkdirAll(profDir, 0700)
	os.WriteFile(filepath.Join(profDir, "nsec"), []byte("nsec1test\n"), 0600)
	config.SetActiveProfile(npub)

	old := accountFlag
	defer func() { accountFlag = old }()

	accountFlag = ""
	got, err := loadAccount()
	if err != nil {
		t.Fatalf("loadAccount() fallback error: %v", err)
	}
	if got != npub {
		t.Errorf("loadAccount() fallback = %q, want %q", got, npub)
	}
}
