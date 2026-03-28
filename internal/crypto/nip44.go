package crypto

import "github.com/nbd-wtf/go-nostr/nip44"

// NIP44Encrypt encrypts plaintext using NIP-44 (ChaCha20-Poly1305).
func NIP44Encrypt(plaintext, skHex, recipientPubHex string) (string, error) {
	convKey, err := nip44.GenerateConversationKey(recipientPubHex, skHex)
	if err != nil {
		return "", err
	}
	return nip44.Encrypt(plaintext, convKey)
}

// NIP44Decrypt decrypts NIP-44 ciphertext.
func NIP44Decrypt(ciphertext, skHex, senderPubHex string) (string, error) {
	convKey, err := nip44.GenerateConversationKey(senderPubHex, skHex)
	if err != nil {
		return "", err
	}
	return nip44.Decrypt(ciphertext, convKey)
}
