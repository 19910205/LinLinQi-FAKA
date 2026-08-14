package supply

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
)

type jsonObject map[string]any

func objectValue(object jsonObject, keys ...string) any {
	for _, wanted := range keys {
		for key, value := range object {
			if strings.EqualFold(key, wanted) {
				return value
			}
		}
	}
	return nil
}

func object(object jsonObject, keys ...string) jsonObject {
	value, _ := objectValue(object, keys...).(map[string]any)
	return value
}

func array(object jsonObject, keys ...string) []any {
	value, _ := objectValue(object, keys...).([]any)
	return value
}

func stringValue(object jsonObject, keys ...string) string {
	value := objectValue(object, keys...)
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case json.Number:
		return typed.String()
	case float64:
		if typed == float64(int64(typed)) {
			return strconv.FormatInt(int64(typed), 10)
		}
		return strconv.FormatFloat(typed, 'f', -1, 64)
	default:
		return ""
	}
}

func intValue(object jsonObject, keys ...string) int64 {
	value := objectValue(object, keys...)
	switch typed := value.(type) {
	case json.Number:
		integer, err := typed.Int64()
		if err != nil {
			return math.MinInt64
		}
		return integer
	case float64:
		if math.IsNaN(typed) || math.IsInf(typed, 0) || math.Trunc(typed) != typed || typed > math.MaxInt64 || typed < math.MinInt64 {
			return math.MinInt64
		}
		return int64(typed)
	case string:
		integer, err := strconv.ParseInt(strings.TrimSpace(typed), 10, 64)
		if err != nil {
			return math.MinInt64
		}
		return integer
	default:
		return 0
	}
}

// legacyMoneyValue converts ShopClone's documented major-unit amounts to
// two-decimal minor units. Invalid monetary values become negative so gateway
// validation rejects them instead of silently treating them as zero.
func legacyMoneyValue(object jsonObject, keys ...string) int64 {
	return moneyValueWithExponent(object, 2, keys...)
}

func moneyValueWithExponent(object jsonObject, exponent int, keys ...string) int64 {
	value := objectValue(object, keys...)
	if value == nil {
		return 0
	}
	minor, err := legacyAmountToMinorUnits(value, exponent)
	if err != nil {
		return -1
	}
	return minor
}

func decimalMoneyValue(object jsonObject, exponent int, keys ...string) (int64, error) {
	value := objectValue(object, keys...)
	if value == nil {
		return 0, errors.New("upstream amount missing")
	}
	return decimalValueToMinorUnits(value, exponent)
}

func boolValue(object jsonObject, keys ...string) bool {
	value := objectValue(object, keys...)
	switch typed := value.(type) {
	case bool:
		return typed
	case string:
		return strings.EqualFold(typed, "true") || typed == "1" || strings.EqualFold(typed, "success")
	case json.Number:
		return typed.String() == "1"
	case float64:
		return typed == 1
	default:
		return false
	}
}

func stringArray(value any) []string {
	items, ok := value.([]any)
	if !ok {
		if text, ok := value.(string); ok {
			return splitDeliveries(text)
		}
		return nil
	}
	result := make([]string, 0, len(items))
	for _, raw := range items {
		switch item := raw.(type) {
		case string:
			if item = strings.TrimSpace(item); item != "" {
				result = append(result, item)
			}
		case map[string]any:
			if text := stringValue(item, "account", "full_info", "Email"); text != "" {
				if password := stringValue(item, "Password"); password != "" && objectValue(item, "Email") != nil {
					text += "|" + password
				}
				result = append(result, text)
			}
		}
	}
	return result
}

func decodeObject(payload []byte) (jsonObject, error) {
	decoder := json.NewDecoder(strings.NewReader(string(payload)))
	decoder.UseNumber()
	var result jsonObject
	if err := decoder.Decode(&result); err != nil {
		return nil, errors.New("decode supplier response")
	}
	return result, nil
}

func decodeArray(payload []byte) ([]any, error) {
	decoder := json.NewDecoder(strings.NewReader(string(payload)))
	decoder.UseNumber()
	var result []any
	if err := decoder.Decode(&result); err != nil {
		return nil, errors.New("decode supplier response")
	}
	return result, nil
}

type shopcloneProtocol interface {
	catalog(context.Context, *shopcloneGateway) ([]Category, []Product, error)
	balance(context.Context, *shopcloneGateway) (protocolBalance, error)
	createOrder(context.Context, *shopcloneGateway, CreateOrderRequest) (OrderResult, error)
	order(context.Context, *shopcloneGateway, string) (OrderResult, error)
}

type protocolBalance struct {
	Amount   int64
	Currency string
}

type unsupportedShopcloneProtocol struct{}

func (unsupportedShopcloneProtocol) catalog(context.Context, *shopcloneGateway) ([]Category, []Product, error) {
	return nil, nil, ErrCapabilityUnsupported
}
func (unsupportedShopcloneProtocol) balance(context.Context, *shopcloneGateway) (protocolBalance, error) {
	return protocolBalance{}, ErrCapabilityUnsupported
}
func (unsupportedShopcloneProtocol) createOrder(context.Context, *shopcloneGateway, CreateOrderRequest) (OrderResult, error) {
	return OrderResult{}, ErrCapabilityUnsupported
}
func (unsupportedShopcloneProtocol) order(context.Context, *shopcloneGateway, string) (OrderResult, error) {
	return OrderResult{}, ErrCapabilityUnsupported
}

var shopcloneProtocols = map[string]shopcloneProtocol{}

func registerShopcloneProtocol(name string, protocol shopcloneProtocol) {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" || protocol == nil {
		panic("invalid shopclone protocol registration")
	}
	if _, exists := shopcloneProtocols[name]; exists {
		panic("duplicate shopclone protocol registration: " + name)
	}
	shopcloneProtocols[name] = protocol
}

type shopcloneGateway struct {
	protocol       string
	credentials    map[string]string
	transport      *protocolTransport
	implementation shopcloneProtocol
	money          MoneySpec
}

func newShopcloneGateway(protocol, baseURL string, credentials map[string]string, allowPrivate bool, money MoneySpec) *shopcloneGateway {
	return &shopcloneGateway{
		protocol:       protocol,
		credentials:    credentials,
		transport:      newProtocolTransport(baseURL, allowPrivate),
		implementation: shopcloneProtocols[protocol],
		money:          money,
	}
}

func (g *shopcloneGateway) getObject(ctx context.Context, path string, query url.Values, headers http.Header) (jsonObject, error) {
	payload, _, err := g.transport.do(ctx, http.MethodGet, path, query, nil, "", headers)
	if err != nil {
		return nil, err
	}
	return decodeObject(payload)
}

func (g *shopcloneGateway) postQueryObject(ctx context.Context, path string, query url.Values, headers http.Header) (jsonObject, error) {
	payload, _, err := g.transport.do(ctx, http.MethodPost, path, query, nil, "", headers)
	if err != nil {
		return nil, err
	}
	return decodeObject(payload)
}

func (g *shopcloneGateway) postFormObject(ctx context.Context, path string, form url.Values, headers http.Header) (jsonObject, error) {
	payload, _, err := g.transport.do(ctx, http.MethodPost, path, nil, []byte(form.Encode()), "application/x-www-form-urlencoded", headers)
	if err != nil {
		return nil, err
	}
	return decodeObject(payload)
}

func (g *shopcloneGateway) postJSONObject(ctx context.Context, path string, query url.Values, input any, headers http.Header) (jsonObject, error) {
	body, err := json.Marshal(input)
	if err != nil {
		return nil, errors.New("encode supplier request")
	}
	payload, _, err := g.transport.do(ctx, http.MethodPost, path, query, body, "application/json", headers)
	if err != nil {
		return nil, err
	}
	return decodeObject(payload)
}

func productFromObject(item jsonObject, categoryID string, aliases map[string][]string, minorUnits ...int) Product {
	exponent := 2
	if len(minorUnits) > 0 {
		exponent = minorUnits[0]
	}
	readString := func(name string) string { return stringValue(item, aliases[name]...) }
	readInt := func(name string) int64 { return intValue(item, aliases[name]...) }
	images := stringArray(objectValue(item, "images", "image_urls", "gallery"))
	if cover := readString("image"); cover != "" {
		images = append([]string{cover}, images...)
	}
	wholesale := json.RawMessage(`{}`)
	if value := objectValue(item, "wholesale_prices", "wholesale", "price_tiers"); value != nil {
		if encoded, err := json.Marshal(value); err == nil && json.Valid(encoded) {
			wholesale = encoded
		}
	}
	return Product{
		ID: readString("id"), ExternalID: readString("id"), ExternalCategoryID: categoryID,
		Name: readString("name"), Summary: readString("summary"), Description: readString("description"),
		CoverURL: readString("image"), ImageURLs: images, Country: readString("country"), Tags: stringArray(objectValue(item, aliases["tags"]...)),
		Currency: currencyValue(item), Price: moneyValueWithExponent(item, exponent, aliases["price"]...),
		OriginalPrice: moneyValueWithExponent(item, exponent, aliases["original_price"]...), MemberPrice: moneyValueWithExponent(item, exponent, aliases["member_price"]...), WholesalePrices: wholesale,
		Stock: readInt("stock"), StockStatus: strings.ToLower(readString("stock_status")), Minimum: int(readInt("minimum")), Maximum: int(readInt("maximum")), FulfillmentType: strings.ToLower(readString("fulfillment_type")), Status: "active",
	}
}

func normalizedCurrencyCode(value string) string {
	value = strings.ToUpper(strings.TrimSpace(value))
	if len(value) != 3 || strings.IndexFunc(value, func(character rune) bool {
		return character < 'A' || character > 'Z'
	}) >= 0 {
		return ""
	}
	return value
}

// currencyValue accepts only explicit ISO-like three-letter codes. Invalid or
// absent upstream metadata deliberately becomes empty so the supplier's
// configured price/balance currency remains the authoritative fallback.
func currencyValue(objects ...jsonObject) string {
	for _, item := range objects {
		if item == nil {
			continue
		}
		if currency := normalizedCurrencyCode(stringValue(item,
			"currency", "currency_code", "currencyCode", "price_currency", "balance_currency", "unit_currency",
		)); currency != "" {
			return currency
		}
	}
	return ""
}

func inheritProductCurrency(products []Product, sources ...jsonObject) []Product {
	currency := currencyValue(sources...)
	if currency == "" {
		return products
	}
	for index := range products {
		if products[index].Currency == "" {
			products[index].Currency = currency
		}
	}
	return products
}

var defaultProductAliases = map[string][]string{
	"id":               {"id", "id_product", "service_id", "Id", "MailCode"},
	"name":             {"name", "title", "service_name", "Name", "MailName"},
	"summary":          {"note"},
	"description":      {"description", "desc", "note"},
	"image":            {"image", "icon", "cover"},
	"country":          {"country", "country_code", "quocgia", "flag"},
	"tags":             {"tags", "labels"},
	"price":            {"price", "money", "amount", "Price"},
	"original_price":   {"original_price", "old_price", "list_price"},
	"member_price":     {"member_price", "vip_price", "agency_price"},
	"stock":            {"stock", "quantity", "amount", "quality", "remain", "totalProduct", "product_count", "total_accounts", "Quantity", "Instock"},
	"stock_status":     {"stock_status", "availability"},
	"minimum":          {"min", "minimum", "min_purchase"},
	"maximum":          {"max", "maximum"},
	"fulfillment_type": {"fulfillment_type", "delivery_type", "delivery_way"},
}

func categoryFromObject(item jsonObject) Category {
	return Category{ExternalID: stringValue(item, "id", "category_id"), ExternalParentID: stringValue(item, "parent_id"), Name: stringValue(item, "name", "category_name", "name_big"), Description: stringValue(item, "description", "note"), ImageURL: stringValue(item, "image", "icon", "cover"), Sort: int(intValue(item, "sort", "sort_order")), Status: "active"}
}

func appendNestedCatalog(categories *[]Category, products *[]Product, categoryItems []any, exponent int, childKeys ...string) error {
	for _, raw := range categoryItems {
		category, ok := raw.(map[string]any)
		if !ok {
			return errors.New("supplier category response invalid")
		}
		categoryDTO := categoryFromObject(category)
		if categoryDTO.ExternalID == "" {
			categoryDTO.ExternalID = categoryDTO.Name
		}
		if categoryDTO.Name != "" {
			*categories = append(*categories, categoryDTO)
		}
		children := array(category, childKeys...)
		for _, childRaw := range children {
			child, ok := childRaw.(map[string]any)
			if !ok {
				return errors.New("supplier product response invalid")
			}
			if nested := object(child, "category"); nested != nil {
				child = nested
			}
			product := productFromObject(child, categoryDTO.ExternalID, defaultProductAliases, exponent)
			if product.Currency == "" {
				product.Currency = currencyValue(category)
			}
			if product.ExternalID == "" && product.Name != "" {
				product.ID, product.ExternalID = product.Name, product.Name
			}
			*products = append(*products, product)
		}
	}
	return nil
}

func (g *shopcloneGateway) Categories(ctx context.Context) ([]Category, error) {
	if g.implementation == nil {
		return nil, ErrCapabilityUnsupported
	}
	categories, products, err := g.implementation.catalog(ctx, g)
	if err != nil {
		return nil, err
	}
	if _, err := normalizeProducts(products); err != nil {
		return nil, err
	}
	if len(categories) == 0 {
		return nil, ErrCapabilityUnsupported
	}
	return categories, nil
}

func (g *shopcloneGateway) Products(ctx context.Context) ([]Product, error) {
	if g.implementation == nil {
		return nil, ErrCapabilityUnsupported
	}
	_, products, err := g.implementation.catalog(ctx, g)
	if err != nil {
		return nil, err
	}
	return normalizeProducts(products)
}

func (g *shopcloneGateway) Balance(ctx context.Context) (BalanceSnapshot, error) {
	if g.implementation == nil {
		return BalanceSnapshot{}, ErrCapabilityUnsupported
	}
	balance, err := g.implementation.balance(ctx, g)
	if err != nil {
		return BalanceSnapshot{}, err
	}
	if balance.Amount < 0 {
		return BalanceSnapshot{}, errors.New("supplier balance response invalid")
	}
	currencyCode := normalizedCurrencyCode(balance.Currency)
	if currencyCode == "" {
		currencyCode = g.money.BalanceCurrency
	}
	return BalanceSnapshot{Balance: balance.Amount, Currency: currencyCode, UpdatedAt: time.Now().UTC()}, nil
}

func (g *shopcloneGateway) CreateOrder(ctx context.Context, input CreateOrderRequest) (OrderResult, error) {
	if g.implementation == nil {
		return OrderResult{}, ErrCapabilityUnsupported
	}
	result, err := g.implementation.createOrder(ctx, g, input)
	if err == nil && result.Cost > 0 {
		result.CostCurrency, result.CostMinorUnit = g.money.PriceCurrency, g.money.PriceMinorUnit
	}
	return result, err
}

func (g *shopcloneGateway) Order(ctx context.Context, externalNo string) (OrderResult, error) {
	if g.implementation == nil {
		return OrderResult{}, ErrCapabilityUnsupported
	}
	result, err := g.implementation.order(ctx, g, externalNo)
	if err == nil && result.Cost > 0 {
		result.CostCurrency, result.CostMinorUnit = g.money.PriceCurrency, g.money.PriceMinorUnit
	}
	return result, err
}

func firstError(err error, fallback string) error {
	if err != nil {
		return err
	}
	return errors.New(fallback)
}

func bearerHeaders(token string) http.Header {
	return http.Header{"Authorization": {"Bearer " + token}}
}

func parsePlainBalance(payload []byte, minorUnits ...int) (int64, error) {
	exponent := 2
	if len(minorUnits) > 0 {
		exponent = minorUnits[0]
	}
	var decimal strings.Builder
	for _, character := range strings.TrimSpace(string(payload)) {
		if unicode.IsDigit(character) && character <= unicode.MaxASCII {
			decimal.WriteRune(character)
			continue
		}
		if character == '.' {
			decimal.WriteRune(character)
		}
	}
	if decimal.Len() == 0 {
		return 0, errors.New("supplier balance response invalid")
	}
	value, err := legacyAmountToMinorUnits(decimal.String(), exponent)
	if err != nil {
		return 0, errors.New("supplier balance response invalid")
	}
	return value, nil
}

func requireNumericExternalID(value string) (int64, error) {
	id, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
	if err != nil || id < 1 {
		return 0, errors.New("supplier product identifier is invalid")
	}
	return id, nil
}

func immediateResult(orderNo string, deliveries []string) (OrderResult, error) {
	orderNo = strings.TrimSpace(orderNo)
	if orderNo == "" || len(deliveries) == 0 {
		return OrderResult{}, errors.New("supplier delivery response invalid")
	}
	return OrderResult{ExternalOrderNo: orderNo, Status: "delivered", Deliveries: deliveries}, nil
}

var _ Gateway = (*shopcloneGateway)(nil)

func (g *shopcloneGateway) String() string { return fmt.Sprintf("shopcloneGateway(%s)", g.protocol) }
