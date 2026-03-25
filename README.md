# nostr

> A human and bot-friendly command-line interface for the [Nostr](https://nostr.com) protocol.

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
- 🔄 Sync local events with relays
- 🐚 Interactive shell with feed, posting, and slash commands

## Installation

### Homebrew (macOS / Linux)

```bash
brew install xdamman/tap/nostr
```

### Shell script (macOS / Linux)

```bash
curl -sf https://nostrcli.sh/install.sh | sh
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
nostr login                            # Create or import a profile
nostr post "Hello Nostr!"             # Post a public note
nostr dm xavier "See you at the meetup" # Send an encrypted DM
nostr                                  # Launch the interactive shell
```

## Examples

### Post a public note

```bash
$ nostr post "Hello Nostr!"

Posting as xavier to 5 relays

  Signer:    npub1xdm...a8f2
  Event ID:  a1b2c3d4e5f6...

  ✓ relay.damus.io     142ms
  ✓ nos.lol             89ms
  ✓ relay.nostr.band   203ms
  ✗ eden.nostr.land   2001ms
  ✓ relay.snort.social 312ms

✓ Published to 4/5 relays
  Saved locally in ~/.nostr/profiles/npub1xdm.../events.jsonl
```

### Pipe content to Nostr

```bash
$ echo "Hello from the command line" | nostr
Posting as xavier to 5 relays
  ✓ relay.damus.io  128ms
  ✓ nos.lol         102ms
  ...
✓ Published to 5/5 relays
```

### Send an encrypted DM

```bash
$ nostr dm xavier "See you at the meetup"
✓ DM sent to xavier
```

```bash
$ echo "Here's that link" | nostr dm xavier
✓ DM sent to xavier
```

### Interactive mode — public feed

Run `nostr` with no arguments to enter the interactive shell. You'll see your feed from followed accounts, and can type to post:

```
$ nostr

23/03 10:15  fiatjaf     working on a new relay implementation
23/03 10:18  jb55        zaps are underrated for micropayments
23/03 10:22  odell       "fix the money, fix the world"
23/03 10:30  gigi        nostr is the social layer bitcoin needed

xavier> this is amazing!
  enter to post a public note to 5 relays, ctrl+c to exit
✓ Published!
```

### Interactive mode — DM conversation

```
$ nostr dm xavier

23/03 14:01  xavier   Hey, are you coming to the meetup?
23/03 14:05  me       Yes! See you there

me> Can't wait 🎉
  enter to send an encrypted message to xavier over 5 relays, ctrl+c to exit
✓ Sent!
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
| `nostr dm [user] [message]` | Send a DM (interactive mode if no message given). `--json` for structured output |
| `nostr [npub\|username\|alias]` | View a user's profile and latest 10 notes |
| `nostr [user] --watch` | Live-stream a user's new notes |

### Infrastructure

| Command | Description |
|---------|-------------|
| `nostr relays` | List current relays with connection status |
| `nostr relays --relay <url\|domain>` | Show a specific relay |
| `nostr relays add wss://...` | Add a relay |
| `nostr relays rm [url\|domain\|number]` | Remove a relay (asks for confirmation) |
| `nostr sync` | Sync local events with relays (interactive) |
| `nostr sync --relay <url\|domain>` | Sync with a specific relay |
| `nostr sync --json` | Sync and output results as JSON |
| `nostr alias [name] [npub\|username]` | Create an alias for a user |

### Reference

| Command | Description |
|---------|-------------|
| `nostr nip[0-9]+` | View a NIP specification |
| `nostr version` | Print version info |
| `nostr update` | Check for updates and self-update |

> **Tip:** Append `--help` to any command for usage details — e.g. `nostr dm --help`

### Global Flags

| Flag | Description |
|------|-------------|
| `--profile <npub\|alias\|username>` | Use a specific profile instead of the active one |
| `--timeout <ms>` | Timeout per relay in milliseconds (default: 2000) |
| `--no-color` | Disable colored output |
| `--raw` | Output raw Nostr event JSON (wire format) |

## Bot / LLM Integration

nostr-cli is designed to be used by bots and LLMs.

- `--raw` — output the standard Nostr event JSON (wire format, as relays see it)
- `--json` — output enriched JSON with the event + relay publish results
- `--no-color` — strip ANSI codes for piped output
- `--watch` — stream events as they arrive (works with `--raw` and `--json`)

```bash
# Post and get the raw event (standard Nostr wire format)
nostr post "Hello from my bot" --raw | jq -r '.id'

# Post and get event + relay results
nostr post "Hello" --json | jq '.relays[] | select(.ok) | .url'

# Stream all new notes from followed accounts (JSONL)
nostr --watch --json

# Stream all incoming DMs as raw events
nostr dm --watch --raw

# Stream DMs with a specific user
nostr dm alice --watch --json

# Fetch a profile as JSON
nostr profile alice@example.com --json

# Non-interactive login
nostr login --new
nostr login --nsec nsec1...
```

### AI coding agent integration

Install the nostr skill in your AI coding agent:

```bash
# Claude Code
/install-skill https://nostrcli.sh/skill

# Codex, Cursor, Windsurf, Aider (via OpenSkills)
npx openskills install https://github.com/xdamman/nostr-cli
```

An [`AGENTS.md`](AGENTS.md) is included at the repo root for tools that auto-load it (Codex, Cursor, Augment, Gemini).

See also: [nostrcli.sh/llms.txt](https://nostrcli.sh/llms.txt)

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
        ├── events.jsonl      # Sent events (for backup)
        └── cache/
            ├── events.jsonl      # Received events (safe to delete)
            ├── relays.json       # Cached relay list
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
