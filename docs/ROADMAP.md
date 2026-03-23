# Roadmap

## Phase 1 — MVP

The basics. Login, publish, read.

- [x] `nostr login` — generate or import keypair, create profile directory
- [x] `nostr profile` — view active profile (kind 0)
- [x] `nostr profile update` — interactive metadata editor
- [x] `nostr post` — publish kind 1 text notes
- [x] `nostr relays` — list, add, remove relays
- [x] Config scaffold — `~/.nostr/` directory structure, active profile pointer
- [x] Default relay list for new profiles

**Goal:** You can create an identity, set relays, and post notes.

## Phase 2 — Social

Interactions with other users.

- [x] `nostr follow` / `nostr unfollow` — manage contact list (kind 3)
- [x] `nostr dm` — send encrypted DMs (NIP-44, NIP-04 fallback)
- [x] `nostr dm` interactive mode — real-time chat
- [x] `nostr switch` — switch between profiles
- [x] `nostr alias` — create/list/remove local aliases
- [x] `nostr [user]` — view profile + recent notes
- [x] User resolution — resolve aliases, usernames, NIP-05, npubs

**Goal:** Full social interaction from the terminal.

## Phase 3 — Power User

Advanced features for daily use.

- [x] `nostr [user] --watch` — live-stream notes
- [x] `nostr nip[N]` — in-terminal NIP viewer
- [x] `nostr post --reply` — reply to events
- [x] `nostr post --json` — output signed event without publishing
- [x] Relay status indicator — show connection health
- [x] Profile caching — offline-first profile display
- [x] NIP-05 verification display

**Goal:** Comfortable enough to replace casual GUI use.

## Phase 4 — Polish

UX refinements and operational improvements.

- [x] `nostr login --new` — skip interactive prompt for new keypair
- [x] Global aliases — aliases work across profiles, not per-profile
- [x] Interactive relay checklist during login
- [x] NIP-65 relay list fetch on import
- [x] Alias prompt during login (defaults to username)
- [x] `nostr profiles` — list all local profiles
- [x] `nostr profiles rm` — interactive profile removal
- [x] `nostr profile --refresh` — force fetch from relays
- [x] Per-relay publish progress — real-time status with timing
- [x] `--timeout` flag — configurable per-relay timeout (default 2s)
- [x] Sent events log — `events.jsonl` at profile root for backup
- [x] Column-aligned `nostr switch` with name, alias, npub, relays
- [x] Colored output — dim/cyan for switch, profile displays

**Goal:** Polished, fast, transparent CLI experience.

## Future

Ideas for later. No promises, no timeline.

- **Notifications** — subscribe to mentions and replies
- **Media** — upload and attach images (NIP-94, Blossom)
- **Zaps** — send/receive zaps (NIP-57)
- **Relay discovery** — auto-discover relays from contact list
- **Interactive shell** — full REPL with feed, DMs, slash commands
- **`nostr update`** — self-update mechanism
