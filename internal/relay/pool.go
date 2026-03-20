package relay

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/nbd-wtf/go-nostr"
)

const (
	ConnectTimeout = 5 * time.Second
	FetchTimeout   = 10 * time.Second
)

// PublishEvent signs and publishes an event to the given relays.
func PublishEvent(ctx context.Context, event nostr.Event, relayURLs []string) error {
	if len(relayURLs) == 0 {
		return fmt.Errorf("no relays configured")
	}

	var (
		mu          sync.Mutex
		successURLs []string
		lastErr     error
		wg          sync.WaitGroup
	)

	for _, url := range relayURLs {
		wg.Add(1)
		go func(url string) {
			defer wg.Done()
			connectCtx, cancel := context.WithTimeout(ctx, ConnectTimeout)
			defer cancel()

			relay, err := nostr.RelayConnect(connectCtx, url)
			if err != nil {
				mu.Lock()
				lastErr = fmt.Errorf("connect to %s: %w", url, err)
				mu.Unlock()
				return
			}
			defer relay.Close()

			if err := relay.Publish(ctx, event); err != nil {
				mu.Lock()
				lastErr = fmt.Errorf("publish to %s: %w", url, err)
				mu.Unlock()
				return
			}

			mu.Lock()
			successURLs = append(successURLs, url)
			mu.Unlock()
		}(url)
	}

	wg.Wait()

	if len(successURLs) == 0 {
		if lastErr != nil {
			return fmt.Errorf("failed to publish to any relay: %w", lastErr)
		}
		return fmt.Errorf("failed to publish to any relay")
	}

	return nil
}

// FetchEvent fetches the latest event matching the filter from the given relays.
func FetchEvent(ctx context.Context, filter nostr.Filter, relayURLs []string) (*nostr.Event, error) {
	if len(relayURLs) == 0 {
		return nil, fmt.Errorf("no relays configured")
	}

	fetchCtx, cancel := context.WithTimeout(ctx, FetchTimeout)
	defer cancel()

	var (
		mu      sync.Mutex
		best    *nostr.Event
		wg      sync.WaitGroup
	)

	for _, url := range relayURLs {
		wg.Add(1)
		go func(url string) {
			defer wg.Done()
			connectCtx, connectCancel := context.WithTimeout(fetchCtx, ConnectTimeout)
			defer connectCancel()

			relay, err := nostr.RelayConnect(connectCtx, url)
			if err != nil {
				return
			}
			defer relay.Close()

			events, err := relay.QuerySync(fetchCtx, filter)
			if err != nil || len(events) == 0 {
				return
			}

			mu.Lock()
			defer mu.Unlock()
			for _, ev := range events {
				if best == nil || ev.CreatedAt > best.CreatedAt {
					best = ev
				}
			}
		}(url)
	}

	wg.Wait()
	return best, nil
}

// FetchEvents fetches all events matching the filter from relays (deduplicated by ID).
func FetchEvents(ctx context.Context, filter nostr.Filter, relayURLs []string) ([]*nostr.Event, error) {
	if len(relayURLs) == 0 {
		return nil, fmt.Errorf("no relays configured")
	}

	fetchCtx, cancel := context.WithTimeout(ctx, FetchTimeout)
	defer cancel()

	var (
		mu     sync.Mutex
		result []*nostr.Event
		seen   = make(map[string]bool)
		wg     sync.WaitGroup
	)

	for _, url := range relayURLs {
		wg.Add(1)
		go func(url string) {
			defer wg.Done()
			connectCtx, connectCancel := context.WithTimeout(fetchCtx, ConnectTimeout)
			defer connectCancel()

			r, err := nostr.RelayConnect(connectCtx, url)
			if err != nil {
				return
			}
			defer r.Close()

			events, err := r.QuerySync(fetchCtx, filter)
			if err != nil {
				return
			}

			mu.Lock()
			defer mu.Unlock()
			for _, ev := range events {
				if !seen[ev.ID] {
					seen[ev.ID] = true
					result = append(result, ev)
				}
			}
		}(url)
	}

	wg.Wait()
	return result, nil
}
