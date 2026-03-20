# nostr-cli

> A human-friendly command-line interface for the [Nostr](https://nostr.com) protocol.

<!-- badges -->
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.21+-00ADD8?logo=go)](https://go.dev)

## Why?

Interacting with Nostr shouldn't require a GUI. `nostr-cli` gives you a fast, scriptable CLI that works like `git` — manage multiple profiles, switch contexts, and publish events from your terminal.

- **Multi-profile support** — switch between identities like git branches
- **Human-friendly** — use aliases and usernames, not just npubs
- **Unix-native** — pipes, scripts, cron jobs — it just works

## Features

- 🔑 Login with existing nsec or generate a new keypair
- 👤 View and update profiles (kind 0)
- 📝 Post text notes (kind 1)
- 💬 Encrypted DMs (NIP-04 / NIP-44)
- 🔄 Follow/unfollow users
- 🌐 Manage relay lists per profile
- 🏷️ Create aliases for quick access to contacts
- 👥 Switch between multiple profiles
- 📖 Built-in NIP reference viewer

## Installation

```bash
# Install via go (binary will be named nostr-cli)
go install github.com/xdamman/nostr-cli@latest

# Or build from source as `nostr`
git clone https://github.com/xdamman/nostr-cli.git
cd nostr-cli
make install   # installs as `nostr` in $GOPATH/bin
```

## Quick Start

```bash
# Create or import a profile
nostr login

# Post a note
nostr post "Hello Nostr!"

# Follow someone
nostr follow npub1...

# Send a DM
nostr dm npub1... "hey there"

# View someone's profile and notes
nostr npub1...
```

## Commands

### Identity

| Command | Description |
|---------|-------------|
| `nostr login` | Create a new profile or import an existing nsec |
| `nostr switch [alias\|username\|npub]` | Switch between profiles |
| `nostr profile` | Show your current profile |
| `nostr profile [npub\|username\|alias]` | Show another user's profile |
| `nostr profile update` | Interactively update your profile fields |

### Social

| Command | Description |
|---------|-------------|
| `nostr post [message]` | Post a text note (interactive if no message given) |
| `nostr follow [npub\|username]` | Follow a user |
| `nostr dm [user] [message]` | Send a DM (interactive mode if no message given) |
| `nostr [npub\|username\|alias]` | View a user's profile and latest 10 notes |
| `nostr [user] --watch` | Live-stream a user's new notes |

### Infrastructure

| Command | Description |
|---------|-------------|
| `nostr relays` | List current relays |
| `nostr relays add wss://...` | Add a relay |
| `nostr relays rm [url\|number]` | Remove a relay |
| `nostr alias [name] [npub\|username]` | Create an alias for a user |

### Reference

| Command | Description |
|---------|-------------|
| `nostr nip[0-9]+` | View a NIP specification |

> **Tip:** Append `--help` to any command for usage details — e.g. `nostr dm --help`

## Configuration

All state lives in `~/.nostr/`:

```
~/.nostr/
└── profiles/
    └── <npub>/
        ├── nsec              # Private key (chmod 600)
        ├── profile.json      # Kind 0 metadata
        ├── relays.json       # Preferred relay list
        └── aliases.csv       # Contact aliases
```

Each profile is isolated — relays, aliases, and keys are scoped per identity.

## Contributing

Contributions welcome! Check [docs/ROADMAP.md](docs/ROADMAP.md) for planned features and [docs/COMMANDS.md](docs/COMMANDS.md) for command specs.

1. Fork the repo
2. Create a feature branch
3. Submit a PR

## License

[MIT](LICENSE)
