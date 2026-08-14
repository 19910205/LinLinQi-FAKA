package currency

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strconv"
	"time"

	"linlinqi/api/internal/model"
)

type exchangeRateAPIOpenProvider struct{ http *providerHTTP }

func newExchangeRateAPIOpenProvider(config model.FXProviderConfig, allowPrivate bool) *exchangeRateAPIOpenProvider {
	timeout := time.Duration(config.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 8 * time.Second
	}
	return &exchangeRateAPIOpenProvider{http: newProviderHTTP(config.BaseURL, timeout, allowPrivate)}
}

func (provider *exchangeRateAPIOpenProvider) Quote(ctx context.Context, baseCode, quoteCode string) (ProviderQuote, error) {
	baseCode, quoteCode, err := normalizePair(baseCode, quoteCode)
	if err != nil {
		return ProviderQuote{}, err
	}
	payload, rawHash, err := provider.http.get(ctx, "/v6/latest/"+baseCode, url.Values{})
	if err != nil {
		return ProviderQuote{}, err
	}
	var response struct {
		Result         string             `json:"result"`
		BaseCode       string             `json:"base_code"`
		LastUpdateUnix int64              `json:"time_last_update_unix"`
		Rates          map[string]float64 `json:"rates"`
	}
	if json.Unmarshal(payload, &response) != nil || response.Result != "success" || response.BaseCode != baseCode || response.Rates[quoteCode] <= 0 || response.LastUpdateUnix <= 0 {
		return ProviderQuote{}, errors.New("exchange-rate response is invalid")
	}
	rate := strconv.FormatFloat(response.Rates[quoteCode], 'f', -1, 64)
	if _, err := ParseRate(rate); err != nil {
		return ProviderQuote{}, err
	}
	return ProviderQuote{BaseCode: baseCode, QuoteCode: quoteCode, Rate: rate, ObservedAt: time.Unix(response.LastUpdateUnix, 0).UTC(), RawHash: rawHash}, nil
}
