package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "embed"
)

//go:embed default-relays.json
var DefaultRelaysJSON []byte

// BaseDir returns the ~/.nostr directory path.
func BaseDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("cannot determine home directory: %w", err)
	}
	return filepath.Join(home, ".nostr"), nil
}

// ProfileDir returns the path to a profile's directory.
func ProfileDir(npub string) (string, error) {
	base, err := BaseDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "profiles", npub), nil
}

// ActiveProfile reads the active profile npub from ~/.nostr/active.
func ActiveProfile() (string, error) {
	base, err := BaseDir()
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(filepath.Join(base, "active"))
	if err != nil {
		return "", fmt.Errorf("no active profile. Run 'nostr-cli login' first")
	}
	return strings.TrimSpace(string(data)), nil
}

// SetActiveProfile writes the active profile npub to ~/.nostr/active.
func SetActiveProfile(npub string) error {
	base, err := BaseDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(base, 0700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(base, "active"), []byte(npub+"\n"), 0644)
}

// EnsureProfileDir creates the profile directory if it doesn't exist.
func EnsureProfileDir(npub string) (string, error) {
	dir, err := ProfileDir(npub)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", fmt.Errorf("failed to create profile directory: %w", err)
	}
	return dir, nil
}

// SaveNsec saves the nsec to the profile directory with mode 0600.
func SaveNsec(npub, nsec string) error {
	dir, err := ProfileDir(npub)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "nsec"), []byte(nsec+"\n"), 0600)
}

// LoadNsec reads the nsec from the profile directory.
func LoadNsec(npub string) (string, error) {
	dir, err := ProfileDir(npub)
	if err != nil {
		return "", err
	}
	data, err := os.ReadFile(filepath.Join(dir, "nsec"))
	if err != nil {
		return "", fmt.Errorf("cannot read nsec: %w", err)
	}
	return strings.TrimSpace(string(data)), nil
}

// LoadRelays reads relays.json from the profile directory.
func LoadRelays(npub string) ([]string, error) {
	dir, err := ProfileDir(npub)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(dir, "relays.json"))
	if err != nil {
		return nil, fmt.Errorf("no relays configured. Run 'nostr-cli relays add' first")
	}
	var relays []string
	if err := json.Unmarshal(data, &relays); err != nil {
		return nil, fmt.Errorf("invalid relays.json: %w", err)
	}
	return relays, nil
}

// SaveRelays writes relays.json to the profile directory.
func SaveRelays(npub string, relays []string) error {
	dir, err := ProfileDir(npub)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(relays, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "relays.json"), data, 0644)
}

// SaveDefaultRelays writes the embedded default relays to the profile directory.
func SaveDefaultRelays(npub string) error {
	var relays []string
	if err := json.Unmarshal(DefaultRelaysJSON, &relays); err != nil {
		return fmt.Errorf("invalid embedded default relays: %w", err)
	}
	return SaveRelays(npub, relays)
}

// DefaultRelays returns the embedded default relay list.
func DefaultRelays() []string {
	var relays []string
	if err := json.Unmarshal(DefaultRelaysJSON, &relays); err != nil {
		return nil
	}
	return relays
}

// LoadResolvedProfile returns the npub to use, considering the --profile flag.
func LoadResolvedProfile(profileFlag string) (string, error) {
	if profileFlag != "" {
		return profileFlag, nil
	}
	return ActiveProfile()
}
