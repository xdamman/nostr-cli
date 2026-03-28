package crypto

import (
	"testing"

	"github.com/nbd-wtf/go-nostr"
)

func generateTestKeypair(t *testing.T) (sk, pk string) {
	t.Helper()
	sk = nostr.GeneratePrivateKey()
	pk, err := nostr.GetPublicKey(sk)
	if err != nil {
		t.Fatalf("GetPublicKey: %v", err)
	}
	return
}

func TestGiftWrap_CreatesTwoEvents(t *testing.T) {
	sk1, pk1 := generateTestKeypair(t)
	_, pk2 := generateTestKeypair(t)

	forRecipient, forSelf, err := CreateGiftWrapDM("hello", sk1, pk1, pk2)
	if err != nil {
		t.Fatalf("CreateGiftWrapDM: %v", err)
	}

	// Both should be kind 1059
	if forRecipient.Kind != 1059 {
		t.Errorf("forRecipient.Kind = %d, want 1059", forRecipient.Kind)
	}
	if forSelf.Kind != 1059 {
		t.Errorf("forSelf.Kind = %d, want 1059", forSelf.Kind)
	}

	// forRecipient should have p tag = recipient
	recipientTag := ""
	for _, tag := range forRecipient.Tags {
		if len(tag) >= 2 && tag[0] == "p" {
			recipientTag = tag[1]
		}
	}
	if recipientTag != pk2 {
		t.Errorf("forRecipient p tag = %q, want %q", recipientTag, pk2)
	}

	// forSelf should have p tag = sender
	selfTag := ""
	for _, tag := range forSelf.Tags {
		if len(tag) >= 2 && tag[0] == "p" {
			selfTag = tag[1]
		}
	}
	if selfTag != pk1 {
		t.Errorf("forSelf p tag = %q, want %q", selfTag, pk1)
	}
}

func TestGiftWrap_Roundtrip(t *testing.T) {
	sk1, pk1 := generateTestKeypair(t)
	sk2, pk2 := generateTestKeypair(t)

	message := "secret message"
	forRecipient, _, err := CreateGiftWrapDM(message, sk1, pk1, pk2)
	if err != nil {
		t.Fatalf("CreateGiftWrapDM: %v", err)
	}

	// Unwrap with recipient's key
	rumor, err := UnwrapGiftWrapDM(forRecipient, sk2)
	if err != nil {
		t.Fatalf("UnwrapGiftWrapDM: %v", err)
	}

	if rumor.Kind != 14 {
		t.Errorf("rumor.Kind = %d, want 14", rumor.Kind)
	}
	if rumor.Content != message {
		t.Errorf("rumor.Content = %q, want %q", rumor.Content, message)
	}
	if rumor.PubKey != pk1 {
		t.Errorf("rumor.PubKey = %q, want sender %q", rumor.PubKey, pk1)
	}
}

func TestGiftWrap_SelfCopyRoundtrip(t *testing.T) {
	sk1, pk1 := generateTestKeypair(t)
	_, pk2 := generateTestKeypair(t)

	message := "secret message"
	_, forSelf, err := CreateGiftWrapDM(message, sk1, pk1, pk2)
	if err != nil {
		t.Fatalf("CreateGiftWrapDM: %v", err)
	}

	// Sender can unwrap their own copy
	rumor, err := UnwrapGiftWrapDM(forSelf, sk1)
	if err != nil {
		t.Fatalf("UnwrapGiftWrapDM (self): %v", err)
	}

	if rumor.Content != message {
		t.Errorf("self copy content = %q, want %q", rumor.Content, message)
	}
}

func TestGiftWrap_HidesSender(t *testing.T) {
	sk1, pk1 := generateTestKeypair(t)
	_, pk2 := generateTestKeypair(t)

	forRecipient, _, err := CreateGiftWrapDM("hello", sk1, pk1, pk2)
	if err != nil {
		t.Fatalf("CreateGiftWrapDM: %v", err)
	}

	// Outer event PubKey should be a random ephemeral key, not the sender
	if forRecipient.PubKey == pk1 {
		t.Error("outer event PubKey should not be the sender's pubkey (should be ephemeral)")
	}
}

func TestGiftWrap_WrongKeyFails(t *testing.T) {
	sk1, pk1 := generateTestKeypair(t)
	_, pk2 := generateTestKeypair(t)
	sk3, _ := generateTestKeypair(t) // unrelated third party

	forRecipient, _, err := CreateGiftWrapDM("hello", sk1, pk1, pk2)
	if err != nil {
		t.Fatalf("CreateGiftWrapDM: %v", err)
	}

	// Third party cannot unwrap
	_, err = UnwrapGiftWrapDM(forRecipient, sk3)
	if err == nil {
		t.Error("expected error when unwrapping with wrong key, got nil")
	}
}
