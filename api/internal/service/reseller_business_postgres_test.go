package service

import (
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"linlinqi/api/internal/model"
)

func TestResellerWholesalePricingAndRefundCreditBreachPostgreSQL(t *testing.T) {
	db := orderPostgresTestDB(t)

	pricingProfile := model.ResellerProfile{
		UserID: uuid.New(), Name: "Pricing reseller", Code: "pricing-" + uuid.NewString(), Status: "active", WholesaleLevel: 1,
	}
	tier := model.ResellerWholesaleTier{Level: 1, Name: "Ten percent settlement discount", DiscountBasisPoint: 1000, Enabled: true}
	category := model.Category{Name: "Reseller pricing", Slug: "reseller-pricing-" + uuid.NewString(), Enabled: true}
	if err := db.Create(&tier).Error; err != nil {
		t.Fatalf("create wholesale tier: %v", err)
	}
	if err := db.Create(&pricingProfile).Error; err != nil {
		t.Fatalf("create pricing reseller: %v", err)
	}
	if err := db.Create(&category).Error; err != nil {
		t.Fatalf("create category: %v", err)
	}
	product := model.Product{CategoryID: category.ID, Name: "Wholesale product", Slug: "wholesale-product-" + uuid.NewString(), Price: 1000, Status: "on_sale", DeliveryType: "manual", InventoryMode: "local"}
	if err := db.Create(&product).Error; err != nil {
		t.Fatalf("create product: %v", err)
	}
	rule := model.ResellerProductRule{ResellerID: pricingProfile.ID, ProductID: product.ID, Enabled: true, PricingMode: "markup", MarkupBasisPoint: 0}
	if err := db.Create(&rule).Error; err != nil {
		t.Fatalf("create reseller rule: %v", err)
	}
	line, err := ResolveLinePricingForReseller(db, product.ID, nil, nil, &pricingProfile.ID, 2)
	if err != nil {
		t.Fatalf("resolve reseller pricing: %v", err)
	}
	if line.PlatformUnitPrice != 1000 || line.Quote.UnitPrice != 1000 || line.Quote.Total != 2000 || line.ResellerMargin != 200 {
		t.Fatalf("wholesale policy produced the wrong financial snapshot: %#v", line)
	}

	creditProfile := model.ResellerProfile{
		UserID: uuid.New(), Name: "Credit reseller", Code: "credit-" + uuid.NewString(), Status: "active", CreditLimit: 50, WholesaleLevel: 0,
	}
	if err := db.Create(&creditProfile).Error; err != nil {
		t.Fatalf("create credit reseller: %v", err)
	}
	creditPolicy := model.ResellerCreditPolicy{ResellerID: creditProfile.ID, Currency: "CNY", CreditLimit: 50}
	if err := db.Create(&creditPolicy).Error; err != nil {
		t.Fatalf("create reseller credit policy: %v", err)
	}
	wallet := model.WalletAccount{OwnerType: "reseller", OwnerID: creditProfile.ID, Currency: "CNY"}
	if err := db.Create(&wallet).Error; err != nil {
		t.Fatalf("create reseller wallet: %v", err)
	}
	resellerID := creditProfile.ID
	order := model.Order{
		OrderNo: "LLQ-RF-" + uuid.NewString()[:24], ResellerID: &resellerID,
		Email: "buyer@example.com", Status: "delivered", PaymentStatus: "paid", Subtotal: 1000, Total: 1000, ResellerMargin: 100,
	}
	if err := db.Create(&order).Error; err != nil {
		t.Fatalf("create reseller order: %v", err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error { return CreditResellerMarginTx(tx, order) }); err != nil {
		t.Fatalf("credit reseller margin: %v", err)
	}
	// The reseller withdrew the credited profit before the customer refund.
	if err := db.Model(&model.WalletAccount{}).Where("id = ?", wallet.ID).Update("balance", 0).Error; err != nil {
		t.Fatalf("simulate completed withdrawal: %v", err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error { return ReverseResellerMarginTx(tx, order, order.Total) }); err != nil {
		t.Fatalf("reverse reseller margin: %v", err)
	}

	if err := db.First(&creditProfile, "id = ?", creditProfile.ID).Error; err != nil || creditProfile.Status != "suspended" {
		t.Fatalf("credit breach did not suspend reseller: status=%q err=%v", creditProfile.Status, err)
	}
	if err := db.First(&wallet, "id = ?", wallet.ID).Error; err != nil || wallet.Balance != -100 {
		t.Fatalf("refund clawback was not persisted: balance=%d err=%v", wallet.Balance, err)
	}
	var event model.ResellerCreditEvent
	if err := db.Where("reseller_id = ?", creditProfile.ID).First(&event).Error; err != nil {
		t.Fatalf("credit breach event missing: %v", err)
	}
	if event.Exposure != 100 || event.CreditLimit != 50 || event.Action != "auto_suspended" || event.OrderID == nil || *event.OrderID != order.ID {
		t.Fatalf("credit breach event has the wrong snapshot: %#v", event)
	}

	if err := db.Transaction(func(tx *gorm.DB) error { return ReverseResellerMarginTx(tx, order, order.Total) }); err != nil {
		t.Fatalf("idempotent refund retry failed: %v", err)
	}
	var eventCount, reversalCount int64
	if err := db.Model(&model.ResellerCreditEvent{}).Where("reseller_id = ?", creditProfile.ID).Count(&eventCount).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.WalletEntry{}).Where("account_id = ? AND type = ?", wallet.ID, "reseller_margin_reversal").Count(&reversalCount).Error; err != nil {
		t.Fatal(err)
	}
	if eventCount != 1 || reversalCount != 1 {
		t.Fatalf("refund retry duplicated financial records: events=%d reversals=%d", eventCount, reversalCount)
	}
}
