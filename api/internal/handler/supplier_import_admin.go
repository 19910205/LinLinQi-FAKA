package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"linlinqi/api/internal/currency"
	"linlinqi/api/internal/model"
	"linlinqi/api/internal/queue"
	"linlinqi/api/internal/service"
	"linlinqi/api/internal/supply"
	"linlinqi/api/pkg/response"
)

const supplierImportMaximumProducts = 500

var supplierImportSlugInvalid = regexp.MustCompile(`[^a-z0-9]+`)

type supplierImportRequest struct {
	ExternalProductIDs []string   `json:"external_product_ids"`
	CategoryMode       string     `json:"category_mode"`
	TargetCategoryID   *uuid.UUID `json:"target_category_id"`
	AutoPublish        bool       `json:"auto_publish"`
	PriceMode          string     `json:"price_mode"`
	MarkupBasisPoint   int        `json:"markup_basis_point"`
	MarkupAmount       int64      `json:"markup_amount"`
	SyncTitle          bool       `json:"sync_title"`
	SyncSummary        bool       `json:"sync_summary"`
	SyncDescription    bool       `json:"sync_description"`
	SyncMedia          bool       `json:"sync_media"`
	MirrorRemoteMedia  bool       `json:"mirror_remote_media"`
	SyncPrice          bool       `json:"sync_price"`
	SyncStock          bool       `json:"sync_stock"`
	SyncVariants       bool       `json:"sync_variants"`
	SyncStatus         bool       `json:"sync_status"`
	SyncPurchaseLimits bool       `json:"sync_purchase_limits"`
}

func (r *supplierImportRequest) normalizeAndValidate() error {
	r.CategoryMode = strings.ToLower(strings.TrimSpace(r.CategoryMode))
	r.PriceMode = strings.ToLower(strings.TrimSpace(r.PriceMode))
	if r.CategoryMode != "mirror" && r.CategoryMode != "target" {
		return errors.New("invalid category mode")
	}
	if r.CategoryMode == "target" && (r.TargetCategoryID == nil || *r.TargetCategoryID == uuid.Nil) {
		return errors.New("target category is required")
	}
	if r.CategoryMode == "mirror" {
		r.TargetCategoryID = nil
	}
	if r.PriceMode == "" {
		r.PriceMode = "fixed_markup"
	}
	switch r.PriceMode {
	case "fixed_markup":
		if r.MarkupBasisPoint < 0 || r.MarkupBasisPoint > 100_000 {
			return errors.New("invalid import percentage markup")
		}
		r.MarkupAmount = 0
	case "fixed_amount":
		if r.MarkupAmount < 0 || r.MarkupAmount > 100_000_000 {
			return errors.New("invalid import fixed amount markup")
		}
		r.MarkupBasisPoint = 0
	default:
		return errors.New("invalid import pricing rule")
	}
	if len(r.ExternalProductIDs) < 1 || len(r.ExternalProductIDs) > supplierImportMaximumProducts {
		return errors.New("invalid import selection")
	}
	seen := make(map[string]struct{}, len(r.ExternalProductIDs))
	for index, externalID := range r.ExternalProductIDs {
		var err error
		externalID, err = supply.NormalizeExternalID(externalID)
		if err != nil {
			return errors.New("invalid external product identifier")
		}
		if _, duplicate := seen[externalID]; duplicate {
			return errors.New("duplicate external product identifier")
		}
		seen[externalID] = struct{}{}
		r.ExternalProductIDs[index] = externalID
	}
	if !r.SyncMedia && r.MirrorRemoteMedia {
		return errors.New("media mirroring requires media sync")
	}
	return nil
}

func supplierImportSlug(prefix, externalID string) string {
	base := strings.ToLower(strings.TrimSpace(prefix + "-" + externalID))
	base = strings.Trim(supplierImportSlugInvalid.ReplaceAllString(base, "-"), "-")
	if base == "" {
		base = "supplier-item"
	}
	digest := sha256.Sum256([]byte(prefix + ":" + externalID))
	if len(base) > 168 {
		base = strings.Trim(base[:168], "-")
	}
	return base + "-" + hex.EncodeToString(digest[:4])
}

func supplierImportSourceCurrency(supplierModel model.Supplier, product model.SupplierCatalogProduct) string {
	configured := strings.ToUpper(strings.TrimSpace(supplierModel.PriceCurrency))
	detected := strings.ToUpper(strings.TrimSpace(product.Currency))
	if supplierModel.CurrencyMode == "auto" && isoCurrencyCodePattern.MatchString(detected) {
		return detected
	}
	return configured
}

type supplierImportFX struct {
	Source   model.CurrencyDefinition
	Target   model.CurrencyDefinition
	Snapshot model.FXRateSnapshot
}

func (h Handler) prepareSupplierImportFX(ctx context.Context, supplierModel model.Supplier, products []model.SupplierCatalogProduct) (model.CurrencyDefinition, map[string]supplierImportFX, error) {
	storeCode := "CNY"
	var setting model.Setting
	if err := h.DB.Where("key = ?", "store_currency").First(&setting).Error; err == nil && strings.TrimSpace(setting.Value) != "" {
		storeCode = strings.ToUpper(strings.TrimSpace(setting.Value))
	} else if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return model.CurrencyDefinition{}, nil, err
	}
	var target model.CurrencyDefinition
	if err := h.DB.Where("code = ? AND enabled = ?", storeCode, true).First(&target).Error; err != nil {
		return model.CurrencyDefinition{}, nil, err
	}
	manager := currency.Manager{DB: h.DB, AllowPrivate: h.Cfg.Env != "production"}
	prepared := make(map[string]supplierImportFX)
	for _, product := range products {
		code := supplierImportSourceCurrency(supplierModel, product)
		if _, exists := prepared[code]; exists {
			continue
		}
		var source model.CurrencyDefinition
		if err := h.DB.Where("code = ? AND enabled = ?", code, true).First(&source).Error; err != nil {
			return model.CurrencyDefinition{}, nil, fmt.Errorf("supplier currency %s is unavailable", code)
		}
		snapshot, err := manager.Resolve(ctx, source.Code, target.Code)
		if err != nil {
			return model.CurrencyDefinition{}, nil, err
		}
		prepared[code] = supplierImportFX{Source: source, Target: target, Snapshot: snapshot}
	}
	return target, prepared, nil
}

type supplierCategoryImporter struct {
	tx                 *gorm.DB
	supplier           model.Supplier
	request            supplierImportRequest
	currency           string
	remote             map[string]model.SupplierCategory
	mappings           map[string]model.SupplierCategoryMapping
	configuredExternal map[string]struct{}
	created            int
	configured         int
}

func (importer *supplierCategoryImporter) mappingValues(remote model.SupplierCategory, categoryID uuid.UUID) map[string]any {
	mirrorCategories := importer.request.CategoryMode == "mirror"
	return map[string]any{
		"category_id": categoryID, "external_category_name": remote.Name,
		"auto_create":  mirrorCategories,
		"auto_publish": importer.request.AutoPublish, "sync_name": mirrorCategories,
		"sync_title":       importer.request.SyncTitle,
		"sync_description": mirrorCategories && importer.request.SyncDescription, "sync_image": mirrorCategories && importer.request.SyncMedia,
		"mirror_remote_image": mirrorCategories && importer.request.MirrorRemoteMedia, "sync_parent": mirrorCategories,
		"price_mode":         importer.request.PriceMode,
		"markup_basis_point": importer.request.MarkupBasisPoint, "markup_amount": importer.request.MarkupAmount,
		"markup_currency": importer.currency, "sync_price": importer.request.SyncPrice,
		"sync_stock": importer.request.SyncStock, "enabled": true, "last_error": "",
	}
}

func (importer *supplierCategoryImporter) bind(remote model.SupplierCategory, categoryID uuid.UUID) (model.SupplierCategoryMapping, error) {
	externalID, err := supply.NormalizeExternalID(remote.ExternalID)
	if err != nil {
		return model.SupplierCategoryMapping{}, errors.New("remote category identifier is invalid")
	}
	externalParentID, err := supply.NormalizeOptionalExternalID(remote.ExternalParentID)
	if err != nil {
		return model.SupplierCategoryMapping{}, errors.New("remote parent category identifier is invalid")
	}
	remote.ExternalID, remote.ExternalParentID = externalID, externalParentID
	if importer.configuredExternal == nil {
		importer.configuredExternal = make(map[string]struct{})
	}
	if _, done := importer.configuredExternal[remote.ExternalID]; done {
		if mapping, exists := importer.mappings[remote.ExternalID]; exists {
			return mapping, nil
		}
	}
	mapping, exists := importer.mappings[remote.ExternalID]
	if !exists {
		mapping = model.SupplierCategoryMapping{
			SupplierID: importer.supplier.ID, ExternalCategoryID: remote.ExternalID,
			ExternalCategoryName: remote.Name, CategoryID: &categoryID,
			PriceMode: importer.request.PriceMode, MarkupAmount: importer.request.MarkupAmount,
			MarkupCurrency: importer.currency, SyncPrice: importer.request.SyncPrice,
			SyncStock: importer.request.SyncStock, Enabled: true,
		}
		if err := importer.tx.Create(&mapping).Error; err != nil {
			return model.SupplierCategoryMapping{}, err
		}
	}
	if err := importer.tx.Model(&mapping).UpdateColumns(importer.mappingValues(remote, categoryID)).Error; err != nil {
		return model.SupplierCategoryMapping{}, err
	}
	mapping.CategoryID = &categoryID
	mapping.AutoCreate = importer.request.CategoryMode == "mirror"
	mapping.AutoPublish = importer.request.AutoPublish
	mapping.SyncName = mapping.AutoCreate
	mapping.SyncTitle = importer.request.SyncTitle
	mapping.SyncDescription = mapping.AutoCreate && importer.request.SyncDescription
	mapping.SyncImage = mapping.AutoCreate && importer.request.SyncMedia
	mapping.MirrorRemoteImage = mapping.AutoCreate && importer.request.MirrorRemoteMedia
	mapping.SyncParent = mapping.AutoCreate
	mapping.PriceMode, mapping.MarkupBasisPoint = importer.request.PriceMode, importer.request.MarkupBasisPoint
	mapping.MarkupAmount = importer.request.MarkupAmount
	mapping.SyncPrice, mapping.SyncStock, mapping.Enabled = importer.request.SyncPrice, importer.request.SyncStock, true
	importer.mappings[remote.ExternalID] = mapping
	importer.configuredExternal[remote.ExternalID] = struct{}{}
	importer.configured++
	return mapping, nil
}

func (importer *supplierCategoryImporter) ensure(externalID string, visiting map[string]bool) (uuid.UUID, error) {
	var err error
	externalID, err = supply.NormalizeExternalID(externalID)
	if err != nil {
		return uuid.Nil, errors.New("remote product has no category")
	}
	if mapping, exists := importer.mappings[externalID]; exists && mapping.CategoryID != nil {
		var count int64
		if err := importer.tx.Model(&model.Category{}).Where("id = ?", *mapping.CategoryID).Count(&count).Error; err != nil {
			return uuid.Nil, err
		}
		if count == 1 {
			if remote, remoteExists := importer.remote[externalID]; remoteExists {
				if _, err := importer.bind(remote, *mapping.CategoryID); err != nil {
					return uuid.Nil, err
				}
			}
			return *mapping.CategoryID, nil
		}
	}
	remote, exists := importer.remote[externalID]
	if !exists {
		return uuid.Nil, errors.New("remote category is unavailable")
	}
	if visiting[externalID] {
		return uuid.Nil, errors.New("remote category hierarchy contains a cycle")
	}
	visiting[externalID] = true
	defer delete(visiting, externalID)
	var parentID *uuid.UUID
	if strings.TrimSpace(remote.ExternalParentID) != "" {
		resolved, err := importer.ensure(remote.ExternalParentID, visiting)
		if err != nil {
			return uuid.Nil, err
		}
		parentID = &resolved
	}
	imageURL := ""
	if importer.request.SyncMedia && !importer.request.MirrorRemoteMedia {
		imageURL = remote.ImageURL
	}
	category := model.Category{
		ParentID: parentID, Name: supplierText(remote.Name, 100),
		Slug:        supplierImportSlug(importer.supplier.Code+"-category", remote.ExternalID),
		Description: supplierText(remote.Description, 2000), ImageURL: supplierText(imageURL, 1000),
		Sort: max(remote.Sort, 0), Enabled: importer.request.AutoPublish,
	}
	if err := createWithExplicitColumns(importer.tx, &category, map[string]any{"enabled": importer.request.AutoPublish}); err != nil {
		return uuid.Nil, err
	}
	if _, err := importer.bind(remote, category.ID); err != nil {
		return uuid.Nil, err
	}
	importer.created++
	return category.ID, nil
}

func supplierImportedInputFields(productID uuid.UUID, raw json.RawMessage) ([]model.ProductInputField, map[string]string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return []model.ProductInputField{}, map[string]string{}, nil
	}
	var upstream []supply.ProductInputField
	if err := json.Unmarshal(raw, &upstream); err != nil {
		return nil, nil, err
	}
	if len(upstream) > 20 {
		return nil, nil, errors.New("upstream product defines too many input fields")
	}
	return service.NormalizeSupplierInputFields(productID, upstream)
}

func supplierImportVariants(raw json.RawMessage) ([]supply.ProductVariant, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var variants []supply.ProductVariant
	if err := json.Unmarshal(raw, &variants); err != nil {
		return nil, err
	}
	if len(variants) > 50 {
		return nil, errors.New("upstream product defines too many variants")
	}
	seen := make(map[string]struct{}, len(variants))
	for index := range variants {
		externalID := variants[index].ExternalID
		if strings.TrimSpace(externalID) == "" {
			externalID = variants[index].ID
		}
		normalized, err := supply.NormalizeExternalID(externalID)
		if err != nil {
			return nil, errors.New("upstream variant identifier is invalid")
		}
		if _, exists := seen[normalized]; exists {
			return nil, errors.New("upstream variant identifiers are not unique")
		}
		seen[normalized] = struct{}{}
		variants[index].ExternalID = normalized
	}
	return variants, nil
}

func supplierImportVariantPrice(req supplierImportRequest, upstreamPrice int64, prepared supplierImportFX) (int64, int64, error) {
	convertedCost, err := currency.Convert(upstreamPrice, prepared.Source.MinorUnit, prepared.Target.MinorUnit, prepared.Snapshot.Rate)
	if err != nil {
		return 0, 0, err
	}
	if req.PriceMode == "fixed_amount" {
		const maxInt64 = int64(^uint64(0) >> 1)
		if convertedCost > maxInt64-req.MarkupAmount {
			return 0, 0, errors.New("supplier import fixed amount markup overflows")
		}
		return convertedCost + req.MarkupAmount, convertedCost, nil
	}
	salePrice, err := currency.ConvertWithMarkup(upstreamPrice, prepared.Source.MinorUnit, prepared.Target.MinorUnit, prepared.Snapshot.Rate, req.MarkupBasisPoint)
	return salePrice, convertedCost, err
}

type supplierImportResult struct {
	Requested          int      `json:"requested"`
	Imported           int      `json:"imported"`
	SkippedMapped      int      `json:"skipped_mapped"`
	CategoriesCreated  int      `json:"categories_created"`
	MappingsConfigured int      `json:"category_mappings_configured"`
	ProductIDs         []string `json:"product_ids"`
	FXSnapshotIDs      []string `json:"fx_snapshot_ids"`
	SyncQueueStatus    string   `json:"sync_queue_status"`
}

type supplierImportJobSnapshot struct {
	Request      supplierImportRequest `json:"request"`
	ChangeReason string                `json:"change_reason"`
}

type adminSupplierImportJob struct {
	ID                 uuid.UUID       `json:"id"`
	SupplierID         uuid.UUID       `json:"supplier_id"`
	TaskID             string          `json:"task_id,omitempty"`
	Status             string          `json:"status"`
	Attempts           int             `json:"attempts"`
	RequestedCount     int             `json:"requested_count"`
	ImportedCount      int             `json:"imported_count"`
	SkippedCount       int             `json:"skipped_count"`
	ProcessedCount     int             `json:"processed_count"`
	ProgressPercent    int             `json:"progress_percent"`
	CategoriesCreated  int             `json:"categories_created"`
	MappingsConfigured int             `json:"mappings_configured"`
	Result             json.RawMessage `json:"result"`
	ErrorSummary       string          `json:"error_summary"`
	StartedAt          *time.Time      `json:"started_at,omitempty"`
	CompletedAt        *time.Time      `json:"completed_at,omitempty"`
	NextAttemptAt      *time.Time      `json:"next_attempt_at,omitempty"`
	CanRetry           bool            `json:"can_retry"`
	CreatedAt          time.Time       `json:"created_at"`
	UpdatedAt          time.Time       `json:"updated_at"`
}

func toAdminSupplierImportJob(job model.SupplierCatalogImportJob) adminSupplierImportJob {
	processed := job.ImportedCount + job.SkippedCount
	percent := 0
	if job.RequestedCount > 0 {
		percent = min(100, processed*100/job.RequestedCount)
	}
	if job.Status == "succeeded" {
		percent = 100
	}
	result := job.ResultSnapshot
	if !json.Valid(result) {
		result = json.RawMessage(`{}`)
	}
	return adminSupplierImportJob{
		ID: job.ID, SupplierID: job.SupplierID, TaskID: job.TaskID,
		Status: job.Status, Attempts: job.Attempts, RequestedCount: job.RequestedCount,
		ImportedCount: job.ImportedCount, SkippedCount: job.SkippedCount,
		ProcessedCount: processed, ProgressPercent: percent,
		CategoriesCreated: job.CategoriesCreated, MappingsConfigured: job.MappingsConfigured,
		Result: result, ErrorSummary: job.ErrorSummary, StartedAt: job.StartedAt,
		CompletedAt: job.CompletedAt, NextAttemptAt: job.NextAttemptAt,
		CanRetry:  job.Status == "failed" || job.Status == "cancelled",
		CreatedAt: job.CreatedAt, UpdatedAt: job.UpdatedAt,
	}
}

func decodeSupplierImportJobSnapshot(raw json.RawMessage) (supplierImportJobSnapshot, error) {
	var snapshot supplierImportJobSnapshot
	if err := json.Unmarshal(raw, &snapshot); err == nil && len(snapshot.Request.ExternalProductIDs) > 0 {
		return snapshot, snapshot.Request.normalizeAndValidate()
	}
	// Migration 043 documented a direct request object before the worker was
	// wired. Accept it so a record created during a rolling deployment remains
	// executable.
	var legacy supplierImportRequest
	if err := json.Unmarshal(raw, &legacy); err != nil {
		return supplierImportJobSnapshot{}, err
	}
	if err := legacy.normalizeAndValidate(); err != nil {
		return supplierImportJobSnapshot{}, err
	}
	return supplierImportJobSnapshot{Request: legacy}, nil
}

func (h Handler) ImportSupplierProducts(c *gin.Context) {
	supplierModel, ok := adminSupplierFromPath(c, h.DB)
	if !ok {
		return
	}
	reason, ok := requireAdminChangeReason(c, "批量接入供应商商品")
	if !ok {
		return
	}
	var req supplierImportRequest
	if decodeStrictJSON(c, &req) != nil || req.normalizeAndValidate() != nil {
		response.Error(c, 422, 42524, "error.supplier_import_fields_invalid")
		return
	}
	if supplierModel.Status != "active" {
		response.Error(c, 409, 40521, "error.supplier_import_requires_active_supplier")
		return
	}
	if req.TargetCategoryID != nil {
		var count int64
		if err := h.DB.Model(&model.Category{}).Where("id = ?", *req.TargetCategoryID).Count(&count).Error; err != nil || count != 1 {
			response.Error(c, 422, 42524, "error.supplier_import_target_category_invalid")
			return
		}
	}
	var remoteProducts []model.SupplierCatalogProduct
	if err := h.DB.Where("supplier_id = ? AND external_id IN ?", supplierModel.ID, req.ExternalProductIDs).Find(&remoteProducts).Error; err != nil {
		response.Error(c, 500, 50511, "error.supplier_import_catalog_fetch_failed")
		return
	}
	if len(remoteProducts) != len(req.ExternalProductIDs) {
		response.Error(c, 409, 40522, "error.supplier_import_catalog_stale")
		return
	}
	for _, product := range remoteProducts {
		if product.Status == "missing" || product.Price < 0 || product.Stock < 0 {
			response.Error(c, 409, 40522, "error.supplier_import_catalog_stale")
			return
		}
	}
	snapshot, err := json.Marshal(supplierImportJobSnapshot{Request: req, ChangeReason: reason})
	if err != nil {
		response.Error(c, 500, 50512, "error.supplier_import_failed")
		return
	}
	var requestedBy *uuid.UUID
	if parsed, parseErr := uuid.Parse(c.GetString("subject")); parseErr == nil && parsed != uuid.Nil {
		requestedBy = &parsed
	}
	job := model.SupplierCatalogImportJob{
		SupplierID: supplierModel.ID, RequestedBy: requestedBy, Status: "queued",
		RequestedCount: len(req.ExternalProductIDs), RequestSnapshot: snapshot,
		ResultSnapshot: json.RawMessage(`{}`),
	}
	if err := h.DB.Create(&job).Error; err != nil {
		response.Error(c, 500, 50512, "error.supplier_import_failed")
		return
	}
	client := queue.NewClient(h.Cfg, h.DB)
	_, enqueueErr := client.EnqueueSupplierCatalogImport(job.ID)
	_ = client.Close()
	if reloadErr := h.DB.First(&job, "id = ?", job.ID).Error; reloadErr != nil {
		response.Error(c, 500, 50512, "error.supplier_import_failed")
		return
	}
	detail := fmt.Sprintf("%s；requested=%d；queue=%s", reason, job.RequestedCount, job.Status)
	if enqueueErr != nil {
		detail += "；durable_recovery=pending"
	}
	h.audit(c, "supplier.catalog.import.queued", "supplier_catalog_import_job", job.ID.String(), detail)
	c.Header("Cache-Control", "no-store")
	c.JSON(202, response.Envelope{Code: 0, Message: "accepted", Data: toAdminSupplierImportJob(job)})
}

// ProcessSupplierCatalogImportJob executes the immutable request approved by
// the operator. The catalog mutation and succeeded status are committed in the
// same database transaction, making an Asynq retry idempotent after crashes.
func (h Handler) ProcessSupplierCatalogImportJob(ctx context.Context, jobID uuid.UUID, taskID string, progress func(imported, skipped int)) error {
	var job model.SupplierCatalogImportJob
	err := h.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&job, "id = ?", jobID).Error; err != nil {
			return err
		}
		if job.Status != "running" || strings.TrimSpace(taskID) == "" || job.TaskID != taskID {
			return queue.ErrSupplierCatalogImportClaimLost
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("%w: supplier catalog import job does not exist", asynq.SkipRetry)
		}
		return err
	}
	snapshot, err := decodeSupplierImportJobSnapshot(job.RequestSnapshot)
	if err != nil {
		return fmt.Errorf("%w: invalid supplier catalog import snapshot", asynq.SkipRetry)
	}
	req := snapshot.Request
	var supplierModel model.Supplier
	if err := h.DB.WithContext(ctx).Where("id = ?", job.SupplierID).First(&supplierModel).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("%w: supplier is unavailable", asynq.SkipRetry)
		}
		return err
	}
	if supplierModel.Status != "active" {
		return fmt.Errorf("%w: supplier is not active", asynq.SkipRetry)
	}
	if req.TargetCategoryID != nil {
		var count int64
		if err := h.DB.WithContext(ctx).Model(&model.Category{}).Where("id = ?", *req.TargetCategoryID).Count(&count).Error; err != nil {
			return err
		}
		if count != 1 {
			return fmt.Errorf("%w: target category is unavailable", asynq.SkipRetry)
		}
	}
	var remoteProducts []model.SupplierCatalogProduct
	if err := h.DB.WithContext(ctx).Where("supplier_id = ? AND external_id IN ?", supplierModel.ID, req.ExternalProductIDs).Find(&remoteProducts).Error; err != nil {
		return err
	}
	if len(remoteProducts) != len(req.ExternalProductIDs) {
		return fmt.Errorf("%w: supplier catalog selection is stale", asynq.SkipRetry)
	}
	for _, product := range remoteProducts {
		if product.Status == "missing" || product.Price < 0 || product.Stock < 0 {
			return fmt.Errorf("%w: supplier catalog selection is stale", asynq.SkipRetry)
		}
	}
	targetCurrency, preparedFX, err := h.prepareSupplierImportFX(ctx, supplierModel, remoteProducts)
	if err != nil {
		return err
	}
	result := supplierImportResult{Requested: len(req.ExternalProductIDs), ProductIDs: []string{}, FXSnapshotIDs: []string{}}
	fxSeen := map[uuid.UUID]struct{}{}
	err = h.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var activeClaim int64
		if err := tx.Model(&model.SupplierCatalogImportJob{}).
			Where("id = ? AND task_id = ? AND status = ?", job.ID, taskID, "running").
			Count(&activeClaim).Error; err != nil {
			return err
		}
		if activeClaim != 1 {
			return queue.ErrSupplierCatalogImportClaimLost
		}
		if err := tx.Exec("LOCK TABLE product_mappings IN SHARE ROW EXCLUSIVE MODE").Error; err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&model.Supplier{}, "id = ?", supplierModel.ID).Error; err != nil {
			return err
		}
		var remoteCategories []model.SupplierCategory
		if err := tx.Where("supplier_id = ?", supplierModel.ID).Find(&remoteCategories).Error; err != nil {
			return err
		}
		categoryByExternal := make(map[string]model.SupplierCategory, len(remoteCategories))
		for _, category := range remoteCategories {
			categoryByExternal[category.ExternalID] = category
		}
		var storedMappings []model.SupplierCategoryMapping
		if err := tx.Where("supplier_id = ?", supplierModel.ID).Find(&storedMappings).Error; err != nil {
			return err
		}
		mappingByExternal := make(map[string]model.SupplierCategoryMapping, len(storedMappings))
		for _, mapping := range storedMappings {
			mappingByExternal[mapping.ExternalCategoryID] = mapping
		}
		categoryImporter := supplierCategoryImporter{tx: tx, supplier: supplierModel, request: req, currency: targetCurrency.Code, remote: categoryByExternal, mappings: mappingByExternal}
		for _, remote := range remoteProducts {
			var mappedCount int64
			if err := tx.Model(&model.ProductMapping{}).Where("supplier_id = ? AND external_product_id = ?", supplierModel.ID, remote.ExternalID).Count(&mappedCount).Error; err != nil {
				return err
			}
			if mappedCount > 0 {
				result.SkippedMapped++
				if progress != nil {
					progress(result.Imported, result.SkippedMapped)
				}
				continue
			}
			var categoryID uuid.UUID
			if req.CategoryMode == "target" {
				categoryID = *req.TargetCategoryID
				if remoteCategory, exists := categoryByExternal[remote.ExternalCategoryID]; exists {
					if _, err := categoryImporter.bind(remoteCategory, categoryID); err != nil {
						return err
					}
				}
			} else {
				resolved, err := categoryImporter.ensure(remote.ExternalCategoryID, map[string]bool{})
				if err != nil {
					return err
				}
				categoryID = resolved
			}
			var inheritedCategoryMappingID *uuid.UUID
			if categoryBinding, exists := categoryImporter.mappings[remote.ExternalCategoryID]; exists && categoryBinding.ID != uuid.Nil {
				bindingID := categoryBinding.ID
				inheritedCategoryMappingID = &bindingID
			}
			sourceCode := supplierImportSourceCurrency(supplierModel, remote)
			prepared, exists := preparedFX[sourceCode]
			if !exists {
				return errors.New("supplier import FX snapshot is unavailable")
			}
			convertedCost, err := currency.Convert(remote.Price, prepared.Source.MinorUnit, targetCurrency.MinorUnit, prepared.Snapshot.Rate)
			if err != nil {
				return err
			}
			var salePrice int64
			if req.PriceMode == "fixed_amount" {
				const maxInt64 = int64(^uint64(0) >> 1)
				if convertedCost > maxInt64-req.MarkupAmount {
					return errors.New("supplier import fixed amount markup overflows")
				}
				salePrice = convertedCost + req.MarkupAmount
			} else {
				salePrice, err = currency.ConvertWithMarkup(remote.Price, prepared.Source.MinorUnit, targetCurrency.MinorUnit, prepared.Snapshot.Rate, req.MarkupBasisPoint)
				if err != nil {
					return err
				}
			}
			comparePrice := int64(0)
			if remote.OriginalPrice > remote.Price {
				if req.PriceMode == "fixed_amount" {
					convertedOriginal, convertErr := currency.Convert(remote.OriginalPrice, prepared.Source.MinorUnit, targetCurrency.MinorUnit, prepared.Snapshot.Rate)
					if convertErr != nil {
						return convertErr
					}
					const maxInt64 = int64(^uint64(0) >> 1)
					if convertedOriginal > maxInt64-req.MarkupAmount {
						return errors.New("supplier import original price markup overflows")
					}
					comparePrice = convertedOriginal + req.MarkupAmount
				} else {
					comparePrice, err = currency.ConvertWithMarkup(remote.OriginalPrice, prepared.Source.MinorUnit, targetCurrency.MinorUnit, prepared.Snapshot.Rate, req.MarkupBasisPoint)
					if err != nil {
						return err
					}
				}
				if comparePrice <= salePrice {
					comparePrice = 0
				}
			}
			coverURL := ""
			if req.SyncMedia && !req.MirrorRemoteMedia {
				coverURL = remote.CoverURL
			}
			status := "draft"
			if req.AutoPublish && remote.Status == "active" {
				status = "on_sale"
			}
			deliveryType := strings.ToLower(strings.TrimSpace(remote.FulfillmentType))
			if deliveryType != "manual" {
				deliveryType = "auto"
			}
			var remoteTags []string
			if !json.Valid(remote.Tags) || json.Unmarshal(remote.Tags, &remoteTags) != nil {
				remoteTags = []string{}
			}
			local := model.Product{
				CategoryID: categoryID, Name: supplierText(remote.Name, 160),
				Slug:     supplierImportSlug(supplierModel.Code+"-product", remote.ExternalID),
				Currency: targetCurrency.Code, Price: salePrice, ComparePrice: comparePrice, CostPrice: convertedCost,
				DeliveryType: deliveryType, InventoryMode: "supplier", MinimumPurchase: max(remote.Minimum, 1), MaximumPurchase: max(remote.Maximum, 0), Status: status,
				Tags:     supplierText(strings.Join(remoteTags, ","), 500),
				CoverURL: supplierText(coverURL, 1000),
			}
			if req.SyncSummary {
				local.Summary = supplierText(remote.Summary, 500)
			}
			if req.SyncDescription {
				local.Description = supplierText(remote.Description, 100_000)
			}
			if err := tx.Create(&local).Error; err != nil {
				return err
			}
			inputFields, parameterMapping, err := supplierImportedInputFields(local.ID, remote.InputFields)
			if err != nil {
				return err
			}
			for index := range inputFields {
				if err := createWithExplicitColumns(tx, &inputFields[index], map[string]any{"enabled": true}); err != nil {
					return err
				}
			}
			encodedParameterMapping, err := service.EncodeSupplierParameterMapping(parameterMapping)
			if err != nil {
				return err
			}
			mapping := model.ProductMapping{
				SupplierID: supplierModel.ID, SupplierCategoryMappingID: inheritedCategoryMappingID,
				InheritCategoryPolicy: inheritedCategoryMappingID != nil,
				ProductID:             local.ID, ExternalProductID: remote.ExternalID,
				ParameterMapping: encodedParameterMapping, PriceMode: req.PriceMode,
				MarkupBasisPoint: req.MarkupBasisPoint, MarkupAmount: req.MarkupAmount, MarkupCurrency: targetCurrency.Code, FixedPriceCurrency: targetCurrency.Code,
				LastUpstreamPrice: remote.Price, LastUpstreamCurrency: sourceCode,
				LastConvertedCost: convertedCost, LastFXSnapshotID: &prepared.Snapshot.ID,
				AutoSyncPrice: req.SyncPrice, AutoSyncStock: req.SyncStock,
				AutoSyncTitle: req.SyncTitle, AutoSyncSummary: req.SyncSummary,
				AutoSyncDescription: req.SyncDescription, AutoSyncMedia: req.SyncMedia,
				MirrorRemoteMedia: req.MirrorRemoteMedia, AutoSyncCategory: req.CategoryMode == "mirror",
				AutoSyncVariants: req.SyncVariants, AutoSyncStatus: req.SyncStatus,
				AutoSyncLimits: req.SyncPurchaseLimits,
			}
			if err := tx.Create(&mapping).Error; err != nil {
				return err
			}
			if err := tx.Model(&mapping).UpdateColumns(map[string]any{
				"supplier_category_mapping_id": inheritedCategoryMappingID,
				"inherit_category_policy":      inheritedCategoryMappingID != nil,
				"auto_sync_price":              req.SyncPrice, "auto_sync_stock": req.SyncStock,
				"auto_sync_title": req.SyncTitle, "auto_sync_summary": req.SyncSummary,
				"auto_sync_description": req.SyncDescription, "auto_sync_media": req.SyncMedia,
				"mirror_remote_media": req.MirrorRemoteMedia, "auto_sync_category": req.CategoryMode == "mirror",
				"auto_sync_variants": req.SyncVariants, "auto_sync_status": req.SyncStatus,
				"auto_sync_limits": req.SyncPurchaseLimits,
			}).Error; err != nil {
				return err
			}
			snapshot := model.SupplierProduct{
				SupplierID: supplierModel.ID, ProductID: local.ID, ExternalID: remote.ExternalID,
				ExternalPrice: remote.Price, ExternalStock: remote.Stock,
				PriceMarkupRate: req.MarkupBasisPoint, AutoSync: req.SyncPrice || req.SyncStock,
			}
			if err := tx.Create(&snapshot).Error; err != nil {
				return err
			}
			if req.SyncVariants {
				variants, variantErr := supplierImportVariants(remote.Variants)
				if variantErr != nil {
					return variantErr
				}
				for _, upstreamVariant := range variants {
					externalVariantID := upstreamVariant.ExternalID
					variantSalePrice, variantCost, variantPriceErr := supplierImportVariantPrice(req, upstreamVariant.Price, prepared)
					if variantPriceErr != nil {
						return variantPriceErr
					}
					attributes, _ := json.Marshal(map[string]any{"external_id": upstreamVariant.ExternalID, "external_sku": upstreamVariant.ExternalSKU})
					variantStatus := "active"
					if strings.EqualFold(strings.TrimSpace(upstreamVariant.Status), "inactive") {
						variantStatus = "inactive"
					}
					localVariant := model.ProductVariant{
						ProductID:     local.ID,
						SKU:           supplierImportSlug(supplierModel.Code+"-variant", externalVariantID),
						Name:          supplierText(strings.TrimSpace(remote.Name+" / "+upstreamVariant.Name), 160),
						Attributes:    string(attributes),
						Price:         variantSalePrice,
						CostPrice:     variantCost,
						Status:        variantStatus,
						PurchaseLimit: max(upstreamVariant.Maximum, 0),
					}
					if err := tx.Create(&localVariant).Error; err != nil {
						return err
					}
					variantMapping := model.ProductMapping{
						SupplierID: supplierModel.ID, SupplierCategoryMappingID: inheritedCategoryMappingID,
						InheritCategoryPolicy: inheritedCategoryMappingID != nil,
						ProductID:             local.ID, VariantID: &localVariant.ID,
						ExternalProductID: externalVariantID, ParameterMapping: encodedParameterMapping,
						PriceMode: req.PriceMode, MarkupBasisPoint: req.MarkupBasisPoint,
						MarkupAmount: req.MarkupAmount, MarkupCurrency: targetCurrency.Code,
						FixedPriceCurrency: targetCurrency.Code, LastUpstreamPrice: upstreamVariant.Price,
						LastUpstreamCurrency: sourceCode, LastConvertedCost: variantCost,
						LastFXSnapshotID: &prepared.Snapshot.ID,
						AutoSyncPrice:    req.SyncPrice, AutoSyncStock: req.SyncStock,
						AutoSyncTitle: req.SyncTitle, AutoSyncSummary: req.SyncSummary,
						AutoSyncDescription: req.SyncDescription, AutoSyncMedia: req.SyncMedia,
						MirrorRemoteMedia: req.MirrorRemoteMedia, AutoSyncCategory: req.CategoryMode == "mirror",
						AutoSyncVariants: req.SyncVariants, AutoSyncStatus: req.SyncStatus,
						AutoSyncLimits: req.SyncPurchaseLimits,
					}
					if err := tx.Create(&variantMapping).Error; err != nil {
						return err
					}
					if err := tx.Model(&variantMapping).UpdateColumns(map[string]any{
						"supplier_category_mapping_id": inheritedCategoryMappingID,
						"inherit_category_policy":      inheritedCategoryMappingID != nil,
						"auto_sync_price":              req.SyncPrice, "auto_sync_stock": req.SyncStock,
						"auto_sync_title": req.SyncTitle, "auto_sync_summary": req.SyncSummary,
						"auto_sync_description": req.SyncDescription, "auto_sync_media": req.SyncMedia,
						"mirror_remote_media": req.MirrorRemoteMedia, "auto_sync_category": req.CategoryMode == "mirror",
						"auto_sync_variants": req.SyncVariants, "auto_sync_status": req.SyncStatus,
						"auto_sync_limits": req.SyncPurchaseLimits,
					}).Error; err != nil {
						return err
					}
					variantSnapshot := model.SupplierProduct{
						SupplierID: supplierModel.ID, ProductID: local.ID, VariantID: &localVariant.ID,
						ExternalID: externalVariantID, ExternalPrice: upstreamVariant.Price,
						ExternalStock: upstreamVariant.Stock, PriceMarkupRate: req.MarkupBasisPoint,
						AutoSync: req.SyncPrice || req.SyncStock,
					}
					if err := tx.Create(&variantSnapshot).Error; err != nil {
						return err
					}
				}
			}
			result.Imported++
			result.ProductIDs = append(result.ProductIDs, local.ID.String())
			fxSeen[prepared.Snapshot.ID] = struct{}{}
			if progress != nil {
				progress(result.Imported, result.SkippedMapped)
			}
		}
		result.CategoriesCreated = categoryImporter.created
		result.MappingsConfigured = categoryImporter.configured
		for snapshotID := range fxSeen {
			result.FXSnapshotIDs = append(result.FXSnapshotIDs, snapshotID.String())
		}
		result.SyncQueueStatus = "pending"
		encodedResult, err := json.Marshal(result)
		if err != nil {
			return err
		}
		completedAt := time.Now().UTC()
		update := tx.Model(&model.SupplierCatalogImportJob{}).
			Where("id = ? AND task_id = ? AND status = ?", job.ID, taskID, "running").
			Updates(map[string]any{
				"status": "succeeded", "imported_count": result.Imported,
				"skipped_count": result.SkippedMapped, "categories_created": result.CategoriesCreated,
				"mappings_configured": result.MappingsConfigured, "result_snapshot": encodedResult,
				"error_summary": "", "completed_at": &completedAt, "next_attempt_at": nil,
			})
		if update.Error != nil {
			return update.Error
		}
		if update.RowsAffected != 1 {
			return queue.ErrSupplierCatalogImportClaimLost
		}
		return nil
	})
	if err != nil {
		return err
	}
	client := queue.NewClient(h.Cfg, h.DB)
	_, enqueueErr := client.Enqueue(queue.TypeSupplierSync, map[string]string{"supplier_id": supplierModel.ID.String(), "trigger": "manual"}, asynq.Queue("default"), asynq.Unique(4*time.Minute))
	_ = client.Close()
	result.SyncQueueStatus = "queued"
	if isDuplicateSupplyTask(enqueueErr) {
		result.SyncQueueStatus = "already_queued"
	} else if enqueueErr != nil {
		result.SyncQueueStatus = "unavailable"
	}
	if encodedResult, encodeErr := json.Marshal(result); encodeErr == nil {
		_ = h.DB.WithContext(ctx).Model(&model.SupplierCatalogImportJob{}).
			Where("id = ? AND task_id = ? AND status = ?", job.ID, taskID, "succeeded").
			Update("result_snapshot", encodedResult).Error
	}
	detail := fmt.Sprintf("requested=%d；imported=%d；skipped=%d；sync_queue=%s", result.Requested, result.Imported, result.SkippedMapped, result.SyncQueueStatus)
	_ = h.DB.WithContext(ctx).Create(&model.AuditLog{
		AdminID: job.RequestedBy, Action: "supplier.catalog.import.completed",
		Resource: "supplier_catalog_import_job", ResourceID: job.ID.String(), Detail: detail,
	}).Error
	return nil
}

func (h Handler) AdminSupplierImportJobs(c *gin.Context) {
	supplierModel, ok := adminSupplierFromPath(c, h.DB)
	if !ok {
		return
	}
	page, pageSize := pagination(c)
	query := h.DB.Model(&model.SupplierCatalogImportJob{}).Where("supplier_id = ?", supplierModel.ID)
	if status := strings.ToLower(strings.TrimSpace(c.Query("status"))); status != "" {
		switch status {
		case "queued", "running", "retrying", "succeeded", "failed", "cancelled":
			query = query.Where("status = ?", status)
		default:
			response.Error(c, 422, 42525, "error.supplier_import_job_filter_invalid")
			return
		}
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		response.Error(c, 500, 50513, "error.supplier_import_jobs_fetch_failed")
		return
	}
	var jobs []model.SupplierCatalogImportJob
	if err := query.Order("created_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&jobs).Error; err != nil {
		response.Error(c, 500, 50513, "error.supplier_import_jobs_fetch_failed")
		return
	}
	items := make([]adminSupplierImportJob, 0, len(jobs))
	for _, job := range jobs {
		items = append(items, toAdminSupplierImportJob(job))
	}
	c.Header("Cache-Control", "no-store")
	response.Page(c, items, total, page, pageSize)
}

func (h Handler) AdminSupplierImportJob(c *gin.Context) {
	supplierModel, ok := adminSupplierFromPath(c, h.DB)
	if !ok {
		return
	}
	jobID, err := uuid.Parse(c.Param("job_id"))
	if err != nil || jobID == uuid.Nil {
		response.Error(c, 422, 42526, "error.supplier_import_job_id_invalid")
		return
	}
	var job model.SupplierCatalogImportJob
	if err := h.DB.Where("id = ? AND supplier_id = ?", jobID, supplierModel.ID).First(&job).Error; err != nil {
		response.Error(c, 404, 40524, "error.supplier_import_job_not_found")
		return
	}
	c.Header("Cache-Control", "no-store")
	response.OK(c, toAdminSupplierImportJob(job))
}

func (h Handler) RetrySupplierImportJob(c *gin.Context) {
	supplierModel, ok := adminSupplierFromPath(c, h.DB)
	if !ok {
		return
	}
	reason, ok := requireAdminChangeReason(c, "重试供应商商品接入任务")
	if !ok {
		return
	}
	jobID, err := uuid.Parse(c.Param("job_id"))
	if err != nil || jobID == uuid.Nil {
		response.Error(c, 422, 42526, "error.supplier_import_job_id_invalid")
		return
	}
	var job model.SupplierCatalogImportJob
	previousAttempts := 0
	err = h.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND supplier_id = ?", jobID, supplierModel.ID).First(&job).Error; err != nil {
			return err
		}
		if job.Status != "failed" && job.Status != "cancelled" {
			return errors.New("supplier import job is not retryable")
		}
		previousAttempts = job.Attempts
		return tx.Model(&job).Updates(map[string]any{
			"status": "queued", "task_id": "", "attempts": 0, "imported_count": 0, "skipped_count": 0,
			"categories_created": 0, "mappings_configured": 0,
			"result_snapshot": json.RawMessage(`{}`), "error_summary": "",
			"started_at": nil, "completed_at": nil, "next_attempt_at": nil,
		}).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		response.Error(c, 404, 40524, "error.supplier_import_job_not_found")
		return
	}
	if err != nil {
		response.Error(c, 409, 40525, "error.supplier_import_job_not_retryable")
		return
	}
	client := queue.NewClient(h.Cfg, h.DB)
	_, enqueueErr := client.EnqueueSupplierCatalogImport(job.ID)
	_ = client.Close()
	if reloadErr := h.DB.First(&job, "id = ?", job.ID).Error; reloadErr != nil {
		response.Error(c, 500, 50512, "error.supplier_import_failed")
		return
	}
	detail := fmt.Sprintf("%s；previous_attempts=%d；queue=%s", reason, previousAttempts, job.Status)
	if enqueueErr != nil {
		detail += "；durable_recovery=pending"
	}
	h.audit(c, "supplier.catalog.import.retry", "supplier_catalog_import_job", job.ID.String(), detail)
	c.Header("Cache-Control", "no-store")
	c.JSON(202, response.Envelope{Code: 0, Message: "accepted", Data: toAdminSupplierImportJob(job)})
}

// supplierText mirrors the queue boundary's Unicode-safe truncation without
// exposing queue internals to HTTP handlers.
func supplierText(value string, maximum int) string {
	value = strings.TrimSpace(value)
	runes := []rune(value)
	if len(runes) <= maximum {
		return value
	}
	return string(runes[:maximum])
}
