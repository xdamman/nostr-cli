# nostr-cli

Command line interface to interact with the Nostr protocol with support for switching between different profiles.

## Installation

`go install github.com/xdamman/nostr-cli@latest`

## Main use
`$> nostr login`

Enter your nsec (leave blank to generate a new one)
Creates a new profile in ~/.nostr/profiles/:npub
Saves nsec in `~/.nostr/profiles/:npub/nsec`
Fetches current profile (kind 0) in ~/.nostr/profiles/:npub/profile.json.

$> nostr relays
Show the list of current relays with their number

$> nostr relays add wss://...
Add a new relay, saves it in `~/.nostr/profiles/:npub/relays.json`

$> nostr relays rm [relayUrl | relayNumber] 

$> nostr switch [alias|username|npub]
Switch between profiles.

$> nostr follow [npub|username]
Start following a nostr user

$> nostr profile [npub|username|alias]
Show the profile based on npub or username, default to your profile

$> nostr profile update
Interactive mode to update
- username
- name
- description
- avatar
- nip05

(always show current value, so just press enter to keep current value)

$> nostr [npub|username|alias] [--watch]

Show the profile of a user with their latest 10 notes. 
Keeps listening for new notes if --watch

$> nostr dm [npub|username|alias]

$> nostr post [message]
Post a new kind 1 text note.
If no message set, enters interactive mode to enter the message to post.

$> nostr alias Xavier [npub|username]
Create an alias (saves it in ~/.nostr/profiles/:npub/aliases.csv)

$> nostr dm [alias|username|npub] [message]
Send `message` as an encrypted direct message. If no message set, enters interactive mode that keeps on listening for new DMs with the other person.

## Power users

$> nostr nip[0-9]{1,2}
Show the corresponding NIP (based on https://nostr-nips.com).

Notes:

Also add a --help or simply "help" as the last action in the command line to get a help message, e.g. `nostr dm help`, `nostr follow help`, `nostr profile --help`).

