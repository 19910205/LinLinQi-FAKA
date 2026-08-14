package handler

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"linlinqi/api/internal/model"
)

func TestReconciliationIncludesRechargeAndExceptionalPaymentFactsPostgreSQL(t *testing.T) {
	db := paymentPostgresTestDB(t)
	now := time.Now().UTC()
	channel := model.PaymentChannel{
		Base: model.Base{ID: uuid.New()}, Name: "Reconciliation Test", Code: "reconcile-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:8],
		Provider: "sandbox", Enabled: true, SupportedCurrencies: []byte(`["CNY"]`), SettlementCurrency: "CNY",
	}
	if err := db.Create(&channel).Error; err != nil {
		t.Fatalf("create payment channel: %v", err)
	}
	user := model.User{
		Base: model.Base{ID: uuid.New()}, Email: "reconcile-" + uuid.NewString() + "@example.com",
		PasswordHash: "not-used", Status: "active",
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create recharge user: %v", err)
	}
	recharge := model.RechargeOrder{
		Base: model.Base{ID: uuid.New()}, RechargeNo: "LQRC-" + uuid.NewString(), IntentNo: "LQRCI-" + uuid.NewString(),
		IdempotencyKeyHash: strings.Repeat("c", 64), UserID: user.ID, Amount: 1000, Currency: "CNY",
		CreditAmount: 1000, CreditCurrency: "CNY", ChannelID: channel.ID, Status: "refunded",
		ProviderTradeNo: "provider-recharge-payment", ExpiresAt: now.Add(time.Minute), PaidAt: &now,
	}
	if err := db.Create(&recharge).Error; err != nil {
		t.Fatalf("create recharge order: %v", err)
	}
	rechargeTransaction := model.RechargeTransaction{
		Base: model.Base{ID: uuid.New()}, RechargeOrderID: recharge.ID, ProviderEventID: "event-" + uuid.NewString(),
		ProviderTradeNo: recharge.ProviderTradeNo, Amount: 999, Currency: "CNY", ExpectedAmount: 1000, ExpectedCurrency: "CNY",
		Status: "succeeded", Disposition: "refunded", MismatchReason: "amount mismatch", RefundNo: "LQRR-" + uuid.NewString(),
		ProviderRefundNo: "provider-recharge-refund", RawPayload: `{}`, PaidAt: &now, RefundedAt: &now,
	}
	if err := db.Create(&rechargeTransaction).Error; err != nil {
		t.Fatalf("create recharge transaction: %v", err)
	}

	order := model.Order{
		Base: model.Base{ID: uuid.New()}, OrderNo: "LQ-RECON-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:20],
		Email: "reconcile@example.com", Status: "expired", PaymentStatus: "expired",
		Subtotal: 1000, Total: 1000, Currency: "CNY", PaymentMethod: channel.Code,
	}
	if err := db.Create(&order).Error; err != nil {
		t.Fatalf("create exceptional payment order: %v", err)
	}
	intent := model.PaymentIntent{
		Base: model.Base{ID: uuid.New()}, OrderID: order.ID, IntentNo: "LQI-" + uuid.NewString(), ChannelID: channel.ID,
		Amount: 1000, Currency: "CNY", OrderAmount: 1000, OrderCurrency: "CNY", Status: "requires_refund",
		ProviderTradeNo: "provider-exceptional-payment", ExpiresAt: now.Add(time.Minute), SucceededAt: &now,
	}
	if err := db.Create(&intent).Error; err != nil {
		t.Fatalf("create exceptional payment intent: %v", err)
	}
	paymentTransaction := model.PaymentTransaction{
		Base: model.Base{ID: uuid.New()}, PaymentIntentID: intent.ID, Direction: "payment",
		ProviderEventID: "payment-event-" + uuid.NewString(), Amount: 1200, Currency: "CNY",
		Status: "requires_refund", RawPayload: `{}`,
	}
	if err := db.Create(&paymentTransaction).Error; err != nil {
		t.Fatalf("create exceptional payment transaction: %v", err)
	}

	records, err := loadReconciliationSystemRecords(db, channel.ID, "CNY", now.Add(-time.Minute), now.Add(time.Minute))
	if err != nil {
		t.Fatalf("load reconciliation records: %v", err)
	}
	byKey := make(map[string]reconciliationSystemRecord, len(records))
	for _, record := range records {
		byKey[record.Key] = record
	}
	assertAmount := func(key string, amount int64, referenceID uuid.UUID) {
		t.Helper()
		record, ok := byKey[key]
		if !ok || record.Amount != amount || record.Currency != "CNY" || record.OrderID != referenceID {
			t.Fatalf("record %s = %#v, exists=%v", key, record, ok)
		}
	}
	assertAmount("payment:"+recharge.ProviderTradeNo, 999, recharge.ID)
	assertAmount("refund:"+rechargeTransaction.ProviderRefundNo, 999, recharge.ID)
	assertAmount("payment:"+intent.ProviderTradeNo, 1200, order.ID)
}
