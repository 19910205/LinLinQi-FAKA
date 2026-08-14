package queue

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"linlinqi/api/internal/model"
	"linlinqi/api/internal/supply"
)

func TestSupplierCategoryPolicyHonorsOperatorTombstonePostgreSQL(t *testing.T) {
	db := isolatedSupplierWorkerDB(t, "linlinqi_supplier_category_tombstone_test_")
	supplierModel := model.Supplier{
		Base: model.Base{ID: uuid.New()}, Name: "Category supplier", Code: "category-supplier",
		BaseURL: "https://supplier.example.test", Protocol: "linlinqi-standard", Status: "active",
	}
	if err := db.Create(&supplierModel).Error; err != nil {
		t.Fatalf("create supplier: %v", err)
	}
	category := model.Category{Name: "Original category", Slug: "original-category", Enabled: true}
	if err := db.Create(&category).Error; err != nil {
		t.Fatalf("create local category: %v", err)
	}
	mapping := model.SupplierCategoryMapping{
		SupplierID: supplierModel.ID, ExternalCategoryID: "remote-category", CategoryID: &category.ID,
		ExternalCategoryName: "Remote category", PriceMode: "fixed_markup", MarkupCurrency: "CNY", Enabled: true,
	}
	if err := db.Create(&mapping).Error; err != nil {
		t.Fatalf("create category binding: %v", err)
	}
	if err := db.Delete(&mapping).Error; err != nil {
		t.Fatalf("delete category binding: %v", err)
	}
	worker := Worker{db: db}
	policy := model.SupplierSyncPolicy{AutoSyncCategories: true, AutoCreateCategories: true}
	run := model.SupplierSyncRun{SupplierID: supplierModel.ID}
	remote := []supply.Category{{ExternalID: "remote-category", Name: "Remote category", Status: "active"}}
	if err := db.Transaction(func(tx *gorm.DB) error {
		_, err := worker.applySupplierCategoryPolicy(tx, supplierModel, policy, remote, &run)
		return err
	}); err != nil {
		t.Fatalf("apply category policy: %v", err)
	}
	var activeMappings, allMappings, localCategories int64
	if err := db.Model(&model.SupplierCategoryMapping{}).Where("supplier_id = ?", supplierModel.ID).Count(&activeMappings).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Unscoped().Model(&model.SupplierCategoryMapping{}).Where("supplier_id = ?", supplierModel.ID).Count(&allMappings).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Model(&model.Category{}).Count(&localCategories).Error; err != nil {
		t.Fatal(err)
	}
	if activeMappings != 0 || allMappings != 1 || localCategories != 1 {
		t.Fatalf("operator tombstone was revived: active=%d all=%d categories=%d", activeMappings, allMappings, localCategories)
	}

	policy.AutoCreateCategories = false
	remote = []supply.Category{{ExternalID: "manual-only", Name: "Manual only", Status: "active"}}
	if err := db.Transaction(func(tx *gorm.DB) error {
		_, err := worker.applySupplierCategoryPolicy(tx, supplierModel, policy, remote, &run)
		return err
	}); err != nil {
		t.Fatalf("unmapped category with auto-create disabled must be skipped: %v", err)
	}
}

func TestActiveSupplierCategoryMappingRejectsDisabledOrDeletedTargetPostgreSQL(t *testing.T) {
	db := isolatedSupplierWorkerDB(t, "linlinqi_supplier_category_status_test_")
	supplierModel := model.Supplier{
		Base: model.Base{ID: uuid.New()}, Name: "Category status supplier", Code: "category-status-supplier",
		BaseURL: "https://supplier.example.test", Protocol: "linlinqi-standard", Status: "active",
	}
	if err := db.Create(&supplierModel).Error; err != nil {
		t.Fatalf("create supplier: %v", err)
	}
	category := model.Category{Name: "Target", Slug: "category-target", Enabled: true}
	if err := db.Create(&category).Error; err != nil {
		t.Fatalf("create category: %v", err)
	}
	mapping := model.SupplierCategoryMapping{
		SupplierID: supplierModel.ID, ExternalCategoryID: "remote-target", CategoryID: &category.ID,
		PriceMode: "fixed_markup", MarkupCurrency: "CNY", Enabled: true,
	}
	if err := db.Create(&mapping).Error; err != nil {
		t.Fatalf("create mapping: %v", err)
	}
	if _, err := activeSupplierCategoryMapping(db, supplierModel.ID, mapping.ExternalCategoryID); err != nil {
		t.Fatalf("live mapping rejected: %v", err)
	}
	if err := db.Model(&mapping).UpdateColumn("enabled", false).Error; err != nil {
		t.Fatalf("disable mapping: %v", err)
	}
	if _, err := activeSupplierCategoryMapping(db, supplierModel.ID, mapping.ExternalCategoryID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("disabled mapping remained active: %v", err)
	}
	if err := db.Model(&mapping).UpdateColumn("enabled", true).Error; err != nil {
		t.Fatalf("enable mapping: %v", err)
	}
	if err := db.Delete(&category).Error; err != nil {
		t.Fatalf("delete target category: %v", err)
	}
	if _, err := activeSupplierCategoryMapping(db, supplierModel.ID, mapping.ExternalCategoryID); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("mapping to deleted category remained active: %v", err)
	}
}
