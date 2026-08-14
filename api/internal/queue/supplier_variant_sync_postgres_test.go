package queue

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"gorm.io/gorm"

	"linlinqi/api/internal/config"
	"linlinqi/api/internal/model"
	"linlinqi/api/internal/security"
	"linlinqi/api/internal/supply"
)

// LinLinQi Standard flattens upstream SKUs into independent purchasable
// products, so the worker must create one local product/mapping per SKU and
// retire the mapping when the upstream SKU disappears.
func TestSupplierSyncCreatesAndRetiresVariantMappingsPostgreSQL(t *testing.T) {
	db := isolatedSupplierWorkerDB(t, "linlinqi_supplier_variants_test_")
	withVariants := atomic.Bool{}
	withVariants.Store(true)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/openapi/v1/categories":
			_, _ = writer.Write([]byte(`{"code":0,"message":"ok","data":[{"external_id":"cat-1","name":"2FA","status":"active"}]}`))
		case "/openapi/v1/products":
			variants := `[{"id":"v-l","external_id":"v-l","external_sku":"V-L","name":"Large","price":150,"stock":3,"maximum":2,"status":"active"},{"id":"v-s","external_id":"v-s","external_sku":"V-S","name":"Small","price":120,"stock":8,"status":"active"}]`
			if !withVariants.Load() {
				variants = `[{"id":"v-s","external_id":"v-s","external_sku":"V-S","name":"Small","price":120,"stock":8,"status":"active"}]`
			}
			_, _ = writer.Write([]byte(`{"code":0,"message":"ok","data":[{"id":"parent-id","external_id":"parent-id","name":"2FA Starter","price":100,"stock":5,"minimum":1,"maximum":3,"external_category_id":"cat-1","variants":` + variants + `,"status":"active"}]}`))
		case "/openapi/v1/account/balance":
			_, _ = writer.Write([]byte(`{"code":0,"message":"ok","data":{"balance":100,"currency":"USD","updated_at":"` + time.Now().UTC().Format(time.RFC3339) + `"}}`))
		default:
			writer.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	vault, err := security.NewVault("supplier-variant-integration-encryption-key")
	if err != nil {
		t.Fatalf("create vault: %v", err)
	}
	supplierModel := model.Supplier{
		Base: model.Base{ID: uuid.New()}, Name: "Variant Supplier", Code: "variant-supplier",
		BaseURL: server.URL, Protocol: "linlinqi-standard", Status: "active",
		PriceCurrency: "USD", CurrencyMode: "auto", BalanceCurrency: "USD",
	}
	supplierModel.APIKeyCipher, supplierModel.APIKeyNonce, _, err = vault.Encrypt("variant-api-key", append(supplierModel.ID[:], []byte("api-key")...))
	if err != nil {
		t.Fatalf("encrypt API key: %v", err)
	}
	supplierModel.APISecretCipher, supplierModel.APISecretNonce, _, err = vault.Encrypt("variant-api-secret-value", append(supplierModel.ID[:], []byte("api-secret")...))
	if err != nil {
		t.Fatalf("encrypt API secret: %v", err)
	}
	if err := db.Create(&supplierModel).Error; err != nil {
		t.Fatalf("create supplier: %v", err)
	}
	policy := model.SupplierSyncPolicy{
		SupplierID: supplierModel.ID, AutoSyncCategories: true, AutoCreateCategories: true,
		AutoSyncProducts: true, AutoCreateProducts: true, SyncTitle: true,
		SyncVariants: true, SyncPrice: true, SyncStock: true, MissingProductAction: "keep",
	}
	if err := db.Create(&policy).Error; err != nil {
		t.Fatalf("create sync policy: %v", err)
	}
	worker := Worker{db: db, vault: vault, cfg: config.Config{Env: "test"}}

	runSync := func() {
		t.Helper()
		payload, _ := json.Marshal(map[string]string{"supplier_id": supplierModel.ID.String(), "trigger": "manual"})
		if err := worker.handleSupplierSync(context.Background(), asynq.NewTask(TypeSupplierSync, payload)); err != nil {
			t.Fatalf("sync supplier: %v", err)
		}
	}
	runSync()

	var products []model.Product
	if err := db.Where("category_id IN (SELECT id FROM categories WHERE slug LIKE 'variant-supplier-%')").Order("name ASC").Find(&products).Error; err != nil {
		t.Fatalf("auto-created products missing: %v", err)
	}
	if len(products) != 2 {
		t.Fatalf("expected 2 local products for flattened SKUs, got %d", len(products))
	}
	var mappings int64
	if err := db.Model(&model.ProductMapping{}).Where("supplier_id = ?", supplierModel.ID).Count(&mappings).Error; err != nil {
		t.Fatalf("count mappings: %v", err)
	}
	if mappings != 2 {
		t.Fatalf("expected 2 mappings, got %d", mappings)
	}
	var snapshots int64
	if err := db.Model(&model.SupplierProduct{}).Where("supplier_id = ?", supplierModel.ID).Count(&snapshots).Error; err != nil {
		t.Fatalf("count supplier products: %v", err)
	}
	if snapshots != 2 {
		t.Fatalf("expected 2 supplier snapshots, got %d", snapshots)
	}
	var variantMapping model.ProductMapping
	if err := db.Where("supplier_id = ? AND external_product_id = ?", supplierModel.ID, "v-l").First(&variantMapping).Error; err != nil {
		t.Fatalf("variant mapping missing: %v", err)
	}
	if !variantMapping.AutoSyncVariants || !variantMapping.AutoSyncPrice || !variantMapping.AutoSyncStock {
		t.Fatalf("variant mapping did not inherit sync switches: %#v", variantMapping)
	}
	var run model.SupplierSyncRun
	if err := db.Where("supplier_id = ?", supplierModel.ID).Order("started_at DESC").First(&run).Error; err != nil {
		t.Fatalf("load sync run: %v", err)
	}
	if run.Status != "succeeded" || run.CategoriesSeen != 1 || run.ProductsSeen != 2 || run.ProductsMade != 2 {
		t.Fatalf("sync run counters are inconsistent: %#v", run)
	}

	withVariants.Store(false)
	runSync()
	var retired model.ProductMapping
	if err := db.Where("supplier_id = ? AND external_product_id = ?", supplierModel.ID, "v-l").First(&retired).Error; err != nil {
		t.Fatalf("retired mapping missing: %v", err)
	}
	if retired.LastError == "" {
		t.Fatalf("vanished upstream SKU must record a missing error")
	}
	var kept model.ProductMapping
	if err := db.Where("supplier_id = ? AND external_product_id = ?", supplierModel.ID, "v-s").First(&kept).Error; err != nil {
		t.Fatalf("kept mapping missing: %v", err)
	}
	if kept.LastError != "" || kept.LastSyncedAt == nil {
		t.Fatalf("kept mapping must stay healthy: %#v", kept)
	}
}

// Some upstream protocols expose nested variants on a single product. The
// worker must then create local SKUs/mappings and retire vanished SKUs.
func TestEnsureSupplierVariantMappingsCreatesAndRetiresNestedVariantsPostgreSQL(t *testing.T) {
	db := isolatedSupplierWorkerDB(t, "linlinqi_supplier_nested_variants_test_")
	category := model.Category{Name: "Nested", Slug: "nested-variants", Enabled: true}
	if err := db.Create(&category).Error; err != nil {
		t.Fatalf("create category: %v", err)
	}
	supplierModel := model.Supplier{
		Base: model.Base{ID: uuid.New()}, Name: "Nested Supplier", Code: "nested-supplier",
		BaseURL: "https://supplier.example.test", Protocol: "acg-faka-new", Status: "active",
		PriceCurrency: "USD", CurrencyMode: "auto", BalanceCurrency: "USD",
	}
	if err := db.Create(&supplierModel).Error; err != nil {
		t.Fatalf("create supplier: %v", err)
	}
	product := model.Product{
		CategoryID: category.ID, Name: "Nested parent", Slug: "nested-parent", Price: 0,
		DeliveryType: "auto", InventoryMode: "supplier", Status: "draft",
	}
	if err := db.Create(&product).Error; err != nil {
		t.Fatalf("create product: %v", err)
	}
	parentMapping := model.ProductMapping{
		SupplierID: supplierModel.ID, ProductID: product.ID, ExternalProductID: "parent",
		PriceMode: "fixed_markup", MarkupBasisPoint: 5000, MarkupCurrency: "CNY",
		AutoSyncVariants: true, AutoSyncPrice: true, AutoSyncStock: true,
		AutoSyncTitle: true, AutoSyncStatus: true, AutoSyncLimits: true,
	}
	if err := db.Create(&parentMapping).Error; err != nil {
		t.Fatalf("create parent mapping: %v", err)
	}
	if err := db.Model(&parentMapping).UpdateColumns(map[string]any{
		"auto_sync_variants": true, "auto_sync_price": true, "auto_sync_stock": true,
		"auto_sync_title": true, "auto_sync_status": true, "auto_sync_limits": true,
	}).Error; err != nil {
		t.Fatalf("enable parent mapping switches: %v", err)
	}
	run := model.SupplierSyncRun{SupplierID: supplierModel.ID, Trigger: "manual", Status: "running", Protocol: "acg-faka-new", StartedAt: time.Now().UTC()}
	if err := db.Create(&run).Error; err != nil {
		t.Fatalf("create sync run: %v", err)
	}
	target := model.CurrencyDefinition{Code: "CNY", MinorUnit: 2}
	rates := map[string]supplierPreparedFX{
		"USD": {Source: model.CurrencyDefinition{Code: "USD", MinorUnit: 2}, Target: target, Snapshot: model.FXRateSnapshot{Rate: "7.0267"}},
	}
	worker := Worker{db: db, cfg: config.Config{Env: "test"}}
	upstream := supply.Product{
		ID: "parent", ExternalID: "parent", ExternalCategoryID: "cat-1",
		Name: "Parent", Price: 100, Stock: 5, Minimum: 1, Maximum: 3, Status: "active",
		Variants: []supply.ProductVariant{
			{ID: "v-l", ExternalID: "v-l", ExternalSKU: "V-L", Name: "Large", Price: 150, Stock: 3, Maximum: 2, Status: "active"},
			{ID: "v-s", ExternalID: "v-s", ExternalSKU: "V-S", Name: "Small", Price: 120, Stock: 8, Status: "active"},
		},
	}
	now := time.Now().UTC()
	remote := map[string]supply.Product{}
	if err := db.Transaction(func(tx *gorm.DB) error {
		return worker.ensureSupplierVariantMappings(tx, supplierModel, parentMapping, upstream, target, rates, remote, now, &run)
	}); err != nil {
		t.Fatalf("ensure variant mappings: %v", err)
	}
	var variants []model.ProductVariant
	if err := db.Where("product_id = ?", product.ID).Order("name ASC").Find(&variants).Error; err != nil {
		t.Fatalf("load variants: %v", err)
	}
	if len(variants) != 2 {
		t.Fatalf("expected 2 local variants, got %d", len(variants))
	}
	for _, variant := range variants {
		if variant.Price <= 0 || variant.CostPrice <= 0 || variant.SKU == "" {
			t.Fatalf("variant price/cost/sku not populated: %#v", variant)
		}
	}
	var variantMappings int64
	if err := db.Model(&model.ProductMapping{}).Where("supplier_id = ? AND product_id = ? AND variant_id IS NOT NULL", supplierModel.ID, product.ID).Count(&variantMappings).Error; err != nil {
		t.Fatalf("count variant mappings: %v", err)
	}
	if variantMappings != 2 {
		t.Fatalf("expected 2 variant mappings, got %d", variantMappings)
	}
	var variantSnapshots int64
	if err := db.Model(&model.SupplierProduct{}).Where("supplier_id = ? AND product_id = ? AND variant_id IS NOT NULL", supplierModel.ID, product.ID).Count(&variantSnapshots).Error; err != nil {
		t.Fatalf("count variant snapshots: %v", err)
	}
	if variantSnapshots != 2 {
		t.Fatalf("expected 2 variant snapshots, got %d", variantSnapshots)
	}
	var largeVariant model.ProductVariant
	if err := db.Where("product_id = ? AND name LIKE ?", product.ID, "%Large%").First(&largeVariant).Error; err != nil {
		t.Fatalf("large variant missing: %v", err)
	}
	if largeVariant.Price != 1581 || largeVariant.CostPrice != 1054 {
		t.Fatalf("large variant money conversion is wrong: price=%d cost=%d", largeVariant.Price, largeVariant.CostPrice)
	}

	upstream.Variants = upstream.Variants[1:]
	remote = map[string]supply.Product{}
	if err := db.Transaction(func(tx *gorm.DB) error {
		return worker.ensureSupplierVariantMappings(tx, supplierModel, parentMapping, upstream, target, rates, remote, now, &run)
	}); err != nil {
		t.Fatalf("retire variant mappings: %v", err)
	}
	if err := db.First(&largeVariant, "id = ?", largeVariant.ID).Error; err != nil {
		t.Fatalf("reload retired variant: %v", err)
	}
	if largeVariant.Status != "inactive" {
		t.Fatalf("vanished nested variant must become inactive, got %q", largeVariant.Status)
	}
	var smallVariant model.ProductVariant
	if err := db.Where("product_id = ? AND name LIKE ?", product.ID, "%Small%").First(&smallVariant).Error; err != nil {
		t.Fatalf("small variant missing: %v", err)
	}
	if smallVariant.Status != "active" {
		t.Fatalf("kept nested variant must remain active, got %q", smallVariant.Status)
	}
	if err := db.Model(&model.ProductMapping{}).Where("supplier_id = ? AND product_id = ? AND variant_id IS NOT NULL", supplierModel.ID, product.ID).Count(&variantMappings).Error; err != nil {
		t.Fatalf("recount variant mappings: %v", err)
	}
	if variantMappings != 2 {
		t.Fatalf("variant mappings must be retained after retirement, got %d", variantMappings)
	}
}
