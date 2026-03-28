---
name: nostr-cli
description: Post notes, send encrypted DMs, manage accounts and profiles, follow users, and interact with Nostr relays from the terminal.
---

# nostr-cli

A command-line tool for interacting with the Nostr protocol. It enables posting notes, sending encrypted DMs, managing accounts, profiles, and aliases, following other users, and managing Nostr relays — all from the terminal or within scripts.

## Installation Check and Setup

### Check Installation
```bash
nostr version
```

### Install if Missing
```bash
curl -sL https://nostrcli.sh/install.sh | bash
```

## Command Reference

### Posting Notes
```bash
nostr post [message]
```

Flags:
- `--reply <event-id>` — Reply to a specific event
- `--tag key=value` — Add extra tags (repeatable). Semicolons for multi-value.
- `--tags '<json>'` — Add extra tags as JSON array
- `--dry-run` — Sign but don't publish
- `--json` — Output result as JSON (includes event ID, signature, etc.)
- `--timeout <ms>` — Relay timeout in milliseconds (default: 2000)

Examples:
```bash
nostr post "Hello Nostr"
echo "My message" | nostr post
nostr post "Reply" --reply <event-id> --json
nostr post "Tagged" --tag t=nostr --tag t=bitcoin
nostr post "Custom" --tags '[["t","nostr"]]'
```

### Replying to Events
```bash
nostr reply <eventId> [message]
```

Reply to an existing event with NIP-10 compliant threading. Fetches the referenced event from relays to determine thread structure.

Flags:
- `--tag key=value` — Add extra tags (repeatable)
- `--tags '<json>'` — Add extra tags as JSON array
- `--dry-run` — Sign but don't publish
- `--json` / `--jsonl` / `--raw` — Machine-readable output

Examples:
```bash
nostr reply note1abc... "Great post!"
nostr reply abc123hex "I agree" --tag t=nostr
nostr reply nevent1... "Check this" --tags '[["p","<hex>"]]'
echo "Nice" | nostr reply note1abc...
```

### Direct Messages
```bash
nostr dm <user> [message]
```

Sends NIP-04 encrypted direct messages. `<user>` can be an npub, alias, or NIP-05 address.

Flags:
- `--tag key=value` — Add extra tags (repeatable)
- `--tags '<json>'` — Add extra tags as JSON array
- `--json` — Output event and relay results as JSON

Examples:
```bash
nostr dm npub1... "Hello"
nostr dm alice@example.com "Message" --json
nostr dm alice "Hello" --tag subject=greeting
echo "Content" | nostr dm alice
```

### User Profiles
```bash
nostr profile [user]
```

Flags:
- `--json` — Structured JSON output
- `--refresh` — Force fetch from relays instead of cache

Examples:
```bash
nostr profile alice
nostr profile npub1... --json
nostr profile alice@example.com --refresh --json
```

### Manage Profiles
```bash
nostr accounts              # List all local accounts
nostr accounts rm [name]    # Remove a local account
```

Flags:
- `--json` — JSON output for list command

### Follow Management
```bash
nostr follow <user>        # Follow a user
nostr unfollow <user>      # Unfollow a user
nostr following            # List users you follow
```

Examples:
```bash
nostr follow alice
nostr unfollow npub1...
nostr following --json
```

### Relay Management
```bash
nostr relays               # List configured relays
nostr relays add <url>     # Add a relay (wss://... format)
nostr relays rm <id|url|domain>  # Remove a relay (asks confirmation)
```

Flags:
- `--json` — JSON output with connection status and ping
- `--relay <url|domain>` — Show a specific relay only
- `--yes` / `-y` — Skip confirmation on `rm` (also skipped with `--json`)

Examples:
```bash
nostr relays --json
nostr relays --relay nos.lol --json
nostr relays add wss://relay.example.com
nostr relays rm nos.lol -y
nostr relays rm 1
```

### Sync Events
```bash
nostr sync                 # Interactive relay selection and sync
nostr sync --relay <url>   # Sync with a specific relay
nostr sync --json          # Machine-readable sync output
```

Flags:
- `--json` — Output sync results as JSON (syncs all relays, no interactive UI)
- `--relay <url|domain>` — Sync with a specific relay only

Examples:
```bash
nostr sync --json
nostr sync --relay nos.lol --json
nostr sync --relay wss://nos.lol
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
nostr aliases --json
```

### Account Management
```bash
nostr switch [account]    # Switch to a different profile
nostr login               # Log in to a profile
```

Flags for login:
- `--new` — Create a new key
- `--nsec <key>` — Log in with nsec (hex-encoded private key)
- `--generate` — Generate a new key

Examples:
```bash
nostr login --new
nostr login --nsec nsec1...
nostr switch alice
```

### User Feed
```bash
nostr [user]
```

Flags:
- `--watch` — Watch for new notes in real time
- `--json` — JSON output
- `--limit <n>` — Limit number of notes returned (default varies)
- `--timeout <ms>` — Relay timeout

Examples:
```bash
nostr alice
nostr npub1... --json --limit 10
nostr alice --watch
```

### NIP Specification Viewer
```bash
nostr nip<N>    # View NIP specification (e.g., nostr nip44)
```

Examples:
```bash
nostr nip44
nostr nip05
```

## Global Flags

- `--account <npub|alias|username>` — Execute command under a specific account
- `--timeout <ms>` — Relay timeout in milliseconds (default: 2000)
- `--no-color` — Strip ANSI color codes
- `--raw` — Output raw Nostr event as compact single-line JSON (wire format)
- `--json` — Enriched JSON output, pretty-printed with colors on TTY
- `--jsonl` — One JSON object per line, no formatting (for bot/pipe consumption)

### `--raw` vs `--json` vs `--jsonl`

- `--raw` returns the **standard Nostr event object** as a single compact JSON line — the exact wire format relays receive. Useful for piping into other nostr tools.
- `--json` returns an **enriched object** with the event plus metadata (relay publish status, timing). Pretty-printed with syntax colors when output is a terminal.
- `--jsonl` returns the same enriched object as `--json` but as a **single line per event** with no formatting. Ideal for bots and streaming pipelines.

## User Resolution

In commands accepting a `<user>` argument, you can specify:
- **npub format**: `npub1...` (long form Nostr public key)
- **Alias**: A local alias created with `nostr alias`
- **NIP-05**: `user@domain.com` (DNS-based user identifier)

## Best Practices for Scriptable Usage

### Output Parsing
```bash
# Raw event — compact single-line JSON (wire format)
nostr post "Message" --raw | jq '.id'

# Enriched JSON — pretty-printed with colors on TTY
nostr post "Message" --json

# JSONL — one compact JSON line per event (for pipes/bots)
nostr post "Message" --jsonl | jq '.relays[] | select(.ok) | .url'

# Piped input works with all output flags
echo "Hello world" | nostr --raw
echo "Hello world" | nostr --json
```

### Building a Bot
Use `--jsonl` for streaming commands — one JSON object per line, easy to parse:
```bash
# Watch all incoming DMs as JSONL (one event per line, runs forever)
nostr dm --watch --jsonl

# Watch DMs from a specific user
nostr dm alice --watch --jsonl

# Stream all notes from followed accounts
nostr --watch --jsonl

# Example: auto-reply bot
nostr dm --watch --jsonl | while read -r line; do
  from=$(echo "$line" | jq -r '.from_npub')
  msg=$(echo "$line" | jq -r '.message')
  echo "Received from $from: $msg"
  echo "pong" | nostr dm "$from" --jsonl
done
```

### Clean Output
Use `--no-color` to avoid ANSI escape codes in piped output:
```bash
nostr post "Message" --no-color
```

### Posting Messages
Post from argument or stdin:
```bash
# From argument
nostr post "Hello Nostr"

# From stdin
echo "My message" | nostr post

# Capture event ID
EVENT_ID=$(nostr post "Message" --json | jq -r '.id')
```

### Sending DMs
Send from argument or stdin:
```bash
# From argument
nostr dm alice "Private message"

# From stdin
echo "Content" | nostr dm alice
```

### Login (Non-Interactive)
```bash
# New key
nostr login --new

# Existing nsec key
nostr login --nsec nsec1abc...
```

## Configuration

- **Config directory**: `~/.nostr/`
- **Sent events**: `~/.nostr/profiles/<npub>/events.jsonl`

All accounts, aliases, and relay configurations are stored in the `~/.nostr/` directory.
