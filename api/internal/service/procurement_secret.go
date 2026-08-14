package service

import (
	"github.com/google/uuid"

	"linlinqi/api/internal/security"
)

var procurementCallbackSecretContext = []byte("supplier-callback-secret")

func procurementCallbackSecretAssociatedData(procurementID uuid.UUID) []byte {
	associated := make([]byte, 0, len(procurementID)+len(procurementCallbackSecretContext))
	associated = append(associated, procurementID[:]...)
	return append(associated, procurementCallbackSecretContext...)
}

// EncryptProcurementCallbackSecret snapshots the callback verification secret
// for an in-flight procurement, allowing supplier credentials to rotate
// without invalidating a callback that was already issued upstream.
func EncryptProcurementCallbackSecret(vault *security.Vault, procurementID uuid.UUID, secret string) ([]byte, []byte, error) {
	ciphertext, nonce, _, err := vault.Encrypt(secret, procurementCallbackSecretAssociatedData(procurementID))
	return ciphertext, nonce, err
}

func DecryptProcurementCallbackSecret(vault *security.Vault, procurementID uuid.UUID, ciphertext, nonce []byte) (string, error) {
	return vault.Decrypt(ciphertext, nonce, procurementCallbackSecretAssociatedData(procurementID))
}
