package nip05

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Verify checks if a NIP-05 identifier resolves to the expected hex pubkey.
func Verify(nip05Addr string, expectedHex string) bool {
	hex, err := Resolve(nip05Addr)
	if err != nil {
		return false
	}
	return strings.EqualFold(hex, expectedHex)
}

// Resolve resolves a NIP-05 identifier to a hex pubkey.
func Resolve(nip05Addr string) (string, error) {
	var user, domain string
	if strings.Contains(nip05Addr, "@") {
		parts := strings.SplitN(nip05Addr, "@", 2)
		user = parts[0]
		domain = parts[1]
	} else {
		user = "_"
		domain = nip05Addr
	}
	if user == "" {
		user = "_"
	}

	url := fmt.Sprintf("https://%s/.well-known/nostr.json?name=%s", domain, user)
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return "", fmt.Errorf("Error: NIP-05 lookup failed for %s\n\n  No .well-known/nostr.json found at %s\n\n  To set up NIP-05 verification, add a nostr.json file at:\n    https://%s/.well-known/nostr.json\n\n  More info: https://nostrcli.sh/nip05", nip05Addr, domain, domain)
	}
	defer resp.Body.Close()

	if resp.StatusCode == 404 || resp.StatusCode != 200 {
		return "", fmt.Errorf("Error: NIP-05 lookup failed for %s\n\n  No .well-known/nostr.json found at %s\n\n  To set up NIP-05 verification, add a nostr.json file at:\n    https://%s/.well-known/nostr.json\n\n  More info: https://nostrcli.sh/nip05", nip05Addr, domain, domain)
	}

	var result struct {
		Names map[string]string `json:"names"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("Error: NIP-05 lookup failed for %s\n\n  Invalid JSON at %s/.well-known/nostr.json\n\n  More info: https://nostrcli.sh/nip05", nip05Addr, domain)
	}

	hex, ok := result.Names[user]
	if !ok {
		return "", fmt.Errorf("Error: NIP-05 lookup failed for %s\n\n  User %q not found in %s/.well-known/nostr.json\n\n  Add this entry to your nostr.json:\n    %q: \"<your-npub-hex>\"\n\n  Generate with: nostr generate nip05 --address %s\n  More info: https://nostrcli.sh/nip05", nip05Addr, user, domain, user, nip05Addr)
	}
	return hex, nil
}
