package handler

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"linlinqi/api/internal/model"
)

func TestPaymentChannelFinancialHistoryIncludesOrdersAndRechargesPostgreSQL(t *testing.T) {
	db := paymentPostgresTestDB(t)
	createChannel := func(code string) model.PaymentChannel {
		channel := model.PaymentChannel{
			Base: model.Base{ID: uuid.New()}, Name: code, Code: code, Provider: "sandbox",
			Enabled: true, SupportedCurrencies: []byte(`["CNY"]`), SettlementCurrency: "CNY",
		}
		if err := db.Create(&channel).Error; err != nil {
			t.Fatalf("create channel %s: %v", code, err)
		}
		return channel
	}

	checkoutChannel := createChannel("history-order-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:8])
	if found, err := paymentChannelHasFinancialHistory(db, checkoutChannel.ID); err != nil || found {
		t.Fatalf("fresh checkout channel history = %v, err = %v", found, err)
	}
	intent := model.PaymentIntent{
		Base: model.Base{ID: uuid.New()}, OrderID: uuid.New(), IntentNo: "history-" + uuid.NewString(),
		ChannelID: checkoutChannel.ID, Amount: 100, Currency: "CNY", OrderAmount: 100, OrderCurrency: "CNY",
		ProviderTradeNo: "provider-history", Status: "pending", ExpiresAt: time.Now().Add(time.Hour),
	}
	if err := db.Create(&intent).Error; err != nil {
		t.Fatalf("create payment history: %v", err)
	}
	if err := db.Delete(&intent).Error; err != nil {
		t.Fatalf("soft-delete payment history: %v", err)
	}
	if found, err := paymentChannelHasFinancialHistory(db, checkoutChannel.ID); err != nil || !found {
		t.Fatalf("soft-deleted payment history = %v, err = %v", found, err)
	}

	rechargeChannel := createChannel("history-recharge-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:8])
	user := model.User{
		Base: model.Base{ID: uuid.New()}, Email: "channel-history-" + uuid.NewString() + "@example.com",
		PasswordHash: "not-used", Status: "active",
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create recharge user: %v", err)
	}
	recharge := model.RechargeOrder{
		Base: model.Base{ID: uuid.New()}, RechargeNo: "LQRC-" + uuid.NewString(), IntentNo: "LQRCI-" + uuid.NewString(),
		IdempotencyKeyHash: strings.Repeat("b", 64), UserID: user.ID, Amount: 100, Currency: "CNY",
		CreditAmount: 100, CreditCurrency: "CNY", ChannelID: rechargeChannel.ID, Status: "pending",
		ExpiresAt: time.Now().Add(time.Hour),
	}
	if err := db.Create(&recharge).Error; err != nil {
		t.Fatalf("create recharge history: %v", err)
	}
	if found, err := paymentChannelHasFinancialHistory(db, rechargeChannel.ID); err != nil || !found {
		t.Fatalf("recharge history = %v, err = %v", found, err)
	}
}

func TestPaymentChannelCheckoutLockRejectsStaleConnectorSnapshotPostgreSQL(t *testing.T) {
	db := paymentPostgresTestDB(t)
	channel := model.PaymentChannel{
		Base: model.Base{ID: uuid.New()}, Name: "Stale Connector", Code: "stale-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:8],
		Provider: "sandbox", Enabled: true, SupportedCurrencies: []byte(`["CNY"]`), SettlementCurrency: "CNY",
	}
	if err := db.Create(&channel).Error; err != nil {
		t.Fatalf("create channel: %v", err)
	}
	if err := db.Model(&model.PaymentChannel{}).Where("id = ?", channel.ID).UpdateColumn("updated_at", channel.UpdatedAt.Add(time.Second)).Error; err != nil {
		t.Fatalf("rotate channel snapshot: %v", err)
	}
	err := db.Transaction(func(tx *gorm.DB) error {
		return lockCurrentPaymentChannelTx(tx, channel)
	})
	if !errors.Is(err, errPaymentChannelChanged) {
		t.Fatalf("stale connector snapshot error = %v", err)
	}
}
