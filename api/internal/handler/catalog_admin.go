package handler

import (
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"linlinqi/api/internal/model"
	"linlinqi/api/internal/service"
	"linlinqi/api/pkg/response"
)

var (
	catalogSlugPattern       = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,178}[a-z0-9])?$`)
	catalogCodePattern       = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{1,59}$`)
	catalogSKUPattern        = regexp.MustCompile(`^[A-Z0-9][A-Z0-9._-]{0,99}$`)
	errCatalogInUse          = errors.New("catalog record is in use")
	errCatalogUnsafeChange   = errors.New("catalog change is unsafe")
	errCatalogConflict       = errors.New("catalog record conflicts")
	errCatalogRelation       = errors.New("catalog relation is invalid")
	errCatalogPaymentChannel = errors.New("catalog payment channel relation is invalid")
	errCatalogInvalidRequest = errors.New("catalog request is invalid")
)

type categoryCatalogRequest struct {
	Name        string  `json:"name"`
	Slug        string  `json:"slug"`
	Description string  `json:"description"`
	Icon        string  `json:"icon"`
	ParentID    *string `json:"parent_id"`
	Sort        int     `json:"sort"`
	Enabled     bool    `json:"enabled"`
}

func (r *categoryCatalogRequest) normalizeAndValidate() error {
	r.Name = strings.TrimSpace(r.Name)
	r.Slug = strings.ToLower(strings.TrimSpace(r.Slug))
	r.Description = strings.TrimSpace(r.Description)
	r.Icon = strings.TrimSpace(r.Icon)
	if utf8.RuneCountInString(r.Name) < 1 || utf8.RuneCountInString(r.Name) > 100 || !catalogSlugPattern.MatchString(r.Slug) || utf8.RuneCountInString(r.Description) > 2000 || utf8.RuneCountInString(r.Icon) > 40 || r.Sort < 0 || r.Sort > 1_000_000 {
		return errCatalogInvalidRequest
	}
	if r.ParentID != nil {
		value := strings.TrimSpace(*r.ParentID)
		if value == "" {
			r.ParentID = nil
		} else if _, err := uuid.Parse(value); err != nil {
			return errCatalogInvalidRequest
		} else {
			r.ParentID = &value
		}
	}
	return nil
}

func categoryRequestParentID(request categoryCatalogRequest) *uuid.UUID {
	if request.ParentID == nil {
		return nil
	}
	parsed, _ := uuid.Parse(*request.ParentID)
	return &parsed
}

func ensureCatalogCategoryParent(tx *gorm.DB, categoryID uuid.UUID, parentID *uuid.UUID, requireEnabled bool) error {
	if parentID == nil {
		return nil
	}
	if *parentID == categoryID {
		return errCatalogRelation
	}
	query := tx.Select("id", "enabled").Where("id = ?", *parentID)
	if requireEnabled {
		query = query.Where("enabled = ?", true)
	}
	if err := query.First(&model.Category{}).Error; err != nil {
		return errCatalogRelation
	}
	if categoryID == uuid.Nil {
		return nil
	}
	var descendantCount int64
	if err := tx.Raw(`WITH RECURSIVE descendants AS (
		SELECT id FROM categories WHERE parent_id = ? AND deleted_at IS NULL
		UNION ALL
		SELECT child.id FROM categories child JOIN descendants parent ON child.parent_id = parent.id WHERE child.deleted_at IS NULL
	) SELECT COUNT(*) FROM descendants WHERE id = ?`, categoryID, *parentID).Scan(&descendantCount).Error; err != nil {
		return err
	}
	if descendantCount > 0 {
		return errCatalogRelation
	}
	return nil
}

func (h Handler) CreateCatalogCategory(c *gin.Context) {
	reason, ok := requireAdminChangeReason(c, "创建商品分类")
	if !ok {
		return
	}
	var req categoryCatalogRequest
	if decodeStrictJSON(c, &req) != nil || req.normalizeAndValidate() != nil {
		response.Error(c, 422, 42292, "error.category_fields_invalid")
		return
	}
	parentID := categoryRequestParentID(req)
	item := model.Category{ParentID: parentID, Name: req.Name, Slug: req.Slug, Description: req.Description, Icon: req.Icon, Sort: req.Sort, Enabled: req.Enabled}
	if err := h.DB.Transaction(func(tx *gorm.DB) error {
		if err := ensureCatalogCategoryParent(tx, uuid.Nil, parentID, req.Enabled); err != nil {
			return err
		}
		return createWithExplicitColumns(tx, &item, map[string]any{"enabled": req.Enabled})
	}); err != nil {
		if errors.Is(err, errCatalogRelation) {
			response.Error(c, 422, 42364, "error.category_parent_invalid")
			return
		}
		response.Error(c, 409, 40978, "error.category_slug_exists")
		return
	}
	item.Enabled = req.Enabled
	h.audit(c, "category.create", "category", item.ID.String(), reason)
	response.Created(c, item)
}

func (h Handler) UpdateCatalogCategory(c *gin.Context) {
	categoryID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, 422, 42293, "error.category_id_invalid")
		return
	}
	reason, ok := requireAdminChangeReason(c, "更新商品分类")
	if !ok {
		return
	}
	var req categoryCatalogRequest
	if decodeStrictJSON(c, &req) != nil || req.normalizeAndValidate() != nil {
		response.Error(c, 422, 42292, "error.category_fields_invalid")
		return
	}
	var item model.Category
	parentID := categoryRequestParentID(req)
	err = h.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&item, "id = ?", categoryID).Error; err != nil {
			return err
		}
		if item.Enabled && !req.Enabled {
			var onSale int64
			if err := tx.Model(&model.Product{}).Where("category_id = ? AND status = ?", item.ID, "on_sale").Count(&onSale).Error; err != nil {
				return err
			}
			if onSale > 0 {
				return errCatalogUnsafeChange
			}
			var enabledChildren int64
			if err := tx.Model(&model.Category{}).Where("parent_id = ? AND enabled = ?", item.ID, true).Count(&enabledChildren).Error; err != nil {
				return err
			}
			if enabledChildren > 0 {
				return errCatalogUnsafeChange
			}
		}
		if err := ensureCatalogCategoryParent(tx, item.ID, parentID, req.Enabled); err != nil {
			return err
		}
		return tx.Model(&item).Updates(map[string]any{"parent_id": parentID, "name": req.Name, "slug": req.Slug, "description": req.Description, "icon": req.Icon, "sort": req.Sort, "enabled": req.Enabled}).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		response.Error(c, 404, 40474, "error.two_factor_status_read_failed")
		return
	}
	if errors.Is(err, errCatalogUnsafeChange) {
		response.Error(c, 409, 40979, "error.category_has_products_on_sale")
		return
	}
	if errors.Is(err, errCatalogRelation) {
		response.Error(c, 422, 42364, "error.category_parent_invalid")
		return
	}
	if err != nil {
		response.Error(c, 409, 40978, "error.category_slug_exists_or_update_failed")
		return
	}
	h.audit(c, "category.update", "category", item.ID.String(), reason)
	h.DB.First(&item, "id = ?", item.ID)
	response.OK(c, item)
}

func (h Handler) DeleteCatalogCategory(c *gin.Context) {
	categoryID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, 422, 42293, "error.category_id_invalid")
		return
	}
	reason, ok := requireAdminChangeReason(c, "删除商品分类")
	if !ok {
		return
	}
	var item model.Category
	err = h.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&item, "id = ?", categoryID).Error; err != nil {
			return err
		}
		var products, children int64
		if err := tx.Model(&model.Product{}).Where("category_id = ?", item.ID).Count(&products).Error; err != nil {
			return err
		}
		if products > 0 {
			return errCatalogInUse
		}
		if err := tx.Model(&model.Category{}).Where("parent_id = ?", item.ID).Count(&children).Error; err != nil {
			return err
		}
		if children > 0 {
			return errCatalogInUse
		}
		return tx.Delete(&item).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		response.Error(c, 404, 40474, "error.two_factor_status_read_failed")
		return
	}
	if errors.Is(err, errCatalogInUse) {
		response.Error(c, 409, 40980, "error.category_has_products_cannot_delete")
		return
	}
	if err != nil {
		response.Error(c, 500, 50077, "error.product_category_delete_failed")
		return
	}
	h.audit(c, "category.delete", "category", item.ID.String(), reason)
	response.OK(c, gin.H{"deleted": true})
}

type catalogProductRequest struct {
	CategoryID        string   `json:"category_id"`
	Name              string   `json:"name"`
	Slug              string   `json:"slug"`
	Summary           string   `json:"summary"`
	Description       string   `json:"description"`
	Price             *int64   `json:"price"`
	ComparePrice      int64    `json:"compare_price"`
	CostPrice         int64    `json:"cost_price"`
	DeliveryType      string   `json:"delivery_type"`
	InventoryMode     string   `json:"inventory_mode"`
	MinimumPurchase   int      `json:"minimum_purchase"`
	MaximumPurchase   int      `json:"maximum_purchase"`
	Status            string   `json:"status"`
	Featured          bool     `json:"featured"`
	Tags              string   `json:"tags"`
	Sort              int      `json:"sort"`
	PaymentChannelIDs []string `json:"payment_channel_ids"`
}

func (r *catalogProductRequest) normalizeAndValidate() error {
	r.CategoryID = strings.TrimSpace(r.CategoryID)
	r.Name = strings.TrimSpace(r.Name)
	r.Slug = strings.ToLower(strings.TrimSpace(r.Slug))
	r.Summary = strings.TrimSpace(r.Summary)
	r.Description = strings.TrimSpace(r.Description)
	r.DeliveryType = strings.ToLower(strings.TrimSpace(defaultString(r.DeliveryType, "auto")))
	r.InventoryMode = strings.ToLower(strings.TrimSpace(defaultString(r.InventoryMode, "local")))
	if r.MinimumPurchase == 0 {
		r.MinimumPurchase = 1
	}
	r.Status = strings.ToLower(strings.TrimSpace(defaultString(r.Status, "draft")))
	r.Tags = strings.TrimSpace(r.Tags)
	if _, err := uuid.Parse(r.CategoryID); err != nil || r.Price == nil || utf8.RuneCountInString(r.Name) < 1 || utf8.RuneCountInString(r.Name) > 160 || !catalogSlugPattern.MatchString(r.Slug) || utf8.RuneCountInString(r.Summary) > 500 || utf8.RuneCountInString(r.Description) > 100_000 || utf8.RuneCountInString(r.Tags) > 500 || r.Sort < 0 || r.Sort > 1_000_000 || r.MinimumPurchase < 1 || r.MinimumPurchase > 1_000_000 || r.MaximumPurchase < 0 || r.MaximumPurchase > 1_000_000 || (r.MaximumPurchase > 0 && r.MaximumPurchase < r.MinimumPurchase) {
		return errCatalogInvalidRequest
	}
	if err := validateProductValues(*r.Price, r.ComparePrice, r.CostPrice, r.DeliveryType, r.InventoryMode, r.Status); err != nil {
		return err
	}
	if r.ComparePrice != 0 && r.ComparePrice < *r.Price {
		return errCatalogInvalidRequest
	}
	return nil
}

func (r catalogProductRequest) product(categoryID uuid.UUID) model.Product {
	return model.Product{CategoryID: categoryID, Name: r.Name, Slug: r.Slug, Summary: r.Summary, Description: r.Description, Price: *r.Price, ComparePrice: r.ComparePrice, CostPrice: r.CostPrice, DeliveryType: r.DeliveryType, InventoryMode: r.InventoryMode, MinimumPurchase: r.MinimumPurchase, MaximumPurchase: r.MaximumPurchase, Status: r.Status, Featured: r.Featured, Tags: r.Tags, Sort: r.Sort}
}

func ensureCatalogCategory(tx *gorm.DB, categoryID uuid.UUID, requireEnabled bool) error {
	var category model.Category
	query := tx.Select("id", "enabled").Where("id = ?", categoryID)
	if requireEnabled {
		query = query.Where("enabled = ?", true)
	}
	if err := query.First(&category).Error; err != nil {
		return errCatalogRelation
	}
	return nil
}

func (h Handler) CreateCatalogProduct(c *gin.Context) {
	reason, ok := requireAdminChangeReason(c, "创建商品")
	if !ok {
		return
	}
	var req catalogProductRequest
	if decodeStrictJSON(c, &req) != nil || req.normalizeAndValidate() != nil {
		response.Error(c, 422, 42294, "error.product_fields_invalid")
		return
	}
	categoryID, _ := uuid.Parse(req.CategoryID)
	channelIDs, channelErr := parseCatalogPaymentChannelIDs(req.PaymentChannelIDs)
	if channelErr != nil {
		response.Error(c, 422, 42297, "error.product_payment_channels_invalid")
		return
	}
	item := req.product(categoryID)
	err := h.DB.Transaction(func(tx *gorm.DB) error {
		if err := ensureCatalogCategory(tx, categoryID, req.Status == "on_sale"); err != nil {
			return err
		}
		if err := ensureCatalogPaymentChannels(tx, channelIDs); err != nil {
			return err
		}
		if err := tx.Create(&item).Error; err != nil {
			return err
		}
		return replaceProductPaymentChannels(tx, item.ID, channelIDs)
	})
	if errors.Is(err, errCatalogRelation) {
		response.Error(c, 422, 42295, "error.product_category_not_found")
		return
	}
	if errors.Is(err, errCatalogPaymentChannel) {
		response.Error(c, 422, 42297, "error.product_payment_channels_invalid")
		return
	}
	if err != nil {
		response.Error(c, 409, 40981, "error.product_slug_exists_or_save_failed")
		return
	}
	h.audit(c, "product.create", "product", item.ID.String(), reason)
	h.DB.Preload("Category").First(&item, "id = ?", item.ID)
	dto, dtoErr := singleAdminCatalogProductDTO(h.DB, item)
	if dtoErr != nil {
		response.Error(c, 500, 50086, "error.product_fetch_failed")
		return
	}
	response.Created(c, dto)
}

func (h Handler) UpdateCatalogProduct(c *gin.Context) {
	productID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, 422, 42296, "error.product_id_invalid")
		return
	}
	reason, ok := requireAdminChangeReason(c, "更新商品")
	if !ok {
		return
	}
	var req catalogProductRequest
	if decodeStrictJSON(c, &req) != nil || req.normalizeAndValidate() != nil {
		response.Error(c, 422, 42294, "error.product_fields_invalid")
		return
	}
	categoryID, _ := uuid.Parse(req.CategoryID)
	channelIDs, channelErr := parseCatalogPaymentChannelIDs(req.PaymentChannelIDs)
	if channelErr != nil {
		response.Error(c, 422, 42297, "error.product_payment_channels_invalid")
		return
	}
	var item model.Product
	err = h.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("LOCK TABLE products IN ACCESS EXCLUSIVE MODE").Error; err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&item, "id = ?", productID).Error; err != nil {
			return err
		}
		if err := ensureCatalogCategory(tx, categoryID, req.Status == "on_sale"); err != nil {
			return err
		}
		if err := ensureCatalogPaymentChannels(tx, channelIDs); err != nil {
			return err
		}
		if req.InventoryMode != item.InventoryMode {
			var unfinished int64
			if err := tx.Model(&model.Order{}).
				Joins("JOIN order_items ON order_items.order_id = orders.id AND order_items.deleted_at IS NULL").
				Where("order_items.product_id = ? AND orders.status IN ?", item.ID, []string{"pending_payment", "processing"}).
				Distinct("orders.id").Count(&unfinished).Error; err != nil {
				return err
			}
			if unfinished > 0 {
				return errProductHasUnfinishedOrders
			}
			if item.InventoryMode == "local" && req.InventoryMode == "supplier" {
				var localStock int64
				if err := tx.Model(&model.Card{}).Where("product_id = ? AND status IN ?", item.ID, []string{"available", "locked"}).Count(&localStock).Error; err != nil {
					return err
				}
				if localStock > 0 {
					return errCatalogUnsafeChange
				}
			}
		}
		updates := map[string]any{"category_id": categoryID, "name": req.Name, "slug": req.Slug, "summary": req.Summary, "description": req.Description, "price": *req.Price, "compare_price": req.ComparePrice, "cost_price": req.CostPrice, "delivery_type": req.DeliveryType, "inventory_mode": req.InventoryMode, "minimum_purchase": req.MinimumPurchase, "maximum_purchase": req.MaximumPurchase, "status": req.Status, "featured": req.Featured, "tags": req.Tags, "sort": req.Sort}
		if err := tx.Model(&item).Updates(updates).Error; err != nil {
			return err
		}
		return replaceProductPaymentChannels(tx, item.ID, channelIDs)
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		response.Error(c, 404, 40475, "error.product_not_found")
		return
	}
	if errors.Is(err, errCatalogRelation) {
		response.Error(c, 422, 42295, "error.product_category_not_found")
		return
	}
	if errors.Is(err, errCatalogPaymentChannel) {
		response.Error(c, 422, 42297, "error.product_payment_channels_invalid")
		return
	}
	if errors.Is(err, errProductHasUnfinishedOrders) {
		response.Error(c, 409, 40933, "error.pending_orders_cannot_switch_inventory_mode")
		return
	}
	if errors.Is(err, errCatalogUnsafeChange) {
		response.Error(c, 409, 40982, "error.local_keys_remaining_cannot_switch_supplier")
		return
	}
	if err != nil {
		response.Error(c, 409, 40981, "error.product_slug_exists_or_update_failed")
		return
	}
	h.audit(c, "product.update", "product", item.ID.String(), reason)
	h.DB.Preload("Category").First(&item, "id = ?", item.ID)
	dto, dtoErr := singleAdminCatalogProductDTO(h.DB, item)
	if dtoErr != nil {
		response.Error(c, 500, 50086, "error.product_fetch_failed")
		return
	}
	response.OK(c, dto)
}

func (h Handler) DeleteCatalogProduct(c *gin.Context) {
	productID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, 422, 42296, "error.product_id_invalid")
		return
	}
	reason, ok := requireAdminChangeReason(c, "删除商品")
	if !ok {
		return
	}
	var item model.Product
	err = h.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&item, "id = ?", productID).Error; err != nil {
			return err
		}
		if item.Status == "on_sale" {
			return errCatalogUnsafeChange
		}
		var orders, cards, batches int64
		if err := tx.Model(&model.OrderItem{}).Where("product_id = ?", item.ID).Count(&orders).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.Card{}).Where("product_id = ?", item.ID).Count(&cards).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.InventoryBatch{}).Where("product_id = ?", item.ID).Count(&batches).Error; err != nil {
			return err
		}
		if orders+cards+batches > 0 {
			return errCatalogInUse
		}
		for _, target := range []any{&model.CartItem{}, &model.ProductPriceTier{}, &model.ProductVariant{}, &model.ProductMapping{}, &model.SupplierProduct{}, &model.ResellerProductRule{}, &model.PromotionProduct{}} {
			if err := tx.Where("product_id = ?", item.ID).Delete(target).Error; err != nil {
				return err
			}
		}
		if err := tx.Where("product_id = ?", item.ID).Delete(&model.ProductPaymentChannel{}).Error; err != nil {
			return err
		}
		return tx.Delete(&item).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		response.Error(c, 404, 40475, "error.product_not_found")
		return
	}
	if errors.Is(err, errCatalogUnsafeChange) {
		response.Error(c, 409, 40983, "error.product_on_sale_must_delist_first")
		return
	}
	if errors.Is(err, errCatalogInUse) {
		response.Error(c, 409, 40984, "error.product_has_orders_cannot_delete")
		return
	}
	if err != nil {
		response.Error(c, 500, 50078, "error.product_delete_failed")
		return
	}
	h.audit(c, "product.delete", "product", item.ID.String(), reason)
	response.OK(c, gin.H{"deleted": true})
}

type productVariantCatalogRequest struct {
	ProductID     string             `json:"product_id"`
	SKU           string             `json:"sku"`
	Name          string             `json:"name"`
	Attributes    *map[string]string `json:"attributes"`
	Price         *int64             `json:"price"`
	ComparePrice  int64              `json:"compare_price"`
	CostPrice     int64              `json:"cost_price"`
	Status        string             `json:"status"`
	Sort          int                `json:"sort"`
	PurchaseLimit int                `json:"purchase_limit"`
}

func (r *productVariantCatalogRequest) normalizeAndValidate() error {
	r.ProductID = strings.TrimSpace(r.ProductID)
	r.SKU = strings.ToUpper(strings.TrimSpace(r.SKU))
	r.Name = strings.TrimSpace(r.Name)
	r.Status = strings.ToLower(strings.TrimSpace(defaultString(r.Status, "active")))
	if _, err := uuid.Parse(r.ProductID); err != nil || !catalogSKUPattern.MatchString(r.SKU) || utf8.RuneCountInString(r.Name) < 1 || utf8.RuneCountInString(r.Name) > 160 || r.Attributes == nil || r.Price == nil || *r.Price < 0 || r.ComparePrice < 0 || r.CostPrice < 0 || (r.ComparePrice != 0 && r.ComparePrice < *r.Price) || (r.Status != "active" && r.Status != "inactive") || r.Sort < 0 || r.Sort > 1_000_000 || r.PurchaseLimit < 0 || r.PurchaseLimit > 1_000_000 {
		return errCatalogInvalidRequest
	}
	if len(*r.Attributes) > 20 {
		return errCatalogInvalidRequest
	}
	clean := make(map[string]string, len(*r.Attributes))
	for key, value := range *r.Attributes {
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		if key == "" || utf8.RuneCountInString(key) > 80 || utf8.RuneCountInString(value) > 200 {
			return errCatalogInvalidRequest
		}
		clean[key] = value
	}
	r.Attributes = &clean
	return nil
}

func variantAttributes(attributes map[string]string) string {
	raw, _ := json.Marshal(attributes)
	return string(raw)
}

type adminProductVariantDTO struct {
	model.ProductVariant
	ProductName string `json:"product_name"`
	Currency    string `json:"currency"`
}

func (h Handler) AdminProductVariants(c *gin.Context) {
	page, pageSize := pagination(c)
	query := h.DB.Model(&model.ProductVariant{})
	if productID := strings.TrimSpace(c.Query("product_id")); productID != "" {
		parsed, err := uuid.Parse(productID)
		if err != nil {
			response.Error(c, 422, 42297, "error.product_filter_id_invalid")
			return
		}
		query = query.Where("product_id = ?", parsed)
	}
	if status := strings.TrimSpace(c.Query("status")); status != "" {
		if status != "active" && status != "inactive" {
			response.Error(c, 422, 42298, "error.spec_status_filter_invalid")
			return
		}
		query = query.Where("status = ?", status)
	}
	if keyword := strings.TrimSpace(c.Query("q")); keyword != "" {
		like := "%" + keyword + "%"
		query = query.Where("sku ILIKE ? OR name ILIKE ?", like, like)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		response.Error(c, 500, 50079, "error.product_sku_list_fetch_failed")
		return
	}
	var variants []model.ProductVariant
	if err := query.Order("created_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&variants).Error; err != nil {
		response.Error(c, 500, 50079, "error.product_sku_list_fetch_failed")
		return
	}
	productIDs := make([]uuid.UUID, 0, len(variants))
	for _, item := range variants {
		productIDs = append(productIDs, item.ProductID)
	}
	var products []model.Product
	h.DB.Select("id", "name", "currency").Where("id IN ?", productIDs).Find(&products)
	names := make(map[uuid.UUID]string, len(products))
	currencies := make(map[uuid.UUID]string, len(products))
	for _, product := range products {
		names[product.ID] = product.Name
		currencies[product.ID] = product.Currency
	}
	items := make([]adminProductVariantDTO, 0, len(variants))
	for _, item := range variants {
		items = append(items, adminProductVariantDTO{ProductVariant: item, ProductName: names[item.ProductID], Currency: currencies[item.ProductID]})
	}
	response.Page(c, items, total, page, pageSize)
}

func (h Handler) CreateCatalogProductVariant(c *gin.Context) {
	reason, ok := requireAdminChangeReason(c, "创建商品规格")
	if !ok {
		return
	}
	var req productVariantCatalogRequest
	if decodeStrictJSON(c, &req) != nil || req.normalizeAndValidate() != nil {
		response.Error(c, 422, 42299, "error.spec_fields_invalid")
		return
	}
	productID, _ := uuid.Parse(req.ProductID)
	item := model.ProductVariant{ProductID: productID, SKU: req.SKU, Name: req.Name, Attributes: variantAttributes(*req.Attributes), Price: *req.Price, ComparePrice: req.ComparePrice, CostPrice: req.CostPrice, Status: req.Status, Sort: req.Sort, PurchaseLimit: req.PurchaseLimit}
	err := h.DB.Transaction(func(tx *gorm.DB) error {
		var product model.Product
		if err := tx.Select("id").First(&product, "id = ?", productID).Error; err != nil {
			return errCatalogRelation
		}
		return tx.Create(&item).Error
	})
	if errors.Is(err, errCatalogRelation) {
		response.Error(c, 404, 40476, "error.parent_product_not_found")
		return
	}
	if err != nil {
		response.Error(c, 409, 40985, "error.spec_sku_exists_or_save_failed")
		return
	}
	h.audit(c, "variant.create", "product_variant", item.ID.String(), reason)
	response.Created(c, item)
}

func (h Handler) UpdateCatalogProductVariant(c *gin.Context) {
	variantID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, 422, 42300, "error.spec_id_invalid")
		return
	}
	reason, ok := requireAdminChangeReason(c, "更新商品规格")
	if !ok {
		return
	}
	var req productVariantCatalogRequest
	if decodeStrictJSON(c, &req) != nil || req.normalizeAndValidate() != nil {
		response.Error(c, 422, 42299, "error.spec_fields_invalid")
		return
	}
	productID, _ := uuid.Parse(req.ProductID)
	var item model.ProductVariant
	err = h.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&item, "id = ?", variantID).Error; err != nil {
			return err
		}
		if item.ProductID != productID {
			return errCatalogUnsafeChange
		}
		if item.Status == "active" && req.Status == "inactive" {
			var unfinished int64
			if err := tx.Model(&model.Order{}).
				Joins("JOIN order_items ON order_items.order_id = orders.id AND order_items.deleted_at IS NULL").
				Where("order_items.variant_id = ? AND orders.status IN ?", item.ID, []string{"pending_payment", "processing"}).
				Distinct("orders.id").Count(&unfinished).Error; err != nil {
				return err
			}
			if unfinished > 0 {
				return errProductHasUnfinishedOrders
			}
		}
		updates := map[string]any{"sku": req.SKU, "name": req.Name, "attributes": variantAttributes(*req.Attributes), "price": *req.Price, "compare_price": req.ComparePrice, "cost_price": req.CostPrice, "status": req.Status, "sort": req.Sort, "purchase_limit": req.PurchaseLimit}
		return tx.Model(&item).Updates(updates).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		response.Error(c, 404, 40477, "error.product_spec_not_found")
		return
	}
	if errors.Is(err, errCatalogUnsafeChange) {
		response.Error(c, 409, 40986, "error.spec_transfer_not_allowed")
		return
	}
	if errors.Is(err, errProductHasUnfinishedOrders) {
		response.Error(c, 409, 40987, "error.spec_pending_orders_cannot_disable")
		return
	}
	if err != nil {
		response.Error(c, 409, 40985, "error.spec_sku_exists_or_update_failed")
		return
	}
	h.audit(c, "variant.update", "product_variant", item.ID.String(), reason)
	h.DB.First(&item, "id = ?", item.ID)
	response.OK(c, item)
}

func (h Handler) DeleteCatalogProductVariant(c *gin.Context) {
	variantID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, 422, 42300, "error.spec_id_invalid")
		return
	}
	reason, ok := requireAdminChangeReason(c, "删除商品规格")
	if !ok {
		return
	}
	var item model.ProductVariant
	err = h.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&item, "id = ?", variantID).Error; err != nil {
			return err
		}
		if item.Status == "active" {
			return errCatalogUnsafeChange
		}
		var dependencies int64
		for _, target := range []any{&model.OrderItem{}, &model.Card{}, &model.InventoryBatch{}, &model.ProductPriceTier{}, &model.ProductMapping{}, &model.SupplierProduct{}, &model.ResellerProductRule{}, &model.CartItem{}} {
			var count int64
			if err := tx.Model(target).Where("variant_id = ?", item.ID).Count(&count).Error; err != nil {
				return err
			}
			dependencies += count
		}
		if dependencies > 0 {
			return errCatalogInUse
		}
		return tx.Delete(&item).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		response.Error(c, 404, 40477, "error.product_spec_not_found")
		return
	}
	if errors.Is(err, errCatalogUnsafeChange) {
		response.Error(c, 409, 40988, "error.active_spec_must_be_disabled_first")
		return
	}
	if errors.Is(err, errCatalogInUse) {
		response.Error(c, 409, 40989, "error.spec_has_orders_can_only_disable")
		return
	}
	if err != nil {
		response.Error(c, 500, 50080, "error.product_sku_delete_failed")
		return
	}
	h.audit(c, "variant.delete", "product_variant", item.ID.String(), reason)
	response.OK(c, gin.H{"deleted": true})
}

type productPriceTierCatalogRequest struct {
	ProductID     string     `json:"product_id"`
	VariantID     *string    `json:"variant_id"`
	MemberLevelID *string    `json:"member_level_id"`
	MinQuantity   int        `json:"min_quantity"`
	UnitPrice     *int64     `json:"unit_price"`
	StartsAt      *time.Time `json:"starts_at"`
	EndsAt        *time.Time `json:"ends_at"`
}

func (r *productPriceTierCatalogRequest) normalizeAndValidate() (uuid.UUID, *uuid.UUID, *uuid.UUID, error) {
	productID, err := uuid.Parse(strings.TrimSpace(r.ProductID))
	if err != nil || r.MinQuantity < 1 || r.MinQuantity > 1_000_000 || r.UnitPrice == nil || *r.UnitPrice < 1 {
		return uuid.Nil, nil, nil, errCatalogInvalidRequest
	}
	parseOptional := func(value *string) (*uuid.UUID, error) {
		if value == nil || strings.TrimSpace(*value) == "" {
			return nil, nil
		}
		parsed, parseErr := uuid.Parse(strings.TrimSpace(*value))
		if parseErr != nil {
			return nil, parseErr
		}
		return &parsed, nil
	}
	variantID, err := parseOptional(r.VariantID)
	if err != nil {
		return uuid.Nil, nil, nil, errCatalogInvalidRequest
	}
	memberLevelID, err := parseOptional(r.MemberLevelID)
	if err != nil {
		return uuid.Nil, nil, nil, errCatalogInvalidRequest
	}
	if r.StartsAt != nil {
		value := r.StartsAt.UTC()
		r.StartsAt = &value
	}
	if r.EndsAt != nil {
		value := r.EndsAt.UTC()
		r.EndsAt = &value
	}
	if r.StartsAt != nil && r.EndsAt != nil && !r.EndsAt.After(*r.StartsAt) {
		return uuid.Nil, nil, nil, errCatalogInvalidRequest
	}
	return productID, variantID, memberLevelID, nil
}

type adminProductPriceTierDTO struct {
	model.ProductPriceTier
	ProductName     string `json:"product_name"`
	Currency        string `json:"currency"`
	VariantName     string `json:"variant_name"`
	MemberLevelName string `json:"member_level_name"`
}

func (h Handler) AdminProductPriceTiers(c *gin.Context) {
	page, pageSize := pagination(c)
	query := h.DB.Model(&model.ProductPriceTier{})
	filters := []struct {
		name   string
		column string
	}{
		{"product_id", "product_id"},
		{"variant_id", "variant_id"},
		{"member_level_id", "member_level_id"},
	}
	for _, filter := range filters {
		if value := strings.TrimSpace(c.Query(filter.name)); value != "" {
			parsed, err := uuid.Parse(value)
			if err != nil {
				response.Error(c, 422, 42301, "error.tiered_price_filter_id_invalid")
				return
			}
			query = query.Where(filter.column+" = ?", parsed)
		}
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		response.Error(c, 500, 50081, "error.tier_price_list_fetch_failed")
		return
	}
	var tiers []model.ProductPriceTier
	if err := query.Order("created_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&tiers).Error; err != nil {
		response.Error(c, 500, 50081, "error.tier_price_list_fetch_failed")
		return
	}
	productIDs, variantIDs, levelIDs := make([]uuid.UUID, 0), make([]uuid.UUID, 0), make([]uuid.UUID, 0)
	for _, tier := range tiers {
		productIDs = append(productIDs, tier.ProductID)
		if tier.VariantID != nil {
			variantIDs = append(variantIDs, *tier.VariantID)
		}
		if tier.MemberLevelID != nil {
			levelIDs = append(levelIDs, *tier.MemberLevelID)
		}
	}
	var products []model.Product
	var variants []model.ProductVariant
	var levels []model.MemberLevel
	h.DB.Select("id", "name", "currency").Where("id IN ?", productIDs).Find(&products)
	h.DB.Select("id", "name").Where("id IN ?", variantIDs).Find(&variants)
	h.DB.Select("id", "name").Where("id IN ?", levelIDs).Find(&levels)
	productNames, productCurrencies, variantNames, levelNames := map[uuid.UUID]string{}, map[uuid.UUID]string{}, map[uuid.UUID]string{}, map[uuid.UUID]string{}
	for _, item := range products {
		productNames[item.ID] = item.Name
		productCurrencies[item.ID] = item.Currency
	}
	for _, item := range variants {
		variantNames[item.ID] = item.Name
	}
	for _, item := range levels {
		levelNames[item.ID] = item.Name
	}
	items := make([]adminProductPriceTierDTO, 0, len(tiers))
	for _, tier := range tiers {
		dto := adminProductPriceTierDTO{ProductPriceTier: tier, ProductName: productNames[tier.ProductID], Currency: productCurrencies[tier.ProductID]}
		if tier.VariantID != nil {
			dto.VariantName = variantNames[*tier.VariantID]
		}
		if tier.MemberLevelID != nil {
			dto.MemberLevelName = levelNames[*tier.MemberLevelID]
		}
		items = append(items, dto)
	}
	response.Page(c, items, total, page, pageSize)
}

func validatePriceTierRelations(tx *gorm.DB, productID uuid.UUID, variantID, memberLevelID *uuid.UUID, unitPrice int64) error {
	var product model.Product
	if err := tx.Select("id", "price").First(&product, "id = ?", productID).Error; err != nil {
		return errCatalogRelation
	}
	basePrice := product.Price
	if variantID != nil {
		var variant model.ProductVariant
		if err := tx.Select("id", "price").Where("id = ? AND product_id = ?", *variantID, productID).First(&variant).Error; err != nil {
			return errCatalogRelation
		}
		basePrice = variant.Price
	} else {
		var minimumVariantPrice int64
		if err := tx.Model(&model.ProductVariant{}).Where("product_id = ? AND status = ?", productID, "active").Select("COALESCE(MIN(price), 0)").Scan(&minimumVariantPrice).Error; err != nil {
			return err
		}
		if minimumVariantPrice > 0 && (basePrice == 0 || minimumVariantPrice < basePrice) {
			basePrice = minimumVariantPrice
		}
	}
	if basePrice <= 0 || unitPrice >= basePrice {
		return errCatalogInvalidRequest
	}
	if memberLevelID != nil {
		var level model.MemberLevel
		if err := tx.Select("id").First(&level, "id = ?", *memberLevelID).Error; err != nil {
			return errCatalogRelation
		}
	}
	return nil
}

func priceTierConflict(tx *gorm.DB, excludeID *uuid.UUID, productID uuid.UUID, variantID, memberLevelID *uuid.UUID, minQuantity int, startsAt, endsAt *time.Time) (bool, error) {
	query := tx.Model(&model.ProductPriceTier{}).Where("product_id = ? AND min_quantity = ?", productID, minQuantity)
	if variantID == nil {
		query = query.Where("variant_id IS NULL")
	} else {
		query = query.Where("variant_id = ?", *variantID)
	}
	if memberLevelID == nil {
		query = query.Where("member_level_id IS NULL")
	} else {
		query = query.Where("member_level_id = ?", *memberLevelID)
	}
	if excludeID != nil {
		query = query.Where("id <> ?", *excludeID)
	}
	if endsAt != nil {
		query = query.Where("starts_at IS NULL OR starts_at <= ?", *endsAt)
	}
	if startsAt != nil {
		query = query.Where("ends_at IS NULL OR ends_at >= ?", *startsAt)
	}
	var count int64
	err := query.Count(&count).Error
	return count > 0, err
}

func (h Handler) saveCatalogPriceTier(c *gin.Context, tierID *uuid.UUID) {
	action := "创建商品阶梯价"
	if tierID != nil {
		action = "更新商品阶梯价"
	}
	reason, ok := requireAdminChangeReason(c, action)
	if !ok {
		return
	}
	var req productPriceTierCatalogRequest
	if decodeStrictJSON(c, &req) != nil {
		response.Error(c, 422, 42302, "error.tiered_price_request_format_invalid")
		return
	}
	productID, variantID, memberLevelID, err := req.normalizeAndValidate()
	if err != nil {
		response.Error(c, 422, 42302, "error.tiered_price_fields_invalid")
		return
	}
	var item model.ProductPriceTier
	err = h.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("LOCK TABLE product_price_tiers IN SHARE ROW EXCLUSIVE MODE").Error; err != nil {
			return err
		}
		if tierID != nil {
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&item, "id = ?", *tierID).Error; err != nil {
				return err
			}
		}
		if err := validatePriceTierRelations(tx, productID, variantID, memberLevelID, *req.UnitPrice); err != nil {
			return err
		}
		conflict, err := priceTierConflict(tx, tierID, productID, variantID, memberLevelID, req.MinQuantity, req.StartsAt, req.EndsAt)
		if err != nil {
			return err
		}
		if conflict {
			return errCatalogConflict
		}
		values := map[string]any{"product_id": productID, "variant_id": variantID, "member_level_id": memberLevelID, "min_quantity": req.MinQuantity, "unit_price": *req.UnitPrice, "starts_at": req.StartsAt, "ends_at": req.EndsAt}
		if tierID == nil {
			item = model.ProductPriceTier{ProductID: productID, VariantID: variantID, MemberLevelID: memberLevelID, MinQuantity: req.MinQuantity, UnitPrice: *req.UnitPrice, StartsAt: req.StartsAt, EndsAt: req.EndsAt}
			return tx.Create(&item).Error
		}
		return tx.Model(&item).Updates(values).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		response.Error(c, 404, 40478, "error.product_tier_price_not_found")
		return
	}
	if errors.Is(err, errCatalogRelation) {
		response.Error(c, 422, 42303, "error.tiered_price_relation_not_found")
		return
	}
	if errors.Is(err, errCatalogInvalidRequest) {
		response.Error(c, 422, 42304, "error.tiered_price_range_invalid")
		return
	}
	if errors.Is(err, errCatalogConflict) {
		response.Error(c, 409, 40990, "error.tiered_price_validity_overlap")
		return
	}
	if err != nil {
		response.Error(c, 500, 50082, "error.product_tier_price_save_failed")
		return
	}
	auditAction := "price-tier.create"
	status := 201
	if tierID != nil {
		auditAction = "price-tier.update"
		status = 200
	}
	h.audit(c, auditAction, "product_price_tier", item.ID.String(), reason)
	if status == 201 {
		response.Created(c, item)
	} else {
		h.DB.First(&item, "id = ?", item.ID)
		response.OK(c, item)
	}
}

func (h Handler) CreateCatalogPriceTier(c *gin.Context) {
	h.saveCatalogPriceTier(c, nil)
}

func (h Handler) UpdateCatalogPriceTier(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, 422, 42305, "error.tiered_price_id_invalid")
		return
	}
	h.saveCatalogPriceTier(c, &id)
}

func (h Handler) DeleteCatalogPriceTier(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, 422, 42305, "error.tiered_price_id_invalid")
		return
	}
	reason, ok := requireAdminChangeReason(c, "删除商品阶梯价")
	if !ok {
		return
	}
	var item model.ProductPriceTier
	if err := h.DB.First(&item, "id = ?", id).Error; err != nil {
		response.Error(c, 404, 40478, "error.product_tier_price_not_found")
		return
	}
	if err := h.DB.Delete(&item).Error; err != nil {
		response.Error(c, 500, 50083, "error.product_tier_price_delete_failed")
		return
	}
	h.audit(c, "price-tier.delete", "product_price_tier", item.ID.String(), reason)
	response.OK(c, gin.H{"deleted": true})
}

type memberLevelCatalogRequest struct {
	Code               string `json:"code"`
	Name               string `json:"name"`
	Currency           string `json:"currency"`
	MinimumSpend       *int64 `json:"minimum_spend"`
	DiscountBasisPoint int    `json:"discount_basis_point"`
	Priority           int    `json:"priority"`
	Enabled            bool   `json:"enabled"`
}

func (r *memberLevelCatalogRequest) normalizeAndValidate() error {
	r.Code = strings.ToLower(strings.TrimSpace(r.Code))
	r.Name = strings.TrimSpace(r.Name)
	r.Currency = strings.ToUpper(strings.TrimSpace(r.Currency))
	if !catalogCodePattern.MatchString(r.Code) || utf8.RuneCountInString(r.Name) < 1 || utf8.RuneCountInString(r.Name) > 100 || (r.Currency != "" && len(r.Currency) != 3) || r.MinimumSpend == nil || *r.MinimumSpend < 0 || *r.MinimumSpend > 1_000_000_000_000 || r.DiscountBasisPoint < 0 || r.DiscountBasisPoint > 10000 || r.Priority < 0 || r.Priority > 1_000_000 {
		return errCatalogInvalidRequest
	}
	return nil
}

type adminMemberLevelDTO struct {
	model.MemberLevel
	MembershipCount int64 `json:"membership_count"`
	PriceTierCount  int64 `json:"price_tier_count"`
}

func (h Handler) AdminCatalogMemberLevels(c *gin.Context) {
	page, pageSize := pagination(c)
	query := h.DB.Model(&model.MemberLevel{})
	if keyword := strings.TrimSpace(c.Query("q")); keyword != "" {
		like := "%" + keyword + "%"
		query = query.Where("code ILIKE ? OR name ILIKE ?", like, like)
	}
	if enabled := strings.TrimSpace(c.Query("enabled")); enabled != "" {
		if enabled != "true" && enabled != "false" {
			response.Error(c, 422, 42306, "error.member_tier_status_filter_invalid")
			return
		}
		query = query.Where("enabled = ?", enabled == "true")
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		response.Error(c, 500, 50084, "error.member_level_list_fetch_failed")
		return
	}
	var levels []model.MemberLevel
	if err := query.Order("priority DESC, created_at ASC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&levels).Error; err != nil {
		response.Error(c, 500, 50084, "error.member_level_list_fetch_failed")
		return
	}
	items := make([]adminMemberLevelDTO, 0, len(levels))
	for _, level := range levels {
		var memberships, tiers int64
		h.DB.Model(&model.UserLevelMembership{}).Where("member_level_id = ?", level.ID).Count(&memberships)
		h.DB.Model(&model.ProductPriceTier{}).Where("member_level_id = ?", level.ID).Count(&tiers)
		items = append(items, adminMemberLevelDTO{MemberLevel: level, MembershipCount: memberships, PriceTierCount: tiers})
	}
	response.Page(c, items, total, page, pageSize)
}

func (h Handler) saveCatalogMemberLevel(c *gin.Context, levelID *uuid.UUID) {
	action := "创建会员等级"
	if levelID != nil {
		action = "更新会员等级"
	}
	reason, ok := requireAdminChangeReason(c, action)
	if !ok {
		return
	}
	var req memberLevelCatalogRequest
	if decodeStrictJSON(c, &req) != nil || req.normalizeAndValidate() != nil {
		response.Error(c, 422, 42307, "error.member_tier_fields_invalid")
		return
	}
	if req.Currency == "" {
		var err error
		req.Currency, err = service.StoreCurrency(h.DB)
		if err != nil {
			response.Error(c, 500, 50084, "error.store_currency_fetch_failed")
			return
		}
	}
	var currencyDefinition model.CurrencyDefinition
	if h.DB.Where("code = ? AND enabled = ?", req.Currency, true).First(&currencyDefinition).Error != nil {
		response.Error(c, 422, 42307, "error.currency_not_supported")
		return
	}
	var item model.MemberLevel
	err := h.DB.Transaction(func(tx *gorm.DB) error {
		if levelID != nil {
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&item, "id = ?", *levelID).Error; err != nil {
				return err
			}
		}
		if levelID == nil {
			item = model.MemberLevel{Code: req.Code, Name: req.Name, Currency: req.Currency, MinimumSpend: *req.MinimumSpend, DiscountBasisPoint: req.DiscountBasisPoint, Priority: req.Priority, Enabled: req.Enabled}
			return createWithExplicitColumns(tx, &item, map[string]any{"enabled": req.Enabled})
		}
		return tx.Model(&item).Updates(map[string]any{"code": req.Code, "name": req.Name, "currency": req.Currency, "minimum_spend": *req.MinimumSpend, "discount_basis_point": req.DiscountBasisPoint, "priority": req.Priority, "enabled": req.Enabled}).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		response.Error(c, 404, 40479, "error.member_level_not_found")
		return
	}
	if err != nil {
		response.Error(c, 409, 40991, "error.member_tier_code_exists_or_save_failed")
		return
	}
	item.Enabled = req.Enabled
	auditAction := "member-level.create"
	if levelID != nil {
		auditAction = "member-level.update"
	}
	h.audit(c, auditAction, "member_level", item.ID.String(), reason)
	if levelID == nil {
		response.Created(c, item)
	} else {
		h.DB.First(&item, "id = ?", item.ID)
		response.OK(c, item)
	}
}

func (h Handler) CreateCatalogMemberLevel(c *gin.Context) {
	h.saveCatalogMemberLevel(c, nil)
}

func (h Handler) UpdateCatalogMemberLevel(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, 422, 42308, "error.member_tier_id_invalid")
		return
	}
	h.saveCatalogMemberLevel(c, &id)
}

func (h Handler) DeleteCatalogMemberLevel(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, 422, 42308, "error.member_tier_id_invalid")
		return
	}
	reason, ok := requireAdminChangeReason(c, "删除会员等级")
	if !ok {
		return
	}
	var item model.MemberLevel
	err = h.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&item, "id = ?", id).Error; err != nil {
			return err
		}
		var memberships, tiers int64
		if err := tx.Model(&model.UserLevelMembership{}).Where("member_level_id = ?", item.ID).Count(&memberships).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.ProductPriceTier{}).Where("member_level_id = ?", item.ID).Count(&tiers).Error; err != nil {
			return err
		}
		if memberships+tiers > 0 {
			return errCatalogInUse
		}
		return tx.Delete(&item).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		response.Error(c, 404, 40479, "error.member_level_not_found")
		return
	}
	if errors.Is(err, errCatalogInUse) {
		response.Error(c, 409, 40992, "error.member_tier_in_use_can_only_disable")
		return
	}
	if err != nil {
		response.Error(c, 500, 50085, "error.member_level_delete_failed")
		return
	}
	h.audit(c, "member-level.delete", "member_level", item.ID.String(), reason)
	response.OK(c, gin.H{"deleted": true})
}
