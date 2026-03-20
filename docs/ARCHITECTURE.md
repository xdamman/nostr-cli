# Architecture

## Project Structure

```
nostr-cli/
├── main.go                 # Entry point
├── cmd/                    # Cobra command definitions
│   ├── root.go             # Root command, global flags
│   ├── login.go
│   ├── post.go
│   ├── profile.go
│   ├── follow.go
│   ├── dm.go
│   ├── relays.go
│   ├── switch.go
│   ├── alias.go
│   └── nip.go
├── internal/               # Core logic (not importable)
│   ├── profile/            # Profile CRUD, switching
│   ├── relay/              # Relay pool management, read/write
│   ├── event/              # Event creation, signing, publishing
│   ├── crypto/             # Key management, encryption (NIP-04/44)
│   ├── nip/                # NIP fetcher and renderer
│   └── config/             # Config loading, paths, defaults
├── pkg/                    # Reusable libraries (importable)
│   └── nostr/              # Thin wrappers around go-nostr if needed
├── docs/                   # Project documentation
└── go.mod
```

## Design Principles

1. **One command = one file** in `cmd/`. Each file registers a Cobra command.
2. **`internal/` owns all logic.** Commands are thin — parse flags, call internal, print output.
3. **Profile-scoped state.** Everything is relative to `~/.nostr/profiles/<npub>/`. No global mutable state.
4. **Offline-first where possible.** Cached profiles, local aliases. Network calls only when needed.

## Key Packages

### `internal/profile`
- Load/save profile metadata (kind 0)
- Resolve user input → npub (alias, username, npub, nip-05)
- Switch active profile (symlink or pointer file at `~/.nostr/active`)

### `internal/relay`
- Manage per-profile relay list (`relays.json`)
- Pool connections via `go-nostr` relay pool
- Publish events, subscribe to filters

### `internal/event`
- Build Nostr events (kind 0, 1, 3, 4, etc.)
- Sign with profile's nsec
- Timestamp, tag handling

### `internal/crypto`
- nsec/npub encoding/decoding (NIP-19)
- NIP-04 encrypted DMs (legacy)
- NIP-44 encrypted DMs (preferred)
- Key generation

### `internal/nip`
- Fetch NIP markdown from nostr-nips.com or GitHub
- Render in terminal (glamour or similar)

### `internal/config`
- Resolve `~/.nostr/` base path
- Load active profile
- Default relay list for new profiles

## State Storage

```
~/.nostr/
├── active                  # Pointer to current profile npub
└── profiles/
    └── <npub>/
        ├── nsec            # Private key (file mode 0600)
        ├── profile.json    # Cached kind 0 event
        ├── relays.json     # ["wss://relay1", "wss://relay2"]
        └── aliases.csv     # name,npub\n
```

**Security:** `nsec` files must be `0600`. The CLI should warn or refuse if permissions are too open (like SSH does with key files).

## Dependencies

| Package | Purpose |
|---------|---------|
| [nbd-wtf/go-nostr](https://github.com/nbd-wtf/go-nostr) | Core Nostr library — events, relays, NIP implementations |
| [spf13/cobra](https://github.com/spf13/cobra) | CLI framework |
| [charmbracelet/glamour](https://github.com/charmbracelet/glamour) | Terminal markdown rendering (for NIP viewer) |
| [charmbracelet/bubbletea](https://github.com/charmbracelet/bubbletea) | Interactive TUI (future: watch mode, DM chat) |

## Event Flow Example

```
User runs: nostr post "hello world"

cmd/post.go
  → loads active profile (internal/config)
  → reads nsec (internal/crypto)
  → builds kind 1 event (internal/event)
  → signs event (internal/crypto)
  → publishes to relays (internal/relay)
  → prints confirmation
```
