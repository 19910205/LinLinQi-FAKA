package service

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"

	"linlinqi/api/internal/model"
	"linlinqi/api/internal/security"
)

func TestPostgresOrderInputValuesAreRequiredEncryptedAndIdempotent(t *testing.T) {
	db := orderPostgresTestDB(t)
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatalf("begin transaction: %v", tx.Error)
	}
	defer tx.Rollback()
	vault, err := security.NewVault("linlinqi-input-fields-integration-secret")
	if err != nil {
		t.Fatalf("create vault: %v", err)
	}
	category := model.Category{Name: "Input Test", Slug: "input-test-" + uuid.NewString(), Enabled: true}
	if err := tx.Create(&category).Error; err != nil {
		t.Fatalf("create category: %v", err)
	}
	product := model.Product{CategoryID: category.ID, Name: "Input Product", Slug: "input-product-" + uuid.NewString(), Price: 100, DeliveryType: "auto", InventoryMode: "local", Status: "on_sale"}
	if err := tx.Create(&product).Error; err != nil {
		t.Fatalf("create product: %v", err)
	}
	field := model.ProductInputField{ProductID: product.ID, Key: "account_id", Label: "Account ID", InputType: "text", Required: true, Sensitive: true, PassToSupplier: true, Options: json.RawMessage(`[]`), MinLength: 4, MaxLength: 40, Enabled: true}
	if err := tx.Create(&field).Error; err != nil {
		t.Fatalf("create field: %v", err)
	}
	cardPlaintext := "CARD-INPUT-INTEGRATION"
	cardCipher, cardNonce, fingerprint, err := vault.Encrypt(cardPlaintext, product.ID[:])
	if err != nil {
		t.Fatalf("encrypt card: %v", err)
	}
	card := model.Card{ProductID: product.ID, EncryptedContent: cardCipher, Nonce: cardNonce, Fingerprint: fingerprint, Preview: security.SecretPreview(cardPlaintext), Status: "available"}
	if err := tx.Create(&card).Error; err != nil {
		t.Fatalf("create card: %v", err)
	}
	baseInput := CreateOrderInput{ProductID: product.ID, Quantity: 1, Email: "buyer@example.com", PaymentMethod: "sandbox"}
	if _, err := CreateOrder(tx, vault, baseInput); !errors.Is(err, ErrInputValueRequired) {
		t.Fatalf("missing required value was not rejected: %v", err)
	}
	credentialID := uuid.New()
	externalOrderNo := "INPUT-" + uuid.NewString()
	plaintext := "account-938475"
	baseInput.APICredentialID = &credentialID
	baseInput.ExternalOrderNo = &externalOrderNo
	baseInput.InputValues = []SubmittedInputValue{{ProductID: product.ID, FieldID: field.ID, Value: plaintext}}
	order, err := CreateOrder(tx, vault, baseInput)
	if err != nil {
		t.Fatalf("create order: %v", err)
	}
	var stored model.OrderInputValue
	if err := tx.Where("order_id = ?", order.ID).First(&stored).Error; err != nil {
		t.Fatalf("load stored input: %v", err)
	}
	if bytes.Contains(stored.ValueCipher, []byte(plaintext)) || stored.ValuePreview == plaintext {
		t.Fatal("plaintext checkout value was persisted")
	}
	revealed, err := RevealOrderInputValues(tx, vault, order.ID, false)
	if err != nil || len(revealed) != 1 || revealed[0].Value != plaintext {
		t.Fatalf("reveal mismatch: values=%#v err=%v", revealed, err)
	}
	parameters, err := SupplierOrderParameters(tx, vault, order.ID, product.ID, nil)
	if err != nil || parameters[field.Key] != plaintext {
		t.Fatalf("supplier parameter mismatch: values=%#v err=%v", parameters, err)
	}
	replayed, err := CreateOrder(tx, vault, baseInput)
	if err != nil || replayed.ID != order.ID {
		t.Fatalf("exact idempotent replay failed: order=%#v err=%v", replayed, err)
	}
	if err := tx.Model(&field).Updates(map[string]any{
		"input_type": "select", "options": json.RawMessage(`["replacement-only"]`),
		"validation_pattern": "replacement-only", "min_length": 16, "max_length": 16,
	}).Error; err != nil {
		t.Fatalf("mutate current field definition: %v", err)
	}
	replayedAfterSchemaChange, err := CreateOrder(tx, vault, baseInput)
	if err != nil || replayedAfterSchemaChange.ID != order.ID {
		t.Fatalf("field definition change broke historical idempotency: order=%#v err=%v", replayedAfterSchemaChange, err)
	}
	conflicting := baseInput
	conflicting.InputValues = []SubmittedInputValue{{ProductID: product.ID, FieldID: field.ID, Value: "different-account"}}
	if _, err := CreateOrder(tx, vault, conflicting); !errors.Is(err, ErrOrderIdempotencyConflict) {
		t.Fatalf("conflicting idempotent replay accepted: %v", err)
	}
}
