# nostr

> A human-friendly command-line interface for the [Nostr](https://nostr.com) protocol.

<!-- badges -->
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.21+-00ADD8?logo=go)](https://go.dev)

## Why?

Interacting with Nostr shouldn't require a GUI. `nostr` gives you a fast, scriptable CLI that works like `git` — manage multiple profiles, switch contexts, and publish events from your terminal.

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
- 🐚 Interactive shell with feed, posting, and slash commands

## Installation

### Homebrew (macOS / Linux)

```bash
brew install xdamman/tap/nostr
```

### Shell script (macOS / Linux)

```bash
curl -sf https://raw.githubusercontent.com/xdamman/nostr-cli/main/install.sh | sh
```

### Debian / Ubuntu (.deb)

Download the `.deb` package from the [latest release](https://github.com/xdamman/nostr-cli/releases/latest), then:

```bash
sudo dpkg -i nostr_*.deb
```

### Fedora / RHEL / openSUSE (.rpm)

Download the `.rpm` package from the [latest release](https://github.com/xdamman/nostr-cli/releases/latest), then:

```bash
sudo rpm -i nostr_*.rpm
```

### From source (requires Go)

```bash
git clone https://github.com/xdamman/nostr-cli.git
cd nostr-cli
make install   # installs as `nostr` in $GOPATH/bin
```

### Update

```bash
nostr update
```

## Quick Start

```bash
# Create or import a profile
nostr login

# Launch the interactive shell (feed + posting + slash commands)
nostr

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
| `nostr version` | Print version info |
| `nostr update` | Check for updates and self-update |

> **Tip:** Append `--help` to any command for usage details — e.g. `nostr dm --help`

## Interactive Shell

Run `nostr` with no arguments to launch the interactive shell:

- Shows your feed from followed users
- Type to post a note
- Slash commands: `/follow`, `/unfollow`, `/dm`, `/profile`, `/switch`, `/alias`, `/aliases`
- Tab/arrow-key autocomplete for slash commands

## Configuration

All state lives in `~/.nostr/`:

```
~/.nostr/
└── profiles/
    └── <npub>/
        ├── nsec              # Private key (chmod 600)
        ├── profile.json      # Kind 0 metadata
        ├── relays.json       # Preferred relay list
        ├── aliases.csv       # Contact aliases
        └── cache/
            ├── events.jsonl      # Cached events
            └── profiles.jsonl    # Cached profile metadata
```

Each profile is isolated — relays, aliases, and keys are scoped per identity.

## Testing

```bash
# Run all tests (includes integration tests that hit real relays)
go test ./...

# Run unit tests only (skip relay integration tests)
go test -short ./...
```

## Releasing

Releases are automated via [GoReleaser](https://goreleaser.com/) and GitHub Actions. Pushing a version tag triggers a build that cross-compiles binaries for all platforms and publishes them to GitHub Releases and the Homebrew tap.

### Supported platforms

| OS | Architecture | Notes |
|----|-------------|-------|
| macOS | Intel (amd64) | Intel Macs |
| macOS | Apple Silicon (arm64) | M1/M2/M3/M4 Macs |
| Linux | x86_64 (amd64) | Standard PCs, servers |
| Linux | ARM64 (arm64) | Raspberry Pi, Asahi Linux, Snapdragon laptops |
| Windows | x86_64 (amd64) | Standard PCs |
| Windows | ARM64 (arm64) | Snapdragon PCs (Surface Pro X, etc.) |

### How to release

```bash
# 1. Make sure main is clean and tests pass
git checkout main
git pull
go test ./...

# 2. Tag a new version (follow semver)
git tag v0.2.0
git push origin v0.2.0
```

That's it. The GitHub Action will:
- Cross-compile 6 binaries (3 OS × 2 architectures)
- Create a GitHub Release with changelogs and downloadable archives
- Update the Homebrew formula in [xdamman/homebrew-tap](https://github.com/xdamman/homebrew-tap)

Users will then get the new version via `nostr update`, `brew upgrade nostr`, or re-running the install script.

### Version numbers

The version is derived entirely from the git tag — there is no version file to edit. GoReleaser injects the tag into the binary at build time via ldflags.

## Contributing

Contributions welcome! Check [docs/ROADMAP.md](docs/ROADMAP.md) for planned features and [docs/COMMANDS.md](docs/COMMANDS.md) for command specs.

1. Fork the repo
2. Create a feature branch
3. Make sure tests pass: `go test ./...`
4. Submit a PR

## License

[MIT](LICENSE)
