package config

import (
	"encoding/csv"
	"os"
	"path/filepath"
	"testing"

	"github.com/xdamman/nostr-cli/internal/crypto"
)

func setupTestDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	BaseDirOverride = dir
	t.Cleanup(func() { BaseDirOverride = "" })
	// Create profiles dir
	os.MkdirAll(filepath.Join(dir, "profiles"), 0700)
	return dir
}

func createTestProfile(t *testing.T, dir, npub string) {
	t.Helper()
	profDir := filepath.Join(dir, "profiles", npub)
	os.MkdirAll(profDir, 0700)
	os.WriteFile(filepath.Join(profDir, "nsec"), []byte("nsec1test\n"), 0600)
}

// setupTestDirWithProfile creates a temp dir with an active profile.
func setupTestDirWithProfile(t *testing.T) (string, string) {
	t.Helper()
	dir := setupTestDir(t)
	npub := "npub1testprofile1234567890abcdefghijklmnopqrstuvwxyz12345"
	createTestProfile(t, dir, npub)
	SetActiveProfile(npub)
	return dir, npub
}

func TestSetActiveProfile_CreatesSymlink(t *testing.T) {
	dir := setupTestDir(t)
	npub := "npub1testprofile1234567890abcdefghijklmnopqrstuvwxyz12345"
	createTestProfile(t, dir, npub)

	if err := SetActiveProfile(npub); err != nil {
		t.Fatalf("SetActiveProfile failed: %v", err)
	}

	// Check active file/link exists
	activePath := filepath.Join(dir, "active")
	info, err := os.Lstat(activePath)
	if err != nil {
		t.Fatalf("active file not found: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Error("active is not a symlink")
	}
}

func TestAliases_CreateAndResolve(t *testing.T) {
	_, activeNpub := setupTestDirWithProfile(t)
	targetNpub := "npub1abc123def456ghi789jkl012mno345pqr678stu901vwx234yz5"

	if err := SetAlias(activeNpub, "xavier", targetNpub); err != nil {
		t.Fatalf("SetAlias failed: %v", err)
	}

	resolved, err := ResolveAlias("xavier")
	if err != nil {
		t.Fatalf("ResolveAlias failed: %v", err)
	}
	if resolved != targetNpub {
		t.Errorf("ResolveAlias = %q, want %q", resolved, targetNpub)
	}

	// Case-insensitive
	resolved, err = ResolveAlias("Xavier")
	if err != nil {
		t.Fatalf("ResolveAlias case-insensitive failed: %v", err)
	}
	if resolved != targetNpub {
		t.Errorf("ResolveAlias case-insensitive = %q, want %q", resolved, targetNpub)
	}
}

func TestAliases_Remove(t *testing.T) {
	_, activeNpub := setupTestDirWithProfile(t)
	targetNpub := "npub1abc123def456ghi789jkl012mno345pqr678stu901vwx234yz5"

	SetAlias(activeNpub, "xavier", targetNpub)

	if err := RemoveAlias(activeNpub, "xavier"); err != nil {
		t.Fatalf("RemoveAlias failed: %v", err)
	}

	_, err := ResolveAlias("xavier")
	if err == nil {
		t.Error("expected error after removing alias, got nil")
	}
}

func TestAliases_RemoveNotFound(t *testing.T) {
	setupTestDirWithProfile(t)

	err := RemoveGlobalAlias("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent alias, got nil")
	}
}

func TestAliases_ScopedPerProfile(t *testing.T) {
	dir := setupTestDir(t)
	npub1 := "npub1testprofile1234567890abcdefghijklmnopqrstuvwxyz12345"
	npub2 := "npub1otherprofile234567890abcdefghijklmnopqrstuvwxyz12345"
	createTestProfile(t, dir, npub1)
	createTestProfile(t, dir, npub2)

	target := "npub1target0000000000000000000000000000000000000000000000"

	// Set alias on profile 1
	SetAlias(npub1, "alice", target)

	// Profile 1 should resolve it
	resolved, err := ResolveAliasFor(npub1, "alice")
	if err != nil {
		t.Fatalf("expected alias on profile 1: %v", err)
	}
	if resolved != target {
		t.Errorf("got %q, want %q", resolved, target)
	}

	// Profile 2 should NOT have it
	_, err = ResolveAliasFor(npub2, "alice")
	if err == nil {
		t.Error("expected error: alias should not exist on profile 2")
	}
}

func TestLoadResolvedProfile_WithAlias(t *testing.T) {
	_, activeNpub := setupTestDirWithProfile(t)
	targetNpub := "npub1abc123def456ghi789jkl012mno345pqr678stu901vwx234yz5"

	SetAlias(activeNpub, "xavier", targetNpub)

	resolved, err := LoadResolvedProfile("xavier")
	if err != nil {
		t.Fatalf("LoadResolvedProfile failed: %v", err)
	}
	if resolved != targetNpub {
		t.Errorf("LoadResolvedProfile = %q, want %q", resolved, targetNpub)
	}
}

func TestLoadResolvedProfile_WithNpub(t *testing.T) {
	setupTestDir(t)
	npub := "npub1abc123def456ghi789jkl012mno345pqr678stu901vwx234yz5"

	resolved, err := LoadResolvedProfile(npub)
	if err != nil {
		t.Fatalf("LoadResolvedProfile failed: %v", err)
	}
	if resolved != npub {
		t.Errorf("LoadResolvedProfile = %q, want %q", resolved, npub)
	}
}

func TestLoadResolvedProfile_FallsBackToActive(t *testing.T) {
	dir := setupTestDir(t)
	npub := "npub1testprofile1234567890abcdefghijklmnopqrstuvwxyz12345"
	createTestProfile(t, dir, npub)
	SetActiveProfile(npub)

	resolved, err := LoadResolvedProfile("")
	if err != nil {
		t.Fatalf("LoadResolvedProfile failed: %v", err)
	}
	if resolved != npub {
		t.Errorf("LoadResolvedProfile = %q, want %q", resolved, npub)
	}
}

func TestLoadResolvedProfile_UnknownAlias(t *testing.T) {
	setupTestDirWithProfile(t)

	_, err := LoadResolvedProfile("nonexistent")
	if err == nil {
		t.Error("expected error for unknown alias, got nil")
	}
}

func TestLoadResolvedProfile_WithUsername(t *testing.T) {
	dir := setupTestDir(t)
	npub := "npub1testprofile1234567890abcdefghijklmnopqrstuvwxyz12345"
	createTestProfile(t, dir, npub)
	SetActiveProfile(npub)

	// Write a profile.json with a name
	profDir := filepath.Join(dir, "profiles", npub)
	os.WriteFile(filepath.Join(profDir, "profile.json"),
		[]byte(`{"name":"alice","display_name":"Alice Wonder"}`), 0644)

	// Resolve by name
	resolved, err := LoadResolvedProfile("alice")
	if err != nil {
		t.Fatalf("LoadResolvedProfile(\"alice\") failed: %v", err)
	}
	if resolved != npub {
		t.Errorf("got %q, want %q", resolved, npub)
	}

	// Resolve by display_name (case-insensitive)
	resolved, err = LoadResolvedProfile("alice wonder")
	if err != nil {
		t.Fatalf("LoadResolvedProfile(\"alice wonder\") failed: %v", err)
	}
	if resolved != npub {
		t.Errorf("got %q, want %q", resolved, npub)
	}
}

func TestLoadResolvedProfile_UsernameInCacheDir(t *testing.T) {
	dir := setupTestDir(t)
	npub := "npub1testprofile1234567890abcdefghijklmnopqrstuvwxyz12345"
	createTestProfile(t, dir, npub)
	SetActiveProfile(npub)

	// Write profile.json in cache subdir (non-local profile pattern)
	cacheDir := filepath.Join(dir, "profiles", npub, "cache")
	os.MkdirAll(cacheDir, 0700)
	os.WriteFile(filepath.Join(cacheDir, "profile.json"),
		[]byte(`{"name":"bob"}`), 0644)

	resolved, err := LoadResolvedProfile("bob")
	if err != nil {
		t.Fatalf("LoadResolvedProfile(\"bob\") failed: %v", err)
	}
	if resolved != npub {
		t.Errorf("got %q, want %q", resolved, npub)
	}
}

func TestMigrateAliases_CSV(t *testing.T) {
	dir, npub := setupTestDirWithProfile(t)

	// Write a per-profile aliases.csv
	csvPath := filepath.Join(dir, "profiles", npub, "aliases.csv")
	f, err := os.Create(csvPath)
	if err != nil {
		t.Fatal(err)
	}
	w := csv.NewWriter(f)
	w.Write([]string{"alice", "npub1alice000000000000000000000000000000000000000000000"})
	w.Write([]string{"bob", "npub1bob00000000000000000000000000000000000000000000000"})
	w.Flush()
	f.Close()

	if err := MigrateAliases(); err != nil {
		t.Fatalf("MigrateAliases failed: %v", err)
	}

	// Check aliases are now in the profile
	aliases, err := LoadAliases(npub)
	if err != nil {
		t.Fatal(err)
	}
	if aliases["alice"] != "npub1alice000000000000000000000000000000000000000000000" {
		t.Errorf("alice = %q", aliases["alice"])
	}
	if aliases["bob"] != "npub1bob00000000000000000000000000000000000000000000000" {
		t.Errorf("bob = %q", aliases["bob"])
	}

	// CSV should be renamed
	if _, err := os.Stat(csvPath); !os.IsNotExist(err) {
		t.Error("aliases.csv should have been renamed")
	}
	if _, err := os.Stat(csvPath + ".migrated"); err != nil {
		t.Error("aliases.csv.migrated should exist")
	}
}

func TestMigrateAliases_GlobalFile(t *testing.T) {
	dir, npub := setupTestDirWithProfile(t)

	// Write a legacy global aliases.json
	globalPath := filepath.Join(dir, "aliases.json")
	os.WriteFile(globalPath, []byte(`{"alice":"npub1alice000000000000000000000000000000000000000000000"}`), 0644)

	if err := MigrateAliases(); err != nil {
		t.Fatalf("MigrateAliases failed: %v", err)
	}

	// Should be in profile now
	aliases, _ := LoadAliases(npub)
	if aliases["alice"] != "npub1alice000000000000000000000000000000000000000000000" {
		t.Errorf("alice = %q", aliases["alice"])
	}

	// Global file should be removed
	if _, err := os.Stat(globalPath); !os.IsNotExist(err) {
		t.Error("global aliases.json should have been removed")
	}
}

func TestMigrateAliases_NoConflict(t *testing.T) {
	dir, npub := setupTestDirWithProfile(t)

	existingNpub := "npub1existing00000000000000000000000000000000000000000000"
	SetAlias(npub, "alice", existingNpub)

	// Write a CSV with conflicting "alice"
	csvPath := filepath.Join(dir, "profiles", npub, "aliases.csv")
	f, _ := os.Create(csvPath)
	w := csv.NewWriter(f)
	w.Write([]string{"alice", "npub1different0000000000000000000000000000000000000000000"})
	w.Flush()
	f.Close()

	MigrateAliases()

	// Existing alias should NOT be overwritten
	aliases, _ := LoadAliases(npub)
	if aliases["alice"] != existingNpub {
		t.Errorf("alice should be %q (existing), got %q", existingNpub, aliases["alice"])
	}
}

func TestLoadAliases_Empty(t *testing.T) {
	_, npub := setupTestDirWithProfile(t)

	aliases, err := LoadAliases(npub)
	if err != nil {
		t.Fatal(err)
	}
	if len(aliases) != 0 {
		t.Errorf("expected 0 aliases, got %d", len(aliases))
	}
}

func TestActiveProfile_NonNpubDir(t *testing.T) {
	dir := setupTestDir(t)

	// Generate a real keypair
	nsec, npub, _, err := crypto.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}

	// Create a profile directory with a username instead of npub
	profDir := filepath.Join(dir, "profiles", "alice")
	os.MkdirAll(profDir, 0700)
	os.WriteFile(filepath.Join(profDir, "nsec"), []byte(nsec), 0600)

	// Set active profile pointing to the username dir
	os.Symlink("profiles/alice", filepath.Join(dir, "active"))

	// ActiveProfile should derive the real npub from the nsec
	got, err := ActiveProfile()
	if err != nil {
		t.Fatalf("ActiveProfile() failed: %v", err)
	}
	if got != npub {
		t.Errorf("ActiveProfile() = %q, want %q", got, npub)
	}
}

func TestProfileDir_NonNpubDir(t *testing.T) {
	dir := setupTestDir(t)

	// Generate a real keypair
	nsec, npub, _, err := crypto.GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}

	// Create profile directory with a username
	profDir := filepath.Join(dir, "profiles", "alice")
	os.MkdirAll(profDir, 0700)
	os.WriteFile(filepath.Join(profDir, "nsec"), []byte(nsec), 0600)
	os.WriteFile(filepath.Join(profDir, "aliases.json"),
		[]byte(`{"bob":"npub1bob00000000000000000000000000000000000000000000000"}`), 0644)

	// Set active symlink to the username dir
	os.Symlink("profiles/alice", filepath.Join(dir, "active"))

	// ProfileDir with the real npub should find the "alice" directory via active symlink
	resolved, err := ProfileDir(npub)
	if err != nil {
		t.Fatalf("ProfileDir(%q) failed: %v", npub, err)
	}
	if resolved != profDir {
		t.Errorf("ProfileDir(%q) = %q, want %q", npub, resolved, profDir)
	}

	// Aliases should be loadable via the real npub
	aliases, err := LoadAliases(npub)
	if err != nil {
		t.Fatalf("LoadAliases(%q) failed: %v", npub, err)
	}
	if aliases["bob"] == "" {
		t.Error("expected to find alias 'bob' via npub lookup")
	}
}
