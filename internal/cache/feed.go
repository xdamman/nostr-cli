package cache

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/nbd-wtf/go-nostr"
)

var (
	feedSeenMu sync.Mutex
	feedSeen   = make(map[string]map[string]bool) // npub -> event ID -> true
)

// feedFile returns the path to the feed.jsonl file.
func feedFile(npub string) string {
	return filepath.Join(CacheDir(npub), "feed.jsonl")
}

// loadFeedSeen loads the set of known feed event IDs for deduplication.
func loadFeedSeen(npub string) map[string]bool {
	feedSeenMu.Lock()
	defer feedSeenMu.Unlock()

	if s, ok := feedSeen[npub]; ok {
		return s
	}

	s := make(map[string]bool)
	feedSeen[npub] = s

	f, err := os.Open(feedFile(npub))
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

// LogFeedEvent appends a feed event (kind 1 from followed user) to feed.jsonl, deduplicating by ID.
func LogFeedEvent(npub string, event nostr.Event) error {
	if npub == "" || event.ID == "" {
		return nil
	}
	if !IsLocalProfile(npub) {
		return nil
	}

	s := loadFeedSeen(npub)

	feedSeenMu.Lock()
	if s[event.ID] {
		feedSeenMu.Unlock()
		return nil
	}
	s[event.ID] = true
	feedSeenMu.Unlock()

	dir := CacheDir(npub)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	data, err := json.Marshal(event)
	if err != nil {
		return err
	}

	f, err := os.OpenFile(feedFile(npub), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()

	_, err = f.Write(append(data, '\n'))
	return err
}

// LogFeedEvents caches multiple feed events at once.
func LogFeedEvents(npub string, events []*nostr.Event) {
	for _, ev := range events {
		if ev != nil {
			_ = LogFeedEvent(npub, *ev)
		}
	}
}

// LoadFeed reads the feed cache and returns the most recent events, sorted by time ascending.
// Returns up to `limit` events. If limit <= 0, returns all.
func LoadFeed(npub string, limit int) ([]nostr.Event, error) {
	f, err := os.Open(feedFile(npub))
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

	// Sort by time ascending (oldest first)
	sort.Slice(events, func(i, j int) bool {
		return events[i].CreatedAt < events[j].CreatedAt
	})

	// Return only the last `limit` events
	if limit > 0 && len(events) > limit {
		events = events[len(events)-limit:]
	}

	return events, nil
}

// LoadFeedSeen preloads the feed dedup set from disk so subsequent FeedSeenID calls are fast.
func LoadFeedSeen(npub string) {
	loadFeedSeen(npub)
}

// FeedSeenID returns true if this event ID was already seen in the feed cache.
func FeedSeenID(npub, eventID string) bool {
	s := loadFeedSeen(npub)
	feedSeenMu.Lock()
	defer feedSeenMu.Unlock()
	return s[eventID]
}
