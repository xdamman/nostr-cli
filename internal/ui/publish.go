package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/nbd-wtf/go-nostr"
	"github.com/xdamman/nostr-cli/internal/cache"
	"github.com/xdamman/nostr-cli/internal/relay"
)

// PublishResult holds the outcome of publishing an event to relays.
type PublishResult struct {
	SuccessURLs []string
	TotalCount  int
}

// PublishEventToRelays publishes a signed event to the given relays with an
// interactive per-relay spinner UI showing progress, success/failure, and timing.
// It also logs the event locally and shows the backup path.
// timeout is the per-relay deadline; pass 0 to use the default (10s).
func PublishEventToRelays(npub string, event nostr.Event, relays []string, timeout time.Duration) (*PublishResult, error) {
	dim := color.New(color.Faint)
	greenFn := color.New(color.FgGreen).SprintFunc()
	redFn := color.New(color.FgRed).SprintFunc()

	// Build relay host labels and print initial spinner state
	type relayLine struct {
		host string
		url  string
	}
	rl := make([]relayLine, len(relays))
	for i, r := range relays {
		host := r
		if u, uErr := url.Parse(r); uErr == nil && u.Host != "" {
			host = u.Host
		}
		rl[i] = relayLine{host: host, url: r}
		fmt.Printf("  %s %s\n", dim.Sprint(SpinnerFrames[0]), dim.Sprint(host))
	}

	// Publish with per-relay progress
	ctx := context.Background()
	ch := relay.PublishEventWithProgress(ctx, event, relays, timeout)

	// Track results by URL
	results := make(map[string]relay.RelayResult)
	var successURLs []string

	// Render function for relay lines
	renderRelays := func(frame int) {
		fmt.Printf("\033[%dA", len(rl))
		for _, l := range rl {
			fmt.Print("\r\033[K")
			if r, ok := results[l.url]; ok {
				ms := r.Duration.Milliseconds()
				if r.OK {
					fmt.Printf("  %s %s  %s\n", greenFn("✓"), l.host, dim.Sprintf("%dms", ms))
				} else {
					fmt.Printf("  %s %s  %s\n", redFn("✗"), l.host, dim.Sprintf("%dms", ms))
				}
			} else {
				f := SpinnerFrames[frame%len(SpinnerFrames)]
				fmt.Printf("  %s %s\n", dim.Sprint(f), dim.Sprint(l.host))
			}
		}
	}

	// Animate spinners while waiting for results
	ticker := time.NewTicker(80 * time.Millisecond)
	defer ticker.Stop()
	frame := 0
	done := false

	for !done {
		select {
		case res, ok := <-ch:
			if !ok {
				done = true
				break
			}
			results[res.URL] = res
			if res.OK {
				successURLs = append(successURLs, res.URL)
			}
			renderRelays(frame)
		case <-ticker.C:
			if len(results) < len(rl) {
				frame++
				renderRelays(frame)
			}
		}
	}

	// Final render to ensure all results shown
	renderRelays(frame)

	if len(successURLs) == 0 {
		return nil, fmt.Errorf("failed to publish to any relay")
	}

	// Log event locally
	_ = cache.LogSentEvent(npub, event)

	green := color.New(color.FgGreen)
	fmt.Println()
	green.Printf("✓ Published to %d/%d relays\n", len(successURLs), len(relays))

	eventsPath := cache.SentEventsPath(npub)
	if eventsPath != "" {
		home, _ := os.UserHomeDir()
		if home != "" {
			eventsPath = strings.Replace(eventsPath, home, "~", 1)
		}
		dim.Printf("  Saved locally in %s\n", eventsPath)
	}

	return &PublishResult{
		SuccessURLs: successURLs,
		TotalCount:  len(relays),
	}, nil
}

// PrintRawEvent outputs the raw Nostr event as JSON (wire format).
func PrintRawEvent(event nostr.Event) {
	data, _ := json.MarshalIndent(event, "", "  ")
	fmt.Println(string(data))
}

// PublishJSONRelay is a per-relay result for JSON output.
type PublishJSONRelay struct {
	URL     string `json:"url"`
	OK      bool   `json:"ok"`
	DurMs   int64  `json:"duration_ms"`
	Error   string `json:"error,omitempty"`
}

// PublishJSONResult is the JSON output for a published event.
type PublishJSONResult struct {
	Event  nostr.Event        `json:"event"`
	Relays []PublishJSONRelay `json:"relays"`
}

// PublishEventSilent publishes an event to relays without interactive output.
// It logs the event locally and returns per-relay results suitable for JSON output.
func PublishEventSilent(npub string, event nostr.Event, relays []string, timeout time.Duration) (*PublishJSONResult, error) {
	ctx := context.Background()
	ch := relay.PublishEventWithProgress(ctx, event, relays, timeout)

	result := &PublishJSONResult{Event: event}
	anyOK := false

	for res := range ch {
		jr := PublishJSONRelay{
			URL:   res.URL,
			OK:    res.OK,
			DurMs: res.Duration.Milliseconds(),
		}
		if res.Err != nil {
			jr.Error = res.Err.Error()
		}
		result.Relays = append(result.Relays, jr)
		if res.OK {
			anyOK = true
		}
	}

	if anyOK {
		_ = cache.LogSentEvent(npub, event)
	}

	if !anyOK {
		return result, fmt.Errorf("failed to publish to any relay")
	}

	return result, nil
}
