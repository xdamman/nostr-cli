package cache

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// CachedProfile holds profile metadata keyed by hex pubkey.
type CachedProfile struct {
	PubKey      string `json:"pubkey"`
	Name        string `json:"name,omitempty"`
	DisplayName string `json:"display_name,omitempty"`
	About       string `json:"about,omitempty"`
	Picture     string `json:"picture,omitempty"`
	NIP05       string `json:"nip05,omitempty"`
	Banner      string `json:"banner,omitempty"`
	Website     string `json:"website,omitempty"`
	LUD16       string `json:"lud16,omitempty"`
	FetchedAt   int64  `json:"fetched_at"`
}

// DisplayName returns the best available name for display.
func (p *CachedProfile) BestName() string {
	if p.Name != "" {
		return p.Name
	}
	if p.DisplayName != "" {
		return p.DisplayName
	}
	return ""
}

var (
	profileCacheMu   sync.RWMutex
	profileCacheMap  map[string]*CachedProfile // hex pubkey → profile
	profileCacheNpub string                    // which npub's cache is loaded
	profileCacheInit sync.Once
)

func profilesFile(npub string) string {
	return filepath.Join(CacheDir(npub), "profiles.jsonl")
}

// LoadProfileCache loads the profiles cache into memory for the given npub.
// Safe to call multiple times; only loads once per npub.
func LoadProfileCache(npub string) {
	profileCacheMu.Lock()
	if profileCacheNpub == npub && profileCacheMap != nil {
		profileCacheMu.Unlock()
		return
	}
	profileCacheNpub = npub
	profileCacheMap = make(map[string]*CachedProfile)
	profileCacheMu.Unlock()

	f, err := os.Open(profilesFile(npub))
	if err != nil {
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 256*1024), 256*1024)

	profileCacheMu.Lock()
	defer profileCacheMu.Unlock()
	for scanner.Scan() {
		var p CachedProfile
		if json.Unmarshal(scanner.Bytes(), &p) == nil && p.PubKey != "" {
			profileCacheMap[p.PubKey] = &p
		}
	}
}

// GetProfile returns a cached profile by hex pubkey, or nil if not cached.
func GetProfile(pubHex string) *CachedProfile {
	profileCacheMu.RLock()
	defer profileCacheMu.RUnlock()
	if profileCacheMap == nil {
		return nil
	}
	return profileCacheMap[pubHex]
}

// PutProfile stores a profile in the in-memory cache and appends to disk.
// Only writes to disk for accounts that have an nsec file (local accounts).
func PutProfile(npub string, p *CachedProfile) error {
	if p == nil || p.PubKey == "" {
		return nil
	}
	if !IsLocalProfile(npub) {
		return nil
	}
	p.FetchedAt = time.Now().Unix()

	profileCacheMu.Lock()
	if profileCacheMap == nil {
		profileCacheMap = make(map[string]*CachedProfile)
	}
	profileCacheMap[p.PubKey] = p
	profileCacheMu.Unlock()

	// Append to disk
	dir := CacheDir(npub)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	data, err := json.Marshal(p)
	if err != nil {
		return err
	}

	f, err := os.OpenFile(profilesFile(npub), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = f.Write(append(data, '\n'))
	return err
}

// IsProfileStale returns true if the profile was fetched more than maxAge ago,
// or if it doesn't exist.
func IsProfileStale(pubHex string, maxAge time.Duration) bool {
	p := GetProfile(pubHex)
	if p == nil {
		return true
	}
	return time.Since(time.Unix(p.FetchedAt, 0)) > maxAge
}

// ResolveNameByHex returns the best display name for a hex pubkey from cache.
// Returns empty string if not cached.
func ResolveNameByHex(pubHex string) string {
	p := GetProfile(pubHex)
	if p == nil {
		return ""
	}
	return p.BestName()
}
