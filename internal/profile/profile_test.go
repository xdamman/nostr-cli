package profile_test

import (
	"context"
	"testing"

	"github.com/xdamman/nostr-cli/internal/config"
	"github.com/xdamman/nostr-cli/internal/profile"
)

// TestFetchKnownProfile verifies that a known profile can be fetched from
// default relays. This is an integration test that hits real relays.
func TestFetchKnownProfile(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	npub := "npub1xsp9fcq340dzaqjctjl7unu3k0c82jdxc350uqym70k8vedzuvdst562dr"
	relays := config.DefaultRelays()
	if len(relays) == 0 {
		t.Fatal("default relays list is empty")
	}

	ctx := context.Background()
	meta, err := profile.FetchFromRelays(ctx, npub, relays)
	if err != nil {
		t.Fatalf("FetchFromRelays returned error: %v", err)
	}
	if meta == nil {
		t.Fatal("FetchFromRelays returned nil metadata — profile not found on any default relay")
	}
	if meta.Name == "" && meta.DisplayName == "" {
		t.Error("profile was fetched but has no name or display_name")
	}
	t.Logf("Fetched profile: name=%q display_name=%q", meta.Name, meta.DisplayName)
}

// TestFetchKnownProfileFromSpecificRelays tests fetching from relays where
// the profile is known to be published. Passes if at least one relay returns it.
func TestFetchKnownProfileFromSpecificRelays(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	npub := "npub1xsp9fcq340dzaqjctjl7unu3k0c82jdxc350uqym70k8vedzuvdst562dr"
	relayURLs := []string{
		"wss://nos.lol",
		"wss://purplepag.es",
	}

	found := 0
	for _, relayURL := range relayURLs {
		ctx := context.Background()
		meta, err := profile.FetchFromRelays(ctx, npub, []string{relayURL})
		if err != nil {
			t.Logf("warning: %s returned error: %v", relayURL, err)
			continue
		}
		if meta == nil {
			t.Logf("warning: profile not found on %s", relayURL)
			continue
		}
		t.Logf("Found on %s: name=%q", relayURL, meta.Name)
		found++
	}
	if found == 0 {
		t.Fatal("profile not found on any of the expected relays")
	}
}
