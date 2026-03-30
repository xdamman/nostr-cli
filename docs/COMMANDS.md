# Command Specification

Priority levels: **P0** = MVP, **P1** = Important, **P2** = Nice to have

---

## `nostr login`

**Priority:** P0  
**NIPs:** NIP-01, NIP-19

Create a new account or import an existing one.

```
nostr login
```

**Behavior:**
1. Prompt for nsec (leave blank to generate a new keypair)
2. Derive npub from nsec
3. Create `~/.nostr/accounts/<npub>/` directory
4. Save nsec (file mode `0600`)
5. Fetch kind 0 from relays → save as `profile.json`
6. Set as active account
7. Print npub and account summary

**Edge cases:**
- If npub directory already exists → ask to overwrite or switch to it
- If no relays configured → use default relay list
- Invalid nsec format → error with hint about expected format (nsec1... or hex)

**Flags:**
| Flag | Description |
|------|-------------|
| `--nsec <key>` | Non-interactive import |
| `--new` | Skip prompt, generate new keypair |
| `--generate` | Alias for `--new` |

---

## `nostr profile`

**Priority:** P0  
**NIPs:** NIP-01 (kind 0), NIP-05, NIP-19

View or update profile metadata.

```
nostr profile                         # Show your profile
nostr profile [npub|username|alias]   # Show someone else's
nostr profile update                  # Interactive edit
```

**`profile` (view):**
- Display: name, username, npub, nip-05, about, picture URL
- Fast profile: cache-first with NIP-05 cache, fetch fresh kind 0 in background
- Use `--refresh` to force relay fetch

**`profile update` (edit):**
- Interactive prompts for each field (show current value, enter to keep)
- Fields: `name`, `username` (`display_name`), `about`, `picture`, `nip05`
- Build and sign new kind 0 event
- Publish to all configured relays

**Flags:**
| Flag | Description |
|------|-------------|
| `-n, --events <n>` | Number of past events to show |
| `--kinds <n,n,...>` | Filter events by kind (comma-separated) |
| `--watch` | Live-stream new events from this user |
| `--refresh` | Force fetch from relays (bypass cache) |
| `--json` / `--jsonl` / `--raw` | Structured output |

**Event viewing:**
When `-n` is specified, the profile is shown followed by the user's most recent events. Combine with `--kinds` to filter by event type and `--watch` to live-stream. Works with `--json`/`--jsonl`/`--raw` for machine-readable output.

**Edge cases:**
- Unknown npub with no relay data → "Profile not found. Try adding relays."
- NIP-05 verification failure → show warning but still display profile

---

## `nostr post`

**Priority:** P0  
**NIPs:** NIP-01 (kind 1), NIP-23 (kind 30023/30024)

Publish a text note or long-form article.

```
nostr post "Hello Nostr!"
nostr post                    # Interactive mode
nostr post -f article.md      # Long-form content
nostr post --long             # Long-form editor
```

**Short-form (kind 1):**
1. If message argument given → use it
2. If stdin piped → read from stdin
3. If no argument → open interactive prompt
4. Build kind 1 event with content
5. Sign and publish to configured relays

**Long-form (kind 30023/30024):**
Activated by `--file`, `--long`, `--title`, or `--slug` flags.
1. Read content from file or built-in editor
2. Parse YAML frontmatter if present (title, summary, image, slug, hashtags, draft)
3. CLI flags override frontmatter values
4. Build kind 30023 (or 30024 if `--draft`) event
5. Sign and publish to configured relays

**Flags:**
| Flag | Description |
|------|-------------|
| `-f, --file <path>` | Read content from a markdown file |
| `--long` | Open built-in multi-line editor |
| `--title <string>` | Article title |
| `--summary <string>` | Article summary |
| `--image <url>` | Header image URL |
| `--slug <string>` | Article identifier / d tag (for updates) |
| `--draft` | Publish as draft (kind 30024) |
| `--hashtag <string>` | Hashtag topics (repeatable, t tags) |
| `--tag key=value` | Add extra tags (repeatable) |
| `--tags '<json>'` | Add extra tags as JSON array |
| `--dry-run` | Sign but don't publish |
| `--json` / `--jsonl` / `--raw` | Machine-readable output |

**Edge cases:**
- Empty message → error
- No relays configured → error with hint to run `nostr relays add`
- Same `--slug` replaces previous article (addressable event)

---

## `nostr reply`

**Priority:** P1
**NIPs:** NIP-01 (kind 1), NIP-10

Reply to an existing event with NIP-10 compliant threading.

```
nostr reply <eventId> [message]
```

The event ID can be hex, note1..., or nevent1... format. The referenced event is fetched from relays to determine thread structure (root vs reply markers).

**Flags:**
| Flag | Description |
|------|-------------|
| `--tag key=value` | Add extra tags (repeatable) |
| `--tags '<json>'` | Add extra tags as JSON array |
| `--dry-run` | Sign but don't publish |
| `--json` / `--jsonl` / `--raw` | Machine-readable output |

---

## `nostr dm`

**Priority:** P1  
**NIPs:** NIP-04 (legacy), NIP-44, NIP-17

Send or receive encrypted direct messages.

```
nostr dm [user] [message]    # Send a message (NIP-17 by default)
nostr dm [user]              # Interactive chat mode
nostr dm --watch             # Stream all incoming DMs
nostr dm [user] --watch      # Stream DMs with a specific user
```

**Protocol:**
- **NIP-17 gift-wrapped DMs** are the default for sending (NIP-44 encryption)
- **Both NIP-04 and NIP-17** messages are received and decrypted
- Use `--nip04` to force legacy NIP-04 encryption
- JSON/JSONL output includes a `protocol` field (`"nip04"` or `"nip17"`)
- In interactive mode, DM protocol is auto-detected per conversation

**Flags:**
| Flag | Description |
|------|-------------|
| `--nip04` | Force NIP-04 encryption (legacy) |
| `--watch` | Stream incoming DMs |
| `--since <time>` | Start time for --watch (duration, timestamp, or ISO date) |
| `--no-decrypt` | Don't decrypt messages (decrypt is default for kind 4) |
| `--tag key=value` | Add extra tags (repeatable) |
| `--tags '<json>'` | Add extra tags as JSON array |
| `--json` / `--jsonl` / `--raw` | Machine-readable output |

**Typing indicators:**
- In interactive DM mode, ephemeral kind 10003 events signal typing activity
- In NIP-17 mode, typing indicators are gift-wrapped for metadata privacy
- In NIP-04 mode, typing indicators are sent as plain ephemeral events
- Shows "\<name\> is typing..." in the status bar

**Multiline input:**
- Use Alt+Enter to insert newlines in interactive DM mode
- Visual line-wrapping textarea for long messages

**Watch mode:**
- Connection errors and subscription failures logged to stderr
- A "ready" line printed to stderr when relay connections are established
- Use `--since` with `--watch` to catch up on missed events then continue streaming

---

## `nostr events`

**Priority:** P1
**NIPs:** NIP-01

Query events from relays with flexible filters.

```
nostr events --kinds <kinds> [flags]
```

**Flags:**
| Flag | Description |
|------|-------------|
| `--kinds <n,n,...>` | Event kinds, comma-separated (required) |
| `--since <time>` | Start time: duration (1h, 7d), timestamp, or ISO date |
| `--until <time>` | End time: same formats as --since |
| `--author <user>` | Filter by author (npub, alias, or NIP-05) |
| `--limit <n>` | Maximum events to return (default: 50) |
| `--decrypt` | Decrypt kind 4 DM content |
| `--no-decrypt` | Explicitly skip decryption |
| `--watch` | Live-stream events (keeps connection open) |
| `--filter key=value` | Tag filter (repeatable, e.g. `p=<hex>`, `t=bitcoin`) |
| `--me` | Shortcut for `--filter "p=<your_pubkey>"` |
| `--json` / `--jsonl` / `--raw` | Machine-readable output |

---

## `nostr event new`

**Priority:** P1
**NIPs:** NIP-01

Create, sign, and publish a raw Nostr event of any kind.

```
nostr event new --kind <n> --content <text> [flags]
```

**Flags:**
| Flag | Description |
|------|-------------|
| `--kind <n>` | Event kind number (required) |
| `--content <text>` | Event content (required, use `-` for stdin) |
| `--tag key=value` | Tags in key=value format (repeatable) |
| `--tags '<json>'` | Extra tags as JSON array |
| `--pow <n>` | Proof of work difficulty (leading zero bits) |
| `--dry-run` | Sign but don't publish |
| `--json` / `--jsonl` / `--raw` | Machine-readable output |

---

## `nostr relays`

**Priority:** P0  
**NIPs:** NIP-01

Manage the relay list for the active account.

```
nostr relays                     # List relays (numbered)
nostr relays add wss://...       # Add a relay
nostr relays rm [url|number]     # Remove a relay
```

**Flags:**
| Flag | Description |
|------|-------------|
| `--json` | Output as JSON with connection status and ping |
| `--relay <url\|domain>` | Show a specific relay only |
| `--yes` / `-y` | Skip confirmation on rm |

---

## `nostr sync`

**Priority:** P1
**NIPs:** NIP-01

Sync locally stored events with configured relays.

```
nostr sync                           # Interactive relay selection
nostr sync --relay nos.lol           # Sync with a specific relay
nostr sync --json                    # Machine-readable output
```

**Flags:**
| Flag | Description |
|------|-------------|
| `--relay <url\|domain>` | Sync with a specific relay |
| `--json` | Output results as JSON without interactive UI |

---

## `nostr follow`

**Priority:** P1  
**NIPs:** NIP-01 (kind 3), NIP-02

Follow or unfollow a user.

```
nostr follow [npub|username|alias]
nostr unfollow [npub|username|alias]
nostr following
```

**Flags for `follow`:**
| Flag | Description |
|------|-------------|
| `--alias <name>` | Set an explicit alias for the followed user |
| `--json` / `--raw` | Structured output (includes event + relays) |

**Flags for `following`:**
| Flag | Description |
|------|-------------|
| `--refresh` | Force refresh from relays |
| `--json` / `--jsonl` | Structured output |

**Behavior notes:**
- In `--json` mode, the spinner is suppressed and output includes the follow event and relay publish results
- In non-interactive mode (piped stdin), the alias prompt is skipped

---

## `nostr switch`

**Priority:** P1  
**NIPs:** NIP-19

Switch between accounts.

```
nostr switch                          # Interactive picker (arrow keys)
nostr switch [alias|username|npub]    # Switch directly
```

In the interactive shell, use `/switch` for an arrow-key account picker.

---

## `nostr accounts`

**Priority:** P1

List all local accounts.

```
nostr accounts
nostr accounts rm [name]
```

**Flags:**
| Flag | Description |
|------|-------------|
| `--json` | JSON output |

---

## `nostr alias`

**Priority:** P1  
**NIPs:** —

Create local aliases for npubs.

```
nostr alias [name] [npub|username]
nostr aliases
nostr alias rm [name]
```

---

## `nostr [user]`

**Priority:** P1  
**NIPs:** NIP-01

View a user's profile and recent notes.

```
nostr [npub|username|alias]
nostr [user] --watch
```

**Flags:**
| Flag | Description |
|------|-------------|
| `--watch` | Live-stream new notes |
| `--limit <n>` | Number of past notes to show (default: 10) |
| `--json` / `--jsonl` / `--raw` | Machine-readable output |

---

## `nostr nip[N]`

**Priority:** P2  
**NIPs:** —

View a NIP specification in the terminal.

```
nostr nip01
nostr nip44
```

---

## Interactive Shell

Run `nostr` with no arguments to launch the interactive shell:

- Shows your feed from followed users
- Type to post a note (Alt+Enter for multiline)
- Visual line-wrapping textarea input
- Slash commands: `/follow`, `/unfollow`, `/dm`, `/profile`, `/switch`, `/alias`, `/aliases`
- Tab/arrow-key autocomplete for slash commands and @ mentions
- `/switch` shows an arrow-key account picker
- `/dm` with autocomplete and compose flow
- `nostr:npub1...` references rendered as `@username` in terminal
- Quoted `nostr:note1...` and `nostr:nevent1...` references displayed inline

---

## Global Flags

| Flag | Description |
|------|-------------|
| `--account <npub\|alias\|username>` | Use a specific account (replaces deprecated `--profile`) |
| `--timeout <ms>` | Relay timeout in milliseconds (default: 2000) |
| `--no-color` | Disable colored output |
| `--json` | Enriched JSON output (pretty-printed on TTY) |
| `--jsonl` | One JSON object per line (for bots/piping) |
| `--raw` | Raw Nostr event JSON (wire format) |

---

## Command Resolution Order

When the CLI receives `nostr <arg>`:

1. Match against known subcommands (`login`, `post`, `dm`, etc.)
2. Match against aliases in `aliases.csv`
3. Match against npub format (`npub1...`)
4. Match against username (fetch via NIP-05 or relay query)
5. Error: "Unknown command or user: <arg>"
