package supply

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"
	"time"
)

var ErrCapabilityUnsupported = errors.New("supplier protocol capability is unsupported")

// Gateway is the normalized boundary consumed by the LinLinQi catalog sync
// and procurement workers. Protocol adapters must convert upstream amounts to
// the supplier protocol's documented minor-unit representation and must never
// include credentials or raw upstream response bodies in returned errors.
type Gateway interface {
	Categories(context.Context) ([]Category, error)
	Products(context.Context) ([]Product, error)
	Balance(context.Context) (BalanceSnapshot, error)
	CreateOrder(context.Context, CreateOrderRequest) (OrderResult, error)
	Order(context.Context, string) (OrderResult, error)
}

// ProductDetailReader and StockReader are optional capabilities. Adapters
// implement them only when the upstream protocol has a real operation; callers
// must use an interface assertion instead of trusting a hand-written label.
type ProductDetailReader interface {
	Product(context.Context, ProductDetailRequest) (Product, error)
}

type ProductDetailRequest struct {
	ExternalProductID string `json:"external_product_id"`
	ParentExternalID  string `json:"parent_external_id,omitempty"`
}

type StockRequest struct {
	ExternalProductID string         `json:"external_product_id"`
	ParentExternalID  string         `json:"parent_external_id,omitempty"`
	VariantID         string         `json:"variant_id,omitempty"`
	Quantity          int            `json:"quantity,omitempty"`
	Parameters        map[string]any `json:"parameters,omitempty"`
}

type StockSnapshot struct {
	ExternalProductID string    `json:"external_product_id"`
	VariantID         string    `json:"variant_id,omitempty"`
	Stock             int64     `json:"stock"`
	StockStatus       string    `json:"stock_status"`
	ObservedAt        time.Time `json:"observed_at"`
}

type StockReader interface {
	Stock(context.Context, StockRequest) (StockSnapshot, error)
}

// OrderCanceller is implemented only when an upstream exposes a documented,
// idempotent cancellation operation. A protocol must not advertise
// order_cancel unless its runtime gateway satisfies this interface.
type OrderCanceller interface {
	CancelOrder(context.Context, string) (OrderResult, error)
}

type QuoteRequest struct {
	ExternalProductID string            `json:"external_product_id"`
	ParentExternalID  string            `json:"parent_external_id,omitempty"`
	VariantID         string            `json:"variant_id,omitempty"`
	Quantity          int               `json:"quantity"`
	Currency          string            `json:"currency,omitempty"`
	Parameters        map[string]string `json:"parameters,omitempty"`
}

type QuoteSnapshot struct {
	Amount    int64     `json:"amount"`
	Currency  string    `json:"currency"`
	MinorUnit int       `json:"minor_unit"`
	QuotedAt  time.Time `json:"quoted_at"`
}

type PriceQuoter interface {
	Quote(context.Context, QuoteRequest) (QuoteSnapshot, error)
}

type DraftCardRequest struct {
	ExternalProductID string            `json:"external_product_id"`
	Page              int               `json:"page,omitempty"`
	PageSize          int               `json:"page_size,omitempty"`
	Parameters        map[string]string `json:"parameters,omitempty"`
}

type DraftCard struct {
	ID      string `json:"id"`
	Preview string `json:"preview"`
}

type DraftCardPage struct {
	Items    []DraftCard `json:"items"`
	Page     int         `json:"page"`
	PageSize int         `json:"page_size"`
}

type DraftCardReader interface {
	DraftCards(context.Context, DraftCardRequest) (DraftCardPage, error)
}

// MoneySpec describes the canonical integer unit expected at the adapter
// boundary. Upstream protocols generally transmit decimal major units, while
// LinLinQi stores and calculates only exact minor-unit integers.
type MoneySpec struct {
	PriceCurrency    string
	PriceMinorUnit   int
	BalanceCurrency  string
	BalanceMinorUnit int
}

func normalizeMoneySpec(spec MoneySpec) (MoneySpec, error) {
	spec.PriceCurrency = strings.ToUpper(strings.TrimSpace(spec.PriceCurrency))
	spec.BalanceCurrency = strings.ToUpper(strings.TrimSpace(spec.BalanceCurrency))
	if spec.PriceMinorUnit < 0 || spec.PriceMinorUnit > 6 || spec.BalanceMinorUnit < 0 || spec.BalanceMinorUnit > 6 {
		return MoneySpec{}, errors.New("supplier money specification is invalid")
	}
	for _, code := range []string{spec.PriceCurrency, spec.BalanceCurrency} {
		if code != "" && (len(code) != 3 || strings.IndexFunc(code, func(character rune) bool { return character < 'A' || character > 'Z' }) >= 0) {
			return MoneySpec{}, errors.New("supplier money currency is invalid")
		}
	}
	return spec, nil
}

func NewGateway(protocol, baseURL string, credentials map[string]string, allowPrivate bool) (Gateway, error) {
	return NewGatewayWithMoney(protocol, baseURL, credentials, allowPrivate, MoneySpec{PriceMinorUnit: 2, BalanceMinorUnit: 2})
}

func NewGatewayWithMoney(protocol, baseURL string, credentials map[string]string, allowPrivate bool, money MoneySpec) (Gateway, error) {
	protocol = strings.ToLower(strings.TrimSpace(protocol))
	if !Executable(protocol) {
		return nil, fmt.Errorf("supplier protocol is unavailable")
	}
	var err error
	money, err = normalizeMoneySpec(money)
	if err != nil {
		return nil, err
	}
	normalized, err := ValidateCredentials(protocol, credentials)
	if err != nil {
		return nil, err
	}
	if shopcloneProtocols[protocol] != nil {
		return newShopcloneGateway(protocol, baseURL, normalized, allowPrivate, money), nil
	}
	switch protocol {
	case "linlinqi-standard":
		return NewClient(baseURL, normalized["api_key"], normalized["api_secret"], allowPrivate), nil
	case "dujiao-next":
		return newDujiaoNextGateway(baseURL, normalized, allowPrivate, money), nil
	case "acg-faka-new", "acg-faka-old", "vendor-fakawang":
		return newACGGateway(protocol, baseURL, normalized, allowPrivate, money), nil
	case "5gsmm":
		return newSMMGateway(baseURL, normalized, allowPrivate, money), nil
	default:
		return nil, fmt.Errorf("supplier protocol runtime adapter is unavailable")
	}
}

func normalizeProduct(product Product) (Product, error) {
	externalID := product.ExternalID
	if strings.TrimSpace(externalID) == "" {
		externalID = product.ID
	}
	var identityErr error
	product.ExternalID, identityErr = NormalizeExternalID(externalID)
	if identityErr != nil {
		return Product{}, errors.New("upstream product identifier is invalid")
	}
	product.Name = strings.TrimSpace(product.Name)
	product.ParentExternalID, identityErr = NormalizeOptionalExternalID(product.ParentExternalID)
	if identityErr != nil || product.ParentExternalID == product.ExternalID {
		return Product{}, errors.New("upstream product parent identifier is invalid")
	}
	product.ExternalCategoryID, identityErr = NormalizeOptionalExternalID(product.ExternalCategoryID)
	if identityErr != nil {
		return Product{}, errors.New("upstream product category identifier is invalid")
	}
	product.Summary = strings.TrimSpace(product.Summary)
	product.Description = strings.TrimSpace(product.Description)
	product.CoverURL = strings.TrimSpace(product.CoverURL)
	product.Currency = strings.ToUpper(strings.TrimSpace(product.Currency))
	product.Country = strings.ToUpper(strings.TrimSpace(product.Country))
	product.StockStatus = strings.ToLower(strings.TrimSpace(product.StockStatus))
	product.FulfillmentType = strings.ToLower(strings.TrimSpace(product.FulfillmentType))
	switch product.StockStatus {
	case "", "unknown":
	case "in_stock", "available", "instock", "stock":
		product.StockStatus = "in_stock"
	case "out_of_stock", "unavailable", "sold_out", "outofstock":
		product.StockStatus = "out_of_stock"
	default:
		product.StockStatus = "unknown"
	}
	switch product.FulfillmentType {
	case "auto", "automatic", "instant", "digital":
		product.FulfillmentType = "auto"
	case "manual":
		product.FulfillmentType = "manual"
	default:
		product.FulfillmentType = ""
	}
	product.Status = strings.ToLower(strings.TrimSpace(product.Status))
	if product.Status == "" {
		product.Status = "active"
	}
	if product.Minimum == 0 {
		product.Minimum = 1
	}
	if product.StockStatus == "" {
		if product.Stock == 0 {
			product.StockStatus = "out_of_stock"
		} else {
			product.StockStatus = "in_stock"
		}
	}
	if len(product.WholesalePrices) == 0 || string(product.WholesalePrices) == "null" {
		product.WholesalePrices = json.RawMessage(`{}`)
	} else {
		var wholesaleObject map[string]any
		if json.Unmarshal(product.WholesalePrices, &wholesaleObject) != nil || wholesaleObject == nil {
			return Product{}, errors.New("upstream product response invalid")
		}
	}
	if product.ExternalID == "" || product.Name == "" || product.Price < 0 || product.OriginalPrice < 0 || product.MemberPrice < 0 || product.Stock < -1 || product.Minimum < 1 || product.Maximum < 0 || (product.Maximum > 0 && product.Maximum < product.Minimum) || (product.Status != "active" && product.Status != "inactive") || (product.StockStatus != "in_stock" && product.StockStatus != "out_of_stock" && product.StockStatus != "unknown") || (product.Currency != "" && (len(product.Currency) != 3 || strings.IndexFunc(product.Currency, func(character rune) bool { return character < 'A' || character > 'Z' }) >= 0)) || (product.Country != "" && len(product.Country) > 8) {
		return Product{}, errors.New("upstream product response invalid")
	}
	if product.Stock == -1 {
		product.Stock = 1_000_000_000
	}
	seenImages := map[string]struct{}{}
	images := make([]string, 0, len(product.ImageURLs)+1)
	for _, imageURL := range append([]string{product.CoverURL}, product.ImageURLs...) {
		imageURL = strings.TrimSpace(imageURL)
		if imageURL == "" {
			continue
		}
		if _, exists := seenImages[imageURL]; exists {
			continue
		}
		seenImages[imageURL] = struct{}{}
		images = append(images, imageURL)
	}
	product.ImageURLs = images
	if product.CoverURL == "" && len(images) > 0 {
		product.CoverURL = images[0]
	}
	seenTags := map[string]struct{}{}
	tags := make([]string, 0, len(product.Tags))
	for _, tag := range product.Tags {
		tag = strings.TrimSpace(tag)
		if tag == "" || len([]rune(tag)) > 100 {
			continue
		}
		key := strings.ToLower(tag)
		if _, exists := seenTags[key]; exists {
			continue
		}
		seenTags[key] = struct{}{}
		tags = append(tags, tag)
		if len(tags) == 50 {
			break
		}
	}
	product.Tags = tags
	return product, nil
}

func normalizeProducts(products []Product) ([]Product, error) {
	result := make([]Product, 0, len(products))
	seen := make(map[string]struct{}, len(products))
	for _, product := range products {
		normalized, err := normalizeProduct(product)
		if err != nil {
			return nil, err
		}
		if _, exists := seen[normalized.ExternalID]; exists {
			return nil, errors.New("upstream product identifiers are not unique")
		}
		seen[normalized.ExternalID] = struct{}{}
		result = append(result, normalized)
	}
	return result, nil
}

func splitDeliveries(value string) []string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	separator := "\n"
	if !strings.Contains(value, "\n") && strings.Contains(value, ",") {
		separator = ","
	}
	parts := strings.Split(value, separator)
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if item := strings.TrimSpace(part); item != "" {
			result = append(result, item)
		}
	}
	return result
}

// DecimalToMinorUnits converts a non-negative base-10 major-unit amount into
// an exact minor-unit integer. It never uses binary floating point and rejects
// values that cannot be represented at the requested precision.
func DecimalToMinorUnits(value string, exponent int) (int64, error) {
	value = strings.TrimSpace(value)
	if exponent < 0 || exponent > 6 || value == "" || strings.Contains(value, "/") {
		return 0, errors.New("upstream amount invalid")
	}
	amount, ok := new(big.Rat).SetString(value)
	if !ok || amount.Sign() < 0 {
		return 0, errors.New("upstream amount invalid")
	}
	scale := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(exponent)), nil)
	amount.Mul(amount, new(big.Rat).SetInt(scale))
	if !amount.IsInt() {
		return 0, errors.New("upstream amount precision invalid")
	}
	minor := amount.Num()
	if !minor.IsInt64() {
		return 0, errors.New("upstream amount invalid")
	}
	return minor.Int64(), nil
}

func decimalMinorUnits(value string, exponent int) (int64, error) {
	return DecimalToMinorUnits(value, exponent)
}

func decimalNumericText(value any) (string, error) {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed), nil
	case json.Number:
		return typed.String(), nil
	case int:
		return strconv.Itoa(typed), nil
	case int8:
		return strconv.FormatInt(int64(typed), 10), nil
	case int16:
		return strconv.FormatInt(int64(typed), 10), nil
	case int32:
		return strconv.FormatInt(int64(typed), 10), nil
	case int64:
		return strconv.FormatInt(typed, 10), nil
	case uint:
		return strconv.FormatUint(uint64(typed), 10), nil
	case uint8:
		return strconv.FormatUint(uint64(typed), 10), nil
	case uint16:
		return strconv.FormatUint(uint64(typed), 10), nil
	case uint32:
		return strconv.FormatUint(uint64(typed), 10), nil
	case uint64:
		return strconv.FormatUint(typed, 10), nil
	case float32:
		if math.IsNaN(float64(typed)) || math.IsInf(float64(typed), 0) {
			return "", errors.New("upstream amount invalid")
		}
		return strconv.FormatFloat(float64(typed), 'g', -1, 32), nil
	case float64:
		if math.IsNaN(typed) || math.IsInf(typed, 0) {
			return "", errors.New("upstream amount invalid")
		}
		return strconv.FormatFloat(typed, 'g', -1, 64), nil
	default:
		return "", errors.New("upstream amount invalid")
	}
}

func decimalValueToMinorUnits(value any, exponent int) (int64, error) {
	text, err := decimalNumericText(value)
	if err != nil {
		return 0, err
	}
	return DecimalToMinorUnits(text, exponent)
}

// legacyAmountToMinorUnits converts the documented ShopClone/ACG-style major
// unit wire amount to an exact minor-unit integer. JSON 1 and JSON 1.00 both
// mean one unit of the supplier currency (for example USD 1.00 => 100 cents);
// interpreting an integer token as cents would undercharge by 100x.
func legacyAmountToMinorUnits(value any, decimalExponent int) (int64, error) {
	text, err := decimalNumericText(value)
	if err != nil {
		return 0, err
	}
	return DecimalToMinorUnits(text, decimalExponent)
}
