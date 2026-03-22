package config

import (
	"encoding/csv"
	"os"
	"path/filepath"
	"testing"
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

func TestSetActiveProfile_CreatesSymlink(t *testing.T) {
	dir := setupTestDir(t)
	npub := "npub1testprofile1234567890abcdefghijklmnopqrstuvwxyz12345"
	createTestProfile(t, dir, npub)

	if err := SetActiveProfile(npub); err != nil {
		t.Fatalf("SetActiveProfile failed: %v", err)
	}

	link := filepath.Join(dir, "active")
	target, err := os.Readlink(link)
	if err != nil {
		t.Fatalf("active is not a symlink: %v", err)
	}
	expected := filepath.Join("profiles", npub)
	if target != expected {
		t.Errorf("symlink target = %q, want %q", target, expected)
	}
}

func TestSetActiveProfile_ReplacesDirectory(t *testing.T) {
	dir := setupTestDir(t)
	npub := "npub1testprofile1234567890abcdefghijklmnopqrstuvwxyz12345"
	createTestProfile(t, dir, npub)

	// Create "active" as a directory (the bug scenario)
	activeDir := filepath.Join(dir, "active")
	os.MkdirAll(activeDir, 0700)
	os.WriteFile(filepath.Join(activeDir, "dummy"), []byte("x"), 0644)

	if err := SetActiveProfile(npub); err != nil {
		t.Fatalf("SetActiveProfile failed: %v", err)
	}

	// Verify it's now a symlink, not a directory
	fi, err := os.Lstat(activeDir)
	if err != nil {
		t.Fatalf("active doesn't exist: %v", err)
	}
	if fi.IsDir() {
		t.Error("active is still a directory, should be a symlink")
	}
	if fi.Mode()&os.ModeSymlink == 0 {
		t.Error("active is not a symlink")
	}
}

func TestGlobalAliases_CreateAndResolve(t *testing.T) {
	setupTestDir(t)
	npub := "npub1abc123def456ghi789jkl012mno345pqr678stu901vwx234yz5"

	// Set alias
	if err := SetGlobalAlias("xavier", npub); err != nil {
		t.Fatalf("SetGlobalAlias failed: %v", err)
	}

	// Resolve it
	resolved, err := ResolveAlias("xavier")
	if err != nil {
		t.Fatalf("ResolveAlias failed: %v", err)
	}
	if resolved != npub {
		t.Errorf("ResolveAlias = %q, want %q", resolved, npub)
	}

	// Case-insensitive
	resolved, err = ResolveAlias("Xavier")
	if err != nil {
		t.Fatalf("ResolveAlias case-insensitive failed: %v", err)
	}
	if resolved != npub {
		t.Errorf("ResolveAlias case-insensitive = %q, want %q", resolved, npub)
	}
}

func TestGlobalAliases_Remove(t *testing.T) {
	setupTestDir(t)
	npub := "npub1abc123def456ghi789jkl012mno345pqr678stu901vwx234yz5"

	SetGlobalAlias("xavier", npub)

	if err := RemoveGlobalAlias("xavier"); err != nil {
		t.Fatalf("RemoveGlobalAlias failed: %v", err)
	}

	// Should no longer resolve
	_, err := ResolveAlias("xavier")
	if err == nil {
		t.Error("expected error after removing alias, got nil")
	}
}

func TestGlobalAliases_RemoveNotFound(t *testing.T) {
	setupTestDir(t)

	err := RemoveGlobalAlias("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent alias, got nil")
	}
}

func TestLoadResolvedProfile_WithAlias(t *testing.T) {
	setupTestDir(t)
	npub := "npub1abc123def456ghi789jkl012mno345pqr678stu901vwx234yz5"

	SetGlobalAlias("xavier", npub)

	resolved, err := LoadResolvedProfile("xavier")
	if err != nil {
		t.Fatalf("LoadResolvedProfile failed: %v", err)
	}
	if resolved != npub {
		t.Errorf("LoadResolvedProfile = %q, want %q", resolved, npub)
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
	setupTestDir(t)

	_, err := LoadResolvedProfile("nonexistent")
	if err == nil {
		t.Error("expected error for unknown alias, got nil")
	}
}

func TestMigrateAliases(t *testing.T) {
	dir := setupTestDir(t)
	npub := "npub1testprofile1234567890abcdefghijklmnopqrstuvwxyz12345"
	createTestProfile(t, dir, npub)

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

	// Run migration
	if err := MigrateAliases(); err != nil {
		t.Fatalf("MigrateAliases failed: %v", err)
	}

	// Check global aliases
	aliases, err := LoadGlobalAliases()
	if err != nil {
		t.Fatal(err)
	}
	if aliases["alice"] != "npub1alice000000000000000000000000000000000000000000000" {
		t.Errorf("alice = %q", aliases["alice"])
	}
	if aliases["bob"] != "npub1bob00000000000000000000000000000000000000000000000" {
		t.Errorf("bob = %q", aliases["bob"])
	}

	// Check CSV was renamed
	if _, err := os.Stat(csvPath); !os.IsNotExist(err) {
		t.Error("aliases.csv should have been renamed")
	}
	if _, err := os.Stat(csvPath + ".migrated"); err != nil {
		t.Error("aliases.csv.migrated should exist")
	}
}

func TestMigrateAliases_NoConflict(t *testing.T) {
	dir := setupTestDir(t)
	npub := "npub1testprofile1234567890abcdefghijklmnopqrstuvwxyz12345"
	createTestProfile(t, dir, npub)

	// Set a global alias first
	existingNpub := "npub1existing00000000000000000000000000000000000000000000"
	SetGlobalAlias("alice", existingNpub)

	// Write a per-profile aliases.csv with conflicting "alice"
	csvPath := filepath.Join(dir, "profiles", npub, "aliases.csv")
	f, _ := os.Create(csvPath)
	w := csv.NewWriter(f)
	w.Write([]string{"alice", "npub1different0000000000000000000000000000000000000000000"})
	w.Flush()
	f.Close()

	MigrateAliases()

	// Global alias should NOT be overwritten
	aliases, _ := LoadGlobalAliases()
	if aliases["alice"] != existingNpub {
		t.Errorf("alice should be %q (existing), got %q", existingNpub, aliases["alice"])
	}
}

func TestLoadGlobalAliases_Empty(t *testing.T) {
	setupTestDir(t)

	aliases, err := LoadGlobalAliases()
	if err != nil {
		t.Fatal(err)
	}
	if len(aliases) != 0 {
		t.Errorf("expected empty aliases, got %d", len(aliases))
	}
}
