package currency

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"time"

	"linlinqi/api/internal/model"
)

var currencyCodePattern = regexp.MustCompile(`^[A-Z]{3}$`)

type ProviderQuote struct {
	BaseCode   string
	QuoteCode  string
	Rate       string
	ObservedAt time.Time
	RawHash    string
}

type Provider interface {
	Quote(context.Context, string, string) (ProviderQuote, error)
}

func NewProvider(config model.FXProviderConfig, allowPrivate bool) (Provider, error) {
	switch config.Driver {
	case "frankfurter-v2":
		return newFrankfurterProvider(config, allowPrivate), nil
	case "exchangerate-api-open":
		return newExchangeRateAPIOpenProvider(config, allowPrivate), nil
	default:
		return nil, errors.New("exchange-rate provider driver is unsupported")
	}
}

func normalizePair(baseCode, quoteCode string) (string, string, error) {
	baseCode, quoteCode = strings.ToUpper(strings.TrimSpace(baseCode)), strings.ToUpper(strings.TrimSpace(quoteCode))
	if !currencyCodePattern.MatchString(baseCode) || !currencyCodePattern.MatchString(quoteCode) {
		return "", "", errors.New("currency code is invalid")
	}
	return baseCode, quoteCode, nil
}
