package handler

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"linlinqi/api/internal/model"
	"linlinqi/api/internal/security"
	"linlinqi/api/internal/service"
	"linlinqi/api/pkg/response"
)

var (
	errInventoryCardState = errors.New("inventory card state cannot be changed")
	errInventoryProduct   = errors.New("inventory product is unavailable")
)

type adminInventoryVariantDTO struct {
	ID            uuid.UUID `json:"id"`
	SKU           string    `json:"sku"`
	Name          string    `json:"name"`
	Status        string    `json:"status"`
	Available     int64     `json:"available"`
	Locked        int64     `json:"locked"`
	Sold          int64     `json:"sold"`
	Disabled      int64     `json:"disabled"`
	Total         int64     `json:"total"`
	PurchaseLimit int       `json:"purchase_limit"`
}

type adminInventoryProductDTO struct {
	ID            uuid.UUID                  `json:"id"`
	Name          string                     `json:"name"`
	Slug          string                     `json:"slug"`
	Status        string                     `json:"status"`
	InventoryMode string                     `json:"inventory_mode"`
	DeliveryType  string                     `json:"delivery_type"`
	Available     int64                      `json:"available"`
	Locked        int64                      `json:"locked"`
	Sold          int64                      `json:"sold"`
	Disabled      int64                      `json:"disabled"`
	Total         int64                      `json:"total"`
	Variants      []adminInventoryVariantDTO `json:"variants"`
}

type inventoryCountRow struct {
	ProductID uuid.UUID
	VariantID *uuid.UUID
	Status    string
	Count     int64
}

type inventoryCounts struct {
	Available int64
	Locked    int64
	Sold      int64
	Disabled  int64
	Total     int64
}

func (counts *inventoryCounts) add(status string, count int64) {
	counts.Total += count
	switch status {
	case "available":
		counts.Available += count
	case "locked":
		counts.Locked += count
	case "sold":
		counts.Sold += count
	case "disabled":
		counts.Disabled += count
	}
}

func inventoryCountKey(productID uuid.UUID, variantID *uuid.UUID) string {
	if variantID == nil {
		return productID.String() + "/base"
	}
	return productID.String() + "/" + variantID.String()
}

func (h Handler) AdminInventoryProducts(c *gin.Context) {
	page, pageSize := pagination(c)
	query := h.DB.Model(&model.Product{})
	if keyword := strings.TrimSpace(c.Query("q")); keyword != "" {
		like := "%" + keyword + "%"
		query = query.Where("name ILIKE ? OR slug ILIKE ?", like, like)
	}
	if status := strings.TrimSpace(c.Query("status")); status != "" {
		if status != "draft" && status != "on_sale" && status != "off_sale" {
			response.Error(c, 422, 42320, "error.product_status_filter_invalid")
			return
		}
		query = query.Where("status = ?", status)
	}
	if inventoryMode := strings.TrimSpace(c.Query("inventory_mode")); inventoryMode != "" {
		if inventoryMode != "local" && inventoryMode != "supplier" {
			response.Error(c, 422, 42321, "error.inventory_mode_filter_invalid")
			return
		}
		query = query.Where("inventory_mode = ?", inventoryMode)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		response.Error(c, 500, 50090, "error.inventory_product_list_fetch_failed")
		return
	}
	var products []model.Product
	if err := query.Select("id", "name", "slug", "status", "inventory_mode", "delivery_type").Order("sort DESC, created_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&products).Error; err != nil {
		response.Error(c, 500, 50090, "error.inventory_product_list_fetch_failed")
		return
	}
	productIDs := make([]uuid.UUID, 0, len(products))
	for _, product := range products {
		productIDs = append(productIDs, product.ID)
	}
	var variants []model.ProductVariant
	var countRows []inventoryCountRow
	if len(productIDs) > 0 {
		if err := h.DB.Select("id", "product_id", "sku", "name", "status", "purchase_limit").Where("product_id IN ?", productIDs).Order("sort DESC, created_at ASC").Find(&variants).Error; err != nil {
			response.Error(c, 500, 50090, "error.inventory_product_sku_list_fetch_failed")
			return
		}
		if err := h.DB.Model(&model.Card{}).Select("product_id, variant_id, status, COUNT(*) AS count").Where("product_id IN ?", productIDs).Group("product_id, variant_id, status").Scan(&countRows).Error; err != nil {
			response.Error(c, 500, 50090, "error.inventory_summary_failed")
			return
		}
	}
	productCounts := map[uuid.UUID]inventoryCounts{}
	variantCounts := map[string]inventoryCounts{}
	for _, row := range countRows {
		counts := productCounts[row.ProductID]
		counts.add(row.Status, row.Count)
		productCounts[row.ProductID] = counts
		variantCount := variantCounts[inventoryCountKey(row.ProductID, row.VariantID)]
		variantCount.add(row.Status, row.Count)
		variantCounts[inventoryCountKey(row.ProductID, row.VariantID)] = variantCount
	}
	variantsByProduct := make(map[uuid.UUID][]adminInventoryVariantDTO)
	for _, variant := range variants {
		counts := variantCounts[inventoryCountKey(variant.ProductID, &variant.ID)]
		variantsByProduct[variant.ProductID] = append(variantsByProduct[variant.ProductID], adminInventoryVariantDTO{
			ID: variant.ID, SKU: variant.SKU, Name: variant.Name, Status: variant.Status,
			Available: counts.Available, Locked: counts.Locked, Sold: counts.Sold, Disabled: counts.Disabled, Total: counts.Total, PurchaseLimit: variant.PurchaseLimit,
		})
	}
	items := make([]adminInventoryProductDTO, 0, len(products))
	for _, product := range products {
		counts := productCounts[product.ID]
		productVariants := variantsByProduct[product.ID]
		if productVariants == nil {
			productVariants = []adminInventoryVariantDTO{}
		}
		items = append(items, adminInventoryProductDTO{
			ID: product.ID, Name: product.Name, Slug: product.Slug, Status: product.Status, InventoryMode: product.InventoryMode, DeliveryType: product.DeliveryType,
			Available: counts.Available, Locked: counts.Locked, Sold: counts.Sold, Disabled: counts.Disabled, Total: counts.Total, Variants: productVariants,
		})
	}
	response.Page(c, items, total, page, pageSize)
}

type adminInventoryCardDTO struct {
	ID          uuid.UUID  `json:"id"`
	ProductID   uuid.UUID  `json:"product_id"`
	VariantID   *uuid.UUID `json:"variant_id,omitempty"`
	ProductName string     `json:"product_name"`
	VariantName string     `json:"variant_name"`
	VariantSKU  string     `json:"variant_sku"`
	Preview     string     `json:"preview"`
	Status      string     `json:"status"`
	OrderID     *uuid.UUID `json:"order_id,omitempty"`
	SoldAt      *time.Time `json:"sold_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

func (h Handler) AdminInventoryCards(c *gin.Context) {
	page, pageSize := pagination(c)
	query := h.DB.Table("cards c").Where("c.deleted_at IS NULL")
	if productID := strings.TrimSpace(c.Query("product_id")); productID != "" {
		parsed, err := uuid.Parse(productID)
		if err != nil {
			response.Error(c, 422, 42322, "error.product_filter_id_invalid")
			return
		}
		query = query.Where("c.product_id = ?", parsed)
	}
	if variantID := strings.TrimSpace(c.Query("variant_id")); variantID != "" {
		parsed, err := uuid.Parse(variantID)
		if err != nil {
			response.Error(c, 422, 42323, "error.spec_filter_id_invalid")
			return
		}
		query = query.Where("c.variant_id = ?", parsed)
	}
	if status := strings.TrimSpace(c.Query("status")); status != "" {
		if status != "available" && status != "locked" && status != "sold" && status != "disabled" {
			response.Error(c, 422, 42324, "error.cardkey_status_filter_invalid")
			return
		}
		query = query.Where("c.status = ?", status)
	}
	query = query.Joins("JOIN products p ON p.id = c.product_id AND p.deleted_at IS NULL").
		Joins("LEFT JOIN product_variants pv ON pv.id = c.variant_id AND pv.deleted_at IS NULL")
	if keyword := strings.TrimSpace(c.Query("q")); keyword != "" {
		like := "%" + keyword + "%"
		query = query.Where("c.preview ILIKE ? OR p.name ILIKE ? OR pv.sku ILIKE ? OR pv.name ILIKE ?", like, like, like, like)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		response.Error(c, 500, 50091, "error.card_secret_list_fetch_failed")
		return
	}
	var items []adminInventoryCardDTO
	if err := query.Select("c.id, c.product_id, c.variant_id, p.name AS product_name, COALESCE(pv.name, '') AS variant_name, COALESCE(pv.sku, '') AS variant_sku, c.preview, c.status, c.order_id, c.sold_at, c.created_at, c.updated_at").
		Order("c.created_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Scan(&items).Error; err != nil {
		response.Error(c, 500, 50091, "error.card_secret_list_fetch_failed")
		return
	}
	response.Page(c, items, total, page, pageSize)
}

type adminInventoryBatchDTO struct {
	ID           uuid.UUID  `json:"id"`
	ProductID    uuid.UUID  `json:"product_id"`
	VariantID    *uuid.UUID `json:"variant_id,omitempty"`
	BatchNo      string     `json:"batch_no"`
	Source       string     `json:"source"`
	TotalCount   int        `json:"total_count"`
	ValidCount   int        `json:"valid_count"`
	InvalidCount int        `json:"invalid_count"`
	ImportedBy   *uuid.UUID `json:"imported_by,omitempty"`
	ImporterName string     `json:"importer_name"`
	ProductName  string     `json:"product_name"`
	VariantName  string     `json:"variant_name"`
	VariantSKU   string     `json:"variant_sku"`
	ExpiresAt    *time.Time `json:"expires_at,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

func (h Handler) AdminInventoryBatches(c *gin.Context) {
	page, pageSize := pagination(c)
	query := h.DB.Table("inventory_batches ib").Where("ib.deleted_at IS NULL").
		Joins("JOIN products p ON p.id = ib.product_id AND p.deleted_at IS NULL").
		Joins("LEFT JOIN product_variants pv ON pv.id = ib.variant_id AND pv.deleted_at IS NULL").
		Joins("LEFT JOIN admins a ON a.id = ib.imported_by AND a.deleted_at IS NULL")
	if productID := strings.TrimSpace(c.Query("product_id")); productID != "" {
		parsed, err := uuid.Parse(productID)
		if err != nil {
			response.Error(c, 422, 42322, "error.product_filter_id_invalid")
			return
		}
		query = query.Where("ib.product_id = ?", parsed)
	}
	if variantID := strings.TrimSpace(c.Query("variant_id")); variantID != "" {
		parsed, err := uuid.Parse(variantID)
		if err != nil {
			response.Error(c, 422, 42323, "error.spec_filter_id_invalid")
			return
		}
		query = query.Where("ib.variant_id = ?", parsed)
	}
	if invalid := strings.TrimSpace(c.Query("has_invalid")); invalid != "" {
		if invalid != "true" && invalid != "false" {
			response.Error(c, 422, 42325, "error.batch_anomaly_filter_invalid")
			return
		}
		if invalid == "true" {
			query = query.Where("ib.invalid_count > 0")
		} else {
			query = query.Where("ib.invalid_count = 0")
		}
	}
	if keyword := strings.TrimSpace(c.Query("q")); keyword != "" {
		like := "%" + keyword + "%"
		query = query.Where("ib.batch_no ILIKE ? OR p.name ILIKE ? OR pv.sku ILIKE ? OR pv.name ILIKE ?", like, like, like, like)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		response.Error(c, 500, 50092, "error.inventory_batch_list_fetch_failed")
		return
	}
	var items []adminInventoryBatchDTO
	if err := query.Select("ib.id, ib.product_id, ib.variant_id, ib.batch_no, ib.source, ib.total_count, ib.valid_count, ib.invalid_count, ib.imported_by, COALESCE(a.name, a.username, '') AS importer_name, p.name AS product_name, COALESCE(pv.name, '') AS variant_name, COALESCE(pv.sku, '') AS variant_sku, ib.expires_at, ib.created_at, ib.updated_at").
		Order("ib.created_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Scan(&items).Error; err != nil {
		response.Error(c, 500, 50092, "error.inventory_batch_list_fetch_failed")
		return
	}
	response.Page(c, items, total, page, pageSize)
}

type inventoryCardsImportRequest struct {
	ProductID string   `json:"product_id"`
	VariantID *string  `json:"variant_id"`
	Cards     []string `json:"cards"`
}

func (r *inventoryCardsImportRequest) normalizeAndValidate() (uuid.UUID, *uuid.UUID, error) {
	productID, err := uuid.Parse(strings.TrimSpace(r.ProductID))
	if err != nil || len(r.Cards) < 1 || len(r.Cards) > 5000 {
		return uuid.Nil, nil, errInventoryProduct
	}
	if r.VariantID == nil || strings.TrimSpace(*r.VariantID) == "" {
		return productID, nil, nil
	}
	variantID, err := uuid.Parse(strings.TrimSpace(*r.VariantID))
	if err != nil {
		return uuid.Nil, nil, errInventoryProduct
	}
	return productID, &variantID, nil
}

func buildEncryptedInventoryCards(vault *security.Vault, productID uuid.UUID, variantID *uuid.UUID, rawCards []string) ([]model.Card, int, error) {
	items := make([]model.Card, 0, len(rawCards))
	seen := make(map[string]struct{}, len(rawCards))
	invalid := 0
	for _, raw := range rawCards {
		content := strings.TrimSpace(raw)
		if content == "" || utf8.RuneCountInString(content) > 2000 {
			invalid++
			continue
		}
		ciphertext, nonce, fingerprint, err := vault.Encrypt(content, productID[:])
		if err != nil {
			return nil, 0, err
		}
		if _, duplicate := seen[fingerprint]; duplicate {
			invalid++
			continue
		}
		seen[fingerprint] = struct{}{}
		items = append(items, model.Card{
			ProductID: productID, VariantID: variantID, EncryptedContent: ciphertext, Nonce: nonce,
			Fingerprint: fingerprint, Preview: security.SecretPreview(content), Status: "available",
		})
	}
	return items, invalid, nil
}

func inventoryBatchNo(now time.Time) string {
	random := strings.ToUpper(strings.ReplaceAll(uuid.NewString(), "-", "")[:8])
	return "LQIB" + now.UTC().Format("20060102150405") + random
}

func (h Handler) ImportInventoryCards(c *gin.Context) {
	reason, ok := requireAdminChangeReason(c, "导入卡密")
	if !ok {
		return
	}
	var req inventoryCardsImportRequest
	if decodeStrictJSON(c, &req) != nil {
		response.Error(c, 422, 42326, "error.cardkey_import_format_invalid")
		return
	}
	productID, variantID, err := req.normalizeAndValidate()
	if err != nil {
		response.Error(c, 422, 42327, "error.cardkey_quantity_invalid")
		return
	}
	items, preInvalid, err := buildEncryptedInventoryCards(h.Vault, productID, variantID, req.Cards)
	if err != nil {
		response.Error(c, 500, 50093, "error.card_encrypt_failed")
		return
	}
	adminID, _ := uuid.Parse(c.GetString("subject"))
	batch := model.InventoryBatch{
		ProductID: productID, VariantID: variantID, BatchNo: inventoryBatchNo(time.Now()), Source: "manual_import",
		TotalCount: len(req.Cards), ImportedBy: &adminID,
	}
	err = h.DB.Transaction(func(tx *gorm.DB) error {
		var product model.Product
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id", "inventory_mode").First(&product, "id = ?", productID).Error; err != nil {
			return err
		}
		if product.InventoryMode != "local" {
			return errProductNotLocalInventory
		}
		var activeVariants int64
		if err := tx.Model(&model.ProductVariant{}).Where("product_id = ? AND status = ?", productID, "active").Count(&activeVariants).Error; err != nil {
			return err
		}
		if activeVariants > 0 && variantID == nil {
			return service.ErrVariantRequired
		}
		if variantID != nil {
			var variant model.ProductVariant
			if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("id = ? AND product_id = ? AND status = ?", *variantID, productID, "active").First(&variant).Error; err != nil {
				return service.ErrVariantUnavailable
			}
		}
		inserted := int64(0)
		if len(items) > 0 {
			result := tx.Clauses(clause.OnConflict{DoNothing: true}).CreateInBatches(&items, 500)
			if result.Error != nil {
				return result.Error
			}
			inserted = result.RowsAffected
		}
		batch.ValidCount = int(inserted)
		batch.InvalidCount = preInvalid + len(items) - int(inserted)
		return tx.Create(&batch).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		response.Error(c, 404, 40490, "error.product_not_found")
		return
	}
	if errors.Is(err, errProductNotLocalInventory) {
		response.Error(c, 409, 40994, "error.supplier_inventory_cannot_import_local_keys")
		return
	}
	if errors.Is(err, service.ErrVariantRequired) || errors.Is(err, service.ErrVariantUnavailable) {
		response.Error(c, 409, 40995, "error.spec_changed_reselect_enabled")
		return
	}
	if err != nil {
		response.Error(c, 500, 50094, "error.card_secret_import_txn_failed")
		return
	}
	h.audit(c, "cards.import", "inventory_batch", batch.ID.String(), fmt.Sprintf("%s；total=%d；valid=%d；invalid=%d", reason, batch.TotalCount, batch.ValidCount, batch.InvalidCount))
	var product model.Product
	if h.DB.Select("id", "name").First(&product, "id = ?", productID).Error == nil {
		_ = h.createOperationalNotifications(h.DB, "inventory.restocked", batch.ID.String(), map[string]string{"entity_id": productID.String(), "product_name": product.Name, "stock": strconv.Itoa(batch.ValidCount), "status": "restocked", "summary": "后台完成卡密库存补货"})
	}
	response.Created(c, gin.H{"batch": batch, "imported": batch.ValidCount, "invalid": batch.InvalidCount})
}

type inventoryCardStatusRequest struct {
	Status string `json:"status"`
}

func validInventoryCardTransition(current, target string) bool {
	return (current == "available" && target == "disabled") || (current == "disabled" && target == "available")
}

func (h Handler) UpdateInventoryCardStatus(c *gin.Context) {
	cardID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, 422, 42328, "error.cardkey_id_invalid")
		return
	}
	reason, ok := requireAdminChangeReason(c, "变更卡密状态")
	if !ok {
		return
	}
	var req inventoryCardStatusRequest
	if decodeStrictJSON(c, &req) != nil {
		response.Error(c, 422, 42329, "error.cardkey_status_format_invalid")
		return
	}
	req.Status = strings.ToLower(strings.TrimSpace(req.Status))
	if req.Status != "available" && req.Status != "disabled" {
		response.Error(c, 422, 42330, "error.cardkey_status_active_disabled")
		return
	}
	var item model.Card
	err = h.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&item, "id = ?", cardID).Error; err != nil {
			return err
		}
		if !validInventoryCardTransition(item.Status, req.Status) || item.OrderID != nil {
			return errInventoryCardState
		}
		if req.Status == "available" {
			var product model.Product
			if err := tx.Select("id", "inventory_mode").Where("id = ? AND inventory_mode = ?", item.ProductID, "local").First(&product).Error; err != nil {
				return errInventoryProduct
			}
			if item.VariantID != nil {
				var variant model.ProductVariant
				if err := tx.Select("id").Where("id = ? AND product_id = ? AND status = ?", *item.VariantID, item.ProductID, "active").First(&variant).Error; err != nil {
					return errInventoryProduct
				}
			}
		}
		return tx.Model(&item).Update("status", req.Status).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		response.Error(c, 404, 40491, "error.card_secret_not_found")
		return
	}
	if errors.Is(err, errInventoryCardState) {
		response.Error(c, 409, 40996, "error.locked_or_sold_key_manual_change_not_allowed")
		return
	}
	if errors.Is(err, errInventoryProduct) {
		response.Error(c, 409, 40997, "error.key_reenable_not_allowed")
		return
	}
	if err != nil {
		response.Error(c, 500, 50095, "error.card_secret_status_change_failed")
		return
	}
	h.audit(c, "card.status", "card", item.ID.String(), reason+"；status="+req.Status)
	response.OK(c, gin.H{"id": item.ID, "status": req.Status})
}
