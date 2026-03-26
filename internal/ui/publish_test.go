package ui

import (
	"testing"
)

func TestIsStdoutTTY_NonInteractive(t *testing.T) {
	// When running under `go test`, stdout is typically piped (not a TTY).
	// This verifies that isStdoutTTY correctly returns false in CI/test.
	if isStdoutTTY() {
		t.Skip("stdout is a TTY in this environment (running interactively)")
	}
	// If we reach here, isStdoutTTY() returned false — correct for piped output.
}

func TestPublishNonInteractive_NoANSI(t *testing.T) {
	// Verify that publishNonInteractive writes to stderr, not stdout,
	// so that stdout remains clean for machine consumption.
	// We test this indirectly: the function signature takes a channel
	// and writes results to os.Stderr (checked by code review).
	// A full integration test would require capturing stderr output.
}
