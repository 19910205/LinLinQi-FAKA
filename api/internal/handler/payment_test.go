package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"linlinqi/api/internal/model"
	"linlinqi/api/internal/payment"
)

func TestAdminPaymentChannelNeverSerializesConnectorSecrets(t *testing.T) {
	channel := model.PaymentChannel{
		Base:         model.Base{ID: uuid.New()},
		Name:         "主支付渠道",
		Code:         "primary_pay",
		Provider:     "signed_http",
		FeeRate:      38,
		Enabled:      true,
		ConfigCipher: []byte("encrypted-secret"),
		ConfigNonce:  []byte("nonce"),
	}
	payload, err := json.Marshal(toAdminPaymentChannel(channel))
	if err != nil {
		t.Fatalf("marshal admin payment channel: %v", err)
	}
	body := string(payload)
	for _, forbidden := range []string{"config_cipher", "config_nonce", "encrypted-secret", "merchant_id", "secret"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("admin payment response leaked %q: %s", forbidden, body)
		}
	}
	if !strings.Contains(body, `"configured":true`) {
		t.Fatalf("admin payment response did not expose safe configuration state: %s", body)
	}
}

func TestPaymentChannelCodeValidation(t *testing.T) {
	for _, value := range []string{"alipay", "wechat_pay", "bank-card-01"} {
		if !paymentChannelCodePattern.MatchString(value) {
			t.Fatalf("valid channel code rejected: %q", value)
		}
	}
	for _, value := range []string{"A", "UpperCase", "bad code", "_leading", strings.Repeat("x", 51)} {
		if paymentChannelCodePattern.MatchString(value) {
			t.Fatalf("invalid channel code accepted: %q", value)
		}
	}
}

func TestUpdatePaymentChannelRejectsUnknownFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest(http.MethodPatch, "/payments/1", strings.NewReader(`{"enabled":true,"provider":"attacker"}`))
	var request updatePaymentChannelRequest
	if err := decodeStrictJSON(context, &request); err == nil {
		t.Fatal("payment channel update accepted server-owned provider field")
	}
}

func TestPaymentCallbackEligibilityRequiresExactAmountAndLiveOrder(t *testing.T) {
	now := time.Now()
	order := model.Order{Status: "pending_payment", PaymentStatus: "pending"}
	intent := model.PaymentIntent{Amount: 1200, Currency: "USD", Status: "pending", ExpiresAt: now.Add(time.Minute)}
	callback := payment.CallbackResult{Amount: 1200, Currency: "USD", Status: "succeeded"}
	if !paymentCallbackCanFulfill(order, intent, callback, now) {
		t.Fatal("valid callback was not eligible for fulfillment")
	}
	callback.Amount = 1199
	if paymentCallbackCanFulfill(order, intent, callback, now) {
		t.Fatal("amount-mismatched callback was eligible for fulfillment")
	}
	if reason := automaticPaymentRefundReason(order, intent, callback); !strings.Contains(reason, "金额") {
		t.Fatalf("amount mismatch did not receive an explicit refund reason: %q", reason)
	}
	callback.Amount = intent.Amount
	callback.Currency = "CNY"
	if paymentCallbackCanFulfill(order, intent, callback, now) {
		t.Fatal("currency-mismatched callback was eligible for fulfillment")
	}
	if reason := automaticPaymentRefundReason(order, intent, callback); !strings.Contains(reason, "币种") {
		t.Fatalf("currency mismatch did not receive an explicit refund reason: %q", reason)
	}
	callback.Currency = intent.Currency
	order.Status = "completed"
	if paymentCallbackCanFulfill(order, intent, callback, now) {
		t.Fatal("terminal order was eligible for a second fulfillment")
	}
}

func TestDerivedPaymentEventIDIsBoundedAndDeterministic(t *testing.T) {
	callback := payment.CallbackResult{
		IntentNo: strings.Repeat("i", 80), ProviderTradeNo: strings.Repeat("t", 160), Status: "succeeded",
	}
	left, right := normalizedPaymentCallbackEventID(callback), normalizedPaymentCallbackEventID(callback)
	if left != right || len(left) > 80 {
		t.Fatalf("derived callback event ID is not stable and bounded: %q %q", left, right)
	}
}

func TestRechargeCallbackRequiresExactLiveReceiptBeforeWalletCredit(t *testing.T) {
	now := time.Now().UTC()
	recharge := model.RechargeOrder{
		Amount: 1000, Currency: "USD", Status: "pending", ProviderTradeNo: "provider-1",
		ExpiresAt: now.Add(time.Minute),
	}
	callback := payment.CallbackResult{Status: "succeeded", ProviderTradeNo: "provider-1", Amount: 1000, Currency: "USD"}
	if !rechargeCallbackCanCredit(recharge, callback, now) {
		t.Fatal("exact live recharge callback was not eligible for wallet credit")
	}
	callback.Amount = 999
	if rechargeCallbackCanCredit(recharge, callback, now) || !strings.Contains(rechargeCallbackRefundReason(recharge, callback, now), "金额") {
		t.Fatal("amount mismatch was not routed to an explicit refund")
	}
	callback.Amount = 1000
	callback.Currency = "CNY"
	if rechargeCallbackCanCredit(recharge, callback, now) || !strings.Contains(rechargeCallbackRefundReason(recharge, callback, now), "币种") {
		t.Fatal("currency mismatch was not routed to an explicit refund")
	}
	callback.Currency = "USD"
	if rechargeCallbackCanCredit(recharge, callback, now.Add(2*time.Minute)) {
		t.Fatal("late recharge callback was eligible for wallet credit")
	}
}

func TestRechargeCallbackEvidenceMinimizesVerifiedRawBody(t *testing.T) {
	callback := payment.CallbackResult{EventID: "evt-1", Status: "succeeded", Amount: 750, Currency: "CNY"}
	body := []byte(`{"event_id":"evt-1","status":"succeeded","amount":750,"currency":"CNY","customer_secret":"must-not-persist"}`)
	payload, err := minimizedRechargeCallbackPayload(callback, body)
	if err != nil {
		t.Fatalf("minimize callback evidence: %v", err)
	}
	if strings.Contains(payload, "must-not-persist") || !strings.Contains(payload, `"verified":true`) || !strings.Contains(payload, `"payload_sha256":"`) {
		t.Fatalf("callback evidence was not safely minimized: %s", payload)
	}
}

func TestValidCheckoutURLAllowsOnlySandboxRelativePathInDevelopment(t *testing.T) {
	valid := []string{"https://pay.example.net/checkout/1", "http://127.0.0.1/pay/1", "/sandbox/pay/abc"}
	for _, candidate := range valid {
		if !validCheckoutURL(candidate, true) {
			t.Fatalf("valid development checkout URL rejected: %q", candidate)
		}
	}
	invalid := []string{"//evil.example/pay", "/sandbox/pay/abc?next=https://evil.example", "/other/pay/abc", "javascript:alert(1)", "https://user:pass@example.net/pay"}
	for _, candidate := range invalid {
		if validCheckoutURL(candidate, true) {
			t.Fatalf("unsafe checkout URL accepted: %q", candidate)
		}
	}
	if validCheckoutURL("/sandbox/pay/abc", false) || validCheckoutURL("http://127.0.0.1/pay/1", false) {
		t.Fatal("development-only checkout URL accepted in production mode")
	}
}
