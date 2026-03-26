package cache

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip19"
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

// sentEventsFile returns the path to the profile-level events.jsonl (sent events only).
func sentEventsFile(npub string) string {
	dir, err := config.ProfileDir(npub)
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "events.jsonl")
}

// SentEventsPath returns the path to the sent events file for display purposes.
func SentEventsPath(npub string) string {
	return sentEventsFile(npub)
}

// CountSentEvents returns the number of lines in the sent events file.
func CountSentEvents(npub string) int {
	path := sentEventsFile(npub)
	if path == "" {
		return 0
	}
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()
	n := 0
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 64*1024)
	for scanner.Scan() {
		if len(scanner.Bytes()) > 0 {
			n++
		}
	}
	return n
}

// LogSentEvent appends a sent event to the profile-level events.jsonl.
// This file is NOT in cache/ — it's user data meant for backup.
func LogSentEvent(npub string, event nostr.Event) error {
	if npub == "" || event.ID == "" {
		return nil
	}
	if !IsLocalProfile(npub) {
		return nil
	}

	path := sentEventsFile(npub)
	if path == "" {
		return nil
	}

	dir, _ := config.ProfileDir(npub)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	data, err := json.Marshal(event)
	if err != nil {
		return err
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = f.Write(append(data, '\n'))
	return err
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

// dmEventsFile returns the path to a DM conversation file:
// ~/.nostr/profiles/:npub/directmessages/:counterpartyNpub.jsonl
func dmEventsFile(npub, counterpartyHex string) string {
	dir, err := config.ProfileDir(npub)
	if err != nil {
		return ""
	}
	dmDir := filepath.Join(dir, "directmessages")
	os.MkdirAll(dmDir, 0700)
	counterpartyNpub, err := nip19.EncodePublicKey(counterpartyHex)
	if err != nil {
		return ""
	}
	return filepath.Join(dmDir, counterpartyNpub+".jsonl")
}

// LogDMEvent stores a DM event in the per-counterparty conversation file.
// This is user data (not cache) and is included in backups.
func LogDMEvent(npub, counterpartyHex string, event nostr.Event) error {
	if npub == "" || event.ID == "" || counterpartyHex == "" {
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

	path := dmEventsFile(npub, counterpartyHex)
	if path == "" {
		return fmt.Errorf("cannot determine DM events path")
	}

	data, err := json.Marshal(event)
	if err != nil {
		return err
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = f.Write(append(data, '\n'))
	return err
}

// QueryDMEvents reads all events from a DM conversation file.
func QueryDMEvents(npub, counterpartyHex string) ([]nostr.Event, error) {
	path := dmEventsFile(npub, counterpartyHex)
	if path == "" {
		return nil, nil
	}
	f, err := os.Open(path)
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
		if json.Unmarshal(scanner.Bytes(), &ev) == nil {
			result = append(result, ev)
		}
	}
	return result, nil
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

// LoadSentEvents reads the profile-level events.jsonl and returns all sent events,
// deduplicated by ID and sorted by CreatedAt ascending.
func LoadSentEvents(npub string) ([]nostr.Event, error) {
	path := sentEventsFile(npub)
	if path == "" {
		return nil, nil
	}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	seen := make(map[string]bool)
	var events []nostr.Event
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		var ev nostr.Event
		if json.Unmarshal(scanner.Bytes(), &ev) == nil && ev.ID != "" && !seen[ev.ID] {
			seen[ev.ID] = true
			events = append(events, ev)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	sort.Slice(events, func(i, j int) bool {
		return events[i].CreatedAt < events[j].CreatedAt
	})

	return events, nil
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
