# Roadmap

## Phase 1 — MVP

The basics. Login, publish, read.

- [ ] `nostr login` — generate or import keypair, create profile directory
- [ ] `nostr profile` — view active profile (kind 0)
- [ ] `nostr profile update` — interactive metadata editor
- [ ] `nostr post` — publish kind 1 text notes
- [ ] `nostr relays` — list, add, remove relays
- [ ] Config scaffold — `~/.nostr/` directory structure, active profile pointer
- [ ] Default relay list for new profiles

**Goal:** You can create an identity, set relays, and post notes.

## Phase 2 — Social

Interactions with other users.

- [ ] `nostr follow` / `nostr unfollow` — manage contact list (kind 3)
- [ ] `nostr dm` — send encrypted DMs (NIP-44, NIP-04 fallback)
- [ ] `nostr dm` interactive mode — real-time chat
- [ ] `nostr switch` — switch between profiles
- [ ] `nostr alias` — create/list/remove local aliases
- [ ] `nostr [user]` — view profile + recent notes
- [ ] User resolution — resolve aliases, usernames, NIP-05, npubs

**Goal:** Full social interaction from the terminal.

## Phase 3 — Power User

Advanced features for daily use.

- [ ] `nostr [user] --watch` — live-stream notes
- [ ] `nostr nip[N]` — in-terminal NIP viewer
- [ ] `nostr post --reply` — reply to events
- [ ] `nostr post --json` — output signed event without publishing
- [ ] Relay status indicator — show connection health
- [ ] Profile caching — offline-first profile display
- [ ] NIP-05 verification display

**Goal:** Comfortable enough to replace casual GUI use.

## Future

Ideas for later. No promises, no timeline.

- **Notifications** — subscribe to mentions and replies
- **Media** — upload and attach images (NIP-94, Blossom)
- **Zaps** — send/receive zaps (NIP-57)
- **Relay discovery** — auto-discover relays from contact list
