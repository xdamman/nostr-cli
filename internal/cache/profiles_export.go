package cache

// GetAllProfiles returns a snapshot of all cached profiles.
func GetAllProfiles() map[string]*CachedProfile {
	profileCacheMu.RLock()
	defer profileCacheMu.RUnlock()
	if profileCacheMap == nil {
		return nil
	}
	out := make(map[string]*CachedProfile, len(profileCacheMap))
	for k, v := range profileCacheMap {
		out[k] = v
	}
	return out
}
