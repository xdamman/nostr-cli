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
- **Auto-detection** — colors off when piped, TTY detection for interactive features

## Features

- 🔑 Login with existing nsec or generate a new keypair
- 👤 View and update profiles (kind 0)
- 📝 Post text notes (kind 1)
- 💬 Encrypted DMs (NIP-04 / NIP-44) with interactive chat UI
- 🔎 Query events from relays with flexible filters
- 🛠️ Create raw events of any kind
- 🔄 Follow/unfollow users
- 🌐 Manage relay lists per profile
- 🏷️ Create aliases for quick access to contacts
- 👥 Switch between multiple profiles
- 📖 Built-in NIP reference viewer
- 🔄 Sync local events with relays
- 🐚 Interactive shell with feed, posting, and slash commands
- 🤖 Bot/agent-friendly output formats (--json, --jsonl, --raw)

## Installation

### Shell script (macOS / Linux)

```bash
curl -sf https://nostrcli.sh/install.sh | sh
```

### Homebrew (macOS / Linux)

```bash
brew install xdamman/tap/nostr
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
nostr login                              # Create or import a profile
nostr post "Hello Nostr!"               # Post a public note
nostr dm xavier "See you at the meetup"  # Send an encrypted DM
nostr                                    # Launch the interactive shell
```

## Commands

### Social

| Command | Description |
|---------|-------------|
| `nostr post [message]` | Post a text note (kind 1). Reads from stdin if piped. |
| `nostr reply <eventId> [message]` | Reply to an event with NIP-10 threading |
| `nostr dm [profile] [message]` | Send an encrypted DM, start interactive chat, or stream DMs with `--watch` |
| `nostr events --kinds <n>` | Query events from relays with filters (kinds, time range, author) |
| `nostr event new --kind <n> --content <text>` | Create and publish a raw event of any kind |
| `nostr follow <profile>` | Follow a user |
| `nostr unfollow <profile>` | Unfollow a user |
| `nostr following` | List accounts you follow |
| `nostr [profile]` | View a user's profile and latest notes |
| `nostr [profile] --watch` | Live-stream a user's new notes |
| `nostr --watch` | Live-stream notes from all followed accounts |

### Profile

| Command | Description |
|---------|-------------|
| `nostr login` | Create a new profile or import an existing nsec |
| `nostr switch [profile]` | Switch between profiles (interactive picker without args) |
| `nostr profile` | Show your current profile |
| `nostr profile [profile]` | Show another user's profile |
| `nostr profile update` | Interactively update your profile fields |
| `nostr profiles` | List all local profiles |

### Infrastructure

| Command | Description |
|---------|-------------|
| `nostr relays` | List relays with live connectivity status |
| `nostr relays add wss://...` | Add a relay |
| `nostr relays rm [url\|number]` | Remove a relay |
| `nostr sync` | Sync local events with relays (interactive) |
| `nostr alias [name] [npub\|nip05]` | Create an alias for a user |
| `nostr aliases` | List all aliases |

### Reference

| Command | Description |
|---------|-------------|
| `nostr nip [number]` | View a NIP specification in the terminal |
| `nostr version` | Print version info |
| `nostr update` | Check for updates and self-update |

> **Tip:** Append `--help` to any command for detailed usage — e.g. `nostr events --help`

### Global Flags

| Flag | Description |
|------|-------------|
| `--profile <npub\|alias\|username>` | Use a specific profile instead of the active one |
| `--timeout <ms>` | Timeout per relay in milliseconds (default: 2000) |
| `--no-color` | Disable colored output |
| `--json` | Output enriched JSON (pretty-printed with colors on TTY) |
| `--jsonl` | Output one JSON object per line (for bots/piping) |
| `--raw` | Output raw Nostr event JSON (wire format as relays see it) |

### Output Formats: `--raw` vs `--json` vs `--jsonl`

- **`--raw`** — The standard Nostr event object, exactly as relays see it. Useful for piping into other nostr tools.
- **`--json`** — Enriched object with the event plus metadata (relay publish status, timing, resolved names). Pretty-printed with syntax highlighting on TTY.
- **`--jsonl`** — Same enriched data as `--json` but compact, one object per line. Ideal for streaming, bots, and `jq` pipelines.

### Auto-Detection Behavior

Colors and interactive features are automatically adjusted based on the environment:

- **stdout is a TTY** → Colors enabled, interactive prompts shown
- **stdout is piped** → Colors disabled automatically (equivalent to `--no-color`)
- **stdin is piped** → Content is read as input (e.g. `echo "Hello" | nostr post`)

The `NO_COLOR` environment variable is also respected per [no-color.org](https://no-color.org).

## Examples

### Post a public note

```bash
$ nostr post "Hello Nostr!"
Posting as xavier to 5 relays
  ✓ relay.damus.io     142ms
  ✓ nos.lol             89ms
  ...
✓ Published to 4/5 relays
```

### Pipe content to Nostr

```bash
$ echo "Hello from the command line" | nostr
✓ Published to 5/5 relays
```

### Reply to an event

```bash
$ nostr reply note1abc... "Great post!"
Replying as xavier to 5 relays
  ✓ relay.damus.io     142ms
  ...
✓ Published to 4/5 relays

$ nostr reply note1abc... "Tagged reply" --tag t=nostr
$ nostr reply nevent1... "Check this" --tags '[["p","<hex>"]]'
```

### Send an encrypted DM

```bash
$ nostr dm xavier "See you at the meetup"
✓ DM sent to xavier

$ echo "Here's that link" | nostr dm xavier
✓ DM sent to xavier
```

### Query events

```bash
# Recent text notes
$ nostr events --kinds 1 --since 1h

# Decrypt DMs from the last 24 hours as JSONL
$ nostr events --kinds 4 --since 24h --decrypt --jsonl

# Notes and reactions from a specific author
$ nostr events --kinds 1,7 --author alice --limit 50 --json
```

### Create raw events

```bash
# Publish a text note
$ nostr event new --kind 1 --content "Hello world"

# Create a reaction
$ nostr event new --kind 7 --content "+" --tag e=<eventid> --tag p=<pubkey>

# Dry run: sign but don't publish
$ nostr event new --kind 1 --content "Test" --dry-run --json

# Read content from stdin
$ echo "Hello" | nostr event new --kind 1 --content -
```

### Interactive shell

Run `nostr` with no arguments:

```
$ nostr
23/03 10:15  fiatjaf     working on a new relay implementation
23/03 10:18  jb55        zaps are underrated for micropayments

xavier> this is amazing!
✓ Published!
```

### Interactive DM conversation

```
$ nostr dm xavier
23/03 14:01  xavier   Hey, are you coming to the meetup?
23/03 14:05  me       Yes! See you there

me> Can't wait 🎉
✓ Sent!
```

## Bot / Agent Integration

nostr-cli is designed to be used by bots and AI agents. Output is machine-parseable, colors are auto-disabled when piped, and streaming commands work great with `jq` pipelines.

### Output parsing

```bash
# Post and capture the event ID
EVENT_ID=$(nostr post "Hello" --jsonl | jq -r '.id')

# Get relay results
nostr post "Hello" --json | jq '.relays[] | select(.ok) | .url'

# Profile as JSON
nostr profile alice --json | jq '.about'
```

### Streaming events

```bash
# Stream all incoming DMs as JSONL
nostr dm --watch --jsonl | while read -r line; do
  echo "$line" | jq .message
done

# Stream DMs with a specific user
nostr dm alice --watch --jsonl

# Query and decrypt recent DMs
nostr events --kinds 4 --since 1h --decrypt --jsonl

# Stream notes from followed accounts
nostr --watch --jsonl
```

### Posting and DMs from scripts

```bash
# Post from stdin
echo "Automated alert: server is down" | nostr post

# Send a DM from a pipe
echo "Alert: disk usage at 90%" | nostr dm ops-team

# Create a raw event non-interactively
nostr event new --kind 1 --content "Hello" --dry-run --json
```

### Non-interactive login

```bash
nostr login --new                    # Generate new keypair
nostr login --nsec nsec1...          # Import existing key
```

### AI agent skill installation

```bash
# Claude Code
/install-skill https://nostrcli.sh/skill

# Codex, Cursor, Windsurf, Aider (via OpenSkills)
npx openskills install https://github.com/xdamman/nostr-cli
```

An [`AGENTS.md`](AGENTS.md) is included at the repo root for tools that auto-load it (Codex, Cursor, Augment, Gemini).

See also: [nostrcli.sh/skill/SKILL.md](https://nostrcli.sh/skill/SKILL.md) · [nostrcli.sh/llms.txt](https://nostrcli.sh/llms.txt)

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
        ├── aliases.json      # Contact aliases
        ├── events.jsonl      # Sent events (for backup)
        ├── directmessages/   # Encrypted DM conversations (for backup)
        │   └── <hex>.jsonl   # All messages with one counterparty
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

Releases are automated via [GoReleaser](https://goreleaser.com/) and GitHub Actions. Pushing a version tag triggers a build.

### Supported platforms

| OS | Architecture | Notes |
|----|-------------|-------|
| macOS | Intel (amd64) | Intel Macs |
| macOS | Apple Silicon (arm64) | M1/M2/M3/M4 Macs |
| Linux | x86_64 (amd64) | Standard PCs, servers |
| Linux | ARM64 (arm64) | Raspberry Pi, Asahi Linux |
| Windows | x86_64 (amd64) | Standard PCs |
| Windows | ARM64 (arm64) | Snapdragon PCs |

### How to release

```bash
git checkout main && git pull
go test ./...
git tag v0.2.0
git push origin v0.2.0
```

## Contributing

Contributions welcome! Check [docs/ROADMAP.md](docs/ROADMAP.md) for planned features.

1. Fork the repo
2. Create a feature branch
3. Make sure tests pass: `go test ./...`
4. Submit a PR

## License

[MIT](LICENSE)
