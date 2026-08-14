package handler

import (
	"errors"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"linlinqi/api/internal/model"
	"linlinqi/api/internal/service"
	"linlinqi/api/internal/supply"
	"linlinqi/api/pkg/response"
)

type supplierCategoryMappingRequest struct {
	SupplierID           uuid.UUID `json:"supplier_id"`
	CategoryID           uuid.UUID `json:"category_id"`
	ExternalCategoryID   string    `json:"external_category_id"`
	ExternalCategoryName string    `json:"external_category_name"`
	DefaultCoverURL      string    `json:"default_cover_url"`
	SyncCategoryName     bool      `json:"sync_category_name"`
	SyncTitle            bool      `json:"sync_title"`
	SyncDescription      bool      `json:"sync_description"`
	SyncImage            bool      `json:"sync_image"`
	MirrorRemoteImage    bool      `json:"mirror_remote_image"`
	SyncParent           bool      `json:"sync_parent"`
	SyncPrice            bool      `json:"sync_price"`
	SyncStock            bool      `json:"sync_stock"`
	AutoPublish          bool      `json:"auto_publish"`
	PriceMode            string    `json:"price_mode"`
	MarkupBasisPoint     int       `json:"markup_basis_point"`
	MarkupAmount         int64     `json:"markup_amount"`
	MarkupCurrency       string    `json:"markup_currency"`
	Sort                 int       `json:"sort"`
	Enabled              bool      `json:"enabled"`
}

func validSupplierDefaultCoverURL(value string) bool {
	if value == "" {
		return true
	}
	if strings.Contains(value, "#") {
		return false
	}
	parsed, err := url.ParseRequestURI(value)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "https" && parsed.Scheme != "http") {
		return false
	}
	return parsed.User == nil && parsed.Fragment == ""
}

func (r *supplierCategoryMappingRequest) normalizeAndValidate() error {
	externalCategoryID, identityErr := supply.NormalizeExternalID(r.ExternalCategoryID)
	if identityErr != nil {
		return errors.New("invalid category binding identity")
	}
	r.ExternalCategoryID = externalCategoryID
	r.ExternalCategoryName = strings.TrimSpace(r.ExternalCategoryName)
	r.DefaultCoverURL = strings.TrimSpace(r.DefaultCoverURL)
	r.PriceMode = strings.ToLower(strings.TrimSpace(r.PriceMode))
	r.MarkupCurrency = strings.ToUpper(strings.TrimSpace(r.MarkupCurrency))
	if r.SupplierID == uuid.Nil || r.CategoryID == uuid.Nil {
		return errors.New("invalid category binding identity")
	}
	if utf8.RuneCountInString(r.ExternalCategoryName) > 200 || len(r.DefaultCoverURL) > 1000 || !validSupplierDefaultCoverURL(r.DefaultCoverURL) {
		return errors.New("invalid category binding presentation")
	}
	if r.Sort < 0 || r.Sort > 1_000_000 || !isoCurrencyCodePattern.MatchString(r.MarkupCurrency) {
		return errors.New("invalid category binding values")
	}
	switch r.PriceMode {
	case "fixed_markup":
		if r.MarkupBasisPoint < 0 || r.MarkupBasisPoint > 100_000 {
			return errors.New("invalid category percentage markup")
		}
		r.MarkupAmount = 0
	case "fixed_amount":
		if r.MarkupAmount < 0 || r.MarkupAmount > 100_000_000 {
			return errors.New("invalid category fixed markup")
		}
		r.MarkupBasisPoint = 0
	default:
		return errors.New("invalid category pricing mode")
	}
	return nil
}

type adminSupplierCategoryMappingItem struct {
	ID                     uuid.UUID  `json:"id"`
	SupplierID             uuid.UUID  `json:"supplier_id"`
	SupplierName           string     `json:"supplier_name"`
	SupplierCode           string     `json:"supplier_code"`
	SupplierStatus         string     `json:"supplier_status"`
	CategoryID             *uuid.UUID `json:"category_id,omitempty"`
	CategoryName           string     `json:"category_name"`
	CategoryImageURL       string     `json:"category_image_url"`
	ExternalCategoryID     string     `json:"external_category_id"`
	ExternalCategoryName   string     `json:"external_category_name"`
	RemoteCategoryImageURL string     `json:"remote_category_image_url"`
	DefaultCoverURL        string     `json:"default_cover_url"`
	SyncTitle              bool       `json:"sync_title"`
	SyncCategoryName       bool       `json:"sync_category_name"`
	SyncDescription        bool       `json:"sync_description"`
	SyncImage              bool       `json:"sync_image"`
	MirrorRemoteImage      bool       `json:"mirror_remote_image"`
	SyncParent             bool       `json:"sync_parent"`
	SyncPrice              bool       `json:"sync_price"`
	SyncStock              bool       `json:"sync_stock"`
	AutoPublish            bool       `json:"auto_publish"`
	PriceMode              string     `json:"price_mode"`
	MarkupBasisPoint       int        `json:"markup_basis_point"`
	MarkupAmount           int64      `json:"markup_amount"`
	MarkupCurrency         string     `json:"markup_currency"`
	Sort                   int        `json:"sort"`
	Enabled                bool       `json:"enabled"`
	LastSyncedAt           *time.Time `json:"last_synced_at,omitempty"`
	LastError              string     `json:"last_error"`
	CreatedAt              time.Time  `json:"created_at"`
	UpdatedAt              time.Time  `json:"updated_at"`
}

func supplierCategoryMappingListQuery(db *gorm.DB) *gorm.DB {
	return db.Table("supplier_category_mappings scm").
		Select(`scm.id, scm.supplier_id, s.name AS supplier_name, s.code AS supplier_code, s.status AS supplier_status,
			scm.category_id, COALESCE(c.name, '') AS category_name, COALESCE(c.image_url, '') AS category_image_url,
			scm.external_category_id,
			COALESCE(NULLIF(scm.external_category_name, ''), sc.name, scm.external_category_id) AS external_category_name,
			COALESCE(sc.image_url, '') AS remote_category_image_url, scm.default_cover_url,
			scm.sync_title, scm.sync_name AS sync_category_name, scm.sync_description, scm.sync_image, scm.mirror_remote_image, scm.sync_parent,
			scm.sync_price, scm.sync_stock, scm.auto_publish, scm.price_mode, scm.markup_basis_point,
			scm.markup_amount, scm.markup_currency, scm.sort, scm.enabled, scm.last_synced_at,
			scm.last_error, scm.created_at, scm.updated_at`).
		Joins("JOIN suppliers s ON s.id = scm.supplier_id AND s.deleted_at IS NULL").
		Joins("LEFT JOIN categories c ON c.id = scm.category_id AND c.deleted_at IS NULL").
		Joins("LEFT JOIN supplier_categories sc ON sc.supplier_id = scm.supplier_id AND sc.external_id = scm.external_category_id AND sc.deleted_at IS NULL").
		Where("scm.deleted_at IS NULL")
}

func applySupplierCategoryMappingFilters(c *gin.Context, query *gorm.DB) (*gorm.DB, bool) {
	if raw := strings.TrimSpace(c.Query("supplier_id")); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil || id == uuid.Nil {
			response.Error(c, 422, 42504, "error.supplier_filter_id_invalid")
			return query, false
		}
		query = query.Where("scm.supplier_id = ?", id)
	}
	if raw := strings.TrimSpace(c.Query("category_id")); raw != "" {
		id, err := uuid.Parse(raw)
		if err != nil || id == uuid.Nil {
			response.Error(c, 422, 42521, "error.supplier_catalog_filter_invalid")
			return query, false
		}
		query = query.Where("scm.category_id = ?", id)
	}
	if raw := strings.ToLower(strings.TrimSpace(c.Query("status"))); raw != "" {
		if raw != "enabled" && raw != "disabled" {
			response.Error(c, 422, 42514, "error.supplier_status_filter_invalid")
			return query, false
		}
		query = query.Where("scm.enabled = ?", raw == "enabled")
	}
	if keyword := strings.TrimSpace(c.Query("q")); keyword != "" {
		like := "%" + keyword + "%"
		query = query.Where(`s.name ILIKE ? OR s.code ILIKE ? OR c.name ILIKE ? OR scm.external_category_id ILIKE ?
			OR scm.external_category_name ILIKE ? OR sc.name ILIKE ?`, like, like, like, like, like, like)
	}
	return query, true
}

func (h Handler) AdminSupplierCategoryMappings(c *gin.Context) {
	page, pageSize := pagination(c)
	query, ok := applySupplierCategoryMappingFilters(c, supplierCategoryMappingListQuery(h.DB))
	if !ok {
		return
	}
	var total int64
	if err := query.Session(&gorm.Session{}).Select("COUNT(DISTINCT scm.id)").Scan(&total).Error; err != nil {
		response.Error(c, 500, 50507, "error.supplier_remote_category_fetch_failed")
		return
	}
	items := make([]adminSupplierCategoryMappingItem, 0)
	if err := query.Order("scm.sort ASC, scm.created_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Scan(&items).Error; err != nil {
		response.Error(c, 500, 50507, "error.supplier_remote_category_fetch_failed")
		return
	}
	response.Page(c, items, total, page, pageSize)
}

type supplierCategoryMappingSummary struct {
	Total     int64 `json:"total"`
	Enabled   int64 `json:"enabled"`
	Disabled  int64 `json:"disabled"`
	Suppliers int64 `json:"suppliers"`
}

func (h Handler) AdminSupplierCategoryMappingSummary(c *gin.Context) {
	query, ok := applySupplierCategoryMappingFilters(c, supplierCategoryMappingListQuery(h.DB))
	if !ok {
		return
	}
	var item supplierCategoryMappingSummary
	if err := query.Select(`COUNT(DISTINCT scm.id) AS total,
		COUNT(DISTINCT scm.id) FILTER (WHERE scm.enabled) AS enabled,
		COUNT(DISTINCT scm.id) FILTER (WHERE NOT scm.enabled) AS disabled,
		COUNT(DISTINCT scm.supplier_id) AS suppliers`).Scan(&item).Error; err != nil {
		response.Error(c, 500, 50507, "error.supplier_remote_category_fetch_failed")
		return
	}
	response.OK(c, item)
}

func (h Handler) validateSupplierCategoryMappingRelations(tx *gorm.DB, req *supplierCategoryMappingRequest) error {
	if err := tx.Select("id").First(&model.Supplier{}, "id = ?", req.SupplierID).Error; err != nil {
		return err
	}
	if err := tx.Select("id").First(&model.Category{}, "id = ?", req.CategoryID).Error; err != nil {
		return err
	}
	var currencyCount int64
	if err := tx.Model(&model.CurrencyDefinition{}).Where("code = ? AND enabled = ?", req.MarkupCurrency, true).Count(&currencyCount).Error; err != nil || currencyCount != 1 {
		return errors.New("markup currency is unavailable")
	}
	if req.PriceMode == "fixed_amount" {
		storeCurrency, err := service.StoreCurrency(tx)
		if err != nil {
			return err
		}
		if req.MarkupCurrency != storeCurrency {
			return errors.New("fixed amount markup currency must match store currency")
		}
	}
	if req.ExternalCategoryName == "" {
		var remote model.SupplierCategory
		if err := tx.Select("name").Where("supplier_id = ? AND external_id = ?", req.SupplierID, req.ExternalCategoryID).First(&remote).Error; err == nil {
			req.ExternalCategoryName = strings.TrimSpace(remote.Name)
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
	}
	if req.ExternalCategoryName == "" {
		req.ExternalCategoryName = req.ExternalCategoryID
	}
	return nil
}

func supplierCategoryMappingColumns(req supplierCategoryMappingRequest) map[string]any {
	return map[string]any{
		"supplier_id": req.SupplierID, "category_id": req.CategoryID,
		"external_category_id": req.ExternalCategoryID, "external_category_name": req.ExternalCategoryName,
		"default_cover_url": req.DefaultCoverURL, "sync_name": req.SyncCategoryName, "sync_title": req.SyncTitle,
		"sync_description": req.SyncDescription, "sync_image": req.SyncImage,
		"mirror_remote_image": req.MirrorRemoteImage, "sync_parent": req.SyncParent,
		"sync_price": req.SyncPrice, "sync_stock": req.SyncStock, "auto_publish": req.AutoPublish,
		"price_mode": req.PriceMode, "markup_basis_point": req.MarkupBasisPoint,
		"markup_amount": req.MarkupAmount, "markup_currency": req.MarkupCurrency,
		"sort": req.Sort, "enabled": req.Enabled,
	}
}

func applySupplierCategoryPolicyToInheritedMappings(tx *gorm.DB, binding model.SupplierCategoryMapping) error {
	if binding.Enabled {
		if binding.CategoryID == nil {
			return errors.New("category binding target is unavailable")
		}
		var liveCategory int64
		if err := tx.Model(&model.Category{}).Where("id = ?", *binding.CategoryID).Count(&liveCategory).Error; err != nil {
			return err
		}
		if liveCategory != 1 {
			return errors.New("category binding target is unavailable")
		}
	}
	updates := map[string]any{
		"auto_sync_price": false,
		"auto_sync_stock": false,
		"auto_sync_title": false,
	}
	if binding.Enabled {
		updates = map[string]any{
			"price_mode": binding.PriceMode, "markup_basis_point": binding.MarkupBasisPoint,
			"markup_amount": binding.MarkupAmount, "markup_currency": binding.MarkupCurrency,
			"auto_sync_price": binding.SyncPrice, "auto_sync_stock": binding.SyncStock,
			"auto_sync_title": binding.SyncTitle,
		}
	}
	query := tx.Model(&model.ProductMapping{}).
		Where("supplier_category_mapping_id = ? AND inherit_category_policy = ?", binding.ID, true)
	if err := query.Updates(updates).Error; err != nil {
		return err
	}
	if binding.Enabled && binding.CategoryID != nil {
		productUpdates := map[string]any{"category_id": *binding.CategoryID}
		if binding.DefaultCoverURL != "" {
			productUpdates["cover_url"] = binding.DefaultCoverURL
		}
		return tx.Model(&model.Product{}).
			Where(`id IN (
				SELECT product_id FROM product_mappings
				WHERE supplier_category_mapping_id = ? AND inherit_category_policy = true
				  AND variant_id IS NULL AND deleted_at IS NULL
			)`, binding.ID).
			Updates(productUpdates).Error
	}
	return nil
}

func (h Handler) CreateSupplierCategoryMapping(c *gin.Context) {
	reason, ok := requireAdminChangeReason(c, "创建供应分类绑定")
	if !ok {
		return
	}
	var req supplierCategoryMappingRequest
	if decodeStrictJSON(c, &req) != nil || req.normalizeAndValidate() != nil {
		response.Error(c, 422, 42524, "error.supplier_fields_invalid")
		return
	}
	item := model.SupplierCategoryMapping{}
	err := h.DB.Transaction(func(tx *gorm.DB) error {
		if err := h.validateSupplierCategoryMappingRelations(tx, &req); err != nil {
			return err
		}
		item = model.SupplierCategoryMapping{
			SupplierID: req.SupplierID, CategoryID: &req.CategoryID,
			ExternalCategoryID: req.ExternalCategoryID, ExternalCategoryName: req.ExternalCategoryName,
			DefaultCoverURL: req.DefaultCoverURL, SyncName: req.SyncCategoryName, SyncTitle: req.SyncTitle,
			SyncDescription: req.SyncDescription, SyncImage: req.SyncImage,
			MirrorRemoteImage: req.MirrorRemoteImage, SyncParent: req.SyncParent,
			SyncPrice: req.SyncPrice, SyncStock: req.SyncStock, AutoPublish: req.AutoPublish,
			PriceMode: req.PriceMode, MarkupBasisPoint: req.MarkupBasisPoint,
			MarkupAmount: req.MarkupAmount, MarkupCurrency: req.MarkupCurrency,
			Sort: req.Sort, Enabled: req.Enabled,
		}
		return createWithExplicitColumns(tx, &item, supplierCategoryMappingColumns(req))
	})
	if err != nil {
		response.Error(c, 409, 40519, "error.supplier_mapping_conflict")
		return
	}
	h.audit(c, "supplier.category-mapping.create", "supplier_category_mapping", item.ID.String(), reason)
	response.Created(c, item)
}

func (h Handler) UpdateSupplierCategoryMapping(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil || id == uuid.Nil {
		response.Error(c, 422, 42504, "error.supplier_id_invalid")
		return
	}
	reason, ok := requireAdminChangeReason(c, "更新供应分类绑定")
	if !ok {
		return
	}
	var req supplierCategoryMappingRequest
	if decodeStrictJSON(c, &req) != nil || req.normalizeAndValidate() != nil {
		response.Error(c, 422, 42524, "error.supplier_fields_invalid")
		return
	}
	var item model.SupplierCategoryMapping
	err = h.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&item, "id = ?", id).Error; err != nil {
			return err
		}
		if err := h.validateSupplierCategoryMappingRelations(tx, &req); err != nil {
			return err
		}
		if err := tx.Model(&item).Updates(supplierCategoryMappingColumns(req)).Error; err != nil {
			return err
		}
		if err := tx.First(&item, "id = ?", item.ID).Error; err != nil {
			return err
		}
		return applySupplierCategoryPolicyToInheritedMappings(tx, item)
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		response.Error(c, 404, 40502, "error.supplier_mapping_conflict")
		return
	}
	if err != nil {
		response.Error(c, 409, 40519, "error.supplier_mapping_conflict")
		return
	}
	h.audit(c, "supplier.category-mapping.update", "supplier_category_mapping", item.ID.String(), reason)
	h.DB.First(&item, "id = ?", item.ID)
	response.OK(c, item)
}

func (h Handler) DeleteSupplierCategoryMapping(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil || id == uuid.Nil {
		response.Error(c, 422, 42504, "error.supplier_id_invalid")
		return
	}
	reason, ok := requireAdminChangeReason(c, "删除供应分类绑定")
	if !ok {
		return
	}
	var item model.SupplierCategoryMapping
	err = h.DB.Transaction(func(tx *gorm.DB) error {
		items, deleteErr := deleteSupplierCategoryMappings(tx, []uuid.UUID{id})
		if deleteErr == nil {
			item = items[0]
		}
		return deleteErr
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		response.Error(c, 404, 40502, "error.supplier_mapping_conflict")
		return
	}
	if err != nil {
		response.Error(c, 500, 50507, "error.supplier_remote_category_fetch_failed")
		return
	}
	h.audit(c, "supplier.category-mapping.delete", "supplier_category_mapping", item.ID.String(), reason)
	response.OK(c, gin.H{"deleted": true})
}

// deleteSupplierCategoryMappings is the single source of truth for operator
// deletion. It fences the complete target set, detaches inherited product
// policies, and leaves GORM soft-delete tombstones that prevent automatic
// catalog sync from silently recreating an operator-deleted binding.
func deleteSupplierCategoryMappings(tx *gorm.DB, ids []uuid.UUID) ([]model.SupplierCategoryMapping, error) {
	items := make([]model.SupplierCategoryMapping, 0, len(ids))
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id IN ?", ids).
		Order("id ASC").
		Find(&items).Error; err != nil {
		return nil, err
	}
	if len(items) != len(ids) {
		return nil, gorm.ErrRecordNotFound
	}
	if err := tx.Model(&model.ProductMapping{}).
		Where("supplier_category_mapping_id IN ? AND inherit_category_policy = ?", ids, true).
		Updates(map[string]any{"supplier_category_mapping_id": nil, "inherit_category_policy": false}).Error; err != nil {
		return nil, err
	}
	if err := tx.Delete(&items).Error; err != nil {
		return nil, err
	}
	return items, nil
}

type supplierCategoryMappingBatchStatusRequest struct {
	IDs     []uuid.UUID `json:"ids"`
	Enabled bool        `json:"enabled"`
}

type supplierCategoryMappingBatchDeleteRequest struct {
	IDs []uuid.UUID `json:"ids"`
}

func validSupplierCategoryMappingBatchIDs(ids []uuid.UUID) bool {
	if len(ids) < 1 || len(ids) > 100 {
		return false
	}
	seen := make(map[uuid.UUID]struct{}, len(ids))
	for _, id := range ids {
		if id == uuid.Nil {
			return false
		}
		seen[id] = struct{}{}
	}
	return len(seen) == len(ids)
}

func (h Handler) BatchDeleteSupplierCategoryMappings(c *gin.Context) {
	reason, ok := requireAdminChangeReason(c, "批量删除供应分类绑定")
	if !ok {
		return
	}
	var req supplierCategoryMappingBatchDeleteRequest
	if decodeStrictJSON(c, &req) != nil || !validSupplierCategoryMappingBatchIDs(req.IDs) {
		response.Error(c, 422, 42524, "error.supplier_fields_invalid")
		return
	}
	err := h.DB.Transaction(func(tx *gorm.DB) error {
		_, deleteErr := deleteSupplierCategoryMappings(tx, req.IDs)
		return deleteErr
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		response.Error(c, 409, 40519, "error.supplier_mapping_conflict")
		return
	}
	if err != nil {
		response.Error(c, 500, 50507, "error.supplier_remote_category_fetch_failed")
		return
	}
	h.audit(c, "supplier.category-mapping.batch-delete", "supplier_category_mapping", "batch", reason)
	response.OK(c, gin.H{"deleted": len(req.IDs)})
}

func (h Handler) BatchUpdateSupplierCategoryMappingStatus(c *gin.Context) {
	reason, ok := requireAdminChangeReason(c, "批量更新供应分类绑定状态")
	if !ok {
		return
	}
	var req supplierCategoryMappingBatchStatusRequest
	if decodeStrictJSON(c, &req) != nil || !validSupplierCategoryMappingBatchIDs(req.IDs) {
		response.Error(c, 422, 42524, "error.supplier_fields_invalid")
		return
	}
	err := h.DB.Transaction(func(tx *gorm.DB) error {
		var items []model.SupplierCategoryMapping
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id IN ?", req.IDs).Find(&items).Error; err != nil {
			return err
		}
		if len(items) != len(req.IDs) {
			return gorm.ErrRecordNotFound
		}
		if err := tx.Model(&model.SupplierCategoryMapping{}).Where("id IN ?", req.IDs).Update("enabled", req.Enabled).Error; err != nil {
			return err
		}
		for index := range items {
			items[index].Enabled = req.Enabled
			if err := applySupplierCategoryPolicyToInheritedMappings(tx, items[index]); err != nil {
				return err
			}
		}
		return nil
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		response.Error(c, 409, 40519, "error.supplier_mapping_conflict")
		return
	}
	if err != nil {
		response.Error(c, 500, 50507, "error.supplier_remote_category_fetch_failed")
		return
	}
	h.audit(c, "supplier.category-mapping.batch-status", "supplier_category_mapping", "batch", reason)
	response.OK(c, gin.H{"updated": len(req.IDs), "enabled": req.Enabled})
}
