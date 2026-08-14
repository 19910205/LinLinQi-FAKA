package handler

import (
	"strings"
	"testing"
	"time"
)

func TestGiftCardCodeGenerationAndNormalization(t *testing.T) {
	code, preview, err := generateGiftCardCode()
	if err != nil {
		t.Fatalf("generate gift card: %v", err)
	}
	if !strings.HasPrefix(code, "LLQ-") || !strings.Contains(preview, "••••••") {
		t.Fatalf("unexpected code or preview: %q %q", code, preview)
	}
	hash, err := giftCardHash(code)
	if err != nil {
		t.Fatalf("hash generated code: %v", err)
	}
	compact := strings.ReplaceAll(strings.ToLower(code), "-", "")
	spaced := compact[:8] + " \n " + compact[8:]
	hashAgain, err := giftCardHash(spaced)
	if err != nil || hashAgain != hash {
		t.Fatalf("human formatting should not change the code hash: %v", err)
	}
	if _, err := giftCardHash("LLQ-DEMO"); err == nil {
		t.Fatal("short or low-entropy gift card code was accepted")
	}
}

func TestGiftCardBatchValidation(t *testing.T) {
	now := time.Now().UTC()
	validExpiry := now.Add(24 * time.Hour)
	valid := issueGiftCardBatchRequest{Name: "企业客户礼赠", Quantity: 20, CardValue: 10_000, ExpiresAt: &validExpiry}
	if err := valid.normalizeAndValidate(now); err != nil {
		t.Fatalf("valid batch rejected: %v", err)
	}
	for name, request := range map[string]issueGiftCardBatchRequest{
		"empty name":      {Quantity: 1, CardValue: 1},
		"zero quantity":   {Name: "礼赠", Quantity: 0, CardValue: 1},
		"large quantity":  {Name: "礼赠", Quantity: 501, CardValue: 1},
		"zero value":      {Name: "礼赠", Quantity: 1, CardValue: 0},
		"expired":         {Name: "礼赠", Quantity: 1, CardValue: 1, ExpiresAt: &now},
		"excessive value": {Name: "礼赠", Quantity: 1, CardValue: 100_000_001},
	} {
		t.Run(name, func(t *testing.T) {
			if err := request.normalizeAndValidate(now); err == nil {
				t.Fatal("invalid batch was accepted")
			}
		})
	}
}
