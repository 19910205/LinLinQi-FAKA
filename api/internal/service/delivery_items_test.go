package service

import (
	"reflect"
	"testing"

	"github.com/google/uuid"

	"linlinqi/api/internal/model"
	"linlinqi/api/internal/security"
)

func TestEncryptedDeliveryItemsPreserveBoundaries(t *testing.T) {
	vault, err := security.NewVault("supplier-delivery-items-unit-encryption-secret")
	if err != nil {
		t.Fatalf("create vault: %v", err)
	}
	item := model.OrderItem{Base: model.Base{ID: uuid.New()}, ProductID: uuid.New(), Quantity: 2}
	want := []string{"CARD-1", "account=user@example.com\npassword=temporary"}
	item.DeliveryItemsCipher, item.DeliveryItemsNonce, err = EncryptDeliveryItems(vault, item.ID, want)
	if err != nil {
		t.Fatalf("encrypt deliveries: %v", err)
	}
	got, err := DecryptDeliveryItems(vault, item)
	if err != nil {
		t.Fatalf("decrypt deliveries: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("delivery boundaries changed: got %#v want %#v", got, want)
	}
	item.Quantity = 1
	if _, err := DecryptDeliveryItems(vault, item); err == nil {
		t.Fatal("delivery quantity mismatch accepted")
	}
}
