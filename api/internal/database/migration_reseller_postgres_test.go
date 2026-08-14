package database

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"linlinqi/api/internal/model"
)

func TestMigration022NormalizesSupplierBalanceAndSeedsExplicitLegacyTierPostgreSQL(t *testing.T) {
	db := migrationPostgresTestDB(t)

	supplier := model.Supplier{
		Name: "Legacy balance supplier", Code: "legacy-balance-" + uuid.NewString(), BaseURL: "https://supplier.example", Balance: 100, BalanceCurrency: "CNY",
	}
	if err := db.Create(&supplier).Error; err != nil {
		t.Fatalf("create supplier: %v", err)
	}
	profile := model.ResellerProfile{
		UserID: uuid.New(), Name: "Legacy reseller", Code: "legacy-reseller-" + uuid.NewString(), Status: "suspended", WholesaleLevel: 3,
	}
	if err := db.Create(&profile).Error; err != nil {
		t.Fatalf("create legacy reseller: %v", err)
	}
	if err := db.Unscoped().Where("level = ?", 3).Delete(&model.ResellerWholesaleTier{}).Error; err != nil {
		t.Fatalf("remove pre-existing tier: %v", err)
	}

	statements := []string{
		`DELETE FROM linlinqi_schema_migrations WHERE version = '20260809_022_reseller_business_policy'`,
		`ALTER TABLE suppliers DROP CONSTRAINT IF EXISTS chk_supplier_balance_nonnegative`,
		`ALTER TABLE suppliers DROP CONSTRAINT IF EXISTS chk_supplier_balance_currency`,
		`ALTER TABLE reseller_profiles DROP CONSTRAINT IF EXISTS chk_reseller_profile_credit_limit`,
		`ALTER TABLE reseller_profiles DROP CONSTRAINT IF EXISTS chk_reseller_profile_wholesale_level`,
		`ALTER TABLE reseller_wholesale_tiers DROP CONSTRAINT IF EXISTS chk_reseller_wholesale_tier_level`,
		`ALTER TABLE reseller_wholesale_tiers DROP CONSTRAINT IF EXISTS chk_reseller_wholesale_tier_discount`,
		`ALTER TABLE reseller_credit_events DROP CONSTRAINT IF EXISTS chk_reseller_credit_event_snapshot`,
	}
	for _, statement := range statements {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("prepare simulated migration 021 database: %v", err)
		}
	}
	syncedAt := time.Now().UTC()
	if err := db.Model(&supplier).Updates(map[string]any{"balance": -100, "balance_currency": "USD", "balance_synced_at": syncedAt}).Error; err != nil {
		t.Fatalf("write invalid legacy supplier cache: %v", err)
	}

	if err := Migrate(db); err != nil {
		t.Fatalf("apply migration 022 to simulated legacy schema: %v", err)
	}
	var normalizedSupplier model.Supplier
	if err := db.First(&normalizedSupplier, "id = ?", supplier.ID).Error; err != nil {
		t.Fatalf("reload normalized supplier: %v", err)
	}
	if normalizedSupplier.Balance != 0 || normalizedSupplier.BalanceCurrency != "CNY" || normalizedSupplier.BalanceSyncedAt != nil {
		t.Fatalf("invalid supplier cache was not safely invalidated: %#v", normalizedSupplier)
	}
	var tier model.ResellerWholesaleTier
	if err := db.Where("level = ?", 3).First(&tier).Error; err != nil {
		t.Fatalf("legacy wholesale tier was not materialized: %v", err)
	}
	if tier.DiscountBasisPoint != 0 || !tier.Enabled {
		t.Fatalf("migration invented a discount for a legacy level: %#v", tier)
	}

	silent := db.Session(&gorm.Session{Logger: logger.Default.LogMode(logger.Silent)})
	if err := silent.Model(&normalizedSupplier).Update("balance", -1).Error; err == nil {
		t.Fatal("negative supplier balance bypassed migration 022 constraint")
	}
	if err := silent.Model(&normalizedSupplier).Update("balance_currency", "USD").Error; err == nil {
		t.Fatal("non-CNY supplier balance bypassed migration 022 constraint")
	}
	if err := Migrate(db); err != nil {
		t.Fatalf("idempotent migration rerun failed: %v", err)
	}
}
