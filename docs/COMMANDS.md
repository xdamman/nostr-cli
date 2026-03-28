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
3. Create `~/.nostr/profiles/<npub>/` directory
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
| `--generate` | Skip prompt, generate new keypair |

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
- Fetch fresh kind 0 from relays, update local cache

**`profile update` (edit):**
- Interactive prompts for each field (show current value, enter to keep)
- Fields: `name`, `username` (`display_name`), `about`, `picture`, `nip05`
- Build and sign new kind 0 event
- Publish to all configured relays

**Edge cases:**
- Unknown npub with no relay data → "Profile not found. Try adding relays."
- NIP-05 verification failure → show warning but still display profile

---

## `nostr post`

**Priority:** P0  
**NIPs:** NIP-01 (kind 1)

Publish a text note.

```
nostr post "Hello Nostr!"
nostr post                    # Interactive mode
```

**Behavior:**
1. If message argument given → use it
2. If no argument → open interactive prompt (or `$EDITOR` if set)
3. Build kind 1 event with content
4. Sign and publish to configured relays
5. Print event ID (note1... and hex)

**Flags:**
| Flag | Description |
|------|-------------|
| `--reply <event_id>` | Reply to an event (adds `e` tag) |
| `--json` | Output the signed event as JSON instead of publishing |

**Edge cases:**
- Empty message → error
- No relays configured → error with hint to run `nostr relays add`

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

**`relays` (list):**
- Print numbered list with animated loading indicator per relay
- Show connection status with response time (✓ connected 142ms, ✗ unreachable)
- Hints shown immediately, relay status updates as responses arrive

**`relays add`:**
- Validate URL format (must be `wss://` or `ws://`)
- Append to `relays.json`
- Attempt connection to verify

**`relays rm`:**
- Accept relay URL, domain name, or the number from `relays` list
- Ask for confirmation before removing (skip with `--yes`/`-y` or `--json`)
- Remove from `relays.json`

**Edge cases:**
- Duplicate relay on add → warn, skip
- Remove last relay → warn but allow

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

**Behavior:**
1. Load local sent events from `events.jsonl`
2. Filter to syncable events (skip superseded replaceable events per NIP-01)
3. Fetch events from each relay (shows per-relay progress)
4. Interactive checklist to select which relays to sync
5. Save any new events from relays locally
6. Publish local events missing from selected relays

**Flags:**
| Flag | Description |
|------|-------------|
| `--relay <url\|domain>` | Sync with a specific relay (full URL or domain) |
| `--json` | Output results as JSON without interactive UI |

**Replaceable events (NIP-01):**
- Kind 0, 3, 10000-19999: only latest per pubkey+kind is synced
- Kind 20000-29999 (ephemeral): skipped entirely
- Kind 30000-39999 (addressable): only latest per pubkey+kind+d-tag

**Edge cases:**
- No local events → only fetches from relays
- All relays in sync → "Everything is in sync"
- Relay unreachable → marked as failed, error shown

---

## `nostr follow`

**Priority:** P1  
**NIPs:** NIP-01 (kind 3), NIP-02

Follow or unfollow a user.

```
nostr follow [npub|username|alias]
nostr unfollow [npub|username|alias]
```

**Behavior:**
1. Fetch current kind 3 (contact list) from relays
2. Add/remove the target npub from `p` tags
3. Sign and publish updated kind 3

**Edge cases:**
- Already following → "Already following <name>"
- Following yourself → warn but allow
- No existing kind 3 → create new one with just this contact

---

## `nostr switch`

**Priority:** P1  
**NIPs:** NIP-19

Switch between accounts.

```
nostr switch                          # List accounts, pick one
nostr switch [alias|username|npub]    # Switch directly
```

**Behavior:**
- - Update `~/.nostr/active` to point to the selected account
- Print confirmation: "Switched to <name> (<npub short>)"

**Edge cases:**
- No other accounts → "Only one account. Use `nostr login` to add another."
- Unknown identifier → "Account not found. Available accounts: ..."

---

## `nostr dm`

**Priority:** P1  
**NIPs:** NIP-04 (legacy), NIP-44 (preferred), NIP-17

Send or receive encrypted direct messages.

```
nostr dm [user] [message]    # Send a message
nostr dm [user]              # Interactive chat mode
```

**Send mode:**
- Encrypt message with NIP-44 (fall back to NIP-04 if needed)
- Publish to relays
- Print confirmation

**Interactive mode:**
- Subscribe to DM events with the target user
- Display incoming messages in real-time
- Prompt for replies
- Ctrl+C to exit

**Flags:**
| Flag | Description |
|------|-------------|
| `--nip04` | Force NIP-04 encryption (legacy) |
| `--json` | Output event and relay results as JSON |

---

## `nostr alias`

**Priority:** P1  
**NIPs:** —

Create local aliases for npubs.

```
nostr alias [name] [npub|username]
nostr alias                           # List aliases
nostr alias rm [name]                 # Remove alias
```

**Behavior:**
- Save to `~/.nostr/profiles/<npub>/aliases.csv`
- Aliases are account-scoped
- Used for resolution in all commands that accept user identifiers

---

## `nostr [user]`

**Priority:** P1  
**NIPs:** NIP-01

View a user's profile and recent notes.

```
nostr [npub|username|alias]
nostr [user] --watch
```

**Behavior:**
- Fetch and display profile (kind 0)
- Fetch and display latest 10 notes (kind 1)
- With `--watch`: keep subscription open, print new notes as they arrive

**Flags:**
| Flag | Description |
|------|-------------|
| `--watch` | Live-stream new notes |
| `--limit <n>` | Number of past notes to show (default: 10) |

---

## `nostr nip[N]`

**Priority:** P2  
**NIPs:** —

View a NIP specification in the terminal.

```
nostr nip01
nostr nip44
```

**Behavior:**
- Fetch NIP markdown from source (nostr-nips.com or GitHub)
- Render in terminal with syntax highlighting
- Cache locally for offline access

---

## Command Resolution Order

When the CLI receives `nostr <arg>`:

1. Match against known subcommands (`login`, `post`, `dm`, etc.)
2. Match against aliases in `aliases.csv`
3. Match against npub format (`npub1...`)
4. Match against username (fetch via NIP-05 or relay query)
5. Error: "Unknown command or user: <arg>"
