package cmd

import (
	"errors"
	"testing"

	"github.com/fatih/color"
	"github.com/xdamman/nostr-cli/internal/profile"
)

func TestRunLoginReturnsWhenInitialPromptInterrupted(t *testing.T) {
	origNsec := loginNsec
	origGenerate := loginGenerate
	origNew := loginNew
	origReadMaskedInput := readLoginMaskedInput
	t.Cleanup(func() {
		loginNsec = origNsec
		loginGenerate = origGenerate
		loginNew = origNew
		readLoginMaskedInput = origReadMaskedInput
	})

	loginNsec = ""
	loginGenerate = false
	loginNew = false
	readLoginMaskedInput = func() (string, error) {
		return "", errInterrupted
	}

	err := runLogin(loginCmd, nil)
	if !errors.Is(err, errInterrupted) {
		t.Fatalf("expected interrupted error, got %v", err)
	}
}

func TestCreateMissingProfileMetadataSavesUsername(t *testing.T) {
	dir := setupCmdTestDir(t)
	npub := "npub1qqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqqsef5cw"

	meta, err := createMissingProfileMetadata(npub, "Alice Example", color.New(color.FgGreen))
	if err != nil {
		t.Fatalf("createMissingProfileMetadata() error = %v", err)
	}
	if meta == nil || meta.Name != "Alice Example" {
		t.Fatalf("metadata name = %#v, want Alice Example", meta)
	}

	got, err := profile.LoadCached(npub)
	if err != nil {
		t.Fatalf("LoadCached() error = %v", err)
	}
	if got.Name != "Alice Example" {
		t.Errorf("saved profile name = %q, want Alice Example", got.Name)
	}

	if dir == "" {
		t.Fatal("test setup did not create a config directory")
	}
}
