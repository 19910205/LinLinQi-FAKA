package handler

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"

	"linlinqi/api/internal/model"
	"linlinqi/api/internal/queue"
)

func TestSupplierCatalogImportFencingRollsBackCatalogTransactionPostgreSQL(t *testing.T) {
	db := isolatedSupplyAdminDB(t, "linlinqi_supplier_import_fence_test_")
	currencyDefinition := model.CurrencyDefinition{
		Code: "CNY", NumericCode: "156", Name: "Renminbi", Symbol: "¥",
		MinorUnit: 2, Enabled: true, Settlement: true,
	}
	if err := db.Where("code = ?", currencyDefinition.Code).Assign(currencyDefinition).FirstOrCreate(&currencyDefinition).Error; err != nil {
		t.Fatalf("create currency: %v", err)
	}
	if err := db.Where("key = ?", "store_currency").Assign(model.Setting{Value: "CNY", Group: "currency"}).FirstOrCreate(&model.Setting{Key: "store_currency"}).Error; err != nil {
		t.Fatalf("create store currency setting: %v", err)
	}
	category := model.Category{Name: "Imported", Slug: "imported-" + uuid.NewString(), Enabled: true}
	if err := db.Create(&category).Error; err != nil {
		t.Fatalf("create category: %v", err)
	}
	supplierModel := model.Supplier{
		Name: "Fence supplier", Code: "fence-" + uuid.NewString(), BaseURL: "https://supplier.example.com",
		Protocol: "linlinqi-standard", Status: "active", BalanceCurrency: "CNY",
		PriceCurrency: "CNY", PriceMinorUnit: 2, CurrencyMode: "manual", SyncIntervalMinutes: 15,
	}
	if err := db.Create(&supplierModel).Error; err != nil {
		t.Fatalf("create supplier: %v", err)
	}
	remote := model.SupplierCatalogProduct{
		SupplierID: supplierModel.ID, ExternalID: "商品:fence-1", Name: "Fenced product",
		Currency: "CNY", Price: 1000, Stock: 5, StockStatus: "in_stock",
		Minimum: 1, FulfillmentType: "auto", Status: "active",
		ImageURLs: json.RawMessage(`[]`), Tags: json.RawMessage(`[]`),
		WholesalePrices: json.RawMessage(`{}`), Variants: json.RawMessage(`[]`),
		InputFields: json.RawMessage(`[]`), RawSnapshot: json.RawMessage(`{}`), SnapshotHash: "fence-snapshot",
	}
	if err := db.Create(&remote).Error; err != nil {
		t.Fatalf("create remote product: %v", err)
	}
	request := supplierImportRequest{
		ExternalProductIDs: []string{remote.ExternalID}, CategoryMode: "target",
		TargetCategoryID: &category.ID, PriceMode: "fixed_markup", SyncPrice: true, SyncStock: true,
	}
	snapshot, _ := json.Marshal(supplierImportJobSnapshot{Request: request, ChangeReason: "fencing regression"})
	oldTaskID := "supplier-catalog-import-old-token"
	job := model.SupplierCatalogImportJob{
		SupplierID: supplierModel.ID, TaskID: oldTaskID, Status: "running", Attempts: 1,
		RequestedCount: 1, RequestSnapshot: snapshot, ResultSnapshot: json.RawMessage(`{}`),
	}
	if err := db.Create(&job).Error; err != nil {
		t.Fatalf("create import job: %v", err)
	}

	swapped := false
	processor := Handler{DB: db}
	err := processor.ProcessSupplierCatalogImportJob(context.Background(), job.ID, oldTaskID, func(imported, skipped int) {
		if swapped {
			return
		}
		swapped = true
		result := db.Model(&model.SupplierCatalogImportJob{}).
			Where("id = ? AND task_id = ?", job.ID, oldTaskID).
			Updates(map[string]any{"status": "retrying", "task_id": "replacement-token"})
		if result.Error != nil || result.RowsAffected != 1 {
			t.Fatalf("replace fencing token: rows=%d err=%v", result.RowsAffected, result.Error)
		}
	})
	if !errors.Is(err, queue.ErrSupplierCatalogImportClaimLost) {
		t.Fatalf("processor error = %v, want fencing claim lost", err)
	}
	if !swapped {
		t.Fatal("test did not replace the fencing token during processing")
	}
	var products, mappings, supplierProducts int64
	if err := db.Model(&model.Product{}).Where("category_id = ? AND inventory_mode = ?", category.ID, "supplier").Count(&products).Error; err != nil {
		t.Fatalf("count products: %v", err)
	}
	if err := db.Model(&model.ProductMapping{}).Where("supplier_id = ?", supplierModel.ID).Count(&mappings).Error; err != nil {
		t.Fatalf("count mappings: %v", err)
	}
	if err := db.Model(&model.SupplierProduct{}).Where("supplier_id = ?", supplierModel.ID).Count(&supplierProducts).Error; err != nil {
		t.Fatalf("count supplier products: %v", err)
	}
	if products != 0 || mappings != 0 || supplierProducts != 0 {
		t.Fatalf("stale token committed catalog rows: products=%d mappings=%d supplier_products=%d", products, mappings, supplierProducts)
	}
}
