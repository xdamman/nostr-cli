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
	PublishTimeout = 2 * time.Second
)

// PublishEvent publishes an event to the given relays and returns
// the URLs of relays that accepted it.
func PublishEvent(ctx context.Context, event nostr.Event, relayURLs []string) ([]string, error) {
	if len(relayURLs) == 0 {
		return nil, fmt.Errorf("no relays configured")
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
			relayCtx, cancel := context.WithTimeout(ctx, PublishTimeout)
			defer cancel()

			relay, err := nostr.RelayConnect(relayCtx, url)
			if err != nil {
				mu.Lock()
				lastErr = fmt.Errorf("connect to %s: %w", url, err)
				mu.Unlock()
				return
			}
			defer relay.Close()

			if err := relay.Publish(relayCtx, event); err != nil {
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
			return nil, fmt.Errorf("failed to publish to any relay: %w", lastErr)
		}
		return nil, fmt.Errorf("failed to publish to any relay")
	}

	return successURLs, nil
}

// RelayResult holds the outcome of publishing to a single relay.
type RelayResult struct {
	URL      string
	OK       bool
	Duration time.Duration
	Err      error
}

// PublishEventWithProgress publishes an event to relays and sends per-relay results
// to the returned channel as each completes. The channel is closed when all are done.
// timeout is the per-relay deadline; if 0, PublishTimeout is used.
func PublishEventWithProgress(ctx context.Context, event nostr.Event, relayURLs []string, timeout time.Duration) <-chan RelayResult {
	if timeout <= 0 {
		timeout = PublishTimeout
	}
	ch := make(chan RelayResult, len(relayURLs))
	var wg sync.WaitGroup

	for _, u := range relayURLs {
		wg.Add(1)
		go func(u string) {
			defer wg.Done()
			start := time.Now()

			relayCtx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()

			r, err := nostr.RelayConnect(relayCtx, u)
			if err != nil {
				ch <- RelayResult{URL: u, Err: fmt.Errorf("connect: %w", err), Duration: time.Since(start)}
				return
			}
			defer r.Close()

			if err := r.Publish(relayCtx, event); err != nil {
				ch <- RelayResult{URL: u, Err: fmt.Errorf("publish: %w", err), Duration: time.Since(start)}
				return
			}

			ch <- RelayResult{URL: u, OK: true, Duration: time.Since(start)}
		}(u)
	}

	go func() {
		wg.Wait()
		close(ch)
	}()

	return ch
}

// PublishEventQuiet publishes an event to the given relays in a fire-and-forget
// fashion with no output or error reporting. Used for ephemeral events like
// typing indicators.
func PublishEventQuiet(ctx context.Context, event nostr.Event, relayURLs []string) {
	for _, u := range relayURLs {
		go func(u string) {
			relayCtx, cancel := context.WithTimeout(ctx, PublishTimeout)
			defer cancel()
			r, err := nostr.RelayConnect(relayCtx, u)
			if err != nil {
				return
			}
			defer r.Close()
			_ = r.Publish(relayCtx, event)
		}(u)
	}
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

// FetchResult holds the outcome of fetching events from a single relay.
type FetchResult struct {
	URL      string
	Events   []*nostr.Event
	Duration time.Duration
	Err      error
}

// FetchEventsPerRelay fetches events from each relay independently and sends
// per-relay results to the returned channel as each completes.
// When the filter has no Limit set (Limit == 0 and LimitZero == false),
// it paginates automatically to fetch all matching events from each relay.
func FetchEventsPerRelay(ctx context.Context, filter nostr.Filter, relayURLs []string, timeout time.Duration) <-chan FetchResult {
	if timeout <= 0 {
		timeout = FetchTimeout
	}
	ch := make(chan FetchResult, len(relayURLs))
	var wg sync.WaitGroup

	paginate := filter.Limit == 0 && !filter.LimitZero

	for _, u := range relayURLs {
		wg.Add(1)
		go func(u string) {
			defer wg.Done()
			start := time.Now()

			connCtx, connCancel := context.WithTimeout(ctx, timeout)
			r, err := nostr.RelayConnect(connCtx, u)
			connCancel()
			if err != nil {
				ch <- FetchResult{URL: u, Err: fmt.Errorf("connect: %w", err), Duration: time.Since(start)}
				return
			}
			defer r.Close()

			if !paginate {
				queryCtx, queryCancel := context.WithTimeout(ctx, timeout)
				defer queryCancel()
				events, err := r.QuerySync(queryCtx, filter)
				if err != nil {
					ch <- FetchResult{URL: u, Err: fmt.Errorf("query: %w", err), Duration: time.Since(start)}
					return
				}
				ch <- FetchResult{URL: u, Events: events, Duration: time.Since(start)}
				return
			}

			// No limit: paginate to fetch all events (timeout applies per page)
			events, err := fetchAllPaginated(ctx, r, filter, timeout)
			if err != nil {
				ch <- FetchResult{URL: u, Err: fmt.Errorf("query: %w", err), Duration: time.Since(start)}
				return
			}
			ch <- FetchResult{URL: u, Events: events, Duration: time.Since(start)}
		}(u)
	}

	go func() {
		wg.Wait()
		close(ch)
	}()

	return ch
}

// fetchAllPaginated fetches all events matching the filter by paginating
// backwards in time using the `until` field. Each page requests up to 500
// events; pagination stops when a page returns fewer than the page size.
// The pageTimeout is applied per page query, not to the entire operation.
func fetchAllPaginated(ctx context.Context, r *nostr.Relay, base nostr.Filter, pageTimeout time.Duration) ([]*nostr.Event, error) {
	const pageSize = 500
	var all []*nostr.Event
	seen := make(map[string]bool)

	page := base
	page.Limit = pageSize

	for {
		pageCtx, pageCancel := context.WithTimeout(ctx, pageTimeout)
		batch, err := r.QuerySync(pageCtx, page)
		pageCancel()
		if err != nil {
			if len(all) > 0 {
				// Return what we have so far
				return all, nil
			}
			return nil, err
		}

		newCount := 0
		var oldest nostr.Timestamp
		for _, ev := range batch {
			if seen[ev.ID] {
				continue
			}
			seen[ev.ID] = true
			all = append(all, ev)
			newCount++
			if oldest == 0 || ev.CreatedAt < oldest {
				oldest = ev.CreatedAt
			}
		}

		// Stop if we got fewer new events than page size (no more pages)
		if newCount < pageSize {
			break
		}

		// Next page: fetch events older than the oldest we've seen
		until := oldest - 1
		page.Until = &until
	}

	return all, nil
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
