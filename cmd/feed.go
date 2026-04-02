package cmd

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/nbd-wtf/go-nostr"
)

// feedEntry is either a nostr event or a plain text info line.
type feedEntry struct {
	event *nostr.Event // nil for info lines
	info  string       // non-empty for info/system lines
	ts    nostr.Timestamp
}

// feed manages a deduplicated, chronologically sorted list of entries.
type feed struct {
	entries []feedEntry
	seen    map[string]bool // event ID -> true
	maxSize int
}

func newFeed(maxSize int) feed {
	return feed{
		seen:    make(map[string]bool),
		maxSize: maxSize,
	}
}

// AddEvent inserts a nostr event. Returns false if already present.
func (f *feed) AddEvent(ev nostr.Event) bool {
	if f.seen[ev.ID] {
		return false
	}
	f.seen[ev.ID] = true
	f.entries = append(f.entries, feedEntry{event: &ev, ts: ev.CreatedAt})
	f.sort()
	f.trim()
	return true
}

// AddEvents inserts multiple events, skipping duplicates. Returns the
// number of new events actually added.
func (f *feed) AddEvents(events []nostr.Event) int {
	added := 0
	for i := range events {
		if f.seen[events[i].ID] {
			continue
		}
		f.seen[events[i].ID] = true
		f.entries = append(f.entries, feedEntry{event: &events[i], ts: events[i].CreatedAt})
		added++
	}
	if added > 0 {
		f.sort()
		f.trim()
	}
	return added
}

// AddInfo appends a plain text line (e.g. slash command output, system message).
// It's placed at the end (uses current time as timestamp).
func (f *feed) AddInfo(text string) {
	f.entries = append(f.entries, feedEntry{info: text, ts: nostr.Now()})
	f.trim()
}

// Len returns the number of entries.
func (f *feed) Len() int {
	return len(f.entries)
}

// HasEvent returns true if the event ID is already in the feed.
func (f *feed) HasEvent(id string) bool {
	return f.seen[id]
}

// Render returns the feed as a slice of rendered lines, newest at the bottom.
// myHex and promptName are used for author name resolution.
// termWidth controls line wrapping.
func (f *feed) Render(myHex, promptName string, termWidth int) []string {
	var lines []string
	for _, entry := range f.entries {
		if entry.event != nil {
			rendered := renderFeedEvent(*entry.event, myHex, promptName, termWidth)
			lines = append(lines, rendered...)
		} else if entry.info != "" {
			// Info lines may contain newlines
			for _, l := range strings.Split(entry.info, "\n") {
				lines = append(lines, l)
			}
		}
	}
	return lines
}

func (f *feed) sort() {
	sort.SliceStable(f.entries, func(i, j int) bool {
		return f.entries[i].ts < f.entries[j].ts
	})
}

func (f *feed) trim() {
	if f.maxSize > 0 && len(f.entries) > f.maxSize {
		// Remove oldest entries and their IDs from seen map
		removed := f.entries[:len(f.entries)-f.maxSize]
		for _, e := range removed {
			if e.event != nil {
				delete(f.seen, e.event.ID)
			}
		}
		f.entries = f.entries[len(f.entries)-f.maxSize:]
	}
}

// renderFeedEvent formats a single event into one or more display lines.
func renderFeedEvent(ev nostr.Event, myHex, promptName string, termW int) []string {
	ts := formatLocalTimestamp(time.Unix(int64(ev.CreatedAt), 0))
	name := resolveAuthorName(ev.PubKey)
	if ev.PubKey == myHex && name != promptName && promptName != "" {
		name = promptName
	}
	nw := updateFeedNameWidth(name)
	prefixLen := 14 + 2 + nw + 2

	content := wrapNote(ev.Content, prefixLen)

	tsStr := dimStyle.Render(ts + "  ")
	nameStr := cyanStyle.Render(fmt.Sprintf("%-*s", nw, name)) + ": "
	full := tsStr + nameStr + content

	return strings.Split(full, "\n")
}
