package profile

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/nbd-wtf/go-nostr"
	"github.com/xdamman/nostr-cli/internal/cache"
	"github.com/xdamman/nostr-cli/internal/config"
	"github.com/xdamman/nostr-cli/internal/crypto"
	"github.com/xdamman/nostr-cli/internal/relay"
)

// Metadata represents kind 0 profile metadata.
type Metadata struct {
	Name        string `json:"name,omitempty"`
	DisplayName string `json:"display_name,omitempty"`
	About       string `json:"about,omitempty"`
	Picture     string `json:"picture,omitempty"`
	NIP05       string `json:"nip05,omitempty"`
	Banner      string `json:"banner,omitempty"`
	Website     string `json:"website,omitempty"`
	LUD16       string `json:"lud16,omitempty"`
}

// LoadCached loads the cached profile.json for the given npub.
func LoadCached(npub string) (*Metadata, error) {
	dir, err := config.ProfileDir(npub)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(filepath.Join(dir, "profile.json"))
	if err != nil {
		return nil, err
	}
	var m Metadata
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("invalid profile.json: %w", err)
	}
	return &m, nil
}

// LoadCachedWithTime is like LoadCached but also used for cache freshness checks.
func LoadCachedWithTime(npub string) (*Metadata, error) {
	return LoadCached(npub)
}

// IsCacheFresh returns true if the profile.json was modified less than 1 hour ago.
func IsCacheFresh(npub string) bool {
	dir, err := config.ProfileDir(npub)
	if err != nil {
		return false
	}
	info, err := os.Stat(filepath.Join(dir, "profile.json"))
	if err != nil {
		return false
	}
	return time.Since(info.ModTime()) < time.Hour
}

// SaveCached writes the profile metadata to profile.json.
// Only writes if the profile directory already exists (i.e., has an nsec file).
func SaveCached(npub string, m *Metadata) error {
	if !config.HasNsec(npub) {
		return nil // Don't create profile dirs for non-local profiles
	}
	dir, err := config.ProfileDir(npub)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, "profile.json"), data, 0644)
}

// FetchFromRelays fetches kind 0 metadata for the given npub from relays.
func FetchFromRelays(ctx context.Context, npub string, relayURLs []string) (*Metadata, error) {
	pubHex, err := crypto.NpubToHex(npub)
	if err != nil {
		return nil, err
	}

	filter := nostr.Filter{
		Authors: []string{pubHex},
		Kinds:   []int{nostr.KindProfileMetadata},
		Limit:   1,
	}

	event, err := relay.FetchEvent(ctx, filter, relayURLs)
	if err != nil {
		return nil, err
	}
	if event == nil {
		return nil, nil
	}

	// Cache the fetched event
	_ = cache.LogEvent(npub, *event)

	var m Metadata
	if err := json.Unmarshal([]byte(event.Content), &m); err != nil {
		return nil, fmt.Errorf("invalid kind 0 content: %w", err)
	}
	return &m, nil
}

// PublishMetadata signs and publishes kind 0 metadata to relays.
func PublishMetadata(ctx context.Context, npub string, m *Metadata, relayURLs []string) error {
	nsec, err := config.LoadNsec(npub)
	if err != nil {
		return err
	}
	skHex, err := crypto.NsecToHex(nsec)
	if err != nil {
		return err
	}
	pubHex, err := crypto.NpubToHex(npub)
	if err != nil {
		return err
	}

	content, err := json.Marshal(m)
	if err != nil {
		return err
	}

	event := nostr.Event{
		PubKey:    pubHex,
		CreatedAt: nostr.Now(),
		Kind:      nostr.KindProfileMetadata,
		Tags:      nostr.Tags{},
		Content:   string(content),
	}
	if err := event.Sign(skHex); err != nil {
		return fmt.Errorf("failed to sign event: %w", err)
	}

	if err := relay.PublishEvent(ctx, event, relayURLs); err != nil {
		return err
	}

	// Cache the published event
	_ = cache.LogEvent(npub, event)
	return nil
}
