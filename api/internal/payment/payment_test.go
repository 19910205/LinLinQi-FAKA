package payment

import (
	"encoding/json"
	"testing"
)

func TestRefundRequestCarriesExplicitCurrency(t *testing.T) {
	payload, err := json.Marshal(RefundRequest{
		RefundNo: "LQR-1", ProviderTradeNo: "trade-1", Reason: "test", Amount: 1054, Currency: "CNY",
	})
	if err != nil {
		t.Fatalf("marshal refund request: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("decode refund request: %v", err)
	}
	if decoded["currency"] != "CNY" || decoded["amount"] != float64(1054) {
		t.Fatalf("refund money contract lost amount/currency: %s", payload)
	}
}
