# Backup

All nostr-cli data lives under `~/.nostr/`. Backing up this directory preserves everything you need to restore your profiles.

## What to back up

### Essential (cannot be recovered)

| File | Description |
|------|-------------|
| `~/.nostr/accounts/<npub>/nsec` | Your private key. **If lost, you lose access to your identity.** |
| `~/.nostr/accounts/<npub>/events.jsonl` | Every event you have signed and published (posts, follows, profile updates). This is your local record of everything you sent. |
| `~/.nostr/accounts/<npub>/directmessages/` | Encrypted DM conversations. One `.jsonl` file per counterparty containing all sent and received messages. |

### Important (recoverable but convenient to keep)

| File | Description |
|------|-------------|
| `~/.nostr/accounts/<npub>/relays.json` | Your relay configuration. Can be re-fetched from NIP-65 on login. |
| `~/.nostr/accounts/<npub>/profile.json` | Your profile metadata. Can be re-fetched from relays. |
| `~/.nostr/aliases.json` | Global aliases (e.g. `xavier` -> `npub1...`). |
| `~/.nostr/active` | Symlink to the active profile. |

### Not needed (cache, safely deletable)

Everything under `~/.nostr/accounts/<npub>/cache/` is re-fetched from relays on demand. See [caching.md](caching.md) for details.

## Backup commands

```bash
# Back up everything
cp -r ~/.nostr ~/nostr-backup

# Back up only essential files (keys + sent events + DMs)
mkdir -p ~/nostr-backup
for dir in ~/.nostr/accounts/*/; do
  name=$(basename "$dir")
  mkdir -p ~/nostr-backup/$name
  cp "$dir/nsec" ~/nostr-backup/$name/ 2>/dev/null
  cp "$dir/events.jsonl" ~/nostr-backup/$name/ 2>/dev/null
  cp -r "$dir/directmessages" ~/nostr-backup/$name/ 2>/dev/null
done

# Back up with tar (excluding cache)
tar czf nostr-backup.tar.gz --exclude='*/cache' ~/.nostr/
```

## Restore

```bash
# Restore from a full backup
cp -r ~/nostr-backup ~/.nostr

# Re-import from just an nsec file
nostr login --nsec <your-nsec>
```

## events.jsonl format

Each line is a signed Nostr event as JSON. This includes all events you have published:

- Kind 0: Profile metadata updates
- Kind 1: Text notes (posts)
- Kind 3: Contact list updates (follow/unfollow)
- Kind 10002: Relay list metadata (NIP-65)

These events are appended as you use nostr-cli. Since they are signed with your key, they serve as a cryptographic proof of authorship.

## directmessages/ format

Each file in `directmessages/` is named `<counterparty_hex>.jsonl` and contains one JSON event per line — both sent and received encrypted DMs (kind 4) with that counterparty. Events are appended in the order they are discovered (from cache or relays). The history is displayed sorted chronologically.
