# nostr-cli

A command-line tool for the Nostr protocol. Post notes, send encrypted DMs, query events, create raw events of any kind, manage accounts and profiles, follow users, and interact with relays — all from the terminal.

## Install

```bash
curl -sL https://nostrcli.sh/install.sh | bash
```

Verify: `nostr version`

## Commands

### Post & Message
```bash
nostr post "Hello Nostr"                    # Post a note
nostr post "Reply" --reply <event-id>       # Reply to an event (simple)
nostr post "Tagged" --tag t=nostr           # Post with extra tags
nostr post "Custom" --tags '[["t","nostr"]]' # Tags as JSON array
nostr post "Test" --dry-run --json          # Sign but don't publish
echo "My message" | nostr post              # Post from stdin
nostr reply note1abc... "Great post!"       # Reply with NIP-10 threading
nostr reply <eventId> "I agree" --tag t=nostr  # Reply with extra tags
nostr reply nevent1... "Check" --tags '[["p","<hex>"]]'  # Reply with JSON tags
nostr dm alice "Hello"                      # Send encrypted DM
nostr dm alice "Hello" --jsonl              # Send DM, JSONL output
nostr dm alice "Hello" --tag subject=hi     # DM with extra tags
echo "Content" | nostr dm alice             # DM from stdin
nostr dm --watch --jsonl                    # Stream ALL incoming DMs
nostr dm alice --watch --jsonl              # Stream DMs with alice
```

### Query Events
```bash
nostr events --kinds 1 --since 1h                    # Recent text notes
nostr events --kinds 4 --since 24h --decrypt --jsonl  # Decrypt DMs as JSONL
nostr events --kinds 1,7 --author alice --limit 50    # Notes + reactions by author
nostr events --kinds 0,1,3 --since 2024-01-01 --json  # Multiple kinds since date
```

The `--since` and `--until` flags accept: durations (1h, 7d, 30m), unix timestamps, or ISO dates (2024-01-01).
The `--kinds` flag accepts comma-separated event kinds (e.g. 1,4,7).

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
| `--profile <npub\|alias>` | Use a specific account |
| `--timeout <ms>` | Relay timeout in milliseconds (default: 2000) |
| `--no-color` | Disable ANSI color codes |
| `--json` | Enriched JSON output (pretty-printed on TTY) |
| `--jsonl` | One JSON object per line (for bots/piping) |
| `--raw` | Raw Nostr event JSON (wire format) |

## Auto-Detection

- Colors are **automatically disabled** when stdout is piped (no `--no-color` needed)
- Stdin piped content is automatically read as input
- The `NO_COLOR` environment variable is respected

## User Resolution

A `<user>` can be:
- `npub1...` — Nostr public key
- `alice` — Local alias
- `user@domain.com` — NIP-05 address

## Best Practices

- Use `--jsonl` for streaming/piping (one JSON object per line)
- Use `--json` for human-readable structured output
- Use `--raw` for wire-format events
- Pipe content via stdin: `echo "msg" | nostr post`
- Capture event IDs: `nostr post "msg" --jsonl | jq -r '.id'`

## Resources

- Website: https://nostrcli.sh
- Source: https://github.com/xdamman/nostr-cli
- Full skill: https://nostrcli.sh/skill/SKILL.md
