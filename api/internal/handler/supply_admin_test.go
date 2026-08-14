package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/hibiken/asynq"

	"linlinqi/api/internal/i18n"
	"linlinqi/api/internal/model"
)

func TestSupplySupplierCreateValidation(t *testing.T) {
	valid := supplySupplierCreateRequest{
		Name: " 北区供货节点 ", Code: " NORTH-01 ", BaseURL: " https://supplier.example.com/ ",
		APIKey: "supplier-key-001", APISecret: "supplier-secret-value-001", Protocol: "standard",
		PriceCurrency: "USD", PriceMinorUnit: 2,
	}
	if err := valid.normalizeAndValidate(); err != nil {
		t.Fatalf("valid supplier rejected: %v", err)
	}
	if valid.Name != "北区供货节点" || valid.Code != "north-01" || valid.BaseURL != "https://supplier.example.com" || valid.Protocol != "linlinqi-standard" {
		t.Fatalf("supplier was not normalized: %#v", valid)
	}
	for name, mutate := range map[string]func(*supplySupplierCreateRequest){
		"unsafe code":  func(request *supplySupplierCreateRequest) { request.Code = "../../node" },
		"short key":    func(request *supplySupplierCreateRequest) { request.APIKey = "short" },
		"short secret": func(request *supplySupplierCreateRequest) { request.APISecret = "too-short" },
		"control byte": func(request *supplySupplierCreateRequest) { request.APISecret = "valid-secret-value\n" },
		"bad protocol": func(request *supplySupplierCreateRequest) { request.Protocol = "custom-v1" },
	} {
		t.Run(name, func(t *testing.T) {
			request := valid
			mutate(&request)
			if err := request.normalizeAndValidate(); err == nil {
				t.Fatal("invalid supplier was accepted")
			}
		})
	}
}

func TestSupplySupplierUpdateRequiresPairedCredentialRotation(t *testing.T) {
	name := "新名称"
	valid := supplySupplierUpdateRequest{Name: &name}
	if err := valid.normalizeAndValidate(); err != nil {
		t.Fatalf("valid supplier update rejected: %v", err)
	}
	key, secret := "replacement-key", "replacement-secret-value"
	keyOnly := supplySupplierUpdateRequest{APIKey: &key}
	if err := keyOnly.normalizeAndValidate(); err == nil {
		t.Fatal("one-sided credential rotation was accepted")
	}
	paired := supplySupplierUpdateRequest{APIKey: &key, APISecret: &secret}
	if err := paired.normalizeAndValidate(); err != nil {
		t.Fatalf("paired credential rotation rejected: %v", err)
	}
	if err := (&supplySupplierUpdateRequest{}).normalizeAndValidate(); err == nil {
		t.Fatal("empty supplier update was accepted")
	}
}

func TestSupplierConnectionMutationGuardOnlyBlocksProcurementIdentityChanges(t *testing.T) {
	item := model.Supplier{BaseURL: "https://supplier.example.com", Protocol: "linlinqi-standard", PriceCurrency: "USD", PriceMinorUnit: 2}
	name := "Renamed"
	interval := 30
	if mutatesSupplierConnection(item, supplySupplierUpdateRequest{Name: &name, SyncIntervalMinutes: &interval}) {
		t.Fatal("display and scheduler settings should not mutate procurement identity")
	}
	sameURL := item.BaseURL
	sameCurrency := item.PriceCurrency
	sameMinor := item.PriceMinorUnit
	if mutatesSupplierConnection(item, supplySupplierUpdateRequest{BaseURL: &sameURL, PriceCurrency: &sameCurrency, PriceMinorUnit: &sameMinor}) {
		t.Fatal("idempotent connection update should remain allowed")
	}
	changedURL := "https://new-supplier.example.com"
	changedProtocol := "dujiao-next"
	changedCurrency := "CNY"
	changedMinor := 0
	changedBalanceCurrency := "EUR"
	changedCurrencyMode := "manual"
	credentials := map[string]string{"api_key": "key", "api_secret": "secret"}
	for _, req := range []supplySupplierUpdateRequest{
		{BaseURL: &changedURL}, {Protocol: &changedProtocol}, {PriceCurrency: &changedCurrency},
		{PriceMinorUnit: &changedMinor}, {BalanceCurrency: &changedBalanceCurrency},
		{CurrencyMode: &changedCurrencyMode}, {Credentials: &credentials},
	} {
		if !mutatesSupplierConnection(item, req) {
			t.Fatalf("procurement identity mutation was not detected: %#v", req)
		}
	}
}

func TestSupplierActivationRequiresFreshHealthyProbe(t *testing.T) {
	now := time.Now().UTC()
	item := model.Supplier{
		Protocol:          "linlinqi-standard",
		CredentialsCipher: []byte("cipher"), CredentialsNonce: []byte("nonce"),
		HealthStatus: "healthy", LastProbeAt: &now,
	}
	if !supplierCanActivate(item) {
		t.Fatal("fresh healthy read-only probe should allow activation")
	}
	item.HealthStatus = "degraded"
	if supplierCanActivate(item) {
		t.Fatal("degraded probe must not allow activation")
	}
	item.HealthStatus, item.LastProbeAt = "healthy", nil
	if supplierCanActivate(item) {
		t.Fatal("supplier without probe evidence must not allow activation")
	}
	item.LastProbeAt, item.CredentialsCipher = &now, nil
	if supplierCanActivate(item) {
		t.Fatal("supplier without configured credentials must not allow activation")
	}
}

func TestSupplierProbeResultIsFencedToConnectionIdentity(t *testing.T) {
	base := model.Supplier{
		BaseURL: "https://supplier.example.com", Protocol: "linlinqi-standard",
		PriceCurrency: "USD", PriceMinorUnit: 2, BalanceCurrency: "USD", CurrencyMode: "auto",
		CredentialsCipher: []byte("cipher"), CredentialsNonce: []byte("nonce"),
	}
	if !sameSupplierProbeIdentity(base, base) {
		t.Fatal("unchanged connection identity was rejected")
	}
	changed := base
	changed.BalanceCurrency = "EUR"
	if sameSupplierProbeIdentity(base, changed) {
		t.Fatal("stale probe was accepted after currency mutation")
	}
	changed = base
	changed.CredentialsCipher = []byte("rotated")
	if sameSupplierProbeIdentity(base, changed) {
		t.Fatal("stale probe was accepted after credential rotation")
	}
}

func TestSupplySupplierURLRejectsSSRFShapes(t *testing.T) {
	for name, raw := range map[string]string{
		"private IP":   "https://127.0.0.1",
		"plain HTTP":   "http://example.com",
		"userinfo":     "https://user:secret@example.com",
		"fragment":     "https://example.com/path#fragment",
		"invalid port": "https://1.1.1.1:0",
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := validateSupplierBaseURL(context.Background(), raw, "production"); err == nil {
				t.Fatalf("unsafe supplier URL accepted: %s", raw)
			}
		})
	}
}

func TestSupplyMappingValidationAndNullableVariant(t *testing.T) {
	item := model.ProductMapping{
		SupplierID: uuid.New(), ProductID: uuid.New(), ExternalProductID: "catalog:sku-001",
		ParameterMapping: json.RawMessage(`{"account_email":"Customer.Email"}`),
		PriceMode:        "FIXED_MARKUP", MarkupBasisPoint: 1800, FixedPrice: 9900,
		AutoSyncPrice: true, AutoSyncStock: true,
	}
	if err := normalizeSupplyMapping(&item); err != nil {
		t.Fatalf("valid mapping rejected: %v", err)
	}
	if item.PriceMode != "fixed_markup" || item.FixedPrice != 0 || string(item.ParameterMapping) != `{"account_email":"Customer.Email"}` {
		t.Fatalf("mapping was not normalized: %#v", item)
	}
	unicodeID := item
	unicodeID.ExternalProductID = " 商品/套餐:甲-01 "
	if err := normalizeSupplyMapping(&unicodeID); err != nil || unicodeID.ExternalProductID != "商品/套餐:甲-01" {
		t.Fatalf("Unicode upstream product ID was rejected or changed: %#v %v", unicodeID, err)
	}
	badID := item
	badID.ExternalProductID = "../../upstream product"
	if err := normalizeSupplyMapping(&badID); err == nil {
		t.Fatal("unsafe upstream product ID was accepted")
	}
	badMarkup := item
	badMarkup.MarkupBasisPoint = 100_001
	if err := normalizeSupplyMapping(&badMarkup); err == nil {
		t.Fatal("excessive supplier markup was accepted")
	}

	variantID := uuid.New()
	current := item
	current.VariantID = &variantID
	var request supplyMappingUpdateRequest
	if err := json.Unmarshal([]byte(`{"variant_id":null,"fixed_price":5000,"price_mode":"fixed_price","parameter_mapping":{"account_email":"customer_email"}}`), &request); err != nil {
		t.Fatalf("decode nullable variant: %v", err)
	}
	updated, err := applySupplyMappingUpdate(current, request)
	if err != nil {
		t.Fatalf("valid mapping update rejected: %v", err)
	}
	if updated.VariantID != nil || updated.PriceMode != "fixed_price" || updated.FixedPrice != 5000 || updated.MarkupBasisPoint != 0 || string(updated.ParameterMapping) != `{"account_email":"customer_email"}` {
		t.Fatalf("mapping update was not normalized: %#v", updated)
	}
	if err := json.Unmarshal([]byte(`{"parameter_mapping":null}`), &request); err == nil {
		t.Fatal("null parameter mapping was accepted")
	}
	duplicateTargets := item
	duplicateTargets.ParameterMapping = json.RawMessage(`{"account_email":"customer","region":"customer"}`)
	if err := normalizeSupplyMapping(&duplicateTargets); err == nil {
		t.Fatal("duplicate upstream parameter targets were accepted")
	}
}

func TestSupplyMappingUpdateDetachesOnlyCategoryOwnedPolicyFields(t *testing.T) {
	priceMode := "fixed_amount"
	markupAmount := int64(500)
	autoSyncPrice := false
	autoSyncStock := false
	autoSyncTitle := false
	externalID := "remote-product-2"
	supplierID := uuid.New()
	for name, request := range map[string]supplyMappingUpdateRequest{
		"supplier":         {SupplierID: &supplierID},
		"external product": {ExternalProductID: &externalID},
		"price mode":       {PriceMode: &priceMode},
		"markup amount":    {MarkupAmount: &markupAmount},
		"price switch":     {AutoSyncPrice: &autoSyncPrice},
		"stock switch":     {AutoSyncStock: &autoSyncStock},
		"title switch":     {AutoSyncTitle: &autoSyncTitle},
	} {
		t.Run(name, func(t *testing.T) {
			if !supplyMappingUpdateOverridesCategoryPolicy(request) {
				t.Fatal("category-owned field did not detach inherited policy")
			}
		})
	}

	autoSyncSummary := true
	autoSyncDescription := true
	autoSyncMedia := true
	autoSyncCategory := true
	parameterMapping := optionalSupplyParameterMapping{Set: true, Value: map[string]string{"account": "customer_account"}}
	for name, request := range map[string]supplyMappingUpdateRequest{
		"summary switch":     {AutoSyncSummary: &autoSyncSummary},
		"description switch": {AutoSyncDescription: &autoSyncDescription},
		"media switch":       {AutoSyncMedia: &autoSyncMedia},
		"category switch":    {AutoSyncCategory: &autoSyncCategory},
		"parameter mapping":  {ParameterMapping: parameterMapping},
	} {
		t.Run(name, func(t *testing.T) {
			if supplyMappingUpdateOverridesCategoryPolicy(request) {
				t.Fatal("product-owned field unexpectedly detached inherited category policy")
			}
		})
	}
}

func TestSupplierCategoryMappingValidationMatchesSharedStoreSemantics(t *testing.T) {
	valid := supplierCategoryMappingRequest{
		SupplierID: uuid.New(), CategoryID: uuid.New(), ExternalCategoryID: "remote:games",
		ExternalCategoryName: " Games ", DefaultCoverURL: "https://cdn.example.com/default.png",
		SyncTitle: true, SyncPrice: true, SyncStock: true, AutoPublish: true,
		PriceMode: "FIXED_MARKUP", MarkupBasisPoint: 1250, MarkupAmount: 99,
		MarkupCurrency: "cny", Sort: 20, Enabled: true,
	}
	if err := valid.normalizeAndValidate(); err != nil {
		t.Fatalf("valid supplier category mapping rejected: %v", err)
	}
	if valid.ExternalCategoryName != "Games" || valid.PriceMode != "fixed_markup" || valid.MarkupAmount != 0 || valid.MarkupCurrency != "CNY" {
		t.Fatalf("supplier category mapping was not normalized: %#v", valid)
	}
	unicodeID := valid
	unicodeID.ExternalCategoryID = " 分类/游戏:亚洲 "
	if err := unicodeID.normalizeAndValidate(); err != nil || unicodeID.ExternalCategoryID != "分类/游戏:亚洲" {
		t.Fatalf("Unicode category binding ID was rejected or changed: %#v %v", unicodeID, err)
	}
	for name, mutate := range map[string]func(*supplierCategoryMappingRequest){
		"missing supplier":   func(request *supplierCategoryMappingRequest) { request.SupplierID = uuid.Nil },
		"unsafe external id": func(request *supplierCategoryMappingRequest) { request.ExternalCategoryID = "../games" },
		"credential in cover": func(request *supplierCategoryMappingRequest) {
			request.DefaultCoverURL = "https://user:secret@example.com/a.png"
		},
		"fragmented cover": func(request *supplierCategoryMappingRequest) {
			request.DefaultCoverURL = "https://example.com/a.png#secret"
		},
		"negative sort":  func(request *supplierCategoryMappingRequest) { request.Sort = -1 },
		"invalid markup": func(request *supplierCategoryMappingRequest) { request.MarkupBasisPoint = 100_001 },
	} {
		t.Run(name, func(t *testing.T) {
			request := valid
			mutate(&request)
			if err := request.normalizeAndValidate(); err == nil {
				t.Fatal("invalid supplier category mapping was accepted")
			}
		})
	}
	fixed := valid
	fixed.PriceMode, fixed.MarkupAmount, fixed.MarkupBasisPoint = "fixed_amount", 500, 1250
	if err := fixed.normalizeAndValidate(); err != nil || fixed.MarkupBasisPoint != 0 {
		t.Fatalf("fixed amount category markup rejected or not normalized: %#v %v", fixed, err)
	}
}

func TestSupplierCategoryMappingBatchIDsAreBoundedAndUnique(t *testing.T) {
	valid := []uuid.UUID{uuid.New(), uuid.New()}
	if !validSupplierCategoryMappingBatchIDs(valid) {
		t.Fatal("valid category binding batch was rejected")
	}
	for name, ids := range map[string][]uuid.UUID{
		"empty":     {},
		"nil":       {uuid.Nil},
		"duplicate": {valid[0], valid[0]},
		"too many":  make([]uuid.UUID, 101),
	} {
		t.Run(name, func(t *testing.T) {
			if name == "too many" {
				for index := range ids {
					ids[index] = uuid.New()
				}
			}
			if validSupplierCategoryMappingBatchIDs(ids) {
				t.Fatal("invalid category binding batch was accepted")
			}
		})
	}
}

func TestSupplyCatalogAndMappingDTOExposeOnlySafeInputDefinitions(t *testing.T) {
	fieldID := uuid.New()
	product := adminSupplyCatalogProduct{
		ID: uuid.New(), Name: "Supplier product", Slug: "supplier-product", Currency: "USD", Status: "on_sale",
		Variants: []adminSupplyCatalogVariant{{ID: uuid.New(), ProductID: uuid.New(), SKU: "USD-SKU", Name: "USD variant", Price: 1250, Currency: "USD", Status: "active"}},
		InputFields: []adminSupplyCatalogInputField{{
			ID: fieldID, Key: "account_email", Label: "Account email", InputType: "email",
			Required: true, PassToSupplier: true,
		}},
	}
	encoded, err := json.Marshal(product)
	if err != nil {
		t.Fatalf("encode supply catalog DTO: %v", err)
	}
	text := string(encoded)
	for _, required := range []string{`"input_fields"`, `"account_email"`, `"pass_to_supplier"`} {
		if !strings.Contains(text, required) {
			t.Fatalf("supply catalog DTO omitted %s: %s", required, text)
		}
	}
	if strings.Count(text, `"currency":"USD"`) != 2 {
		t.Fatalf("supply catalog product and variant currency contract missing: %s", text)
	}
	for _, forbidden := range []string{`"sensitive"`, `"options"`, `"validation_pattern"`, `"value"`, `"value_cipher"`} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("supply catalog DTO exposed forbidden definition/value field %s: %s", forbidden, text)
		}
	}

	mapping := adminSupplyMappingItem{ID: uuid.New(), ParameterMapping: json.RawMessage(`{"account_email":"Customer.Email"}`)}
	encoded, err = json.Marshal(mapping)
	if err != nil || !strings.Contains(string(encoded), `"parameter_mapping":{"account_email":"Customer.Email"}`) {
		t.Fatalf("mapping list DTO omitted parameter_mapping: %s %v", encoded, err)
	}
}

func TestSupplierRemoteCategoryDTOExposesProductCount(t *testing.T) {
	encoded, err := json.Marshal(adminSupplierRemoteCategory{
		ID:           uuid.New(),
		ExternalID:   "remote-category-1",
		Name:         "远程分类",
		ProductCount: 27,
	})
	if err != nil {
		t.Fatalf("encode remote category DTO: %v", err)
	}
	if !strings.Contains(string(encoded), `"product_count":27`) {
		t.Fatalf("remote category DTO omitted product_count: %s", encoded)
	}
}

func TestSupplyProcurementAndOrderDTOExposeCurrencyContracts(t *testing.T) {
	procurement, err := json.Marshal(adminSupplyProcurementItem{
		ID: uuid.New(), CostAmount: 725, CostCurrency: "CNY", UpstreamCurrency: "USD",
	})
	if err != nil {
		t.Fatalf("encode procurement DTO: %v", err)
	}
	for _, required := range []string{`"cost_currency":"CNY"`, `"upstream_currency":"USD"`} {
		if !strings.Contains(string(procurement), required) {
			t.Fatalf("procurement DTO omitted %s: %s", required, procurement)
		}
	}

	order, err := json.Marshal(adminSupplyOrderSummary{ID: uuid.New(), Currency: "EUR"})
	if err != nil || !strings.Contains(string(order), `"currency":"EUR"`) {
		t.Fatalf("order summary DTO omitted currency: %s %v", order, err)
	}
	item, err := json.Marshal(adminSupplyOrderItemSummary{ID: uuid.New(), Currency: "JPY"})
	if err != nil || !strings.Contains(string(item), `"currency":"JPY"`) {
		t.Fatalf("order item summary DTO omitted currency: %s %v", item, err)
	}
}

func TestSupplyDTOIsStrictAndQueueDuplicateIsIdempotent(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest("POST", "/admin/v1/suppliers", strings.NewReader(`{"name":"节点","code":"node-1","base_url":"https://example.com","api_key":"supplier-key","api_secret":"supplier-secret-value","protocol":"linlinqi-standard","api_key_cipher":"forbidden"}`))
	context.Request.Header.Set("Content-Type", "application/json")
	var request supplySupplierCreateRequest
	if err := decodeStrictJSON(context, &request); err == nil {
		t.Fatal("server-owned credential cipher field was accepted")
	}
	if !isDuplicateSupplyTask(asynq.ErrDuplicateTask) || !isDuplicateSupplyTask(asynq.ErrTaskIDConflict) || isDuplicateSupplyTask(errors.New("redis unavailable")) {
		t.Fatal("supplier sync queue error classification is not idempotent")
	}
	dto := toAdminSupplySupplier(model.Supplier{APIKeyCipher: []byte("cipher-key"), APIKeyNonce: []byte("key-nonce"), APISecretCipher: []byte("cipher-secret"), APISecretNonce: []byte("secret-nonce")})
	applyAdminSupplierSyncPolicy(&dto, model.SupplierSyncPolicy{SyncPrice: false, SyncStock: true})
	encoded, err := json.Marshal(dto)
	if err != nil {
		t.Fatalf("encode supplier DTO: %v", err)
	}
	for _, forbidden := range []string{"cipher-key", "key-nonce", "cipher-secret", "secret-nonce", "api_key_cipher", "api_secret_cipher"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("supplier DTO exposed credential material %q: %s", forbidden, encoded)
		}
	}
	if !strings.Contains(string(encoded), `"sync_price":false`) || !strings.Contains(string(encoded), `"sync_stock":true`) || !strings.Contains(string(encoded), `"sync_interval_minutes":`) {
		t.Fatalf("supplier DTO omitted effective synchronization policy: %s", encoded)
	}
}

func TestProcurementRetryMessageNeverUsesUpstreamPayload(t *testing.T) {
	for _, status := range []string{"creating", "dispatching", "processing", "retrying", "completed", "failed", "cancelled", "unexpected"} {
		message := procurementRetryMessage(status, 2, nil, i18n.LocaleZH)
		if strings.TrimSpace(message) == "" || strings.Contains(strings.ToLower(message), "response_body") {
			t.Fatalf("unsafe or empty retry message for %s: %q", status, message)
		}
	}
}

func TestProcurementRecoveryEvidenceAndManualDeliveryValidation(t *testing.T) {
	if normalized, ok := validProcurementEvidence(" upstream ticket SUP-2048 "); !ok || normalized != "upstream ticket SUP-2048" {
		t.Fatalf("valid recovery evidence rejected: %q %v", normalized, ok)
	}
	for _, value := range []string{"bad", strings.Repeat("x", 1001), "valid\x00evidence"} {
		if _, ok := validProcurementEvidence(value); ok {
			t.Fatalf("invalid recovery evidence accepted: %q", value)
		}
	}
	request := procurementManualCompletionRequest{Deliveries: []string{"CODE-1", "CODE-2"}, CostAmount: cents(500), Evidence: "supplier ticket SUP-2048"}
	encoded, err := json.Marshal(request)
	if err != nil {
		t.Fatalf("encode recovery request: %v", err)
	}
	if strings.Contains(string(encoded), "card_ciphertext") || strings.Contains(string(encoded), "response_body") {
		t.Fatalf("manual recovery request exposed server-owned fields: %s", encoded)
	}
}
