# nostr-cli

A command-line tool for the Nostr protocol. Post notes, send encrypted DMs (NIP-17/NIP-44), query events, create raw events of any kind, publish long-form articles (NIP-23), manage accounts and profiles, follow users, and interact with relays — all from the terminal.

## Install

```bash
curl -sL https://nostrcli.sh/install.sh | bash
```

Verify: `nostr version`

## Commands

### Post & Message
```bash
nostr post "Hello Nostr"                    # Post a note
nostr post "Tagged" --tag t=nostr           # Post with extra tags
nostr post "Custom" --tags '[["t","nostr"]]' # Tags as JSON array
nostr post "Test" --dry-run --json          # Sign but don't publish
echo "My message" | nostr post              # Post from stdin

# Long-form content (NIP-23)
nostr post -f article.md --title "My Article"            # Publish article (kind 30023)
nostr post -f article.md --slug my-article               # With explicit slug
nostr post --long --title "Quick Thoughts"               # Write in built-in editor
nostr post -f article.md --draft                         # Publish as draft (kind 30024)
nostr post -f updated.md --slug my-article               # Update existing article
nostr post -f article.md --hashtag nostr --hashtag bitcoin  # With hashtags
nostr post -f article.md --title "T" --summary "S" --image https://img.url/h.jpg

nostr reply note1abc... "Great post!"       # Reply with NIP-10 threading
nostr reply <eventId> "I agree" --tag t=nostr  # Reply with extra tags
nostr reply nevent1... "Check" --tags '[["p","<hex>"]]'  # Reply with JSON tags

nostr dm alice "Hello"                      # Send NIP-17 gift-wrapped DM (default)
nostr dm alice "Hello" --nip04              # Send legacy NIP-04 encrypted DM
nostr dm alice "Hello" --jsonl              # Send DM, JSONL output
nostr dm alice "Hello" --tag subject=hi     # DM with extra tags
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
nostr events --watch --kinds 4 --filter "p=<hex>" --jsonl  # Filter by tag
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
nostr event new --kind 1 --content "Hello" --tag t=nostr --tags '[["r","https://example.com"]]'
```

### Accounts & Profiles
```bash
nostr profile alice --json                  # View Nostr profile as JSON
nostr profile npub1... --refresh --json     # Force refresh from relays
nostr accounts --json                       # List all local accounts
nostr login --new                           # Generate new keypair
nostr login --nsec nsec1...                 # Import existing key
nostr switch alice                          # Switch active account
```

### Social
```bash
nostr follow alice                          # Follow a user
nostr unfollow alice                        # Unfollow a user
nostr following --json                      # List followed users
nostr alice --json --limit 10               # View user's recent notes
nostr alice --watch                         # Live-stream notes
nostr --watch --jsonl                       # Stream followed accounts' notes
```

### Relays
```bash
nostr relays --json                         # List relays with status
nostr relays add wss://relay.example.com    # Add a relay
nostr relays rm nos.lol -y                  # Remove by domain
```

### Sync
```bash
nostr sync --json                           # Sync all relays, JSON output
nostr sync --relay nos.lol --json           # Sync a specific relay
```

### Aliases
```bash
nostr alias alice npub1...                  # Create alias
nostr alias bob user@domain.com             # Alias from NIP-05
nostr aliases                               # List all aliases
nostr alias rm alice                        # Remove alias
```

## Global Flags

| Flag | Description |
|------|-------------|
| `--account <npub\|alias>` | Use a specific account (replaces deprecated `--profile`) |
| `--timeout <ms>` | Relay timeout in milliseconds (default: 2000) |
| `--no-color` | Disable ANSI color codes |
| `--json` | Enriched JSON output (pretty-printed on TTY) |
| `--jsonl` | One JSON object per line (for bots/piping) |
| `--raw` | Raw Nostr event JSON (wire format) |

## DM Protocol: NIP-17 and NIP-04

- **NIP-17 gift-wrapped DMs** are the default for sending (NIP-44 encryption)
- **Both NIP-04 and NIP-17** are received and decrypted automatically
- Use `--nip04` to force legacy NIP-04 encryption
- The `protocol` field in JSON/JSONL output indicates which protocol was used (`"nip04"` or `"nip17"`)
- In interactive mode, the DM protocol is auto-detected per conversation

## Auto-Detection

- Colors are **automatically disabled** when stdout is piped (no `--no-color` needed)
- Stdin piped content is automatically read as input
- The `NO_COLOR` environment variable is respected

## User Resolution

A `<user>` can be:
- `npub1...` — Nostr public key
- `alice` — Local alias
- `user@domain.com` — NIP-05 address

## Bot / Agent Patterns

### Stream DMs and respond
```bash
nostr dm --watch --jsonl | while read -r line; do
  message=$(echo "$line" | jq -r .message)
  sender=$(echo "$line" | jq -r .from_npub)
  protocol=$(echo "$line" | jq -r .protocol)
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
# Get last hour of DMs, then continue streaming
nostr dm --watch --since 1h --jsonl
```

### Filter by tags
```bash
nostr events --watch --kinds 1 --filter "t=bitcoin" --jsonl
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

### Non-interactive setup
```bash
nostr login --new
nostr relays add wss://relay.damus.io
nostr relays add wss://nos.lol
nostr post "Bot is online" --jsonl
```

## Resources

- Website: https://nostrcli.sh
- Source: https://github.com/xdamman/nostr-cli
- Full skill: https://nostrcli.sh/skill/SKILL.md
