package handler

import (
	"errors"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	fx "linlinqi/api/internal/currency"
	"linlinqi/api/internal/model"
	"linlinqi/api/internal/service"
)

type storefrontCurrencyQuote struct {
	Conversion service.CheckoutCurrencyConversion
}

func (h Handler) storefrontCurrency(c *gin.Context, requested string) (storefrontCurrencyQuote, error) {
	sourceCode, err := service.StoreCurrency(h.DB)
	if err != nil {
		return storefrontCurrencyQuote{}, err
	}
	targetCode := strings.ToUpper(strings.TrimSpace(requested))
	if targetCode == "" {
		targetCode = sourceCode
	}
	if !isoCurrencyCodePattern.MatchString(targetCode) {
		return storefrontCurrencyQuote{}, errCurrencySelectionInvalid
	}
	var source, target model.CurrencyDefinition
	if err := h.DB.Where("code = ? AND enabled = ?", sourceCode, true).First(&source).Error; err != nil {
		return storefrontCurrencyQuote{}, err
	}
	if err := h.DB.Where("code = ? AND enabled = ?", targetCode, true).First(&target).Error; err != nil {
		return storefrontCurrencyQuote{}, errCurrencySelectionUnavailable
	}
	conversion := service.CheckoutCurrencyConversion{Source: source, Target: target}
	if source.Code != target.Code {
		var snapshot model.FXRateSnapshot
		now := time.Now().UTC()
		err := h.DB.Where("base_code = ? AND quote_code = ? AND expires_at > ?", source.Code, target.Code, now).
			Order("selected_at DESC").First(&snapshot).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return storefrontCurrencyQuote{}, err
		}
		if errors.Is(err, gorm.ErrRecordNotFound) {
			manager := fx.Manager{DB: h.DB, AllowPrivate: h.Cfg.Env != "production"}
			snapshot, err = manager.Resolve(c.Request.Context(), source.Code, target.Code)
			if err != nil {
				return storefrontCurrencyQuote{}, service.ErrFXQuoteUnavailable
			}
		}
		if !snapshot.ExpiresAt.After(now) {
			return storefrontCurrencyQuote{}, service.ErrFXQuoteUnavailable
		}
		conversion.Snapshot = &snapshot
	}
	return storefrontCurrencyQuote{Conversion: conversion}, nil
}

func currencyRequestError(err error) (int, int, string) {
	switch {
	case errors.Is(err, errCurrencySelectionInvalid):
		return 422, 42266, "error.currency_code_invalid"
	case errors.Is(err, errCurrencySelectionUnavailable):
		return 422, 42266, "error.currency_unavailable"
	case errors.Is(err, service.ErrFXQuoteUnavailable):
		return 503, 50366, "error.currency_rate_unavailable"
	default:
		return 500, 50066, "error.currency_quote_failed"
	}
}

func convertPublicProductDTO(dto publicProductDTO, conversion service.CheckoutCurrencyConversion) (publicProductDTO, error) {
	var err error
	dto.SourceCurrency = conversion.Source.Code
	dto.Currency = conversion.Target.Code
	dto.FX = conversion.FX()
	if dto.Price, err = conversion.Amount(dto.Price); err != nil {
		return publicProductDTO{}, err
	}
	if dto.ComparePrice, err = conversion.Amount(dto.ComparePrice); err != nil {
		return publicProductDTO{}, err
	}
	return dto, nil
}

func convertPublicVariantDTO(dto publicProductVariantDTO, conversion service.CheckoutCurrencyConversion) (publicProductVariantDTO, error) {
	var err error
	if dto.Price, err = conversion.Amount(dto.Price); err != nil {
		return publicProductVariantDTO{}, err
	}
	if dto.ComparePrice, err = conversion.Amount(dto.ComparePrice); err != nil {
		return publicProductVariantDTO{}, err
	}
	return dto, nil
}
