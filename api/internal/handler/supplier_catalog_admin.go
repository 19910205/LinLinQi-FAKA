package handler

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"linlinqi/api/internal/model"
	"linlinqi/api/pkg/response"
)

func adminSupplierFromPath(c *gin.Context, db *gorm.DB) (model.Supplier, bool) {
	supplierID, err := uuid.Parse(c.Param("id"))
	if err != nil || supplierID == uuid.Nil {
		response.Error(c, 422, 42504, "error.supplier_id_invalid")
		return model.Supplier{}, false
	}
	var supplierModel model.Supplier
	if err := db.First(&supplierModel, "id = ?", supplierID).Error; err != nil {
		response.Error(c, 404, 40502, "error.supplier_not_found")
		return model.Supplier{}, false
	}
	return supplierModel, true
}

func defaultSupplierSyncPolicy(supplierID uuid.UUID) model.SupplierSyncPolicy {
	return model.SupplierSyncPolicy{
		SupplierID: supplierID, MirrorRemoteMedia: true, SyncPrice: true,
		SyncStock: true, MissingProductAction: "keep",
	}
}

func (h Handler) AdminSupplierSyncPolicy(c *gin.Context) {
	supplierModel, ok := adminSupplierFromPath(c, h.DB)
	if !ok {
		return
	}
	policy := defaultSupplierSyncPolicy(supplierModel.ID)
	if err := h.DB.Where("supplier_id = ?", supplierModel.ID).First(&policy).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		response.Error(c, 500, 50505, "error.supplier_sync_policy_fetch_failed")
		return
	}
	c.Header("Cache-Control", "no-store")
	response.OK(c, policy)
}

type supplierSyncPolicyRequest struct {
	AutoSyncCategories   bool   `json:"auto_sync_categories"`
	AutoCreateCategories bool   `json:"auto_create_categories"`
	AutoSyncProducts     bool   `json:"auto_sync_products"`
	AutoCreateProducts   bool   `json:"auto_create_products"`
	SyncTitle            bool   `json:"sync_title"`
	SyncSummary          bool   `json:"sync_summary"`
	SyncDescription      bool   `json:"sync_description"`
	SyncMedia            bool   `json:"sync_media"`
	MirrorRemoteMedia    bool   `json:"mirror_remote_media"`
	SyncPrice            bool   `json:"sync_price"`
	SyncStock            bool   `json:"sync_stock"`
	SyncVariants         bool   `json:"sync_variants"`
	SyncStatus           bool   `json:"sync_status"`
	SyncPurchaseLimits   bool   `json:"sync_purchase_limits"`
	MissingProductAction string `json:"missing_product_action"`
}

func (r *supplierSyncPolicyRequest) normalizeAndValidate() error {
	r.MissingProductAction = strings.ToLower(strings.TrimSpace(r.MissingProductAction))
	if r.MissingProductAction == "" {
		r.MissingProductAction = "keep"
	}
	if r.MissingProductAction != "keep" && r.MissingProductAction != "unpublish" && r.MissingProductAction != "disable_mapping" {
		return errors.New("invalid missing-product action")
	}
	if r.AutoCreateCategories && !r.AutoSyncCategories {
		return errors.New("automatic category creation requires category sync")
	}
	if r.AutoCreateProducts && (!r.AutoSyncProducts || !r.AutoSyncCategories) {
		return errors.New("automatic product creation requires product and category sync")
	}
	if !r.SyncMedia && r.MirrorRemoteMedia {
		return errors.New("media mirroring requires media sync")
	}
	return nil
}

func supplierSyncPolicyColumns(req supplierSyncPolicyRequest) map[string]any {
	return map[string]any{
		"auto_sync_categories": req.AutoSyncCategories, "auto_create_categories": req.AutoCreateCategories,
		"auto_sync_products": req.AutoSyncProducts, "auto_create_products": req.AutoCreateProducts,
		"sync_title": req.SyncTitle, "sync_summary": req.SyncSummary,
		"sync_description": req.SyncDescription, "sync_media": req.SyncMedia,
		"mirror_remote_media": req.MirrorRemoteMedia, "sync_price": req.SyncPrice,
		"sync_stock": req.SyncStock, "sync_variants": req.SyncVariants,
		"sync_status": req.SyncStatus, "sync_purchase_limits": req.SyncPurchaseLimits,
		"missing_product_action": req.MissingProductAction,
	}
}

func (h Handler) UpdateSupplierSyncPolicy(c *gin.Context) {
	supplierModel, ok := adminSupplierFromPath(c, h.DB)
	if !ok {
		return
	}
	reason, ok := requireAdminChangeReason(c, "更新供应商同步策略")
	if !ok {
		return
	}
	var req supplierSyncPolicyRequest
	if decodeStrictJSON(c, &req) != nil || req.normalizeAndValidate() != nil {
		response.Error(c, 422, 42520, "error.supplier_sync_policy_fields_invalid")
		return
	}
	policy := defaultSupplierSyncPolicy(supplierModel.ID)
	err := h.DB.Transaction(func(tx *gorm.DB) error {
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("supplier_id = ?", supplierModel.ID).First(&policy).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			policy = defaultSupplierSyncPolicy(supplierModel.ID)
			if err := tx.Create(&policy).Error; err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
		if err := tx.Model(&policy).UpdateColumns(supplierSyncPolicyColumns(req)).Error; err != nil {
			return err
		}
		return tx.First(&policy, "id = ?", policy.ID).Error
	})
	if err != nil {
		response.Error(c, 500, 50506, "error.supplier_sync_policy_save_failed")
		return
	}
	h.audit(c, "supplier.sync-policy.update", "supplier", supplierModel.ID.String(), reason)
	response.OK(c, policy)
}

type adminSupplierRemoteCategory struct {
	ID                  uuid.UUID  `json:"id"`
	ExternalID          string     `json:"external_id"`
	ExternalParentID    string     `json:"external_parent_id"`
	Name                string     `json:"name"`
	Description         string     `json:"description"`
	ImageURL            string     `json:"image_url"`
	Sort                int        `json:"sort"`
	Status              string     `json:"status"`
	SnapshotHash        string     `json:"snapshot_hash"`
	LastSeenAt          time.Time  `json:"last_seen_at"`
	MappingID           *uuid.UUID `json:"mapping_id,omitempty"`
	LocalCategoryID     *uuid.UUID `json:"local_category_id,omitempty"`
	LocalCategoryName   string     `json:"local_category_name"`
	AutoCreate          bool       `json:"auto_create"`
	AutoPublish         bool       `json:"auto_publish"`
	SyncName            bool       `json:"sync_name"`
	SyncTitle           bool       `json:"sync_title"`
	SyncDescription     bool       `json:"sync_description"`
	SyncImage           bool       `json:"sync_image"`
	MirrorRemoteImage   bool       `json:"mirror_remote_image"`
	SyncParent          bool       `json:"sync_parent"`
	DefaultCoverURL     string     `json:"default_cover_url"`
	SyncPrice           bool       `json:"sync_price"`
	SyncStock           bool       `json:"sync_stock"`
	PriceMode           string     `json:"price_mode"`
	MarkupBasisPoint    int        `json:"markup_basis_point"`
	MarkupAmount        int64      `json:"markup_amount"`
	MarkupCurrency      string     `json:"markup_currency"`
	MappingLastSyncedAt *time.Time `json:"mapping_last_synced_at,omitempty"`
	MappingLastError    string     `json:"mapping_last_error"`
	MappingEnabled      bool       `json:"mapping_enabled"`
	MappingSort         int        `json:"mapping_sort"`
	ProductCount        int64      `json:"product_count"`
}

func (h Handler) AdminSupplierRemoteCategories(c *gin.Context) {
	supplierModel, ok := adminSupplierFromPath(c, h.DB)
	if !ok {
		return
	}
	page, pageSize := pagination(c)
	query := h.DB.Table("supplier_categories sc").
		Select(`sc.id, sc.external_id, sc.external_parent_id, sc.name, sc.description, sc.image_url,
			sc.sort, sc.status, sc.snapshot_hash, sc.last_seen_at,
			scm.id AS mapping_id, scm.category_id AS local_category_id,
			COALESCE(c.name, '') AS local_category_name,
			COALESCE(scm.auto_create, false) AS auto_create,
			COALESCE(scm.auto_publish, false) AS auto_publish,
			COALESCE(scm.sync_name, false) AS sync_name,
			COALESCE(scm.sync_title, false) AS sync_title,
			COALESCE(scm.sync_description, false) AS sync_description,
			COALESCE(scm.sync_image, false) AS sync_image,
			COALESCE(scm.mirror_remote_image, true) AS mirror_remote_image,
			COALESCE(scm.sync_parent, false) AS sync_parent,
			COALESCE(scm.default_cover_url, '') AS default_cover_url,
			COALESCE(scm.sync_price, true) AS sync_price,
			COALESCE(scm.sync_stock, true) AS sync_stock,
			COALESCE(scm.price_mode, 'fixed_markup') AS price_mode,
			COALESCE(scm.markup_basis_point, 0) AS markup_basis_point,
			COALESCE(scm.markup_amount, 0) AS markup_amount,
			COALESCE(NULLIF(scm.markup_currency, ''), 'CNY') AS markup_currency,
			scm.last_synced_at AS mapping_last_synced_at,
			COALESCE(scm.last_error, '') AS mapping_last_error,
			COALESCE(scm.enabled, true) AS mapping_enabled,
			COALESCE(scm.sort, 0) AS mapping_sort,
			(SELECT COUNT(*)
			 FROM supplier_catalog_products scp
			 WHERE scp.supplier_id = sc.supplier_id
			   AND scp.external_category_id = sc.external_id
			   AND scp.deleted_at IS NULL) AS product_count`).
		Joins("LEFT JOIN supplier_category_mappings scm ON scm.supplier_id = sc.supplier_id AND scm.external_category_id = sc.external_id AND scm.deleted_at IS NULL").
		Joins("LEFT JOIN categories c ON c.id = scm.category_id AND c.deleted_at IS NULL").
		Where("sc.supplier_id = ? AND sc.deleted_at IS NULL", supplierModel.ID)
	if keyword := strings.TrimSpace(c.Query("q")); keyword != "" {
		like := "%" + keyword + "%"
		query = query.Where("sc.name ILIKE ? OR sc.external_id ILIKE ? OR sc.description ILIKE ?", like, like, like)
	}
	if status := strings.ToLower(strings.TrimSpace(c.Query("status"))); status != "" {
		if status != "active" && status != "inactive" && status != "missing" {
			response.Error(c, 422, 42521, "error.supplier_catalog_filter_invalid")
			return
		}
		query = query.Where("sc.status = ?", status)
	}
	if mapped := strings.ToLower(strings.TrimSpace(c.Query("mapped"))); mapped != "" {
		if mapped != "true" && mapped != "false" {
			response.Error(c, 422, 42521, "error.supplier_catalog_filter_invalid")
			return
		}
		if mapped == "true" {
			query = query.Where("scm.id IS NOT NULL")
		} else {
			query = query.Where("scm.id IS NULL")
		}
	}
	var total int64
	if err := query.Session(&gorm.Session{}).Select("COUNT(DISTINCT sc.id)").Scan(&total).Error; err != nil {
		response.Error(c, 500, 50507, "error.supplier_remote_category_fetch_failed")
		return
	}
	items := make([]adminSupplierRemoteCategory, 0)
	if err := query.Order("sc.sort DESC, sc.name ASC, sc.external_id ASC").Offset((page - 1) * pageSize).Limit(pageSize).Scan(&items).Error; err != nil {
		response.Error(c, 500, 50507, "error.supplier_remote_category_fetch_failed")
		return
	}
	response.Page(c, items, total, page, pageSize)
}

type adminSupplierRemoteProduct struct {
	ID                 uuid.UUID       `json:"id"`
	ExternalID         string          `json:"external_id"`
	ParentExternalID   string          `json:"parent_external_id"`
	ExternalCategoryID string          `json:"external_category_id"`
	ExternalSKU        string          `json:"external_sku"`
	Name               string          `json:"name"`
	Summary            string          `json:"summary"`
	Description        string          `json:"description"`
	CoverURL           string          `json:"cover_url"`
	ImageURLs          json.RawMessage `json:"image_urls"`
	Country            string          `json:"country"`
	Tags               json.RawMessage `json:"tags"`
	Currency           string          `json:"currency"`
	Price              int64           `json:"price"`
	OriginalPrice      int64           `json:"original_price"`
	MemberPrice        int64           `json:"member_price"`
	WholesalePrices    json.RawMessage `json:"wholesale_prices"`
	Stock              int64           `json:"stock"`
	StockStatus        string          `json:"stock_status"`
	Minimum            int             `json:"minimum"`
	Maximum            int             `json:"maximum"`
	FulfillmentType    string          `json:"fulfillment_type"`
	Status             string          `json:"status"`
	UpstreamCreatedAt  *time.Time      `json:"upstream_created_at,omitempty"`
	UpstreamUpdatedAt  *time.Time      `json:"upstream_updated_at,omitempty"`
	Variants           json.RawMessage `json:"variants"`
	InputFields        json.RawMessage `json:"input_fields"`
	SnapshotHash       string          `json:"snapshot_hash"`
	LastSeenAt         time.Time       `json:"last_seen_at"`
	Mapped             bool            `json:"mapped"`
	MappingIDs         []uuid.UUID     `json:"mapping_ids"`
	LocalProductIDs    []uuid.UUID     `json:"local_product_ids"`
	LocalProductNames  []string        `json:"local_product_names"`
}

func (h Handler) AdminSupplierRemoteProducts(c *gin.Context) {
	supplierModel, ok := adminSupplierFromPath(c, h.DB)
	if !ok {
		return
	}
	page, pageSize := pagination(c)
	query := h.DB.Model(&model.SupplierCatalogProduct{}).Where("supplier_id = ?", supplierModel.ID)
	if keyword := strings.TrimSpace(c.Query("q")); keyword != "" {
		like := "%" + keyword + "%"
		query = query.Where("name ILIKE ? OR external_id ILIKE ? OR external_sku ILIKE ? OR description ILIKE ?", like, like, like, like)
	}
	if categoryID := strings.TrimSpace(c.Query("category_id")); categoryID != "" {
		query = query.Where("external_category_id = ?", categoryID)
	}
	if status := strings.ToLower(strings.TrimSpace(c.Query("status"))); status != "" {
		if status != "active" && status != "inactive" && status != "missing" {
			response.Error(c, 422, 42521, "error.supplier_catalog_filter_invalid")
			return
		}
		query = query.Where("status = ?", status)
	}
	if mapped := strings.ToLower(strings.TrimSpace(c.Query("mapped"))); mapped != "" {
		if mapped != "true" && mapped != "false" {
			response.Error(c, 422, 42521, "error.supplier_catalog_filter_invalid")
			return
		}
		exists := "EXISTS (SELECT 1 FROM product_mappings pm WHERE pm.supplier_id = supplier_catalog_products.supplier_id AND pm.external_product_id = supplier_catalog_products.external_id AND pm.deleted_at IS NULL)"
		if mapped == "true" {
			query = query.Where(exists)
		} else {
			query = query.Where("NOT " + exists)
		}
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		response.Error(c, 500, 50508, "error.supplier_remote_product_fetch_failed")
		return
	}
	var stored []model.SupplierCatalogProduct
	if err := query.Order("last_seen_at DESC, name ASC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&stored).Error; err != nil {
		response.Error(c, 500, 50508, "error.supplier_remote_product_fetch_failed")
		return
	}
	externalIDs := make([]string, 0, len(stored))
	for _, product := range stored {
		externalIDs = append(externalIDs, product.ExternalID)
	}
	type mappingRow struct {
		ID          uuid.UUID
		ExternalID  string
		ProductID   uuid.UUID
		ProductName string
	}
	rowsByExternal := make(map[string][]mappingRow, len(externalIDs))
	if len(externalIDs) > 0 {
		var rows []mappingRow
		if err := h.DB.Table("product_mappings pm").
			Select("pm.id, pm.external_product_id AS external_id, pm.product_id, p.name AS product_name").
			Joins("JOIN products p ON p.id = pm.product_id AND p.deleted_at IS NULL").
			Where("pm.supplier_id = ? AND pm.external_product_id IN ? AND pm.deleted_at IS NULL", supplierModel.ID, externalIDs).
			Order("pm.created_at ASC").Scan(&rows).Error; err != nil {
			response.Error(c, 500, 50508, "error.supplier_remote_product_fetch_failed")
			return
		}
		for _, row := range rows {
			rowsByExternal[row.ExternalID] = append(rowsByExternal[row.ExternalID], row)
		}
	}
	items := make([]adminSupplierRemoteProduct, 0, len(stored))
	for _, product := range stored {
		imageURLs, tags, wholesalePrices, variants, inputFields := product.ImageURLs, product.Tags, product.WholesalePrices, product.Variants, product.InputFields
		if !json.Valid(imageURLs) {
			imageURLs = json.RawMessage(`[]`)
		}
		if !json.Valid(tags) {
			tags = json.RawMessage(`[]`)
		}
		if !json.Valid(wholesalePrices) {
			wholesalePrices = json.RawMessage(`{}`)
		}
		if !json.Valid(variants) {
			variants = json.RawMessage(`[]`)
		}
		if !json.Valid(inputFields) {
			inputFields = json.RawMessage(`[]`)
		}
		item := adminSupplierRemoteProduct{
			ID: product.ID, ExternalID: product.ExternalID, ParentExternalID: product.ParentExternalID, ExternalCategoryID: product.ExternalCategoryID,
			ExternalSKU: product.ExternalSKU, Name: product.Name, Summary: product.Summary,
			Description: product.Description, CoverURL: product.CoverURL, ImageURLs: imageURLs,
			Country: product.Country, Tags: tags, Currency: product.Currency, Price: product.Price,
			OriginalPrice: product.OriginalPrice, MemberPrice: product.MemberPrice, WholesalePrices: wholesalePrices,
			Stock: product.Stock, StockStatus: product.StockStatus, Minimum: product.Minimum, Maximum: product.Maximum,
			FulfillmentType: product.FulfillmentType, Status: product.Status,
			UpstreamCreatedAt: product.UpstreamCreatedAt, UpstreamUpdatedAt: product.UpstreamUpdatedAt,
			Variants: variants, InputFields: inputFields, SnapshotHash: product.SnapshotHash,
			LastSeenAt: product.LastSeenAt, MappingIDs: []uuid.UUID{}, LocalProductIDs: []uuid.UUID{},
			LocalProductNames: []string{},
		}
		for _, row := range rowsByExternal[product.ExternalID] {
			item.MappingIDs = append(item.MappingIDs, row.ID)
			item.LocalProductIDs = append(item.LocalProductIDs, row.ProductID)
			item.LocalProductNames = append(item.LocalProductNames, row.ProductName)
		}
		item.Mapped = len(item.MappingIDs) > 0
		items = append(items, item)
	}
	response.Page(c, items, total, page, pageSize)
}

func (h Handler) AdminSupplierSyncRuns(c *gin.Context) {
	supplierModel, ok := adminSupplierFromPath(c, h.DB)
	if !ok {
		return
	}
	page, pageSize := pagination(c)
	query := h.DB.Model(&model.SupplierSyncRun{}).Where("supplier_id = ?", supplierModel.ID)
	if status := strings.ToLower(strings.TrimSpace(c.Query("status"))); status != "" {
		if status != "queued" && status != "running" && status != "succeeded" && status != "partial" && status != "failed" && status != "cancelled" {
			response.Error(c, 422, 42522, "error.supplier_sync_run_filter_invalid")
			return
		}
		query = query.Where("status = ?", status)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		response.Error(c, 500, 50509, "error.supplier_sync_runs_fetch_failed")
		return
	}
	items := make([]model.SupplierSyncRun, 0)
	if err := query.Order("started_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&items).Error; err != nil {
		response.Error(c, 500, 50509, "error.supplier_sync_runs_fetch_failed")
		return
	}
	response.Page(c, items, total, page, pageSize)
}

func (h Handler) AdminSupplierSyncRunChanges(c *gin.Context) {
	supplierModel, ok := adminSupplierFromPath(c, h.DB)
	if !ok {
		return
	}
	runID, err := uuid.Parse(c.Param("run_id"))
	if err != nil || runID == uuid.Nil {
		response.Error(c, 422, 42523, "error.supplier_sync_run_id_invalid")
		return
	}
	var count int64
	if err := h.DB.Model(&model.SupplierSyncRun{}).Where("id = ? AND supplier_id = ?", runID, supplierModel.ID).Count(&count).Error; err != nil || count != 1 {
		response.Error(c, 404, 40520, "error.supplier_sync_run_not_found")
		return
	}
	page, pageSize := pagination(c)
	query := h.DB.Model(&model.SupplierSyncChange{}).Where("run_id = ?", runID)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		response.Error(c, 500, 50510, "error.supplier_sync_changes_fetch_failed")
		return
	}
	items := make([]model.SupplierSyncChange, 0)
	if err := query.Order("created_at ASC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&items).Error; err != nil {
		response.Error(c, 500, 50510, "error.supplier_sync_changes_fetch_failed")
		return
	}
	response.Page(c, items, total, page, pageSize)
}
