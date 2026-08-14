package supply

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"time"
)

// smmGateway currently exposes catalog and balance discovery only. SMM orders
// have lifecycle semantics (links, partials, cancellations and refills) that
// cannot be represented as a virtual-card delivery. Procurement remains gated
// until the dedicated service-fulfillment aggregate is enabled.
type smmGateway struct {
	transport *protocolTransport
	key       string
	money     MoneySpec
}

func newSMMGateway(baseURL string, credentials map[string]string, allowPrivate bool, money MoneySpec) *smmGateway {
	return &smmGateway{transport: newProtocolTransport(baseURL, allowPrivate), key: credentials["api_key"], money: money}
}

func (g *smmGateway) call(ctx context.Context, action string) (any, error) {
	payload, _, err := g.transport.do(ctx, http.MethodPost, "/api/v2", nil, []byte(url.Values{"key": {g.key}, "action": {action}}.Encode()), "application/x-www-form-urlencoded", nil)
	if err != nil {
		return nil, err
	}
	if action == "services" {
		return decodeArray(payload)
	}
	return decodeObject(payload)
}

func (g *smmGateway) Categories(context.Context) ([]Category, error) {
	return nil, ErrCapabilityUnsupported
}

func (g *smmGateway) Products(ctx context.Context) ([]Product, error) {
	raw, err := g.call(ctx, "services")
	if err != nil {
		return nil, err
	}
	items, ok := raw.([]any)
	if !ok {
		return nil, errors.New("supplier service response invalid")
	}
	products := make([]Product, 0, len(items))
	for _, rawItem := range items {
		item, ok := rawItem.(map[string]any)
		if !ok {
			continue
		}
		rate, err := decimalMoneyValue(item, g.money.PriceMinorUnit, "rate")
		if err != nil {
			return nil, err
		}
		// SMM rate is normally quoted per 1000 units. It is retained as the
		// upstream cost snapshot and must not be interpreted as a card unit cost.
		products = append(products, Product{ID: stringValue(item, "service"), ExternalID: stringValue(item, "service"), ExternalCategoryID: stringValue(item, "category"), Name: stringValue(item, "name"), Description: stringValue(item, "description"), Price: rate, Stock: 1_000_000_000, Minimum: int(intValue(item, "min")), Maximum: int(intValue(item, "max")), Status: "active"})
	}
	return normalizeProducts(products)
}

func (g *smmGateway) Balance(ctx context.Context) (BalanceSnapshot, error) {
	raw, err := g.call(ctx, "balance")
	if err != nil {
		return BalanceSnapshot{}, err
	}
	response, ok := raw.(map[string]any)
	if !ok {
		return BalanceSnapshot{}, errors.New("supplier balance response invalid")
	}
	balance, err := decimalMinorUnits(stringValue(response, "balance"), g.money.BalanceMinorUnit)
	if err != nil {
		return BalanceSnapshot{}, err
	}
	currencyCode := normalizedCurrencyCode(stringValue(response, "currency"))
	if currencyCode == "" {
		currencyCode = g.money.BalanceCurrency
	}
	return BalanceSnapshot{Balance: balance, Currency: currencyCode, UpdatedAt: time.Now().UTC()}, nil
}

func (g *smmGateway) CreateOrder(context.Context, CreateOrderRequest) (OrderResult, error) {
	return OrderResult{}, ErrCapabilityUnsupported
}

func (g *smmGateway) Order(context.Context, string) (OrderResult, error) {
	return OrderResult{}, ErrCapabilityUnsupported
}

var _ Gateway = (*smmGateway)(nil)
