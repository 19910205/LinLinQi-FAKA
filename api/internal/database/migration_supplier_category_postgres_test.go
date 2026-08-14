package database

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"linlinqi/api/internal/model"
)

const supplierCategoryBindingMigrationVersion = "20260811_045_supplier_category_bindings_v1"
const supplierCategoryBindingMigrationChecksum = "a2fb7bd5ce9072aa5d9942357f4e9d5a061463990534b342fe13c72cd739ec51"

func preparePreSupplierCategoryBindingMigration(t *testing.T, priceMode string) (*gorm.DB, *model.SupplierCategoryMapping, *model.SupplierCategory) {
	t.Helper()
	db := migrationPostgresTestDB(t)
	supplierModel := model.Supplier{
		Name: "Legacy category supplier", Code: "legacy-category-" + uuid.NewString(),
		BaseURL: "https://legacy-category.example.test", Protocol: "linlinqi-standard", Status: "active",
		PriceCurrency: "CNY", BalanceCurrency: "CNY",
	}
	if err := db.Create(&supplierModel).Error; err != nil {
		t.Fatalf("create legacy supplier: %v", err)
	}
	localCategory := model.Category{Name: "Legacy local category", Slug: "legacy-local-" + uuid.NewString(), Enabled: true}
	if err := db.Create(&localCategory).Error; err != nil {
		t.Fatalf("create legacy local category: %v", err)
	}
	remoteCategory := model.SupplierCategory{
		SupplierID: supplierModel.ID, ExternalID: "远端-分类-001", Name: "远端分类名称",
		Status: "active", RawSnapshot: []byte(`{}`), SnapshotHash: strings.Repeat("a", 64), LastSeenAt: time.Now().UTC(),
	}
	if err := db.Create(&remoteCategory).Error; err != nil {
		t.Fatalf("create legacy remote category: %v", err)
	}
	statements := []string{
		`DELETE FROM linlinqi_schema_migrations WHERE version = '` + supplierCategoryBindingMigrationVersion + `'`,
		`DROP INDEX IF EXISTS idx_supplier_category_bindings_status_sort`,
		`DROP INDEX IF EXISTS idx_product_mappings_category_policy`,
		`ALTER TABLE supplier_category_mappings DROP CONSTRAINT IF EXISTS chk_supplier_category_binding_operations`,
		`ALTER TABLE supplier_category_mappings DROP CONSTRAINT IF EXISTS chk_supplier_category_price_mode`,
		`ALTER TABLE product_mappings DROP CONSTRAINT IF EXISTS chk_product_mapping_category_policy`,
		`ALTER TABLE supplier_category_mappings DROP COLUMN IF EXISTS external_category_name CASCADE`,
		`ALTER TABLE supplier_category_mappings DROP COLUMN IF EXISTS default_cover_url CASCADE`,
		`ALTER TABLE supplier_category_mappings DROP COLUMN IF EXISTS sync_title CASCADE`,
		`ALTER TABLE supplier_category_mappings DROP COLUMN IF EXISTS sync_parent CASCADE`,
		`ALTER TABLE supplier_category_mappings DROP COLUMN IF EXISTS sync_price CASCADE`,
		`ALTER TABLE supplier_category_mappings DROP COLUMN IF EXISTS sync_stock CASCADE`,
		`ALTER TABLE supplier_category_mappings DROP COLUMN IF EXISTS sort CASCADE`,
		`ALTER TABLE supplier_category_mappings DROP COLUMN IF EXISTS enabled CASCADE`,
		`ALTER TABLE product_mappings DROP COLUMN IF EXISTS supplier_category_mapping_id CASCADE`,
		`ALTER TABLE product_mappings DROP COLUMN IF EXISTS inherit_category_policy CASCADE`,
	}
	for _, statement := range statements {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("prepare pre-045 schema: %v", err)
		}
	}
	mappingID := uuid.New()
	if err := db.Exec(`INSERT INTO supplier_category_mappings (
		id, created_at, updated_at, supplier_id, external_category_id, category_id,
		auto_create, auto_publish, sync_name, sync_description, sync_image, mirror_remote_image,
		price_mode, markup_basis_point, markup_amount, markup_currency, last_error
	) VALUES (?, ?, ?, ?, ?, ?, true, false, true, false, false, true, ?, 1200, 0, 'CNY', '')`,
		mappingID, time.Now().UTC(), time.Now().UTC(), supplierModel.ID, remoteCategory.ExternalID, localCategory.ID, priceMode).Error; err != nil {
		t.Fatalf("insert pre-045 category mapping: %v", err)
	}
	return db, &model.SupplierCategoryMapping{Base: model.Base{ID: mappingID}}, &remoteCategory
}

func TestMigration045UpgradesLegacyCategoryBindingsPostgreSQL(t *testing.T) {
	db, mapping, remoteCategory := preparePreSupplierCategoryBindingMigration(t, "fixed_markup")
	if err := Migrate(db); err != nil {
		t.Fatalf("apply migration 045 to legacy category bindings: %v", err)
	}
	if err := db.First(mapping, "id = ?", mapping.ID).Error; err != nil {
		t.Fatalf("reload migrated category binding: %v", err)
	}
	if mapping.ExternalCategoryName != remoteCategory.Name || !mapping.SyncName || !mapping.SyncTitle || !mapping.SyncParent {
		t.Fatalf("legacy category metadata was not preserved: %#v", mapping)
	}
	if !mapping.SyncPrice || !mapping.SyncStock || !mapping.Enabled || mapping.Sort != 0 {
		t.Fatalf("safe operational defaults were not restored: %#v", mapping)
	}
	var applied schemaMigration
	if err := db.First(&applied, "version = ?", supplierCategoryBindingMigrationVersion).Error; err != nil {
		t.Fatalf("load migration 045 record: %v", err)
	}
	if applied.Checksum != supplierCategoryBindingMigrationChecksum {
		t.Fatalf("migration 045 checksum = %q, want %q", applied.Checksum, supplierCategoryBindingMigrationChecksum)
	}
	var productPolicyColumns int64
	if err := db.Raw(`SELECT COUNT(*) FROM information_schema.columns
		WHERE table_schema = current_schema() AND table_name = 'product_mappings'
		  AND column_name IN ('supplier_category_mapping_id', 'inherit_category_policy')`).Scan(&productPolicyColumns).Error; err != nil {
		t.Fatalf("inspect migrated product policy columns: %v", err)
	}
	if productPolicyColumns != 2 {
		t.Fatalf("product policy provenance columns = %d, want 2", productPolicyColumns)
	}
	if err := Migrate(db); err != nil {
		t.Fatalf("idempotent migration 045 rerun failed: %v", err)
	}
}

func TestMigration045RejectsUnexecutableLegacyFixedPriceBindingsPostgreSQL(t *testing.T) {
	db, _, _ := preparePreSupplierCategoryBindingMigration(t, "fixed_price")
	err := Migrate(db)
	if err == nil || !strings.Contains(err.Error(), "active fixed_price category mappings") {
		t.Fatalf("migration 045 did not block legacy fixed_price binding: %v", err)
	}
	var applied int64
	if countErr := db.Model(&schemaMigration{}).Where("version = ?", supplierCategoryBindingMigrationVersion).Count(&applied).Error; countErr != nil {
		t.Fatalf("count rejected migration record: %v", countErr)
	}
	if applied != 0 {
		t.Fatalf("rejected migration 045 was recorded %d times", applied)
	}
}
