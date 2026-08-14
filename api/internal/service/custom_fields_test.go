package service

import (
	"encoding/json"
	"errors"
	"testing"

	"linlinqi/api/internal/model"
)

func testInputField(inputType string) model.ProductInputField {
	return model.ProductInputField{
		Key: "account_id", Label: "账号 ID", InputType: inputType,
		Required: true, MinLength: 2, MaxLength: 64, Options: json.RawMessage(`[]`),
	}
}

func TestNormalizeSubmittedValueValidatesTypesAndPatterns(t *testing.T) {
	email := testInputField("email")
	email.ValidationPattern = `[^@]+@example\.com`
	value, err := normalizeSubmittedValue(email, "  USER@example.com ")
	if err != nil || value != "user@example.com" {
		t.Fatalf("valid email rejected or not normalized: value=%q err=%v", value, err)
	}
	if _, err := normalizeSubmittedValue(email, "user@invalid.test"); !errors.Is(err, ErrInputValuesInvalid) {
		t.Fatalf("pattern mismatch accepted: %v", err)
	}

	number := testInputField("number")
	if value, err := normalizeSubmittedValue(number, "-12.50"); err != nil || value != "-12.50" {
		t.Fatalf("valid decimal rejected: value=%q err=%v", value, err)
	}
	if _, err := normalizeSubmittedValue(number, "1e9"); !errors.Is(err, ErrInputValuesInvalid) {
		t.Fatalf("scientific notation unexpectedly accepted: %v", err)
	}
}

func TestNormalizeSubmittedValueRequiresExactSelectOption(t *testing.T) {
	field := testInputField("select")
	field.Options = json.RawMessage(`["Asia","Europe"]`)
	if value, err := normalizeSubmittedValue(field, "Asia"); err != nil || value != "Asia" {
		t.Fatalf("valid option rejected: value=%q err=%v", value, err)
	}
	if _, err := normalizeSubmittedValue(field, "asia"); !errors.Is(err, ErrInputValuesInvalid) {
		t.Fatalf("case-changed option accepted: %v", err)
	}
	if _, err := normalizeSubmittedValue(field, "  "); !errors.Is(err, ErrInputValueRequired) {
		t.Fatalf("required empty option accepted: %v", err)
	}
}

func TestMaskedInputPreviewNeverStoresShortPlaintext(t *testing.T) {
	if preview := maskedInputPreview("12345678"); preview == "12345678" || preview == "" {
		t.Fatalf("short value leaked into preview: %q", preview)
	}
	if preview := maskedInputPreview("account-123456789"); preview == "account-123456789" {
		t.Fatalf("long value leaked into preview: %q", preview)
	}
}
