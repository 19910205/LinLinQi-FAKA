package handler

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"linlinqi/api/internal/config"
	"linlinqi/api/internal/database"
	"linlinqi/api/internal/model"
	"linlinqi/api/internal/security"
)

func isolatedSupplyAdminDB(t *testing.T, prefix string) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("LINLINQI_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set LINLINQI_TEST_DATABASE_URL to run PostgreSQL supply admin integration tests")
	}
	adminDB, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("connect PostgreSQL: %v", err)
	}
	schemaName := prefix + strings.ReplaceAll(uuid.NewString(), "-", "")
	if err := adminDB.Exec(`CREATE SCHEMA "` + schemaName + `"`).Error; err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		_ = adminDB.Exec(`DROP SCHEMA "` + schemaName + `" CASCADE`).Error
		if sqlDB, dbErr := adminDB.DB(); dbErr == nil {
			_ = sqlDB.Close()
		}
	})
	parsedDSN, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse DSN: %v", err)
	}
	query := parsedDSN.Query()
	query.Set("search_path", schemaName)
	parsedDSN.RawQuery = query.Encode()
	db, err := gorm.Open(postgres.Open(parsedDSN.String()), &gorm.Config{})
	if err != nil {
		t.Fatalf("open isolated schema: %v", err)
	}
	if err := database.Migrate(db); err != nil {
		t.Fatalf("migrate isolated schema: %v", err)
	}
	return db
}

func TestCreateSupplySupplierWritesSingleResponsePostgreSQL(t *testing.T) {
	db := isolatedSupplyAdminDB(t, "linlinqi_supply_admin_test_")
	vault, err := security.NewVault("supply-admin-integration-encryption-key-2026")
	if err != nil {
		t.Fatalf("create vault: %v", err)
	}

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest("POST", "/admin/v1/suppliers", strings.NewReader(`{"name":"Test Supplier","code":"single-response","base_url":"https://example.com","api_key":"supplier-key-001","api_secret":"supplier-secret-value-001","protocol":"linlinqi-standard","price_currency":"USD","price_minor_unit":2,"balance_currency":"USD"}`))
	context.Request.Header.Set("Content-Type", "application/json")
	context.Request.Header.Set("X-Change-Reason", "integration test supplier creation")
	Handler{DB: db, Cfg: config.Config{Env: "production"}, Vault: vault}.CreateSupplySupplier(context)
	if recorder.Code != 201 {
		t.Fatalf("create supplier status = %d, body = %s", recorder.Code, recorder.Body.String())
	}
	decoder := json.NewDecoder(strings.NewReader(recorder.Body.String()))
	var envelope map[string]any
	if err := decoder.Decode(&envelope); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if err := decoder.Decode(&map[string]any{}); err != io.EOF {
		t.Fatalf("handler wrote more than one JSON response: %q (%v)", recorder.Body.String(), err)
	}
	var supplierCount, auditCount int64
	if err := db.Model(&model.Supplier{}).Where("code = ?", "single-response").Count(&supplierCount).Error; err != nil || supplierCount != 1 {
		t.Fatalf("supplier persistence mismatch: count=%d err=%v", supplierCount, err)
	}
	var stored model.Supplier
	if err := db.First(&stored, "code = ?", "single-response").Error; err != nil {
		t.Fatalf("load created supplier: %v", err)
	}
	if stored.Status != "disabled" || stored.HealthStatus != "unknown" || stored.LastProbeAt != nil {
		t.Fatalf("new connection bypassed read-only probe gate: status=%s health=%s probed=%v", stored.Status, stored.HealthStatus, stored.LastProbeAt)
	}
	if err := db.Model(&model.AuditLog{}).Where("action = ?", "supplier.create").Count(&auditCount).Error; err != nil || auditCount != 1 {
		t.Fatalf("supplier audit mismatch: count=%d err=%v", auditCount, err)
	}
}

func TestSupplierConnectionMutationDisablesUntilFreshProbePostgreSQL(t *testing.T) {
	db := isolatedSupplyAdminDB(t, "linlinqi_supplier_probe_gate_test_")
	now := time.Now().UTC().Add(-time.Minute)
	item := model.Supplier{
		Name: "Probe gated supplier", Code: "probe-gated-supplier",
		BaseURL: "https://supplier.example.test", Protocol: "linlinqi-standard",
		Status: "active", HealthStatus: "healthy", LastProbeAt: &now,
		CredentialsCipher: []byte("cipher"), CredentialsNonce: []byte("nonce"),
		PriceCurrency: "CNY", PriceMinorUnit: 2, BalanceCurrency: "CNY", CurrencyMode: "auto",
		SyncIntervalMinutes: 15,
	}
	if err := db.Create(&item).Error; err != nil {
		t.Fatalf("create supplier: %v", err)
	}
	h := Handler{DB: db, Cfg: config.Config{Env: "production"}}
	request := func(body string) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		context, _ := gin.CreateTestContext(recorder)
		context.Params = gin.Params{{Key: "id", Value: item.ID.String()}}
		context.Request = httptest.NewRequest("PATCH", "/admin/v1/suppliers/"+item.ID.String(), strings.NewReader(body))
		context.Request.Header.Set("Content-Type", "application/json")
		context.Request.Header.Set("X-Change-Reason", "verify supplier probe gate")
		h.UpdateSupplySupplier(context)
		return recorder
	}

	if recorder := request(`{"balance_currency":"USD"}`); recorder.Code != 200 {
		t.Fatalf("save connection mutation status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	var reloaded model.Supplier
	if err := db.First(&reloaded, "id = ?", item.ID).Error; err != nil {
		t.Fatalf("reload supplier: %v", err)
	}
	item = reloaded
	if item.Status != "disabled" || item.HealthStatus != "unknown" || item.LastProbeAt != nil || item.LastProbeError != "probe_required" {
		t.Fatalf("connection mutation retained stale activation evidence: %#v", item)
	}
	if recorder := request(`{"status":"active"}`); recorder.Code != 409 {
		t.Fatalf("activation without fresh probe status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	fresh := time.Now().UTC()
	if err := db.Model(&item).Updates(map[string]any{"health_status": "healthy", "last_probe_at": &fresh, "last_probe_error": ""}).Error; err != nil {
		t.Fatalf("persist simulated successful read-only probe: %v", err)
	}
	if recorder := request(`{"status":"active"}`); recorder.Code != 200 {
		t.Fatalf("activation after fresh probe status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	if err := db.First(&item, "id = ?", item.ID).Error; err != nil || item.Status != "active" {
		t.Fatalf("supplier was not activated after healthy probe: status=%s err=%v", item.Status, err)
	}
}

func TestSupplierProbeUsesOnlyReadOperationsPostgreSQL(t *testing.T) {
	db := isolatedSupplyAdminDB(t, "linlinqi_supplier_readonly_probe_test_")
	seen := make(map[string]int)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet {
			t.Errorf("probe performed mutating upstream request: %s %s", request.Method, request.URL.Path)
			writer.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		seen[request.URL.Path]++
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/openapi/v1/account/balance":
			_, _ = writer.Write([]byte(`{"code":0,"message":"ok","data":{"balance":1234,"currency":"CNY","updated_at":"2026-08-11T00:00:00Z"}}`))
		case "/openapi/v1/categories", "/openapi/v1/products":
			_, _ = writer.Write([]byte(`{"code":0,"message":"ok","data":[]}`))
		default:
			t.Errorf("probe called unexpected upstream endpoint: %s", request.URL.Path)
			writer.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	vault, err := security.NewVault("supplier-readonly-probe-test-encryption-key")
	if err != nil {
		t.Fatalf("create vault: %v", err)
	}
	item := model.Supplier{
		Base: model.Base{ID: uuid.New()}, Name: "Read only probe", Code: "read-only-probe",
		BaseURL: server.URL, Protocol: "linlinqi-standard", Status: "disabled",
		PriceCurrency: "CNY", PriceMinorUnit: 2, BalanceCurrency: "CNY", CurrencyMode: "auto",
		HealthStatus: "unknown", SyncIntervalMinutes: 15,
	}
	item.CredentialsCipher, item.CredentialsNonce, err = encryptSupplierCredentials(vault, item.ID, item.Protocol, map[string]string{
		"api_key": "supplier-key-001", "api_secret": "supplier-secret-value-001",
	})
	if err != nil {
		t.Fatalf("encrypt supplier credentials: %v", err)
	}
	if err := db.Create(&item).Error; err != nil {
		t.Fatalf("create supplier: %v", err)
	}

	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Params = gin.Params{{Key: "id", Value: item.ID.String()}}
	context.Request = httptest.NewRequest("POST", "/admin/v1/suppliers/"+item.ID.String()+"/probe", nil)
	context.Request.Header.Set("X-Change-Reason", "verify read only supplier connection")
	Handler{DB: db, Cfg: config.Config{Env: "development"}, Vault: vault}.AdminSupplierProbe(context)
	if recorder.Code != 200 {
		t.Fatalf("probe status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	for _, path := range []string{"/openapi/v1/account/balance", "/openapi/v1/categories", "/openapi/v1/products"} {
		if seen[path] != 1 {
			t.Fatalf("read-only probe endpoint %s calls=%d", path, seen[path])
		}
	}
	if seen["/openapi/v1/orders"] != 0 {
		t.Fatal("connection probe created a test order")
	}
}

func TestSupplierCategoryPolicyImmediatelyUpdatesInheritedProductsPostgreSQL(t *testing.T) {
	db := isolatedSupplyAdminDB(t, "linlinqi_category_policy_propagation_test_")
	supplierModel := model.Supplier{
		Name: "Policy supplier", Code: "policy-supplier", BaseURL: "https://policy-supplier.example.test",
		Protocol: "linlinqi-standard", Status: "active", PriceCurrency: "CNY", BalanceCurrency: "CNY",
	}
	if err := db.Create(&supplierModel).Error; err != nil {
		t.Fatalf("create supplier: %v", err)
	}
	originalCategory := model.Category{Name: "Original", Slug: "policy-original", Enabled: true}
	targetCategory := model.Category{Name: "Target", Slug: "policy-target", Enabled: true}
	if err := db.Create(&originalCategory).Error; err != nil {
		t.Fatalf("create original category: %v", err)
	}
	if err := db.Create(&targetCategory).Error; err != nil {
		t.Fatalf("create target category: %v", err)
	}
	product := model.Product{
		CategoryID: originalCategory.ID, Name: "Inherited product", Slug: "inherited-product",
		Currency: "CNY", Price: 1000, DeliveryType: "auto", InventoryMode: "supplier", Status: "draft",
	}
	if err := db.Create(&product).Error; err != nil {
		t.Fatalf("create product: %v", err)
	}
	binding := model.SupplierCategoryMapping{
		SupplierID: supplierModel.ID, ExternalCategoryID: "remote-policy", CategoryID: &targetCategory.ID,
		PriceMode: "fixed_amount", MarkupAmount: 250, MarkupCurrency: "CNY",
		SyncTitle: true, SyncPrice: true, SyncStock: false, Enabled: true,
	}
	if err := db.Create(&binding).Error; err != nil {
		t.Fatalf("create category binding: %v", err)
	}
	if err := db.Model(&binding).UpdateColumn("sync_stock", false).Error; err != nil {
		t.Fatalf("persist disabled inherited stock switch: %v", err)
	}
	binding.SyncStock = false
	mapping := model.ProductMapping{
		SupplierID: supplierModel.ID, SupplierCategoryMappingID: &binding.ID, InheritCategoryPolicy: true,
		ProductID: product.ID, ExternalProductID: "remote-product", PriceMode: "fixed_markup",
	}
	if err := db.Create(&mapping).Error; err != nil {
		t.Fatalf("create product mapping: %v", err)
	}
	if err := applySupplierCategoryPolicyToInheritedMappings(db, binding); err != nil {
		t.Fatalf("apply category policy: %v", err)
	}
	if err := db.First(&product, "id = ?", product.ID).Error; err != nil {
		t.Fatalf("reload product: %v", err)
	}
	if product.CategoryID != targetCategory.ID {
		t.Fatalf("inherited product category = %s, want %s", product.CategoryID, targetCategory.ID)
	}
	if err := db.First(&mapping, "id = ?", mapping.ID).Error; err != nil {
		t.Fatalf("reload mapping: %v", err)
	}
	if mapping.PriceMode != "fixed_amount" || mapping.MarkupAmount != 250 || !mapping.AutoSyncPrice || mapping.AutoSyncStock || !mapping.AutoSyncTitle {
		t.Fatalf("inherited product policy was not propagated: %#v", mapping)
	}

	binding.Enabled = false
	if err := applySupplierCategoryPolicyToInheritedMappings(db, binding); err != nil {
		t.Fatalf("disable category policy: %v", err)
	}
	if err := db.First(&mapping, "id = ?", mapping.ID).Error; err != nil {
		t.Fatalf("reload disabled mapping: %v", err)
	}
	if mapping.AutoSyncPrice || mapping.AutoSyncStock || mapping.AutoSyncTitle {
		t.Fatalf("disabled category policy remained executable: %#v", mapping)
	}
}

func TestSupplyMoneyDTOSelectContractsPostgreSQL(t *testing.T) {
	db := isolatedSupplyAdminDB(t, "linlinqi_supply_money_contract_test_")
	supplierModel := model.Supplier{
		Base: model.Base{ID: uuid.New()}, Name: "Currency Supplier", Code: "currency-supplier",
		BaseURL: "https://currency-supplier.example.test", Protocol: "linlinqi-standard", Status: "active",
		PriceCurrency: "USD", BalanceCurrency: "USD",
	}
	if err := db.Create(&supplierModel).Error; err != nil {
		t.Fatalf("create supplier: %v", err)
	}
	category := model.Category{Name: "Currency catalog", Slug: "currency-catalog", Enabled: true}
	if err := db.Create(&category).Error; err != nil {
		t.Fatalf("create category: %v", err)
	}
	product := model.Product{
		CategoryID: category.ID, Name: "USD supplier product", Slug: "usd-supplier-product",
		Currency: "USD", Price: 1250, DeliveryType: "auto", InventoryMode: "supplier", Status: "on_sale",
	}
	if err := db.Create(&product).Error; err != nil {
		t.Fatalf("create product: %v", err)
	}
	variant := model.ProductVariant{ProductID: product.ID, SKU: "USD-SUPPLIER-SKU", Name: "USD variant", Price: 1375, Status: "active"}
	if err := db.Create(&variant).Error; err != nil {
		t.Fatalf("create variant: %v", err)
	}
	order := model.Order{
		Base: model.Base{ID: uuid.New()}, OrderNo: "LQ-SUPPLY-CURRENCY-1", Email: "buyer@example.test",
		Status: "processing", PaymentStatus: "paid", Subtotal: 2100, Total: 2100, Currency: "EUR",
	}
	if err := db.Create(&order).Error; err != nil {
		t.Fatalf("create order: %v", err)
	}
	orderItem := model.OrderItem{
		Base: model.Base{ID: uuid.New()}, OrderID: order.ID, ProductID: product.ID,
		ProductName: product.Name, VariantName: variant.Name, UnitPrice: 2100, Currency: "EUR", Quantity: 1,
	}
	if err := db.Create(&orderItem).Error; err != nil {
		t.Fatalf("create order item: %v", err)
	}
	procurement := model.ProcurementOrder{
		Base: model.Base{ID: uuid.New()}, ProcurementNo: "LQP-SUPPLY-CURRENCY-1",
		SupplierID: supplierModel.ID, OrderID: order.ID, OrderItemID: orderItem.ID,
		ExternalProductID: "upstream-usd-product", Quantity: 1, CostAmount: 725, CostCurrency: "CNY",
		UpstreamCostAmount: 100, UpstreamCurrency: "USD", Status: "processing",
	}
	if err := db.Create(&procurement).Error; err != nil {
		t.Fatalf("create procurement: %v", err)
	}

	gin.SetMode(gin.TestMode)
	handler := Handler{DB: db}
	request := func(target string, params gin.Params, invoke func(*gin.Context)) *httptest.ResponseRecorder {
		t.Helper()
		recorder := httptest.NewRecorder()
		context, _ := gin.CreateTestContext(recorder)
		context.Params = params
		context.Request = httptest.NewRequest("GET", target, nil)
		invoke(context)
		return recorder
	}
	catalog := request("/admin/v1/supply/catalog", nil, handler.AdminSupplyCatalog)
	if catalog.Code != 200 || strings.Count(catalog.Body.String(), `"currency":"USD"`) != 2 {
		t.Fatalf("catalog product/variant currency contract mismatch: status=%d body=%s", catalog.Code, catalog.Body.String())
	}
	list := request("/admin/v1/operations/procurements", nil, handler.AdminSupplyProcurements)
	if list.Code != 200 || !strings.Contains(list.Body.String(), `"cost_currency":"CNY"`) || !strings.Contains(list.Body.String(), `"upstream_currency":"USD"`) {
		t.Fatalf("procurement currency SELECT contract mismatch: status=%d body=%s", list.Code, list.Body.String())
	}
	detail := request(
		"/admin/v1/operations/procurements/"+procurement.ID.String(),
		gin.Params{{Key: "id", Value: procurement.ID.String()}},
		handler.AdminSupplyProcurementDetail,
	)
	if detail.Code != 200 || strings.Count(detail.Body.String(), `"currency":"EUR"`) != 2 || !strings.Contains(detail.Body.String(), `"cost_currency":"CNY"`) || !strings.Contains(detail.Body.String(), `"upstream_currency":"USD"`) {
		t.Fatalf("procurement detail money contract mismatch: status=%d body=%s", detail.Code, detail.Body.String())
	}
}

func TestSupplyParameterMappingPersistsUpdatesAndListsPostgreSQL(t *testing.T) {
	db := isolatedSupplyAdminDB(t, "linlinqi_supply_mapping_test_")
	supplier := model.Supplier{Name: "Mapping Supplier", Code: "mapping-supplier", BaseURL: "https://supplier.example.com", Protocol: "linlinqi-standard", Status: "active"}
	if err := db.Create(&supplier).Error; err != nil {
		t.Fatalf("create supplier: %v", err)
	}
	category := model.Category{Name: "Mapping", Slug: "mapping", Enabled: true}
	if err := db.Create(&category).Error; err != nil {
		t.Fatalf("create category: %v", err)
	}
	product := model.Product{CategoryID: category.ID, Name: "Mapping Product", Slug: "mapping-product", Price: 100, CostPrice: 50, DeliveryType: "auto", InventoryMode: "supplier", Status: "on_sale"}
	if err := db.Create(&product).Error; err != nil {
		t.Fatalf("create product: %v", err)
	}
	fields := []model.ProductInputField{
		{ProductID: product.ID, Key: "account_email", Label: "Account email", InputType: "email", Required: true, Sensitive: true, PassToSupplier: true, Options: json.RawMessage(`[]`), MinLength: 3, MaxLength: 190, Enabled: true, Sort: 30},
		{ProductID: product.ID, Key: "region", Label: "Region", InputType: "text", PassToSupplier: true, Options: json.RawMessage(`[]`), MaxLength: 100, Enabled: true, Sort: 25},
		{ProductID: product.ID, Key: "internal_note", Label: "Internal note", InputType: "text", PassToSupplier: false, Options: json.RawMessage(`[]`), MaxLength: 200, Enabled: true, Sort: 20},
		{ProductID: product.ID, Key: "legacy_account", Label: "Legacy account", InputType: "text", PassToSupplier: true, Options: json.RawMessage(`[]`), MaxLength: 200, Enabled: false, Sort: 10},
	}
	if err := db.Create(&fields).Error; err != nil {
		t.Fatalf("create checkout fields: %v", err)
	}
	// GORM applies the model's default:true tag to a zero bool on Create; make
	// the disabled fixture explicit so this test exercises the catalog filter.
	if err := db.Model(&fields[3]).Update("enabled", false).Error; err != nil {
		t.Fatalf("disable legacy checkout field fixture: %v", err)
	}
	handler := Handler{DB: db}
	request := func(method, target, body string, invoke func(*gin.Context)) *httptest.ResponseRecorder {
		t.Helper()
		recorder := httptest.NewRecorder()
		context, _ := gin.CreateTestContext(recorder)
		context.Request = httptest.NewRequest(method, target, strings.NewReader(body))
		context.Request.Header.Set("Content-Type", "application/json")
		if method != "GET" {
			context.Request.Header.Set("X-Change-Reason", "integration test mapping change")
		}
		invoke(context)
		return recorder
	}

	catalogResponse := request("GET", "/admin/v1/supply/catalog", "", handler.AdminSupplyCatalog)
	if catalogResponse.Code != 200 {
		t.Fatalf("catalog status = %d, body = %s", catalogResponse.Code, catalogResponse.Body.String())
	}
	catalogBody := catalogResponse.Body.String()
	for _, required := range []string{`"input_fields"`, `"account_email"`, `"internal_note"`, `"pass_to_supplier"`} {
		if !strings.Contains(catalogBody, required) {
			t.Fatalf("catalog omitted safe field definition %s: %s", required, catalogBody)
		}
	}
	for _, forbidden := range []string{`"legacy_account"`, `"sensitive"`, `"value_cipher"`, `"value_preview"`} {
		if strings.Contains(catalogBody, forbidden) {
			t.Fatalf("catalog exposed disabled or sensitive field data %s: %s", forbidden, catalogBody)
		}
	}

	createBody := `{"supplier_id":"` + supplier.ID.String() + `","product_id":"` + product.ID.String() + `","variant_id":null,"external_product_id":"remote-product","parameter_mapping":{"account_email":"Customer.Email"},"price_mode":"fixed_markup","markup_basis_point":0,"fixed_price":0,"auto_sync_price":false,"auto_sync_stock":false}`
	createResponse := request("POST", "/admin/v1/operations/mappings", createBody, handler.CreateSupplyMapping)
	if createResponse.Code != 201 {
		t.Fatalf("create mapping status = %d, body = %s", createResponse.Code, createResponse.Body.String())
	}
	var stored model.ProductMapping
	if err := db.Where("supplier_id = ? AND product_id = ?", supplier.ID, product.ID).First(&stored).Error; err != nil {
		t.Fatalf("load stored mapping: %v", err)
	}
	var persistedMapping map[string]string
	if err := json.Unmarshal(stored.ParameterMapping, &persistedMapping); err != nil || persistedMapping["account_email"] != "Customer.Email" {
		t.Fatalf("stored parameter mapping mismatch: %s", stored.ParameterMapping)
	}
	if stored.AutoSyncPrice || stored.AutoSyncStock {
		t.Fatalf("explicit false sync settings were replaced by model defaults: %#v", stored)
	}

	listResponse := request("GET", "/admin/v1/operations/mappings", "", handler.AdminSupplyMappings)
	if listResponse.Code != 200 || !strings.Contains(listResponse.Body.String(), `"parameter_mapping":{"account_email":"Customer.Email"}`) {
		t.Fatalf("mapping list omitted saved mapping: status=%d body=%s", listResponse.Code, listResponse.Body.String())
	}
	patchBody := `{"parameter_mapping":{"account_email":"customer_email"}}`
	patchResponse := request("PATCH", "/admin/v1/operations/mappings/"+stored.ID.String(), patchBody, func(context *gin.Context) {
		context.Params = gin.Params{{Key: "id", Value: stored.ID.String()}}
		handler.UpdateSupplyMapping(context)
	})
	if patchResponse.Code != 200 {
		t.Fatalf("patch mapping status = %d, body = %s", patchResponse.Code, patchResponse.Body.String())
	}
	if err := db.First(&stored, "id = ?", stored.ID).Error; err != nil {
		t.Fatalf("reload updated mapping: %v", err)
	}
	persistedMapping = nil
	if err := json.Unmarshal(stored.ParameterMapping, &persistedMapping); err != nil || persistedMapping["account_email"] != "customer_email" {
		t.Fatalf("updated parameter mapping mismatch: %s %v", stored.ParameterMapping, err)
	}

	invalidBody := `{"supplier_id":"` + supplier.ID.String() + `","product_id":"` + product.ID.String() + `","variant_id":null,"external_product_id":"remote-product-2","parameter_mapping":{"internal_note":"note"},"price_mode":"fixed_markup","markup_basis_point":0,"fixed_price":0,"auto_sync_price":false,"auto_sync_stock":true}`
	invalidResponse := request("POST", "/admin/v1/operations/mappings", invalidBody, handler.CreateSupplyMapping)
	if invalidResponse.Code != 422 {
		t.Fatalf("non-supplier checkout field mapping was not rejected as validation error: status=%d body=%s", invalidResponse.Code, invalidResponse.Body.String())
	}
	collisionPatch := `{"parameter_mapping":{"account_email":"region"}}`
	collisionResponse := request("PATCH", "/admin/v1/operations/mappings/"+stored.ID.String(), collisionPatch, func(context *gin.Context) {
		context.Params = gin.Params{{Key: "id", Value: stored.ID.String()}}
		handler.UpdateSupplyMapping(context)
	})
	if collisionResponse.Code != 422 {
		t.Fatalf("mapped key collision with an identity field was accepted: status=%d body=%s", collisionResponse.Code, collisionResponse.Body.String())
	}
}

func TestBatchDeleteSupplierCategoryMappingsIsAtomicAndLeavesTombstonesPostgreSQL(t *testing.T) {
	db := isolatedSupplyAdminDB(t, "linlinqi_category_batch_delete_test_")
	supplierModel := model.Supplier{
		Name: "Batch delete supplier", Code: "batch-delete-supplier",
		BaseURL: "https://batch-delete.example.test", Protocol: "linlinqi-standard", Status: "active",
		PriceCurrency: "CNY", BalanceCurrency: "CNY",
	}
	if err := db.Create(&supplierModel).Error; err != nil {
		t.Fatalf("create supplier: %v", err)
	}
	category := model.Category{Name: "Batch target", Slug: "batch-delete-target", Enabled: true}
	if err := db.Create(&category).Error; err != nil {
		t.Fatalf("create category: %v", err)
	}
	products := []model.Product{
		{CategoryID: category.ID, Name: "Batch product A", Slug: "batch-product-a", Currency: "CNY", DeliveryType: "auto", InventoryMode: "supplier", Status: "draft"},
		{CategoryID: category.ID, Name: "Batch product B", Slug: "batch-product-b", Currency: "CNY", DeliveryType: "auto", InventoryMode: "supplier", Status: "draft"},
	}
	if err := db.Create(&products).Error; err != nil {
		t.Fatalf("create products: %v", err)
	}
	bindings := []model.SupplierCategoryMapping{
		{SupplierID: supplierModel.ID, ExternalCategoryID: "remote-batch-a", ExternalCategoryName: "Remote A", CategoryID: &category.ID, PriceMode: "fixed_markup", MarkupCurrency: "CNY", Enabled: true},
		{SupplierID: supplierModel.ID, ExternalCategoryID: "remote-batch-b", ExternalCategoryName: "Remote B", CategoryID: &category.ID, PriceMode: "fixed_markup", MarkupCurrency: "CNY", Enabled: true},
	}
	if err := db.Create(&bindings).Error; err != nil {
		t.Fatalf("create bindings: %v", err)
	}
	productMappings := []model.ProductMapping{
		{SupplierID: supplierModel.ID, SupplierCategoryMappingID: &bindings[0].ID, InheritCategoryPolicy: true, ProductID: products[0].ID, ExternalProductID: "remote-product-a", PriceMode: "fixed_markup", MarkupCurrency: "CNY", FixedPriceCurrency: "CNY"},
		{SupplierID: supplierModel.ID, SupplierCategoryMappingID: &bindings[1].ID, InheritCategoryPolicy: true, ProductID: products[1].ID, ExternalProductID: "remote-product-b", PriceMode: "fixed_markup", MarkupCurrency: "CNY", FixedPriceCurrency: "CNY"},
	}
	if err := db.Create(&productMappings).Error; err != nil {
		t.Fatalf("create inherited product mappings: %v", err)
	}

	gin.SetMode(gin.TestMode)
	handler := Handler{DB: db}
	request := func(ids []uuid.UUID, reason string) *httptest.ResponseRecorder {
		t.Helper()
		encoded, err := json.Marshal(gin.H{"ids": ids})
		if err != nil {
			t.Fatalf("encode batch request: %v", err)
		}
		recorder := httptest.NewRecorder()
		context, _ := gin.CreateTestContext(recorder)
		context.Request = httptest.NewRequest("DELETE", "/admin/v1/supplier-category-mappings/batch", strings.NewReader(string(encoded)))
		context.Request.Header.Set("Content-Type", "application/json")
		if reason != "" {
			context.Request.Header.Set("X-Change-Reason", reason)
		}
		handler.BatchDeleteSupplierCategoryMappings(context)
		return recorder
	}

	missingReason := request([]uuid.UUID{bindings[0].ID}, "")
	if missingReason.Code != 422 {
		t.Fatalf("missing reason status = %d, body = %s", missingReason.Code, missingReason.Body.String())
	}
	conflict := request([]uuid.UUID{bindings[0].ID, uuid.New()}, "verify atomic missing target")
	if conflict.Code != 409 {
		t.Fatalf("partial target status = %d, body = %s", conflict.Code, conflict.Body.String())
	}
	var stillActive int64
	if err := db.Model(&model.SupplierCategoryMapping{}).Where("id = ?", bindings[0].ID).Count(&stillActive).Error; err != nil || stillActive != 1 {
		t.Fatalf("failed batch partially deleted binding: active=%d err=%v", stillActive, err)
	}
	var stillInherited model.ProductMapping
	if err := db.First(&stillInherited, "id = ?", productMappings[0].ID).Error; err != nil {
		t.Fatalf("reload inherited mapping after rollback: %v", err)
	}
	if stillInherited.SupplierCategoryMappingID == nil || !stillInherited.InheritCategoryPolicy {
		t.Fatalf("failed batch partially detached mapping: %#v", stillInherited)
	}

	success := request([]uuid.UUID{bindings[0].ID, bindings[1].ID}, "operator removed obsolete category bindings")
	if success.Code != 200 || !strings.Contains(success.Body.String(), `"deleted":2`) {
		t.Fatalf("batch delete status = %d, body = %s", success.Code, success.Body.String())
	}
	var activeCount, tombstoneCount int64
	if err := db.Model(&model.SupplierCategoryMapping{}).Where("id IN ?", []uuid.UUID{bindings[0].ID, bindings[1].ID}).Count(&activeCount).Error; err != nil {
		t.Fatalf("count active bindings: %v", err)
	}
	if err := db.Unscoped().Model(&model.SupplierCategoryMapping{}).
		Where("id IN ? AND deleted_at IS NOT NULL", []uuid.UUID{bindings[0].ID, bindings[1].ID}).
		Count(&tombstoneCount).Error; err != nil {
		t.Fatalf("count binding tombstones: %v", err)
	}
	if activeCount != 0 || tombstoneCount != 2 {
		t.Fatalf("binding deletion did not preserve tombstones: active=%d tombstones=%d", activeCount, tombstoneCount)
	}
	var detached []model.ProductMapping
	if err := db.Where("id IN ?", []uuid.UUID{productMappings[0].ID, productMappings[1].ID}).Order("id ASC").Find(&detached).Error; err != nil {
		t.Fatalf("reload detached mappings: %v", err)
	}
	if len(detached) != 2 {
		t.Fatalf("detached mapping count = %d", len(detached))
	}
	for _, mapping := range detached {
		if mapping.SupplierCategoryMappingID != nil || mapping.InheritCategoryPolicy {
			t.Fatalf("mapping retained deleted category policy: %#v", mapping)
		}
	}
	var auditCount int64
	if err := db.Model(&model.AuditLog{}).Where("action = ?", "supplier.category-mapping.batch-delete").Count(&auditCount).Error; err != nil || auditCount != 1 {
		t.Fatalf("batch delete audit mismatch: count=%d err=%v", auditCount, err)
	}
}
