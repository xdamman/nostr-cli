package crypto

import (
	"fmt"

	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip19"
)

// GenerateKeyPair generates a new Nostr keypair, returning (nsec, npub, hex pubkey).
func GenerateKeyPair() (nsec, npub, pubHex string, err error) {
	sk := nostr.GeneratePrivateKey()
	nsec, err = nip19.EncodePrivateKey(sk)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to encode nsec: %w", err)
	}
	pk, err := nostr.GetPublicKey(sk)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to derive public key: %w", err)
	}
	npub, err = nip19.EncodePublicKey(pk)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to encode npub: %w", err)
	}
	return nsec, npub, pk, nil
}

// NsecToKeys derives npub and hex pubkey from an nsec.
func NsecToKeys(nsec string) (npub, pubHex, skHex string, err error) {
	prefix, value, err := nip19.Decode(nsec)
	if err != nil {
		return "", "", "", fmt.Errorf("invalid nsec: %w", err)
	}
	if prefix != "nsec" {
		return "", "", "", fmt.Errorf("expected nsec, got %s", prefix)
	}
	skHex = value.(string)
	pubHex, err = nostr.GetPublicKey(skHex)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to derive public key: %w", err)
	}
	npub, err = nip19.EncodePublicKey(pubHex)
	if err != nil {
		return "", "", "", fmt.Errorf("failed to encode npub: %w", err)
	}
	return npub, pubHex, skHex, nil
}

// NsecToHex converts an nsec to its hex private key.
func NsecToHex(nsec string) (string, error) {
	_, _, skHex, err := NsecToKeys(nsec)
	return skHex, err
}

// NpubToHex converts an npub to its hex public key.
func NpubToHex(npub string) (string, error) {
	prefix, value, err := nip19.Decode(npub)
	if err != nil {
		return "", fmt.Errorf("invalid npub: %w", err)
	}
	if prefix != "npub" {
		return "", fmt.Errorf("expected npub, got %s", prefix)
	}
	return value.(string), nil
}
