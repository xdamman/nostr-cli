# Caching

nostr-cli caches data locally to avoid unnecessary relay fetches and provide faster responses.

## Cache locations

All cache data lives under `~/.nostr/profiles/<npub>/cache/`:

```
~/.nostr/profiles/<npub>/
├── nsec                    # Private key (local profiles only)
├── relays.json             # Configured relays
├── profile.json            # Profile metadata (local profiles)
├── aliases.json            # Contact aliases (e.g. "alice" → npub1...)
├── events.jsonl            # Sent events (your posts, follows, profile updates — NOT cache)
├── directmessages/         # Encrypted DM conversations (NOT cache)
│   └── <counterparty>.jsonl  # All events (sent & received) with one counterparty
└── cache/
    ├── profile.json        # Cached profile metadata (non-local profiles)
    ├── relays.json         # Cached relay list from NIP-65 (non-local profiles)
    ├── events.jsonl        # Received events (feed events from others)
    ├── feed.jsonl          # Feed events from followed users
    ├── following.json      # List of followed pubkeys
    └── profiles.jsonl      # Cached profile metadata for other users
```

## What gets cached

### Profile metadata
When you run `nostr profile <user>`, the kind 0 metadata is cached locally. Subsequent runs serve from cache instantly and show when it was last refreshed:

```
Last refreshed 3 hours ago. Run with --refresh to fetch from relays.
```

### Relay lists
NIP-65 relay lists (kind 10002) are fetched and cached when viewing a profile. This allows future lookups to query the user's preferred relays.

### Feed events
Events from your followed users are cached in `cache/feed.jsonl` for faster shell startup.

### Following list
Your contact list (kind 3) is cached in `cache/following.json` so the shell can load your feed without fetching from relays every time.

## Cache freshness

Profile metadata cache is considered fresh for 1 hour. After that, `nostr profile` will still show cached data but indicate its age. Use `--refresh` to force a fresh fetch.

### Direct messages
DM conversations are stored per-counterparty in `directmessages/<hex>.jsonl`. Each file contains all encrypted DM events (both sent and received) with a single counterparty, sorted by arrival time. The filename is the counterparty's hex public key.

These are **user data, not cache** — they are not re-fetched from relays and should be backed up.

## Safely clearing cache

The `cache/` directory can be safely deleted at any time. It will be rebuilt on next use. This does NOT affect:

- Your private keys (`nsec`)
- Your relay configuration (`relays.json`)
- Your sent events (`events.jsonl`)
- Your DM conversations (`directmessages/`)

```bash
# Clear cache for a specific profile
rm -rf ~/.nostr/profiles/<npub>/cache/

# Clear all caches
rm -rf ~/.nostr/profiles/*/cache/
```
