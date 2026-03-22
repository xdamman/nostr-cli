package cache

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/nbd-wtf/go-nostr"
	"github.com/xdamman/nostr-cli/internal/config"
)

// IsLocalProfile returns true if the npub has an nsec file (i.e., is a logged-in profile).
func IsLocalProfile(npub string) bool {
	return config.HasNsec(npub)
}

var (
	seenMu sync.Mutex
	seen   = make(map[string]map[string]bool) // npub -> event ID -> true
)

// CacheDir returns the cache directory path for the given npub.
func CacheDir(npub string) string {
	dir, err := config.ProfileDir(npub)
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "cache")
}

// eventsFile returns the path to the events.jsonl file.
func eventsFile(npub string) string {
	return filepath.Join(CacheDir(npub), "events.jsonl")
}

// loadSeen loads the set of known event IDs for deduplication.
func loadSeen(npub string) map[string]bool {
	seenMu.Lock()
	defer seenMu.Unlock()

	if s, ok := seen[npub]; ok {
		return s
	}

	s := make(map[string]bool)
	seen[npub] = s

	f, err := os.Open(eventsFile(npub))
	if err != nil {
		return s
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		var ev nostr.Event
		if json.Unmarshal(scanner.Bytes(), &ev) == nil && ev.ID != "" {
			s[ev.ID] = true
		}
	}
	return s
}

// LogEvent appends an event as a JSON line to the cache, deduplicating by ID.
// Only writes to profiles that have an nsec file (local profiles).
func LogEvent(npub string, event nostr.Event) error {
	if npub == "" || event.ID == "" {
		return nil
	}

	if !IsLocalProfile(npub) {
		return nil
	}

	s := loadSeen(npub)

	seenMu.Lock()
	if s[event.ID] {
		seenMu.Unlock()
		return nil
	}
	s[event.ID] = true
	seenMu.Unlock()

	dir := CacheDir(npub)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create cache dir: %w", err)
	}

	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	f, err := os.OpenFile(eventsFile(npub), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open events file: %w", err)
	}
	defer f.Close()

	_, err = f.Write(append(data, '\n'))
	return err
}

// LogEvents caches multiple events at once.
func LogEvents(npub string, events []*nostr.Event) {
	for _, ev := range events {
		if ev != nil {
			_ = LogEvent(npub, *ev)
		}
	}
}

// QueryEvents reads all cached events and returns those matching the filter function.
func QueryEvents(npub string, filter func(nostr.Event) bool) ([]nostr.Event, error) {
	f, err := os.Open(eventsFile(npub))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var result []nostr.Event
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		var ev nostr.Event
		if json.Unmarshal(scanner.Bytes(), &ev) == nil && filter(ev) {
			result = append(result, ev)
		}
	}
	return result, scanner.Err()
}

// GetEventsByKind returns cached events of the specified kind.
func GetEventsByKind(npub string, kind int) ([]nostr.Event, error) {
	return QueryEvents(npub, func(ev nostr.Event) bool {
		return ev.Kind == kind
	})
}

// GetEventsByAuthor returns cached events by the specified author (hex pubkey).
func GetEventsByAuthor(npub string, pubkey string) ([]nostr.Event, error) {
	return QueryEvents(npub, func(ev nostr.Event) bool {
		return ev.PubKey == pubkey
	})
}
