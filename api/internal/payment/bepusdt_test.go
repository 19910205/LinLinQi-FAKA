package payment

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestBepusdtSignMatchesGatewayReference(t *testing.T) {
	// Reference vector computed with the same algorithm as
	// v03413/bepusdt app/utils/utils.go EpusdtSign.
	data := map[string]any{
		"order_id":     "LQI1700000000000000000",
		"amount":       100.5,
		"fiat":         "CNY",
		"trade_type":   "usdt.trc20",
		"name":         "LinLinQi 数字商品订单 A",
		"notify_url":   "https://pay.example.com/api/v1/payments/bepusdt/callback",
		"redirect_url": "https://shop.example.com/orders",
		"timeout":      900,
	}
	want := bepusdtSign(data, "test-token")
	if len(want) != 32 {
		t.Fatalf("signature length = %d, want 32 md5 hex chars", len(want))
	}
	// Empty values must be skipped and the signature field must be ignored.
	data["address"] = ""
	data["signature"] = "bogus"
	if got := bepusdtSign(data, "test-token"); got != want {
		t.Fatalf("empty/signature fields changed the signature: got %s want %s", got, want)
	}
}

func TestBepusdtVerifyCallbackRejectsBadSignature(t *testing.T) {
	driver := NewBepusdtDriver(BepusdtConfig{Code: "bepusdt", BaseURL: "http://127.0.0.1:1", APIToken: "secret", TradeType: "usdt.trc20", Fiat: "CNY", MinorUnit: 2})
	body := []byte(`{"trade_id":"T1","order_id":"LQI1","amount":100.5,"actual_amount":"14.58","token":"TAddr","block_transaction_id":"0xabc","status":2,"signature":"wrong"}`)
	if _, err := driver.VerifyCallback(nil, body); err == nil {
		t.Fatal("expected invalid signature error")
	}
}

func TestBepusdtVerifyCallbackAcceptsValidPayment(t *testing.T) {
	driver := NewBepusdtDriver(BepusdtConfig{Code: "bepusdt", BaseURL: "http://127.0.0.1:1", APIToken: "secret", TradeType: "usdt.trc20", Fiat: "CNY", MinorUnit: 2})
	raw := map[string]any{
		"trade_id":            "T1",
		"order_id":            "LQI1",
		"amount":              100.5,
		"actual_amount":       "14.58",
		"token":               "TAddr",
		"block_transaction_id": "0xabc",
		"status":              2,
	}
	raw["signature"] = bepusdtSign(raw, "secret")
	body, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}
	got, err := driver.VerifyCallback(nil, body)
	if err != nil {
		t.Fatalf("verify callback: %v", err)
	}
	if got.Status != "succeeded" || got.IntentNo != "LQI1" || got.ProviderTradeNo != "T1" || got.EventID != "0xabc" {
		t.Fatalf("unexpected callback result: %+v", got)
	}
	if got.Amount != 10050 || got.Currency != "CNY" {
		t.Fatalf("callback amount/currency = %d %s, want 10050 CNY", got.Amount, got.Currency)
	}
}

func TestBepusdtVerifyCallbackRejectsNonPaymentStatus(t *testing.T) {
	driver := NewBepusdtDriver(BepusdtConfig{Code: "bepusdt", BaseURL: "http://127.0.0.1:1", APIToken: "secret", TradeType: "usdt.trc20", Fiat: "CNY", MinorUnit: 2})
	raw := map[string]any{
		"trade_id": "T1", "order_id": "LQI1", "amount": 100.5, "actual_amount": "14.58",
		"token": "TAddr", "block_transaction_id": "0xabc", "status": 3,
	}
	raw["signature"] = bepusdtSign(raw, "secret")
	body, _ := json.Marshal(raw)
	if _, err := driver.VerifyCallback(nil, body); err == nil {
		t.Fatal("expected timeout callback to be rejected")
	}
}

func TestBepusdtRefundNotSupported(t *testing.T) {
	driver := NewBepusdtDriver(BepusdtConfig{Code: "bepusdt", BaseURL: "http://127.0.0.1:1", APIToken: "secret", TradeType: "usdt.trc20", Fiat: "CNY", MinorUnit: 2})
	if _, err := driver.Refund(context.Background(), RefundRequest{}); err != ErrRefundNotSupported {
		t.Fatalf("refund error = %v, want ErrRefundNotSupported", err)
	}
}

func TestBepusdtCreateParsesEnvelope(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/order/create-transaction" {
			t.Fatalf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status_code":200,"message":"success","data":{"trade_id":"TRADE1","order_id":"LQI1","status":1,"amount":100.5,"actual_amount":"14.58","token":"TAddr","expiration_time":600,"payment_url":"http://127.0.0.1/pay/checkout/TRADE1"},"request_id":""}`))
	}))
	defer server.Close()
	driver := NewBepusdtDriver(BepusdtConfig{Code: "bepusdt", BaseURL: server.URL, APIToken: "secret", TradeType: "usdt.trc20", Fiat: "CNY", MinorUnit: 2, Timeout: 600, AllowPrivate: true})
	result, err := driver.Create(context.Background(), CreateRequest{IntentNo: "LQI1", Amount: 10050, Currency: "CNY", Subject: "order", NotifyURL: "https://pay.example.com/callback", ReturnURL: "https://shop.example.com/orders"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if result.ProviderTradeNo != "TRADE1" || result.CheckoutURL != "http://127.0.0.1/pay/checkout/TRADE1" {
		t.Fatalf("unexpected create result: %+v", result)
	}
	if result.ExpiresAt.IsZero() || time.Until(result.ExpiresAt) > 11*time.Minute || time.Until(result.ExpiresAt) < 9*time.Minute {
		t.Fatalf("unexpected expiry: %v", result.ExpiresAt)
	}
}

func TestMinorMajorFloatRoundTrip(t *testing.T) {
	cases := []struct {
		minor     int64
		unit      int
		asFloat   float64
		backMinor int64
	}{
		{10050, 2, 100.5, 10050},
		{100, 2, 1, 100},
		{123456, 2, 1234.56, 123456},
		{150, 0, 150, 150},
	}
	for _, tc := range cases {
		if got := minorToMajorFloat(tc.minor, tc.unit); got != tc.asFloat {
			t.Fatalf("minorToMajor(%d,%d) = %v, want %v", tc.minor, tc.unit, got, tc.asFloat)
		}
		if got := majorToMinorFloat(tc.asFloat, tc.unit); got != tc.backMinor {
			t.Fatalf("majorToMinor(%v,%d) = %d, want %d", tc.asFloat, tc.unit, got, tc.backMinor)
		}
	}
}
