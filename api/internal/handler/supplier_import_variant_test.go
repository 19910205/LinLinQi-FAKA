package handler

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"linlinqi/api/internal/model"
)

func TestSupplierImportVariantsBoundsAndValidation(t *testing.T) {
	valid, err := supplierImportVariants(json.RawMessage(`[{"id":"v-1","external_id":"v-1","name":"Large","price":150,"stock":3}]`))
	if err != nil || len(valid) != 1 || valid[0].ExternalID != "v-1" {
		t.Fatalf("valid variants rejected: %#v %v", valid, err)
	}
	if items, err := supplierImportVariants(json.RawMessage(`null`)); err != nil || items != nil {
		t.Fatalf("null variants must be treated as absent")
	}
	if _, err := supplierImportVariants(json.RawMessage(`{"bad":true}`)); err == nil {
		t.Fatalf("non-array variants must be rejected")
	}
	tooMany := make([]map[string]any, 51)
	for index := range tooMany {
		tooMany[index] = map[string]any{"id": fmt.Sprintf("v-%d", index), "name": "v"}
	}
	raw, _ := json.Marshal(tooMany)
	if _, err := supplierImportVariants(raw); err == nil || !strings.Contains(err.Error(), "too many variants") {
		t.Fatalf("variant count bound must be enforced")
	}
	unicode, err := supplierImportVariants(json.RawMessage(`[{"id":" 规格-大 ","name":"Large"}]`))
	if err != nil || len(unicode) != 1 || unicode[0].ExternalID != "规格-大" {
		t.Fatalf("Unicode variant fallback ID was rejected or changed: %#v %v", unicode, err)
	}
	if _, err := supplierImportVariants(json.RawMessage(`[{"external_id":"variant"},{"external_id":" variant "}]`)); err == nil {
		t.Fatal("duplicate normalized variant identifiers were accepted")
	}
	overlong, _ := json.Marshal([]map[string]string{{"external_id": strings.Repeat("界", 181)}})
	if _, err := supplierImportVariants(overlong); err == nil {
		t.Fatal("overlong variant identifier was accepted")
	}
}

func TestSupplierImportRequestUsesOpaqueExternalIDBoundary(t *testing.T) {
	req := supplierImportRequest{
		ExternalProductIDs: []string{" 商品-甲 ", "套餐/乙:02"},
		CategoryMode:       "mirror",
		PriceMode:          "fixed_markup",
	}
	if err := req.normalizeAndValidate(); err != nil {
		t.Fatalf("Unicode import identifiers rejected: %v", err)
	}
	if req.ExternalProductIDs[0] != "商品-甲" || req.ExternalProductIDs[1] != "套餐/乙:02" {
		t.Fatalf("import identifiers changed inconsistently: %#v", req.ExternalProductIDs)
	}
	unsafe := req
	unsafe.ExternalProductIDs = []string{"catalog/../secret"}
	if err := unsafe.normalizeAndValidate(); err == nil {
		t.Fatal("traversal import identifier was accepted")
	}
}

func TestSupplierImportVariantPriceAppliesUSDMarkupToCNY(t *testing.T) {
	req := supplierImportRequest{PriceMode: "fixed_markup", MarkupBasisPoint: 5000}
	prepared := supplierImportFX{
		Source:   model.CurrencyDefinition{MinorUnit: 2},
		Target:   model.CurrencyDefinition{MinorUnit: 2},
		Snapshot: model.FXRateSnapshot{Rate: "7.0267"},
	}
	sale, cost, err := supplierImportVariantPrice(req, 100, prepared)
	if err != nil {
		t.Fatalf("supplierImportVariantPrice: %v", err)
	}
	if sale != 1054 || cost != 703 {
		t.Fatalf("USD 1.00 at 7.0267 with 50%% markup = %d/%d, want 1054/703", sale, cost)
	}
}

func TestSupplierImportVariantPriceFixedAmountOverflow(t *testing.T) {
	const maxInt64 = int64(^uint64(0) >> 1)
	req := supplierImportRequest{PriceMode: "fixed_amount", MarkupAmount: maxInt64 - 100}
	prepared := supplierImportFX{
		Source:   model.CurrencyDefinition{MinorUnit: 2},
		Target:   model.CurrencyDefinition{MinorUnit: 2},
		Snapshot: model.FXRateSnapshot{Rate: "7.0267"},
	}
	if _, _, err := supplierImportVariantPrice(req, 100, prepared); err == nil {
		t.Fatalf("overflowing fixed amount markup must be rejected")
	}
}

func TestSupplierImportJobSnapshotRoundTripAndLegacyCompatibility(t *testing.T) {
	categoryID := uuid.New()
	request := supplierImportRequest{
		ExternalProductIDs: []string{"目录:商品-一"}, CategoryMode: "target",
		TargetCategoryID: &categoryID, PriceMode: "fixed_markup", SyncPrice: true,
	}
	encoded, err := json.Marshal(supplierImportJobSnapshot{Request: request, ChangeReason: "首次接入测试"})
	if err != nil {
		t.Fatalf("encode snapshot: %v", err)
	}
	decoded, err := decodeSupplierImportJobSnapshot(encoded)
	if err != nil || decoded.ChangeReason != "首次接入测试" || len(decoded.Request.ExternalProductIDs) != 1 {
		t.Fatalf("decode versioned snapshot: %#v %v", decoded, err)
	}
	legacy, _ := json.Marshal(request)
	decoded, err = decodeSupplierImportJobSnapshot(legacy)
	if err != nil || decoded.Request.TargetCategoryID == nil || *decoded.Request.TargetCategoryID != categoryID {
		t.Fatalf("decode legacy snapshot: %#v %v", decoded, err)
	}
}

func TestSupplierImportJobDTOReportsProgressAndDedicatedRetry(t *testing.T) {
	now := time.Now().UTC()
	job := model.SupplierCatalogImportJob{
		Base:       model.Base{ID: uuid.New(), CreatedAt: now, UpdatedAt: now},
		SupplierID: uuid.New(), TaskID: "task-safe", Status: "failed", Attempts: 3,
		RequestedCount: 8, ImportedCount: 3, SkippedCount: 1,
		ResultSnapshot: json.RawMessage(`{"product_ids":["safe-local-id"]}`),
		ErrorSummary:   "temporary database failure", CompletedAt: &now,
	}
	dto := toAdminSupplierImportJob(job)
	if dto.ProcessedCount != 4 || dto.ProgressPercent != 50 || !dto.CanRetry {
		t.Fatalf("unexpected import job DTO: %#v", dto)
	}
	encoded, err := json.Marshal(dto)
	if err != nil {
		t.Fatalf("encode import job DTO: %v", err)
	}
	if strings.Contains(string(encoded), "request_snapshot") || !strings.Contains(string(encoded), "safe-local-id") {
		t.Fatalf("DTO request/result contract is unsafe or incomplete: %s", encoded)
	}
}
