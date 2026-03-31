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
- **Bot/agent-friendly** — `--json`, `--jsonl`, `--raw` output formats for automation

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

## Quick Start

```bash
nostr login                              # Create or import an account
nostr post "Hello Nostr!"               # Post a public note
nostr dm xavier "See you at the meetup"  # Send an encrypted DM
nostr                                    # Launch the interactive shell
```

---

## Non-Interactive Mode (CLI / Scripting / Bots)

Commands designed for scripting, piping, and bot integration. All support `--json`, `--jsonl`, `--raw` output. Colors are auto-disabled when stdout is piped.

### Post

```bash
nostr post "Hello Nostr!"                                  # Post a note
echo "Hello from a script" | nostr post                    # Post from stdin
nostr post -f article.md --title "My Article"              # Long-form article (NIP-23)
nostr post -f article.md --slug my-article --draft         # Publish as draft
nostr post "Tagged" --tag t=nostr --tag t=bitcoin          # Post with tags
nostr post "Test" --dry-run --json                         # Sign but don't publish
EVENT_ID=$(nostr post "Hello" --jsonl | jq -r '.id')       # Capture event ID
```

### Reply

```bash
nostr reply note1abc... "Great post!"                      # Reply with NIP-10 threading
nostr reply note1abc... "Tagged" --tag t=nostr             # Reply with extra tags
echo "Nice work" | nostr reply note1abc...                 # Reply from stdin
```

### DM

```bash
nostr dm alice "Hey!"                                      # Send NIP-17 gift-wrapped DM
nostr dm alice "Hello" --nip04                             # Force legacy NIP-04
echo "Alert: disk full" | nostr dm ops-team                # DM from stdin
nostr dm --watch --jsonl                                   # Stream ALL incoming DMs
nostr dm alice --watch --jsonl                             # Stream DMs with alice
nostr dm --watch --since 1h --jsonl                        # Catch up + stream
```

### Follow

```bash
nostr follow alice --json                                  # Follow with JSON output
nostr follow alice --alias al                              # Follow with alias
nostr unfollow alice                                       # Unfollow
nostr following --json                                     # List followed users
```

### Profile

```bash
nostr profile alice --json                                 # View profile as JSON
nostr profile alice -n 10 --jsonl                          # Last 10 events as JSONL
nostr profile alice -n 5 --kinds 1,7 --jsonl               # Filter by event kind
nostr profile alice --watch --jsonl                        # Live-stream events
nostr profile npub1... --refresh --json                    # Force refresh from relays
```

### Events

```bash
nostr events --kinds 1 --since 1h                          # Recent text notes
nostr events --kinds 4 --since 24h --decrypt --jsonl       # Decrypt DMs as JSONL
nostr events --kinds 1,7 --author alice --limit 50 --json  # Notes + reactions by author
nostr events --watch --kinds 4 --me --decrypt --jsonl      # Stream DMs addressed to you
nostr events --watch --kinds 1 --filter "t=bitcoin" --jsonl # Stream tagged notes
```

### Event New

```bash
nostr event new --kind 1 --content "Hello world"           # Create raw event
nostr event new --kind 7 --content "+" --tag e=<id> --tag p=<pubkey>  # Reaction
echo "Hello" | nostr event new --kind 1 --content -        # Content from stdin
nostr event new --kind 1 --content "Test" --dry-run --json # Dry run
```

### Accounts

```bash
nostr login --nsec nsec1...                                # Import existing key
nostr login --new                                          # Generate new keypair
nostr accounts --json                                      # List all accounts
nostr switch alice                                         # Switch active account
```

### Relays

```bash
nostr relays --json                                        # List relays with status
nostr relays add wss://relay.example.com                   # Add a relay
nostr relays rm nos.lol -y                                 # Remove a relay
```

### Generate

```bash
nostr generate nip05 --address user@domain.com             # Generate NIP-05 nostr.json
nostr generate nip05 --address user@domain.com --json      # Output JSON to stdout
```

### Version & Update

```bash
nostr version --json                                       # Version info as JSON
nostr update -y                                            # Auto-update
```

### User Lookup

```bash
nostr alice --json --limit 10                              # View user's recent notes
nostr alice --watch --jsonl                                # Stream notes
nostr --watch --jsonl                                      # Stream followed accounts' notes
```

### Output Formats

| Flag | Description | Use case |
|------|-------------|----------|
| `--json` | Enriched JSON (event + metadata, pretty-printed on TTY) | Inspection, debugging |
| `--jsonl` | One JSON object per line | Streaming, bots, `jq` pipelines |
| `--raw` | Raw Nostr event JSON (wire format) | Piping to other nostr tools |

### Auto-Detection Behavior

- **stdout is a TTY** → Colors enabled, interactive prompts shown
- **stdout is piped** → Colors disabled automatically
- **stdin is piped** → Content read as input (e.g. `echo "Hello" | nostr post`)
- `NO_COLOR` environment variable is respected per [no-color.org](https://no-color.org)

### Global Flags

| Flag | Description |
|------|-------------|
| `--account <npub\|alias\|username>` | Use a specific account instead of the active one |
| `--timeout <ms>` | Timeout per relay in milliseconds (default: 2000) |
| `--no-color` | Disable colored output |
| `--json` | Enriched JSON output |
| `--jsonl` | One JSON object per line |
| `--raw` | Raw Nostr event JSON |

---

## Interactive Mode (Terminal UI)

For humans using the terminal. Rich TUI built with bubbletea.

### Interactive Shell

Run `nostr` with no arguments to launch the interactive shell:

```
$ nostr
23/03 10:15  fiatjaf     working on a new relay implementation
23/03 10:18  jb55        zaps are underrated for micropayments

xavier> this is amazing!
✓ Published!
```

- Shows your feed from followed users
- Type to post a note (Shift+Enter for multiline)
- Slash commands: `/follow`, `/unfollow`, `/dm`, `/profile`, `/switch`, `/alias`, `/aliases`
- Tab/arrow-key autocomplete for slash commands and `@` mentions
- `nostr:npub1...` references rendered as `@username` in terminal

### Interactive DM

```
$ nostr dm xavier
23/03 14:01  xavier   Hey, are you coming to the meetup?
23/03 14:05  me       Yes! See you there
                                          xavier is typing...
me> Can't wait 🎉
✓ Sent!
```

- Full-screen chat with typing indicators (ephemeral kind 10003, gift-wrapped in NIP-17)
- Shift+Enter for multiline messages
- DM protocol auto-detected per conversation

### Interactive Profile Update

```bash
nostr profile update    # Interactive form to update your profile fields
```

### Interactive Features

- `@` mention autocomplete in shell and DMs
- `/switch` with arrow-key account picker
- `/dm` with user picker and compose flow
- Profile form with `nostr profile update`
- Long-form editor with `nostr post --long`
- Visual line-wrapping textarea input
- Interactive relay sync with `nostr sync`

---

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

## NIP Support

| NIP | Description | Status |
|-----|-------------|--------|
| NIP-01 | Basic protocol (events, relays) | ✓ |
| NIP-02 | Contact list / follow list (kind 3) | ✓ |
| NIP-04 | Encrypted DMs (legacy) | ✓ (receive + `--nip04` flag) |
| NIP-05 | DNS-based identifiers | ✓ (resolution + generation) |
| NIP-10 | Reply threading (e/p tags) | ✓ |
| NIP-17 | Gift-wrapped DMs | ✓ (default for sending) |
| NIP-19 | Bech32 encoding (npub, nsec, note, nevent) | ✓ |
| NIP-23 | Long-form content (kind 30023/30024) | ✓ |
| NIP-24 | Bot flag in profile metadata | ✓ |
| NIP-44 | Versioned encryption | ✓ (used by NIP-17) |

## Agent / Bot Recipes

nostr-cli is designed for bots and AI agents. All output is machine-parseable, colors auto-disabled when piped, and streaming commands work great with `jq`.

### Stream DMs and respond

```bash
nostr dm --watch --jsonl | while read -r line; do
  message=$(echo "$line" | jq -r .message)
  sender=$(echo "$line" | jq -r .from_npub)
  nostr dm "$sender" "Got: $message"
done
```

### Bot inbox (events addressed to you)

```bash
nostr events --watch --kinds 4 --me --decrypt --jsonl | while read -r line; do
  echo "$line" | jq '{from: .author, msg: .content, protocol: .protocol}'
done
```

### Post and capture event ID

```bash
EVENT_ID=$(nostr post "Hello" --jsonl | jq -r '.id')
```

### Non-interactive setup

```bash
nostr login --new
nostr relays add wss://relay.damus.io
nostr relays add wss://nos.lol
nostr post "Bot is online" --jsonl
```

### Use a specific account for bot operations

```bash
nostr post "Bot message" --account mybot --jsonl
nostr dm alice "Alert" --account mybot
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

## Testing

```bash
go test ./...          # All tests (includes integration tests hitting real relays)
go test -short ./...   # Unit tests only
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
