package queue

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"linlinqi/api/internal/config"
	"linlinqi/api/internal/database"
	"linlinqi/api/internal/model"
	"linlinqi/api/internal/service"
)

func refundPostgresTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("LINLINQI_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set LINLINQI_TEST_DATABASE_URL to run PostgreSQL refund integration tests")
	}
	adminDB, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("connect PostgreSQL: %v", err)
	}
	schemaName := "linlinqi_refund_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if err := adminDB.Exec(`CREATE SCHEMA "` + schemaName + `"`).Error; err != nil {
		t.Fatalf("create refund test schema: %v", err)
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
		t.Fatalf("open isolated refund schema: %v", err)
	}
	if err := database.Migrate(db); err != nil {
		t.Fatalf("migrate isolated refund schema: %v", err)
	}
	return db
}

func createRefundStateFixture(t *testing.T, db *gorm.DB, intentStatus string) (model.Order, model.PaymentIntent) {
	t.Helper()
	now := time.Now().UTC()
	order := model.Order{
		Base: model.Base{ID: uuid.New()}, OrderNo: "LQ-REFUND-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:20],
		Email: "refund@example.com", Status: "delivered", PaymentStatus: "paid",
		Subtotal: 1000, Total: 1000, PaymentMethod: "test", PaidAt: &now,
	}
	if err := db.Create(&order).Error; err != nil {
		t.Fatalf("create refund order: %v", err)
	}
	intent := model.PaymentIntent{
		Base: model.Base{ID: uuid.New()}, OrderID: order.ID, IntentNo: "LQI-" + strings.ReplaceAll(uuid.NewString(), "-", ""),
		ChannelID: uuid.New(), Amount: 1000, Currency: "CNY", OrderAmount: 1000, OrderCurrency: "CNY", Status: intentStatus,
		ProviderTradeNo: "trade-" + uuid.NewString(), ExpiresAt: now.Add(time.Minute), SucceededAt: &now,
	}
	if err := db.Create(&intent).Error; err != nil {
		t.Fatalf("create refund payment intent: %v", err)
	}
	return order, intent
}

func TestFinalizeSuccessfulRefundUpdatesIntentTerminalStatePostgreSQL(t *testing.T) {
	db := refundPostgresTestDB(t)
	order, intent := createRefundStateFixture(t, db, "succeeded")
	now := time.Now().UTC()
	first := model.Refund{
		Base: model.Base{ID: uuid.New()}, RefundNo: "LQR-" + uuid.NewString(), OrderID: order.ID,
		PaymentIntentID: &intent.ID, Amount: 400, Currency: "CNY", OrderAmount: 400, OrderCurrency: "CNY", Reason: "partial", Status: "processing", RequestedBy: "admin:test",
	}
	if err := db.Create(&first).Error; err != nil {
		t.Fatalf("create first refund: %v", err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		return finalizeSuccessfulRefundTx(tx, first, "provider-refund-1", now)
	}); err != nil {
		t.Fatalf("finalize first refund: %v", err)
	}
	if err := db.First(&intent, "id = ?", intent.ID).Error; err != nil || intent.Status != "partially_refunded" {
		t.Fatalf("payment intent did not enter partial refund state: status=%q err=%v", intent.Status, err)
	}

	second := model.Refund{
		Base: model.Base{ID: uuid.New()}, RefundNo: "LQR-" + uuid.NewString(), OrderID: order.ID,
		PaymentIntentID: &intent.ID, Amount: 600, Currency: "CNY", OrderAmount: 600, OrderCurrency: "CNY", Reason: "remainder", Status: "processing", RequestedBy: "admin:test",
	}
	if err := db.Create(&second).Error; err != nil {
		t.Fatalf("create second refund: %v", err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		return finalizeSuccessfulRefundTx(tx, second, "provider-refund-2", now.Add(time.Second))
	}); err != nil {
		t.Fatalf("finalize second refund: %v", err)
	}
	if err := db.First(&intent, "id = ?", intent.ID).Error; err != nil || intent.Status != "refunded" {
		t.Fatalf("payment intent did not enter fully refunded state: status=%q err=%v", intent.Status, err)
	}
	if err := db.First(&order, "id = ?", order.ID).Error; err != nil || order.PaymentStatus != "refunded" || order.Status != "refunded" {
		t.Fatalf("order did not enter refunded state: %#v err=%v", order, err)
	}
}

func TestWalletRefundWorkerUsesLocalLedgerAndIsReplaySafePostgreSQL(t *testing.T) {
	db := refundPostgresTestDB(t)
	now := time.Now().UTC()
	userID := uuid.New()
	user := model.User{
		Base: model.Base{ID: userID}, Email: "wallet-worker-" + uuid.NewString() + "@example.com",
		PasswordHash: "not-used", Status: "active",
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create wallet owner: %v", err)
	}
	order := model.Order{
		Base: model.Base{ID: uuid.New()}, OrderNo: "LQ-WALLET-WORKER-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:16],
		UserID: &userID, Email: "wallet-worker@example.com", Status: "delivered", PaymentStatus: "paid",
		Subtotal: 1000, Total: 1000, Currency: "CNY", PaymentMethod: "balance", PaidAt: &now,
	}
	if err := db.Create(&order).Error; err != nil {
		t.Fatalf("create wallet order: %v", err)
	}
	wallet := model.WalletAccount{
		Base: model.Base{ID: uuid.New()}, OwnerType: "user", OwnerID: userID,
		Currency: "CNY", Balance: 4000,
	}
	if err := db.Create(&wallet).Error; err != nil {
		t.Fatalf("create wallet account: %v", err)
	}
	orderID := order.ID
	debit := model.WalletEntry{
		Base: model.Base{ID: uuid.New()}, AccountID: wallet.ID,
		EntryNo: "LQW-STORE-" + order.ID.String(), Type: "order_payment", Amount: -order.Total, BalanceAfter: wallet.Balance,
		ReferenceType: "order", ReferenceID: &orderID, Description: "original wallet payment",
	}
	if err := db.Create(&debit).Error; err != nil {
		t.Fatalf("create wallet debit: %v", err)
	}
	var settlement service.WalletOrderSettlement
	if err := db.Transaction(func(tx *gorm.DB) error {
		var locked model.Order
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&locked, "id = ?", order.ID).Error; err != nil {
			return err
		}
		var err error
		settlement, err = service.EnsureWalletOrderPaymentAuditTx(tx, locked, now)
		return err
	}); err != nil {
		t.Fatalf("create wallet settlement audit: %v", err)
	}
	if settlement.PaymentIntentID == uuid.Nil {
		t.Fatal("wallet settlement omitted payment intent")
	}

	worker := &Worker{db: db}
	process := func(refund model.Refund) {
		t.Helper()
		payload, err := json.Marshal(map[string]string{"refund_id": refund.ID.String()})
		if err != nil {
			t.Fatalf("marshal wallet refund task: %v", err)
		}
		task := asynq.NewTask(TypeRefundProcess, payload)
		if err := worker.handleRefund(context.Background(), task); err != nil {
			t.Fatalf("process wallet refund: %v", err)
		}
		// A redelivered task must observe the terminal row and perform no
		// second ledger mutation.
		if err := worker.handleRefund(context.Background(), task); err != nil {
			t.Fatalf("replay wallet refund: %v", err)
		}
	}

	firstIntentID := settlement.PaymentIntentID
	first := model.Refund{
		Base: model.Base{ID: uuid.New()}, RefundNo: "LQR-WALLET-" + uuid.NewString(), OrderID: order.ID,
		PaymentIntentID: &firstIntentID, Amount: 400, Currency: "CNY", OrderAmount: 400, OrderCurrency: "CNY",
		Reason: "partial local wallet refund", Status: "pending", RequestedBy: "admin:test",
	}
	if err := db.Create(&first).Error; err != nil {
		t.Fatalf("create partial wallet refund: %v", err)
	}
	process(first)
	if err := db.First(&wallet, "id = ?", wallet.ID).Error; err != nil || wallet.Balance != 4400 {
		t.Fatalf("partial wallet refund balance is wrong: balance=%d err=%v", wallet.Balance, err)
	}

	secondIntentID := settlement.PaymentIntentID
	second := model.Refund{
		Base: model.Base{ID: uuid.New()}, RefundNo: "LQR-WALLET-" + uuid.NewString(), OrderID: order.ID,
		PaymentIntentID: &secondIntentID, Amount: 600, Currency: "CNY", OrderAmount: 600, OrderCurrency: "CNY",
		Reason: "remaining local wallet refund", Status: "pending", RequestedBy: "admin:test",
	}
	if err := db.Create(&second).Error; err != nil {
		t.Fatalf("create remaining wallet refund: %v", err)
	}
	process(second)

	if err := db.First(&wallet, "id = ?", wallet.ID).Error; err != nil || wallet.Balance != 5000 {
		t.Fatalf("full wallet restoration is wrong: balance=%d err=%v", wallet.Balance, err)
	}
	if err := db.First(&order, "id = ?", order.ID).Error; err != nil || order.Status != "refunded" || order.PaymentStatus != "refunded" {
		t.Fatalf("wallet order did not reach refunded state: %#v err=%v", order, err)
	}
	var intent model.PaymentIntent
	if err := db.First(&intent, "id = ?", settlement.PaymentIntentID).Error; err != nil || intent.ChannelID != uuid.Nil || intent.Status != "refunded" {
		t.Fatalf("wallet intent is not terminal or tried to use a channel: %#v err=%v", intent, err)
	}
	var credits int64
	if err := db.Model(&model.WalletEntry{}).Where("account_id = ? AND type = ?", wallet.ID, "order_refund").Count(&credits).Error; err != nil || credits != 2 {
		t.Fatalf("wallet refund tasks duplicated or lost ledger entries: credits=%d err=%v", credits, err)
	}
	for _, expected := range []model.Refund{first, second} {
		var stored model.Refund
		if err := db.First(&stored, "id = ?", expected.ID).Error; err != nil || stored.Status != "succeeded" || !strings.HasPrefix(stored.ProviderRefundNo, "wallet:") {
			t.Fatalf("wallet refund audit is incomplete: %#v err=%v", stored, err)
		}
	}
}

func TestFinalizeAutomaticRefundClearsRequiresRefundStatePostgreSQL(t *testing.T) {
	db := refundPostgresTestDB(t)
	order, intent := createRefundStateFixture(t, db, "requires_refund")
	refund := model.Refund{
		Base: model.Base{ID: uuid.New()}, RefundNo: "LQR-" + uuid.NewString(), OrderID: order.ID,
		PaymentIntentID: &intent.ID, Amount: intent.Amount, Currency: "CNY", OrderAmount: order.Total, OrderCurrency: "CNY", Reason: "late payment", Status: "processing", RequestedBy: "system",
	}
	transaction := model.PaymentTransaction{PaymentIntentID: intent.ID, Direction: "payment", ProviderEventID: "late-" + uuid.NewString(), Amount: intent.Amount, Currency: "CNY", Status: "requires_refund", RawPayload: `{}`}
	if err := db.Create(&transaction).Error; err != nil {
		t.Fatalf("create captured payment transaction: %v", err)
	}
	if err := db.Create(&refund).Error; err != nil {
		t.Fatalf("create automatic refund: %v", err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		return finalizeSuccessfulRefundTx(tx, refund, "provider-refund-auto", time.Now().UTC())
	}); err != nil {
		t.Fatalf("finalize automatic refund: %v", err)
	}
	if err := db.First(&intent, "id = ?", intent.ID).Error; err != nil || intent.Status != "refunded" {
		t.Fatalf("requires_refund state was not cleared: status=%q err=%v", intent.Status, err)
	}
}

func TestRechargeRefundWorkerFinalizesDurableExceptionWithoutWalletCreditPostgreSQL(t *testing.T) {
	db := refundPostgresTestDB(t)
	user := model.User{Base: model.Base{ID: uuid.New()}, Email: "queue-recharge-" + uuid.NewString() + "@example.com", PasswordHash: "not-used", Status: "active"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create recharge user: %v", err)
	}
	channel := model.PaymentChannel{
		Base: model.Base{ID: uuid.New()}, Name: "Queue Sandbox", Code: "queue-sandbox-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:8],
		Provider: "sandbox", Enabled: true,
	}
	if err := db.Create(&channel).Error; err != nil {
		t.Fatalf("create payment channel: %v", err)
	}
	now := time.Now().UTC()
	recharge := model.RechargeOrder{
		Base: model.Base{ID: uuid.New()}, RechargeNo: "LQRC-" + uuid.NewString(), IntentNo: "LQRCI-" + uuid.NewString(),
		IdempotencyKeyHash: strings.Repeat("b", 64), UserID: user.ID, Amount: 1000, Currency: "CNY",
		CreditAmount: 1000, CreditCurrency: "CNY", ChannelID: channel.ID, Status: "requires_refund",
		ProviderTradeNo: "provider-mismatch-" + uuid.NewString(), ExpiresAt: now.Add(time.Minute), PaidAt: &now,
	}
	if err := db.Create(&recharge).Error; err != nil {
		t.Fatalf("create recharge order: %v", err)
	}
	transaction := model.RechargeTransaction{
		Base: model.Base{ID: uuid.New()}, RechargeOrderID: recharge.ID, ProviderEventID: "event-" + uuid.NewString(),
		ProviderTradeNo: recharge.ProviderTradeNo, Amount: 999, Currency: "CNY", ExpectedAmount: 1000, ExpectedCurrency: "CNY",
		Status: "succeeded", Disposition: "refund_pending", MismatchReason: "amount mismatch", RefundNo: "LQRR-" + uuid.NewString(),
		RawPayload: `{}`, PaidAt: &now,
	}
	if err := db.Create(&transaction).Error; err != nil {
		t.Fatalf("create recharge transaction: %v", err)
	}
	payload, err := json.Marshal(map[string]string{"recharge_transaction_id": transaction.ID.String()})
	if err != nil {
		t.Fatalf("marshal refund task: %v", err)
	}
	worker := &Worker{db: db, cfg: config.Config{Env: "development", OpenAPISecret: "sandbox-secret"}}
	if err := worker.handleRechargeRefund(context.Background(), asynq.NewTask(TypeRechargeRefundProcess, payload)); err != nil {
		t.Fatalf("process recharge refund: %v", err)
	}
	if err := db.First(&transaction, "id = ?", transaction.ID).Error; err != nil {
		t.Fatalf("reload recharge transaction: %v", err)
	}
	if transaction.Disposition != "refunded" || transaction.ProviderRefundNo == "" || transaction.RefundedAt == nil {
		t.Fatalf("recharge refund did not reach terminal success: %#v", transaction)
	}
	if err := db.First(&recharge, "id = ?", recharge.ID).Error; err != nil || recharge.Status != "refunded" {
		t.Fatalf("recharge order did not reflect refund completion: status=%q err=%v", recharge.Status, err)
	}
	var entries int64
	if err := db.Model(&model.WalletEntry{}).Where("reference_id = ?", recharge.ID).Count(&entries).Error; err != nil || entries != 0 {
		t.Fatalf("recharge refund changed wallet ledger: entries=%d err=%v", entries, err)
	}
}

func TestFinalizeAutomaticRefundUsesSignedCapturedAmountAsCapacityPostgreSQL(t *testing.T) {
	db := refundPostgresTestDB(t)
	order, intent := createRefundStateFixture(t, db, "requires_refund")
	const captured = int64(1200)
	transaction := model.PaymentTransaction{PaymentIntentID: intent.ID, Direction: "payment", ProviderEventID: "overpayment-" + uuid.NewString(), Amount: captured, Currency: "CNY", Status: "requires_refund", RawPayload: `{}`}
	if err := db.Create(&transaction).Error; err != nil {
		t.Fatalf("create overpayment transaction: %v", err)
	}
	refund := model.Refund{
		Base: model.Base{ID: uuid.New()}, RefundNo: "LQR-" + uuid.NewString(), OrderID: order.ID,
		PaymentIntentID: &intent.ID, Amount: captured, Currency: "CNY", OrderAmount: order.Total, OrderCurrency: "CNY",
		Reason: "signed overpayment", Status: "processing", RequestedBy: "system",
	}
	if err := db.Create(&refund).Error; err != nil {
		t.Fatalf("create overpayment refund: %v", err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		return finalizeSuccessfulRefundTx(tx, refund, "provider-refund-overpayment", time.Now().UTC())
	}); err != nil {
		t.Fatalf("finalize overpayment refund: %v", err)
	}
	if err := db.First(&intent, "id = ?", intent.ID).Error; err != nil || intent.Status != "refunded" {
		t.Fatalf("overpayment intent did not close: status=%q err=%v", intent.Status, err)
	}
}
