package cache

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// NIP05CacheEntry stores a cached NIP-05 verification result.
type NIP05CacheEntry struct {
	Npub      string `json:"npub"`
	NIP05     string `json:"nip05"`
	Verified  bool   `json:"verified"`
	CheckedAt int64  `json:"checked_at"`
}

// nip05CachePath returns the path to the NIP-05 cache file for the given npub.
func nip05CachePath(npub string) string {
	dir := CacheDir(npub)
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "nip05.json")
}

// LoadNIP05Cache loads the cached NIP-05 verification result for the given npub.
// Returns nil if not cached or expired (older than maxAge).
func LoadNIP05Cache(npub string, maxAge time.Duration) *NIP05CacheEntry {
	path := nip05CachePath(npub)
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var entry NIP05CacheEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		return nil
	}
	if time.Since(time.Unix(entry.CheckedAt, 0)) > maxAge {
		return nil
	}
	return &entry
}

// SaveNIP05Cache stores a NIP-05 verification result in the cache.
func SaveNIP05Cache(npub string, nip05Addr string, verified bool) error {
	path := nip05CachePath(npub)
	if path == "" {
		return nil
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	entry := NIP05CacheEntry{
		Npub:      npub,
		NIP05:     nip05Addr,
		Verified:  verified,
		CheckedAt: time.Now().Unix(),
	}
	data, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
