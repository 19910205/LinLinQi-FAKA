package handler

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"linlinqi/api/internal/content"
	"linlinqi/api/internal/model"
	"linlinqi/api/internal/service"
)

// Public DTOs deliberately enumerate fields that may leave the public API.
// Keep commercial cost data and internal inventory routing out of these types.
type publicCategoryDTO struct {
	ID          uuid.UUID  `json:"id"`
	ParentID    *uuid.UUID `json:"parent_id,omitempty"`
	Name        string     `json:"name"`
	Slug        string     `json:"slug"`
	Description string     `json:"description"`
	Icon        string     `json:"icon"`
	ImageURL    string     `json:"image_url"`
}

type publicProductDTO struct {
	ID             uuid.UUID          `json:"id"`
	CategoryID     uuid.UUID          `json:"category_id"`
	Category       publicCategoryDTO  `json:"category"`
	Name           string             `json:"name"`
	Slug           string             `json:"slug"`
	Summary        string             `json:"summary"`
	Description    string             `json:"description"`
	CoverURL       string             `json:"cover_url"`
	Media          []catalogMediaDTO  `json:"media"`
	Currency       string             `json:"currency"`
	SourceCurrency string             `json:"source_currency"`
	FX             service.CheckoutFX `json:"fx"`
	Price          int64              `json:"price"`
	ComparePrice   int64              `json:"compare_price"`
	DeliveryType   string             `json:"delivery_type"`
	Minimum        int                `json:"minimum"`
	Maximum        int                `json:"maximum"`
	SoldCount      int64              `json:"sold_count"`
	Featured       bool               `json:"featured"`
	Tags           string             `json:"tags"`
}

type publicProductVariantDTO struct {
	ID            uuid.UUID `json:"id"`
	SKU           string    `json:"sku"`
	Name          string    `json:"name"`
	Attributes    string    `json:"attributes"`
	Price         int64     `json:"price"`
	ComparePrice  int64     `json:"compare_price"`
	Sort          int       `json:"sort"`
	PurchaseLimit int       `json:"purchase_limit"`
	Stock         int64     `json:"stock"`
}

type publicProductInputFieldDTO struct {
	ID                uuid.UUID       `json:"id"`
	Key               string          `json:"key"`
	Label             string          `json:"label"`
	InputType         string          `json:"input_type"`
	Required          bool            `json:"required"`
	Sensitive         bool            `json:"sensitive"`
	Placeholder       string          `json:"placeholder"`
	HelpText          string          `json:"help_text"`
	Options           json.RawMessage `json:"options"`
	ValidationPattern string          `json:"validation_pattern,omitempty"`
	MinLength         int             `json:"min_length"`
	MaxLength         int             `json:"max_length"`
	Sort              int             `json:"sort"`
}

type openAPIProductDTO struct {
	ID                 uuid.UUID                    `json:"id"`
	ExternalID         string                       `json:"external_id"`
	ExternalSKU        string                       `json:"external_sku"`
	ExternalCategoryID string                       `json:"external_category_id"`
	CategoryID         uuid.UUID                    `json:"category_id"`
	Name               string                       `json:"name"`
	Slug               string                       `json:"slug"`
	Summary            string                       `json:"summary"`
	Description        string                       `json:"description"`
	CoverURL           string                       `json:"cover_url"`
	ImageURLs          []string                     `json:"image_urls"`
	Currency           string                       `json:"currency"`
	SourceCurrency     string                       `json:"source_currency"`
	FX                 service.CheckoutFX           `json:"fx"`
	Price              int64                        `json:"price"`
	ComparePrice       int64                        `json:"compare_price"`
	Stock              int64                        `json:"stock"`
	Minimum            int                          `json:"minimum"`
	Maximum            int                          `json:"maximum"`
	Status             string                       `json:"status"`
	Delivery           string                       `json:"delivery"`
	DeliveryType       string                       `json:"delivery_type"`
	InventoryMode      string                       `json:"inventory_mode"`
	Tags               string                       `json:"tags"`
	CreatedAt          time.Time                    `json:"created_at"`
	UpdatedAt          time.Time                    `json:"updated_at"`
	Variants           []openAPIProductVariantDTO   `json:"variants,omitempty"`
	InputFields        []publicProductInputFieldDTO `json:"input_fields,omitempty"`
}

func toPublicProductInputFieldDTO(field model.ProductInputField) publicProductInputFieldDTO {
	options := field.Options
	if len(options) == 0 {
		options = json.RawMessage("[]")
	}
	return publicProductInputFieldDTO{
		ID: field.ID, Key: field.Key, Label: field.Label, InputType: field.InputType,
		Required: field.Required, Sensitive: field.Sensitive, Placeholder: field.Placeholder,
		HelpText: field.HelpText, Options: options, ValidationPattern: field.ValidationPattern,
		MinLength: field.MinLength, MaxLength: field.MaxLength, Sort: field.Sort,
	}
}

type openAPIProductVariantDTO struct {
	ID            uuid.UUID       `json:"id"`
	ExternalID    string          `json:"external_id"`
	ExternalSKU   string          `json:"external_sku"`
	Name          string          `json:"name"`
	Attributes    json.RawMessage `json:"attributes"`
	Price         int64           `json:"price"`
	ComparePrice  int64           `json:"compare_price"`
	Stock         int64           `json:"stock"`
	Minimum       int             `json:"minimum"`
	Maximum       int             `json:"maximum"`
	PurchaseLimit int             `json:"purchase_limit"`
	Status        string          `json:"status"`
}

type openAPICategoryDTO struct {
	ID               uuid.UUID `json:"id"`
	ExternalID       string    `json:"external_id"`
	ExternalParentID string    `json:"external_parent_id,omitempty"`
	Name             string    `json:"name"`
	Slug             string    `json:"slug"`
	Description      string    `json:"description"`
	Icon             string    `json:"icon"`
	ImageURL         string    `json:"image_url"`
	Sort             int       `json:"sort"`
	Status           string    `json:"status"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type dashboardRecentOrderItemDTO struct {
	ProductName string `json:"product_name"`
}

type dashboardRecentOrderDTO struct {
	OrderNo   string                        `json:"order_no"`
	Status    string                        `json:"status"`
	Total     int64                         `json:"total"`
	Currency  string                        `json:"currency"`
	CreatedAt time.Time                     `json:"created_at"`
	Items     []dashboardRecentOrderItemDTO `json:"items"`
}

// Public order responses intentionally omit client IPs, account identifiers,
// coupon/internal routing IDs, encrypted fields and reseller margins. The
// lookup token is populated only on the one-time create response.
type publicOrderItemDTO struct {
	ID          uuid.UUID  `json:"id"`
	ProductID   uuid.UUID  `json:"product_id"`
	VariantID   *uuid.UUID `json:"variant_id,omitempty"`
	VariantName string     `json:"variant_name,omitempty"`
	ProductName string     `json:"product_name"`
	UnitPrice   int64      `json:"unit_price"`
	Currency    string     `json:"currency"`
	Quantity    int        `json:"quantity"`
	CardContent string     `json:"card_content,omitempty"`
}

type publicOrderDTO struct {
	ID              uuid.UUID            `json:"id"`
	OrderNo         string               `json:"order_no"`
	LookupToken     string               `json:"lookup_token,omitempty"`
	ExternalOrderNo *string              `json:"external_order_no,omitempty"`
	Email           string               `json:"email"`
	Status          string               `json:"status"`
	PaymentStatus   string               `json:"payment_status"`
	Subtotal        int64                `json:"subtotal"`
	Discount        int64                `json:"discount"`
	Adjustments     json.RawMessage      `json:"adjustments"`
	Total           int64                `json:"total"`
	Currency        string               `json:"currency"`
	PaymentMethod   string               `json:"payment_method"`
	PaidAt          *time.Time           `json:"paid_at,omitempty"`
	DeliveredAt     *time.Time           `json:"delivered_at,omitempty"`
	CreatedAt       time.Time            `json:"created_at"`
	UpdatedAt       time.Time            `json:"updated_at"`
	Items           []publicOrderItemDTO `json:"items"`
}

func toPublicCategoryDTO(category model.Category) publicCategoryDTO {
	return publicCategoryDTO{
		ID:          category.ID,
		ParentID:    category.ParentID,
		Name:        category.Name,
		Slug:        category.Slug,
		Description: category.Description,
		Icon:        category.Icon,
		ImageURL:    category.ImageURL,
	}
}

func toPublicProductDTO(product model.Product) publicProductDTO {
	return publicProductDTO{
		ID:           product.ID,
		CategoryID:   product.CategoryID,
		Category:     toPublicCategoryDTO(product.Category),
		Name:         product.Name,
		Slug:         product.Slug,
		Summary:      product.Summary,
		Description:  content.SanitizeRichHTML(product.Description),
		CoverURL:     product.CoverURL,
		Media:        []catalogMediaDTO{},
		Currency:     product.Currency,
		Price:        product.Price,
		ComparePrice: product.ComparePrice,
		DeliveryType: product.DeliveryType,
		Minimum:      product.MinimumPurchase,
		Maximum:      product.MaximumPurchase,
		SoldCount:    product.SoldCount,
		Featured:     product.Featured,
		Tags:         product.Tags,
	}
}

func toPublicProductVariantDTO(variant model.ProductVariant, stock int64) publicProductVariantDTO {
	return publicProductVariantDTO{
		ID:            variant.ID,
		SKU:           variant.SKU,
		Name:          variant.Name,
		Attributes:    variant.Attributes,
		Price:         variant.Price,
		ComparePrice:  variant.ComparePrice,
		Sort:          variant.Sort,
		PurchaseLimit: variant.PurchaseLimit,
		Stock:         stock,
	}
}

func toPublicOrderDTO(order model.Order) publicOrderDTO {
	items := make([]publicOrderItemDTO, 0, len(order.Items))
	for _, item := range order.Items {
		items = append(items, publicOrderItemDTO{
			ID: item.ID, ProductID: item.ProductID, VariantID: item.VariantID,
			VariantName: item.VariantName, ProductName: item.ProductName,
			UnitPrice: item.UnitPrice, Currency: item.Currency, Quantity: item.Quantity, CardContent: item.CardContent,
		})
	}
	adjustments := order.Adjustments
	if len(adjustments) == 0 {
		adjustments = json.RawMessage("[]")
	}
	return publicOrderDTO{
		ID: order.ID, OrderNo: order.OrderNo, LookupToken: order.LookupToken,
		ExternalOrderNo: order.ExternalOrderNo, Email: order.Email, Status: order.Status,
		PaymentStatus: order.PaymentStatus, Subtotal: order.Subtotal, Discount: order.Discount,
		Adjustments: adjustments, Total: order.Total, Currency: order.Currency, PaymentMethod: order.PaymentMethod,
		PaidAt: order.PaidAt, DeliveredAt: order.DeliveredAt, CreatedAt: order.CreatedAt,
		UpdatedAt: order.UpdatedAt, Items: items,
	}
}
