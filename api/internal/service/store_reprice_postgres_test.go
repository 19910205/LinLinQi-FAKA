package service

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"

	"linlinqi/api/internal/model"
)

func TestRepriceStoreCurrencyTxPostgres(t *testing.T) {
	db := orderPostgresTestDB(t)
	tx := db.Begin()
	defer tx.Rollback()

	var usd, cny model.CurrencyDefinition
	if err := tx.Where("code = ?", "USD").First(&usd).Error; err != nil {
		t.Fatal(err)
	}
	if err := tx.Where("code = ?", "CNY").First(&cny).Error; err != nil {
		t.Fatal(err)
	}
	suffix := uuid.NewString()[:8]
	category := model.Category{Name: "FX " + suffix, Slug: "fx-" + suffix, Enabled: true}
	if err := tx.Create(&category).Error; err != nil {
		t.Fatal(err)
	}
	product := model.Product{CategoryID: category.ID, Name: "FX product", Slug: "fx-product-" + suffix, Price: 100, ComparePrice: 120, CostPrice: 80, Currency: "USD", Status: "on_sale", DeliveryType: "auto", InventoryMode: "local"}
	if err := tx.Create(&product).Error; err != nil {
		t.Fatal(err)
	}
	variant := model.ProductVariant{ProductID: product.ID, SKU: "FX-" + suffix, Name: "FX variant", Price: 100, ComparePrice: 120, CostPrice: 80, Status: "active"}
	tier := model.ProductPriceTier{ProductID: product.ID, MinQuantity: 2, UnitPrice: 90}
	promotionRules, _ := json.Marshal(storePromotionRule{Amount: 20, MinAmount: 100, MaxDiscount: 30, UnitPrice: 85})
	promotion := model.Promotion{Name: "FX promotion", Code: "FXPROMO_" + suffix, Type: "fixed", Currency: "USD", Rules: string(promotionRules), StartsAt: time.Now().Add(-time.Hour), EndsAt: time.Now().Add(time.Hour), Status: "active"}
	coupon := model.Coupon{Code: "FXCOUPON_" + suffix, Type: "fixed", Currency: "USD", Value: 50, MinAmount: 100, StartsAt: time.Now().Add(-time.Hour), EndsAt: time.Now().Add(time.Hour), Enabled: true}
	level := model.MemberLevel{Code: "fx_" + suffix, Name: "FX level", Currency: "USD", MinimumSpend: 10000, Enabled: true}
	user := model.User{Email: "fx-" + suffix + "@example.test", PasswordHash: "not-used", Status: "active"}
	if err := tx.Create(&variant).Error; err != nil {
		t.Fatal(err)
	}
	if err := tx.Create(&tier).Error; err != nil {
		t.Fatal(err)
	}
	if err := tx.Create(&promotion).Error; err != nil {
		t.Fatal(err)
	}
	if err := tx.Create(&coupon).Error; err != nil {
		t.Fatal(err)
	}
	if err := tx.Create(&level).Error; err != nil {
		t.Fatal(err)
	}
	if err := tx.Create(&user).Error; err != nil {
		t.Fatal(err)
	}
	reseller := model.ResellerProfile{UserID: user.ID, Name: "FX reseller", Code: "FXR-" + suffix, Status: "active", AppliedAt: time.Now()}
	if err := tx.Create(&reseller).Error; err != nil {
		t.Fatal(err)
	}
	rule := model.ResellerProductRule{ResellerID: reseller.ID, ProductID: product.ID, Enabled: true, PricingMode: "fixed", Currency: "USD", FixedPrice: 120}
	policy := model.ResellerCreditPolicy{ResellerID: reseller.ID, Currency: "USD", CreditLimit: 10000}
	if err := tx.Create(&rule).Error; err != nil {
		t.Fatal(err)
	}
	if err := tx.Create(&policy).Error; err != nil {
		t.Fatal(err)
	}

	snapshot := model.FXRateSnapshot{BaseCode: "USD", QuoteCode: "CNY", Rate: "7.0267"}
	result, err := RepriceStoreCurrencyTx(tx, usd, cny, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if result.Products != 1 || result.Variants != 1 || result.PriceTiers != 1 || result.Coupons != 1 || result.Promotions != 1 || result.MemberLevels != 1 || result.ResellerRules != 1 || result.ResellerCreditPolicies != 1 {
		t.Fatalf("unexpected repricing counts: %+v", result)
	}
	if err := tx.First(&product, "id = ?", product.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := tx.First(&variant, "id = ?", variant.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := tx.First(&tier, "id = ?", tier.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := tx.First(&coupon, "id = ?", coupon.ID).Error; err != nil {
		t.Fatal(err)
	}
	if err := tx.First(&rule, "id = ?", rule.ID).Error; err != nil {
		t.Fatal(err)
	}
	if product.Currency != "CNY" || product.Price != 703 || variant.Price != 703 || tier.UnitPrice != 632 || coupon.Currency != "CNY" || coupon.Value != 351 || rule.Currency != "CNY" || rule.FixedPrice != 843 {
		t.Fatalf("unexpected converted values: product=%+v variant=%+v tier=%+v coupon=%+v rule=%+v", product, variant, tier, coupon, rule)
	}
	var targetPolicy model.ResellerCreditPolicy
	if err := tx.Where("reseller_id = ? AND currency = ?", reseller.ID, "CNY").First(&targetPolicy).Error; err != nil {
		t.Fatal(err)
	}
	if targetPolicy.CreditLimit != 70267 {
		t.Fatalf("converted credit limit = %d, want 70267", targetPolicy.CreditLimit)
	}
}
