package service

import (
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	fx "linlinqi/api/internal/currency"
	"linlinqi/api/internal/model"
)

var ErrFXQuoteUnavailable = errors.New("checkout exchange-rate quote is unavailable")

// CheckoutFX identifies the immutable rate used to turn the store-currency
// price into the currency selected by the buyer. Amounts remain integer minor
// units; Rate is an exact decimal string and must never be parsed as float64.
type CheckoutFX struct {
	SnapshotID     *uuid.UUID `json:"snapshot_id,omitempty"`
	SourceCurrency string     `json:"source_currency"`
	TargetCurrency string     `json:"target_currency"`
	Rate           string     `json:"rate"`
	SourceTier     string     `json:"source_tier"`
	ExpiresAt      *time.Time `json:"expires_at,omitempty"`
}

type CheckoutCurrencyConversion struct {
	Source   model.CurrencyDefinition
	Target   model.CurrencyDefinition
	Snapshot *model.FXRateSnapshot
}

func LoadCheckoutCurrencyConversion(tx *gorm.DB, sourceCode, targetCode string, snapshotID *uuid.UUID, now time.Time) (CheckoutCurrencyConversion, error) {
	sourceCode = strings.ToUpper(strings.TrimSpace(sourceCode))
	targetCode = strings.ToUpper(strings.TrimSpace(targetCode))
	if targetCode == "" {
		targetCode = sourceCode
	}
	var source, target model.CurrencyDefinition
	if err := tx.Where("code = ? AND enabled = ?", sourceCode, true).First(&source).Error; err != nil {
		return CheckoutCurrencyConversion{}, ErrFXQuoteUnavailable
	}
	if sourceCode == targetCode {
		return CheckoutCurrencyConversion{Source: source, Target: source}, nil
	}
	if snapshotID == nil {
		return CheckoutCurrencyConversion{}, ErrFXQuoteUnavailable
	}
	if err := tx.Where("code = ? AND enabled = ?", targetCode, true).First(&target).Error; err != nil {
		return CheckoutCurrencyConversion{}, ErrFXQuoteUnavailable
	}
	var snapshot model.FXRateSnapshot
	if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).First(&snapshot, "id = ?", *snapshotID).Error; err != nil {
		return CheckoutCurrencyConversion{}, ErrFXQuoteUnavailable
	}
	if snapshot.BaseCode != source.Code || snapshot.QuoteCode != target.Code || !snapshot.ExpiresAt.After(now.UTC()) {
		return CheckoutCurrencyConversion{}, ErrFXQuoteUnavailable
	}
	return CheckoutCurrencyConversion{Source: source, Target: target, Snapshot: &snapshot}, nil
}

func (conversion CheckoutCurrencyConversion) FX() CheckoutFX {
	result := CheckoutFX{SourceCurrency: conversion.Source.Code, TargetCurrency: conversion.Target.Code, Rate: "1", SourceTier: "system"}
	if conversion.Snapshot != nil {
		id, expires := conversion.Snapshot.ID, conversion.Snapshot.ExpiresAt.UTC()
		result.SnapshotID, result.Rate, result.SourceTier, result.ExpiresAt = &id, conversion.Snapshot.Rate, conversion.Snapshot.SourceTier, &expires
	}
	return result
}

func (conversion CheckoutCurrencyConversion) Amount(amount int64) (int64, error) {
	if amount < 0 {
		return 0, errors.New("money amount is invalid")
	}
	if conversion.Source.Code == conversion.Target.Code {
		return amount, nil
	}
	if conversion.Snapshot == nil {
		return 0, ErrFXQuoteUnavailable
	}
	return fx.Convert(amount, conversion.Source.MinorUnit, conversion.Target.MinorUnit, conversion.Snapshot.Rate)
}

func (conversion CheckoutCurrencyConversion) signedAmount(amount int64) (int64, error) {
	negative := amount < 0
	if negative {
		amount = -amount
	}
	converted, err := conversion.Amount(amount)
	if negative {
		converted = -converted
	}
	return converted, err
}

func (conversion CheckoutCurrencyConversion) PriceQuote(source PriceQuote) (PriceQuote, error) {
	result := source
	var err error
	if result.UnitPrice, err = conversion.Amount(source.UnitPrice); err != nil {
		return PriceQuote{}, err
	}
	if result.Subtotal, err = conversion.Amount(source.Subtotal); err != nil {
		return PriceQuote{}, err
	}
	if result.Discount, err = conversion.Amount(source.Discount); err != nil {
		return PriceQuote{}, err
	}
	if result.Discount > result.Subtotal {
		result.Discount = result.Subtotal
	}
	result.Total = result.Subtotal - result.Discount
	result.Adjustments = make([]PriceAdjustment, 0, len(source.Adjustments))
	for _, adjustment := range source.Adjustments {
		adjustment.Amount, err = conversion.signedAmount(adjustment.Amount)
		if err != nil {
			return PriceQuote{}, err
		}
		result.Adjustments = append(result.Adjustments, adjustment)
	}
	return result, nil
}

func ConvertCheckoutQuote(source CheckoutQuote, conversion CheckoutCurrencyConversion) (CheckoutQuote, error) {
	result := source
	result.Currency = conversion.Target.Code
	result.FX = conversion.FX()
	result.Lines = make([]CheckoutLineQuote, 0, len(source.Lines))
	result.Subtotal, result.Discount, result.ResellerMargin = 0, 0, 0
	for _, line := range source.Lines {
		converted, err := conversion.PriceQuote(line.Quote)
		if err != nil {
			return CheckoutQuote{}, err
		}
		line.Quote = converted
		result.Lines = append(result.Lines, line)
		result.Subtotal, err = checkedAddInt64(result.Subtotal, converted.Subtotal)
		if err != nil {
			return CheckoutQuote{}, err
		}
		result.Discount, err = checkedAddInt64(result.Discount, converted.Discount)
		if err != nil {
			return CheckoutQuote{}, err
		}
	}
	var err error
	if result.CouponDiscount, err = conversion.Amount(source.CouponDiscount); err != nil {
		return CheckoutQuote{}, err
	}
	if result.Discount, err = checkedAddInt64(result.Discount, result.CouponDiscount); err != nil {
		return CheckoutQuote{}, err
	}
	netAmount := result.Subtotal - result.Discount
	if netAmount < 1 {
		return CheckoutQuote{}, ErrFXQuoteUnavailable
	}
	if result.Fee, err = roundedNearestRatio(netAmount, int64(source.FeeRate), 10000); err != nil {
		return CheckoutQuote{}, err
	}
	if result.ResellerMargin, err = conversion.Amount(source.ResellerMargin); err != nil {
		return CheckoutQuote{}, err
	}
	result.Total, err = checkedAddInt64(result.Subtotal-result.Discount, result.Fee)
	if err != nil || result.Total < 1 {
		return CheckoutQuote{}, ErrFXQuoteUnavailable
	}
	result.Adjustments = make([]PriceAdjustment, 0, len(source.Adjustments))
	for _, adjustment := range source.Adjustments {
		adjustment.Amount, err = conversion.signedAmount(adjustment.Amount)
		if err != nil {
			return CheckoutQuote{}, err
		}
		result.Adjustments = append(result.Adjustments, adjustment)
	}
	return result, nil
}

type convertedOrderPricing struct {
	Line           ResolvedLine
	Currency       string
	Subtotal       int64
	Discount       int64
	Total          int64
	ResellerMargin int64
	Adjustments    json.RawMessage
	FXSnapshotID   *uuid.UUID
}

func convertSingleOrderPricing(tx *gorm.DB, input CreateOrderInput, line ResolvedLine, coupon ResolvedCoupon) (convertedOrderPricing, error) {
	sourceCurrency, err := normalizedProductCurrency(line.Product)
	if err != nil {
		return convertedOrderPricing{}, err
	}
	conversion, err := LoadCheckoutCurrencyConversion(tx, sourceCurrency, input.Currency, input.FXSnapshotID, time.Now())
	if err != nil {
		return convertedOrderPricing{}, err
	}
	line.Quote, err = conversion.PriceQuote(line.Quote)
	if err != nil {
		return convertedOrderPricing{}, err
	}
	line.PlatformUnitPrice, err = conversion.Amount(line.PlatformUnitPrice)
	if err != nil {
		return convertedOrderPricing{}, err
	}
	line.ResellerMargin, err = conversion.Amount(line.ResellerMargin)
	if err != nil {
		return convertedOrderPricing{}, err
	}
	line.Product.Currency = conversion.Target.Code
	couponDiscount, err := conversion.Amount(coupon.Discount)
	if err != nil {
		return convertedOrderPricing{}, err
	}
	discount, err := checkedAddInt64(line.Quote.Discount, couponDiscount)
	if err != nil {
		return convertedOrderPricing{}, err
	}
	netAmount := line.Quote.Subtotal - discount
	if netAmount < 1 {
		return convertedOrderPricing{}, ErrProductUnavailable
	}
	fee, err := roundedNearestRatio(netAmount, int64(input.FeeRate), 10000)
	if err != nil {
		return convertedOrderPricing{}, err
	}
	total, err := checkedAddInt64(netAmount, fee)
	if err != nil {
		return convertedOrderPricing{}, err
	}
	adjustments := append([]PriceAdjustment{}, line.Quote.Adjustments...)
	if couponDiscount > 0 {
		adjustments = append(adjustments, PriceAdjustment{Code: "coupon", Label: "优惠券", Amount: -couponDiscount})
	}
	encoded, err := json.Marshal(adjustments)
	if err != nil {
		return convertedOrderPricing{}, err
	}
	var snapshotID *uuid.UUID
	if conversion.Snapshot != nil {
		id := conversion.Snapshot.ID
		snapshotID = &id
	}
	return convertedOrderPricing{Line: line, Currency: conversion.Target.Code, Subtotal: line.Quote.Subtotal, Discount: discount, Total: total, ResellerMargin: line.ResellerMargin, Adjustments: encoded, FXSnapshotID: snapshotID}, nil
}
