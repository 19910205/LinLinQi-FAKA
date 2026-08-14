package currency

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"time"

	"linlinqi/api/internal/model"
)

type frankfurterProvider struct {
	http        *providerHTTP
	providerKey string
}

func newFrankfurterProvider(config model.FXProviderConfig, allowPrivate bool) *frankfurterProvider {
	timeout := time.Duration(config.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 8 * time.Second
	}
	return &frankfurterProvider{http: newProviderHTTP(config.BaseURL, timeout, allowPrivate), providerKey: strings.TrimSpace(config.ProviderKey)}
}

func (provider *frankfurterProvider) Quote(ctx context.Context, baseCode, quoteCode string) (ProviderQuote, error) {
	baseCode, quoteCode, err := normalizePair(baseCode, quoteCode)
	if err != nil {
		return ProviderQuote{}, err
	}
	query := url.Values{}
	if provider.providerKey != "" {
		query.Set("providers", provider.providerKey)
	}
	payload, rawHash, err := provider.http.get(ctx, "/v2/rate/"+baseCode+"/"+quoteCode, query)
	if err != nil {
		return ProviderQuote{}, err
	}
	var response struct {
		Date  string          `json:"date"`
		Base  string          `json:"base"`
		Quote string          `json:"quote"`
		Rate  json.RawMessage `json:"rate"`
	}
	if json.Unmarshal(payload, &response) != nil || response.Base != baseCode || response.Quote != quoteCode {
		return ProviderQuote{}, errors.New("exchange-rate response pair is invalid")
	}
	rate := strings.Trim(string(response.Rate), `"`)
	if _, err := ParseRate(rate); err != nil {
		return ProviderQuote{}, err
	}
	observedAt, err := time.Parse("2006-01-02", response.Date)
	if err != nil {
		return ProviderQuote{}, errors.New("exchange-rate response date is invalid")
	}
	return ProviderQuote{BaseCode: baseCode, QuoteCode: quoteCode, Rate: rate, ObservedAt: observedAt.UTC(), RawHash: rawHash}, nil
}
