---
name: nostr-cli
description: Post notes, send encrypted DMs (NIP-17/NIP-44), query events, create raw events, publish long-form articles (NIP-23), manage accounts, follow users, and interact with Nostr relays from the terminal.
---

# nostr-cli

A command-line tool for interacting with the Nostr protocol. Post notes, send encrypted DMs (NIP-17 gift wrap / NIP-44 / NIP-04 legacy), query events with flexible filters, create raw events of any kind, publish long-form articles (NIP-23), manage accounts, profiles, and aliases, follow users, and manage relays — all from the terminal or within scripts.

## Installation Check and Setup

### Check Installation
```bash
nostr version
```

### Install if Missing
```bash
curl -sL https://nostrcli.sh/install.sh | bash
```

## Auto-Detection Behavior

nostr-cli auto-detects the environment:
- **stdout is a TTY** → Colors enabled, interactive prompts shown
- **stdout is piped** → Colors disabled automatically, no interactive prompts
- **stdin is piped** → Content read as input (e.g. `echo "Hello" | nostr post`)
- The `NO_COLOR` env var and `--no-color` flag also disable colors explicitly

## Output Formats

| Flag | Description | Use case |
|------|-------------|----------|
| `--json` | Pretty-printed enriched JSON (event + metadata) | Human inspection, debugging |
| `--jsonl` | Compact single-line JSON per event | Piping, bots, streaming, `jq` |
| `--raw` | Raw Nostr event JSON (wire format) | Forwarding to other nostr tools |

## Command Reference

### Posting Notes
```bash
nostr post [message]
```

Publishes a kind 1 text note. Message can come from argument, stdin, or interactive prompt.

Flags:
- `--tag key=value` — Add extra tags (repeatable)
- `--tags '<json>'` — Add extra tags as JSON array
- `--dry-run` — Sign but don't publish
- `--json` / `--jsonl` / `--raw` — Machine-readable output

Examples:
```bash
nostr post "Hello Nostr"
echo "My message" | nostr post
nostr post "Tagged" --tag t=nostr --tag t=bitcoin
EVENT_ID=$(nostr post "Message" --jsonl | jq -r '.id')
```

### Long-Form Content (NIP-23)
```bash
nostr post -f <file> [flags]
nostr post --long [flags]
```

Publish long-form articles (kind 30023) or drafts (kind 30024).

Flags:
- `-f, --file <path>` — Read content from a markdown file
- `--long` — Open built-in multi-line editor
- `--title <string>` — Article title
- `--summary <string>` — Article summary
- `--image <url>` — Header image URL
- `--slug <string>` — Article identifier / d tag (for updates)
- `--draft` — Publish as draft (kind 30024 instead of 30023)
- `--hashtag <string>` — Hashtag topics (repeatable, t tags)

YAML frontmatter in markdown files is auto-parsed. CLI flags override frontmatter.

Examples:
```bash
nostr post -f article.md --title "My Article"
nostr post -f article.md --slug my-article --title "My Article" --summary "Great read"
nostr post --long --title "Quick Thoughts"
nostr post -f article.md --draft
nostr post -f updated.md --slug my-article    # Updates existing article
nostr post -f article.md --hashtag nostr --hashtag bitcoin
```

### Replying to Events
```bash
nostr reply <eventId> [message]
```

Reply with NIP-10 compliant threading. Event ID can be hex, note1..., or nevent1....

Flags:
- `--tag key=value` — Add extra tags (repeatable)
- `--tags '<json>'` — Add extra tags as JSON array
- `--dry-run` — Sign but don't publish
- `--json` / `--jsonl` / `--raw` — Machine-readable output

### Direct Messages
```bash
nostr dm <user> [message]
```

NIP-17 gift-wrapped DMs by default (NIP-44 encryption). Both NIP-04 and NIP-17 received automatically.

Flags:
- `--nip04` — Force NIP-04 encryption (legacy)
- `--watch` — Stream incoming DMs
- `--since <time>` — Start time for --watch
- `--no-decrypt` — Don't decrypt messages
- `--tag key=value` / `--tags '<json>'` — Extra tags
- `--json` / `--jsonl` / `--raw` — Machine-readable output

JSON output includes `protocol` field (`"nip04"` or `"nip17"`).

Examples:
```bash
nostr dm alice "Hello"                        # NIP-17 by default
nostr dm alice "Hello" --nip04                # Legacy NIP-04
nostr dm --watch --jsonl                      # Stream all DMs
nostr dm --watch --since 1h --jsonl           # Catch up and stream
```

### Query Events
```bash
nostr events --kinds <kinds> [flags]
```

Flags:
- `--kinds <n,n,...>` — Event kinds, comma-separated
- `--since <time>` / `--until <time>` — Time range
- `--author <user>` — Filter by author
- `--limit <n>` — Max events (default: 50)
- `--decrypt` — Decrypt kind 4 DMs
- `--watch` — Live-stream events
- `--filter key=value` — Tag filter (repeatable)
- `--me` — Shortcut for `--filter "p=<your_pubkey>"`
- `--json` / `--jsonl` / `--raw` — Machine-readable output

Examples:
```bash
nostr events --kinds 1 --since 1h
nostr events --kinds 4 --since 24h --decrypt --jsonl
nostr events --watch --kinds 4 --me --decrypt --jsonl
nostr events --watch --kinds 1 --filter "t=bitcoin" --jsonl
```

### Create Raw Events
```bash
nostr event new --kind <n> --content <text> [flags]
```

Flags:
- `--kind <n>` — Event kind (required)
- `--content <text>` — Content (required, `-` for stdin)
- `--tag key=value` / `--tags '<json>'` — Tags
- `--pow <n>` — Proof of work difficulty
- `--dry-run` — Sign but don't publish
- `--json` / `--jsonl` / `--raw` — Machine-readable output

### Accounts & Profiles
```bash
nostr profile [user]               # View profile
nostr profile update               # Edit profile
nostr accounts                     # List accounts
nostr login --new                  # Generate keypair
nostr login --nsec nsec1...        # Import key
nostr switch [account]             # Switch account
```

### Social
```bash
nostr follow <user>                # Follow
nostr unfollow <user>              # Unfollow
nostr following                    # List following
nostr <user> --watch               # Stream notes
```

### Relays & Sync
```bash
nostr relays                       # List relays
nostr relays add wss://...         # Add relay
nostr relays rm <id|url> -y        # Remove relay
nostr sync --json                  # Sync events
```

### Aliases
```bash
nostr alias <name> <user>          # Create alias
nostr aliases                      # List aliases
nostr alias rm <name>              # Remove alias
```

## Global Flags

| Flag | Description |
|------|-------------|
| `--account <npub\|alias\|username>` | Execute command under a specific account |
| `--timeout <ms>` | Relay timeout in milliseconds (default: 2000) |
| `--no-color` | Disable colored output |
| `--json` | Enriched JSON output |
| `--jsonl` | One JSON per line (bots/piping) |
| `--raw` | Raw Nostr event JSON |

## User Resolution

- **npub**: `npub1...`
- **Alias**: local alias
- **NIP-05**: `user@domain.com`

## Bot/Agent Patterns

### Monitor and respond to DMs
```bash
nostr dm --watch --jsonl | while read -r line; do
  message=$(echo "$line" | jq -r .message)
  sender=$(echo "$line" | jq -r .from_npub)
  nostr dm "$sender" "Got: $message"
done
```

### Stream events addressed to you
```bash
nostr events --watch --kinds 4 --me --decrypt --jsonl
```

### Post with event ID capture
```bash
EVENT_ID=$(nostr post "Hello" --jsonl | jq -r '.id')
```

### Non-interactive setup
```bash
nostr login --new
nostr relays add wss://relay.damus.io
nostr post "Bot is online" --jsonl
```
