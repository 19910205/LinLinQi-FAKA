package service

import (
	"encoding/json"
	"errors"
	"net/mail"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"linlinqi/api/internal/model"
	"linlinqi/api/internal/security"
)

var (
	ErrInputValuesInvalid = errors.New("checkout input values are invalid")
	ErrInputValueRequired = errors.New("required checkout input value is missing")
	numberInputPattern    = regexp.MustCompile(`^-?(?:0|[1-9][0-9]*)(?:\.[0-9]{1,8})?$`)
)

type SubmittedInputValue struct {
	ProductID uuid.UUID
	VariantID *uuid.UUID
	FieldID   uuid.UUID
	Value     string
}

type RevealedOrderInputValue struct {
	ID             uuid.UUID  `json:"id"`
	ProductID      uuid.UUID  `json:"product_id"`
	VariantID      *uuid.UUID `json:"variant_id,omitempty"`
	FieldID        *uuid.UUID `json:"field_id,omitempty"`
	Key            string     `json:"key"`
	Label          string     `json:"label"`
	InputType      string     `json:"input_type"`
	Sensitive      bool       `json:"sensitive"`
	PassToSupplier bool       `json:"pass_to_supplier"`
	Value          string     `json:"value"`
	ValuePreview   string     `json:"value_preview"`
}

func inputLineKey(productID uuid.UUID, variantID *uuid.UUID) string {
	key := productID.String() + ":"
	if variantID != nil {
		key += variantID.String()
	}
	return key
}

func normalizeSubmittedValue(field model.ProductInputField, raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		if field.Required {
			return "", ErrInputValueRequired
		}
		return "", nil
	}
	for _, character := range value {
		if character == '\x00' || (unicode.IsControl(character) && character != '\n' && character != '\r' && character != '\t') {
			return "", ErrInputValuesInvalid
		}
		if field.InputType != "textarea" && unicode.IsControl(character) {
			return "", ErrInputValuesInvalid
		}
	}
	length := utf8.RuneCountInString(value)
	if length < field.MinLength || field.MaxLength < 1 || length > field.MaxLength || length > 2000 {
		return "", ErrInputValuesInvalid
	}
	switch field.InputType {
	case "text", "textarea":
	case "email":
		address, err := mail.ParseAddress(value)
		if err != nil || !strings.EqualFold(address.Address, value) || length > 190 {
			return "", ErrInputValuesInvalid
		}
		value = strings.ToLower(value)
	case "number":
		if length > 64 || !numberInputPattern.MatchString(value) {
			return "", ErrInputValuesInvalid
		}
	case "select":
		var options []string
		if json.Unmarshal(field.Options, &options) != nil {
			return "", ErrInputValuesInvalid
		}
		matched := false
		for _, option := range options {
			if value == option {
				matched = true
				break
			}
		}
		if !matched {
			return "", ErrInputValuesInvalid
		}
	default:
		return "", ErrInputValuesInvalid
	}
	if field.ValidationPattern != "" {
		pattern, err := regexp.Compile("^(?:" + field.ValidationPattern + ")$")
		if err != nil || !pattern.MatchString(value) {
			return "", ErrInputValuesInvalid
		}
	}
	return value, nil
}

func orderInputAssociatedData(id uuid.UUID) []byte {
	return append(id[:], []byte("order-input-value")...)
}

func maskedInputPreview(value string) string {
	if value == "" {
		return ""
	}
	return security.SecretPreview(value)
}

// PersistOrderInputValues validates the complete set of enabled fields for
// every distinct order line. It rejects missing, duplicate and extra fields,
// then encrypts all accepted values inside the caller's order transaction.
func PersistOrderInputValues(tx *gorm.DB, vault *security.Vault, orderID uuid.UUID, lines []OrderLine, submitted []SubmittedInputValue) error {
	lineSet := make(map[string]OrderLine, len(lines))
	productIDs := make([]uuid.UUID, 0, len(lines))
	productSeen := make(map[uuid.UUID]struct{}, len(lines))
	for _, line := range lines {
		key := inputLineKey(line.ProductID, line.VariantID)
		lineSet[key] = line
		if _, exists := productSeen[line.ProductID]; !exists {
			productSeen[line.ProductID] = struct{}{}
			productIDs = append(productIDs, line.ProductID)
		}
	}
	var fields []model.ProductInputField
	if len(productIDs) > 0 {
		if err := tx.Where("product_id IN ? AND enabled = ?", productIDs, true).Order("sort DESC, created_at ASC").Find(&fields).Error; err != nil {
			return err
		}
	}
	fieldsByProduct := make(map[uuid.UUID][]model.ProductInputField, len(productIDs))
	fieldByID := make(map[uuid.UUID]model.ProductInputField, len(fields))
	for _, field := range fields {
		fieldsByProduct[field.ProductID] = append(fieldsByProduct[field.ProductID], field)
		fieldByID[field.ID] = field
	}
	if len(submitted) > len(lineSet)*20 {
		return ErrInputValuesInvalid
	}
	type acceptedValue struct {
		line  OrderLine
		field model.ProductInputField
		value string
	}
	accepted := make([]acceptedValue, 0, len(submitted))
	seen := make(map[string]struct{}, len(submitted))
	for _, input := range submitted {
		lineKey := inputLineKey(input.ProductID, input.VariantID)
		line, exists := lineSet[lineKey]
		field, fieldExists := fieldByID[input.FieldID]
		if !exists || !fieldExists || field.ProductID != input.ProductID {
			return ErrInputValuesInvalid
		}
		key := lineKey + ":" + field.ID.String()
		if _, duplicate := seen[key]; duplicate {
			return ErrInputValuesInvalid
		}
		seen[key] = struct{}{}
		value, err := normalizeSubmittedValue(field, input.Value)
		if err != nil {
			return err
		}
		if value != "" {
			accepted = append(accepted, acceptedValue{line: line, field: field, value: value})
		}
	}
	for lineKey, line := range lineSet {
		for _, field := range fieldsByProduct[line.ProductID] {
			if field.Required {
				if _, exists := seen[lineKey+":"+field.ID.String()]; !exists {
					return ErrInputValueRequired
				}
			}
		}
	}
	for _, entry := range accepted {
		fieldID := entry.field.ID
		item := model.OrderInputValue{
			Base: model.Base{ID: uuid.New()}, OrderID: orderID, ProductID: entry.line.ProductID,
			VariantID: entry.line.VariantID, ProductInputFieldID: &fieldID, Key: entry.field.Key,
			Label: entry.field.Label, InputType: entry.field.InputType, Sensitive: entry.field.Sensitive,
			PassToSupplier: entry.field.PassToSupplier, ValuePreview: maskedInputPreview(entry.value),
		}
		ciphertext, nonce, _, err := vault.Encrypt(entry.value, orderInputAssociatedData(item.ID))
		if err != nil {
			return err
		}
		item.ValueCipher, item.ValueNonce = ciphertext, nonce
		if err := tx.Create(&item).Error; err != nil {
			return err
		}
	}
	return nil
}

func RevealOrderInputValues(db *gorm.DB, vault *security.Vault, orderID uuid.UUID, supplierOnly bool) ([]RevealedOrderInputValue, error) {
	query := db.Where("order_id = ?", orderID)
	if supplierOnly {
		query = query.Where("pass_to_supplier = ?", true)
	}
	var items []model.OrderInputValue
	if err := query.Order("created_at ASC").Find(&items).Error; err != nil {
		return nil, err
	}
	result := make([]RevealedOrderInputValue, 0, len(items))
	for _, item := range items {
		value, err := vault.Decrypt(item.ValueCipher, item.ValueNonce, orderInputAssociatedData(item.ID))
		if err != nil {
			return nil, err
		}
		result = append(result, RevealedOrderInputValue{
			ID: item.ID, ProductID: item.ProductID, VariantID: item.VariantID,
			FieldID: item.ProductInputFieldID, Key: item.Key, Label: item.Label,
			InputType: item.InputType, Sensitive: item.Sensitive, PassToSupplier: item.PassToSupplier,
			Value: value, ValuePreview: item.ValuePreview,
		})
	}
	return result, nil
}

func MaskedOrderInputValues(db *gorm.DB, orderID uuid.UUID) ([]RevealedOrderInputValue, error) {
	var items []model.OrderInputValue
	if err := db.Where("order_id = ?", orderID).Order("created_at ASC").Find(&items).Error; err != nil {
		return nil, err
	}
	result := make([]RevealedOrderInputValue, 0, len(items))
	for _, item := range items {
		result = append(result, RevealedOrderInputValue{
			ID: item.ID, ProductID: item.ProductID, VariantID: item.VariantID,
			FieldID: item.ProductInputFieldID, Key: item.Key, Label: item.Label,
			InputType: item.InputType, Sensitive: item.Sensitive, PassToSupplier: item.PassToSupplier,
			ValuePreview: item.ValuePreview,
		})
	}
	return result, nil
}

func SupplierOrderParameters(db *gorm.DB, vault *security.Vault, orderID, productID uuid.UUID, variantID *uuid.UUID) (map[string]string, error) {
	values, err := RevealOrderInputValues(db, vault, orderID, true)
	if err != nil {
		return nil, err
	}
	parameters := make(map[string]string)
	for _, item := range values {
		if item.ProductID != productID || (item.VariantID == nil) != (variantID == nil) {
			continue
		}
		if variantID != nil && *item.VariantID != *variantID {
			continue
		}
		parameters[item.Key] = item.Value
	}
	return parameters, nil
}

func OrderInputValuesMatch(db *gorm.DB, vault *security.Vault, orderID uuid.UUID, submitted []SubmittedInputValue) bool {
	values, err := RevealOrderInputValues(db, vault, orderID, false)
	if err != nil {
		return false
	}
	// Idempotency is evaluated against the immutable order snapshot, not the
	// mutable current field definition. Otherwise changing a select list,
	// validation expression or input type after an order was accepted could
	// make an exact retry of that external order number fail spuriously.
	existing := make(map[string]RevealedOrderInputValue, len(values))
	for _, item := range values {
		if item.FieldID == nil {
			return false
		}
		key := inputValueIdentity(item.ProductID, item.VariantID, *item.FieldID)
		if _, duplicate := existing[key]; duplicate {
			return false
		}
		existing[key] = item
	}

	// Empty optional values are intentionally not stored. Retain an identity
	// lookup for those entries so a retry cannot smuggle an unrelated field ID
	// while still allowing definitions to be edited or soft-deleted later.
	fieldIDs := make([]uuid.UUID, 0, len(submitted))
	for _, item := range submitted {
		fieldIDs = append(fieldIDs, item.FieldID)
	}
	var fields []model.ProductInputField
	if len(fieldIDs) > 0 && db.Unscoped().Select("id", "product_id").Where("id IN ?", fieldIDs).Find(&fields).Error != nil {
		return false
	}
	fieldProducts := make(map[uuid.UUID]uuid.UUID, len(fields))
	for _, field := range fields {
		fieldProducts[field.ID] = field.ProductID
	}

	matched := make(map[string]struct{}, len(submitted))
	for _, item := range submitted {
		key := inputValueIdentity(item.ProductID, item.VariantID, item.FieldID)
		if _, duplicate := matched[key]; duplicate {
			return false
		}
		matched[key] = struct{}{}
		stored, exists := existing[key]
		if !exists {
			if strings.TrimSpace(item.Value) != "" || fieldProducts[item.FieldID] != item.ProductID {
				return false
			}
			continue
		}
		value, valid := normalizeSnapshotReplayValue(stored.InputType, item.Value)
		if !valid || value != stored.Value {
			return false
		}
	}
	for key := range existing {
		if _, ok := matched[key]; !ok {
			return false
		}
	}
	return true
}

func inputValueIdentity(productID uuid.UUID, variantID *uuid.UUID, fieldID uuid.UUID) string {
	return productID.String() + ":" + pointerUUIDString(variantID) + ":" + fieldID.String()
}

func normalizeSnapshotReplayValue(inputType, raw string) (string, bool) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", true
	}
	for _, character := range value {
		if character == '\x00' || (unicode.IsControl(character) && (inputType != "textarea" || (character != '\n' && character != '\r' && character != '\t'))) {
			return "", false
		}
	}
	if inputType == "email" {
		value = strings.ToLower(value)
	}
	return value, true
}

func pointerUUIDString(value *uuid.UUID) string {
	if value == nil {
		return ""
	}
	return value.String()
}
