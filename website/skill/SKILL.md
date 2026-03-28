---
name: nostr-cli
description: Post notes, send encrypted DMs, query events, create raw events, manage profiles, follow users, and interact with Nostr relays from the terminal.
---

# nostr-cli

A command-line tool for interacting with the Nostr protocol. Post notes, send encrypted DMs, query events with flexible filters, create raw events of any kind, manage profiles and aliases, follow users, and manage relays — all from the terminal or within scripts.

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

Most commands support three machine-readable output formats:

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
- `--reply <event-id>` — Reply to a specific event (hex, note1, or nevent)
- `--json` / `--jsonl` / `--raw` — Machine-readable output

Examples:
```bash
nostr post "Hello Nostr"
echo "My message" | nostr post
nostr post "Reply" --reply note1abc... --jsonl
EVENT_ID=$(nostr post "Message" --jsonl | jq -r '.id')
```

### Direct Messages
```bash
nostr dm <user> [message]
```

Send NIP-04 encrypted direct messages. `<user>` can be an npub, alias, or NIP-05 address.

Modes:
- `nostr dm <user> <message>` — Send one-shot DM
- `echo "msg" | nostr dm <user>` — Send from stdin
- `nostr dm <user>` — Interactive chat (TUI)
- `nostr dm <user> --watch` — Stream messages with this user
- `nostr dm --watch` — Stream ALL incoming DMs
- `nostr dm` — Show aliases

Flags:
- `--watch` — Stream incoming DMs (no send prompt)
- `--json` / `--jsonl` / `--raw` — Machine-readable output

Examples:
```bash
nostr dm alice "Hello"
echo "Alert" | nostr dm alice
nostr dm --watch --jsonl | while read -r line; do echo "$line" | jq .message; done
nostr dm alice --watch --jsonl
```

### Query Events
```bash
nostr events --kinds <kinds> [flags]
```

Query events from relays with flexible filters.

Flags:
- `--kinds <n,n,...>` — Event kinds, comma-separated (required). Common: 0 (profile), 1 (note), 3 (follows), 4 (DM), 7 (reaction)
- `--since <time>` — Start time: duration (1h, 7d, 30m), unix timestamp, or ISO date (2024-01-01)
- `--until <time>` — End time: same formats as --since
- `--author <user>` — Filter by author (npub, alias, or NIP-05)
- `--limit <n>` — Maximum events to return (default: 50)
- `--decrypt` — Decrypt kind 4 DM content (requires private key)
- `--json` / `--jsonl` / `--raw` — Machine-readable output

Examples:
```bash
nostr events --kinds 1 --since 1h
nostr events --kinds 4 --since 24h --decrypt --jsonl
nostr events --kinds 1,7 --author alice --limit 50 --json
nostr events --kinds 0,1,3 --since 2024-01-01 --jsonl
```

### Create Raw Events
```bash
nostr event new --kind <n> --content <text> [flags]
```

Create, sign, and publish a Nostr event of any kind.

Flags:
- `--kind <n>` — Event kind number (required)
- `--content <text>` — Event content (required, use `-` for stdin)
- `--tag key=value` — Tags in key=value format (repeatable)
- `--pow <n>` — Proof of work difficulty (leading zero bits)
- `--dry-run` — Sign but don't publish (outputs signed event)
- `--json` / `--jsonl` / `--raw` — Machine-readable output

Examples:
```bash
nostr event new --kind 1 --content "Hello world"
nostr event new --kind 7 --content "+" --tag e=<eventid> --tag p=<pubkey>
nostr event new --kind 0 --content '{"name":"bot","about":"I am a bot"}'
echo "Hello" | nostr event new --kind 1 --content -
nostr event new --kind 1 --content "Test" --dry-run --json
```

### User Profiles
```bash
nostr profile [user]
```

View profile metadata. Without arguments, shows your own profile.

Flags:
- `--refresh` — Force fetch from relays instead of cache
- `--json` / `--jsonl` / `--raw` — Structured output

Examples:
```bash
nostr profile
nostr profile alice --json
nostr profile npub1... --refresh --json
```

### Profile Update
```bash
nostr profile update
```

Interactively update your profile fields (name, display name, about, picture, NIP-05, website). Changes are published to relays.

### Follow Management
```bash
nostr follow <user>        # Follow a user
nostr unfollow <user>      # Unfollow a user
nostr following            # List users you follow
```

Flags for `following`:
- `--refresh` — Force refresh from relays
- `--json` / `--jsonl` — Structured output

Examples:
```bash
nostr follow alice
nostr unfollow npub1...
nostr following --json
```

### Relay Management
```bash
nostr relays               # List configured relays with live status
nostr relays add <url>     # Add a relay (wss://... format)
nostr relays rm <id|url>   # Remove a relay
```

Flags:
- `--json` — JSON output with connection status and ping
- `--relay <url|domain>` — Show a specific relay only
- `--yes` / `-y` — Skip confirmation on `rm`

Examples:
```bash
nostr relays --json
nostr relays add wss://relay.example.com
nostr relays rm nos.lol -y
nostr relays rm 1
```

### Aliases
```bash
nostr alias <name> <user>    # Create an alias
nostr aliases                 # List all aliases
nostr alias rm <name>         # Remove an alias
```

Examples:
```bash
nostr alias alice npub1...
nostr alias bob alice@example.com
nostr aliases
```

### Sync Events
```bash
nostr sync                 # Interactive relay selection and sync
nostr sync --relay <url>   # Sync with a specific relay
nostr sync --json          # Machine-readable sync output
```

### Profile Management
```bash
nostr login                # Interactive login (import or generate)
nostr login --new          # Generate new keypair non-interactively
nostr login --nsec nsec1...  # Import existing key non-interactively
nostr switch [profile]     # Switch active profile
nostr profiles             # List all local profiles
```

### User Lookup
```bash
nostr <user>               # View profile and latest notes
nostr <user> --watch       # Stream new notes from a user
nostr --watch              # Stream notes from all followed accounts
```

Flags:
- `--watch` — Live-stream new notes (Ctrl+C to exit)
- `--limit <n>` — Number of notes to show (default: 10)
- `--json` / `--jsonl` / `--raw` — Machine-readable output

### NIP Reference
```bash
nostr nip <number>         # View a NIP specification
```

Examples:
```bash
nostr nip 01
nostr nip 44
```

### Other
```bash
nostr version              # Print version info
nostr update               # Check for updates and self-update
```

## Global Flags

| Flag | Description |
|------|-------------|
| `--profile <npub\|alias\|username>` | Execute command under a specific profile |
| `--timeout <ms>` | Relay timeout in milliseconds (default: 2000) |
| `--no-color` | Disable colored output (auto-detected when piped) |
| `--json` | Enriched JSON output (pretty-printed on TTY) |
| `--jsonl` | One JSON object per line (for bots/piping) |
| `--raw` | Raw Nostr event JSON (wire format) |

## User Resolution

In commands accepting a `<user>` argument, you can specify:
- **npub**: `npub1...` (Nostr public key in bech32)
- **Alias**: A local alias created with `nostr alias`
- **NIP-05**: `user@domain.com` (DNS-based identifier)

## Bot/Agent Patterns and Recipes

### Monitor DMs and respond
```bash
nostr dm --watch --jsonl | while read -r line; do
  message=$(echo "$line" | jq -r .message)
  sender=$(echo "$line" | jq -r .from_npub)
  # Process message and respond
  nostr dm "$sender" "Got your message: $message"
done
```

### Automated posting
```bash
# Post from a script
echo "Server status: all systems go" | nostr post --jsonl

# Post with event ID capture
EVENT_ID=$(nostr post "Hello" --jsonl | jq -r '.id')
```

### Query and process events
```bash
# Get recent DMs as structured data
nostr events --kinds 4 --since 1h --decrypt --jsonl | jq '{from: .author, msg: .content}'

# Export notes from a user
nostr events --kinds 1 --author alice --since 7d --jsonl > alice_notes.jsonl
```

### Non-interactive setup
```bash
nostr login --new
nostr relays add wss://relay.damus.io
nostr relays add wss://nos.lol
nostr post "Bot is online" --jsonl
```

## Configuration

- **Config directory**: `~/.nostr/`
- **Profiles**: `~/.nostr/profiles/<npub>/`
- **Sent events**: `~/.nostr/profiles/<npub>/events.jsonl`
- **DM history**: `~/.nostr/profiles/<npub>/directmessages/<hex>.jsonl`

All profiles, aliases, and relay configurations are stored in `~/.nostr/`. Each profile is isolated with its own keys, relays, and aliases.
