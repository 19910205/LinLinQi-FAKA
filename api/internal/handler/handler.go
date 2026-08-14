package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"linlinqi/api/internal/config"
	"linlinqi/api/internal/content"
	fx "linlinqi/api/internal/currency"
	"linlinqi/api/internal/middleware"
	"linlinqi/api/internal/model"
	"linlinqi/api/internal/queue"
	"linlinqi/api/internal/security"
	"linlinqi/api/internal/service"
	"linlinqi/api/pkg/response"
)

type Handler struct {
	DB    *gorm.DB
	Cfg   config.Config
	Vault *security.Vault
	Redis *redis.Client
}

// createWithExplicitColumns keeps caller-supplied zero values authoritative
// for columns whose GORM model declares a non-zero default. GORM replaces a
// false bool with default:true during Create, so the explicit columns must be
// written back in the same transaction before the record is observable.
func createWithExplicitColumns(tx *gorm.DB, value any, columns map[string]any) error {
	if err := tx.Create(value).Error; err != nil {
		return err
	}
	if len(columns) == 0 {
		return nil
	}
	return tx.Model(value).UpdateColumns(columns).Error
}

func (h Handler) Health(c *gin.Context) {
	sqlDB, err := h.DB.DB()
	if err != nil || sqlDB.PingContext(c) != nil {
		response.Error(c, http.StatusServiceUnavailable, 50301, "error.database_unavailable")
		return
	}
	if err := h.Redis.Ping(c).Err(); err != nil {
		response.Error(c, http.StatusServiceUnavailable, 50302, "error.redis_unavailable")
		return
	}
	if err := h.StorageReady(); err != nil {
		response.Error(c, http.StatusServiceUnavailable, 50303, "error.media_storage_unavailable")
		return
	}
	response.OK(c, gin.H{"status": "ready", "database": "ok", "redis": "ok", "storage": "ok", "time": time.Now().UTC()})
}

func (h Handler) Live(c *gin.Context) {
	response.OK(c, gin.H{"status": "alive", "time": time.Now().UTC()})
}

func (h Handler) StoreConfig(c *gin.Context) {
	storefront, err := h.resolveStorefront(c)
	if err != nil {
		response.Error(c, 500, 50005, "error.shop_config_fetch_failed")
		return
	}
	var announcements []model.Announcement
	if err := h.DB.Where("enabled = ?", true).Order("sort desc").Find(&announcements).Error; err != nil {
		response.Error(c, 500, 50005, "error.shop_config_fetch_failed")
		return
	}
	publicAnnouncements := make([]publicContentAnnouncementDTO, 0, len(announcements))
	for _, announcement := range announcements {
		publicAnnouncements = append(publicAnnouncements, toPublicContentAnnouncementDTO(announcement))
	}
	settings := map[string]string{}
	var stored []model.Setting
	if err := h.DB.Where("key IN ?", []string{"store_name", "store_tagline", "store_currency", "store_logo_url", "store_support_email", "store_seo_title", "store_seo_description"}).Find(&stored).Error; err != nil {
		response.Error(c, 500, 50005, "error.shop_config_fetch_failed")
		return
	}
	for _, setting := range stored {
		settings[setting.Key] = setting.Value
	}
	result := gin.H{
		"name": defaultString(settings["store_name"], "LinLinQi"), "tagline": defaultString(settings["store_tagline"], "可信赖的数字商品自动交付平台"), "currency": defaultString(settings["store_currency"], "CNY"),
		"logo_url": settings["store_logo_url"], "support_email": defaultString(settings["store_support_email"], h.Cfg.SupportEmail),
		"seo": gin.H{"title": settings["store_seo_title"], "description": settings["store_seo_description"]}, "announcements": publicAnnouncements,
		"reseller": false,
	}
	if storefront != nil {
		theme, seo, support := decodeJSONMap(storefront.Site.Theme), decodeJSONMap(storefront.Site.SEO), decodeJSONMap(storefront.Site.Support)
		result["name"] = defaultString(storefront.Site.SiteName, storefront.Profile.Name)
		result["logo_url"] = storefront.Site.LogoURL
		result["theme"] = theme
		result["seo"] = seo
		result["support"] = support
		result["reseller"] = true
		result["reseller_code"] = storefront.Profile.Code
		if supportEmail, ok := support["email"].(string); ok && strings.TrimSpace(supportEmail) != "" {
			result["support_email"] = supportEmail
		}
	}
	response.OK(c, result)
}

func (h Handler) Categories(c *gin.Context) {
	storefront, err := h.resolveStorefront(c)
	if err != nil {
		response.Error(c, 500, 50001, "error.category_fetch_failed")
		return
	}
	var categories []model.Category
	if err := h.DB.Where("enabled = ?", true).Order("sort desc, created_at asc").Find(&categories).Error; err != nil {
		response.Error(c, 500, 50001, "error.category_fetch_failed")
		return
	}
	rules := map[uuid.UUID]model.ResellerCategoryRule{}
	if storefront != nil {
		var stored []model.ResellerCategoryRule
		if err := h.DB.Where("reseller_id = ?", storefront.Profile.ID).Find(&stored).Error; err != nil {
			response.Error(c, 500, 50001, "error.category_fetch_failed")
			return
		}
		for _, rule := range stored {
			rules[rule.CategoryID] = rule
		}
	}
	visible := make(map[uuid.UUID]bool, len(categories))
	for _, category := range categories {
		rule, configured := rules[category.ID]
		visible[category.ID] = !configured || rule.Enabled
	}
	for changed := true; changed; {
		changed = false
		for _, category := range categories {
			if visible[category.ID] && category.ParentID != nil && !visible[*category.ParentID] {
				visible[category.ID] = false
				changed = true
			}
		}
	}
	type orderedCategory struct {
		DTO  publicCategoryDTO
		Sort int
	}
	ordered := make([]orderedCategory, 0, len(categories))
	for _, category := range categories {
		if !visible[category.ID] {
			continue
		}
		dto := toPublicCategoryDTO(category)
		sortValue := category.Sort
		if rule, configured := rules[category.ID]; configured {
			if strings.TrimSpace(rule.Name) != "" {
				dto.Name = rule.Name
			}
			if strings.TrimSpace(rule.ImageURL) != "" {
				dto.ImageURL = rule.ImageURL
			}
			sortValue = rule.Sort
		}
		ordered = append(ordered, orderedCategory{DTO: dto, Sort: sortValue})
	}
	sort.SliceStable(ordered, func(left, right int) bool { return ordered[left].Sort > ordered[right].Sort })
	items := make([]publicCategoryDTO, 0, len(ordered))
	for _, item := range ordered {
		items = append(items, item.DTO)
	}
	response.OK(c, items)
}

func (h Handler) Products(c *gin.Context) {
	currencyQuote, currencyErr := h.storefrontCurrency(c, c.Query("currency"))
	if currencyErr != nil {
		status, code, message := currencyRequestError(currencyErr)
		response.Error(c, status, code, message)
		return
	}
	storefront, err := h.resolveStorefront(c)
	if err != nil {
		response.Error(c, 500, 50002, "error.shop_product_fetch_failed")
		return
	}
	page, pageSize := pagination(c)
	query := h.DB.Model(&model.Product{}).Preload("Category").Where("status = ?", "on_sale")
	if storefront != nil {
		query = applyResellerCatalogScope(query, storefront.Profile.ID)
	}
	if category := c.Query("category"); category != "" {
		query = query.Joins("JOIN categories ON categories.id = products.category_id").Where("categories.slug = ?", category)
	}
	if keyword := strings.TrimSpace(c.Query("q")); keyword != "" {
		like := "%" + keyword + "%"
		query = query.Where("products.name ILIKE ? OR products.summary ILIKE ?", like, like)
	}
	if c.Query("featured") == "true" {
		query = query.Where("featured = ?", true)
	}
	var total int64
	query.Count(&total)
	var products []model.Product
	if err := query.Order("sort desc, featured desc, created_at desc").Offset((page - 1) * pageSize).Limit(pageSize).Find(&products).Error; err != nil {
		response.Error(c, 500, 50002, "error.product_fetch_failed")
		return
	}
	productIDs := make([]uuid.UUID, 0, len(products))
	for _, product := range products {
		productIDs = append(productIDs, product.ID)
	}
	mediaByProduct, mediaErr := catalogMediaForOwners(h.DB, "product", productIDs)
	if mediaErr != nil {
		response.Error(c, 500, 50002, "error.product_media_fetch_failed")
		return
	}
	items := make([]gin.H, 0, len(products))
	for _, product := range products {
		if storefront == nil {
			dto := toPublicProductDTO(product)
			dto, err = convertPublicProductDTO(dto, currencyQuote.Conversion)
			if err != nil {
				response.Error(c, 503, 50366, "error.currency_rate_unavailable")
				return
			}
			dto.Media = mediaByProduct[product.ID]
			items = append(items, gin.H{"product": dto, "stock": h.productStock(product)})
			continue
		}
		dto, _, stock, enabled, presentationErr := h.resellerProductPresentation(h.DB, product, storefront.Profile.ID, false)
		if presentationErr != nil {
			response.Error(c, 500, 50002, "error.shop_product_price_fetch_failed")
			return
		}
		if enabled {
			dto, presentationErr = convertPublicProductDTO(dto, currencyQuote.Conversion)
			if presentationErr != nil {
				response.Error(c, 503, 50366, "error.currency_rate_unavailable")
				return
			}
			dto.Media = mediaByProduct[product.ID]
			items = append(items, gin.H{"product": dto, "stock": stock})
		}
	}
	response.Page(c, items, total, page, pageSize)
}

func (h Handler) Product(c *gin.Context) {
	currencyQuote, currencyErr := h.storefrontCurrency(c, c.Query("currency"))
	if currencyErr != nil {
		status, code, message := currencyRequestError(currencyErr)
		response.Error(c, status, code, message)
		return
	}
	storefront, err := h.resolveStorefront(c)
	if err != nil {
		response.Error(c, 500, 50002, "error.shop_product_fetch_failed")
		return
	}
	var product model.Product
	if err := h.DB.Preload("Category").Where("slug = ? AND status = ?", c.Param("slug"), "on_sale").First(&product).Error; err != nil {
		response.Error(c, 404, 40401, "error.product_not_found_or_unavailable")
		return
	}
	if storefront != nil {
		dto, variants, stock, enabled, presentationErr := h.resellerProductPresentation(h.DB, product, storefront.Profile.ID, true)
		if presentationErr != nil {
			response.Error(c, 500, 50002, "error.shop_product_price_fetch_failed")
			return
		}
		if !enabled {
			response.Error(c, 404, 40401, "error.product_not_in_store")
			return
		}
		dto, presentationErr = convertPublicProductDTO(dto, currencyQuote.Conversion)
		if presentationErr != nil {
			response.Error(c, 503, 50366, "error.currency_rate_unavailable")
			return
		}
		for index := range variants {
			variants[index], presentationErr = convertPublicVariantDTO(variants[index], currencyQuote.Conversion)
			if presentationErr != nil {
				response.Error(c, 503, 50366, "error.currency_rate_unavailable")
				return
			}
		}
		inputFields, fieldErr := h.publicProductInputFields(product.ID)
		if fieldErr != nil {
			response.Error(c, 500, 50002, "error.product_input_fields_fetch_failed")
			return
		}
		mediaByProduct, mediaErr := catalogMediaForOwners(h.DB, "product", []uuid.UUID{product.ID})
		if mediaErr != nil {
			response.Error(c, 500, 50002, "error.product_media_fetch_failed")
			return
		}
		dto.Media = mediaByProduct[product.ID]
		response.OK(c, gin.H{"product": dto, "stock": stock, "variants": variants, "input_fields": inputFields})
		return
	}
	stock := h.productStock(product)
	var variants []model.ProductVariant
	h.DB.Where("product_id = ? AND status = ?", product.ID, "active").Order("sort DESC, created_at ASC").Find(&variants)
	publicVariants := make([]publicProductVariantDTO, 0, len(variants))
	for _, variant := range variants {
		variantDTO, conversionErr := convertPublicVariantDTO(toPublicProductVariantDTO(variant, h.productStockForVariant(product, &variant.ID)), currencyQuote.Conversion)
		if conversionErr != nil {
			response.Error(c, 503, 50366, "error.currency_rate_unavailable")
			return
		}
		publicVariants = append(publicVariants, variantDTO)
	}
	inputFields, fieldErr := h.publicProductInputFields(product.ID)
	if fieldErr != nil {
		response.Error(c, 500, 50002, "error.product_input_fields_fetch_failed")
		return
	}
	dto := toPublicProductDTO(product)
	dto, err = convertPublicProductDTO(dto, currencyQuote.Conversion)
	if err != nil {
		response.Error(c, 503, 50366, "error.currency_rate_unavailable")
		return
	}
	mediaByProduct, mediaErr := catalogMediaForOwners(h.DB, "product", []uuid.UUID{product.ID})
	if mediaErr != nil {
		response.Error(c, 500, 50002, "error.product_media_fetch_failed")
		return
	}
	dto.Media = mediaByProduct[product.ID]
	response.OK(c, gin.H{"product": dto, "stock": stock, "variants": publicVariants, "input_fields": inputFields})
}

func (h Handler) publicProductInputFields(productID uuid.UUID) ([]publicProductInputFieldDTO, error) {
	var fields []model.ProductInputField
	if err := h.DB.Where("product_id = ? AND enabled = ?", productID, true).Order("sort DESC, created_at ASC").Find(&fields).Error; err != nil {
		return nil, err
	}
	items := make([]publicProductInputFieldDTO, 0, len(fields))
	for _, field := range fields {
		items = append(items, toPublicProductInputFieldDTO(field))
	}
	return items, nil
}

func (h Handler) productStock(product model.Product) int64 {
	var variants []model.ProductVariant
	if err := h.DB.Select("id").Where("product_id = ? AND status = ?", product.ID, "active").Find(&variants).Error; err == nil && len(variants) > 0 {
		var stock int64
		for _, variant := range variants {
			stock += h.productStockForVariant(product, &variant.ID)
		}
		return stock
	}
	return h.productStockForVariant(product, nil)
}

func (h Handler) productStockForVariant(product model.Product, variantID *uuid.UUID) int64 {
	var stock int64
	if product.InventoryMode == "supplier" {
		// During a rolling deployment an API instance can briefly run before the
		// reservation migration has reached the database. Keep the storefront
		// readable in that narrow window, while the order service still refuses
		// to create unreserved supplier orders until the table exists.
		if !h.DB.Migrator().HasTable(&model.SupplierInventoryReservation{}) {
			query := h.DB.Model(&model.SupplierProduct{}).
				Where("product_id = ?", product.ID)
			if variantID == nil {
				query = query.Where("variant_id IS NULL")
			} else {
				query = query.Where("variant_id = ?", *variantID)
			}
			query.Select("COALESCE(MAX(external_stock), 0)").Scan(&stock)
			return stock
		}
		// Public stock is the best executable mapping's observed stock minus
		// local holds belonging to non-terminal orders. MAX alone would expose
		// stale capacity and allow the storefront to sell through concurrent
		// pending/paid reservations.
		query := h.DB.Table("supplier_products sp").
			Joins("JOIN suppliers s ON s.id = sp.supplier_id AND s.deleted_at IS NULL AND s.status = ?", "active").
			Joins(`JOIN product_mappings pm ON pm.supplier_id = sp.supplier_id AND pm.product_id = sp.product_id AND pm.external_product_id = sp.external_id AND pm.variant_id IS NOT DISTINCT FROM sp.variant_id AND pm.deleted_at IS NULL`).
			Joins(`LEFT JOIN (
				SELECT r.supplier_product_id, COALESCE(SUM(r.quantity), 0) AS held
				FROM supplier_inventory_reservations r
				JOIN orders ro ON ro.id = r.order_id AND ro.deleted_at IS NULL
				WHERE r.deleted_at IS NULL AND r.status = 'reserved' AND ro.status IN ('pending_payment','paid','processing')
				GROUP BY r.supplier_product_id
			) holds ON holds.supplier_product_id = sp.id`).
			Where("sp.product_id = ? AND sp.deleted_at IS NULL", product.ID)
		if variantID == nil {
			query = query.Where("sp.variant_id IS NULL")
		} else {
			query = query.Where("sp.variant_id = ?", *variantID)
		}
		if err := query.Select("COALESCE(MAX(GREATEST(sp.external_stock - COALESCE(holds.held, 0), 0)), 0)").Scan(&stock).Error; err != nil {
			return 0
		}
		return stock
	}
	query := h.DB.Model(&model.Card{}).Where("product_id = ? AND status = ?", product.ID, "available")
	if variantID == nil {
		query = query.Where("variant_id IS NULL")
	} else {
		query = query.Where("variant_id = ?", *variantID)
	}
	query.Count(&stock)
	return stock
}

type createOrderRequest struct {
	ProductID         string                      `json:"product_id"`
	ExternalProductID string                      `json:"external_product_id"`
	VariantID         string                      `json:"variant_id"`
	Quantity          int                         `json:"quantity"`
	Contact           string                      `json:"contact"`
	Email             string                      `json:"email"`
	PaymentMethod     string                      `json:"payment_method"`
	CouponCode        string                      `json:"coupon_code"`
	ClientOrderNo     string                      `json:"client_order_no"`
	CallbackURL       string                      `json:"callback_url"`
	InputValues       []checkoutInputValueRequest `json:"input_values"`
	Parameters        map[string]string           `json:"parameters"`
	Currency          string                      `json:"currency"`
}

func (h Handler) CreateOrder(c *gin.Context) {
	var req createOrderRequest
	if err := decodeStrictJSON(c, &req); err != nil || req.Quantity < 1 || req.Quantity > 20 || strings.TrimSpace(req.PaymentMethod) == "" || strings.TrimSpace(req.ExternalProductID) != "" || strings.TrimSpace(req.CallbackURL) != "" {
		response.Error(c, 422, 42201, "error.order_parameters_incomplete")
		return
	}
	if userID := optionalUserID(c); userID != nil {
		var account model.User
		if err := h.DB.Select("email").First(&account, "id = ? AND status = ?", *userID, "active").Error; err != nil {
			response.Error(c, 401, 40140, "error.invalid_login_state")
			return
		}
		req.Contact = account.Email
		req.Email = account.Email
	}
	contact := req.Contact
	if strings.TrimSpace(contact) == "" {
		contact = req.Email
	}
	contact, validContact := normalizeCheckoutContact(contact)
	if !validContact {
		response.Error(c, 422, 42201, "error.order_parameters_incomplete")
		return
	}
	req.Email = contact
	req.PaymentMethod = strings.ToLower(strings.TrimSpace(req.PaymentMethod))
	req.CouponCode = strings.TrimSpace(req.CouponCode)
	currencyQuote, currencyErr := h.storefrontCurrency(c, req.Currency)
	if currencyErr != nil {
		status, code, message := currencyRequestError(currencyErr)
		response.Error(c, status, code, message)
		return
	}
	productID, err := uuid.Parse(req.ProductID)
	if err != nil {
		response.Error(c, 422, 42202, "error.product_id_invalid")
		return
	}
	var variantID *uuid.UUID
	if strings.TrimSpace(req.VariantID) != "" {
		parsed, parseErr := uuid.Parse(req.VariantID)
		if parseErr != nil {
			response.Error(c, 422, 42206, "error.invalid_spec_number")
			return
		}
		variantID = &parsed
	}
	input := service.CreateOrderInput{
		ProductID: productID, VariantID: variantID, Quantity: req.Quantity, Email: strings.ToLower(req.Email),
		PaymentMethod: req.PaymentMethod, CouponCode: req.CouponCode, ClientIP: c.ClientIP(),
	}
	// Buyer-selected currency is a display quote only. Immutable order and
	// ledger facts always use the administrator's store currency.
	input.Currency = currencyQuote.Conversion.Source.Code
	input.InputValues, err = parseSingleOrderInputValues(h.DB, productID, variantID, req.InputValues, req.Parameters)
	if err != nil {
		response.Error(c, 422, 42209, "error.product_input_values_invalid")
		return
	}
	input.StorefrontRequest, err = storefrontOrderIdempotency(c, req.Email, req.ClientOrderNo, struct {
		ProductID     uuid.UUID                     `json:"product_id"`
		VariantID     *uuid.UUID                    `json:"variant_id"`
		Quantity      int                           `json:"quantity"`
		Email         string                        `json:"email"`
		PaymentMethod string                        `json:"payment_method"`
		CouponCode    string                        `json:"coupon_code"`
		Currency      string                        `json:"currency"`
		ClientOrderNo string                        `json:"client_order_no"`
		InputValues   []service.SubmittedInputValue `json:"input_values"`
	}{productID, variantID, req.Quantity, req.Email, req.PaymentMethod, req.CouponCode, input.Currency, strings.TrimSpace(req.ClientOrderNo), input.InputValues})
	if err != nil {
		response.Error(c, 422, 42299, "error.idempotency_key_required_or_invalid")
		return
	}
	storefront, storefrontErr := h.resolveStorefront(c)
	if storefrontErr != nil {
		response.Error(c, 500, 50003, "error.shop_settlement_config_fetch_failed")
		return
	}
	input.ResellerID = storefrontResellerID(storefront)
	if userID, parseErr := uuid.Parse(c.GetString("subject")); parseErr == nil {
		input.UserID = &userID
	}
	var channel model.PaymentChannel
	if req.PaymentMethod != "balance" {
		if err := h.DB.Where("code = ? AND enabled = ?", req.PaymentMethod, true).First(&channel).Error; err != nil {
			response.Error(c, 422, 42204, "error.payment_channel_unavailable")
			return
		}
		if _, err := paymentChannelSettlementCurrency(channel); err != nil {
			response.Error(c, 422, 42299, "error.payment_channel_settlement_currency_invalid")
			return
		}
		input.FeeRate = channel.FeeRate
		input.PaymentChannelID = &channel.ID
	}
	var riskQuote service.CheckoutQuote
	quoteErr := h.DB.Transaction(func(tx *gorm.DB) error {
		var resolveErr error
		riskQuote, resolveErr = service.ResolveCheckoutQuoteForReseller(tx, input.UserID, input.ResellerID, input.Email, input.CouponCode, input.FeeRate, []service.CheckoutLineInput{{ProductID: productID, VariantID: variantID, Quantity: req.Quantity}})
		return resolveErr
	})
	if quoteErr == nil {
		decisionID, allowed := h.authorizeCheckout(c, input.UserID, input.Email, riskQuote.Total)
		if !allowed {
			return
		}
		input.RiskDecisionID = &decisionID
	}
	var order *model.Order
	if h.Cfg.Env != "production" && req.PaymentMethod == "sandbox" {
		order, err = service.CreateOrder(h.DB, h.Vault, input)
	} else {
		order, err = service.CreatePendingOrder(h.DB, h.Vault, input)
	}
	if errors.Is(err, service.ErrInsufficientStock) {
		response.Error(c, 409, 40901, "error.insufficient_stock_reduce_quantity")
		return
	}
	if errors.Is(err, service.ErrPendingOrderLimit) {
		response.Error(c, 429, 42901, "error.unpaid_order_quota_exceeded")
		return
	}
	if errors.Is(err, service.ErrPaymentChannelNotAllowed) {
		response.Error(c, 422, 42205, "error.product_channel_unsupported")
		return
	}
	if errors.Is(err, service.ErrVariantRequired) || errors.Is(err, service.ErrVariantUnavailable) || errors.Is(err, service.ErrResellerProductUnavailable) {
		response.Error(c, 422, 42207, "error.valid_spec_and_quantity_required")
		return
	}
	if errors.Is(err, service.ErrCouponUnavailable) {
		response.Error(c, 422, 42208, "error.coupon_invalid_or_conditions_not_met")
		return
	}
	if errors.Is(err, service.ErrOrderIdempotencyConflict) {
		response.Error(c, 409, 40910, "error.order_idempotency_conflict")
		return
	}
	if isOrderInputValidationError(err) {
		response.Error(c, 422, 42209, "error.product_input_values_invalid_or_required")
		return
	}
	if err != nil {
		response.Error(c, 500, 50003, "error.order_create_failed")
		return
	}
	values := map[string]string{"order_no": order.OrderNo, "email": order.Email, "status": order.Status, "amount": strconv.FormatInt(order.Total, 10), "currency": order.Currency, "summary": "商城订单已创建"}
	if order.UserID != nil {
		values["user_id"] = order.UserID.String()
	}
	_ = h.createOperationalNotifications(h.DB, "order.created", order.ID.String(), values)
	if order.Status == "processing" {
		_ = h.enqueueSupplierOrder(order.ID)
	} else if order.Status == "delivered" {
		_ = h.dispatchOrderDelivery(order.ID)
	}
	response.Created(c, toPublicOrderDTO(*order))
}

func (h Handler) QueryOrder(c *gin.Context) {
	lookupToken := strings.TrimSpace(c.GetHeader("X-Order-Token"))
	if len(lookupToken) < 40 || len(lookupToken) > 100 {
		response.Error(c, 422, 42203, "error.valid_order_query_key_required")
		return
	}
	var order model.Order
	if err := h.DB.Preload("Items").Where("order_no = ? AND lookup_token_hash = ?", c.Param("order_no"), service.HashOrderLookupToken(lookupToken)).First(&order).Error; err != nil {
		response.Error(c, 404, 40402, "error.order_match_not_found")
		return
	}
	if (order.Status == "delivered" || order.Status == "completed") && order.PaymentStatus == "paid" {
		if err := h.revealOrder(&order); err != nil {
			response.Error(c, 500, 50004, "error.delivery_content_decrypt_failed")
			return
		}
	}
	response.OK(c, toPublicOrderDTO(order))
}

type registerRequest struct {
	Email        string `json:"email" binding:"required,email"`
	Password     string `json:"password" binding:"required,min=8,max=72"`
	Nickname     string `json:"nickname" binding:"required,max=80"`
	ReferralCode string `json:"referral_code"`
}

func (h Handler) Register(c *gin.Context) {
	var req registerRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, 422, 42210, "error.registration_information_incomplete")
		return
	}
	email, validEmail := normalizeUserEmail(req.Email)
	nickname := strings.TrimSpace(req.Nickname)
	if !validEmail || !validUserNickname(nickname) {
		response.Error(c, 422, 42210, "error.registration_information_incomplete")
		return
	}
	if validateUserPassword(req.Password) != nil {
		response.Error(c, 422, 42210, "error.password_policy_not_met")
		return
	}
	hash, hashErr := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if hashErr != nil {
		response.Error(c, 422, 42210, "error.invalid_password_format")
		return
	}
	preferredLocale := supportedNotificationLocale(c.GetHeader("X-LinLinQi-Locale"))
	user := model.User{Email: email, PasswordHash: string(hash), Nickname: nickname, Status: "active", PreferredLocale: preferredLocale}
	err := h.DB.Transaction(func(tx *gorm.DB) error {
		var affiliate *model.AffiliateProfile
		if strings.TrimSpace(req.ReferralCode) != "" {
			profile, findErr := service.AffiliateProfileForReferral(tx, req.ReferralCode)
			if findErr != nil {
				return findErr
			}
			affiliate = profile
		}
		if createErr := tx.Create(&user).Error; createErr != nil {
			return createErr
		}
		if affiliate != nil {
			if err := service.CreateAffiliateReferral(tx, *affiliate, user.ID); err != nil {
				return err
			}
		}
		return h.createOperationalNotifications(tx, "user.registered", user.ID.String(), map[string]string{"user_id": user.ID.String(), "email": user.Email, "status": user.Status, "locale": preferredLocale, "summary": "新用户完成注册"})
	})
	if err != nil {
		if strings.TrimSpace(req.ReferralCode) != "" && errors.Is(err, gorm.ErrRecordNotFound) {
			response.Error(c, 422, 42213, "error.promo_code_invalid_or_account_not_enabled")
			return
		}
		response.Error(c, 409, 40910, "error.email_already_registered")
		return
	}
	refreshToken, sessionID, err := h.createUserSession(c, user.ID)
	if err != nil {
		response.Error(c, 500, 50010, "error.login_session_create_failed")
		return
	}
	token, _ := middleware.IssueUserToken(user.ID.String(), h.Cfg.JWTSecret, sessionID.String(), user.SessionVersion, 15*time.Minute)
	response.Created(c, gin.H{"token": token, "refresh_token": refreshToken, "expires_in": 900, "user": user})
}

type loginRequest struct {
	Account  string `json:"account" binding:"required"`
	Password string `json:"password" binding:"required"`
	OTP      string `json:"otp"`
}

var dummyLoginPasswordHash = func() []byte {
	hash, err := bcrypt.GenerateFromPassword([]byte("LinLinQi-Dummy-Login-Hash-2026"), bcrypt.DefaultCost)
	if err != nil {
		panic(err)
	}
	return hash
}()

func (h Handler) UserLogin(c *gin.Context) {
	var req loginRequest
	if c.ShouldBindJSON(&req) != nil {
		response.Error(c, 422, 42211, "error.email_and_password_required")
		return
	}
	account := strings.ToLower(strings.TrimSpace(req.Account))
	if len(account) == 0 || len(account) > 190 || len(req.Password) == 0 || len(req.Password) > 72 || strings.IndexFunc(account, unicode.IsControl) >= 0 {
		_ = bcrypt.CompareHashAndPassword(dummyLoginPasswordHash, []byte("invalid-login-input"))
		h.recordLogin(c, "user", truncateSecurityValue(account, 190), nil, false, "invalid_credentials")
		response.Error(c, 401, 40110, "error.invalid_email_or_password")
		return
	}
	var user model.User
	findErr := h.DB.Where("email = ? AND status = ?", account, "active").First(&user).Error
	passwordHash := dummyLoginPasswordHash
	if findErr == nil {
		passwordHash = []byte(user.PasswordHash)
	}
	if bcrypt.CompareHashAndPassword(passwordHash, []byte(req.Password)) != nil || findErr != nil {
		h.recordLogin(c, "user", account, nil, false, "invalid_credentials")
		response.Error(c, 401, 40110, "error.invalid_email_or_password")
		return
	}
	h.DB.Model(&user).Update("last_login_at", time.Now())
	h.recordLogin(c, "user", user.Email, &user.ID, true, "")
	refreshToken, sessionID, err := h.createUserSession(c, user.ID)
	if err != nil {
		response.Error(c, 500, 50010, "error.login_session_create_failed")
		return
	}
	token, _ := middleware.IssueUserToken(user.ID.String(), h.Cfg.JWTSecret, sessionID.String(), user.SessionVersion, 15*time.Minute)
	response.OK(c, gin.H{"token": token, "refresh_token": refreshToken, "expires_in": 900, "user": user})
}

func (h Handler) AdminLogin(c *gin.Context) {
	var req loginRequest
	if c.ShouldBindJSON(&req) != nil {
		response.Error(c, 422, 42220, "error.admin_credentials_required")
		return
	}
	account := strings.TrimSpace(req.Account)
	if len(account) == 0 || len(account) > 80 || len(req.Password) == 0 || len(req.Password) > 72 || strings.IndexFunc(account, unicode.IsControl) >= 0 || len(req.OTP) > 32 {
		_ = bcrypt.CompareHashAndPassword(dummyLoginPasswordHash, []byte("invalid-login-input"))
		h.recordLogin(c, "admin", truncateSecurityValue(account, 80), nil, false, "invalid_credentials")
		response.Error(c, 401, 40111, "error.invalid_account_or_password")
		return
	}
	var admin model.Admin
	findErr := h.DB.Where("username = ? AND status = ?", account, "active").First(&admin).Error
	passwordHash := dummyLoginPasswordHash
	if findErr == nil {
		passwordHash = []byte(admin.PasswordHash)
	}
	if bcrypt.CompareHashAndPassword(passwordHash, []byte(req.Password)) != nil || findErr != nil {
		h.recordLogin(c, "admin", account, nil, false, "invalid_credentials")
		response.Error(c, 401, 40111, "error.invalid_account_or_password")
		return
	}
	if ok, err := h.validateAdminOTP(admin.ID, req.OTP); err != nil || !ok {
		h.recordLogin(c, "admin", account, &admin.ID, false, "invalid_totp")
		response.Error(c, 401, 40112, "error.two_factor_code_required")
		return
	}
	h.DB.Model(&admin).Update("last_login_at", time.Now())
	h.recordLogin(c, "admin", admin.Username, &admin.ID, true, "")
	token, err := middleware.IssueVersionedToken(admin.ID.String(), "admin", admin.Role, h.Cfg.AdminJWTSecret, admin.SessionVersion, 30*time.Minute)
	if err != nil {
		response.Error(c, 500, 50020, "error.admin_session_create_failed")
		return
	}
	profile, err := loadAdminSessionProfile(h.DB, admin.ID)
	if err != nil {
		response.Error(c, 500, 50020, "error.admin_session_create_failed")
		return
	}
	response.OK(c, gin.H{"token": token, "expires_in": 1800, "admin": profile})
}

func (h Handler) Dashboard(c *gin.Context) {
	currencyCode, err := service.StoreCurrency(h.DB)
	if err != nil {
		response.Error(c, 500, 50030, "error.store_currency_fetch_failed")
		return
	}
	now := time.Now().UTC()
	start := now.Truncate(24 * time.Hour)
	yesterday := start.Add(-24 * time.Hour)
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	var todayOrders, yesterdayOrders, totalUsers, monthlyNewUsers, activeProducts, lowStock int64
	var todayRevenue, yesterdayRevenue, todayRefunds, yesterdayRefunds int64
	h.DB.Model(&model.Order{}).Where("created_at >= ? AND currency = ?", start, currencyCode).Count(&todayOrders)
	h.DB.Model(&model.Order{}).Where("paid_at >= ? AND currency = ?", start, currencyCode).Select("COALESCE(SUM(total),0)").Scan(&todayRevenue)
	h.DB.Model(&model.Refund{}).Where("processed_at >= ? AND status = ? AND order_currency = ?", start, "succeeded", currencyCode).Select("COALESCE(SUM(order_amount),0)").Scan(&todayRefunds)
	todayRevenue -= todayRefunds
	h.DB.Model(&model.Order{}).Where("created_at >= ? AND created_at < ? AND currency = ?", yesterday, start, currencyCode).Count(&yesterdayOrders)
	h.DB.Model(&model.Order{}).Where("paid_at >= ? AND paid_at < ? AND currency = ?", yesterday, start, currencyCode).Select("COALESCE(SUM(total),0)").Scan(&yesterdayRevenue)
	h.DB.Model(&model.Refund{}).Where("processed_at >= ? AND processed_at < ? AND status = ? AND order_currency = ?", yesterday, start, "succeeded", currencyCode).Select("COALESCE(SUM(order_amount),0)").Scan(&yesterdayRefunds)
	yesterdayRevenue -= yesterdayRefunds
	h.DB.Model(&model.User{}).Count(&totalUsers)
	h.DB.Model(&model.User{}).Where("created_at >= ?", monthStart).Count(&monthlyNewUsers)
	h.DB.Model(&model.Product{}).Where("status = ?", "on_sale").Count(&activeProducts)
	stockThreshold := 10
	var stockSetting model.Setting
	if h.DB.First(&stockSetting, "key = ?", "inventory_warning_threshold").Error == nil {
		if parsed, err := strconv.Atoi(stockSetting.Value); err == nil && parsed >= 1 && parsed <= 100000 {
			stockThreshold = parsed
		}
	}
	h.DB.Raw("SELECT COUNT(*) FROM products p WHERE p.deleted_at IS NULL AND (SELECT COUNT(*) FROM cards c WHERE c.product_id = p.id AND c.status = 'available' AND c.deleted_at IS NULL) < ?", stockThreshold).Scan(&lowStock)
	var orders []model.Order
	h.DB.Select("id", "order_no", "status", "total", "currency", "created_at").Where("currency = ?", currencyCode).Order("created_at desc").Limit(6).Find(&orders)
	recentOrders := make([]dashboardRecentOrderDTO, 0, len(orders))
	if len(orders) > 0 {
		orderIDs := make([]uuid.UUID, 0, len(orders))
		for _, order := range orders {
			orderIDs = append(orderIDs, order.ID)
		}
		var itemRows []struct {
			OrderID     uuid.UUID
			ProductName string
		}
		h.DB.Model(&model.OrderItem{}).Distinct("order_id", "product_name").Where("order_id IN ?", orderIDs).Order("order_id, product_name").Find(&itemRows)
		productNames := make(map[uuid.UUID][]dashboardRecentOrderItemDTO, len(orders))
		for _, row := range itemRows {
			productNames[row.OrderID] = append(productNames[row.OrderID], dashboardRecentOrderItemDTO{ProductName: row.ProductName})
		}
		for _, order := range orders {
			items := productNames[order.ID]
			if items == nil {
				items = []dashboardRecentOrderItemDTO{}
			}
			recentOrders = append(recentOrders, dashboardRecentOrderDTO{OrderNo: order.OrderNo, Status: order.Status, Total: order.Total, Currency: order.Currency, CreatedAt: order.CreatedAt, Items: items})
		}
	}
	var delivered, paid int64
	h.DB.Model(&model.Order{}).Where("paid_at >= ? AND currency = ?", start, currencyCode).Count(&paid)
	h.DB.Model(&model.Order{}).Where("paid_at >= ? AND delivered_at IS NOT NULL AND currency = ?", start, currencyCode).Count(&delivered)
	var paymentAttempts, paymentSuccess int64
	h.DB.Model(&model.PaymentIntent{}).Where("created_at >= ? AND currency = ? AND status NOT IN ?", start, currencyCode, []string{"creating", "pending"}).Count(&paymentAttempts)
	h.DB.Model(&model.PaymentIntent{}).Where("created_at >= ? AND currency = ? AND status IN ?", start, currencyCode, []string{"succeeded", "partially_refunded", "refunded"}).Count(&paymentSuccess)
	var averageDeliveryMS float64
	h.DB.Model(&model.Order{}).Where("delivered_at IS NOT NULL AND paid_at IS NOT NULL AND delivered_at >= ? AND currency = ?", start, currencyCode).Select("COALESCE(AVG(EXTRACT(EPOCH FROM (delivered_at - paid_at)) * 1000),0)").Scan(&averageDeliveryMS)
	var retriedOrders int64
	h.DB.Model(&model.FulfillmentAttempt{}).Where("created_at >= ? AND attempt > 1", start).Distinct("order_id").Count(&retriedOrders)
	type hourlyRow struct {
		Hour    time.Time
		Revenue int64
	}
	var hourlyRows []hourlyRow
	h.DB.Model(&model.Order{}).Select("date_trunc('hour', paid_at) AS hour, COALESCE(SUM(total),0) AS revenue").Where("paid_at >= ? AND currency = ?", start, currencyCode).Group("hour").Order("hour").Scan(&hourlyRows)
	hourlyRevenue := make([]int64, 24)
	for _, row := range hourlyRows {
		hourlyRevenue[row.Hour.UTC().Hour()] = row.Revenue
	}
	var hourlyRefunds []hourlyRow
	h.DB.Model(&model.Refund{}).Select("date_trunc('hour', processed_at) AS hour, COALESCE(SUM(order_amount),0) AS revenue").Where("processed_at >= ? AND status = ? AND order_currency = ?", start, "succeeded", currencyCode).Group("hour").Order("hour").Scan(&hourlyRefunds)
	for _, row := range hourlyRefunds {
		hourlyRevenue[row.Hour.UTC().Hour()] -= row.Revenue
	}
	percentChange := func(current, previous int64) float64 {
		if previous == 0 {
			if current > 0 {
				return 100
			}
			return 0
		}
		return float64(current-previous) * 100 / float64(previous)
	}
	ratio := func(part, whole int64) float64 {
		if whole == 0 {
			return 0
		}
		return float64(part) * 100 / float64(whole)
	}
	response.OK(c, gin.H{
		"currency":      currencyCode,
		"today_revenue": todayRevenue, "today_orders": todayOrders, "total_users": totalUsers,
		"active_products": activeProducts, "low_stock": lowStock, "recent_orders": recentOrders,
		"revenue_change": percentChange(todayRevenue, yesterdayRevenue), "order_change": percentChange(todayOrders, yesterdayOrders),
		"monthly_new_users": monthlyNewUsers, "average_order_value": func() int64 {
			if paid == 0 {
				return 0
			}
			return todayRevenue / paid
		}(),
		"delivery_success_rate": ratio(delivered, paid), "payment_success_rate": ratio(paymentSuccess, paymentAttempts),
		"average_delivery_ms": averageDeliveryMS, "retried_orders": retriedOrders, "hourly_revenue": hourlyRevenue,
	})
}

func (h Handler) AdminProducts(c *gin.Context) {
	page, pageSize := pagination(c)
	query := h.DB.Model(&model.Product{}).Preload("Category")
	if keyword := strings.TrimSpace(c.Query("q")); keyword != "" {
		like := "%" + keyword + "%"
		query = query.Where("products.name ILIKE ? OR products.slug ILIKE ? OR products.summary ILIKE ?", like, like, like)
	}
	if status := strings.TrimSpace(c.Query("status")); status != "" {
		if status != "draft" && status != "on_sale" && status != "off_sale" {
			response.Error(c, 422, 42309, "error.product_status_filter_invalid")
			return
		}
		query = query.Where("products.status = ?", status)
	}
	if inventoryMode := strings.TrimSpace(c.Query("inventory_mode")); inventoryMode != "" {
		if inventoryMode != "local" && inventoryMode != "supplier" {
			response.Error(c, 422, 42310, "error.inventory_mode_filter_invalid")
			return
		}
		query = query.Where("products.inventory_mode = ?", inventoryMode)
	}
	if categoryID := strings.TrimSpace(c.Query("category_id")); categoryID != "" {
		parsed, err := uuid.Parse(categoryID)
		if err != nil {
			response.Error(c, 422, 42311, "error.product_category_filter_id_invalid")
			return
		}
		query = query.Where("products.category_id = ?", parsed)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		response.Error(c, 500, 50086, "error.product_fetch_failed")
		return
	}
	var items []model.Product
	if err := query.Order("products.sort DESC, products.created_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&items).Error; err != nil {
		response.Error(c, 500, 50086, "error.product_fetch_failed")
		return
	}
	dtos, err := adminCatalogProductDTOs(h.DB, items)
	if err != nil {
		response.Error(c, 500, 50086, "error.product_fetch_failed")
		return
	}
	response.Page(c, dtos, total, page, pageSize)
}

func (h Handler) AdminOrders(c *gin.Context) {
	page, pageSize := pagination(c)
	query := applyAdminOrderFilters(h.DB.Model(&model.Order{}), c)
	var total int64
	query.Count(&total)
	var items []model.Order
	query.Preload("Items").Order("created_at desc").Offset((page - 1) * pageSize).Limit(pageSize).Find(&items)
	response.Page(c, items, total, page, pageSize)
}

func (h Handler) AdminCards(c *gin.Context) {
	page, pageSize := pagination(c)
	query := h.DB.Model(&model.Card{})
	if productID := c.Query("product_id"); productID != "" {
		query = query.Where("product_id = ?", productID)
	}
	var total int64
	query.Count(&total)
	var items []model.Card
	query.Order("created_at desc").Offset((page - 1) * pageSize).Limit(pageSize).Find(&items)
	response.Page(c, items, total, page, pageSize)
}

func (h Handler) AdminUsers(c *gin.Context) {
	page, pageSize := pagination(c)
	var total int64
	h.DB.Model(&model.User{}).Count(&total)
	var items []model.User
	h.DB.Order("created_at desc").Offset((page - 1) * pageSize).Limit(pageSize).Find(&items)
	response.Page(c, items, total, page, pageSize)
}

func (h Handler) AdminPayments(c *gin.Context) {
	var items []model.PaymentChannel
	h.DB.Where("provider <> ?", "internal_manual").Order("sort desc").Find(&items)
	result := make([]adminPaymentChannel, 0, len(items))
	for _, item := range items {
		result = append(result, toAdminPaymentChannel(item))
	}
	response.OK(c, result)
}

func (h Handler) AdminCategories(c *gin.Context) {
	var items []model.Category
	h.DB.Order("sort desc").Find(&items)
	response.OK(c, items)
}

func (h Handler) CreateCategory(c *gin.Context) {
	var item model.Category
	if err := c.ShouldBindJSON(&item); err != nil || strings.TrimSpace(item.Name) == "" || strings.TrimSpace(item.Slug) == "" {
		response.Error(c, 422, 42230, "error.category_name_and_code_required")
		return
	}
	item.ID = uuid.Nil
	if err := h.DB.Create(&item).Error; err != nil {
		response.Error(c, 409, 40930, "error.category_slug_exists")
		return
	}
	h.audit(c, "category.create", "category", item.ID.String(), item.Name)
	response.Created(c, item)
}

type productRequest struct {
	CategoryID    string `json:"category_id"`
	Name          string `json:"name"`
	Slug          string `json:"slug"`
	Summary       string `json:"summary"`
	Description   string `json:"description"`
	Price         *int64 `json:"price"`
	ComparePrice  int64  `json:"compare_price"`
	CostPrice     int64  `json:"cost_price"`
	DeliveryType  string `json:"delivery_type"`
	InventoryMode string `json:"inventory_mode"`
	Status        string `json:"status"`
	Featured      bool   `json:"featured"`
	Tags          string `json:"tags"`
}

type updateProductRequest struct {
	Name          *string `json:"name"`
	Summary       *string `json:"summary"`
	Description   *string `json:"description"`
	Price         *int64  `json:"price"`
	ComparePrice  *int64  `json:"compare_price"`
	CostPrice     *int64  `json:"cost_price"`
	DeliveryType  *string `json:"delivery_type"`
	InventoryMode *string `json:"inventory_mode"`
	Status        *string `json:"status"`
	Featured      *bool   `json:"featured"`
	Tags          *string `json:"tags"`
	Sort          *int    `json:"sort"`
}

var (
	errProductHasUnfinishedOrders = errors.New("product has unfinished orders")
	errProductNotLocalInventory   = errors.New("product does not use local inventory")
)

func (h Handler) CreateProduct(c *gin.Context) {
	var req productRequest
	if decodeStrictJSON(c, &req) != nil || req.normalizeAndValidate() != nil {
		response.Error(c, 422, 42231, "error.product_fields_price_status_invalid")
		return
	}
	categoryID, err := uuid.Parse(req.CategoryID)
	if err != nil {
		response.Error(c, 422, 42232, "error.category_id_invalid")
		return
	}
	item := model.Product{CategoryID: categoryID, Name: req.Name, Slug: req.Slug, Summary: req.Summary, Description: req.Description, Price: *req.Price, ComparePrice: req.ComparePrice, CostPrice: req.CostPrice, DeliveryType: req.DeliveryType, InventoryMode: req.InventoryMode, Status: req.Status, Featured: req.Featured, Tags: req.Tags}
	if err := h.DB.Create(&item).Error; err != nil {
		response.Error(c, 409, 40931, "error.product_slug_exists_or_category_invalid")
		return
	}
	h.audit(c, "product.create", "product", item.ID.String(), item.Name)
	response.Created(c, item)
}

func (h Handler) UpdateProduct(c *gin.Context) {
	productID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, 404, 40430, "error.product_not_found")
		return
	}
	var req updateProductRequest
	if decodeStrictJSON(c, &req) != nil || req.empty() || req.normalizeAndValidate() != nil {
		response.Error(c, 422, 42233, "error.update_parameters_invalid")
		return
	}
	updates := req.updates()
	var item model.Product
	err = h.DB.Transaction(func(tx *gorm.DB) error {
		if req.InventoryMode != nil {
			// Order creation reads the product mode in its own transaction. This short,
			// PostgreSQL-specific lock makes that read occur wholly before or after a
			// mode switch, closing the otherwise possible check/insert race.
			if err := tx.Exec("LOCK TABLE products IN ACCESS EXCLUSIVE MODE").Error; err != nil {
				return err
			}
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&item, "id = ?", productID).Error; err != nil {
			return err
		}
		if req.InventoryMode != nil && *req.InventoryMode != item.InventoryMode {
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
		}
		return tx.Model(&item).Updates(updates).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		response.Error(c, 404, 40430, "error.product_not_found")
		return
	}
	if errors.Is(err, errProductHasUnfinishedOrders) {
		response.Error(c, 409, 40933, "error.pending_orders_cannot_switch_inventory_mode")
		return
	}
	if err != nil {
		response.Error(c, 500, 50030, "error.product_update_failed")
		return
	}
	h.audit(c, "product.update", "product", item.ID.String(), "")
	h.DB.Preload("Category").First(&item, "id = ?", item.ID)
	response.OK(c, item)
}

type importCardsRequest struct {
	ProductID string   `json:"product_id" binding:"required"`
	VariantID string   `json:"variant_id"`
	Cards     []string `json:"cards" binding:"required,min=1,max=5000"`
}

func (h Handler) ImportCards(c *gin.Context) {
	var req importCardsRequest
	if c.ShouldBindJSON(&req) != nil {
		response.Error(c, 422, 42234, "error.product_and_cards_required")
		return
	}
	productID, err := uuid.Parse(req.ProductID)
	if err != nil {
		response.Error(c, 422, 42235, "error.product_id_invalid")
		return
	}
	var variantID *uuid.UUID
	if strings.TrimSpace(req.VariantID) != "" {
		parsed, parseErr := uuid.Parse(req.VariantID)
		if parseErr != nil {
			response.Error(c, 422, 42236, "error.invalid_spec_number")
			return
		}
		variantID = &parsed
	}
	var product model.Product
	if err := h.DB.Select("id", "inventory_mode").First(&product, "id = ?", productID).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		response.Error(c, 404, 40431, "error.product_not_found")
		return
	} else if err != nil {
		response.Error(c, 500, 50031, "error.product_fetch_failed")
		return
	}
	var activeVariants int64
	if err := h.DB.Model(&model.ProductVariant{}).Where("product_id = ? AND status = ?", productID, "active").Count(&activeVariants).Error; err != nil {
		response.Error(c, 500, 50031, "error.product_sku_list_fetch_failed")
		return
	}
	if activeVariants > 0 && variantID == nil {
		response.Error(c, 422, 42237, "error.spec_required_for_import")
		return
	}
	if variantID != nil {
		var variant model.ProductVariant
		if err := h.DB.Where("id = ? AND product_id = ? AND status = ?", *variantID, productID, "active").First(&variant).Error; err != nil {
			response.Error(c, 422, 42236, "error.product_spec_unavailable")
			return
		}
	}
	if product.InventoryMode != "local" {
		response.Error(c, 409, 40934, "error.supplier_inventory_cannot_import_local_keys")
		return
	}
	items := make([]model.Card, 0, len(req.Cards))
	for _, content := range req.Cards {
		if trimmed := strings.TrimSpace(content); trimmed != "" {
			ciphertext, nonce, fingerprint, err := h.Vault.Encrypt(trimmed, productID[:])
			if err != nil {
				response.Error(c, 500, 50032, "error.card_encrypt_failed")
				return
			}
			items = append(items, model.Card{ProductID: productID, VariantID: variantID, EncryptedContent: ciphertext, Nonce: nonce, Fingerprint: fingerprint, Preview: security.SecretPreview(trimmed), Status: "available"})
		}
	}
	if len(items) == 0 {
		response.Error(c, 422, 42234, "error.cards_blank_lines_only")
		return
	}
	err = h.DB.Transaction(func(tx *gorm.DB) error {
		var locked model.Product
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id", "inventory_mode").First(&locked, "id = ?", productID).Error; err != nil {
			return err
		}
		if locked.InventoryMode != "local" {
			return errProductNotLocalInventory
		}
		var variantCount int64
		if err := tx.Model(&model.ProductVariant{}).Where("product_id = ? AND status = ?", productID, "active").Count(&variantCount).Error; err != nil {
			return err
		}
		if variantCount > 0 && variantID == nil {
			return service.ErrVariantRequired
		}
		if variantID != nil {
			var variant model.ProductVariant
			if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("id = ? AND product_id = ? AND status = ?", *variantID, productID, "active").First(&variant).Error; err != nil {
				return service.ErrVariantUnavailable
			}
		}
		return tx.CreateInBatches(&items, 500).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		response.Error(c, 404, 40431, "error.product_not_found")
		return
	}
	if errors.Is(err, errProductNotLocalInventory) {
		response.Error(c, 409, 40934, "error.inventory_mode_changed_cannot_import_local_keys")
		return
	}
	if errors.Is(err, service.ErrVariantRequired) || errors.Is(err, service.ErrVariantUnavailable) {
		response.Error(c, 409, 40935, "error.spec_changed_reselect_before_import")
		return
	}
	if err != nil {
		response.Error(c, 500, 50031, "error.card_import_failed")
		return
	}
	h.audit(c, "cards.import", "product", productID.String(), strconv.Itoa(len(items)))
	response.Created(c, gin.H{"imported": len(items)})
}

func (h Handler) SaveSettings(c *gin.Context) {
	var values map[string]string
	if decodeStrictJSON(c, &values) != nil || len(values) == 0 {
		response.Error(c, 422, 42237, "error.settings_content_empty")
		return
	}
	reason, ok := requireAdminChangeReason(c, "修改系统设置")
	if !ok {
		return
	}
	values, groups, err := normalizeAdminSettings(values)
	if err != nil {
		response.Error(c, 422, 42238, "error.settings_unsupported_items")
		return
	}
	storeCurrency, currencyChanged := values["store_currency"]
	var sourceCurrency, targetCurrency model.CurrencyDefinition
	var repriceSnapshot model.FXRateSnapshot
	var repriceResult service.StoreRepriceResult
	if currencyChanged {
		currentCode, err := service.StoreCurrency(h.DB)
		if err != nil {
			response.Error(c, 500, 50038, "error.system_settings_fetch_failed")
			return
		}
		if err := h.DB.Where("code = ? AND enabled = ?", storeCurrency, true).First(&targetCurrency).Error; err != nil {
			response.Error(c, 422, 42238, "error.store_currency_unavailable")
			return
		}
		if err := h.DB.Where("code = ? AND enabled = ?", currentCode, true).First(&sourceCurrency).Error; err != nil {
			response.Error(c, 500, 50038, "error.system_settings_fetch_failed")
			return
		}
		currencyChanged = sourceCurrency.Code != targetCurrency.Code
		if currencyChanged {
			manager := fx.Manager{DB: h.DB, AllowPrivate: h.Cfg.Env != "production"}
			repriceSnapshot, err = manager.Resolve(c.Request.Context(), sourceCurrency.Code, targetCurrency.Code)
			if err != nil {
				response.Error(c, 503, 50338, "error.store_currency_rate_unavailable")
				return
			}
		}
	}
	if err := h.DB.Transaction(func(tx *gorm.DB) error {
		if currencyChanged {
			if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtext(?))", "linlinqi:store-currency-change").Error; err != nil {
				return err
			}
			currentCode, err := service.StoreCurrency(tx)
			if err != nil || currentCode != sourceCurrency.Code {
				return service.ErrCurrencyMismatch
			}
			repriceResult, err = service.RepriceStoreCurrencyTx(tx, sourceCurrency, targetCurrency, repriceSnapshot)
			if err != nil {
				return err
			}
		}
		for key, value := range values {
			if err := tx.Save(&model.Setting{Key: key, Value: value, Group: groups[key]}).Error; err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		response.Error(c, 500, 50039, "error.system_settings_save_failed")
		return
	}
	queued, queueFailures := 0, 0
	if currencyChanged {
		var suppliers []model.Supplier
		if err := h.DB.Where("status = ?", "active").Find(&suppliers).Error; err == nil {
			client := queue.NewClient(h.Cfg, h.DB)
			defer client.Close()
			for _, supplier := range suppliers {
				if _, err := client.Enqueue(queue.TypeSupplierSync, map[string]string{"supplier_id": supplier.ID.String()}, asynq.Queue("default"), asynq.Unique(time.Minute)); err != nil {
					queueFailures++
					continue
				}
				queued++
			}
		} else {
			queueFailures++
		}
	}
	h.audit(c, "settings.update", "settings", "", reason+"；updated="+strconv.Itoa(len(values)))
	response.OK(c, gin.H{
		"updated": len(values), "currency_changed": currencyChanged, "catalog_repricing": repriceResult,
		"fx_snapshot_id": repriceSnapshot.ID, "fx_rate": repriceSnapshot.Rate,
		"supplier_repricing_queued": queued, "supplier_repricing_queue_failures": queueFailures,
	})
}

func (h Handler) AdminSettings(c *gin.Context) {
	var settings []model.Setting
	if err := h.DB.Order("\"group\", key").Find(&settings).Error; err != nil {
		response.Error(c, 500, 50038, "error.system_settings_fetch_failed")
		return
	}
	response.OK(c, settings)
}

func (h Handler) audit(c *gin.Context, action, resource, resourceID, detail string) {
	var adminID *uuid.UUID
	if parsed, err := uuid.Parse(c.GetString("subject")); err == nil {
		adminID = &parsed
	}
	metadata := []string{detail}
	if requestID := c.GetString("request_id"); requestID != "" {
		metadata = append(metadata, "request_id="+requestID)
	}
	if reason := strings.TrimSpace(c.GetHeader("X-Change-Reason")); reason != "" {
		// Keep the audit write valid for multibyte administrator notes. Byte
		// slicing can split a UTF-8 sequence and make PostgreSQL reject the log.
		reason = truncateSecurityValue(reason, 500)
		metadata = append(metadata, "reason="+reason)
	}
	h.DB.Create(&model.AuditLog{AdminID: adminID, Action: action, Resource: resource, ResourceID: resourceID, IP: c.ClientIP(), Detail: strings.Trim(strings.Join(metadata, "; "), "; ")})
}

func (h Handler) recordLogin(c *gin.Context, realm, account string, principalID *uuid.UUID, succeeded bool, reason string) {
	account = truncateSecurityValue(strings.TrimSpace(account), 190)
	ip := truncateSecurityValue(c.ClientIP(), 64)
	userAgent := truncateSecurityValue(c.Request.UserAgent(), 500)
	event := model.LoginEvent{Realm: realm, PrincipalID: principalID, Account: account, IP: ip, UserAgent: userAgent, Succeeded: succeeded, Reason: truncateSecurityValue(reason, 200)}
	h.DB.Create(&event)
	code := realm + ".login.failed"
	status := "failed"
	if succeeded {
		code, status = realm+".login.succeeded", "succeeded"
	}
	values := map[string]string{"email": account, "ip": ip, "status": status, "locale": supportedNotificationLocale(c.GetHeader("X-LinLinQi-Locale")), "summary": "登录事件：" + status}
	if realm == "user" && principalID != nil {
		values["user_id"] = principalID.String()
	}
	_ = h.createOperationalNotifications(h.DB, code, event.ID.String(), values)
}

func defaultString(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func decodeStrictJSON(c *gin.Context, destination any) error {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 1<<20)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("request body must contain one JSON value")
		}
		return err
	}
	return nil
}

func validateProductValues(price, comparePrice, costPrice int64, deliveryType, inventoryMode, status string) error {
	if price < 0 || comparePrice < 0 || costPrice < 0 {
		return errors.New("product prices cannot be negative")
	}
	if deliveryType != "auto" && deliveryType != "manual" {
		return fmt.Errorf("unsupported delivery type %q", deliveryType)
	}
	if inventoryMode != "local" && inventoryMode != "supplier" {
		return fmt.Errorf("unsupported inventory mode %q", inventoryMode)
	}
	if status != "draft" && status != "on_sale" && status != "off_sale" {
		return fmt.Errorf("unsupported product status %q", status)
	}
	return nil
}

func (r *productRequest) normalizeAndValidate() error {
	r.CategoryID = strings.TrimSpace(r.CategoryID)
	r.Name = strings.TrimSpace(r.Name)
	r.Slug = strings.TrimSpace(r.Slug)
	r.DeliveryType = strings.ToLower(strings.TrimSpace(defaultString(r.DeliveryType, "auto")))
	r.InventoryMode = strings.ToLower(strings.TrimSpace(defaultString(r.InventoryMode, "local")))
	r.Status = strings.ToLower(strings.TrimSpace(defaultString(r.Status, "draft")))
	if r.CategoryID == "" || r.Name == "" || r.Slug == "" || r.Price == nil {
		return errors.New("required product fields are missing")
	}
	if utf8.RuneCountInString(r.Name) > 160 || utf8.RuneCountInString(r.Slug) > 180 || utf8.RuneCountInString(r.Summary) > 500 || utf8.RuneCountInString(r.Tags) > 500 {
		return errors.New("product text field is too long")
	}
	return validateProductValues(*r.Price, r.ComparePrice, r.CostPrice, r.DeliveryType, r.InventoryMode, r.Status)
}

func (r updateProductRequest) empty() bool {
	return r.Name == nil && r.Summary == nil && r.Description == nil && r.Price == nil && r.ComparePrice == nil && r.CostPrice == nil && r.DeliveryType == nil && r.InventoryMode == nil && r.Status == nil && r.Featured == nil && r.Tags == nil && r.Sort == nil
}

func (r *updateProductRequest) normalizeAndValidate() error {
	if r.Name != nil {
		value := strings.TrimSpace(*r.Name)
		if value == "" || utf8.RuneCountInString(value) > 160 {
			return errors.New("invalid product name")
		}
		r.Name = &value
	}
	if r.Summary != nil && utf8.RuneCountInString(*r.Summary) > 500 {
		return errors.New("product summary is too long")
	}
	if r.Tags != nil && utf8.RuneCountInString(*r.Tags) > 500 {
		return errors.New("product tags are too long")
	}
	if r.Price != nil && *r.Price < 0 {
		return errors.New("product price cannot be negative")
	}
	if r.ComparePrice != nil && *r.ComparePrice < 0 {
		return errors.New("product compare price cannot be negative")
	}
	if r.CostPrice != nil && *r.CostPrice < 0 {
		return errors.New("product cost price cannot be negative")
	}
	if r.Sort != nil && *r.Sort < 0 {
		return errors.New("product sort cannot be negative")
	}
	if r.DeliveryType != nil {
		value := strings.ToLower(strings.TrimSpace(*r.DeliveryType))
		if value != "auto" && value != "manual" {
			return fmt.Errorf("unsupported delivery type %q", value)
		}
		r.DeliveryType = &value
	}
	if r.InventoryMode != nil {
		value := strings.ToLower(strings.TrimSpace(*r.InventoryMode))
		if value != "local" && value != "supplier" {
			return fmt.Errorf("unsupported inventory mode %q", value)
		}
		r.InventoryMode = &value
	}
	if r.Status != nil {
		value := strings.ToLower(strings.TrimSpace(*r.Status))
		if value != "draft" && value != "on_sale" && value != "off_sale" {
			return fmt.Errorf("unsupported product status %q", value)
		}
		r.Status = &value
	}
	return nil
}

func (r updateProductRequest) updates() map[string]any {
	updates := make(map[string]any, 12)
	if r.Name != nil {
		updates["name"] = *r.Name
	}
	if r.Summary != nil {
		updates["summary"] = *r.Summary
	}
	if r.Description != nil {
		updates["description"] = *r.Description
	}
	if r.Price != nil {
		updates["price"] = *r.Price
	}
	if r.ComparePrice != nil {
		updates["compare_price"] = *r.ComparePrice
	}
	if r.CostPrice != nil {
		updates["cost_price"] = *r.CostPrice
	}
	if r.DeliveryType != nil {
		updates["delivery_type"] = *r.DeliveryType
	}
	if r.InventoryMode != nil {
		updates["inventory_mode"] = *r.InventoryMode
	}
	if r.Status != nil {
		updates["status"] = *r.Status
	}
	if r.Featured != nil {
		updates["featured"] = *r.Featured
	}
	if r.Tags != nil {
		updates["tags"] = *r.Tags
	}
	if r.Sort != nil {
		updates["sort"] = *r.Sort
	}
	return updates
}

func openAPIImageURLs(coverURL string, mediaItems []catalogMediaDTO) []string {
	seen := make(map[string]struct{}, len(mediaItems)+1)
	result := make([]string, 0, len(mediaItems)+1)
	appendURL := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		if _, exists := seen[value]; exists {
			return
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	appendURL(coverURL)
	for _, item := range mediaItems {
		appendURL(item.URL)
	}
	return result
}

type openAPICatalogQuery struct {
	Page            int
	PageSize        int
	Paged           bool
	UpdatedAfter    *time.Time
	IncludeInactive bool
}

func parseOpenAPICatalogQuery(c *gin.Context) (openAPICatalogQuery, error) {
	result := openAPICatalogQuery{Page: 1, PageSize: 100}
	if raw := strings.TrimSpace(c.Query("page")); raw != "" {
		page, err := strconv.Atoi(raw)
		if err != nil || page < 1 || page > 10_000 {
			return result, errors.New("invalid page")
		}
		result.Page, result.Paged = page, true
	}
	if raw := strings.TrimSpace(c.Query("page_size")); raw != "" {
		pageSize, err := strconv.Atoi(raw)
		if err != nil || pageSize < 1 || pageSize > 500 {
			return result, errors.New("invalid page size")
		}
		result.PageSize, result.Paged = pageSize, true
	}
	if raw := strings.TrimSpace(c.Query("updated_after")); raw != "" {
		updatedAfter, err := time.Parse(time.RFC3339Nano, raw)
		if err != nil {
			return result, errors.New("invalid updated_after")
		}
		result.UpdatedAfter = &updatedAfter
	}
	if raw := strings.TrimSpace(c.Query("include_inactive")); raw != "" {
		includeInactive, err := strconv.ParseBool(raw)
		if err != nil {
			return result, errors.New("invalid include_inactive")
		}
		result.IncludeInactive = includeInactive
	}
	return result, nil
}

func setOpenAPICatalogHeaders(c *gin.Context, total int64, query openAPICatalogQuery, returned int) {
	c.Header("X-LinLinQi-Total-Count", strconv.FormatInt(total, 10))
	c.Header("X-LinLinQi-Page", strconv.Itoa(query.Page))
	c.Header("X-LinLinQi-Page-Size", strconv.Itoa(query.PageSize))
	hasMore := query.Paged && int64(query.Page*query.PageSize) < total
	c.Header("X-LinLinQi-Has-More", strconv.FormatBool(hasMore))
	c.Header("X-LinLinQi-Returned-Count", strconv.Itoa(returned))
}

func (h Handler) OpenAPICategories(c *gin.Context) {
	query, queryErr := parseOpenAPICatalogQuery(c)
	if queryErr != nil {
		response.Error(c, 422, 42230, "error.supply_catalog_query_invalid")
		return
	}
	categoryQuery := h.DB.Model(&model.Category{})
	if !query.IncludeInactive {
		categoryQuery = categoryQuery.Where("enabled = ?", true)
	}
	if query.UpdatedAfter != nil {
		categoryQuery = categoryQuery.Where("updated_at > ?", *query.UpdatedAfter)
	}
	var total int64
	if err := categoryQuery.Count(&total).Error; err != nil {
		response.Error(c, 500, 50020, "error.supply_catalog_fetch_failed")
		return
	}
	categoryQuery = categoryQuery.Preload("ImageAsset").Order("sort DESC, created_at ASC")
	if query.Paged {
		categoryQuery = categoryQuery.Offset((query.Page - 1) * query.PageSize).Limit(query.PageSize)
	}
	var categories []model.Category
	if err := categoryQuery.Find(&categories).Error; err != nil {
		response.Error(c, 500, 50020, "error.supply_catalog_fetch_failed")
		return
	}
	categoryIDs := make([]uuid.UUID, 0, len(categories))
	for _, category := range categories {
		categoryIDs = append(categoryIDs, category.ID)
	}
	mediaByCategory, err := catalogMediaForOwners(h.DB, "category", categoryIDs)
	if err != nil {
		response.Error(c, 500, 50020, "error.supply_catalog_fetch_failed")
		return
	}
	items := make([]openAPICategoryDTO, 0, len(categories))
	for _, category := range categories {
		imageURL := strings.TrimSpace(category.ImageURL)
		if category.ImageAsset != nil && strings.TrimSpace(category.ImageAsset.PublicURL) != "" {
			imageURL = category.ImageAsset.PublicURL
		}
		if mediaItems := mediaByCategory[category.ID]; imageURL == "" && len(mediaItems) > 0 {
			imageURL = mediaItems[0].URL
		}
		parentID := ""
		if category.ParentID != nil {
			parentID = category.ParentID.String()
		}
		status := "inactive"
		if category.Enabled {
			status = "active"
		}
		items = append(items, openAPICategoryDTO{
			ID: category.ID, ExternalID: category.ID.String(), ExternalParentID: parentID,
			Name: category.Name, Slug: category.Slug, Description: category.Description,
			Icon: category.Icon, ImageURL: imageURL, Sort: category.Sort, Status: "active",
			CreatedAt: category.CreatedAt, UpdatedAt: category.UpdatedAt,
		})
		items[len(items)-1].Status = status
	}
	setOpenAPICatalogHeaders(c, total, query, len(items))
	response.OK(c, items)
}

func (h Handler) OpenAPIProducts(c *gin.Context) {
	query, queryErr := parseOpenAPICatalogQuery(c)
	if queryErr != nil {
		response.Error(c, 422, 42230, "error.supply_catalog_query_invalid")
		return
	}
	currencyQuote, currencyErr := h.storefrontCurrency(c, c.Query("currency"))
	if currencyErr != nil {
		status, code, message := currencyRequestError(currencyErr)
		response.Error(c, status, code, message)
		return
	}
	productQuery := h.DB.Model(&model.Product{})
	if !query.IncludeInactive {
		productQuery = productQuery.Where("status = ?", "on_sale")
	} else {
		productQuery = productQuery.Where("status IN ?", []string{"on_sale", "off_sale"})
	}
	if query.UpdatedAfter != nil {
		productQuery = productQuery.Where("updated_at > ?", *query.UpdatedAfter)
	}
	var total int64
	if err := productQuery.Count(&total).Error; err != nil {
		response.Error(c, 500, 50020, "error.supply_catalog_fetch_failed")
		return
	}
	productQuery = productQuery.Preload("Category").Order("sort desc, created_at asc")
	if query.Paged {
		productQuery = productQuery.Offset((query.Page - 1) * query.PageSize).Limit(query.PageSize)
	}
	var products []model.Product
	if err := productQuery.Find(&products).Error; err != nil {
		response.Error(c, 500, 50020, "error.supply_catalog_fetch_failed")
		return
	}
	productIDs := make([]uuid.UUID, 0, len(products))
	for _, product := range products {
		productIDs = append(productIDs, product.ID)
	}
	mediaByProduct, mediaErr := catalogMediaForOwners(h.DB, "product", productIDs)
	if mediaErr != nil {
		response.Error(c, 500, 50020, "error.supply_catalog_fetch_failed")
		return
	}
	items := make([]openAPIProductDTO, 0, len(products))
	for _, product := range products {
		stock := h.productStock(product)
		variants := make([]model.ProductVariant, 0)
		h.DB.Where("product_id = ? AND status = ?", product.ID, "active").Order("sort DESC, created_at ASC").Find(&variants)
		publicVariants := make([]openAPIProductVariantDTO, 0, len(variants))
		for _, variant := range variants {
			minimum, maximum := openAPIProductVariantBounds(product, variant)
			price, conversionErr := currencyQuote.Conversion.Amount(variant.Price)
			if conversionErr != nil {
				response.Error(c, 503, 50366, "error.currency_rate_unavailable")
				return
			}
			comparePrice, conversionErr := currencyQuote.Conversion.Amount(variant.ComparePrice)
			if conversionErr != nil {
				response.Error(c, 503, 50366, "error.currency_rate_unavailable")
				return
			}
			attributes := json.RawMessage(variant.Attributes)
			if !json.Valid(attributes) {
				attributes = json.RawMessage(`{}`)
			}
			publicVariants = append(publicVariants, openAPIProductVariantDTO{
				ID: variant.ID, ExternalID: variant.ID.String(), ExternalSKU: variant.SKU,
				Name: variant.Name, Attributes: attributes, Price: price, ComparePrice: comparePrice,
				Stock: h.productStockForVariant(product, &variant.ID), Minimum: minimum,
				Maximum: maximum, PurchaseLimit: maximum, Status: "active",
			})
		}
		inputFields, err := h.publicProductInputFields(product.ID)
		if err != nil {
			response.Error(c, 500, 50020, "error.supply_catalog_fetch_failed")
			return
		}
		price, conversionErr := currencyQuote.Conversion.Amount(product.Price)
		if conversionErr != nil {
			response.Error(c, 503, 50366, "error.currency_rate_unavailable")
			return
		}
		comparePrice, conversionErr := currencyQuote.Conversion.Amount(product.ComparePrice)
		if conversionErr != nil {
			response.Error(c, 503, 50366, "error.currency_rate_unavailable")
			return
		}
		imageURLs := openAPIImageURLs(product.CoverURL, mediaByProduct[product.ID])
		coverURL := strings.TrimSpace(product.CoverURL)
		if coverURL == "" && len(imageURLs) > 0 {
			coverURL = imageURLs[0]
		}
		status := "inactive"
		if product.Status == "on_sale" {
			status = "active"
		}
		items = append(items, openAPIProductDTO{
			ID: product.ID, ExternalID: product.ID.String(), ExternalSKU: product.Slug,
			ExternalCategoryID: product.CategoryID.String(), CategoryID: product.CategoryID,
			Name: product.Name, Slug: product.Slug, Summary: product.Summary,
			Description: content.SanitizeRichHTML(product.Description), CoverURL: coverURL, ImageURLs: imageURLs,
			SourceCurrency: currencyQuote.Conversion.Source.Code, Currency: currencyQuote.Conversion.Target.Code,
			FX: currencyQuote.Conversion.FX(), Price: price, ComparePrice: comparePrice, Stock: stock,
			Minimum: max(product.MinimumPurchase, 1), Maximum: max(product.MaximumPurchase, 0), Status: status, Delivery: product.DeliveryType,
			DeliveryType: product.DeliveryType, InventoryMode: product.InventoryMode, Tags: product.Tags,
			CreatedAt: product.CreatedAt, UpdatedAt: product.UpdatedAt,
			Variants: publicVariants, InputFields: inputFields,
		})
	}
	setOpenAPICatalogHeaders(c, total, query, len(items))
	response.OK(c, items)
}

func (h Handler) OpenAPICreateOrder(c *gin.Context) {
	var req createOrderRequest
	if err := decodeStrictJSON(c, &req); err != nil {
		response.Error(c, 422, 42220, "error.order_fields_required")
		return
	}
	currencyQuote, currencyErr := h.storefrontCurrency(c, req.Currency)
	if currencyErr != nil {
		status, code, message := currencyRequestError(currencyErr)
		response.Error(c, status, code, message)
		return
	}
	external := strings.TrimSpace(req.ClientOrderNo)
	email := strings.ToLower(strings.TrimSpace(req.Email))
	paymentMethod := strings.ToLower(strings.TrimSpace(req.PaymentMethod))
	if paymentMethod == "" {
		paymentMethod = "supplier_balance"
	}
	productIdentifier := strings.TrimSpace(req.ProductID)
	if productIdentifier == "" {
		productIdentifier = strings.TrimSpace(req.ExternalProductID)
	}
	if !validOpenAPIIdentifier(external, 100) || req.Quantity < 1 || req.Quantity > 20 || !validOpenAPIEmail(email) || paymentMethod != "supplier_balance" {
		response.Error(c, 422, 42220, "error.order_fields_required")
		return
	}
	productID, variantID, err := resolveOpenAPIProduct(h.DB, productIdentifier, req.VariantID)
	if err != nil {
		response.Error(c, 422, 42202, "error.product_id_invalid")
		return
	}
	inputValues, err := parseSingleOrderInputValues(h.DB, productID, variantID, req.InputValues, req.Parameters)
	if err != nil {
		response.Error(c, 422, 42222, "error.product_input_values_invalid")
		return
	}
	callbackURL, err := normalizeOpenAPICallbackURL(c.Request.Context(), req.CallbackURL, h.Cfg.Env)
	if err != nil {
		response.Error(c, 422, 42221, "error.supplier_callback_url_invalid")
		return
	}
	credentialID, parseErr := uuid.Parse(c.GetString("api_credential_id"))
	if parseErr != nil {
		response.Error(c, 401, 40121, "error.invalid_api_credential")
		return
	}
	var credential model.APICredential
	if err := h.DB.First(&credential, "id = ?", credentialID).Error; err != nil {
		response.Error(c, 401, 40121, "error.invalid_api_credential")
		return
	}
	if credential.OwnerID == nil {
		response.Error(c, 403, 40321, "error.api_credential_no_billing_account")
		return
	}
	callbackSecret := ""
	if callbackURL != "" {
		callbackSecret, err = h.Vault.Decrypt(credential.SecretCipher, credential.SecretNonce, credential.ID[:])
		if err != nil {
			response.Error(c, 500, 50020, "error.supply_order_create_failed")
			return
		}
	}
	input := service.CreateOrderInput{ProductID: productID, VariantID: variantID, Quantity: req.Quantity, Email: email, PaymentMethod: paymentMethod, CouponCode: req.CouponCode, ClientIP: c.ClientIP(), ExternalOrderNo: &external, APICredentialID: &credentialID, InputValues: inputValues, Currency: currencyQuote.Conversion.Target.Code}
	if currencyQuote.Conversion.Snapshot != nil {
		id := currencyQuote.Conversion.Snapshot.ID
		input.FXSnapshotID = &id
	}
	pricingUserID, pricingResellerID, pricingContextErr := openAPICredentialPricingContext(credential)
	if pricingContextErr != nil {
		response.Error(c, 403, 40321, "error.api_credential_no_billing_account")
		return
	}
	input.UserID, input.ResellerID = pricingUserID, pricingResellerID
	var order *model.Order
	err = h.DB.Transaction(func(tx *gorm.DB) error {
		callbackEndpointID, callbackErr := h.ensureOpenAPICallbackEndpoint(tx, credential, callbackSecret, callbackURL)
		if callbackErr != nil {
			return callbackErr
		}
		input.CallbackEndpointID = callbackEndpointID
		var createErr error
		order, createErr = service.CreateOrder(tx, h.Vault, input)
		if createErr != nil {
			return createErr
		}
		ownerType := credential.OwnerType
		if ownerType == "" {
			ownerType = "user"
		}
		var wallet model.WalletAccount
		if err := tx.Where("owner_type = ? AND owner_id = ? AND currency = ?", ownerType, *credential.OwnerID, order.Currency).First(&wallet).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return service.ErrInsufficientBalance
			}
			return err
		}
		if _, walletErr := service.ApplyWalletMutation(tx, service.WalletMutation{EntryNo: "LQW-API-" + order.ID.String(), AccountID: wallet.ID, Amount: -order.Total, Type: "api_order", ReferenceType: "order", ReferenceID: &order.ID, Description: "OpenAPI 供货订单 " + order.OrderNo}); walletErr != nil {
			return walletErr
		}
		_, auditErr := service.EnsureWalletOrderPaymentAuditTx(tx, *order, time.Now().UTC())
		return auditErr
	})
	if errors.Is(err, service.ErrInsufficientStock) {
		response.Error(c, 409, 40901, "error.insufficient_stock")
		return
	}
	if errors.Is(err, service.ErrInsufficientBalance) {
		response.Error(c, 402, 40201, "error.insufficient_balance")
		return
	}
	if errors.Is(err, service.ErrOrderIdempotencyConflict) {
		response.Error(c, 409, 40921, "error.openapi_order_idempotency_conflict")
		return
	}
	if isOrderInputValidationError(err) {
		response.Error(c, 422, 42222, "error.product_input_values_invalid_or_required")
		return
	}
	if errors.Is(err, errOpenAPICallbackLimit) {
		response.Error(c, 409, 40922, "error.openapi_callback_limit_reached")
		return
	}
	if errors.Is(err, service.ErrVariantRequired) || errors.Is(err, service.ErrVariantUnavailable) {
		response.Error(c, 422, 42207, "error.valid_spec_and_quantity_required")
		return
	}
	if errors.Is(err, service.ErrCouponUnavailable) {
		response.Error(c, 422, 42208, "error.coupon_invalid_or_conditions_not_met")
		return
	}
	if err != nil {
		response.Error(c, 500, 50020, "error.supply_order_create_failed")
		return
	}
	_ = h.createOperationalNotifications(h.DB, "openapi.order.created", order.ID.String(), map[string]string{"order_no": order.OrderNo, "email": order.Email, "status": order.Status, "amount": strconv.FormatInt(order.Total, 10), "currency": order.Currency, "channel": "openapi", "summary": "OpenAPI 供货订单已创建"})
	result, err := h.toOpenAPISupplyOrderResult(*order)
	if err != nil {
		response.Error(c, 500, 50021, "error.delivery_content_decrypt_failed")
		return
	}
	if order.Status == "processing" {
		_ = h.enqueueSupplierOrder(order.ID)
	} else if order.Status == "delivered" {
		_ = h.dispatchOrderDelivery(order.ID)
	}
	response.Created(c, result)
}

func (h Handler) enqueueSupplierOrder(orderID uuid.UUID) error {
	client := queue.NewClient(h.Cfg, h.DB)
	defer client.Close()
	_, err := client.Enqueue(queue.TypeSupplierPurchase, map[string]string{"order_id": orderID.String()})
	return err
}

func (h Handler) dispatchOrderDelivery(orderID uuid.UUID) error {
	outbox, err := service.CreateDeliveryOutbox(h.DB, h.Vault, orderID, h.Cfg.UserAppURL)
	if err != nil {
		return err
	}
	client := queue.NewClient(h.Cfg, h.DB)
	defer client.Close()
	if outbox.NotificationID != nil {
		if _, err := client.Enqueue(queue.TypeNotificationDispatch, map[string]string{"delivery_id": outbox.NotificationID.String()}); err != nil {
			return err
		}
	}
	for _, deliveryID := range outbox.WebhookIDs {
		if _, err := client.Enqueue(queue.TypeWebhookDeliver, map[string]string{"delivery_id": deliveryID.String()}); err != nil {
			return err
		}
	}
	var order model.Order
	if h.DB.Select("id", "user_id", "order_no", "email", "status", "total", "currency").First(&order, "id = ?", orderID).Error == nil {
		values := map[string]string{"order_no": order.OrderNo, "email": order.Email, "status": order.Status, "amount": strconv.FormatInt(order.Total, 10), "currency": order.Currency, "summary": "订单已完成自动交付"}
		if order.UserID != nil {
			values["user_id"] = order.UserID.String()
		}
		_ = h.createOperationalNotifications(h.DB, "order.delivered", order.ID.String(), values)
	}
	type lowStockItem struct {
		ProductID   uuid.UUID
		ProductName string
		Stock       int64
	}
	var lowStock []lowStockItem
	h.DB.Table("order_items oi").Select("oi.product_id, p.name AS product_name, COUNT(c.id) AS stock").
		Joins("JOIN products p ON p.id = oi.product_id AND p.deleted_at IS NULL AND p.inventory_mode = 'local'").
		Joins("LEFT JOIN cards c ON c.product_id = oi.product_id AND c.status = 'available' AND c.deleted_at IS NULL").
		Where("oi.order_id = ? AND oi.deleted_at IS NULL", orderID).Group("oi.product_id, p.name").Having("COUNT(c.id) <= 10").Scan(&lowStock)
	for _, item := range lowStock {
		code := "inventory.low_stock"
		if item.Stock == 0 {
			code = "inventory.out_of_stock"
		}
		_ = h.createOperationalNotifications(h.DB, code, item.ProductID.String()+":"+time.Now().UTC().Format("2006010215"), map[string]string{"product_name": item.ProductName, "stock": strconv.FormatInt(item.Stock, 10), "status": "warning", "summary": "商品库存已低于预警线"})
	}
	return nil
}

func (h Handler) OpenAPIOrder(c *gin.Context) {
	credentialID, err := uuid.Parse(c.GetString("api_credential_id"))
	if err != nil {
		response.Error(c, 401, 40121, "error.invalid_api_credential")
		return
	}
	var order model.Order
	if err := h.DB.Preload("Items").Where("order_no = ? AND api_credential_id = ?", c.Param("order_no"), credentialID).First(&order).Error; err != nil {
		response.Error(c, 404, 40420, "error.order_not_found")
		return
	}
	result, err := h.toOpenAPISupplyOrderResult(order)
	if err != nil {
		response.Error(c, 500, 50021, "error.delivery_content_decrypt_failed")
		return
	}
	response.OK(c, result)
}

func (h Handler) revealOrder(order *model.Order) error {
	if (order.Status != "delivered" && order.Status != "completed") || order.PaymentStatus != "paid" {
		return nil
	}
	for i := range order.Items {
		if len(order.Items[i].CardCiphertext) == 0 {
			continue
		}
		content, err := h.Vault.Decrypt(order.Items[i].CardCiphertext, order.Items[i].CardNonce, order.Items[i].ProductID[:])
		if err != nil {
			return err
		}
		order.Items[i].CardContent = content
	}
	return nil
}

func pagination(c *gin.Context) (int, int) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if page < 1 {
		page = 1
	}
	// Bound OFFSET work and prevent integer overflow in (page-1)*pageSize.
	// Large datasets provide dedicated exports instead of unbounded scans.
	if page > 10000 {
		page = 10000
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if pageSize > 100 {
		pageSize = 100
	}
	return page, pageSize
}
