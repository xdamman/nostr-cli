# nostr-cli

A command-line tool for the Nostr protocol. Post notes, send encrypted DMs (NIP-17/NIP-44), query events, create raw events of any kind, publish long-form articles (NIP-23), manage accounts and profiles, follow users, and interact with relays — all from the terminal.

All commands support `--json`, `--jsonl`, and `--raw` output. Colors are auto-disabled when piped. Designed for bots and AI agents.

## Install

```bash
curl -sL https://nostrcli.sh/install.sh | bash
```

Verify: `nostr version`

## Non-Interactive Setup

```bash
nostr login --new                    # Generate new keypair
nostr login --nsec nsec1...          # Import existing key
nostr relays add wss://relay.damus.io
nostr relays add wss://nos.lol
nostr post "Bot is online" --jsonl
```

## Commands

### Post & Message

```bash
nostr post "Hello Nostr"                    # Post a note
echo "My message" | nostr post              # Post from stdin
nostr post "Tagged" --tag t=nostr           # Post with extra tags
nostr post "Custom" --tags '[["t","nostr"]]' # Tags as JSON array
nostr post "Test" --dry-run --json          # Sign but don't publish
EVENT_ID=$(nostr post "Message" --jsonl | jq -r '.id')  # Capture event ID

# Long-form content (NIP-23)
nostr post -f article.md --title "My Article"            # Publish article (kind 30023)
nostr post -f article.md --slug my-article               # With explicit slug
nostr post -f article.md --draft                         # Publish as draft (kind 30024)
nostr post -f updated.md --slug my-article               # Update existing article
nostr post -f article.md --hashtag nostr --hashtag bitcoin  # With hashtags
nostr post -f article.md --title "T" --summary "S" --image https://img.url/h.jpg

nostr reply note1abc... "Great post!"       # Reply with NIP-10 threading
nostr reply <eventId> "I agree" --tag t=nostr  # Reply with extra tags
echo "Nice work" | nostr reply note1abc...  # Reply from stdin

nostr dm alice "Hello"                      # Send NIP-17 gift-wrapped DM (default)
nostr dm alice "Hello" --nip04              # Send legacy NIP-04 encrypted DM
nostr dm alice "Hello" --jsonl              # Send DM, JSONL output
echo "Content" | nostr dm alice             # DM from stdin
nostr dm --watch --jsonl                    # Stream ALL incoming DMs (NIP-04 + NIP-17)
nostr dm --watch --since 1h --jsonl         # Catch up and stream DMs
nostr dm alice --watch --jsonl              # Stream DMs with alice
```

### Query Events

```bash
nostr events --kinds 1 --since 1h                    # Recent text notes
nostr events --kinds 4 --since 24h --decrypt --jsonl  # Decrypt DMs as JSONL
nostr events --kinds 1,7 --author alice --limit 50    # Notes + reactions by author
nostr events --kinds 0,1,3 --since 2024-01-01 --json  # Multiple kinds since date
nostr events --watch --kinds 4 --decrypt --jsonl      # Live-stream decrypted DMs
nostr events --watch --kinds 1 --jsonl                # Live-stream all notes
nostr events --watch --kinds 4 --me --decrypt --jsonl # Stream DMs to me, decrypted
nostr events --kinds 1 --filter "t=bitcoin" --jsonl   # Notes tagged bitcoin
```

The `--since` and `--until` flags accept: durations (1h, 7d, 30m), unix timestamps, or ISO dates (2024-01-01).
The `--kinds` flag accepts comma-separated event kinds (e.g. 1,4,7).
The `--watch` flag keeps the connection open and streams events in real-time.
The `--filter key=value` flag (repeatable) filters by nostr tags (e.g. p, t, e).
The `--me` flag is shorthand for `--filter "p=<your_pubkey>"`.

### Create Raw Events

```bash
nostr event new --kind 1 --content "Hello world"
nostr event new --kind 7 --content "+" --tag e=<id> --tag p=<pubkey>
nostr event new --kind 0 --content '{"name":"bot"}'
echo "Hello" | nostr event new --kind 1 --content -
nostr event new --kind 1 --content "Test" --dry-run --json
```

### Accounts & Profiles

```bash
nostr profile alice --json                  # View Nostr profile as JSON
nostr profile alice -n 10 --jsonl           # View profile + last 10 events as JSONL
nostr profile alice -n 5 --kinds 1,7 --jsonl # Filter events by kind
nostr profile alice --watch --jsonl         # Live-stream new events
nostr profile npub1... --refresh --json     # Force refresh from relays
nostr accounts --json                       # List all local accounts
nostr login --new                           # Generate new keypair
nostr login --nsec nsec1...                 # Import existing key
nostr switch alice                          # Switch active account
```

### Social

```bash
nostr follow alice                          # Follow a user
nostr follow alice --alias al               # Follow with explicit alias
nostr follow alice --json                   # Follow with JSON output
nostr unfollow alice                        # Unfollow a user
nostr following --json                      # List followed users
nostr alice --json --limit 10               # View user's recent notes
nostr alice --watch --jsonl                 # Live-stream notes
nostr --watch --jsonl                       # Stream followed accounts' notes
```

### Relays

```bash
nostr relays --json                         # List relays with status
nostr relays add wss://relay.example.com    # Add a relay
nostr relays rm nos.lol -y                  # Remove by domain
```

### Aliases

```bash
nostr alias alice npub1...                  # Create alias
nostr alias bob user@domain.com             # Alias from NIP-05
nostr aliases                               # List all aliases
nostr alias rm alice                        # Remove alias
```

### Generate NIP-05

```bash
nostr generate nip05 --address user@domain.com          # Use active account
nostr generate nip05 --address user@domain.com --npub npub1...
nostr generate nip05 --address user@domain.com --json   # Output JSON to stdout
```

### Version & Update

```bash
nostr version --json                        # Version info as JSON
nostr update --json                         # Check for updates, JSON output
nostr update -y                             # Auto-update
```

## Global Flags

| Flag | Description |
|------|-------------|
| `--account <npub\|alias>` | Use a specific account |
| `--timeout <ms>` | Relay timeout in milliseconds (default: 2000) |
| `--no-color` | Disable ANSI color codes |
| `--json` | Enriched JSON output (pretty-printed on TTY) |
| `--jsonl` | One JSON object per line (for bots/piping) |
| `--raw` | Raw Nostr event JSON (wire format) |

## Output Formats

- **`--raw`** — Standard Nostr event object, as relays see it
- **`--json`** — Enriched object with event + metadata (relay status, timing, resolved names)
- **`--jsonl`** — Same enriched data, one object per line. Ideal for streaming, bots, `jq`

## User Resolution

A `<user>` can be:
- `npub1...` — Nostr public key
- `alice` — Local alias
- `user@domain.com` — NIP-05 address

## Auto-Detection

- Colors auto-disabled when stdout is piped
- Stdin piped content read as input
- `NO_COLOR` env var respected

## Bot / Agent Patterns

### Stream DMs and respond

```bash
nostr dm --watch --jsonl | while read -r line; do
  message=$(echo "$line" | jq -r .message)
  sender=$(echo "$line" | jq -r .from_npub)
  nostr dm "$sender" "Got: $message"
done
```

### Stream events addressed to you (bot inbox)

```bash
nostr events --watch --kinds 4 --me --decrypt --jsonl | while read -r line; do
  echo "$line" | jq '{from: .author, msg: .content, protocol: .protocol}'
done
```

### Stream with catch-up

```bash
nostr dm --watch --since 1h --jsonl
```

### Post and capture event ID

```bash
EVENT_ID=$(nostr post "Hello" --jsonl | jq -r '.id')
```

### Use a specific account for bot operations

```bash
nostr post "Bot message" --account mybot --jsonl
nostr dm alice "Alert" --account mybot
```

## DM Protocol

- **NIP-17 gift-wrapped DMs** are the default for sending (NIP-44 encryption)
- **Both NIP-04 and NIP-17** are received and decrypted automatically
- Use `--nip04` to force legacy NIP-04 encryption
- The `protocol` field in JSON/JSONL output indicates which protocol was used

## NIP-05 Agent Identities

All nostr-cli agents have verified NIP-05 identities:
- `agent@xavierdamman.com` — verified NIP-05 addresses for bot accounts
- Agent profiles set `bot: true` per NIP-24

## Resources

- Website: https://nostrcli.sh
- Source: https://github.com/xdamman/nostr-cli
- Full skill: https://nostrcli.sh/skill/SKILL.md
