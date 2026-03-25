---
name: nostr-cli
description: Post notes, send encrypted DMs, manage profiles, follow users, and interact with Nostr relays from the terminal.
---

# nostr-cli

A command-line tool for interacting with the Nostr protocol. It enables posting notes, sending encrypted DMs, managing user profiles and aliases, following other users, and managing Nostr relays — all from the terminal or within scripts.

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
- `--json` — Output result as JSON (includes event ID, signature, etc.)
- `--timeout <ms>` — Relay timeout in milliseconds (default: 2000)

Examples:
```bash
nostr post "Hello Nostr"
echo "My message" | nostr post
nostr post "Reply" --reply <event-id> --json
```

### Direct Messages
```bash
nostr dm <user> [message]
```

Sends NIP-44 encrypted direct messages. `<user>` can be an npub, alias, or NIP-05 address.

Flags:
- `--json` — Output event and relay results as JSON

Examples:
```bash
nostr dm npub1... "Hello"
nostr dm alice@example.com "Message" --json
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
nostr profiles              # List all cached profiles
nostr profiles rm [name]    # Remove a cached profile
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

### Profile Management
```bash
nostr switch [profile]    # Switch to a different profile
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

- `--profile <npub|alias>` — Execute command under a specific profile
- `--timeout <ms>` — Relay timeout in milliseconds (default: 2000)
- `--no-color` — Strip ANSI color codes
- `--json` — Output as JSON (where supported)

## User Resolution

In commands accepting a `<user>` argument, you can specify:
- **npub format**: `npub1...` (long form Nostr public key)
- **Alias**: A local alias created with `nostr alias`
- **NIP-05**: `user@domain.com` (DNS-based user identifier)

## Best Practices for Scriptable Usage

### Output Parsing
Always use `--json` when you need to parse output programmatically:
```bash
nostr post "Message" --json | jq '.id'
nostr profile alice --json | jq '.about'
nostr relays --json | jq '.[]'
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

All profiles, aliases, and relay configurations are stored in the `~/.nostr/` directory.
