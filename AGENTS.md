# nostr-cli

A command-line tool for the Nostr protocol. Post notes, send encrypted DMs, manage profiles, follow users, and interact with relays — all from the terminal.

## Install

```bash
curl -sL https://nostrcli.sh/install.sh | bash
```

Verify: `nostr version`

## Commands

### Post & Message
```bash
nostr post "Hello Nostr"                    # Post a note
nostr post "Reply" --reply <event-id>       # Reply to an event
echo "My message" | nostr post              # Post from stdin
nostr dm alice "Hello"                      # Send encrypted DM
nostr dm alice "Hello" --json               # Send DM, JSON output
echo "Content" | nostr dm alice             # DM from stdin
```

### Profiles
```bash
nostr profile alice --json                  # View profile as JSON
nostr profile npub1... --refresh --json     # Force refresh from relays
nostr profiles --json                       # List all local profiles
nostr login --new                           # Generate new keypair
nostr login --nsec nsec1...                 # Import existing key
nostr switch alice                          # Switch active profile
```

### Social
```bash
nostr follow alice                          # Follow a user
nostr unfollow alice                        # Unfollow a user
nostr following --json                      # List followed users
nostr alice --json --limit 10               # View user's recent notes
nostr alice --watch                         # Live-stream notes
```

### Relays
```bash
nostr relays --json                         # List relays with status
nostr relays --relay nos.lol --json         # Show a specific relay
nostr relays add wss://relay.example.com    # Add a relay
nostr relays rm wss://relay.example.com     # Remove a relay
nostr relays rm nos.lol -y                  # Remove by domain, skip confirmation
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
nostr aliases --json                        # List all aliases
nostr alias rm alice                        # Remove alias
```

## Global Flags

- `--profile <npub|alias>` — Use a specific profile
- `--timeout <ms>` — Relay timeout in milliseconds (default: 2000)
- `--no-color` — Disable ANSI color codes
- `--raw` — Output raw Nostr event JSON (wire format, as relays see it)
- `--json` — Enriched JSON output with event + relay results (most commands)

## User Resolution

A `<user>` can be:
- `npub1...` — Nostr public key
- `alice` — Local alias
- `user@domain.com` — NIP-05 address

## Best Practices

- Use `--json` when parsing output programmatically
- Use `--no-color` when piping output to other tools
- Use `--timeout` to control relay response time
- Pipe content via stdin: `echo "msg" | nostr post`
- Capture event IDs: `nostr post "msg" --json | jq -r '.id'`

## Resources

- Website: https://nostrcli.sh
- Source: https://github.com/xdamman/nostr-cli
- Full skill: https://nostrcli.sh/skill/SKILL.md
