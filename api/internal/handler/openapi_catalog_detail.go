package handler

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"linlinqi/api/internal/content"
	"linlinqi/api/internal/model"
	"linlinqi/api/pkg/response"
)

type openAPIStockDTO struct {
	ExternalProductID string     `json:"external_product_id"`
	ProductID         uuid.UUID  `json:"product_id"`
	VariantID         *uuid.UUID `json:"variant_id,omitempty"`
	Stock             int64      `json:"stock"`
	StockStatus       string     `json:"stock_status"`
	ObservedAt        time.Time  `json:"observed_at"`
}

func openAPIProductVariantBounds(product model.Product, variant model.ProductVariant) (int, int) {
	minimum := max(product.MinimumPurchase, 1)
	maximum := max(product.MaximumPurchase, 0)
	if variant.PurchaseLimit > 0 && (maximum == 0 || variant.PurchaseLimit < maximum) {
		maximum = variant.PurchaseLimit
	}
	return minimum, maximum
}

func (h Handler) openAPIProductDetailDTO(product model.Product, quote storefrontCurrencyQuote) (openAPIProductDTO, error) {
	var variants []model.ProductVariant
	if err := h.DB.Where("product_id = ? AND status = ?", product.ID, "active").Order("sort DESC, created_at ASC").Find(&variants).Error; err != nil {
		return openAPIProductDTO{}, err
	}
	publicVariants := make([]openAPIProductVariantDTO, 0, len(variants))
	for _, variant := range variants {
		minimum, maximum := openAPIProductVariantBounds(product, variant)
		price, err := quote.Conversion.Amount(variant.Price)
		if err != nil {
			return openAPIProductDTO{}, err
		}
		comparePrice, err := quote.Conversion.Amount(variant.ComparePrice)
		if err != nil {
			return openAPIProductDTO{}, err
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
		return openAPIProductDTO{}, err
	}
	price, err := quote.Conversion.Amount(product.Price)
	if err != nil {
		return openAPIProductDTO{}, err
	}
	comparePrice, err := quote.Conversion.Amount(product.ComparePrice)
	if err != nil {
		return openAPIProductDTO{}, err
	}
	mediaByProduct, err := catalogMediaForOwners(h.DB, "product", []uuid.UUID{product.ID})
	if err != nil {
		return openAPIProductDTO{}, err
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
	return openAPIProductDTO{
		ID: product.ID, ExternalID: product.ID.String(), ExternalSKU: product.Slug,
		ExternalCategoryID: product.CategoryID.String(), CategoryID: product.CategoryID,
		Name: product.Name, Slug: product.Slug, Summary: product.Summary,
		Description: content.SanitizeRichHTML(product.Description), CoverURL: coverURL, ImageURLs: imageURLs,
		SourceCurrency: quote.Conversion.Source.Code, Currency: quote.Conversion.Target.Code,
		FX: quote.Conversion.FX(), Price: price, ComparePrice: comparePrice, Stock: h.productStock(product),
		Minimum: max(product.MinimumPurchase, 1), Maximum: max(product.MaximumPurchase, 0), Status: status, Delivery: product.DeliveryType,
		DeliveryType: product.DeliveryType, InventoryMode: product.InventoryMode, Tags: product.Tags,
		CreatedAt: product.CreatedAt, UpdatedAt: product.UpdatedAt,
		Variants: publicVariants, InputFields: inputFields,
	}, nil
}

func (h Handler) findOpenAPIProduct(identifier string, includeInactive bool) (model.Product, error) {
	identifier = strings.TrimSpace(identifier)
	if !validOpenAPIIdentifier(identifier, 180) {
		return model.Product{}, gorm.ErrRecordNotFound
	}
	query := h.DB.Preload("Category")
	if !includeInactive {
		query = query.Where("status = ?", "on_sale")
	} else {
		query = query.Where("status IN ?", []string{"on_sale", "off_sale"})
	}
	var product model.Product
	if id, err := uuid.Parse(identifier); err == nil {
		if err := query.First(&product, "id = ?", id).Error; err == nil {
			return product, nil
		}
	}
	if err := query.First(&product, "slug = ?", identifier).Error; err != nil {
		return model.Product{}, err
	}
	return product, nil
}

// OpenAPIProduct exposes a stable single-record form of the catalog DTO. It
// avoids forcing downstream stores to download an entire catalog to refresh a
// single product after a change notification.
func (h Handler) OpenAPIProduct(c *gin.Context) {
	includeInactive := false
	if value, err := optionalBoolQuery(c, "include_inactive"); err != nil {
		response.Error(c, 422, 42230, "error.supply_catalog_query_invalid")
		return
	} else if value != nil {
		includeInactive = *value
	}
	quote, err := h.storefrontCurrency(c, c.Query("currency"))
	if err != nil {
		status, code, message := currencyRequestError(err)
		response.Error(c, status, code, message)
		return
	}
	product, err := h.findOpenAPIProduct(c.Param("product_id"), includeInactive)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		response.Error(c, 404, 40401, "error.product_not_found_or_unavailable")
		return
	}
	if err != nil {
		response.Error(c, 500, 50020, "error.supply_catalog_fetch_failed")
		return
	}
	item, err := h.openAPIProductDetailDTO(product, quote)
	if err != nil {
		response.Error(c, 500, 50020, "error.supply_catalog_fetch_failed")
		return
	}
	c.Header("Last-Modified", product.UpdatedAt.UTC().Format(httpTimeFormat))
	response.OK(c, item)
}

// OpenAPIProductStock reports reservation-aware available stock. The response
// never exposes per-supplier routing or reservation counts.
func (h Handler) OpenAPIProductStock(c *gin.Context) {
	product, err := h.findOpenAPIProduct(c.Param("product_id"), false)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		response.Error(c, 404, 40401, "error.product_not_found_or_unavailable")
		return
	}
	if err != nil {
		response.Error(c, 500, 50020, "error.supply_catalog_fetch_failed")
		return
	}
	var variantID *uuid.UUID
	if identifier := strings.TrimSpace(c.Query("variant_id")); identifier != "" {
		var variant model.ProductVariant
		query := h.DB.Select("id").Where("product_id = ? AND status = ?", product.ID, "active")
		if parsed, parseErr := uuid.Parse(identifier); parseErr == nil {
			query = query.Where("id = ?", parsed)
		} else {
			query = query.Where("sku = ?", identifier)
		}
		if err := query.First(&variant).Error; err != nil {
			response.Error(c, 404, 40401, "error.product_not_found_or_unavailable")
			return
		}
		variantID = &variant.ID
	}
	stock := h.productStockForVariant(product, variantID)
	stockStatus := "out_of_stock"
	if stock > 0 {
		stockStatus = "in_stock"
	}
	response.OK(c, openAPIStockDTO{
		ExternalProductID: product.ID.String(), ProductID: product.ID, VariantID: variantID,
		Stock: stock, StockStatus: stockStatus, ObservedAt: time.Now().UTC(),
	})
}

const httpTimeFormat = "Mon, 02 Jan 2006 15:04:05 GMT"
