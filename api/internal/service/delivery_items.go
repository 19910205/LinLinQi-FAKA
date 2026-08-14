package service

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/google/uuid"

	"linlinqi/api/internal/model"
	"linlinqi/api/internal/security"
)

var deliveryItemsContext = []byte("supplier-delivery-items")

func deliveryItemsAssociatedData(itemID uuid.UUID) []byte {
	associated := make([]byte, 0, len(itemID)+len(deliveryItemsContext))
	associated = append(associated, itemID[:]...)
	return append(associated, deliveryItemsContext...)
}

// EncryptDeliveryItems preserves the boundary between individual supplier
// deliveries. CardCiphertext remains the human-readable joined representation,
// while this encrypted JSON array is used for supplier-to-supplier callbacks.
func EncryptDeliveryItems(vault *security.Vault, itemID uuid.UUID, values []string) ([]byte, []byte, error) {
	encoded, err := json.Marshal(values)
	if err != nil {
		return nil, nil, err
	}
	ciphertext, nonce, _, err := vault.Encrypt(string(encoded), deliveryItemsAssociatedData(itemID))
	return ciphertext, nonce, err
}

func DecryptDeliveryItems(vault *security.Vault, item model.OrderItem) ([]string, error) {
	if len(item.DeliveryItemsCipher) > 0 || len(item.DeliveryItemsNonce) > 0 {
		if len(item.DeliveryItemsCipher) == 0 || len(item.DeliveryItemsNonce) == 0 {
			return nil, errors.New("incomplete encrypted delivery item payload")
		}
		plaintext, err := vault.Decrypt(item.DeliveryItemsCipher, item.DeliveryItemsNonce, deliveryItemsAssociatedData(item.ID))
		if err != nil {
			return nil, err
		}
		var values []string
		if err := json.Unmarshal([]byte(plaintext), &values); err != nil {
			return nil, err
		}
		if len(values) != item.Quantity {
			return nil, errors.New("encrypted delivery item quantity mismatch")
		}
		return values, nil
	}
	if len(item.CardCiphertext) == 0 || len(item.CardNonce) == 0 {
		return nil, nil
	}
	content, err := vault.Decrypt(item.CardCiphertext, item.CardNonce, item.ProductID[:])
	if err != nil {
		return nil, err
	}
	if item.Quantity <= 1 {
		return []string{content}, nil
	}
	// Legacy supplier orders stored joined text before delivery boundaries were
	// introduced. Split only when the count is unambiguous; otherwise polling
	// remains available and no guessed card boundary is emitted.
	parts := strings.Split(content, "\n")
	if len(parts) == item.Quantity {
		return parts, nil
	}
	return nil, errors.New("legacy delivery item boundaries are ambiguous")
}
