# Roadmap

## Phase 1 — MVP ✅

The basics. Login, publish, read.

- [x] `nostr login` — generate or import keypair, create profile directory
- [x] `nostr profile` — view active profile (kind 0)
- [x] `nostr profile update` — interactive metadata editor
- [x] `nostr post` — publish kind 1 text notes
- [x] `nostr relays` — list, add, remove relays
- [x] Config scaffold — `~/.nostr/` directory structure, active profile pointer
- [x] Default relay list for new profiles

## Phase 2 — Social ✅

Interactions with other users.

- [x] `nostr follow` / `nostr unfollow` — manage contact list (kind 3)
- [x] `nostr dm` — send encrypted DMs (NIP-44, NIP-04 fallback)
- [x] `nostr dm` interactive mode — real-time chat
- [x] `nostr switch` — switch between accounts
- [x] `nostr alias` — create/list/remove local aliases
- [x] `nostr [user]` — view profile + recent notes
- [x] User resolution — resolve aliases, usernames, NIP-05, npubs

## Phase 3 — Power User ✅

Advanced features for daily use.

- [x] `nostr [user] --watch` — live-stream notes
- [x] `nostr nip[N]` — in-terminal NIP viewer
- [x] `nostr reply` — reply to events with NIP-10 threading
- [x] `nostr post --json` — output signed event without publishing
- [x] Relay status indicator — show connection health
- [x] Profile caching — offline-first profile display
- [x] NIP-05 verification display

## Phase 4 — Polish ✅

UX refinements and operational improvements.

- [x] `nostr login --new` — skip interactive prompt for new keypair
- [x] Global aliases — aliases work across accounts, not per-account
- [x] Interactive relay checklist during login
- [x] NIP-65 relay list fetch on import
- [x] Alias prompt during login (defaults to username)
- [x] `nostr accounts` — list all local accounts (replaced `nostr profiles`)
- [x] `nostr accounts rm` — interactive account removal
- [x] `nostr profile --refresh` — force fetch from relays
- [x] Per-relay publish progress — real-time status with timing
- [x] `--timeout` flag — configurable per-relay timeout (default 2s)
- [x] Sent events log — `events.jsonl` at profile root for backup
- [x] Column-aligned `nostr switch` with name, alias, npub, relays
- [x] Colored output — dim/cyan for switch, profile displays
- [x] `--account` flag — replaced `--profile`

## Phase 5 — Events & Bot Platform ✅

Event querying, raw events, and bot-friendly features.

- [x] `nostr events` — query with `--kinds`, `--since`, `--until`, `--author`, `--limit`
- [x] `nostr events --watch` — live streaming with `--decrypt`
- [x] `nostr events --filter key=value` — tag-based filtering
- [x] `nostr events --me` — shortcut for filtering by own pubkey
- [x] `nostr event new` — raw event creation of any kind
- [x] `--tag key=value` and `--tags '<json>'` — flexible tagging on post, dm, reply, event new
- [x] Auto TTY detection — no `--pipe` flag needed
- [x] `--no-decrypt` flag — decrypt is default for kind 4
- [x] `dm --watch` stderr logging — `ready`, connection errors
- [x] `dm --watch --since` — catch-up then stream

## Phase 6 — NIP-17 & Interactive ✅

Modern DM protocol and interactive shell improvements.

- [x] NIP-17 gift-wrapped DMs — default for sending
- [x] NIP-44 encryption
- [x] Receive both NIP-04 and NIP-17
- [x] `--nip04` flag for legacy DMs
- [x] `protocol` field in JSON output
- [x] @ mention autocomplete in all input contexts
- [x] Bubbletea inline input — consolidated from raw line editor
- [x] Auto-detect DM protocol in interactive mode
- [x] Combined relay progress for NIP-17 (parallel publish)
- [x] Interactive `/switch` with arrow key picker
- [x] Interactive `/dm` with autocomplete + compose flow

## Phase 7 — Long-Form & Rendering ✅

Long-form content and terminal rendering improvements.

- [x] `nostr post -f article.md` — publish long-form content (NIP-23, kind 30023)
- [x] `nostr post --long` — built-in multi-line editor
- [x] YAML frontmatter parsing (title, summary, image, slug, hashtags, draft)
- [x] `--title`, `--summary`, `--image`, `--slug`, `--draft`, `--hashtag` flags
- [x] `nostr:npub1...` → `@username` rendering in terminal
- [x] Quoted `nostr:note1...` / `nostr:nevent1...` references
- [x] Fast profile — cache-first, NIP-05 cache, `--refresh`

## Future

Ideas for later. No promises, no timeline.

- **Notifications** — subscribe to mentions and replies
- **Media** — upload and attach images (NIP-94, Blossom)
- **Zaps** — send/receive zaps (NIP-57)
- **Relay discovery** — auto-discover relays from contact list
- **Lists** — NIP-51 lists (mute, bookmarks, etc.)
- **Relay auth** — NIP-42 authentication
- **Marketplace** — NIP-15 marketplace events
