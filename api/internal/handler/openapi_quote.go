package handler

import (
	"errors"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"linlinqi/api/internal/model"
	"linlinqi/api/internal/service"
	"linlinqi/api/pkg/response"
)

type openAPIProductQuoteRequest struct {
	VariantID string `json:"variant_id"`
	Quantity  int    `json:"quantity"`
	Currency  string `json:"currency"`
}

// openAPIProductQuoteDTO intentionally contains only the customer-facing sale
// quote. Platform cost, upstream cost and reseller margin never cross the
// OpenAPI boundary.
type openAPIProductQuoteDTO struct {
	ExternalProductID string             `json:"external_product_id"`
	ProductID         uuid.UUID          `json:"product_id"`
	VariantID         *uuid.UUID         `json:"variant_id,omitempty"`
	Quantity          int                `json:"quantity"`
	UnitAmount        int64              `json:"unit_amount"`
	Subtotal          int64              `json:"subtotal"`
	DiscountAmount    int64              `json:"discount_amount"`
	Amount            int64              `json:"amount"`
	Currency          string             `json:"currency"`
	MinorUnit         int                `json:"minor_unit"`
	FX                service.CheckoutFX `json:"fx"`
	QuotedAt          time.Time          `json:"quoted_at"`
}

func openAPICredentialPricingContext(credential model.APICredential) (userID, resellerID *uuid.UUID, err error) {
	if credential.OwnerID == nil || *credential.OwnerID == uuid.Nil {
		return nil, nil, errors.New("OpenAPI credential has no billing owner")
	}
	ownerID := *credential.OwnerID
	switch strings.ToLower(strings.TrimSpace(credential.OwnerType)) {
	case "", "user":
		return &ownerID, nil, nil
	case "reseller":
		return nil, &ownerID, nil
	default:
		return nil, nil, errors.New("OpenAPI credential owner type is unsupported")
	}
}

func openAPIProductQuoteFromResolved(resolved service.ResolvedLine, conversion service.CheckoutCurrencyConversion, quotedAt time.Time) (openAPIProductQuoteDTO, error) {
	if !strings.EqualFold(strings.TrimSpace(resolved.Product.Currency), conversion.Source.Code) {
		return openAPIProductQuoteDTO{}, service.ErrCurrencyMismatch
	}
	if conversion.Target.MinorUnit < 0 || conversion.Target.MinorUnit > 6 {
		return openAPIProductQuoteDTO{}, errors.New("currency minor unit is invalid")
	}
	quote, err := conversion.PriceQuote(resolved.Quote)
	if err != nil {
		return openAPIProductQuoteDTO{}, err
	}
	return openAPIProductQuoteDTO{
		ExternalProductID: resolved.Product.ID.String(), ProductID: resolved.Product.ID, VariantID: resolved.VariantID,
		Quantity: quote.Quantity, UnitAmount: quote.UnitPrice, Subtotal: quote.Subtotal,
		DiscountAmount: quote.Discount, Amount: quote.Total,
		Currency: conversion.Target.Code, MinorUnit: conversion.Target.MinorUnit,
		FX: conversion.FX(), QuotedAt: quotedAt.UTC(),
	}, nil
}

// OpenAPIProductQuote returns the exact non-mutating sale-price calculation
// used by order creation. The API credential is the pricing identity: a user
// credential receives its eligible member/tier price, while a reseller
// credential receives that reseller's configured storefront price.
func (h Handler) OpenAPIProductQuote(c *gin.Context) {
	var req openAPIProductQuoteRequest
	if decodeStrictJSON(c, &req) != nil || req.Quantity < 1 || req.Quantity > 20 {
		response.Error(c, 422, 42207, "error.valid_spec_and_quantity_required")
		return
	}
	productID, variantID, err := resolveOpenAPIProduct(h.DB, c.Param("product_id"), req.VariantID)
	if err != nil {
		response.Error(c, 422, 42202, "error.product_id_invalid")
		return
	}
	credentialID, err := uuid.Parse(c.GetString("api_credential_id"))
	if err != nil {
		response.Error(c, 401, 40121, "error.invalid_api_credential")
		return
	}
	var credential model.APICredential
	if err := h.DB.Select("id", "owner_type", "owner_id").First(&credential, "id = ?", credentialID).Error; err != nil {
		response.Error(c, 401, 40121, "error.invalid_api_credential")
		return
	}
	userID, resellerID, err := openAPICredentialPricingContext(credential)
	if err != nil {
		response.Error(c, 403, 40321, "error.api_credential_no_billing_account")
		return
	}
	currencyQuote, err := h.storefrontCurrency(c, req.Currency)
	if err != nil {
		status, code, message := currencyRequestError(err)
		response.Error(c, status, code, message)
		return
	}
	var resolved service.ResolvedLine
	err = h.DB.Transaction(func(tx *gorm.DB) error {
		var resolveErr error
		resolved, resolveErr = service.ResolveLinePricingForReseller(tx, productID, variantID, userID, resellerID, req.Quantity)
		return resolveErr
	})
	if errors.Is(err, service.ErrProductUnavailable) || errors.Is(err, service.ErrVariantRequired) || errors.Is(err, service.ErrVariantUnavailable) || errors.Is(err, service.ErrResellerProductUnavailable) {
		response.Error(c, 422, 42207, "error.valid_spec_and_quantity_required")
		return
	}
	if err != nil {
		response.Error(c, 500, 50020, "error.shop_product_price_fetch_failed")
		return
	}
	result, err := openAPIProductQuoteFromResolved(resolved, currencyQuote.Conversion, time.Now().UTC())
	if errors.Is(err, service.ErrCurrencyMismatch) {
		response.Error(c, 500, 50066, "error.currency_quote_failed")
		return
	}
	if err != nil {
		response.Error(c, 503, 50366, "error.currency_rate_unavailable")
		return
	}
	c.Header("Cache-Control", "no-store")
	response.OK(c, result)
}
