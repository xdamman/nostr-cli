package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/xdamman/nostr-cli/internal/config"
	"github.com/xdamman/nostr-cli/internal/resolve"
)

func setupCmdTestDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	config.BaseDirOverride = dir
	t.Cleanup(func() { config.BaseDirOverride = "" })
	os.MkdirAll(filepath.Join(dir, "profiles"), 0700)
	return dir
}

func createCmdTestProfile(t *testing.T, dir, npub string) {
	t.Helper()
	profDir := filepath.Join(dir, "profiles", npub)
	os.MkdirAll(profDir, 0700)
	os.WriteFile(filepath.Join(profDir, "nsec"), []byte("nsec1test\n"), 0600)
}

func TestLoadProfile_ResolvesAlias(t *testing.T) {
	dir := setupCmdTestDir(t)
	npub := "npub1ycsauae9zj8cd4qwt4g9lydujvk8t9vy0neska92j47kwuwy84pqzkw0se"
	createCmdTestProfile(t, dir, npub)
	config.SetActiveProfile(npub)
	config.SetAlias(npub, "xavier", npub)

	// Save and restore accountFlag
	old := accountFlag
	defer func() { accountFlag = old }()

	accountFlag = "xavier"
	resolved, err := loadAccount()
	if err != nil {
		t.Fatalf("loadAccount() with alias failed: %v", err)
	}
	if resolved != npub {
		t.Errorf("resolved = %q, want %q", resolved, npub)
	}
}

func TestLoadProfile_ResolvesNpub(t *testing.T) {
	setupCmdTestDir(t)
	npub := "npub1ycsauae9zj8cd4qwt4g9lydujvk8t9vy0neska92j47kwuwy84pqzkw0se"

	old := accountFlag
	defer func() { accountFlag = old }()

	accountFlag = npub
	resolved, err := loadAccount()
	if err != nil {
		t.Fatalf("loadAccount() with npub failed: %v", err)
	}
	if resolved != npub {
		t.Errorf("resolved = %q, want %q", resolved, npub)
	}
}

func TestLoadProfile_FallsBackToActive(t *testing.T) {
	dir := setupCmdTestDir(t)
	npub := "npub1ycsauae9zj8cd4qwt4g9lydujvk8t9vy0neska92j47kwuwy84pqzkw0se"
	createCmdTestProfile(t, dir, npub)
	config.SetActiveProfile(npub)

	old := accountFlag
	defer func() { accountFlag = old }()

	accountFlag = ""
	resolved, err := loadAccount()
	if err != nil {
		t.Fatalf("loadAccount() with no flag failed: %v", err)
	}
	if resolved != npub {
		t.Errorf("resolved = %q, want %q", resolved, npub)
	}
}

func TestResolveToNpub_Alias(t *testing.T) {
	dir := setupCmdTestDir(t)
	activeNpub := "npub1testprofile1234567890abcdefghijklmnopqrstuvwxyz12345"
	createCmdTestProfile(t, dir, activeNpub)
	config.SetActiveProfile(activeNpub)

	targetNpub := "npub1ycsauae9zj8cd4qwt4g9lydujvk8t9vy0neska92j47kwuwy84pqzkw0se"
	config.SetAlias(activeNpub, "xavier", targetNpub)

	resolved, err := resolve.ResolveToNpub(activeNpub, "xavier")
	if err != nil {
		t.Fatalf("ResolveToNpub(\"xavier\") failed: %v", err)
	}
	if resolved != targetNpub {
		t.Errorf("resolved = %q, want %q", resolved, targetNpub)
	}

	// Case-insensitive
	resolved, err = resolve.ResolveToNpub(activeNpub, "Xavier")
	if err != nil {
		t.Fatalf("ResolveToNpub(\"Xavier\") failed: %v", err)
	}
	if resolved != targetNpub {
		t.Errorf("resolved = %q, want %q", resolved, targetNpub)
	}
}

func TestProfileCommand_FlagExists(t *testing.T) {
	found := false
	for _, cmd := range rootCmd.Commands() {
		if cmd.Name() == "profile" {
			found = true
			jsonFlag := cmd.Flags().Lookup("json")
			if jsonFlag == nil {
				t.Error("profile command missing --json flag")
			}
			subFound := false
			for _, sub := range cmd.Commands() {
				if sub.Name() == "update" {
					subFound = true
				}
			}
			if !subFound {
				t.Error("profile command missing 'update' subcommand")
			}
			break
		}
	}
	if !found {
		t.Error("profile command not registered on rootCmd")
	}
}

func TestLoginNewFlagExists(t *testing.T) {
	found := false
	for _, cmd := range rootCmd.Commands() {
		if cmd.Name() == "login" {
			found = true
			newFlag := cmd.Flags().Lookup("new")
			if newFlag == nil {
				t.Error("login command missing --new flag")
			}
			genFlag := cmd.Flags().Lookup("generate")
			if genFlag == nil {
				t.Error("login command missing --generate flag")
			}
			break
		}
	}
	if !found {
		t.Error("login command not registered on rootCmd")
	}
}
