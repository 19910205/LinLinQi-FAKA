package handler

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"linlinqi/api/internal/model"
	"linlinqi/api/internal/service"
)

func performAdminRefund(t *testing.T, h Handler, orderID uuid.UUID, amount int64, reason, idempotencyKey string) *httptest.ResponseRecorder {
	t.Helper()
	body := fmt.Sprintf(`{"order_id":%q,"amount":%d,"reason":%q}`, orderID.String(), amount, reason)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodPost, "/admin/v1/refunds", bytes.NewBufferString(body))
	context.Request.Header.Set("Content-Type", "application/json")
	context.Request.Header.Set("Idempotency-Key", idempotencyKey)
	context.Set("subject", "refund-test-admin")
	h.CreateRefund(context)
	return recorder
}

func createWalletRefundOrder(t *testing.T, db *gorm.DB, method, ownerType string, ownerID uuid.UUID, amount int64) (model.Order, model.WalletAccount) {
	t.Helper()
	now := time.Now().UTC()
	order := model.Order{
		Base: model.Base{ID: uuid.New()}, OrderNo: "LQ-WALLET-REFUND-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:16],
		Email: "wallet-refund-" + uuid.NewString() + "@example.com", Status: "delivered", PaymentStatus: "paid",
		Subtotal: amount, Total: amount, Currency: "CNY", PaymentMethod: method, PaidAt: &now,
	}
	entryNo, entryType := "LQW-STORE-"+order.ID.String(), "order_payment"
	if method == "balance" {
		order.UserID = &ownerID
	} else {
		credentialID := uuid.New()
		order.APICredentialID = &credentialID
		entryNo, entryType = "LQW-API-"+order.ID.String(), "api_order"
		if ownerType == "reseller" {
			order.ResellerID = &ownerID
		} else {
			order.UserID = &ownerID
		}
	}
	if err := db.Create(&order).Error; err != nil {
		t.Fatalf("create wallet order: %v", err)
	}
	wallet := model.WalletAccount{Base: model.Base{ID: uuid.New()}, OwnerType: ownerType, OwnerID: ownerID, Currency: "CNY", Balance: 4_000}
	if err := db.Create(&wallet).Error; err != nil {
		t.Fatalf("create wallet: %v", err)
	}
	orderID := order.ID
	debit := model.WalletEntry{
		Base: model.Base{ID: uuid.New()}, AccountID: wallet.ID, EntryNo: entryNo, Type: entryType,
		Amount: -amount, BalanceAfter: wallet.Balance, ReferenceType: "order", ReferenceID: &orderID, Description: "original payment",
	}
	if err := db.Create(&debit).Error; err != nil {
		t.Fatalf("create original wallet debit: %v", err)
	}
	return order, wallet
}

func TestAdminWalletRefundIsLocalPartialFullAndIdempotentPostgreSQL(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := paymentPostgresTestDB(t)
	user := model.User{Email: "wallet-refund-" + uuid.NewString() + "@example.com", PasswordHash: "not-used", Status: "active"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	order, wallet := createWalletRefundOrder(t, db, "balance", "user", user.ID, 1_000)
	h := Handler{DB: db}

	firstKey := "wallet-refund-first-" + uuid.NewString()
	first := performAdminRefund(t, h, order.ID, 400, "partial wallet refund", firstKey)
	if first.Code != http.StatusCreated {
		t.Fatalf("partial refund failed: status=%d body=%s", first.Code, first.Body.String())
	}
	var refund model.Refund
	if err := db.Where("order_id = ?", order.ID).First(&refund).Error; err != nil || refund.Status != "succeeded" || refund.ProviderRefundNo == "" {
		t.Fatalf("local refund was not finalized: %#v err=%v", refund, err)
	}
	if err := db.First(&wallet, "id = ?", wallet.ID).Error; err != nil || wallet.Balance != 4_400 {
		t.Fatalf("partial wallet credit is wrong: balance=%d err=%v", wallet.Balance, err)
	}
	if err := db.First(&order, "id = ?", order.ID).Error; err != nil || order.PaymentStatus != "partially_refunded" {
		t.Fatalf("order was not partially refunded: status=%q err=%v", order.PaymentStatus, err)
	}

	replay := performAdminRefund(t, h, order.ID, 400, "partial wallet refund", firstKey)
	if replay.Code != http.StatusCreated {
		t.Fatalf("idempotent retry failed: status=%d body=%s", replay.Code, replay.Body.String())
	}
	var refundCount, creditCount int64
	if err := db.Model(&model.Refund{}).Where("order_id = ?", order.ID).Count(&refundCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.WalletEntry{}).Where("account_id = ? AND type = ?", wallet.ID, "order_refund").Count(&creditCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.First(&wallet, "id = ?", wallet.ID).Error; err != nil || wallet.Balance != 4_400 || refundCount != 1 || creditCount != 1 {
		t.Fatalf("retry duplicated money: balance=%d refunds=%d credits=%d err=%v", wallet.Balance, refundCount, creditCount, err)
	}

	second := performAdminRefund(t, h, order.ID, 0, "refund all remaining balance", "wallet-refund-rest-"+uuid.NewString())
	if second.Code != http.StatusCreated {
		t.Fatalf("full remaining refund failed: status=%d body=%s", second.Code, second.Body.String())
	}
	if err := db.First(&wallet, "id = ?", wallet.ID).Error; err != nil || wallet.Balance != 5_000 {
		t.Fatalf("full wallet restoration is wrong: balance=%d err=%v", wallet.Balance, err)
	}
	if err := db.First(&order, "id = ?", order.ID).Error; err != nil || order.PaymentStatus != "refunded" || order.Status != "refunded" {
		t.Fatalf("order was not fully refunded: %#v err=%v", order, err)
	}
	var intent model.PaymentIntent
	if err := db.Where("order_id = ?", order.ID).First(&intent).Error; err != nil || intent.Status != "refunded" || intent.ProviderTradeNo != "wallet:"+order.ID.String() {
		t.Fatalf("wallet payment intent audit is wrong: %#v err=%v", intent, err)
	}
}

func TestAdminOpenAPIResellerWalletRefundRestoresOriginalOwnerPostgreSQL(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := paymentPostgresTestDB(t)
	resellerID := uuid.New()
	order, wallet := createWalletRefundOrder(t, db, "supplier_balance", "reseller", resellerID, 700)
	h := Handler{DB: db}

	result := performAdminRefund(t, h, order.ID, 0, "OpenAPI reseller wallet refund", "openapi-reseller-refund-"+uuid.NewString())
	if result.Code != http.StatusCreated {
		t.Fatalf("OpenAPI wallet refund failed: status=%d body=%s", result.Code, result.Body.String())
	}
	if err := db.First(&wallet, "id = ?", wallet.ID).Error; err != nil || wallet.Balance != 4_700 {
		t.Fatalf("reseller wallet was not restored: balance=%d err=%v", wallet.Balance, err)
	}
	var credits []model.WalletEntry
	if err := db.Where("reference_type = ?", "refund").Find(&credits).Error; err != nil || len(credits) != 1 || credits[0].AccountID != wallet.ID || credits[0].Amount != 700 {
		t.Fatalf("refund was credited to the wrong owner: %#v err=%v", credits, err)
	}
}

func TestAdminRefundRecognizesCanonicalWalletIntentForLegacyMethodPostgreSQL(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := paymentPostgresTestDB(t)
	userID := uuid.New()
	user := model.User{
		Base: model.Base{ID: userID}, Email: "legacy-wallet-" + uuid.NewString() + "@example.com",
		PasswordHash: "not-used", Status: "active",
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create legacy wallet owner: %v", err)
	}
	order, wallet := createWalletRefundOrder(t, db, "balance", "user", userID, 500)
	if err := db.Transaction(func(tx *gorm.DB) error {
		var locked model.Order
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&locked, "id = ?", order.ID).Error; err != nil {
			return err
		}
		_, err := service.EnsureWalletOrderPaymentAuditTx(tx, locked, time.Now().UTC())
		return err
	}); err != nil {
		t.Fatalf("create canonical wallet audit: %v", err)
	}
	if err := db.Model(&order).Update("payment_method", "legacy_wallet_v0").Error; err != nil {
		t.Fatalf("simulate legacy payment method: %v", err)
	}

	result := performAdminRefund(t, Handler{DB: db}, order.ID, 0, "legacy wallet order refund", "legacy-wallet-refund-"+uuid.NewString())
	if result.Code != http.StatusCreated {
		t.Fatalf("canonical wallet intent was not recognized: status=%d body=%s", result.Code, result.Body.String())
	}
	if err := db.First(&wallet, "id = ?", wallet.ID).Error; err != nil || wallet.Balance != 4500 {
		t.Fatalf("legacy wallet refund did not restore original account: balance=%d err=%v", wallet.Balance, err)
	}
	var channels int64
	if err := db.Model(&model.PaymentChannel{}).Count(&channels).Error; err != nil || channels != 0 {
		t.Fatalf("local refund unexpectedly required an external channel: channels=%d err=%v", channels, err)
	}
}

func TestAdminWalletRefundRejectsConcurrentRefundAndProcurementPostgreSQL(t *testing.T) {
	gin.SetMode(gin.TestMode)
	t.Run("active refund", func(t *testing.T) {
		db := paymentPostgresTestDB(t)
		order, wallet := createWalletRefundOrder(t, db, "balance", "user", uuid.New(), 600)
		active := model.Refund{
			Base: model.Base{ID: uuid.New()}, RefundNo: "LQR-ACTIVE-" + uuid.NewString(), OrderID: order.ID,
			Amount: 100, Currency: "CNY", OrderAmount: 100, OrderCurrency: "CNY",
			Reason: "existing refund", Status: "pending", RequestedBy: "admin:other",
		}
		if err := db.Create(&active).Error; err != nil {
			t.Fatalf("create active refund: %v", err)
		}
		result := performAdminRefund(t, Handler{DB: db}, order.ID, 100, "concurrent refund", "concurrent-refund-"+uuid.NewString())
		if result.Code != http.StatusConflict {
			t.Fatalf("concurrent refund was accepted: status=%d body=%s", result.Code, result.Body.String())
		}
		var count int64
		if err := db.Model(&model.Refund{}).Where("order_id = ?", order.ID).Count(&count).Error; err != nil || count != 1 {
			t.Fatalf("concurrent refund created another row: count=%d err=%v", count, err)
		}
		if err := db.First(&wallet, "id = ?", wallet.ID).Error; err != nil || wallet.Balance != 4000 {
			t.Fatalf("rejected refund changed wallet: balance=%d err=%v", wallet.Balance, err)
		}
	})

	t.Run("active supplier procurement", func(t *testing.T) {
		db := paymentPostgresTestDB(t)
		order, wallet := createWalletRefundOrder(t, db, "balance", "user", uuid.New(), 600)
		procurement := model.ProcurementOrder{
			Base: model.Base{ID: uuid.New()}, ProcurementNo: "LQP-ACTIVE-" + uuid.NewString(),
			SupplierID: uuid.New(), OrderID: order.ID, OrderItemID: uuid.New(), ExternalProductID: "remote-product",
			Quantity: 1, CostAmount: 100, CostCurrency: "CNY", UpstreamCostAmount: 100, UpstreamCurrency: "CNY",
			Status: "processing", RequestBody: `{}`, ResponseBody: `{}`,
		}
		if err := db.Create(&procurement).Error; err != nil {
			t.Fatalf("create active procurement: %v", err)
		}
		result := performAdminRefund(t, Handler{DB: db}, order.ID, 100, "refund during procurement", "procurement-refund-"+uuid.NewString())
		if result.Code != http.StatusConflict {
			t.Fatalf("refund raced active procurement: status=%d body=%s", result.Code, result.Body.String())
		}
		var refunds int64
		if err := db.Model(&model.Refund{}).Where("order_id = ?", order.ID).Count(&refunds).Error; err != nil || refunds != 0 {
			t.Fatalf("procurement race created a refund: count=%d err=%v", refunds, err)
		}
		if err := db.First(&wallet, "id = ?", wallet.ID).Error; err != nil || wallet.Balance != 4000 {
			t.Fatalf("procurement race changed wallet: balance=%d err=%v", wallet.Balance, err)
		}
	})
}
