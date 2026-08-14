package queue

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"linlinqi/api/internal/config"
	"linlinqi/api/internal/database"
	"linlinqi/api/internal/model"
	"linlinqi/api/internal/security"
	"linlinqi/api/internal/service"
	"linlinqi/api/internal/supply"
)

func TestSupplierSyncPersistsRealBalanceSnapshotPostgreSQL(t *testing.T) {
	db := isolatedSupplierWorkerDB(t, "linlinqi_supplier_balance_test_")
	var productCalls, balanceCalls atomic.Int32
	remoteUpdatedAt := time.Now().UTC().Truncate(time.Second)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/openapi/v1/categories":
			_, _ = writer.Write([]byte(`{"code":0,"message":"ok","data":[]}`))
		case "/openapi/v1/products":
			productCalls.Add(1)
			_, _ = writer.Write([]byte(`{"code":0,"message":"ok","data":[{"id":"remote-disabled-sync","external_id":"remote-disabled-sync","name":"Disabled sync product","price":1200,"stock":9}]}`))
		case "/openapi/v1/account/balance":
			balanceCalls.Add(1)
			_, _ = writer.Write([]byte(`{"code":0,"message":"ok","data":{"balance":7654321,"currency":"CNY","updated_at":"` + remoteUpdatedAt.Format(time.RFC3339) + `"}}`))
		default:
			writer.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	vault, err := security.NewVault("supplier-balance-integration-encryption-key")
	if err != nil {
		t.Fatalf("create vault: %v", err)
	}
	supplierModel := model.Supplier{
		Base: model.Base{ID: uuid.New()}, Name: "Balance Supplier", Code: "balance-supplier",
		BaseURL: server.URL, Protocol: "linlinqi-standard", Status: "active",
	}
	supplierModel.APIKeyCipher, supplierModel.APIKeyNonce, _, err = vault.Encrypt("balance-api-key", append(supplierModel.ID[:], []byte("api-key")...))
	if err != nil {
		t.Fatalf("encrypt API key: %v", err)
	}
	supplierModel.APISecretCipher, supplierModel.APISecretNonce, _, err = vault.Encrypt("balance-api-secret-value", append(supplierModel.ID[:], []byte("api-secret")...))
	if err != nil {
		t.Fatalf("encrypt API secret: %v", err)
	}
	if err := db.Create(&supplierModel).Error; err != nil {
		t.Fatalf("create supplier: %v", err)
	}
	category := model.Category{Name: "Disabled sync", Slug: "disabled-sync", Enabled: true}
	if err := db.Create(&category).Error; err != nil {
		t.Fatalf("create category: %v", err)
	}
	product := model.Product{CategoryID: category.ID, Name: "Disabled sync product", Slug: "disabled-sync-product", Price: 1000, CostPrice: 700, DeliveryType: "auto", InventoryMode: "supplier", Status: "on_sale"}
	if err := db.Create(&product).Error; err != nil {
		t.Fatalf("create product: %v", err)
	}
	mapping := model.ProductMapping{SupplierID: supplierModel.ID, ProductID: product.ID, ExternalProductID: "remote-disabled-sync", PriceMode: "fixed_markup"}
	if err := db.Create(&mapping).Error; err != nil {
		t.Fatalf("create mapping: %v", err)
	}
	if err := db.Model(&mapping).UpdateColumns(map[string]any{"auto_sync_price": false, "auto_sync_stock": false}).Error; err != nil {
		t.Fatalf("disable mapping sync: %v", err)
	}
	payload, _ := json.Marshal(map[string]string{"supplier_id": supplierModel.ID.String()})
	worker := Worker{db: db, vault: vault, cfg: config.Config{Env: "test"}}
	if err := worker.handleSupplierSync(context.Background(), asynq.NewTask(TypeSupplierSync, payload)); err != nil {
		t.Fatalf("sync supplier: %v", err)
	}
	var stored model.Supplier
	if err := db.First(&stored, "id = ?", supplierModel.ID).Error; err != nil {
		t.Fatalf("reload supplier: %v", err)
	}
	if stored.Balance != 7654321 || stored.BalanceCurrency != "CNY" || stored.BalanceSyncedAt == nil || stored.LastSyncAt == nil {
		t.Fatalf("balance snapshot was not persisted: %#v", stored)
	}
	if productCalls.Load() != 1 || balanceCalls.Load() != 1 {
		t.Fatalf("unexpected upstream calls: products=%d balance=%d", productCalls.Load(), balanceCalls.Load())
	}
	var legacy model.SupplierProduct
	if err := db.Where("supplier_id = ? AND product_id = ?", supplierModel.ID, product.ID).First(&legacy).Error; err != nil {
		t.Fatalf("reload legacy supplier product: %v", err)
	}
	if legacy.AutoSync {
		t.Fatal("disabled mapping sync was revived by the legacy default:true tag")
	}
}

func isolatedSupplierWorkerDB(t *testing.T, prefix string) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("LINLINQI_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set LINLINQI_TEST_DATABASE_URL to run PostgreSQL supplier worker integration tests")
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

func TestSupplierMappedPrice(t *testing.T) {
	for name, test := range map[string]struct {
		mapping  model.ProductMapping
		upstream int64
		want     int64
		valid    bool
	}{
		"no markup":       {mapping: model.ProductMapping{PriceMode: "fixed_markup"}, upstream: 1234, want: 1234, valid: true},
		"rounded markup":  {mapping: model.ProductMapping{PriceMode: "fixed_markup", MarkupBasisPoint: 1250}, upstream: 101, want: 114, valid: true},
		"fixed amount":    {mapping: model.ProductMapping{PriceMode: "fixed_amount", MarkupAmount: 25}, upstream: 101, want: 126, valid: true},
		"fixed price":     {mapping: model.ProductMapping{PriceMode: "fixed_price", FixedPrice: 8800}, upstream: 7000, want: 8800, valid: true},
		"negative source": {mapping: model.ProductMapping{PriceMode: "fixed_markup"}, upstream: -1, valid: false},
		"invalid markup":  {mapping: model.ProductMapping{PriceMode: "fixed_markup", MarkupBasisPoint: 100_001}, upstream: 100, valid: false},
		"invalid fixed":   {mapping: model.ProductMapping{PriceMode: "fixed_price", FixedPrice: 0}, upstream: 100, valid: false},
		"invalid amount":  {mapping: model.ProductMapping{PriceMode: "fixed_amount", MarkupAmount: -1}, upstream: 100, valid: false},
		"overflow":        {mapping: model.ProductMapping{PriceMode: "fixed_markup", MarkupBasisPoint: 1}, upstream: int64(^uint64(0) >> 1), valid: false},
		"unknown mode":    {mapping: model.ProductMapping{PriceMode: "percentage"}, upstream: 100, valid: false},
	} {
		t.Run(name, func(t *testing.T) {
			got, err := supplierMappedPrice(test.mapping, test.upstream)
			if test.valid && err != nil {
				t.Fatalf("valid price rejected: %v", err)
			}
			if !test.valid && err == nil {
				t.Fatalf("invalid price accepted: %d", got)
			}
			if test.valid && got != test.want {
				t.Fatalf("mapped price = %d, want %d", got, test.want)
			}
		})
	}
}

func TestSupplierMappedConvertedPriceAppliesFXBeforeMarkup(t *testing.T) {
	sale, cost, err := supplierMappedConvertedPrice(model.ProductMapping{PriceMode: "fixed_markup", MarkupBasisPoint: 5000}, 100, 2, 2, "7.0267")
	if err != nil {
		t.Fatal(err)
	}
	if cost != 703 || sale != 1054 {
		t.Fatalf("converted cost/sale = %d/%d, want 703/1054", cost, sale)
	}
	fixed, fixedCost, err := supplierMappedConvertedPrice(model.ProductMapping{PriceMode: "fixed_price", FixedPrice: 1200}, 100, 2, 2, "7.0267")
	if err != nil || fixed != 1200 || fixedCost != 703 {
		t.Fatalf("fixed price conversion = %d/%d, %v; want 1200/703", fixed, fixedCost, err)
	}
	amount, amountCost, err := supplierMappedConvertedPrice(model.ProductMapping{PriceMode: "fixed_amount", MarkupAmount: 250}, 100, 2, 2, "7.0267")
	if err != nil || amount != 953 || amountCost != 703 {
		t.Fatalf("fixed amount conversion = %d/%d, %v; want 953/703", amount, amountCost, err)
	}
}

func TestSupplierCategoryFixedAmountMarkupRequiresStoreCurrency(t *testing.T) {
	mapping := model.SupplierCategoryMapping{PriceMode: "fixed_amount", MarkupAmount: 250, MarkupCurrency: "CNY"}
	pricing, err := supplierCategoryProductPricing(mapping, "CNY")
	if err != nil || pricing.PriceMode != "fixed_amount" || pricing.MarkupAmount != 250 {
		t.Fatalf("valid category pricing rejected: %#v %v", pricing, err)
	}
	mapping.MarkupCurrency = "USD"
	if _, err := supplierCategoryProductPricing(mapping, "CNY"); err == nil {
		t.Fatal("cross-currency fixed amount category markup was accepted")
	}
	mapping.PriceMode = "fixed_markup"
	if _, err := supplierCategoryProductPricing(mapping, "CNY"); err != nil {
		t.Fatalf("percentage category markup must not depend on markup currency: %v", err)
	}
}

func TestInheritedSupplierCategoryProductPolicyTracksSourceAndCurrentRule(t *testing.T) {
	binding := model.SupplierCategoryMapping{
		Base:           model.Base{ID: uuid.New()},
		PriceMode:      "fixed_amount",
		MarkupAmount:   250,
		MarkupCurrency: "CNY",
		SyncPrice:      true,
		SyncStock:      false,
		SyncTitle:      true,
	}
	mapping := model.ProductMapping{PriceMode: "fixed_price", FixedPrice: 9900, AutoSyncStock: true}
	if err := inheritSupplierCategoryProductPolicy(&mapping, binding, "CNY"); err != nil {
		t.Fatalf("inherit valid category policy: %v", err)
	}
	if mapping.SupplierCategoryMappingID == nil || *mapping.SupplierCategoryMappingID != binding.ID || !mapping.InheritCategoryPolicy {
		t.Fatalf("category policy provenance was not recorded: %#v", mapping)
	}
	if mapping.PriceMode != "fixed_amount" || mapping.MarkupAmount != 250 || mapping.FixedPrice != 0 {
		t.Fatalf("current category pricing was not inherited: %#v", mapping)
	}
	if !mapping.AutoSyncPrice || mapping.AutoSyncStock || !mapping.AutoSyncTitle {
		t.Fatalf("category synchronization switches were not inherited: %#v", mapping)
	}

	binding.MarkupCurrency = "USD"
	if err := inheritSupplierCategoryProductPolicy(&mapping, binding, "CNY"); err == nil {
		t.Fatal("cross-currency fixed amount category policy was inherited")
	}
}

func TestValidSupplierDeliveriesBoundsSensitivePayload(t *testing.T) {
	if !validSupplierDeliveries([]string{"CODE-001", "account=user@example.com\npassword=temporary"}) {
		t.Fatal("valid delivery content was rejected")
	}
	for name, deliveries := range map[string][]string{
		"empty":     {""},
		"null byte": {"CODE\x00SECRET"},
		"oversized": {strings.Repeat("X", (64<<10)+1)},
	} {
		t.Run(name, func(t *testing.T) {
			if validSupplierDeliveries(deliveries) {
				t.Fatal("invalid delivery content was accepted")
			}
		})
	}
}

func TestSupplierProcurementAuditRequestContainsOnlyParameterKeys(t *testing.T) {
	body, err := supplierProcurementAuditRequestBody("LQP-1", "remote-product", 2, true, []string{"Customer.Email", "region_code"}, json.RawMessage(`{"account_email":"Customer.Email"}`))
	if err != nil {
		t.Fatalf("encode procurement audit request: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(body), &decoded); err != nil {
		t.Fatalf("decode procurement audit request: %v", err)
	}
	keys, ok := decoded["parameter_keys"].([]any)
	if !ok || len(keys) != 2 || keys[0] != "Customer.Email" || keys[1] != "region_code" {
		t.Fatalf("mapped parameter keys missing from audit request: %#v", decoded)
	}
	for _, forbidden := range []string{"parameters", "parameter_values", "buyer-secret-value"} {
		if _, exists := decoded[forbidden]; exists || strings.Contains(body, "buyer-secret-value") {
			t.Fatalf("audit request contains forbidden parameter material %q: %s", forbidden, body)
		}
	}
}

func TestSupplierProcurementBindingUsesImmutableSnapshot(t *testing.T) {
	originalSupplierID := uuid.New()
	changedSupplierID := uuid.New()
	body, err := supplierProcurementAuditRequestBody(
		"LQP-SNAPSHOT", "original-external-product", 1, true, []string{"Original.Customer"},
		json.RawMessage(`{"account_email":"Original.Customer"}`),
	)
	if err != nil {
		t.Fatalf("encode procurement snapshot: %v", err)
	}
	procurement := model.ProcurementOrder{
		SupplierID: originalSupplierID, ExternalProductID: "original-external-product", RequestBody: body,
	}
	// This represents today's edited mapping. It must never influence the
	// already-created procurement binding or its outgoing parameter keys.
	changedMapping := model.ProductMapping{
		SupplierID: changedSupplierID, ExternalProductID: "changed-external-product",
		ParameterMapping: json.RawMessage(`{"account_email":"Changed.Customer"}`),
	}
	binding, err := bindingFromSupplierProcurement(procurement)
	if err != nil {
		t.Fatalf("resolve procurement binding: %v", err)
	}
	if binding.SupplierID == changedMapping.SupplierID || binding.ExternalProductID == changedMapping.ExternalProductID {
		t.Fatalf("existing procurement used current mapping: %#v", binding)
	}
	if binding.SupplierID != originalSupplierID || binding.ExternalProductID != "original-external-product" {
		t.Fatalf("existing procurement lost immutable supplier/product binding: %#v", binding)
	}
	mapped, err := service.ApplySupplierParameterMapping(map[string]string{"account_email": "secret-value"}, binding.ParameterMapping)
	if err != nil || mapped["Original.Customer"] != "secret-value" || mapped["Changed.Customer"] != "" {
		t.Fatalf("existing procurement did not use key mapping snapshot: %#v %v", mapped, err)
	}
}

func TestSupplierProcurementRetryUsesOriginalSupplierAndMappingPostgreSQL(t *testing.T) {
	db := isolatedSupplierWorkerDB(t, "linlinqi_supplier_snapshot_test_")
	var originalCalls, changedCalls atomic.Int32
	var captured supply.CreateOrderRequest
	originalServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		originalCalls.Add(1)
		if request.URL.Path != "/openapi/v1/orders" || request.Method != http.MethodPost {
			t.Errorf("unexpected original supplier request: %s %s", request.Method, request.URL.Path)
			writer.WriteHeader(http.StatusNotFound)
			return
		}
		if err := json.NewDecoder(request.Body).Decode(&captured); err != nil {
			t.Errorf("decode original supplier request: %v", err)
			writer.WriteHeader(http.StatusBadRequest)
			return
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"code":0,"message":"ok","data":{"external_order_no":"ORIGINAL-UPSTREAM-ORDER","status":"delivered","deliveries":["CARD-SNAPSHOT-1"],"cost":75}}`))
	}))
	defer originalServer.Close()
	changedServer := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		changedCalls.Add(1)
		writer.WriteHeader(http.StatusInternalServerError)
	}))
	defer changedServer.Close()

	vault, err := security.NewVault("supplier-snapshot-integration-encryption-key")
	if err != nil {
		t.Fatalf("create vault: %v", err)
	}
	createSupplier := func(name, code, baseURL, status, key, secret string) model.Supplier {
		t.Helper()
		item := model.Supplier{Base: model.Base{ID: uuid.New()}, Name: name, Code: code, BaseURL: baseURL, Protocol: "linlinqi-standard", Status: status}
		var encryptionErr error
		item.APIKeyCipher, item.APIKeyNonce, _, encryptionErr = vault.Encrypt(key, append(item.ID[:], []byte("api-key")...))
		if encryptionErr != nil {
			t.Fatalf("encrypt supplier key: %v", encryptionErr)
		}
		item.APISecretCipher, item.APISecretNonce, _, encryptionErr = vault.Encrypt(secret, append(item.ID[:], []byte("api-secret")...))
		if encryptionErr != nil {
			t.Fatalf("encrypt supplier secret: %v", encryptionErr)
		}
		if err := db.Create(&item).Error; err != nil {
			t.Fatalf("create supplier: %v", err)
		}
		return item
	}
	originalSecret := "original-supplier-secret-value"
	originalSupplier := createSupplier("Original Supplier", "snapshot-original", originalServer.URL, "disabled", "original-key", originalSecret)
	changedSupplier := createSupplier("Changed Supplier", "snapshot-changed", changedServer.URL, "active", "changed-key", "changed-supplier-secret-value")
	category := model.Category{Name: "Snapshot", Slug: "supplier-snapshot", Enabled: true}
	if err := db.Create(&category).Error; err != nil {
		t.Fatalf("create category: %v", err)
	}
	product := model.Product{CategoryID: category.ID, Name: "Snapshot Product", Slug: "supplier-snapshot-product", Price: 100, CostPrice: 50, DeliveryType: "auto", InventoryMode: "supplier", Status: "on_sale"}
	if err := db.Create(&product).Error; err != nil {
		t.Fatalf("create product: %v", err)
	}
	field := model.ProductInputField{ProductID: product.ID, Key: "account_email", Label: "Account email", InputType: "email", Required: true, Sensitive: true, PassToSupplier: true, Options: json.RawMessage(`[]`), MinLength: 3, MaxLength: 190, Enabled: true}
	if err := db.Create(&field).Error; err != nil {
		t.Fatalf("create checkout field: %v", err)
	}
	now := time.Now().UTC()
	order := model.Order{Base: model.Base{ID: uuid.New()}, OrderNo: "LQ-SNAPSHOT-1", Email: "buyer@example.com", Status: "processing", PaymentStatus: "paid", Subtotal: 100, Total: 100, PaymentMethod: "supplier_balance", PaidAt: &now, Adjustments: []byte(`[]`)}
	if err := db.Create(&order).Error; err != nil {
		t.Fatalf("create order: %v", err)
	}
	item := model.OrderItem{Base: model.Base{ID: uuid.New()}, OrderID: order.ID, ProductID: product.ID, ProductName: product.Name, UnitPrice: 100, PlatformUnitPrice: 100, Quantity: 1}
	if err := db.Create(&item).Error; err != nil {
		t.Fatalf("create order item: %v", err)
	}
	if err := service.PersistOrderInputValues(db, vault, order.ID,
		[]service.OrderLine{{ProductID: product.ID, Quantity: 1}},
		[]service.SubmittedInputValue{{ProductID: product.ID, FieldID: field.ID, Value: "buyer@example.com"}},
	); err != nil {
		t.Fatalf("persist checkout values: %v", err)
	}
	snapshot := model.FXRateSnapshot{
		BaseCode: "USD", QuoteCode: "CNY", Rate: "7.0267", SourceTier: "system",
		ObservedAt: now, SelectedAt: now, ExpiresAt: now.Add(time.Hour), StaleAfter: now.Add(2 * time.Hour),
		Decision: "supplier snapshot integration",
	}
	if err := db.Create(&snapshot).Error; err != nil {
		t.Fatalf("create FX snapshot: %v", err)
	}
	requestBody, err := supplierProcurementAuditRequestBody(
		"LQP-SNAPSHOT-1", "original-product", 1, false, []string{"Original.Customer"},
		json.RawMessage(`{"account_email":"Original.Customer"}`),
	)
	if err != nil {
		t.Fatalf("encode procurement request snapshot: %v", err)
	}
	procurement := model.ProcurementOrder{Base: model.Base{ID: uuid.New()}, ProcurementNo: "LQP-SNAPSHOT-1", SupplierID: originalSupplier.ID, OrderID: order.ID, OrderItemID: item.ID, ExternalProductID: "original-product", Quantity: 1, CostAmount: 527, CostCurrency: "CNY", UpstreamCostAmount: 75, UpstreamCurrency: "USD", FXSnapshotID: &snapshot.ID, Status: "creating", RequestBody: requestBody}
	procurement.CallbackSecretCipher, procurement.CallbackSecretNonce, err = service.EncryptProcurementCallbackSecret(vault, procurement.ID, originalSecret)
	if err != nil {
		t.Fatalf("encrypt callback secret: %v", err)
	}
	if err := db.Create(&procurement).Error; err != nil {
		t.Fatalf("create existing procurement: %v", err)
	}
	if err := db.Create(&model.ProductMapping{
		SupplierID: changedSupplier.ID, ProductID: product.ID, ExternalProductID: "changed-product",
		ParameterMapping: json.RawMessage(`{"account_email":"Changed.Customer"}`), PriceMode: "fixed_markup", AutoSyncStock: true,
	}).Error; err != nil {
		t.Fatalf("create changed current mapping: %v", err)
	}
	if err := db.Create(&model.SupplierProduct{SupplierID: changedSupplier.ID, ProductID: product.ID, ExternalID: "changed-product", ExternalPrice: 80, ExternalStock: 10, AutoSync: true}).Error; err != nil {
		t.Fatalf("create changed supplier stock: %v", err)
	}

	worker := Worker{db: db, vault: vault, cfg: config.Config{Env: "test"}}
	if err := worker.purchaseSupplierItem(context.Background(), &order, &item); err != nil {
		t.Fatalf("retry existing procurement: %v", err)
	}
	if originalCalls.Load() != 1 || changedCalls.Load() != 0 {
		t.Fatalf("retry used wrong supplier: original_calls=%d changed_calls=%d", originalCalls.Load(), changedCalls.Load())
	}
	if captured.ExternalProductID != "original-product" || captured.Parameters["Original.Customer"] != "buyer@example.com" || captured.Parameters["Changed.Customer"] != "" {
		t.Fatalf("retry lost procurement snapshot: %#v", captured)
	}
	if strings.Contains(procurement.RequestBody, "buyer@example.com") {
		t.Fatalf("procurement audit request leaked parameter value: %s", procurement.RequestBody)
	}
}

func TestSupplierCallbackCompletesProcurementPostgreSQL(t *testing.T) {
	db := isolatedSupplierWorkerDB(t, "linlinqi_supplier_worker_test_")
	vault, err := security.NewVault("supplier-worker-integration-encryption-key")
	if err != nil {
		t.Fatalf("create vault: %v", err)
	}
	supplierModel := model.Supplier{Base: model.Base{ID: uuid.New()}, Name: "Worker Supplier", Code: "worker-supplier", BaseURL: "https://supplier.example.com", Protocol: "linlinqi-standard", Status: "active"}
	supplierModel.APIKeyCipher, supplierModel.APIKeyNonce, _, err = vault.Encrypt("supplier-worker-key", append(supplierModel.ID[:], []byte("api-key")...))
	if err != nil {
		t.Fatalf("encrypt supplier key: %v", err)
	}
	supplierModel.APISecretCipher, supplierModel.APISecretNonce, _, err = vault.Encrypt("supplier-worker-secret-value", append(supplierModel.ID[:], []byte("api-secret")...))
	if err != nil {
		t.Fatalf("encrypt supplier secret: %v", err)
	}
	if err := db.Create(&supplierModel).Error; err != nil {
		t.Fatalf("create supplier: %v", err)
	}
	category := model.Category{Name: "Worker", Slug: "worker", Enabled: true}
	if err := db.Create(&category).Error; err != nil {
		t.Fatalf("create category: %v", err)
	}
	product := model.Product{CategoryID: category.ID, Name: "Worker Product", Slug: "worker-product", Price: 100, CostPrice: 50, DeliveryType: "auto", InventoryMode: "supplier", Status: "on_sale"}
	if err := db.Create(&product).Error; err != nil {
		t.Fatalf("create product: %v", err)
	}
	if err := db.Create(&model.ProductMapping{SupplierID: supplierModel.ID, ProductID: product.ID, ExternalProductID: "remote-product", PriceMode: "fixed_markup", AutoSyncStock: true}).Error; err != nil {
		t.Fatalf("create mapping: %v", err)
	}
	if err := db.Create(&model.SupplierProduct{SupplierID: supplierModel.ID, ProductID: product.ID, ExternalID: "remote-product", ExternalPrice: 75, ExternalStock: 3, AutoSync: true}).Error; err != nil {
		t.Fatalf("create supplier product: %v", err)
	}
	now := time.Now().UTC()
	order := model.Order{Base: model.Base{ID: uuid.New()}, OrderNo: "LQ-WORKER-1", Email: "buyer@example.com", Status: "processing", PaymentStatus: "paid", Subtotal: 100, Total: 100, PaymentMethod: "supplier_balance", PaidAt: &now, Adjustments: []byte(`[]`)}
	if err := db.Create(&order).Error; err != nil {
		t.Fatalf("create order: %v", err)
	}
	item := model.OrderItem{Base: model.Base{ID: uuid.New()}, OrderID: order.ID, ProductID: product.ID, ProductName: product.Name, UnitPrice: 100, PlatformUnitPrice: 100, Quantity: 1}
	if err := db.Create(&item).Error; err != nil {
		t.Fatalf("create item: %v", err)
	}
	snapshot := model.FXRateSnapshot{
		BaseCode: "USD", QuoteCode: "CNY", Rate: "7.0267", SourceTier: "system",
		ObservedAt: now, SelectedAt: now, ExpiresAt: now.Add(time.Hour), StaleAfter: now.Add(2 * time.Hour),
		Decision: "supplier worker integration",
	}
	if err := db.Create(&snapshot).Error; err != nil {
		t.Fatalf("create FX snapshot: %v", err)
	}
	procurement := model.ProcurementOrder{Base: model.Base{ID: uuid.New()}, ProcurementNo: "LQP-WORKER-1", SupplierID: supplierModel.ID, OrderID: order.ID, OrderItemID: item.ID, ExternalProductID: "remote-product", Quantity: 1, CostAmount: 527, CostCurrency: "CNY", UpstreamCostAmount: 75, UpstreamCurrency: "USD", FXSnapshotID: &snapshot.ID, Status: "processing"}
	if err := db.Create(&procurement).Error; err != nil {
		t.Fatalf("create procurement: %v", err)
	}
	result := supply.OrderResult{ExternalOrderNo: "UPSTREAM-WORKER-1", Status: "delivered", Deliveries: []string{"CARD-WORKER-SECRET"}, Cost: 75}
	encoded, _ := json.Marshal(result)
	event := model.WebhookEvent{Base: model.Base{ID: uuid.New()}, EventID: "supplier:" + strings.Repeat("a", 64), EventType: "order.delivered", Endpoint: "/api/v1/supplier-callbacks/" + supplierModel.ID.String(), Payload: `{}`, SupplierID: &supplierModel.ID, ProcurementOrderID: &procurement.ID, Status: "queued"}
	event.PayloadCipher, event.PayloadNonce, _, err = vault.Encrypt(string(encoded), event.ID[:])
	if err != nil {
		t.Fatalf("encrypt callback: %v", err)
	}
	if err := db.Create(&event).Error; err != nil {
		t.Fatalf("create callback event: %v", err)
	}
	worker := Worker{db: db, vault: vault, cfg: config.Config{Env: "test"}}
	if err := worker.purchaseSupplierItem(context.Background(), &order, &item); err != nil {
		t.Fatalf("process callback fulfillment: %v", err)
	}
	if err := db.First(&item, "id = ?", item.ID).Error; err != nil {
		t.Fatalf("reload item: %v", err)
	}
	values, err := service.DecryptDeliveryItems(vault, item)
	if err != nil || len(values) != 1 || values[0] != "CARD-WORKER-SECRET" {
		t.Fatalf("fulfilled delivery mismatch: %#v %v", values, err)
	}
	if err := db.First(&procurement, "id = ?", procurement.ID).Error; err != nil || procurement.Status != "completed" || procurement.ExternalOrderNo != "UPSTREAM-WORKER-1" || procurement.CostAmount != 527 {
		t.Fatalf("procurement was not completed: %#v %v", procurement, err)
	}
	if err := db.First(&event, "id = ?", event.ID).Error; err != nil || event.Status != "processed" || event.ProcessedAt == nil {
		t.Fatalf("callback event was not processed: %#v %v", event, err)
	}
}
