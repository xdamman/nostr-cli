# NIP-05 Setup Guide

Human-readable Nostr identities — like email addresses for your public key.

## What is NIP-05?

NIP-05 lets you verify your Nostr identity with a human-readable address like `user@domain.com`. Instead of sharing a long `npub1...` string, people can find you by your domain-verified name.

It works by placing a small JSON file at `https://domain.com/.well-known/nostr.json` that maps usernames to public keys. Nostr clients check this file to verify that an account really belongs to that domain.

## How it works

When a Nostr client sees `user@domain.com` as a NIP-05 identifier, it:

1. Fetches `https://domain.com/.well-known/nostr.json?name=user`
2. Looks for the username in the `names` object
3. Verifies the hex public key matches the profile
4. Displays a verification checkmark ✓

## Step-by-step setup

### 1. Generate your nostr.json

Use the built-in generator:

```bash
$ nostr generate nip05 --address user@yourdomain.com

✓ Generated nostr.json in current directory
```

Or in interactive mode:

```bash
$ nostr generate nip05

NIP-05 address (user@domain): user@yourdomain.com
npub (leave blank to use active account):

✓ Generated nostr.json in current directory
```

This creates a `nostr.json` file:

```json
{
  "names": {
    "user": "your-hex-pubkey-here"
  }
}
```

### 2. Upload to your web server

Place the file at `/.well-known/nostr.json` on your domain:

```bash
scp nostr.json yourserver:/var/www/html/.well-known/nostr.json
```

### 3. Configure CORS headers

Your server must return `Access-Control-Allow-Origin: *` for the nostr.json file.

**Nginx:**

```nginx
location /.well-known/nostr.json {
    add_header Access-Control-Allow-Origin *;
    add_header Content-Type application/json;
}
```

**Apache:**

```apache
<Directory "/var/www/html/.well-known">
    Header set Access-Control-Allow-Origin "*"
</Directory>
```

**Caddy:**

```
yourdomain.com {
    handle /.well-known/nostr.json {
        header Access-Control-Allow-Origin *
        file_server
    }
}
```

### 4. Verify it works

Test that the file is accessible:

```bash
$ curl https://yourdomain.com/.well-known/nostr.json

{
  "names": {
    "user": "your-hex-pubkey-here"
  }
}
```

Then update your Nostr profile's NIP-05 field:

```bash
$ nostr profile update
# Set nip05 to: user@yourdomain.com
```

## Multiple users on one domain

A single `nostr.json` can map multiple usernames. The `nostr generate nip05` command automatically merges entries if the domain already has a nostr.json file:

```json
{
  "names": {
    "alice": "abc123...",
    "bob": "def456...",
    "_": "789abc..."
  }
}
```

The `_` key is special — it's the default identity for the bare domain (e.g., `yourdomain.com` without a username prefix).

## Common issues

- **CORS errors:** Nostr clients fetch nostr.json from the browser. Without `Access-Control-Allow-Origin: *`, the request will be blocked. This is the #1 issue.

- **HTTPS required:** NIP-05 only works over HTTPS. Make sure your domain has a valid SSL certificate.

- **Content-Type:** The file should be served as `application/json`. Most servers do this automatically for `.json` files.

- **Case sensitivity:** Usernames in NIP-05 are case-insensitive per the spec. Clients should lowercase the username before looking it up.

## NIP-05 error messages in nostr-cli

nostr-cli provides helpful error messages when NIP-05 resolution fails:

### No nostr.json found

```
Error: NIP-05 lookup failed for user@domain.com

  No .well-known/nostr.json found at domain.com

  To set up NIP-05 verification, add a nostr.json file at:
    https://domain.com/.well-known/nostr.json

  More info: https://nostrcli.sh/nip05
```

### User not found in nostr.json

```
Error: NIP-05 lookup failed for user@domain.com

  User "user" not found in domain.com/.well-known/nostr.json

  Add this entry to your nostr.json:
    "user": "<your-npub-hex>"

  Generate with: nostr generate nip05 --address user@domain.com
  More info: https://nostrcli.sh/nip05
```

### Invalid JSON

```
Error: NIP-05 lookup failed for user@domain.com

  Invalid JSON at domain.com/.well-known/nostr.json

  More info: https://nostrcli.sh/nip05
```

## Resources

- [NIP-05 specification](https://github.com/nostr-protocol/nips/blob/master/05.md)
- [nostr-cli documentation](https://nostrcli.sh)
- [Source code on GitHub](https://github.com/xdamman/nostr-cli)
