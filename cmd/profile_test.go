package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/xdamman/nostr-cli/internal/config"
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

func TestProfileCommand_ResolvesAlias(t *testing.T) {
	dir := setupCmdTestDir(t)
	npub := "npub1ycsauae9zj8cd4qwt4g9lydujvk8t9vy0neska92j47kwuwy84pqzkw0se"
	createCmdTestProfile(t, dir, npub)
	config.SetActiveProfile(npub)

	// Set alias
	config.SetGlobalAlias("xavier", npub)

	// LoadResolvedProfile should resolve the alias
	resolved, err := config.LoadResolvedProfile("xavier")
	if err != nil {
		t.Fatalf("LoadResolvedProfile(\"xavier\") failed: %v", err)
	}
	if resolved != npub {
		t.Errorf("resolved = %q, want %q", resolved, npub)
	}
}

func TestProfileCommand_ResolvesNpub(t *testing.T) {
	setupCmdTestDir(t)
	npub := "npub1ycsauae9zj8cd4qwt4g9lydujvk8t9vy0neska92j47kwuwy84pqzkw0se"

	resolved, err := config.LoadResolvedProfile(npub)
	if err != nil {
		t.Fatalf("LoadResolvedProfile(npub) failed: %v", err)
	}
	if resolved != npub {
		t.Errorf("resolved = %q, want %q", resolved, npub)
	}
}

func TestProfileCommand_FallsBackToActive(t *testing.T) {
	dir := setupCmdTestDir(t)
	npub := "npub1ycsauae9zj8cd4qwt4g9lydujvk8t9vy0neska92j47kwuwy84pqzkw0se"
	createCmdTestProfile(t, dir, npub)
	config.SetActiveProfile(npub)

	resolved, err := config.LoadResolvedProfile("")
	if err != nil {
		t.Fatalf("LoadResolvedProfile(\"\") failed: %v", err)
	}
	if resolved != npub {
		t.Errorf("resolved = %q, want %q", resolved, npub)
	}
}

func TestProfileCommand_UnknownAliasErrors(t *testing.T) {
	setupCmdTestDir(t)

	_, err := config.LoadResolvedProfile("nonexistent")
	if err == nil {
		t.Error("expected error for unknown alias")
	}
}

func TestProfileCommand_FlagExists(t *testing.T) {
	// Verify the profile command exists and is registered
	found := false
	for _, cmd := range rootCmd.Commands() {
		if cmd.Name() == "profile" {
			found = true
			// Verify --json flag exists
			jsonFlag := cmd.Flags().Lookup("json")
			if jsonFlag == nil {
				t.Error("profile command missing --json flag")
			}
			// Verify 'update' subcommand exists
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

func TestProfileCommand_LookupUserProfile_WithoutActiveProfile(t *testing.T) {
	setupCmdTestDir(t)
	// No active profile set, no aliases

	// lookupUserProfile should not panic with empty activeNpub
	// We can't test the full function without relays, but we can test
	// the resolution path: resolve.ResolveToNpub should work without activeNpub
	npub := "npub1ycsauae9zj8cd4qwt4g9lydujvk8t9vy0neska92j47kwuwy84pqzkw0se"

	// Set a global alias — this should work even without an active profile
	config.SetGlobalAlias("xavier", npub)

	resolved, err := config.ResolveAlias("xavier")
	if err != nil {
		t.Fatalf("ResolveAlias failed without active profile: %v", err)
	}
	if resolved != npub {
		t.Errorf("resolved = %q, want %q", resolved, npub)
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
