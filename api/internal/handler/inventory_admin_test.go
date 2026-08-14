package handler

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"linlinqi/api/internal/security"
)

func TestInventoryCardsImportRequestValidation(t *testing.T) {
	variantID := "dfc2b2d7-3a34-4ce6-a85b-50150510e95e"
	req := inventoryCardsImportRequest{
		ProductID: "8d02d80d-ea58-4425-bef0-593462408f7b",
		VariantID: &variantID,
		Cards:     []string{"secret"},
	}
	productID, parsedVariantID, err := req.normalizeAndValidate()
	if err != nil || productID.String() != req.ProductID || parsedVariantID == nil || parsedVariantID.String() != variantID {
		t.Fatalf("valid import request rejected: product=%v variant=%v err=%v", productID, parsedVariantID, err)
	}
	for _, invalid := range []inventoryCardsImportRequest{
		{ProductID: "not-a-uuid", Cards: []string{"secret"}},
		{ProductID: req.ProductID, Cards: nil},
		{ProductID: req.ProductID, Cards: make([]string, 5001)},
	} {
		if _, _, err := invalid.normalizeAndValidate(); err == nil {
			t.Fatalf("invalid import request accepted: product=%q cards=%d", invalid.ProductID, len(invalid.Cards))
		}
	}
}

func TestBuildEncryptedInventoryCardsDeduplicatesWithoutPersistingPlaintext(t *testing.T) {
	vault, err := security.NewVault("inventory-test-encryption-key-with-safe-length")
	if err != nil {
		t.Fatal(err)
	}
	productID := uuid.MustParse("8d02d80d-ea58-4425-bef0-593462408f7b")
	tooLong := strings.Repeat("x", 2001)
	items, invalid, err := buildEncryptedInventoryCards(vault, productID, nil, []string{" ABCD1234 ", "ABCD1234", "", tooLong})
	if err != nil {
		t.Fatalf("encrypt inventory cards: %v", err)
	}
	if len(items) != 1 || invalid != 3 {
		t.Fatalf("unexpected import preparation result: items=%d invalid=%d", len(items), invalid)
	}
	if len(items[0].EncryptedContent) == 0 || len(items[0].Nonce) == 0 || len(items[0].Fingerprint) == 0 || items[0].Status != "available" {
		t.Fatalf("encrypted card is incomplete: %#v", items[0])
	}
	raw, err := json.Marshal(items[0])
	if err != nil {
		t.Fatal(err)
	}
	serialized := string(raw)
	for _, forbidden := range []string{"ABCD1234", "encrypted_content", "fingerprint", "nonce"} {
		if strings.Contains(strings.ToLower(serialized), strings.ToLower(forbidden)) {
			t.Fatalf("card JSON leaked protected material %q: %s", forbidden, serialized)
		}
	}
}

func TestInventoryCardTransitionOnlyAllowsQuarantineCycle(t *testing.T) {
	allowed := [][2]string{{"available", "disabled"}, {"disabled", "available"}}
	for _, transition := range allowed {
		if !validInventoryCardTransition(transition[0], transition[1]) {
			t.Fatalf("safe transition rejected: %v", transition)
		}
	}
	for _, transition := range [][2]string{
		{"available", "available"}, {"locked", "disabled"}, {"locked", "available"},
		{"sold", "disabled"}, {"sold", "available"}, {"disabled", "sold"},
	} {
		if validInventoryCardTransition(transition[0], transition[1]) {
			t.Fatalf("unsafe transition accepted: %v", transition)
		}
	}
}

func TestInventoryBatchNumberIsOpaqueAndTimeScoped(t *testing.T) {
	now := time.Date(2026, 8, 9, 12, 30, 45, 0, time.UTC)
	first := inventoryBatchNo(now)
	second := inventoryBatchNo(now)
	pattern := regexp.MustCompile(`^LQIB20260809123045[A-F0-9]{8}$`)
	if !pattern.MatchString(first) || !pattern.MatchString(second) {
		t.Fatalf("unexpected inventory batch number: %q / %q", first, second)
	}
	if first == second {
		t.Fatalf("inventory batch numbers collided: %q", first)
	}
}
