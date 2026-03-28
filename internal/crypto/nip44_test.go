package crypto

import (
	"testing"

	"github.com/nbd-wtf/go-nostr"
)

func TestNIP44_EncryptDecrypt(t *testing.T) {
	// Generate two keypairs
	sk1 := nostr.GeneratePrivateKey()
	pk1, err := nostr.GetPublicKey(sk1)
	if err != nil {
		t.Fatalf("GetPublicKey: %v", err)
	}

	sk2 := nostr.GeneratePrivateKey()
	pk2, err := nostr.GetPublicKey(sk2)
	if err != nil {
		t.Fatalf("GetPublicKey: %v", err)
	}

	plaintext := "Hello, NIP-44!"

	// Encrypt with sender's sk + recipient's pk
	ciphertext, err := NIP44Encrypt(plaintext, sk1, pk2)
	if err != nil {
		t.Fatalf("NIP44Encrypt: %v", err)
	}

	if ciphertext == plaintext {
		t.Fatal("ciphertext should not equal plaintext")
	}

	// Decrypt with recipient's sk + sender's pk
	decrypted, err := NIP44Decrypt(ciphertext, sk2, pk1)
	if err != nil {
		t.Fatalf("NIP44Decrypt: %v", err)
	}

	if decrypted != plaintext {
		t.Fatalf("got %q, want %q", decrypted, plaintext)
	}
}

func TestNIP44_DifferentCiphertexts(t *testing.T) {
	sk1 := nostr.GeneratePrivateKey()
	sk2 := nostr.GeneratePrivateKey()
	pk2, _ := nostr.GetPublicKey(sk2)

	plaintext := "test message"
	ct1, err := NIP44Encrypt(plaintext, sk1, pk2)
	if err != nil {
		t.Fatal(err)
	}
	ct2, err := NIP44Encrypt(plaintext, sk1, pk2)
	if err != nil {
		t.Fatal(err)
	}

	// Different random nonces → different ciphertexts
	if ct1 == ct2 {
		t.Fatal("two encryptions of the same plaintext should produce different ciphertexts (random nonce)")
	}
}
