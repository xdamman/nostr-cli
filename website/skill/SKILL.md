---
name: nostr-cli
description: Post notes, send encrypted DMs (NIP-17/NIP-44), query events, create raw events, publish long-form articles (NIP-23), manage accounts, follow users, and interact with Nostr relays from the terminal.
---

# nostr-cli

A command-line tool for interacting with the Nostr protocol. All commands below are non-interactive and designed for scripting, piping, and bot integration. Colors are auto-disabled when piped. Use `--json`, `--jsonl`, or `--raw` for machine-readable output.

## Installation

```bash
# Check if installed
nostr version

# Install if missing
curl -sL https://nostrcli.sh/install.sh | bash
```

## Non-Interactive Setup

```bash
nostr login --new                    # Generate new keypair
nostr login --nsec nsec1...          # Import existing key
nostr relays add wss://relay.damus.io
nostr relays add wss://nos.lol
```

## Output Formats

| Flag | Description | Use case |
|------|-------------|----------|
| `--json` | Enriched JSON (event + metadata, pretty-printed on TTY) | Inspection, debugging |
| `--jsonl` | One JSON object per line | Streaming, bots, `jq` pipelines |
| `--raw` | Raw Nostr event JSON (wire format) | Forwarding to other nostr tools |

## Command Reference

### Post Notes

```bash
nostr post [message]
```

Publishes a kind 1 text note. Message from argument, stdin, or `--json` for structured output.

**Flags:** `--tag key=value` (repeatable), `--tags '<json>'`, `--dry-run`, `--json` / `--jsonl` / `--raw`

**Examples:**
```bash
nostr post "Hello Nostr"
echo "My message" | nostr post
nostr post "Tagged" --tag t=nostr --tag t=bitcoin
nostr post "Test" --dry-run --json
EVENT_ID=$(nostr post "Message" --jsonl | jq -r '.id')
```

### Long-Form Content (NIP-23)

```bash
nostr post -f <file> [flags]
```

Publish articles (kind 30023) or drafts (kind 30024).

**Flags:** `-f, --file <path>`, `--title`, `--summary`, `--image <url>`, `--slug`, `--draft`, `--hashtag` (repeatable), `--dry-run`, `--json` / `--jsonl` / `--raw`

YAML frontmatter auto-parsed. CLI flags override frontmatter.

**Examples:**
```bash
nostr post -f article.md --title "My Article"
nostr post -f article.md --slug my-article --draft
nostr post -f updated.md --slug my-article    # Update existing
nostr post -f article.md --hashtag nostr --hashtag bitcoin
```

### Reply

```bash
nostr reply <eventId> [message]
```

Reply with NIP-10 threading. Event ID: hex, note1..., or nevent1...

**Flags:** `--tag key=value`, `--tags '<json>'`, `--dry-run`, `--json` / `--jsonl` / `--raw`

**Examples:**
```bash
nostr reply note1abc... "Great post!"
nostr reply note1abc... "Tagged" --tag t=nostr
echo "Nice work" | nostr reply note1abc...
```

### Direct Messages

```bash
nostr dm <user> [message]
```

Send encrypted DMs. NIP-17 gift-wrapped by default. Both NIP-04 and NIP-17 received/decrypted.

**Non-interactive modes:**
- `nostr dm <user> <message>` — Send one-shot DM
- `echo "msg" | nostr dm <user>` — Send from stdin
- `nostr dm --watch` — Stream ALL incoming DMs
- `nostr dm <user> --watch` — Stream DMs with a specific user

**Flags:** `--nip04` (legacy), `--watch`, `--since <time>`, `--no-decrypt`, `--tag key=value`, `--json` / `--jsonl` / `--raw`

**Output:** JSON/JSONL includes `protocol` field (`"nip04"` or `"nip17"`), `message`, `from_npub`.

**Examples:**
```bash
nostr dm alice "Hello"
nostr dm alice "Hello" --nip04
echo "Alert" | nostr dm alice
nostr dm --watch --jsonl | while read -r line; do echo "$line" | jq .message; done
nostr dm alice --watch --jsonl
nostr dm --watch --since 1h --jsonl
```

### Query Events

```bash
nostr events --kinds <kinds> [flags]
```

Query events from relays with flexible filters.

**Flags:** `--kinds <n,n,...>` (required), `--since <time>`, `--until <time>`, `--author <user>`, `--limit <n>`, `--decrypt`, `--watch`, `--filter key=value` (repeatable), `--me`, `--json` / `--jsonl` / `--raw`

Time formats: durations (1h, 7d), unix timestamps, ISO dates (2024-01-01).

**Examples:**
```bash
nostr events --kinds 1 --since 1h
nostr events --kinds 4 --since 24h --decrypt --jsonl
nostr events --kinds 1,7 --author alice --limit 50 --json
nostr events --watch --kinds 4 --me --decrypt --jsonl
nostr events --watch --kinds 1 --filter "t=bitcoin" --jsonl
```

### Create Raw Events

```bash
nostr event new --kind <n> --content <text> [flags]
```

Create, sign, and publish a Nostr event of any kind.

**Flags:** `--kind <n>` (required), `--content <text>` (required, `-` for stdin), `--tag key=value`, `--tags '<json>'`, `--pow <n>`, `--dry-run`, `--json` / `--jsonl` / `--raw`

**Examples:**
```bash
nostr event new --kind 1 --content "Hello world"
nostr event new --kind 7 --content "+" --tag e=<eventid> --tag p=<pubkey>
echo "Hello" | nostr event new --kind 1 --content -
nostr event new --kind 1 --content "Test" --dry-run --json
```

### Profiles

```bash
nostr profile [user]
```

View profile metadata and past events.

**Flags:** `-n, --events <n>`, `--kinds <n,n,...>`, `--watch`, `--refresh`, `--json` / `--jsonl` / `--raw`

**Examples:**
```bash
nostr profile alice --json
nostr profile alice -n 10 --jsonl
nostr profile alice -n 5 --kinds 1,7 --jsonl
nostr profile alice --watch --jsonl
```

### Follow Management

```bash
nostr follow <user>        # Follow
nostr unfollow <user>      # Unfollow
nostr following            # List followed
```

**Flags:** `--alias <name>`, `--json` / `--jsonl` / `--raw`

**Examples:**
```bash
nostr follow alice --json
nostr follow alice --alias al
nostr following --json
```

### Relay Management

```bash
nostr relays               # List relays
nostr relays add <url>     # Add relay
nostr relays rm <id|url>   # Remove relay
```

**Flags:** `--json`, `--yes` / `-y`

**Examples:**
```bash
nostr relays --json
nostr relays add wss://relay.example.com
nostr relays rm nos.lol -y
```

### Accounts

```bash
nostr login --new                    # Generate keypair
nostr login --nsec nsec1...          # Import key
nostr switch <account>               # Switch account
nostr accounts --json                # List accounts
```

### Aliases

```bash
nostr alias <name> <user>    # Create alias
nostr aliases                # List aliases
nostr alias rm <name>        # Remove alias
```

### User Lookup

```bash
nostr <user> --json --limit 10       # Recent notes
nostr <user> --watch --jsonl         # Stream notes
nostr --watch --jsonl                # Stream followed accounts
```

### Generate NIP-05

```bash
nostr generate nip05 --address user@domain.com
nostr generate nip05 --address user@domain.com --npub npub1...
nostr generate nip05 --address user@domain.com --json
```

### Other

```bash
nostr version --json
nostr update -y
nostr nip 01                         # View NIP spec
```

## Global Flags

| Flag | Description |
|------|-------------|
| `--account <npub\|alias>` | Use a specific account |
| `--timeout <ms>` | Relay timeout (default: 2000) |
| `--no-color` | Disable colors (auto when piped) |
| `--json` | Enriched JSON output |
| `--jsonl` | One JSON per line |
| `--raw` | Raw Nostr event JSON |

## User Resolution

- `npub1...` — Nostr public key (bech32)
- `alice` — Local alias
- `user@domain.com` — NIP-05 address

## Bot/Agent Recipes

### Stream DMs and respond

```bash
nostr dm --watch --jsonl | while read -r line; do
  message=$(echo "$line" | jq -r .message)
  sender=$(echo "$line" | jq -r .from_npub)
  nostr dm "$sender" "Got: $message"
done
```

### Bot inbox

```bash
nostr events --watch --kinds 4 --me --decrypt --jsonl | while read -r line; do
  echo "$line" | jq '{from: .author, msg: .content}'
done
```

### Post and capture event ID

```bash
EVENT_ID=$(nostr post "Hello" --jsonl | jq -r '.id')
```

### Use a specific account

```bash
nostr post "Bot msg" --account mybot --jsonl
nostr dm alice "Alert" --account mybot
```

## Configuration

- Config directory: `~/.nostr/`
- Accounts: `~/.nostr/accounts/<npub>/`
- Sent events: `~/.nostr/accounts/<npub>/events.jsonl`
- DM history: `~/.nostr/accounts/<npub>/directmessages/<hex>.jsonl`
