package supply

import (
	"context"
	"crypto/md5" // #nosec G501 -- required by the legacy ACG supplier wire protocol
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

type acgGateway struct {
	transport *protocolTransport
	protocol  string
	appID     string
	appKey    string
	money     MoneySpec
}

func newACGGateway(protocol, baseURL string, credentials map[string]string, allowPrivate bool, money MoneySpec) Gateway {
	base := &acgGateway{transport: newProtocolTransport(baseURL, allowPrivate), protocol: protocol, appID: credentials["app_id"], appKey: credentials["app_key"], money: money}
	if protocol == "acg-faka-new" {
		return &acgAdvancedGateway{acgGateway: base}
	}
	return base
}

type acgAdvancedGateway struct {
	*acgGateway
}

func acgHTTPBuildQuery(values url.Values, omitSign, omitEmptyScalar bool) string {
	keys := make([]string, 0, len(values))
	for name := range values {
		if omitSign && name == "sign" {
			continue
		}
		entries := values[name]
		if len(entries) == 0 || (omitEmptyScalar && len(entries) == 1 && entries[0] == "") {
			continue
		}
		keys = append(keys, name)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, name := range keys {
		entries := values[name]
		if len(entries) == 1 {
			parts = append(parts, url.QueryEscape(name)+"="+url.QueryEscape(entries[0]))
			continue
		}
		for index, value := range entries {
			arrayKey := name + "[" + strconv.Itoa(index) + "]"
			parts = append(parts, url.QueryEscape(arrayKey)+"="+url.QueryEscape(value))
		}
	}
	return strings.Join(parts, "&")
}

func phpURLDecode(value string) string {
	decoded := make([]byte, 0, len(value))
	fromHex := func(character byte) (byte, bool) {
		switch {
		case character >= '0' && character <= '9':
			return character - '0', true
		case character >= 'a' && character <= 'f':
			return character - 'a' + 10, true
		case character >= 'A' && character <= 'F':
			return character - 'A' + 10, true
		default:
			return 0, false
		}
	}
	for index := 0; index < len(value); index++ {
		switch value[index] {
		case '+':
			decoded = append(decoded, ' ')
		case '%':
			if index+2 < len(value) {
				high, highOK := fromHex(value[index+1])
				low, lowOK := fromHex(value[index+2])
				if highOK && lowOK {
					decoded = append(decoded, high<<4|low)
					index += 2
					continue
				}
			}
			decoded = append(decoded, value[index])
		default:
			decoded = append(decoded, value[index])
		}
	}
	return string(decoded)
}

func acgSignature(values url.Values, key string) string {
	// This intentionally mirrors PHP:
	// md5(urldecode(http_build_query(ksort(filtered values)) + "&key=" + key)).
	// It is used only for the legacy supplier's outbound compatibility scheme;
	// production transport separately enforces HTTPS and SSRF-safe destinations.
	encoded := acgHTTPBuildQuery(values, true, true) + "&key=" + key
	digest := md5.Sum([]byte(phpURLDecode(encoded))) // #nosec G401 -- exact upstream protocol requirement
	return hex.EncodeToString(digest[:])
}

func (g *acgGateway) call(ctx context.Context, path string, values url.Values) (jsonObject, error) {
	if values == nil {
		values = url.Values{}
	}
	values.Set("app_id", g.appID)
	values.Set("app_key", g.appKey)
	values.Set("sign", acgSignature(values, g.appKey))
	response, _, err := g.transport.do(ctx, http.MethodPost, path, nil, []byte(acgHTTPBuildQuery(values, false, false)), "application/x-www-form-urlencoded", nil)
	if err != nil {
		return nil, err
	}
	object, err := decodeObject(response)
	if err != nil {
		return nil, err
	}
	if intValue(object, "code") != 200 {
		return nil, errors.New("supplier rejected request")
	}
	return object, nil
}

func (g *acgGateway) Balance(ctx context.Context) (BalanceSnapshot, error) {
	response, err := g.call(ctx, "/shared/authentication/connect", nil)
	if err != nil {
		return BalanceSnapshot{}, err
	}
	data := object(response, "data")
	balance, err := decimalMinorUnits(stringValue(data, "balance"), g.money.BalanceMinorUnit)
	if err != nil {
		return BalanceSnapshot{}, err
	}
	return BalanceSnapshot{Balance: balance, Currency: g.money.BalanceCurrency, UpdatedAt: time.Now().UTC()}, nil
}

func acgOptionalMoney(item jsonObject, exponent int, keys ...string) (int64, error) {
	if objectValue(item, keys...) == nil || strings.TrimSpace(stringValue(item, keys...)) == "" {
		return 0, nil
	}
	return decimalMoneyValue(item, exponent, keys...)
}

func acgProductFromItem(item jsonObject, categoryID string, money MoneySpec) (Product, error) {
	externalID := stringValue(item, "code", "shared_code", "id")
	price, err := decimalMoneyValue(item, money.PriceMinorUnit, "user_price", "price", "factory_price")
	if err != nil {
		return Product{}, err
	}
	originalPrice, err := acgOptionalMoney(item, money.PriceMinorUnit, "price", "original_price")
	if err != nil {
		return Product{}, err
	}
	memberPrice, err := acgOptionalMoney(item, money.PriceMinorUnit, "user_price", "member_price")
	if err != nil {
		return Product{}, err
	}
	if originalPrice <= price {
		originalPrice = 0
	}
	images := stringArray(objectValue(item, "images", "image_urls", "gallery"))
	if cover := stringValue(item, "cover", "image"); cover != "" {
		images = append([]string{cover}, images...)
	}
	stock := intValue(item, "stock", "inventory", "amount")
	if stock == 0 && boolValue(item, "unlimited") {
		stock = -1
	}
	return Product{
		ID: externalID, ExternalID: externalID, ExternalCategoryID: categoryID,
		Name: stringValue(item, "name", "title"), Summary: stringValue(item, "description"), Description: stringValue(item, "content", "description"),
		ImageURLs: images, Country: stringValue(item, "country", "country_code"), Tags: stringArray(objectValue(item, "tags", "labels")),
		Currency: money.PriceCurrency, Price: price, OriginalPrice: originalPrice, MemberPrice: memberPrice,
		Stock: stock, StockStatus: stringValue(item, "stock_status"), Minimum: int(intValue(item, "minimum", "min")), Maximum: int(intValue(item, "maximum", "max")),
		FulfillmentType: stringValue(item, "fulfillment_type", "delivery_type", "delivery_way"), Status: "active",
	}, nil
}

func acgCatalogItems(value any, categoryID string, exponent int, categories *[]Category, products *[]Product) error {
	items, ok := value.([]any)
	if !ok {
		if object, ok := value.(map[string]any); ok {
			for _, nested := range object {
				if err := acgCatalogItems(nested, categoryID, exponent, categories, products); err == nil {
					continue
				}
			}
			return nil
		}
		return errors.New("supplier catalog response invalid")
	}
	for _, raw := range items {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		isCategory := boolValue(item, "is_category") || len(array(item, "commodities", "products", "children")) > 0
		if isCategory {
			category := categoryFromObject(item)
			if category.ExternalID == "" {
				category.ExternalID = stringValue(item, "code")
			}
			if category.Name != "" && category.ExternalID != "" {
				category.ExternalParentID = categoryID
				*categories = append(*categories, category)
			}
			children := objectValue(item, "commodities", "products", "children")
			if children != nil {
				if err := acgCatalogItems(children, category.ExternalID, exponent, categories, products); err != nil {
					return err
				}
			}
			continue
		}
		product, err := acgProductFromItem(item, categoryID, MoneySpec{PriceMinorUnit: exponent})
		if err != nil {
			return err
		}
		*products = append(*products, product)
	}
	return nil
}

func (g *acgGateway) catalog(ctx context.Context) ([]Category, []Product, error) {
	response, err := g.call(ctx, "/shared/commodity/items", nil)
	if err != nil {
		return nil, nil, err
	}
	data := objectValue(response, "data")
	categories, products := []Category{}, []Product{}
	if err := acgCatalogItems(data, "", g.money.PriceMinorUnit, &categories, &products); err != nil {
		return nil, nil, err
	}
	for index := range products {
		products[index].Currency = g.money.PriceCurrency
	}
	normalized, err := normalizeProducts(products)
	return categories, normalized, err
}

func (g *acgGateway) Categories(ctx context.Context) ([]Category, error) {
	categories, _, err := g.catalog(ctx)
	if err != nil {
		return nil, err
	}
	if len(categories) == 0 {
		return nil, ErrCapabilityUnsupported
	}
	return categories, nil
}

func (g *acgGateway) Products(ctx context.Context) ([]Product, error) {
	_, products, err := g.catalog(ctx)
	return products, err
}

func findACGProductObject(value any) (jsonObject, bool) {
	switch typed := value.(type) {
	case map[string]any:
		if stringValue(typed, "code", "shared_code", "id") != "" && stringValue(typed, "name", "title") != "" {
			return typed, true
		}
		for _, nested := range typed {
			if item, ok := findACGProductObject(nested); ok {
				return item, true
			}
		}
	case []any:
		for _, nested := range typed {
			if item, ok := findACGProductObject(nested); ok {
				return item, true
			}
		}
	}
	return nil, false
}

func (g *acgGateway) Product(ctx context.Context, input ProductDetailRequest) (Product, error) {
	externalID := strings.TrimSpace(input.ExternalProductID)
	if externalID == "" {
		return Product{}, errors.New("supplier product identifier is required")
	}
	response, err := g.call(ctx, "/shared/commodity/item", url.Values{"sharedCode": {externalID}})
	if err != nil {
		return Product{}, err
	}
	item, ok := findACGProductObject(objectValue(response, "data"))
	if !ok {
		return Product{}, errors.New("supplier product response invalid")
	}
	product, err := acgProductFromItem(item, "", g.money)
	if err != nil {
		return Product{}, err
	}
	product.ID, product.ExternalID = externalID, externalID
	return normalizeProduct(product)
}

func (g *acgGateway) Stock(ctx context.Context, input StockRequest) (StockSnapshot, error) {
	externalID := strings.TrimSpace(input.ExternalProductID)
	if externalID == "" {
		return StockSnapshot{}, errors.New("supplier product identifier is required")
	}
	values := url.Values{}
	path := "/shared/commodity/stock"
	values.Set("code", externalID)
	stockKey := "stock"
	if g.protocol == "acg-faka-old" {
		path = "/shared/commodity/inventory"
		values = url.Values{"sharedCode": {externalID}}
		stockKey = "count"
	}
	if race, ok := input.Parameters["race"].(string); ok && strings.TrimSpace(race) != "" {
		values.Set("race", strings.TrimSpace(race))
	}
	response, err := g.call(ctx, path, values)
	if err != nil {
		return StockSnapshot{}, err
	}
	data := object(response, "data")
	stock := intValue(data, stockKey, "stock", "count", "inventory")
	if stock < 0 {
		return StockSnapshot{}, errors.New("supplier stock response invalid")
	}
	status := "out_of_stock"
	if stock > 0 {
		status = "in_stock"
	}
	return StockSnapshot{ExternalProductID: externalID, VariantID: strings.TrimSpace(input.VariantID), Stock: stock, StockStatus: status, ObservedAt: time.Now().UTC()}, nil
}

func decodeACGSKUObject(raw string) (map[string]string, error) {
	decoder := json.NewDecoder(strings.NewReader(raw))
	opening, err := decoder.Token()
	if err != nil || opening != json.Delim('{') {
		return nil, errors.New("supplier sku parameter is invalid")
	}
	result := map[string]string{}
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return nil, errors.New("supplier sku parameter is invalid")
		}
		key, ok := keyToken.(string)
		if !ok || strings.TrimSpace(key) == "" || len([]rune(key)) > 100 || strings.ContainsAny(key, "[]\x00\r\n") {
			return nil, errors.New("supplier sku parameter is invalid")
		}
		if _, duplicate := result[key]; duplicate {
			return nil, errors.New("supplier sku parameter is ambiguous")
		}
		var value string
		if decoder.Decode(&value) != nil || len(value) > 1000 || strings.ContainsRune(value, '\x00') {
			return nil, errors.New("supplier sku parameter is invalid")
		}
		result[key] = value
	}
	if closing, err := decoder.Token(); err != nil || closing != json.Delim('}') {
		return nil, errors.New("supplier sku parameter is invalid")
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("supplier sku parameter is invalid")
	}
	return result, nil
}

func acgOrderParameters(parameters map[string]string) (url.Values, error) {
	values := url.Values{}
	skuKeys := map[string]struct{}{}
	setSKU := func(name, value string) error {
		canonical := strings.ToLower(strings.TrimSpace(name))
		if _, duplicate := skuKeys[canonical]; duplicate {
			return errors.New("supplier sku parameter is ambiguous")
		}
		skuKeys[canonical] = struct{}{}
		values.Set("sku["+name+"]", value)
		return nil
	}
	reserved := map[string]struct{}{"app_id": {}, "app_key": {}, "sign": {}, "shared_code": {}, "contact": {}, "num": {}, "request_no": {}, "device": {}}
	for rawKey, value := range parameters {
		key := strings.TrimSpace(rawKey)
		if key == "" || len([]rune(key)) > 100 || strings.ContainsAny(key, "[]\x00\r\n") || len(value) > 10_000 || strings.ContainsRune(value, '\x00') {
			return nil, errors.New("supplier order parameter is invalid")
		}
		if _, blocked := reserved[strings.ToLower(key)]; blocked {
			return nil, errors.New("supplier order parameter collides with a reserved field")
		}
		if strings.EqualFold(key, "sku") {
			sku, err := decodeACGSKUObject(value)
			if err != nil {
				return nil, err
			}
			keys := make([]string, 0, len(sku))
			for name := range sku {
				keys = append(keys, name)
			}
			sort.Strings(keys)
			for _, name := range keys {
				if err := setSKU(name, sku[name]); err != nil {
					return nil, err
				}
			}
			continue
		}
		if strings.HasPrefix(strings.ToLower(key), "sku.") {
			name := strings.TrimSpace(key[4:])
			if name == "" || len([]rune(name)) > 100 || strings.ContainsAny(name, "[]\x00\r\n") {
				return nil, errors.New("supplier sku parameter is invalid")
			}
			if err := setSKU(name, value); err != nil {
				return nil, err
			}
			continue
		}
		values.Set(key, value)
	}
	return values, nil
}

func mergeACGParameters(target url.Values, parameters map[string]string) error {
	parsed, err := acgOrderParameters(parameters)
	if err != nil {
		return err
	}
	for key, entries := range parsed {
		for _, value := range entries {
			target.Add(key, value)
		}
	}
	return nil
}

func acgOrderNumber(data jsonObject) string {
	// ACG serializes the order model with trade_no. Some compatible releases
	// expose the same field in camelCase, while older adapters used order_no.
	return stringValue(data, "trade_no", "tradeNo", "order_no", "orderNo")
}

func acgOrderCost(data jsonObject, money MoneySpec) (int64, string, int, bool, error) {
	if objectValue(data, "amount") == nil {
		return 0, "", 0, false, nil
	}
	cost, err := decimalMoneyValue(data, money.PriceMinorUnit, "amount")
	if err != nil {
		return 0, "", 0, false, errors.New("supplier order amount invalid")
	}

	configuredCurrency := normalizedCurrencyCode(money.PriceCurrency)
	if strings.TrimSpace(money.PriceCurrency) != "" && configuredCurrency == "" {
		return 0, "", 0, false, errors.New("supplier order currency invalid")
	}
	currency := configuredCurrency
	if raw := objectValue(data, "currency", "currency_code", "currencyCode"); raw != nil {
		explicitCurrency := normalizedCurrencyCode(stringValue(data, "currency", "currency_code", "currencyCode"))
		if explicitCurrency == "" {
			return 0, "", 0, false, errors.New("supplier order currency invalid")
		}
		if configuredCurrency != "" && explicitCurrency != configuredCurrency {
			return 0, "", 0, false, errors.New("supplier order currency conflicts with configuration")
		}
		currency = explicitCurrency
	}
	if currency == "" {
		return 0, "", 0, false, errors.New("supplier order currency missing")
	}
	return cost, currency, money.PriceMinorUnit, true, nil
}

func acgOrderDeliveries(data jsonObject) ([]string, bool, error) {
	raw := objectValue(data, "secret")
	if raw == nil {
		return nil, false, nil
	}
	secret, ok := raw.(string)
	if !ok {
		return nil, false, errors.New("supplier delivery response invalid")
	}
	if strings.TrimSpace(secret) == "" {
		return nil, false, nil
	}
	deliveries := splitDeliveries(secret)
	if len(deliveries) == 0 {
		return nil, false, errors.New("supplier delivery response invalid")
	}
	return deliveries, true, nil
}

func (g *acgGateway) orderResult(data jsonObject, fallbackOrderNo string) (OrderResult, error) {
	orderNo := acgOrderNumber(data)
	if orderNo == "" {
		orderNo = strings.TrimSpace(fallbackOrderNo)
	}
	if orderNo == "" {
		return OrderResult{}, errors.New("supplier order response invalid")
	}

	cost, costCurrency, costMinorUnit, hasCost, err := acgOrderCost(data, g.money)
	if err != nil {
		return OrderResult{}, err
	}
	deliveries, delivered, err := acgOrderDeliveries(data)
	if err != nil {
		return OrderResult{}, err
	}
	result := OrderResult{ExternalOrderNo: orderNo, Status: "processing"}
	if delivered {
		result.Status = "delivered"
		result.Deliveries = deliveries
	}
	if hasCost {
		result.Cost = cost
		result.CostCurrency = costCurrency
		result.CostMinorUnit = costMinorUnit
	}
	return result, nil
}

func (g *acgGateway) CreateOrder(ctx context.Context, input CreateOrderRequest) (OrderResult, error) {
	values := url.Values{"shared_code": {input.ExternalProductID}, "contact": {input.Email}, "num": {strconv.Itoa(input.Quantity)}, "request_no": {input.ClientOrderNo}, "device": {"0"}}
	if err := mergeACGParameters(values, input.Parameters); err != nil {
		return OrderResult{}, err
	}
	response, err := g.call(ctx, "/shared/commodity/trade", values)
	if err != nil {
		return OrderResult{}, err
	}
	data := object(response, "data")
	return g.orderResult(data, "")
}

func (g *acgAdvancedGateway) Quote(ctx context.Context, input QuoteRequest) (QuoteSnapshot, error) {
	externalID := strings.TrimSpace(input.ExternalProductID)
	if externalID == "" || input.Quantity < 1 || input.Quantity > 1_000_000 {
		return QuoteSnapshot{}, errors.New("supplier quote request is invalid")
	}
	values := url.Values{}
	if err := mergeACGParameters(values, input.Parameters); err != nil {
		return QuoteSnapshot{}, err
	}
	values.Set("code", externalID)
	values.Set("num", strconv.Itoa(input.Quantity))
	response, err := g.call(ctx, "/shared/commodity/valuation", values)
	if err != nil {
		return QuoteSnapshot{}, err
	}
	amount, err := decimalMoneyValue(object(response, "data"), g.money.PriceMinorUnit, "price", "amount")
	if err != nil {
		return QuoteSnapshot{}, err
	}
	return QuoteSnapshot{Amount: amount, Currency: g.money.PriceCurrency, MinorUnit: g.money.PriceMinorUnit, QuotedAt: time.Now().UTC()}, nil
}

func acgDraftCards(value any) []DraftCard {
	result := []DraftCard{}
	var walk func(any)
	walk = func(current any) {
		switch typed := current.(type) {
		case []any:
			for _, item := range typed {
				walk(item)
			}
		case map[string]any:
			id := stringValue(typed, "id", "card_id")
			preview := stringValue(typed, "draft", "preview", "name")
			if id != "" {
				result = append(result, DraftCard{ID: id, Preview: preview})
				return
			}
			for _, nested := range typed {
				walk(nested)
			}
		}
	}
	walk(value)
	return result
}

func (g *acgAdvancedGateway) DraftCards(ctx context.Context, input DraftCardRequest) (DraftCardPage, error) {
	externalID := strings.TrimSpace(input.ExternalProductID)
	if externalID == "" {
		return DraftCardPage{}, errors.New("supplier draft request is invalid")
	}
	page, pageSize := input.Page, input.PageSize
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if page > 1_000_000 || pageSize > 100 {
		return DraftCardPage{}, errors.New("supplier draft pagination is invalid")
	}
	values := url.Values{}
	if err := mergeACGParameters(values, input.Parameters); err != nil {
		return DraftCardPage{}, err
	}
	values.Set("code", externalID)
	values.Set("page", strconv.Itoa(page))
	values.Set("limit", strconv.Itoa(pageSize))
	response, err := g.call(ctx, "/shared/commodity/draft", values)
	if err != nil {
		return DraftCardPage{}, err
	}
	return DraftCardPage{Items: acgDraftCards(objectValue(response, "data")), Page: page, PageSize: pageSize}, nil
}

func (g *acgGateway) Order(ctx context.Context, externalNo string) (OrderResult, error) {
	response, err := g.call(ctx, "/shared/commodity/query", url.Values{"tradeNo": {externalNo}})
	if err != nil {
		return OrderResult{}, err
	}
	data := object(response, "data")
	return g.orderResult(data, externalNo)
}

func acgRawSnapshot(value any) json.RawMessage {
	encoded, _ := json.Marshal(value)
	return encoded
}

var _ Gateway = (*acgGateway)(nil)
var _ ProductDetailReader = (*acgGateway)(nil)
var _ StockReader = (*acgGateway)(nil)
var _ Gateway = (*acgAdvancedGateway)(nil)
var _ PriceQuoter = (*acgAdvancedGateway)(nil)
var _ DraftCardReader = (*acgAdvancedGateway)(nil)
