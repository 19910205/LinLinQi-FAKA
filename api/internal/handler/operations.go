package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"linlinqi/api/internal/content"
	"linlinqi/api/internal/model"
	"linlinqi/api/internal/service"
	"linlinqi/api/pkg/response"
)

var errStorefrontCartMismatch = errors.New("cart belongs to another storefront")
var errCartOwnerMismatch = errors.New("cart belongs to another user")

func cartAccessibleToUser(cartUserID, requestUserID *uuid.UUID) bool {
	return cartUserID == nil || (requestUserID != nil && *cartUserID == *requestUserID)
}

type publicContentPostDTO struct {
	ID          uuid.UUID  `json:"id"`
	Title       string     `json:"title"`
	Slug        string     `json:"slug"`
	Summary     string     `json:"summary"`
	Content     string     `json:"content"`
	CoverURL    string     `json:"cover_url"`
	PublishedAt *time.Time `json:"published_at"`
}

type publicContentBannerDTO struct {
	ID        uuid.UUID `json:"id"`
	Title     string    `json:"title"`
	ImageURL  string    `json:"image_url"`
	TargetURL string    `json:"target_url"`
	Placement string    `json:"placement"`
}

type publicContentAnnouncementDTO struct {
	ID        uuid.UUID `json:"id"`
	Title     string    `json:"title"`
	Content   string    `json:"content"`
	Level     string    `json:"level"`
	CreatedAt time.Time `json:"created_at"`
}

func toPublicContentPostDTO(post model.Post) publicContentPostDTO {
	return publicContentPostDTO{ID: post.ID, Title: post.Title, Slug: post.Slug, Summary: post.Summary, Content: content.SanitizeRichHTML(post.Content), CoverURL: post.CoverURL, PublishedAt: post.PublishedAt}
}

func toPublicContentBannerDTO(banner model.Banner) publicContentBannerDTO {
	return publicContentBannerDTO{ID: banner.ID, Title: banner.Title, ImageURL: banner.ImageURL, TargetURL: banner.TargetURL, Placement: banner.Placement}
}

func toPublicContentAnnouncementDTO(announcement model.Announcement) publicContentAnnouncementDTO {
	return publicContentAnnouncementDTO{ID: announcement.ID, Title: announcement.Title, Content: content.SanitizeRichHTML(announcement.Content), Level: announcement.Level, CreatedAt: announcement.CreatedAt}
}

func (h Handler) PublicContent(c *gin.Context) {
	var banners []model.Banner
	var posts []model.Post
	var announcements []model.Announcement
	now := time.Now()
	if err := h.DB.Where("enabled = ? AND (starts_at IS NULL OR starts_at <= ?) AND (ends_at IS NULL OR ends_at >= ?)", true, now, now).Order("sort DESC").Find(&banners).Error; err != nil {
		response.Error(c, 500, 50044, "error.public_content_fetch_failed")
		return
	}
	if err := h.DB.Where("status = ? AND published_at IS NOT NULL AND published_at <= ?", "published", now).Order("published_at DESC").Limit(12).Find(&posts).Error; err != nil {
		response.Error(c, 500, 50044, "error.public_content_fetch_failed")
		return
	}
	if err := h.DB.Where("enabled = ?", true).Order("sort DESC").Find(&announcements).Error; err != nil {
		response.Error(c, 500, 50044, "error.public_content_fetch_failed")
		return
	}
	publicBanners := make([]publicContentBannerDTO, 0, len(banners))
	for _, banner := range banners {
		publicBanners = append(publicBanners, toPublicContentBannerDTO(banner))
	}
	publicPosts := make([]publicContentPostDTO, 0, len(posts))
	for _, post := range posts {
		publicPosts = append(publicPosts, toPublicContentPostDTO(post))
	}
	publicAnnouncements := make([]publicContentAnnouncementDTO, 0, len(announcements))
	for _, announcement := range announcements {
		publicAnnouncements = append(publicAnnouncements, toPublicContentAnnouncementDTO(announcement))
	}
	response.OK(c, gin.H{"banners": publicBanners, "posts": publicPosts, "announcements": publicAnnouncements})
}

func (h Handler) PublicPost(c *gin.Context) {
	var post model.Post
	if err := h.DB.Where("slug = ? AND status = ? AND published_at IS NOT NULL AND published_at <= ?", c.Param("slug"), "published", time.Now()).First(&post).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		response.Error(c, 404, 40440, "error.content_not_found")
		return
	} else if err != nil {
		response.Error(c, 500, 50044, "error.public_content_fetch_failed")
		return
	}
	response.OK(c, toPublicContentPostDTO(post))
}

func (h Handler) GetCart(c *gin.Context) {
	token := strings.TrimSpace(c.Param("token"))
	if len(token) < 16 || len(token) > 100 {
		response.Error(c, 422, 42240, "error.cart_token_invalid")
		return
	}
	storefront, storefrontErr := h.resolveStorefront(c)
	if storefrontErr != nil {
		response.Error(c, 500, 50040, "error.shop_cart_fetch_failed")
		return
	}
	resellerID := storefrontResellerID(storefront)
	var cart model.Cart
	if err := h.DB.Preload("Items").Where("guest_token = ? AND expires_at > ?", token, time.Now()).First(&cart).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		currencyQuote, currencyErr := h.storefrontCurrency(c, c.Query("currency"))
		if currencyErr != nil {
			status, code, message := currencyRequestError(currencyErr)
			response.Error(c, status, code, message)
			return
		}
		response.OK(c, gin.H{"guest_token": token, "items": []any{}, "currency": currencyQuote.Conversion.Target.Code, "fx": currencyQuote.Conversion.FX()})
		return
	} else if err != nil {
		response.Error(c, 500, 50040, "error.cart_fetch_failed")
		return
	}
	if !sameStorefront(cart.ResellerID, resellerID) {
		response.Error(c, 409, 40941, "error.cart_belongs_to_other_shop_reselect")
		return
	}
	userID := optionalUserID(c)
	if !cartAccessibleToUser(cart.UserID, userID) {
		response.Error(c, 404, 40440, "error.cart_empty_or_expired")
		return
	}
	currencyQuote, currencyErr := h.storefrontCurrency(c, cart.Currency)
	if currencyErr != nil {
		status, code, message := currencyRequestError(currencyErr)
		response.Error(c, status, code, message)
		return
	}
	items := make([]gin.H, 0, len(cart.Items))
	for _, cartItem := range cart.Items {
		var product model.Product
		if err := h.DB.Preload("Category").First(&product, "id = ?", cartItem.ProductID).Error; err != nil {
			items = append(items, gin.H{"id": cartItem.ID, "product_id": cartItem.ProductID, "variant_id": cartItem.VariantID, "quantity": cartItem.Quantity, "available": false})
			continue
		}
		productDTO := toPublicProductDTO(product)
		productDTO, conversionErr := convertPublicProductDTO(productDTO, currencyQuote.Conversion)
		if conversionErr != nil {
			response.Error(c, 503, 50366, "error.currency_rate_unavailable")
			return
		}
		inputFields, inputFieldErr := h.publicProductInputFields(product.ID)
		if inputFieldErr != nil {
			response.Error(c, 500, 50040, "error.product_input_fields_fetch_failed")
			return
		}
		entry := gin.H{"id": cartItem.ID, "product_id": cartItem.ProductID, "variant_id": cartItem.VariantID, "quantity": cartItem.Quantity, "product": productDTO, "input_fields": inputFields, "available": false}
		line, err := service.ResolveLinePricingForReseller(h.DB, product.ID, cartItem.VariantID, userID, resellerID, cartItem.Quantity)
		if err == nil {
			convertedQuote, conversionErr := currencyQuote.Conversion.PriceQuote(line.Quote)
			if conversionErr != nil {
				response.Error(c, 503, 50366, "error.currency_rate_unavailable")
				return
			}
			productDTO.Price = convertedQuote.UnitPrice
			if productDTO.ComparePrice < productDTO.Price {
				productDTO.ComparePrice = productDTO.Price
			}
			entry["product"] = productDTO
			stock := h.productStockForVariant(product, cartItem.VariantID)
			entry["stock"] = stock
			entry["quote"] = convertedQuote
			entry["available"] = stock >= int64(cartItem.Quantity)
			if cartItem.VariantID != nil {
				var variant model.ProductVariant
				if h.DB.First(&variant, "id = ? AND product_id = ?", *cartItem.VariantID, product.ID).Error == nil {
					variantDTO := toPublicProductVariantDTO(variant, stock)
					variantDTO, conversionErr = convertPublicVariantDTO(variantDTO, currencyQuote.Conversion)
					if conversionErr != nil {
						response.Error(c, 503, 50366, "error.currency_rate_unavailable")
						return
					}
					variantDTO.Price = convertedQuote.UnitPrice
					if variantDTO.ComparePrice < variantDTO.Price {
						variantDTO.ComparePrice = variantDTO.Price
					}
					entry["variant"] = variantDTO
				}
			}
		}
		items = append(items, entry)
	}
	response.OK(c, gin.H{"id": cart.ID, "guest_token": cart.GuestToken, "currency": cart.Currency, "fx": currencyQuote.Conversion.FX(), "expires_at": cart.ExpiresAt, "items": items})
}

type cartItemRequest struct {
	ProductID string `json:"product_id"`
	VariantID string `json:"variant_id"`
	Quantity  int    `json:"quantity"`
	Currency  string `json:"currency"`
}

type checkoutQuoteLineRequest struct {
	ProductID string `json:"product_id"`
	VariantID string `json:"variant_id"`
	Quantity  int    `json:"quantity"`
}

type checkoutQuoteRequest struct {
	Lines         []checkoutQuoteLineRequest `json:"lines"`
	Contact       string                     `json:"contact"`
	Email         string                     `json:"email"`
	CouponCode    string                     `json:"coupon_code"`
	PaymentMethod string                     `json:"payment_method"`
	Currency      string                     `json:"currency"`
}

func (h Handler) CheckoutQuote(c *gin.Context) {
	var req checkoutQuoteRequest
	if decodeStrictJSON(c, &req) != nil || len(req.Lines) == 0 || strings.TrimSpace(req.PaymentMethod) == "" {
		response.Error(c, 422, 42244, "error.quote_items_and_payment_required")
		return
	}
	currencyQuote, currencyErr := h.storefrontCurrency(c, req.Currency)
	if currencyErr != nil {
		status, code, message := currencyRequestError(currencyErr)
		response.Error(c, status, code, message)
		return
	}
	userID := optionalUserID(c)
	storefront, storefrontErr := h.resolveStorefront(c)
	if storefrontErr != nil {
		response.Error(c, 500, 50040, "error.shop_quote_config_fetch_failed")
		return
	}
	resellerID := storefrontResellerID(storefront)
	email := req.Contact
	if strings.TrimSpace(email) == "" {
		email = req.Email
	}
	if userID != nil {
		var account model.User
		if err := h.DB.Select("email").First(&account, "id = ? AND status = ?", *userID, "active").Error; err != nil {
			response.Error(c, 401, 40140, "error.invalid_login_state")
			return
		}
		email = account.Email
	}
	var validContact bool
	email, validContact = normalizeCheckoutContact(email)
	if !validContact {
		response.Error(c, 422, 42245, "error.order_parameters_incomplete")
		return
	}
	if strings.TrimSpace(req.CouponCode) != "" && userID == nil {
		if !isCheckoutEmail(email) {
			response.Error(c, 422, 42245, "error.guest_email_required_for_coupon")
			return
		}
	}
	var channel model.PaymentChannel
	if strings.TrimSpace(req.PaymentMethod) != "balance" {
		if err := h.DB.Where("code = ? AND enabled = ?", strings.TrimSpace(req.PaymentMethod), true).First(&channel).Error; err != nil {
			response.Error(c, 422, 42204, "error.payment_channel_unavailable")
			return
		}
		if !paymentChannelSupportsCurrency(channel, currencyQuote.Conversion.Target.Code) {
			response.Error(c, 422, 42299, "error.payment_channel_currency_unsupported")
			return
		}
	}
	lines := make([]service.CheckoutLineInput, 0, len(req.Lines))
	for _, item := range req.Lines {
		productID, err := uuid.Parse(strings.TrimSpace(item.ProductID))
		if err != nil || item.Quantity < 1 || item.Quantity > 20 {
			response.Error(c, 422, 42244, "error.quote_item_invalid")
			return
		}
		var variantID *uuid.UUID
		if strings.TrimSpace(item.VariantID) != "" {
			parsed, parseErr := uuid.Parse(strings.TrimSpace(item.VariantID))
			if parseErr != nil {
				response.Error(c, 422, 42243, "error.spec_id_invalid")
				return
			}
			variantID = &parsed
		}
		lines = append(lines, service.CheckoutLineInput{ProductID: productID, VariantID: variantID, Quantity: item.Quantity})
	}
	var quote service.CheckoutQuote
	err := h.DB.Transaction(func(tx *gorm.DB) error {
		productIDs := make([]uuid.UUID, 0, len(lines))
		for _, line := range lines {
			productIDs = append(productIDs, line.ProductID)
		}
		if channel.ID != uuid.Nil {
			if err := service.EnsurePaymentChannelAllowedCurrency(tx, channel.ID, productIDs, currencyQuote.Conversion.Target.Code); err != nil {
				return err
			}
		}
		var quoteErr error
		quote, quoteErr = service.ResolveCheckoutQuoteForReseller(tx, userID, resellerID, email, req.CouponCode, channel.FeeRate, lines)
		return quoteErr
	})
	if errors.Is(err, service.ErrPaymentChannelNotAllowed) {
		response.Error(c, 422, 42205, "error.some_products_channel_unsupported")
		return
	}
	if errors.Is(err, service.ErrVariantRequired) || errors.Is(err, service.ErrVariantUnavailable) || errors.Is(err, service.ErrResellerProductUnavailable) {
		response.Error(c, 422, 42243, "error.valid_spec_and_quantity_required")
		return
	}
	if errors.Is(err, service.ErrCouponUnavailable) {
		response.Error(c, 422, 42251, "error.coupon_invalid_or_conditions_not_met")
		return
	}
	if err != nil {
		response.Error(c, 422, 42244, "error.quote_generation_failed")
		return
	}
	quote, err = service.ConvertCheckoutQuote(quote, currencyQuote.Conversion)
	if err != nil {
		response.Error(c, 503, 50366, "error.currency_rate_unavailable")
		return
	}
	for index := range quote.Lines {
		var product model.Product
		if h.DB.Select("id", "inventory_mode").First(&product, "id = ?", quote.Lines[index].ProductID).Error != nil {
			continue
		}
		quote.Lines[index].Stock = h.productStockForVariant(product, quote.Lines[index].VariantID)
		quote.Lines[index].Available = quote.Lines[index].Stock >= int64(quote.Lines[index].Quote.Quantity)
	}
	response.OK(c, quote)
}

func (h Handler) UpsertCartItem(c *gin.Context) {
	token := strings.TrimSpace(c.Param("token"))
	if len(token) < 16 || len(token) > 100 {
		response.Error(c, 422, 42240, "error.cart_token_invalid")
		return
	}
	var req cartItemRequest
	if decodeStrictJSON(c, &req) != nil || strings.TrimSpace(req.ProductID) == "" || req.Quantity < 1 || req.Quantity > 20 {
		response.Error(c, 422, 42241, "error.cart_item_parameters_invalid")
		return
	}
	currencyQuote, currencyErr := h.storefrontCurrency(c, req.Currency)
	if currencyErr != nil {
		status, code, message := currencyRequestError(currencyErr)
		response.Error(c, status, code, message)
		return
	}
	productID, err := uuid.Parse(req.ProductID)
	if err != nil {
		response.Error(c, 422, 42242, "error.product_id_invalid")
		return
	}
	var variantID *uuid.UUID
	if strings.TrimSpace(req.VariantID) != "" {
		parsed, parseErr := uuid.Parse(req.VariantID)
		if parseErr != nil {
			response.Error(c, 422, 42243, "error.spec_id_invalid")
			return
		}
		variantID = &parsed
	}
	userID := optionalUserID(c)
	storefront, storefrontErr := h.resolveStorefront(c)
	if storefrontErr != nil {
		response.Error(c, 500, 50040, "error.shop_cart_config_fetch_failed")
		return
	}
	resellerID := storefrontResellerID(storefront)
	err = h.DB.Transaction(func(tx *gorm.DB) error {
		resolved, err := service.ResolveLinePricingForReseller(tx, productID, variantID, userID, resellerID, req.Quantity)
		if err != nil {
			return err
		}
		productCurrency := strings.ToUpper(strings.TrimSpace(resolved.Product.Currency))
		if len(productCurrency) != 3 {
			return service.ErrCurrencyMismatch
		}
		if productCurrency != currencyQuote.Conversion.Source.Code {
			return service.ErrCurrencyMismatch
		}
		cartCurrency := currencyQuote.Conversion.Target.Code
		var cart model.Cart
		if err := tx.Where("guest_token = ?", token).First(&cart).Error; err != nil {
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			cart = model.Cart{UserID: userID, ResellerID: resellerID, GuestToken: token, Currency: cartCurrency, ExpiresAt: time.Now().Add(14 * 24 * time.Hour)}
			if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "guest_token"}}, DoNothing: true}).Create(&cart).Error; err != nil {
				return err
			}
			if err := tx.Where("guest_token = ?", token).First(&cart).Error; err != nil {
				return err
			}
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&cart, "id = ?", cart.ID).Error; err != nil {
			return err
		}
		if !sameStorefront(cart.ResellerID, resellerID) {
			return errStorefrontCartMismatch
		}
		if !cartAccessibleToUser(cart.UserID, userID) {
			return errCartOwnerMismatch
		}
		if !strings.EqualFold(cart.Currency, cartCurrency) {
			return service.ErrCurrencyMismatch
		}
		expiresAt := time.Now().Add(14 * 24 * time.Hour)
		updates := map[string]any{"expires_at": expiresAt}
		if userID != nil {
			updates["user_id"] = *userID
		}
		if err := tx.Model(&cart).Updates(updates).Error; err != nil {
			return err
		}
		var item model.CartItem
		query := tx.Where("cart_id = ? AND product_id = ?", cart.ID, productID)
		if variantID == nil {
			query = query.Where("variant_id IS NULL")
		} else {
			query = query.Where("variant_id = ?", *variantID)
		}
		var otherQuantity int64
		otherQuery := tx.Model(&model.CartItem{}).Where("cart_id = ?", cart.ID)
		if variantID == nil {
			otherQuery = otherQuery.Where("NOT (product_id = ? AND variant_id IS NULL)", productID)
		} else {
			otherQuery = otherQuery.Where("product_id <> ? OR variant_id IS DISTINCT FROM ?", productID, *variantID)
		}
		if err := otherQuery.Select("COALESCE(SUM(quantity), 0)").Scan(&otherQuantity).Error; err != nil {
			return err
		}
		if otherQuantity+int64(req.Quantity) > 20 {
			return fmt.Errorf("cart quantity limit exceeded")
		}
		if err := query.First(&item).Error; err == nil {
			return tx.Model(&item).Updates(map[string]any{"quantity": req.Quantity, "selected_card_ids": "[]"}).Error
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		return tx.Create(&model.CartItem{CartID: cart.ID, ProductID: productID, VariantID: variantID, Quantity: req.Quantity, SelectedCardIDs: "[]"}).Error
	})
	if err != nil {
		if errors.Is(err, errCartOwnerMismatch) {
			response.Error(c, 404, 40440, "error.cart_empty_or_expired")
			return
		}
		if errors.Is(err, errStorefrontCartMismatch) {
			response.Error(c, 409, 40941, "error.cart_belongs_to_other_shop_reselect")
			return
		}
		if errors.Is(err, service.ErrCurrencyMismatch) {
			response.Error(c, 409, 40942, "error.cart_currency_changed_clear_and_reselect")
			return
		}
		if errors.Is(err, service.ErrVariantRequired) || errors.Is(err, service.ErrVariantUnavailable) || errors.Is(err, service.ErrResellerProductUnavailable) {
			response.Error(c, 422, 42243, "error.valid_spec_and_quantity_required")
			return
		}
		response.Error(c, 409, 40940, "error.product_unpurchasable_or_cart_update_failed")
		return
	}
	h.GetCart(c)
}

func (h Handler) DeleteCartItem(c *gin.Context) {
	token := strings.TrimSpace(c.Param("token"))
	if len(token) < 16 || len(token) > 100 {
		response.Error(c, 422, 42240, "error.cart_token_invalid")
		return
	}
	productID, err := uuid.Parse(c.Param("product_id"))
	if err != nil {
		response.Error(c, 422, 42242, "error.product_id_invalid")
		return
	}
	storefront, storefrontErr := h.resolveStorefront(c)
	if storefrontErr != nil {
		response.Error(c, 500, 50042, "error.shop_cart_config_fetch_failed")
		return
	}
	resellerID := storefrontResellerID(storefront)
	requestUserID := optionalUserID(c)
	cartIDs := h.DB.Model(&model.Cart{}).Select("id").Where("guest_token = ? AND expires_at > ?", token, time.Now())
	if requestUserID == nil {
		cartIDs = cartIDs.Where("user_id IS NULL")
	} else {
		cartIDs = cartIDs.Where("user_id IS NULL OR user_id = ?", *requestUserID)
	}
	if resellerID == nil {
		cartIDs = cartIDs.Where("reseller_id IS NULL")
	} else {
		cartIDs = cartIDs.Where("reseller_id = ?", *resellerID)
	}
	query := h.DB.Where("cart_id IN (?) AND product_id = ?", cartIDs, productID)
	if rawVariantID := strings.TrimSpace(c.Query("variant_id")); rawVariantID != "" {
		variantID, parseErr := uuid.Parse(rawVariantID)
		if parseErr != nil {
			response.Error(c, 422, 42243, "error.spec_id_invalid")
			return
		}
		query = query.Where("variant_id = ?", variantID)
	} else {
		query = query.Where("variant_id IS NULL")
	}
	result := query.Delete(&model.CartItem{})
	if result.Error != nil {
		response.Error(c, 500, 50042, "error.cart_item_remove_failed")
		return
	}
	response.OK(c, gin.H{"removed": result.RowsAffected})
}

func optionalUserID(c *gin.Context) *uuid.UUID {
	userID, err := uuid.Parse(c.GetString("subject"))
	if err != nil {
		return nil
	}
	return &userID
}

type cartOrderRequest struct {
	GuestToken    string                      `json:"guest_token" binding:"required"`
	Contact       string                      `json:"contact"`
	Email         string                      `json:"email"`
	PaymentMethod string                      `json:"payment_method" binding:"required"`
	CouponCode    string                      `json:"coupon_code"`
	InputValues   []checkoutInputValueRequest `json:"input_values"`
	Currency      string                      `json:"currency"`
}

func (h Handler) CreateCartOrder(c *gin.Context) {
	var req cartOrderRequest
	decodeErr := decodeStrictJSON(c, &req)
	cartToken := strings.TrimSpace(req.GuestToken)
	if decodeErr != nil || len(cartToken) < 16 || len(cartToken) > 100 || (optionalUserID(c) == nil && strings.TrimSpace(req.Contact) == "" && strings.TrimSpace(req.Email) == "") || strings.TrimSpace(req.PaymentMethod) == "" {
		response.Error(c, 422, 42248, "error.cart_checkout_incomplete")
		return
	}
	email := req.Contact
	if strings.TrimSpace(email) == "" {
		email = req.Email
	}
	if userID := optionalUserID(c); userID != nil {
		var account model.User
		if err := h.DB.Select("email").First(&account, "id = ? AND status = ?", *userID, "active").Error; err != nil {
			response.Error(c, 401, 40140, "error.invalid_login_state")
			return
		}
		email = account.Email
	}
	var validContact bool
	email, validContact = normalizeCheckoutContact(email)
	if !validContact {
		response.Error(c, 422, 42248, "error.order_parameters_incomplete")
		return
	}
	inputValues, inputErr := parseCartOrderInputValues(req.InputValues)
	if inputErr != nil {
		response.Error(c, 422, 42252, "error.product_input_values_invalid")
		return
	}
	var cart model.Cart
	if err := h.DB.Preload("Items").Where("guest_token = ? AND expires_at > ?", cartToken, time.Now()).First(&cart).Error; err != nil || len(cart.Items) == 0 {
		response.Error(c, 422, 42249, "error.cart_empty_or_expired")
		return
	}
	requestUserID := optionalUserID(c)
	if !cartAccessibleToUser(cart.UserID, requestUserID) {
		response.Error(c, 422, 42249, "error.cart_empty_or_expired")
		return
	}
	storefront, storefrontErr := h.resolveStorefront(c)
	if storefrontErr != nil {
		response.Error(c, 500, 50003, "error.shop_settlement_config_fetch_failed")
		return
	}
	resellerID := storefrontResellerID(storefront)
	if !sameStorefront(cart.ResellerID, resellerID) {
		response.Error(c, 409, 40941, "error.cart_belongs_to_other_shop_checkout")
		return
	}
	if strings.TrimSpace(req.Currency) == "" || !strings.EqualFold(cart.Currency, req.Currency) {
		response.Error(c, 409, 40942, "error.cart_currency_changed_clear_and_reselect")
		return
	}
	currencyQuote, currencyErr := h.storefrontCurrency(c, cart.Currency)
	if currencyErr != nil {
		status, code, message := currencyRequestError(currencyErr)
		response.Error(c, status, code, message)
		return
	}
	lines := make([]service.OrderLine, 0, len(cart.Items))
	for _, item := range cart.Items {
		lines = append(lines, service.OrderLine{ProductID: item.ProductID, VariantID: item.VariantID, Quantity: item.Quantity})
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
	}
	userID := requestUserID
	quoteLines := make([]service.CheckoutLineInput, 0, len(lines))
	for _, line := range lines {
		quoteLines = append(quoteLines, service.CheckoutLineInput{ProductID: line.ProductID, VariantID: line.VariantID, Quantity: line.Quantity})
	}
	var riskDecisionID *uuid.UUID
	var riskQuote service.CheckoutQuote
	quoteErr := h.DB.Transaction(func(tx *gorm.DB) error {
		var resolveErr error
		riskQuote, resolveErr = service.ResolveCheckoutQuoteForReseller(tx, userID, resellerID, email, req.CouponCode, channel.FeeRate, quoteLines)
		return resolveErr
	})
	if quoteErr == nil {
		decisionID, allowed := h.authorizeCheckout(c, userID, email, riskQuote.Total)
		if !allowed {
			return
		}
		riskDecisionID = &decisionID
	}
	var fxSnapshotID *uuid.UUID
	orderCurrency := currencyQuote.Conversion.Source.Code
	storefrontRequest, err := storefrontOrderIdempotency(c, email, "", struct {
		GuestToken    string                        `json:"guest_token"`
		Email         string                        `json:"email"`
		PaymentMethod string                        `json:"payment_method"`
		CouponCode    string                        `json:"coupon_code"`
		Currency      string                        `json:"currency"`
		Lines         []service.OrderLine           `json:"lines"`
		InputValues   []service.SubmittedInputValue `json:"input_values"`
	}{cartToken, email, strings.TrimSpace(req.PaymentMethod), strings.TrimSpace(req.CouponCode), orderCurrency, lines, inputValues})
	if err != nil {
		response.Error(c, 422, 42299, "error.idempotency_key_required_or_invalid")
		return
	}
	order, err := service.CreatePendingCartOrder(h.DB, h.Vault, userID, resellerID, email, req.PaymentMethod, c.ClientIP(), req.CouponCode, orderCurrency, fxSnapshotID, channel.FeeRate, channel.ID, riskDecisionID, lines, inputValues, storefrontRequest)
	if errors.Is(err, service.ErrPaymentChannelNotAllowed) {
		response.Error(c, 422, 42205, "error.some_products_channel_unsupported")
		return
	}
	if errors.Is(err, service.ErrInsufficientStock) {
		response.Error(c, 409, 40901, "error.partial_insufficient_stock")
		return
	}
	if errors.Is(err, service.ErrPendingOrderLimit) {
		response.Error(c, 429, 42901, "error.unpaid_order_quota_exceeded")
		return
	}
	if errors.Is(err, service.ErrVariantRequired) || errors.Is(err, service.ErrVariantUnavailable) || errors.Is(err, service.ErrResellerProductUnavailable) {
		response.Error(c, 422, 42250, "error.cart_spec_limit_exceeded")
		return
	}
	if errors.Is(err, service.ErrCouponUnavailable) {
		response.Error(c, 422, 42251, "error.coupon_invalid_or_conditions_not_met")
		return
	}
	if errors.Is(err, service.ErrOrderIdempotencyConflict) {
		response.Error(c, 409, 40910, "error.order_idempotency_conflict")
		return
	}
	if isOrderInputValidationError(err) {
		response.Error(c, 422, 42252, "error.product_input_values_invalid_or_required")
		return
	}
	if err != nil {
		response.Error(c, 500, 50003, "error.cart_order_create_failed")
		return
	}
	if h.Cfg.Env != "production" && req.PaymentMethod == "sandbox" {
		if err := service.FulfillOrder(h.DB, order.ID); err != nil {
			response.Error(c, 500, 50004, "error.sandbox_delivery_failed")
			return
		}
		h.DB.Preload("Items").First(order, "id = ?", order.ID)
		_ = h.revealOrder(order)
	}
	if order.Status == "processing" {
		_ = h.enqueueSupplierOrder(order.ID)
	} else if order.Status == "delivered" {
		_ = h.dispatchOrderDelivery(order.ID)
	}
	h.DB.Where("cart_id = ?", cart.ID).Delete(&model.CartItem{})
	h.DB.Delete(&cart)
	response.Created(c, toPublicOrderDTO(*order))
}

func (h Handler) Me(c *gin.Context) {
	userID, err := uuid.Parse(c.GetString("subject"))
	if err != nil {
		response.Error(c, 401, 40140, "error.invalid_login_state")
		return
	}
	var user model.User
	if err := h.DB.First(&user, "id = ?", userID).Error; err != nil {
		response.Error(c, 404, 40441, "error.user_not_found")
		return
	}
	var memberLevel any
	membership, _, reconcileErr := service.ReconcileUserMembership(h.DB, userID, time.Now().UTC())
	if reconcileErr != nil {
		response.Error(c, 500, 50056, "error.member_level_list_fetch_failed")
		return
	}
	if membership != nil {
		var level model.MemberLevel
		if err := h.DB.First(&level, "id = ?", membership.MemberLevelID).Error; err != nil && err != gorm.ErrRecordNotFound {
			response.Error(c, 500, 50056, "error.member_level_list_fetch_failed")
			return
		} else if err == nil {
			memberLevel = toUserMemberLevelDTO(level)
		}
	}
	response.OK(c, gin.H{"user": toUserAccountDTO(user), "member_level": memberLevel})
}

func (h Handler) MyOrders(c *gin.Context) {
	userID, err := uuid.Parse(c.GetString("subject"))
	if err != nil {
		response.Error(c, 401, 40140, "error.invalid_login_state")
		return
	}
	page, pageSize := pagination(c)
	var total int64
	if err := h.DB.Model(&model.Order{}).Where("user_id = ?", userID).Count(&total).Error; err != nil {
		response.Error(c, 500, 50056, "error.order_fetch_failed")
		return
	}
	var orders []model.Order
	if err := h.DB.Preload("Items").Where("user_id = ?", userID).Order("created_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&orders).Error; err != nil {
		response.Error(c, 500, 50056, "error.order_fetch_failed")
		return
	}
	for index := range orders {
		if (orders[index].Status == "delivered" || orders[index].Status == "completed") && orders[index].PaymentStatus == "paid" {
			if err := h.revealOrder(&orders[index]); err != nil {
				response.Error(c, 500, 50044, "error.order_delivery_decrypt_failed")
				return
			}
		}
	}
	items := make([]publicOrderDTO, 0, len(orders))
	for _, order := range orders {
		items = append(items, toPublicOrderDTO(order))
	}
	response.Page(c, items, total, page, pageSize)
}

func (h Handler) MyWallet(c *gin.Context) {
	userID, err := uuid.Parse(c.GetString("subject"))
	if err != nil {
		response.Error(c, 401, 40140, "error.invalid_login_state")
		return
	}
	requestedCurrency, specified, err := optionalCurrencyQuery(c)
	if err != nil {
		response.Error(c, 422, 42256, "error.currency_code_invalid")
		return
	}
	definition, err := resolveEnabledCurrencyDefinition(h.DB, requestedCurrency, !specified)
	if errors.Is(err, errCurrencySelectionInvalid) {
		response.Error(c, 422, 42256, "error.currency_code_invalid")
		return
	}
	if errors.Is(err, errCurrencySelectionUnavailable) {
		response.Error(c, 422, 42256, "error.currency_unavailable")
		return
	}
	if err != nil {
		response.Error(c, 500, 50056, "error.currency_definition_fetch_failed")
		return
	}
	var account model.WalletAccount
	accountExists := true
	if err := h.DB.Where("owner_type = ? AND owner_id = ? AND currency = ?", "user", userID, definition.Code).First(&account).Error; err != nil {
		if err != gorm.ErrRecordNotFound {
			response.Error(c, 500, 50056, "error.wallet_fetch_failed")
			return
		}
		accountExists = false
		account = model.WalletAccount{OwnerType: "user", OwnerID: userID, Currency: definition.Code}
	}
	var accounts []model.WalletAccount
	if err := h.DB.Where("owner_type = ? AND owner_id = ?", "user", userID).Order("currency ASC").Find(&accounts).Error; err != nil {
		response.Error(c, 500, 50056, "error.wallet_fetch_failed")
		return
	}
	currencyCodes := make([]string, 0, len(accounts))
	for _, item := range accounts {
		currencyCodes = append(currencyCodes, item.Currency)
	}
	var definitions []model.CurrencyDefinition
	if len(currencyCodes) > 0 {
		if err := h.DB.Where("code IN ?", currencyCodes).Find(&definitions).Error; err != nil {
			response.Error(c, 500, 50056, "error.currency_definition_fetch_failed")
			return
		}
	}
	definitionByCode := make(map[string]model.CurrencyDefinition, len(definitions))
	for _, item := range definitions {
		definitionByCode[item.Code] = item
	}
	accountItems := make([]userWalletAccountDTO, 0, len(accounts))
	var selectedAccount userWalletAccountDTO
	for _, item := range accounts {
		itemDefinition, found := definitionByCode[item.Currency]
		if !found || item.Balance < 0 || item.Frozen < 0 || item.Frozen > item.Balance {
			response.Error(c, 500, 50056, "error.wallet_snapshot_invalid")
			return
		}
		dto := userWalletAccountDTO{
			ID: item.ID, Currency: item.Currency, Balance: item.Balance, Frozen: item.Frozen,
			Available: item.Balance - item.Frozen, MinorUnit: itemDefinition.MinorUnit,
			Symbol: itemDefinition.Symbol, CurrencyEnabled: itemDefinition.Enabled,
		}
		accountItems = append(accountItems, dto)
		if item.ID == account.ID {
			selectedAccount = dto
		}
	}
	if !accountExists {
		selectedAccount = userWalletAccountDTO{Currency: definition.Code, MinorUnit: definition.MinorUnit, Symbol: definition.Symbol, CurrencyEnabled: definition.Enabled}
		accountItems = append(accountItems, selectedAccount)
	}
	var entries []model.WalletEntry
	if accountExists {
		if err := h.DB.Where("account_id = ?", account.ID).Order("created_at DESC").Limit(50).Find(&entries).Error; err != nil {
			response.Error(c, 500, 50056, "error.wallet_transaction_fetch_failed")
			return
		}
	}
	entryItems := make([]userWalletEntryDTO, 0, len(entries))
	for _, entry := range entries {
		entryItems = append(entryItems, userWalletEntryDTO{
			ID: entry.ID, EntryNo: entry.EntryNo, Type: entry.Type, Amount: entry.Amount,
			BalanceAfter: entry.BalanceAfter, Currency: account.Currency, ReferenceType: entry.ReferenceType,
			Description: entry.Description, CreatedAt: entry.CreatedAt,
		})
	}
	response.OK(c, gin.H{
		"account": selectedAccount, "accounts": accountItems, "selected_currency": definition.Code,
		"entries": entryItems,
	})
}

type ticketRequest struct {
	OrderID  string `json:"order_id"`
	Category string `json:"category" binding:"required"`
	Subject  string `json:"subject" binding:"required,max=200"`
	Body     string `json:"body" binding:"required,max=10000"`
}

func (h Handler) CreateTicket(c *gin.Context) {
	userID, err := uuid.Parse(c.GetString("subject"))
	if err != nil {
		response.Error(c, 401, 40140, "error.invalid_login_state")
		return
	}
	var user model.User
	if h.DB.First(&user, "id = ?", userID).Error != nil {
		response.Error(c, 404, 40441, "error.user_not_found")
		return
	}
	var req ticketRequest
	if decodeStrictJSON(c, &req) != nil {
		response.Error(c, 422, 42244, "error.ticket_content_incomplete")
		return
	}
	req.Category = strings.ToLower(strings.TrimSpace(req.Category))
	req.Subject = strings.TrimSpace(req.Subject)
	req.Body = strings.TrimSpace(req.Body)
	allowedCategory := map[string]bool{"billing": true, "delivery": true, "product": true, "refund": true, "other": true}
	if !allowedCategory[req.Category] || len([]rune(req.Subject)) < 3 || len([]rune(req.Subject)) > 200 || len([]rune(req.Body)) < 2 || len([]rune(req.Body)) > 10000 {
		response.Error(c, 422, 42244, "error.ticket_format_invalid")
		return
	}
	var openCount int64
	if err := h.DB.Model(&model.SupportTicket{}).Where("user_id = ? AND status IN ?", userID, []string{"open", "in_progress", "waiting_user"}).Count(&openCount).Error; err != nil {
		response.Error(c, 500, 50040, "error.ticket_status_fetch_failed")
		return
	}
	if openCount >= 10 {
		response.Error(c, 429, 42940, "error.open_ticket_limit_reached")
		return
	}
	var orderID *uuid.UUID
	if req.OrderID != "" {
		parsed, parseErr := uuid.Parse(req.OrderID)
		if parseErr != nil {
			response.Error(c, 422, 42245, "error.order_number_invalid")
			return
		}
		var ownedOrder model.Order
		if err := h.DB.Select("id").Where("id = ? AND user_id = ?", parsed, userID).First(&ownedOrder).Error; err != nil {
			response.Error(c, 422, 42245, "error.order_not_owned")
			return
		}
		orderID = &parsed
	}
	now := time.Now()
	ticket := model.SupportTicket{TicketNo: fmt.Sprintf("LQT%s%s", now.Format("20060102150405"), strings.ToUpper(strings.ReplaceAll(uuid.NewString(), "-", "")[:8])), UserID: &userID, OrderID: orderID, Email: user.Email, Category: req.Category, Subject: req.Subject, Priority: "normal", Status: "open", LastMessageAt: &now, AdminUnread: 1}
	var deliveryID uuid.UUID
	err = h.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&ticket).Error; err != nil {
			return err
		}
		message := model.TicketMessage{TicketID: ticket.ID, AuthorType: "user", AuthorID: &userID, Body: req.Body}
		if err := tx.Create(&message).Error; err != nil {
			return err
		}
		delivery, err := h.createTicketNotification(tx, "admin", "admin", "新售后工单 "+ticket.TicketNo, "用户提交了售后工单："+ticket.Subject, "ticket-created:"+ticket.ID.String())
		if err != nil {
			return err
		}
		deliveryID = delivery.ID
		return nil
	})
	if err != nil {
		response.Error(c, 500, 50040, "error.ticket_create_failed")
		return
	}
	h.enqueueNotification(deliveryID)
	response.Created(c, gin.H{"ticket": ticket})
}

func (h Handler) MyTickets(c *gin.Context) {
	userID, err := uuid.Parse(c.GetString("subject"))
	if err != nil {
		response.Error(c, 401, 40140, "error.invalid_login_state")
		return
	}
	page, pageSize := pagination(c)
	var total int64
	h.DB.Model(&model.SupportTicket{}).Where("user_id = ?", userID).Count(&total)
	var items []model.SupportTicket
	h.DB.Where("user_id = ?", userID).Order("COALESCE(last_message_at, created_at) DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&items)
	response.Page(c, items, total, page, pageSize)
}

func (h Handler) MyReseller(c *gin.Context) {
	userID, err := uuid.Parse(c.GetString("subject"))
	if err != nil {
		response.Error(c, 401, 40140, "error.invalid_login_state")
		return
	}
	var profile model.ResellerProfile
	if err := h.DB.Where("user_id = ?", userID).First(&profile).Error; err != nil {
		response.Error(c, 404, 40443, "error.reseller_application_required")
		return
	}
	var domains []model.ResellerDomain
	var site model.ResellerSite
	var rules []model.ResellerProductRule
	var wallet model.WalletAccount
	currencyCode, currencyErr := service.StoreCurrency(h.DB)
	if currencyErr != nil {
		response.Error(c, 500, 50080, "error.store_currency_fetch_failed")
		return
	}
	if err := h.DB.Where("reseller_id = ?", profile.ID).Order("created_at DESC").Find(&domains).Error; err != nil {
		response.Error(c, 500, 50080, "error.reseller_overview_fetch_failed")
		return
	}
	if err := h.DB.Where("reseller_id = ?", profile.ID).First(&site).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		response.Error(c, 500, 50080, "error.reseller_overview_fetch_failed")
		return
	}
	if err := h.DB.Where("reseller_id = ?", profile.ID).Find(&rules).Error; err != nil {
		response.Error(c, 500, 50080, "error.reseller_overview_fetch_failed")
		return
	}
	if err := h.DB.Where("owner_type = ? AND owner_id = ? AND currency = ?", "reseller", profile.ID, currencyCode).First(&wallet).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		response.Error(c, 500, 50080, "error.reseller_overview_fetch_failed")
		return
	}
	creditLimit, err := service.ResellerCreditLimit(h.DB, profile.ID, currencyCode)
	if err != nil {
		response.Error(c, 500, 50080, "error.reseller_credit_state_invalid")
		return
	}
	profile.CreditLimit = creditLimit
	credit, err := service.CalculateResellerCreditState(wallet.Balance, wallet.Frozen, creditLimit)
	if err != nil {
		response.Error(c, 500, 50080, "error.reseller_credit_state_invalid")
		return
	}
	wholesale, err := service.LoadResellerWholesalePolicy(h.DB, profile.WholesaleLevel)
	if err != nil {
		response.Error(c, 500, 50080, "error.reseller_overview_fetch_failed")
		return
	}
	response.OK(c, gin.H{"profile": profile, "domains": domains, "site": site, "product_rules": rules, "wallet": wallet, "credit": credit, "wholesale": wholesale})
}

type resellerApplicationRequest struct {
	Name string `json:"name"`
}

func (h Handler) ApplyReseller(c *gin.Context) {
	userID, err := uuid.Parse(c.GetString("subject"))
	if err != nil {
		response.Error(c, 401, 40140, "error.invalid_login_state")
		return
	}
	var existing model.ResellerProfile
	if h.DB.Where("user_id = ?", userID).First(&existing).Error == nil {
		response.Error(c, 409, 40943, "error.reseller_application_already_submitted")
		return
	}
	var req resellerApplicationRequest
	if decodeStrictJSON(c, &req) != nil {
		response.Error(c, 422, 42252, "error.business_name_required")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	if len([]rune(req.Name)) < 2 || len([]rune(req.Name)) > 160 {
		response.Error(c, 422, 42252, "error.business_name_length")
		return
	}
	now := time.Now()
	currencyCode, currencyErr := service.StoreCurrency(h.DB)
	if currencyErr != nil {
		response.Error(c, 500, 50080, "error.store_currency_fetch_failed")
		return
	}
	profile := model.ResellerProfile{UserID: userID, Name: req.Name, Code: "LQR" + strings.ToUpper(strings.ReplaceAll(uuid.NewString(), "-", "")[:12]), Status: "pending", AppliedAt: now}
	err = h.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&profile).Error; err != nil {
			return err
		}
		if err := tx.Create(&model.WalletAccount{OwnerType: "reseller", OwnerID: profile.ID, Currency: currencyCode}).Error; err != nil {
			return err
		}
		return tx.Create(&model.ResellerSite{ResellerID: profile.ID, SiteName: profile.Name, Theme: `{"mode":"system","density":"comfortable"}`, SEO: `{}`, Support: `{}`}).Error
	})
	if err != nil {
		response.Error(c, 500, 50043, "error.reseller_apply_failed")
		return
	}
	response.Created(c, profile)
}

func (h Handler) OperationsSummary(c *gin.Context) {
	counts := gin.H{}
	models := map[string]any{"orders": &model.Order{}, "payments": &model.PaymentIntent{}, "refunds": &model.Refund{}, "tickets": &model.SupportTicket{}, "risk_reviews": &model.RiskDecision{}, "procurements": &model.ProcurementOrder{}, "webhooks": &model.WebhookDelivery{}, "affiliates": &model.AffiliateProfile{}, "resellers": &model.ResellerProfile{}, "jobs": &model.JobRecord{}}
	for name, value := range models {
		var count int64
		h.DB.Model(value).Count(&count)
		counts[name] = count
	}
	response.OK(c, counts)
}

func (h Handler) AdminResourceList(c *gin.Context) {
	page, pageSize := pagination(c)
	resourceName := c.Param("resource")
	switch resourceName {
	case "refunds":
		listResource[model.Refund](c, h.DB, page, pageSize)
	case "tickets":
		listResource[model.SupportTicket](c, h.DB, page, pageSize)
	case "risk-rules":
		listResource[model.RiskRule](c, h.DB, page, pageSize)
	case "risk-decisions":
		listResource[model.RiskDecision](c, h.DB, page, pageSize)
	case "reconciliations":
		listResource[model.ReconciliationBatch](c, h.DB, page, pageSize)
	case "promotions":
		h.AdminPromotions(c)
	case "coupons":
		h.AdminCoupons(c)
	case "gift-cards":
		listResource[model.GiftCard](c, h.DB, page, pageSize)
	case "affiliates":
		h.AdminAffiliateProfiles(c)
	case "affiliate-withdrawals":
		listResource[model.AffiliateWithdrawal](c, h.DB, page, pageSize)
	case "resellers":
		listResource[model.ResellerProfile](c, h.DB, page, pageSize)
	case "reseller-domains":
		listResource[model.ResellerDomain](c, h.DB, page, pageSize)
	case "wallets":
		listResource[model.WalletAccount](c, h.DB, page, pageSize)
	case "wallet-entries":
		listResource[model.WalletEntry](c, h.DB, page, pageSize)
	case "login-events":
		listResource[model.LoginEvent](c, h.DB, page, pageSize)
	case "security-events":
		listResource[model.SecurityEvent](c, h.DB, page, pageSize)
	case "roles":
		listResource[model.Role](c, h.DB, page, pageSize)
	case "audit-logs":
		listResource[model.AuditLog](c, h.DB, page, pageSize)
	case "posts":
		h.AdminContentPosts(c)
	case "banners":
		h.AdminContentBanners(c)
	case "variants":
		h.AdminProductVariants(c)
	case "price-tiers":
		h.AdminProductPriceTiers(c)
	case "inventory-batches":
		h.AdminInventoryBatches(c)
	case "member-levels":
		h.AdminCatalogMemberLevels(c)
	case "payment-intents":
		listResource[model.PaymentIntent](c, h.DB, page, pageSize)
	case "payment-transactions":
		listResource[model.PaymentTransaction](c, h.DB, page, pageSize)
	case "affiliate-commissions":
		listResource[model.AffiliateCommission](c, h.DB, page, pageSize)
	case "reseller-sites":
		listResource[model.ResellerSite](c, h.DB, page, pageSize)
	case "reseller-product-rules":
		listResource[model.ResellerProductRule](c, h.DB, page, pageSize)
	case "ip-blocklist":
		listResource[model.IPBlocklist](c, h.DB, page, pageSize)
	case "jobs":
		listResource[model.JobRecord](c, h.DB, page, pageSize)
	default:
		response.Error(c, 404, 40442, "error.operation_resource_not_found")
	}
}

// CreateAdminResource exposes carefully selected configuration records to the
// operations console. Transactional resources (orders, refunds, wallets,
// procurements and deliveries) deliberately stay behind their domain services.
func (h Handler) CreateAdminResource(c *gin.Context) {
	resourceName := c.Param("resource")
	switch resourceName {
	case "promotions":
		h.CreatePromotion(c)
	case "coupons":
		h.CreateCoupon(c)
	case "risk-rules":
		createAdminRecord[model.RiskRule](c, h, resourceName, func(item *model.RiskRule) bool {
			return service.ValidateRiskRule(*item) == nil
		})
	case "member-levels":
		h.CreateCatalogMemberLevel(c)
	case "posts":
		h.CreateContentPost(c)
	case "banners":
		h.CreateContentBanner(c)
	case "roles":
		// Keep the legacy operational URL, but send every role mutation through
		// the same strict, audited RBAC transaction. A generic model insert here
		// would bypass permission validation and the last-manager invariant.
		h.CreateAccessRole(c)
	case "variants":
		h.CreateCatalogProductVariant(c)
	case "price-tiers":
		h.CreateCatalogPriceTier(c)
	default:
		response.Error(c, 404, 40443, "error.resource_generic_create_not_allowed")
	}
}

func createAdminRecord[T any](c *gin.Context, h Handler, resourceName string, valid func(*T) bool) {
	var payload map[string]any
	if err := c.ShouldBindJSON(&payload); err != nil || len(payload) == 0 {
		response.Error(c, 422, 42248, "error.request_json_invalid")
		return
	}
	// These values are always server-owned, including for super administrators.
	delete(payload, "id")
	delete(payload, "created_at")
	delete(payload, "updated_at")
	delete(payload, "deleted_at")
	raw, err := json.Marshal(payload)
	if err != nil {
		response.Error(c, 422, 42248, "error.request_json_invalid")
		return
	}
	var item T
	if err := json.Unmarshal(raw, &item); err != nil || !valid(&item) {
		response.Error(c, 422, 42249, "error.resource_fields_invalid")
		return
	}
	if err := h.DB.Create(&item).Error; err != nil {
		response.Error(c, 409, 40943, "error.resource_creation_failed_check_unique_key")
		return
	}
	h.audit(c, resourceName+".create", resourceName, "", "管理控制台创建")
	response.Created(c, item)
}

type transitionRequest struct {
	Status string `json:"status" binding:"required"`
	Reason string `json:"reason"`
}

func (h Handler) TransitionOrder(c *gin.Context) {
	orderID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, 422, 42246, "error.order_number_invalid")
		return
	}
	adminID, _ := uuid.Parse(c.GetString("subject"))
	var req transitionRequest
	if decodeStrictJSON(c, &req) != nil {
		response.Error(c, 422, 42247, "error.target_status_required")
		return
	}
	changeReason, ok := requireAdminChangeReason(c, "变更订单状态")
	if !ok {
		return
	}
	var current model.Order
	if err := h.DB.Select("id", "status").First(&current, "id = ?", orderID).Error; err != nil {
		response.Error(c, 404, 40440, "error.order_not_found")
		return
	}
	req.Status = strings.TrimSpace(req.Status)
	if !validAdminManualOrderTransition(current.Status, req.Status) {
		response.Error(c, 409, 40941, "error.order_status_manual_change_not_allowed")
		return
	}
	if err := service.TransitionOrder(h.DB, orderID, req.Status, "admin", &adminID, changeReason); err != nil {
		response.Error(c, 409, 40941, "error.order_status_manual_change_not_allowed")
		return
	}
	h.audit(c, "order.transition", "order", orderID.String(), changeReason+"；target="+req.Status)
	response.OK(c, gin.H{"status": req.Status})
}

func listResource[T any](c *gin.Context, db *gorm.DB, page, pageSize int) {
	var total int64
	var items []T
	query := db.Model(new(T))
	if status := strings.TrimSpace(c.Query("status")); status != "" {
		query = query.Where("status = ?", status)
	}
	query.Count(&total)
	query.Order("created_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&items)
	response.Page(c, items, total, page, pageSize)
}

func toJSONArray(values []string) string {
	if len(values) == 0 {
		return "[]"
	}
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, `"`+strings.ReplaceAll(value, `"`, `\"`)+`"`)
	}
	return "[" + strings.Join(parts, ",") + "]"
}
