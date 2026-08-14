package handler

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"linlinqi/api/internal/config"
	"linlinqi/api/internal/model"
)

func TestSafeOAuthRedirectRejectsExternalAndAmbiguousTargets(t *testing.T) {
	tests := map[string]string{
		"":                                 "/account/profile",
		"/account/wallet?result=success":   "/account/wallet?result=success",
		"https://attacker.example/account": "/account/profile",
		"//attacker.example/account":       "/account/profile",
		"/\\attacker.example":              "/account/profile",
		"/account/profile#token":           "/account/profile",
	}
	for input, expected := range tests {
		if actual := safeOAuthRedirect(input); actual != expected {
			t.Errorf("safeOAuthRedirect(%q) = %q, want %q", input, actual, expected)
		}
	}
}

func TestOAuthProviderDiscoveryNeverExposesConfigurationSecrets(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := Handler{Cfg: config.Config{
		Env:                "development",
		OAuthProvidersJSON: `{"corp":{"name":"企业身份","issuer":"http://127.0.0.1:9999","client_id":"private-client-id","client_secret":"private-client-secret","scopes":["profile"]}}`,
	}}
	responseRecorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(responseRecorder)
	context.Request = httptest.NewRequest("GET", "/api/v1/auth/oauth/providers", nil)
	h.OAuthProviders(context)
	body := responseRecorder.Body.String()
	if responseRecorder.Code != 200 || !strings.Contains(body, "企业身份") {
		t.Fatalf("unexpected provider response: status=%d body=%s", responseRecorder.Code, body)
	}
	for _, forbidden := range []string{"private-client-id", "private-client-secret", "127.0.0.1", "issuer", "scopes"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("provider response leaked %q: %s", forbidden, body)
		}
	}
}

func TestFinancialDTOsDoNotSerializeSecrets(t *testing.T) {
	now := time.Now()
	recharge := model.RechargeOrder{
		Base: model.Base{ID: uuid.New()}, RechargeNo: "LQRC1", IntentNo: "secret-intent",
		IdempotencyKeyHash: "secret-idempotency-hash", ProviderTradeNo: "provider-trade",
		Amount: 1000, Currency: "CNY", Status: "pending", ExpiresAt: now,
	}
	rechargeJSON, err := json.Marshal(toRechargeDTO(recharge, model.PaymentChannel{Code: "pay", Name: "支付"}))
	if err != nil {
		t.Fatal(err)
	}
	for _, forbidden := range []string{"secret-intent", "secret-idempotency-hash", "provider-trade"} {
		if strings.Contains(string(rechargeJSON), forbidden) {
			t.Fatalf("user recharge DTO leaked %q: %s", forbidden, rechargeJSON)
		}
	}

	withdrawal := model.ResellerWithdrawal{
		Base: model.Base{ID: uuid.New()}, WithdrawalNo: "LQRW1", Amount: 10000,
		Method: "bank", AccountCipher: []byte("cipher-secret"), AccountNonce: []byte("nonce-secret"),
		AccountPreview: "••••1234", Status: "pending",
	}
	withdrawalJSON, err := json.Marshal(toResellerWithdrawalDTO(withdrawal))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(withdrawalJSON), "cipher-secret") || strings.Contains(string(withdrawalJSON), "nonce-secret") {
		t.Fatalf("withdrawal DTO leaked encrypted account material: %s", withdrawalJSON)
	}
}

func TestIdempotencyKeyValidation(t *testing.T) {
	if !validIdempotencyKey("recharge-0123456789abcdef") {
		t.Fatal("expected valid idempotency key")
	}
	for _, invalid := range []string{"short", "recharge key with spaces", strings.Repeat("a", 101), "recharge-中文-0123456789"} {
		if validIdempotencyKey(invalid) {
			t.Fatalf("expected invalid idempotency key: %q", invalid)
		}
	}
}
