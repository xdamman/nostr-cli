# nostr

> A human and bot-friendly command-line interface for the [Nostr](https://nostr.com) protocol.

<!-- badges -->
[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.21+-00ADD8?logo=go)](https://go.dev)

## Why?

Interacting with Nostr shouldn't require a GUI. `nostr` gives you a fast, scriptable CLI that works like `git` — manage multiple accounts, switch contexts, and publish events from your terminal.

- **Multi-account support** — switch between identities like git branches
- **Human-friendly** — use aliases and usernames, not just npubs
- **Unix-native** — pipes, scripts, cron jobs — it just works
- **Auto-detection** — colors off when piped, TTY detection for interactive features

## Features

- 🔑 Login with existing nsec or generate a new keypair
- 👤 View and update profiles (kind 0)
- 📝 Post text notes (kind 1) and long-form articles (NIP-23, kind 30023/30024)
- 💬 Encrypted DMs (NIP-17 gift wrap / NIP-44 / NIP-04 legacy) with interactive chat UI
- 🔎 Query events from relays with flexible filters
- 🛠️ Create raw events of any kind
- 🔄 Follow/unfollow users with `--alias` and `--json` output
- 🌐 Manage relay lists per account
- 🏷️ Create aliases for quick access to contacts
- 👥 Switch between multiple accounts
- 📖 Built-in NIP reference viewer
- 🔄 Sync local events with relays
- 🐚 Interactive shell with feed, posting, and slash commands
- ✏️ Multiline input with Shift+Enter in shell and DM modes
- ⌨️ Typing indicators in interactive DM mode (ephemeral kind 10003)
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
nostr login                              # Create or import an account
nostr post "Hello Nostr!"               # Post a public note
nostr dm xavier "See you at the meetup"  # Send an encrypted DM
nostr                                    # Launch the interactive shell
```

## Commands

### Social

| Command | Description |
|---------|-------------|
| `nostr post [message]` | Post a text note (kind 1). Reads from stdin if piped. |
| `nostr post -f article.md` | Publish long-form content (NIP-23, kind 30023). |
| `nostr post --long --title "Title"` | Write long-form content in the built-in editor. |
| `nostr reply <eventId> [message]` | Reply to an event with NIP-10 threading |
| `nostr dm [profile] [message]` | Send an encrypted DM, start interactive chat, or stream DMs with `--watch` |
| `nostr events --kinds <n>` | Query events from relays with filters (kinds, time range, author, tags) |
| `nostr event new --kind <n> --content <text>` | Create and publish a raw event of any kind |
| `nostr follow <profile>` | Follow a user (with `--alias` and `--json` support) |
| `nostr unfollow <profile>` | Unfollow a user |
| `nostr following` | List accounts you follow |
| `nostr profile <user> -n 10` | View a user's profile and past events |
| `nostr [profile]` | View a user's profile and latest notes |
| `nostr [profile] --watch` | Live-stream a user's new notes |
| `nostr --watch` | Live-stream notes from all followed accounts |

### Account & Profile

| Command | Description |
|---------|-------------|
| `nostr login` | Create a new account or import an existing nsec |
| `nostr switch [account]` | Switch between accounts (interactive picker without args) |
| `nostr profile` | Show your current Nostr profile |
| `nostr profile [user]` | Show another user's Nostr profile |
| `nostr profile [user] -n 10` | View a user's past events |
| `nostr profile [user] --kinds 1,7` | Filter events by kind |
| `nostr profile [user] --watch` | Live-stream new events |
| `nostr profile update` | Interactively update your Nostr profile fields |
| `nostr accounts` | List all local accounts |

### Infrastructure

| Command | Description |
|---------|-------------|
| `nostr relays` | List relays with live connectivity status |
| `nostr relays add wss://...` | Add a relay |
| `nostr relays rm [url\|number]` | Remove a relay |
| `nostr sync` | Sync local events with relays (interactive) |
| `nostr alias [name] [npub\|nip05]` | Create an alias for a user |
| `nostr aliases` | List all aliases |
| `nostr generate nip05` | Generate a NIP-05 nostr.json file |

### Reference

| Command | Description |
|---------|-------------|
| `nostr nip [number]` | View a NIP specification in the terminal |
| `nostr version` | Print version info (supports `--json`) |
| `nostr update` | Check for updates and self-update (supports `--json`) |

> **Tip:** Append `--help` to any command for detailed usage — e.g. `nostr events --help`

### Global Flags

| Flag | Description |
|------|-------------|
| `--account <npub\|alias\|username>` | Use a specific account instead of the active one |
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

### Publish long-form content (NIP-23)

```bash
# Publish a markdown file as an article
$ nostr post -f article.md --title "My Article" --slug my-article

# Write in the built-in editor
$ nostr post --long --title "Quick Thoughts"

# Publish as draft (kind 30024)
$ nostr post -f article.md --draft

# Update an existing article (same slug replaces previous)
$ nostr post -f updated.md --slug my-article

# Full metadata
$ nostr post -f article.md --title "My Article" --summary "Great read" \
  --image https://example.com/header.jpg --hashtag nostr --hashtag bitcoin
```

Files with YAML frontmatter (`---`) auto-extract title, summary, image, slug, and hashtags. CLI flags override frontmatter values.

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

### View profile and past events

```bash
# Show profile + last 5 events
$ nostr profile fiatjaf -n 5
Name:    fiatjaf
NIP-05:  fiatjaf@nostr.com ✓
About:   creating nostr and other things

  30/03 10:15  working on a new relay implementation
  30/03 09:42  just shipped NIP-XX support
  29/03 22:10  the protocol is the product
  29/03 18:30  testing gift-wrapped DMs
  29/03 15:05  nostr is inevitable

# Filter by event kind
$ nostr profile fiatjaf -n 10 --kinds 1,7 --jsonl

# Live-stream new events
$ nostr profile fiatjaf --watch
```

### Interactive DM conversation

Interactive DM mode features typing indicators — you'll see "\<name\> is typing..." in the status bar. Typing indicators use ephemeral kind 10003 events, gift-wrapped in NIP-17 mode for metadata privacy.

```
$ nostr dm xavier
23/03 14:01  xavier   Hey, are you coming to the meetup?
23/03 14:05  me       Yes! See you there
                                          xavier is typing...
me> Can't wait 🎉
✓ Sent!
```

Use **Shift+Enter** for multiline messages in both shell and DM interactive modes.

## Bot / Agent Integration

nostr-cli is designed to be used by bots and AI agents. Output is machine-parseable, colors are auto-disabled when piped, and streaming commands work great with `jq` pipelines.

All agent profiles include NIP-05 identities (e.g. `agent@xavierdamman.com`) and set `bot: true` per NIP-24.

### Output parsing

```bash
# Post and capture the event ID
EVENT_ID=$(nostr post "Hello" --jsonl | jq -r '.id')

# Get relay results
nostr post "Hello" --json | jq '.relays[] | select(.ok) | .url'

# Profile as JSON
nostr profile alice --json | jq '.about'

# View a user's recent events as JSONL
nostr profile alice -n 10 --jsonl

# Follow with alias and JSON output
nostr follow alice --alias al --json
```

### Streaming events

```bash
# Stream all incoming DMs as JSONL
nostr dm --watch --jsonl | while read -r line; do
  echo "$line" | jq .message
done

# Stream DMs with a specific user
nostr dm alice --watch --jsonl

# Catch up on missed DMs (last hour) and continue streaming
nostr dm --watch --since 1h --jsonl

# Query and decrypt recent DMs (one-shot)
nostr events --kinds 4 --since 1h --decrypt --jsonl

# Live-stream decrypted DMs
nostr events --watch --kinds 4 --decrypt --jsonl

# Stream only DMs addressed to you, decrypted
nostr events --watch --kinds 4 --me --decrypt --jsonl

# View a user's events for bot processing
nostr profile alice -n 20 --kinds 1 --jsonl | while read -r line; do
  echo "$line" | jq .content
done

# Stream notes with tag-based filtering
nostr events --watch --kinds 1 --filter "t=bitcoin" --jsonl

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
- Type to post a note (Shift+Enter for multiline)
- Slash commands: `/follow`, `/unfollow`, `/dm`, `/profile`, `/switch`, `/alias`, `/aliases`
- Tab/arrow-key autocomplete for slash commands
- Visual line-wrapping textarea input

## Configuration

All state lives in `~/.nostr/`:

```
~/.nostr/
└── accounts/
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

Each account is isolated — relays, aliases, and keys are scoped per identity.

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
