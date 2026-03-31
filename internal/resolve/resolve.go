package resolve

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/nbd-wtf/go-nostr/nip19"
	"github.com/xdamman/nostr-cli/internal/config"
	"github.com/xdamman/nostr-cli/internal/crypto"
)

// Resolve resolves user input to a hex pubkey.
// Order: alias lookup → npub/hex detection → NIP-05 resolution.
// npub is the profile whose aliases to search.
func Resolve(npub string, input string) (string, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", fmt.Errorf("empty user input")
	}

	// Strip leading @ if present
	input = strings.TrimPrefix(input, "@")

	// 1. Try alias lookup for the given profile
	if resolved, err := config.ResolveAliasFor(npub, input); err == nil {
		return crypto.NpubToHex(resolved)
	}

	// 2. Try npub
	if strings.HasPrefix(input, "npub1") {
		return crypto.NpubToHex(input)
	}

	// 3. Try hex pubkey (64 hex chars)
	if len(input) == 64 && isHex(input) {
		return input, nil
	}

	// 4. Try NIP-05
	if strings.Contains(input, "@") || strings.Contains(input, ".") {
		hex, err := resolveNIP05(input)
		if err == nil {
			return hex, nil
		}
	}

	// If the input looks like a plain name (not npub, hex, or NIP-05),
	// show existing aliases and hint how to create one.
	if !strings.HasPrefix(input, "npub1") && !strings.Contains(input, "@") && !strings.Contains(input, ".") {
		aliases, _ := config.LoadGlobalAliases()
		if len(aliases) > 0 {
			names := make([]string, 0, len(aliases))
			for name := range aliases {
				names = append(names, name)
			}
			return "", fmt.Errorf("no alias found for %q. Existing aliases: %s\n  To add an alias: nostr alias %s <npub>", input, strings.Join(names, ", "), input)
		}
		return "", fmt.Errorf("no alias found for %q. You have no aliases yet.\n  To add an alias: nostr alias %s <npub>", input, input)
	}
	return "", fmt.Errorf("could not resolve %q to a pubkey", input)
}

// ResolveToNpub resolves input to an npub string.
func ResolveToNpub(activeNpub string, input string) (string, error) {
	// If already npub format, return as-is
	input = strings.TrimPrefix(strings.TrimSpace(input), "@")
	if strings.HasPrefix(input, "npub1") {
		return input, nil
	}
	hex, err := Resolve(activeNpub, input)
	if err != nil {
		return "", err
	}
	result, err := nip19.EncodePublicKey(hex)
	if err != nil {
		return "", err
	}
	return result, nil
}

func resolveNIP05(input string) (string, error) {
	var user, domain string
	if strings.Contains(input, "@") {
		parts := strings.SplitN(input, "@", 2)
		user = parts[0]
		domain = parts[1]
	} else {
		// bare domain → _@domain
		user = "_"
		domain = input
	}

	if user == "" {
		user = "_"
	}

	url := fmt.Sprintf("https://%s/.well-known/nostr.json?name=%s", domain, user)
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return "", fmt.Errorf("Error: NIP-05 lookup failed for %s\n\n  No .well-known/nostr.json found at %s\n\n  To set up NIP-05 verification, add a nostr.json file at:\n    https://%s/.well-known/nostr.json\n\n  More info: https://nostrcli.sh/nip05", input, domain, domain)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 404 || resp.StatusCode != 200 {
		return "", fmt.Errorf("Error: NIP-05 lookup failed for %s\n\n  No .well-known/nostr.json found at %s\n\n  To set up NIP-05 verification, add a nostr.json file at:\n    https://%s/.well-known/nostr.json\n\n  More info: https://nostrcli.sh/nip05", input, domain, domain)
	}

	var result struct {
		Names map[string]string `json:"names"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("Error: NIP-05 lookup failed for %s\n\n  Invalid JSON at %s/.well-known/nostr.json\n\n  More info: https://nostrcli.sh/nip05", input, domain)
	}

	hex, ok := result.Names[user]
	if !ok {
		return "", fmt.Errorf("Error: NIP-05 lookup failed for %s\n\n  User %q not found in %s/.well-known/nostr.json\n\n  Add this entry to your nostr.json:\n    %q: \"<your-npub-hex>\"\n\n  Generate with: nostr generate nip05 --address %s\n  More info: https://nostrcli.sh/nip05", input, user, domain, user, input)
	}
	return hex, nil
}

func isHex(s string) bool {
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

// LoadAliases reads aliases for the given profile.
func LoadAliases(npub string) (map[string]string, error) {
	return config.LoadAliases(npub)
}
