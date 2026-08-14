package handler

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"linlinqi/api/internal/config"
	"linlinqi/api/internal/database"
	"linlinqi/api/internal/model"
	"linlinqi/api/internal/payment"
	"linlinqi/api/internal/security"
)

func paymentPostgresTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("LINLINQI_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set LINLINQI_TEST_DATABASE_URL to run PostgreSQL payment integration tests")
	}
	adminDB, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("connect PostgreSQL: %v", err)
	}
	schemaName := "linlinqi_payment_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if err := adminDB.Exec(`CREATE SCHEMA "` + schemaName + `"`).Error; err != nil {
		t.Fatalf("create payment test schema: %v", err)
	}
	t.Cleanup(func() {
		_ = adminDB.Exec(`DROP SCHEMA "` + schemaName + `" CASCADE`).Error
		if sqlDB, dbErr := adminDB.DB(); dbErr == nil {
			_ = sqlDB.Close()
		}
	})
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse PostgreSQL URL: %v", err)
	}
	query := parsed.Query()
	query.Set("search_path", schemaName)
	parsed.RawQuery = query.Encode()
	db, err := gorm.Open(postgres.Open(parsed.String()), &gorm.Config{})
	if err != nil {
		t.Fatalf("open isolated payment schema: %v", err)
	}
	if err := database.Migrate(db); err != nil {
		t.Fatalf("migrate isolated payment schema: %v", err)
	}
	return db
}

func TestFinalizePaymentIntentNeverRegressesTerminalStatePostgreSQL(t *testing.T) {
	db := paymentPostgresTestDB(t)
	channelID := uuid.New()
	succeededAt := time.Now()
	terminal := model.PaymentIntent{
		Base: model.Base{ID: uuid.New()}, OrderID: uuid.New(), IntentNo: "terminal-" + uuid.NewString(),
		ChannelID: channelID, Amount: 1200, Currency: "CNY", OrderAmount: 1200, OrderCurrency: "CNY", Status: "succeeded",
		ProviderTradeNo: "provider-terminal", ExpiresAt: time.Now().Add(time.Minute), SucceededAt: &succeededAt,
	}
	if err := db.Create(&terminal).Error; err != nil {
		t.Fatalf("create terminal payment intent: %v", err)
	}
	got, err := finalizePaymentIntentCreation(db, terminal.ID, payment.CreateResult{
		ProviderTradeNo: terminal.ProviderTradeNo, CheckoutURL: "https://pay.example.com/terminal", ExpiresAt: time.Now().Add(15 * time.Minute),
	})
	if err != nil {
		t.Fatalf("finalize terminal payment intent: %v", err)
	}
	if got.Status != "succeeded" || got.CheckoutURL != "" {
		t.Fatalf("terminal state regressed while saving provider response: %#v", got)
	}

	creating := model.PaymentIntent{
		Base: model.Base{ID: uuid.New()}, OrderID: uuid.New(), IntentNo: "creating-" + uuid.NewString(),
		ChannelID: channelID, Amount: 900, Currency: "CNY", OrderAmount: 900, OrderCurrency: "CNY", Status: "creating", ExpiresAt: time.Now().Add(time.Minute),
	}
	if err := db.Create(&creating).Error; err != nil {
		t.Fatalf("create creating payment intent: %v", err)
	}
	got, err = finalizePaymentIntentCreation(db, creating.ID, payment.CreateResult{
		ProviderTradeNo: "provider-created", CheckoutURL: "https://pay.example.com/created", ExpiresAt: time.Now().Add(15 * time.Minute),
	})
	if err != nil {
		t.Fatalf("finalize creating payment intent: %v", err)
	}
	if got.Status != "pending" || got.ProviderTradeNo != "provider-created" {
		t.Fatalf("creating intent was not finalized safely: %#v", got)
	}
}

func TestOrphanPaymentIncidentIsDurableAndIdempotentPostgreSQL(t *testing.T) {
	db := paymentPostgresTestDB(t)
	channel := model.PaymentChannel{Base: model.Base{ID: uuid.New()}, Code: "disabled-channel"}
	callback := payment.CallbackResult{
		IntentNo: "unknown-intent", ProviderTradeNo: "provider-orphan", Status: "succeeded", Amount: 1800, Currency: "CNY",
	}
	eventID := channel.ID.String() + ":provider-event"
	for range 2 {
		if err := db.Transaction(func(tx *gorm.DB) error {
			return recordOrphanPaymentSecurityEvent(tx, channel, callback, eventID, "203.0.113.9", "provider-test")
		}); err != nil {
			t.Fatalf("record orphan payment incident: %v", err)
		}
	}
	var events []model.SecurityEvent
	if err := db.Where("event_type = ?", "payment.orphan_received").Find(&events).Error; err != nil {
		t.Fatalf("load orphan payment incidents: %v", err)
	}
	if len(events) != 1 || events[0].Resolved || !strings.Contains(events[0].Details, "provider-orphan") {
		t.Fatalf("orphan payment incident was not durable/idempotent: %#v", events)
	}
}

func TestFastCallbackMovesCreatingIntentAtomicallyPostgreSQL(t *testing.T) {
	db := paymentPostgresTestDB(t)
	secret := "sandbox-payment-callback-secret"
	channel := model.PaymentChannel{
		Base: model.Base{ID: uuid.New()}, Name: "Callback Sandbox", Code: "sandbox",
		Provider: "sandbox", Enabled: true,
	}
	if err := db.Create(&channel).Error; err != nil {
		t.Fatalf("create callback channel: %v", err)
	}
	order := model.Order{
		Base: model.Base{ID: uuid.New()}, OrderNo: "LQ-CALLBACK-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:20],
		Email: "callback@example.com", Status: "expired", PaymentStatus: "expired",
		Subtotal: 1200, Total: 1200, PaymentMethod: channel.Code,
	}
	if err := db.Create(&order).Error; err != nil {
		t.Fatalf("create callback order: %v", err)
	}
	intent := model.PaymentIntent{
		Base: model.Base{ID: uuid.New()}, OrderID: order.ID, IntentNo: "callback-" + uuid.NewString(),
		ChannelID: channel.ID, Amount: order.Total, Currency: "CNY", OrderAmount: order.Total, OrderCurrency: "CNY", Status: "creating", ExpiresAt: time.Now().Add(time.Minute),
	}
	if err := db.Create(&intent).Error; err != nil {
		t.Fatalf("create callback intent: %v", err)
	}
	callback := payment.CallbackResult{
		EventID: "fast-callback", IntentNo: intent.IntentNo, ProviderTradeNo: "provider-fast-callback",
		Status: "succeeded", Amount: intent.Amount, Currency: intent.Currency,
	}
	body, err := json.Marshal(callback)
	if err != nil {
		t.Fatalf("marshal callback: %v", err)
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest("POST", "/api/v1/payments/"+channel.Code+"/callback", bytes.NewReader(body))
	context.Request.Header.Set("X-Signature", hex.EncodeToString(mac.Sum(nil)))
	context.Params = gin.Params{{Key: "channel", Value: channel.Code}}
	handler := Handler{DB: db, Cfg: config.Config{Env: "development", OpenAPISecret: secret, RedisAddr: "127.0.0.1:1"}}
	handler.PaymentCallback(context)
	if recorder.Code != 200 {
		t.Fatalf("fast callback failed: status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if err := db.First(&intent, "id = ?", intent.ID).Error; err != nil {
		t.Fatalf("reload callback intent: %v", err)
	}
	if intent.Status != "requires_refund" || intent.ProviderTradeNo != callback.ProviderTradeNo || intent.SucceededAt == nil {
		t.Fatalf("fast callback was not committed atomically: %#v", intent)
	}
	var refunds int64
	if err := db.Model(&model.Refund{}).Where("payment_intent_id = ? AND status = ?", intent.ID, "pending").Count(&refunds).Error; err != nil || refunds != 1 {
		t.Fatalf("fast late callback did not create one refund: count=%d err=%v", refunds, err)
	}
}

func TestMismatchedRechargeCallbackPersistsRefundEvidenceWithoutWalletCreditPostgreSQL(t *testing.T) {
	db := paymentPostgresTestDB(t)
	secret := "sandbox-recharge-callback-secret"
	user := model.User{
		Base: model.Base{ID: uuid.New()}, Email: "recharge-" + uuid.NewString() + "@example.com",
		PasswordHash: "not-used", Status: "active",
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create recharge user: %v", err)
	}
	channel := model.PaymentChannel{
		Base: model.Base{ID: uuid.New()}, Name: "Recharge Callback Sandbox", Code: "sandbox",
		Provider: "sandbox", Enabled: true,
	}
	if err := db.Create(&channel).Error; err != nil {
		t.Fatalf("create recharge channel: %v", err)
	}
	recharge := model.RechargeOrder{
		Base: model.Base{ID: uuid.New()}, RechargeNo: "LQRC-" + uuid.NewString(), IntentNo: "LQRCI-" + uuid.NewString(),
		IdempotencyKeyHash: strings.Repeat("a", 64), UserID: user.ID, Amount: 1000, Currency: "CNY",
		CreditAmount: 1000, CreditCurrency: "CNY", ChannelID: channel.ID, Status: "pending",
		ProviderTradeNo: "provider-recharge-mismatch", ExpiresAt: time.Now().Add(time.Minute),
	}
	if err := db.Create(&recharge).Error; err != nil {
		t.Fatalf("create recharge order: %v", err)
	}
	callback := payment.CallbackResult{
		EventID: "recharge-mismatch", IntentNo: recharge.IntentNo, ProviderTradeNo: recharge.ProviderTradeNo,
		Status: "succeeded", Amount: 999, Currency: "CNY",
	}
	body, err := json.Marshal(map[string]any{
		"event_id": callback.EventID, "intent_no": callback.IntentNo, "provider_trade_no": callback.ProviderTradeNo,
		"status": callback.Status, "amount": callback.Amount, "currency": callback.Currency,
		"customer_secret": "must-not-persist",
	})
	if err != nil {
		t.Fatalf("marshal recharge callback: %v", err)
	}
	invoke := func() {
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write(body)
		gin.SetMode(gin.TestMode)
		recorder := httptest.NewRecorder()
		context, _ := gin.CreateTestContext(recorder)
		context.Request = httptest.NewRequest("POST", "/api/v1/payments/"+channel.Code+"/callback", bytes.NewReader(body))
		context.Request.Header.Set("X-Signature", hex.EncodeToString(mac.Sum(nil)))
		context.Params = gin.Params{{Key: "channel", Value: channel.Code}}
		handler := Handler{DB: db, Cfg: config.Config{Env: "development", OpenAPISecret: secret, RedisAddr: "127.0.0.1:1"}}
		handler.PaymentCallback(context)
		if recorder.Code != 200 {
			t.Fatalf("mismatched recharge callback was not durably accepted: status=%d body=%s", recorder.Code, recorder.Body.String())
		}
	}
	invoke()
	invoke()
	if err := db.First(&recharge, "id = ?", recharge.ID).Error; err != nil {
		t.Fatalf("reload recharge order: %v", err)
	}
	if recharge.Status != "requires_refund" || recharge.PaidAt == nil {
		t.Fatalf("recharge exception state was not committed: %#v", recharge)
	}
	var transactions []model.RechargeTransaction
	if err := db.Where("recharge_order_id = ?", recharge.ID).Find(&transactions).Error; err != nil {
		t.Fatalf("load recharge callback evidence: %v", err)
	}
	if len(transactions) != 1 {
		t.Fatalf("provider retry created duplicate refund evidence: %#v", transactions)
	}
	transaction := transactions[0]
	if transaction.Status != "succeeded" || transaction.Disposition != "refund_pending" || transaction.Amount != 999 || transaction.ExpectedAmount != 1000 || transaction.RefundNo == "" {
		t.Fatalf("mismatched receipt was not captured for refund: %#v", transaction)
	}
	if strings.Contains(transaction.RawPayload, "must-not-persist") || !strings.Contains(transaction.RawPayload, "payload_sha256") {
		t.Fatalf("recharge callback raw body was not minimized: %s", transaction.RawPayload)
	}
	var walletEntries, walletAccounts int64
	if err := db.Model(&model.WalletEntry{}).Where("reference_id = ?", recharge.ID).Count(&walletEntries).Error; err != nil {
		t.Fatalf("count wallet entries: %v", err)
	}
	if err := db.Model(&model.WalletAccount{}).Where("owner_type = ? AND owner_id = ?", "user", user.ID).Count(&walletAccounts).Error; err != nil {
		t.Fatalf("count wallet accounts: %v", err)
	}
	if walletEntries != 0 || walletAccounts != 0 {
		t.Fatalf("mismatched recharge changed wallet state: entries=%d accounts=%d", walletEntries, walletAccounts)
	}
}

func TestBepusdtChannelDriverAndCreatePostgreSQL(t *testing.T) {
	db := paymentPostgresTestDB(t)
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/order/create-transaction" {
			t.Errorf("unexpected bepusdt path %s", r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode bepusdt request: %v", err)
		}
		if body["order_id"] != "LQI-BEPUSDT-1" || body["fiat"] != "CNY" || body["trade_type"] != "usdt.trc20" || body["signature"] == "" {
			t.Errorf("unexpected bepusdt create payload: %#v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status_code":200,"message":"success","data":{"trade_id":"BEP-TRADE-1","order_id":"LQI-BEPUSDT-1","status":1,"amount":100.5,"actual_amount":"14.58","token":"TAddr","expiration_time":600,"payment_url":"http://127.0.0.1/pay/checkout/BEP-TRADE-1"},"request_id":""}`))
	}))
	defer fake.Close()

	vault, err := security.NewVault("bepusdt-integration-encryption-key-2026-32b")
	if err != nil {
		t.Fatalf("create vault: %v", err)
	}
	channel := model.PaymentChannel{
		Base: model.Base{ID: uuid.New()}, Name: "BEpusdt USDT TRC20", Code: "bepusdt_usdt_trc20",
		Provider: "bepusdt", Enabled: true, SupportedCurrencies: json.RawMessage(`["CNY"]`), SettlementCurrency: "CNY",
	}
	cfg := paymentDriverConfig{BaseURL: fake.URL, APIToken: "api-token-123456", TradeType: "usdt.trc20", Fiat: "CNY", Timeout: 900}
	payload, _ := json.Marshal(cfg)
	channel.ConfigCipher, channel.ConfigNonce, _, err = vault.Encrypt(string(payload), channel.ID[:])
	if err != nil {
		t.Fatalf("encrypt bepusdt config: %v", err)
	}
	if err := db.Create(&channel).Error; err != nil {
		t.Fatalf("create bepusdt channel: %v", err)
	}
	h := Handler{DB: db, Vault: vault, Cfg: config.Config{Env: "test"}}
	driver, err := h.paymentDriver(channel)
	if err != nil {
		t.Fatalf("construct bepusdt driver: %v", err)
	}
	if _, ok := driver.(*payment.BepusdtDriver); !ok {
		t.Fatalf("paymentDriver returned %T, want *payment.BepusdtDriver", driver)
	}
	result, err := driver.Create(t.Context(), payment.CreateRequest{
		IntentNo: "LQI-BEPUSDT-1", OrderNo: "LQ-1", Amount: 10050, Currency: "CNY",
		Subject: "LinLinQi 数字商品订单 LQ-1", NotifyURL: "http://127.0.0.1:8081/api/v1/payments/bepusdt_usdt_trc20/callback", ReturnURL: "http://127.0.0.1:8080/orders",
	})
	if err != nil {
		t.Fatalf("bepusdt create: %v", err)
	}
	if result.ProviderTradeNo != "BEP-TRADE-1" || result.CheckoutURL != "http://127.0.0.1/pay/checkout/BEP-TRADE-1" {
		t.Fatalf("unexpected bepusdt create result: %#v", result)
	}
}
