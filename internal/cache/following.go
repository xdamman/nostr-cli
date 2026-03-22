package cache

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// FollowingCache holds the cached contact list.
type FollowingCache struct {
	Hexes     []string  `json:"hexes"`
	UpdatedAt time.Time `json:"updated_at"`
}

// followingFile returns the path to the following.json cache file.
func followingFile(npub string) string {
	return filepath.Join(CacheDir(npub), "following.json")
}

// SaveFollowing caches the following list for the given profile.
func SaveFollowing(npub string, hexes []string) error {
	if npub == "" || !IsLocalProfile(npub) {
		return nil
	}
	dir := CacheDir(npub)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	data, err := json.Marshal(FollowingCache{
		Hexes:     hexes,
		UpdatedAt: time.Now(),
	})
	if err != nil {
		return err
	}
	return os.WriteFile(followingFile(npub), data, 0644)
}

// LoadFollowing reads the cached following list. Returns nil if not cached.
func LoadFollowing(npub string) *FollowingCache {
	data, err := os.ReadFile(followingFile(npub))
	if err != nil {
		return nil
	}
	var fc FollowingCache
	if json.Unmarshal(data, &fc) != nil {
		return nil
	}
	return &fc
}

// IsFollowingStale returns true if the following cache is older than maxAge.
func IsFollowingStale(npub string, maxAge time.Duration) bool {
	fc := LoadFollowing(npub)
	if fc == nil {
		return true
	}
	return time.Since(fc.UpdatedAt) > maxAge
}
