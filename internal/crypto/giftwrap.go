package crypto

import (
	"github.com/nbd-wtf/go-nostr"
	"github.com/nbd-wtf/go-nostr/nip44"
	"github.com/nbd-wtf/go-nostr/nip59"
)

// CreateGiftWrapDM creates a NIP-17 gift-wrapped DM.
// Returns TWO events: one for the recipient and one for yourself (sent copy).
func CreateGiftWrapDM(content, skHex, senderPubHex, recipientPubHex string) (forRecipient, forSelf nostr.Event, err error) {
	// Build the rumor (kind 14 = NIP-17 DM, unsigned)
	rumor := nostr.Event{
		Kind:      14,
		Content:   content,
		Tags:      nostr.Tags{{"p", recipientPubHex}},
		CreatedAt: nostr.Now(),
		PubKey:    senderPubHex,
	}

	// encrypt function: NIP-44 encrypt with sender's key → recipient
	encryptFor := func(targetPub string) func(string) (string, error) {
		return func(plaintext string) (string, error) {
			convKey, err := nip44.GenerateConversationKey(targetPub, skHex)
			if err != nil {
				return "", err
			}
			return nip44.Encrypt(plaintext, convKey)
		}
	}

	// sign function: sign with sender's secret key
	sign := func(ev *nostr.Event) error {
		return ev.Sign(skHex)
	}

	// Gift wrap for recipient
	forRecipient, err = nip59.GiftWrap(rumor, recipientPubHex, encryptFor(recipientPubHex), sign, nil)
	if err != nil {
		return
	}

	// Gift wrap a copy for sender
	forSelf, err = nip59.GiftWrap(rumor, senderPubHex, encryptFor(senderPubHex), sign, nil)
	if err != nil {
		return
	}

	return
}

// UnwrapGiftWrapDM unwraps a NIP-17 gift-wrapped event.
func UnwrapGiftWrapDM(event nostr.Event, skHex string) (rumor nostr.Event, err error) {
	return nip59.GiftUnwrap(event, func(otherpubkey, ciphertext string) (string, error) {
		convKey, err := nip44.GenerateConversationKey(otherpubkey, skHex)
		if err != nil {
			return "", err
		}
		return nip44.Decrypt(ciphertext, convKey)
	})
}
