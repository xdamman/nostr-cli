package config

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "embed"
)

//go:embed default-relays.json
var DefaultRelaysJSON []byte

// BaseDirOverride allows tests to override the base directory.
var BaseDirOverride string

// BaseDir returns the ~/.nostr directory path.
func BaseDir() (string, error) {
	if BaseDirOverride != "" {
		return BaseDirOverride, nil
	}
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

// ActiveProfile reads the active profile npub from the ~/.nostr/active symlink target.
func ActiveProfile() (string, error) {
	base, err := BaseDir()
	if err != nil {
		return "", err
	}
	link := filepath.Join(base, "active")
	target, err := os.Readlink(link)
	if err != nil {
		// Backwards compat: try reading as a plain file
		data, fileErr := os.ReadFile(link)
		if fileErr == nil {
			npub := strings.TrimSpace(string(data))
			// Migrate: replace the file with a symlink
			_ = SetActiveProfile(npub)
			return npub, nil
		}
		// No symlink and no file — try to auto-resolve
		return autoResolveProfile(base)
	}
	// The symlink target is "profiles/<npub>", extract the npub
	return filepath.Base(target), nil
}

// autoResolveProfile finds available profiles and either auto-selects or returns a helpful error.
func autoResolveProfile(base string) (string, error) {
	profilesDir := filepath.Join(base, "profiles")
	entries, err := os.ReadDir(profilesDir)
	if err != nil {
		return "", fmt.Errorf("no profile set up yet. Run 'nostr login' first")
	}

	var npubs []string
	for _, e := range entries {
		if !e.IsDir() || e.Type()&os.ModeSymlink != 0 {
			continue
		}
		name := e.Name()
		if strings.HasPrefix(name, "npub1") {
			// Only count profiles that have an nsec file
			if HasNsec(name) {
				npubs = append(npubs, name)
			}
		}
	}

	switch len(npubs) {
	case 0:
		return "", fmt.Errorf("no profile set up yet. Run 'nostr login' first")
	case 1:
		// Auto-select the only profile
		_ = SetActiveProfile(npubs[0])
		return npubs[0], nil
	default:
		return "", fmt.Errorf("no active profile. Run 'nostr switch' to select a profile")
	}
}

// SetActiveProfile creates a symlink ~/.nostr/active -> ~/.nostr/profiles/<npub>.
func SetActiveProfile(npub string) error {
	base, err := BaseDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(base, 0700); err != nil {
		return err
	}
	link := filepath.Join(base, "active")
	// Remove existing symlink, file, or directory
	if fi, err := os.Lstat(link); err == nil {
		if fi.IsDir() && fi.Mode()&os.ModeSymlink == 0 {
			os.RemoveAll(link)
		} else {
			os.Remove(link)
		}
	}
	// Create relative symlink: active -> profiles/<npub>
	return os.Symlink(filepath.Join("profiles", npub), link)
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
		return nil, fmt.Errorf("no relays configured. Run 'nostr relays add' first")
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

// CreateProfileSymlink creates a symlink ~/.nostr/profiles/<alias> -> ~/.nostr/profiles/<npub>.
func CreateProfileSymlink(alias, npub string) error {
	base, err := BaseDir()
	if err != nil {
		return err
	}
	link := filepath.Join(base, "profiles", alias)
	target := filepath.Join(base, "profiles", npub)

	// Don't create if target doesn't exist
	if _, err := os.Stat(target); os.IsNotExist(err) {
		return nil
	}

	// Remove existing symlink if present
	if fi, err := os.Lstat(link); err == nil {
		if fi.Mode()&os.ModeSymlink != 0 {
			os.Remove(link)
		} else {
			return fmt.Errorf("%s already exists and is not a symlink", alias)
		}
	}

	return os.Symlink(npub, link)
}

// RemoveProfileSymlink removes a profile symlink if it exists.
func RemoveProfileSymlink(alias string) error {
	base, err := BaseDir()
	if err != nil {
		return err
	}
	link := filepath.Join(base, "profiles", alias)
	fi, err := os.Lstat(link)
	if err != nil {
		return nil // doesn't exist, nothing to do
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return os.Remove(link)
	}
	return nil
}

// HasNsec checks whether the given profile directory contains an nsec file.
func HasNsec(npub string) bool {
	dir, err := ProfileDir(npub)
	if err != nil {
		return false
	}
	_, err = os.Stat(filepath.Join(dir, "nsec"))
	return err == nil
}

// aliasesFilePath returns the path to the global aliases.json file.
func aliasesFilePath() (string, error) {
	base, err := BaseDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "aliases.json"), nil
}

// LoadGlobalAliases reads the global aliases.json file.
func LoadGlobalAliases() (map[string]string, error) {
	path, err := aliasesFilePath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]string), nil
		}
		return nil, err
	}
	var aliases map[string]string
	if err := json.Unmarshal(data, &aliases); err != nil {
		return nil, fmt.Errorf("invalid aliases.json: %w", err)
	}
	return aliases, nil
}

// SaveGlobalAliases writes the global aliases.json file.
func SaveGlobalAliases(aliases map[string]string) error {
	path, err := aliasesFilePath()
	if err != nil {
		return err
	}
	base := filepath.Dir(path)
	if err := os.MkdirAll(base, 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(aliases, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// ResolveAlias looks up an alias in the global aliases.json and returns the npub.
func ResolveAlias(alias string) (string, error) {
	aliases, err := LoadGlobalAliases()
	if err != nil {
		return "", err
	}
	for name, npub := range aliases {
		if strings.EqualFold(name, alias) {
			return npub, nil
		}
	}
	return "", fmt.Errorf("alias %q not found", alias)
}

// SetGlobalAlias sets an alias in the global aliases.json and creates a profile symlink.
func SetGlobalAlias(name, npub string) error {
	aliases, err := LoadGlobalAliases()
	if err != nil {
		return err
	}
	aliases[name] = npub
	if err := SaveGlobalAliases(aliases); err != nil {
		return err
	}
	// Create profile symlink (best-effort)
	_ = CreateProfileSymlink(name, npub)
	return nil
}

// RemoveGlobalAlias removes an alias from the global aliases.json and its profile symlink.
func RemoveGlobalAlias(name string) error {
	aliases, err := LoadGlobalAliases()
	if err != nil {
		return err
	}
	if _, ok := aliases[name]; !ok {
		return fmt.Errorf("alias %q not found", name)
	}
	delete(aliases, name)
	if err := SaveGlobalAliases(aliases); err != nil {
		return err
	}
	_ = RemoveProfileSymlink(name)
	return nil
}

// MigrateAliases merges per-profile aliases.csv files into the global aliases.json.
// Migrated CSV files are renamed to aliases.csv.migrated.
func MigrateAliases() error {
	base, err := BaseDir()
	if err != nil {
		return err
	}
	profilesDir := filepath.Join(base, "profiles")
	entries, err := os.ReadDir(profilesDir)
	if err != nil {
		return nil // no profiles dir, nothing to migrate
	}

	globalAliases, err := LoadGlobalAliases()
	if err != nil {
		return err
	}

	migrated := false
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), "npub1") {
			continue
		}
		csvPath := filepath.Join(profilesDir, e.Name(), "aliases.csv")
		f, err := os.Open(csvPath)
		if err != nil {
			continue // no aliases.csv
		}
		reader := csv.NewReader(f)
		records, err := reader.ReadAll()
		f.Close()
		if err != nil {
			continue
		}
		for _, rec := range records {
			if len(rec) >= 2 {
				name := rec[0]
				npub := rec[1]
				if _, exists := globalAliases[name]; !exists {
					globalAliases[name] = npub
					migrated = true
				}
			}
		}
		// Rename to .migrated
		os.Rename(csvPath, csvPath+".migrated")
	}

	if migrated {
		return SaveGlobalAliases(globalAliases)
	}
	return nil
}

// LoadResolvedProfile returns the npub to use, considering the --profile flag.
// If the flag doesn't start with "npub1", it tries to resolve it as an alias.
func LoadResolvedProfile(profileFlag string) (string, error) {
	if profileFlag != "" {
		if strings.HasPrefix(profileFlag, "npub1") {
			return profileFlag, nil
		}
		// Try alias resolution
		npub, err := ResolveAlias(profileFlag)
		if err == nil {
			return npub, nil
		}
		return "", fmt.Errorf("unknown profile or alias: %s", profileFlag)
	}
	return ActiveProfile()
}
