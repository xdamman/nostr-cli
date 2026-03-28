package cmd

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip19"
	"github.com/xdamman/nostr-cli/internal/cache"
	"github.com/xdamman/nostr-cli/internal/config"
	"github.com/xdamman/nostr-cli/internal/crypto"
	internalRelay "github.com/xdamman/nostr-cli/internal/relay"
)

// runWatchFeed streams all new events from followed accounts.
// Output: timestamp:name:content (or JSONL with --json via postJSONOut flag on root).
func runWatchFeed() error {
	npub, err := loadProfile()
	if err != nil {
		return err
	}
	myHex, err := crypto.NpubToHex(npub)
	if err != nil {
		return err
	}
	relays, err := config.LoadRelays(npub)
	if err != nil {
		return err
	}

	// Load followed accounts
	cache.LoadProfileCache(npub)
	following := cache.LoadFollowing(npub)
	var followedHexes []string
	if following != nil {
		followedHexes = following.Hexes
	}
	if len(followedHexes) == 0 {
		// Fetch from relays
		filter := nostr.Filter{
			Authors: []string{myHex},
			Kinds:   []int{nostr.KindFollowList},
			Limit:   1,
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		ev, _ := internalRelay.FetchEvent(ctx, filter, relays)
		cancel()
		if ev != nil {
			for _, tag := range ev.Tags {
				if len(tag) >= 2 && tag[0] == "p" {
					followedHexes = append(followedHexes, tag[1])
				}
			}
		}
	}

	if len(followedHexes) == 0 {
		return fmt.Errorf("not following anyone yet — run 'nostr follow <user>' first")
	}

	// Include self
	allAuthors := append(followedHexes, myHex)

	rawMode := rawFlag
	jsonMode := jsonFlag || jsonlFlag

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var seenMu sync.Mutex
	seen := make(map[string]bool)
	var printMu sync.Mutex

	since := nostr.Now()

	for _, url := range relays {
		go func(url string) {
			connectCtx, connectCancel := context.WithTimeout(ctx, internalRelay.ConnectTimeout)
			defer connectCancel()

			relay, err := nostr.RelayConnect(connectCtx, url)
			if err != nil {
				return
			}
			defer relay.Close()

			sub, err := relay.Subscribe(ctx, nostr.Filters{
				{
					Authors: allAuthors,
					Kinds:   []int{nostr.KindTextNote},
					Since:   &since,
				},
			})
			if err != nil {
				return
			}
			defer sub.Unsub()

			for {
				select {
				case <-ctx.Done():
					return
				case ev, ok := <-sub.Events:
					if !ok {
						return
					}

					seenMu.Lock()
					if seen[ev.ID] {
						seenMu.Unlock()
						continue
					}
					seen[ev.ID] = true
					seenMu.Unlock()

					_ = cache.LogFeedEvent(npub, *ev)

					// Resolve author name
					authorName := cache.ResolveNameByHex(ev.PubKey)
					if authorName == "" {
						// Try alias
						authorNpub, _ := nip19.EncodePublicKey(ev.PubKey)
						authorName = resolveProfileName(authorNpub)
					}
					if authorName == "" {
						authorNpub, _ := nip19.EncodePublicKey(ev.PubKey)
						if len(authorNpub) > 20 {
							authorName = authorNpub[:20] + "..."
						} else {
							authorName = authorNpub
						}
					}

					ts := time.Unix(int64(ev.CreatedAt), 0)
					content := strings.ReplaceAll(ev.Content, "\n", " ")

					printMu.Lock()
					if rawMode {
						printRaw(ev)
					} else if jsonMode {
						entry := map[string]interface{}{
							"timestamp": ts.Format(time.RFC3339),
							"from":      authorName,
							"content":   ev.Content,
							"event_id":  ev.ID,
							"pubkey":    ev.PubKey,
							"kind":      ev.Kind,
						}
						if jsonlFlag {
							printJSONL(entry)
						} else {
							printJSON(entry)
						}
					} else {
						fmt.Printf("%s:%s:%s\n", ts.Format("2006-01-02T15:04:05"), authorName, content)
						os.Stdout.Sync()
					}
					printMu.Unlock()
				}
			}
		}(url)
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
	cancel()
	return nil
}
