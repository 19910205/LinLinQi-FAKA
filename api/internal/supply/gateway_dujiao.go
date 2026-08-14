package supply

import (
	"context"
	"crypto/hmac"
	"crypto/md5" // #nosec G501 -- Dujiao protocol body digest; authentication is HMAC-SHA256
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

type dujiaoNextGateway struct {
	transport *protocolTransport
	key       string
	secret    string
	money     MoneySpec
}

func newDujiaoNextGateway(baseURL string, credentials map[string]string, allowPrivate bool, money MoneySpec) *dujiaoNextGateway {
	return &dujiaoNextGateway{transport: newProtocolTransport(baseURL, allowPrivate), key: credentials["api_key"], secret: credentials["api_secret"], money: money}
}

func (g *dujiaoNextGateway) call(ctx context.Context, method, path string, query url.Values, input, output any) error {
	var body []byte
	var err error
	if input != nil {
		body, err = json.Marshal(input)
		if err != nil {
			return errors.New("encode supplier request")
		}
	}
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	// Dujiao's canonical wire format requires an MD5 body digest, but that
	// digest is authenticated by HMAC-SHA256 with the supplier secret.
	digest := md5.Sum(body) // #nosec G401 -- compatibility digest inside HMAC-SHA256
	canonical := method + "\n" + path + "\n" + timestamp + "\n" + hex.EncodeToString(digest[:])
	mac := hmac.New(sha256.New, []byte(g.secret))
	_, _ = mac.Write([]byte(canonical))
	headers := http.Header{}
	headers.Set("Dujiao-Next-Api-Key", g.key)
	headers.Set("Dujiao-Next-Timestamp", timestamp)
	headers.Set("Dujiao-Next-Signature", hex.EncodeToString(mac.Sum(nil)))
	payload, _, err := g.transport.do(ctx, method, path, query, body, "application/json", headers)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(payload, output); err != nil {
		return errors.New("decode supplier response")
	}
	return nil
}

type dujiaoLocalized map[string]string

func (value dujiaoLocalized) text() string {
	for _, key := range []string{"zh-CN", "zh_CN", "zh-TW", "en", "vi"} {
		if text := strings.TrimSpace(value[key]); text != "" {
			return text
		}
	}
	for _, text := range value {
		if text = strings.TrimSpace(text); text != "" {
			return text
		}
	}
	return ""
}

func (g *dujiaoNextGateway) Categories(ctx context.Context) ([]Category, error) {
	var response struct {
		OK         bool `json:"ok"`
		Categories []struct {
			ID       json.Number     `json:"id"`
			ParentID json.Number     `json:"parent_id"`
			Name     dujiaoLocalized `json:"name"`
			Icon     string          `json:"icon"`
			Sort     int             `json:"sort_order"`
		} `json:"categories"`
	}
	if err := g.call(ctx, http.MethodGet, "/api/v1/upstream/categories", nil, nil, &response); err != nil {
		return nil, err
	}
	if !response.OK {
		return nil, errors.New("supplier rejected category request")
	}
	result := make([]Category, 0, len(response.Categories))
	for _, item := range response.Categories {
		id := item.ID.String()
		name := item.Name.text()
		if id == "" || id == "0" || name == "" {
			return nil, errors.New("supplier category response invalid")
		}
		parentID := item.ParentID.String()
		if parentID == "0" {
			parentID = ""
		}
		result = append(result, Category{ExternalID: id, ExternalParentID: parentID, Name: name, ImageURL: strings.TrimSpace(item.Icon), Sort: item.Sort, Status: "active"})
	}
	return result, nil
}

type dujiaoProduct struct {
	ID               json.Number     `json:"id"`
	CategoryID       json.Number     `json:"category_id"`
	Title            dujiaoLocalized `json:"title"`
	Description      dujiaoLocalized `json:"description"`
	Content          dujiaoLocalized `json:"content"`
	Images           []string        `json:"images"`
	Tags             []string        `json:"tags"`
	PriceAmount      string          `json:"price_amount"`
	OriginalPrice    string          `json:"original_price"`
	MemberPrice      string          `json:"member_price"`
	WholesalePrices  json.RawMessage `json:"wholesale_prices"`
	Currency         string          `json:"currency"`
	FulfillmentType  string          `json:"fulfillment_type"`
	Active           bool            `json:"is_active"`
	CreatedAt        string          `json:"created_at"`
	UpdatedAt        string          `json:"updated_at"`
	ManualFormSchema map[string]any  `json:"manual_form_schema"`
	SKUs             []struct {
		ID            json.Number       `json:"id"`
		Code          string            `json:"sku_code"`
		Specs         map[string]string `json:"spec_values"`
		PriceAmount   string            `json:"price_amount"`
		OriginalPrice string            `json:"original_price"`
		MemberPrice   string            `json:"member_price"`
		StockStatus   string            `json:"stock_status"`
		StockQuantity int64             `json:"stock_quantity"`
		Active        bool              `json:"is_active"`
	} `json:"skus"`
}

var dujiaoSafeFieldKey = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

func dujiaoFieldKey(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if dujiaoSafeFieldKey.MatchString(value) {
		return value
	}
	digest := sha256.Sum256([]byte(value))
	return "field_" + hex.EncodeToString(digest[:8])
}

func dujiaoSchemaInt(object map[string]any, key string, fallback int) int {
	value := intValue(object, key)
	if value < 0 || value > 2000 {
		return fallback
	}
	return int(value)
}

func dujiaoSchemaOptions(value any) []string {
	items, _ := value.([]any)
	result := make([]string, 0, len(items))
	seen := map[string]struct{}{}
	for _, raw := range items {
		text := ""
		switch typed := raw.(type) {
		case string:
			text = strings.TrimSpace(typed)
		case json.Number:
			text = typed.String()
		case float64:
			text = strconv.FormatFloat(typed, 'f', -1, 64)
		case bool:
			text = strconv.FormatBool(typed)
		}
		if text == "" || len([]rune(text)) > 120 {
			continue
		}
		if _, exists := seen[text]; exists {
			continue
		}
		seen[text] = struct{}{}
		result = append(result, text)
		if len(result) == 50 {
			break
		}
	}
	return result
}

func dujiaoInputFields(schema map[string]any) []ProductInputField {
	properties, _ := schema["properties"].(map[string]any)
	requiredValues, _ := schema["required"].([]any)
	required := make(map[string]bool, len(requiredValues))
	for _, raw := range requiredValues {
		if value, ok := raw.(string); ok {
			required[value] = true
		}
	}
	keys := make([]string, 0, len(properties))
	for key := range properties {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]ProductInputField, 0, len(properties))
	for _, key := range keys {
		raw := properties[key]
		property, _ := raw.(map[string]any)
		label, _ := property["title"].(string)
		if label == "" {
			label = key
		}
		inputType := "text"
		typeName, _ := property["type"].(string)
		format, _ := property["format"].(string)
		options := dujiaoSchemaOptions(property["enum"])
		if len(options) > 0 || typeName == "boolean" {
			inputType = "select"
			if len(options) == 0 {
				options = []string{"true", "false"}
			}
		} else if typeName == "number" || typeName == "integer" {
			inputType = "number"
		} else if format == "email" {
			inputType = "email"
		} else if format == "textarea" {
			inputType = "textarea"
		}
		minLength := dujiaoSchemaInt(property, "minLength", 0)
		maxLength := dujiaoSchemaInt(property, "maxLength", 500)
		if maxLength < 1 {
			maxLength = 500
		}
		if inputType == "email" && maxLength > 190 {
			maxLength = 190
		}
		if inputType == "number" && maxLength > 64 {
			maxLength = 64
		}
		if minLength > maxLength {
			minLength = 0
		}
		pattern, _ := property["pattern"].(string)
		if len(pattern) > 300 {
			pattern = ""
		} else if pattern != "" {
			if _, err := regexp.Compile("^(?:" + pattern + ")$"); err != nil {
				pattern = ""
			}
		}
		placeholder, _ := property["placeholder"].(string)
		help, _ := property["description"].(string)
		sensitive, _ := property["x-sensitive"].(bool)
		sensitive = sensitive || format == "password"
		result = append(result, ProductInputField{Key: dujiaoFieldKey(key), ExternalKey: key, Label: strings.TrimSpace(label), InputType: inputType, Required: required[key], Sensitive: sensitive, Placeholder: strings.TrimSpace(placeholder), HelpText: strings.TrimSpace(help), Options: options, ValidationPattern: pattern, MinLength: minLength, MaxLength: maxLength})
	}
	return result
}

func optionalDujiaoMoney(value string, exponent int) (int64, error) {
	if strings.TrimSpace(value) == "" {
		return 0, nil
	}
	return decimalMinorUnits(value, exponent)
}

func normalizedDujiaoCurrency(responseCurrency, configuredCurrency string) (string, error) {
	responseCurrency = strings.ToUpper(strings.TrimSpace(responseCurrency))
	configuredCurrency = strings.ToUpper(strings.TrimSpace(configuredCurrency))
	valid := len(responseCurrency) == 3 && strings.IndexFunc(responseCurrency, func(character rune) bool { return character < 'A' || character > 'Z' }) < 0
	if !valid {
		return "", errors.New("supplier response currency is invalid")
	}
	if configuredCurrency != "" && responseCurrency != configuredCurrency {
		return "", errors.New("supplier response currency does not match connection money configuration")
	}
	return responseCurrency, nil
}

func normalizeDujiaoOrderStatus(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "paid":
		return "processing"
	case "canceled", "cancelled", "refunded":
		return "cancelled"
	default:
		return strings.ToLower(strings.TrimSpace(value))
	}
}

func (g *dujiaoNextGateway) orderCost(amount, responseCurrency string) (int64, string, int, error) {
	amount = strings.TrimSpace(amount)
	if amount == "" {
		return 0, "", 0, nil
	}
	currencyCode, err := normalizedDujiaoCurrency(responseCurrency, g.money.PriceCurrency)
	if err != nil {
		return 0, "", 0, err
	}
	cost, err := decimalMinorUnits(amount, g.money.PriceMinorUnit)
	if err != nil {
		return 0, "", 0, err
	}
	return cost, currencyCode, g.money.PriceMinorUnit, nil
}

func dujiaoTime(value string) *time.Time {
	parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(value))
	if err != nil {
		return nil
	}
	parsed = parsed.UTC()
	return &parsed
}

func (g *dujiaoNextGateway) normalizeDujiaoProducts(item dujiaoProduct) ([]Product, error) {
	currencyCode, err := normalizedDujiaoCurrency(item.Currency, g.money.PriceCurrency)
	if err != nil {
		return nil, err
	}
	basePrice, err := decimalMinorUnits(item.PriceAmount, g.money.PriceMinorUnit)
	if err != nil {
		return nil, err
	}
	originalPrice, err := optionalDujiaoMoney(item.OriginalPrice, g.money.PriceMinorUnit)
	if err != nil {
		return nil, err
	}
	memberPrice, err := optionalDujiaoMoney(item.MemberPrice, g.money.PriceMinorUnit)
	if err != nil {
		return nil, err
	}
	status := "inactive"
	if item.Active {
		status = "active"
	}
	wholesale := item.WholesalePrices
	if len(wholesale) == 0 || !json.Valid(wholesale) {
		wholesale = json.RawMessage(`{}`)
	}
	base := Product{
		ID: item.ID.String(), ExternalID: item.ID.String(), ExternalCategoryID: item.CategoryID.String(),
		Name: item.Title.text(), Summary: item.Description.text(), Description: item.Content.text(),
		ImageURLs: item.Images, Tags: item.Tags, Currency: currencyCode,
		Price: basePrice, OriginalPrice: originalPrice, MemberPrice: memberPrice, WholesalePrices: wholesale,
		Stock: 0, StockStatus: "out_of_stock", Minimum: 1, FulfillmentType: item.FulfillmentType,
		Status: status, UpstreamCreatedAt: dujiaoTime(item.CreatedAt), UpstreamUpdatedAt: dujiaoTime(item.UpdatedAt),
		InputFields: dujiaoInputFields(item.ManualFormSchema),
	}
	if len(item.SKUs) == 0 {
		return []Product{base}, nil
	}
	result := make([]Product, 0, len(item.SKUs))
	for _, sku := range item.SKUs {
		price, err := decimalMinorUnits(sku.PriceAmount, g.money.PriceMinorUnit)
		if err != nil {
			return nil, err
		}
		skuOriginal, err := optionalDujiaoMoney(sku.OriginalPrice, g.money.PriceMinorUnit)
		if err != nil {
			return nil, err
		}
		skuMember, err := optionalDujiaoMoney(sku.MemberPrice, g.money.PriceMinorUnit)
		if err != nil {
			return nil, err
		}
		name := base.Name
		if len(sku.Specs) > 0 {
			keys := make([]string, 0, len(sku.Specs))
			for key := range sku.Specs {
				keys = append(keys, key)
			}
			sort.Strings(keys)
			parts := make([]string, 0, len(keys))
			for _, key := range keys {
				parts = append(parts, key+":"+sku.Specs[key])
			}
			name += " / " + strings.Join(parts, " · ")
		}
		skuStatus := "inactive"
		if item.Active && sku.Active {
			skuStatus = "active"
		}
		product := base
		product.ID, product.ExternalID, product.ParentExternalID, product.ExternalSKU, product.Name = sku.ID.String(), sku.ID.String(), item.ID.String(), strings.TrimSpace(sku.Code), name
		product.Price, product.Stock, product.Status = price, sku.StockQuantity, skuStatus
		product.OriginalPrice, product.MemberPrice = skuOriginal, skuMember
		product.StockStatus = strings.ToLower(strings.TrimSpace(sku.StockStatus))
		result = append(result, product)
	}
	return result, nil
}

func (g *dujiaoNextGateway) Products(ctx context.Context) ([]Product, error) {
	result := make([]Product, 0)
	for page := 1; page <= 1000; page++ {
		var response struct {
			OK       bool            `json:"ok"`
			Items    []dujiaoProduct `json:"items"`
			Total    int             `json:"total"`
			Page     int             `json:"page"`
			PageSize int             `json:"page_size"`
		}
		query := url.Values{"page": {strconv.Itoa(page)}, "page_size": {"100"}, "include_inactive": {"true"}}
		if err := g.call(ctx, http.MethodGet, "/api/v1/upstream/products", query, nil, &response); err != nil {
			return nil, err
		}
		if !response.OK {
			return nil, errors.New("supplier rejected catalog request")
		}
		for _, item := range response.Items {
			products, err := g.normalizeDujiaoProducts(item)
			if err != nil {
				return nil, err
			}
			result = append(result, products...)
		}
		if len(response.Items) == 0 || response.PageSize <= 0 || page*response.PageSize >= response.Total {
			break
		}
	}
	return normalizeProducts(result)
}

func (g *dujiaoNextGateway) Product(ctx context.Context, input ProductDetailRequest) (Product, error) {
	externalID := strings.TrimSpace(input.ExternalProductID)
	parentID := strings.TrimSpace(input.ParentExternalID)
	if externalID == "" {
		return Product{}, errors.New("supplier product identifier is required")
	}
	lookupID := parentID
	if lookupID == "" {
		lookupID = externalID
	}
	var response struct {
		OK      bool          `json:"ok"`
		Product dujiaoProduct `json:"product"`
	}
	path := "/api/v1/upstream/products/" + url.PathEscape(lookupID)
	if err := g.call(ctx, http.MethodGet, path, nil, nil, &response); err != nil {
		return Product{}, err
	}
	if !response.OK {
		return Product{}, errors.New("supplier rejected product request")
	}
	products, err := g.normalizeDujiaoProducts(response.Product)
	if err != nil {
		return Product{}, err
	}
	for _, product := range products {
		if product.ExternalID == externalID {
			return normalizeProduct(product)
		}
	}
	return Product{}, errors.New("supplier product response does not contain requested SKU")
}

func (g *dujiaoNextGateway) Stock(ctx context.Context, input StockRequest) (StockSnapshot, error) {
	product, err := g.Product(ctx, ProductDetailRequest{ExternalProductID: input.ExternalProductID, ParentExternalID: input.ParentExternalID})
	if err != nil {
		return StockSnapshot{}, err
	}
	return StockSnapshot{
		ExternalProductID: product.ExternalID, VariantID: strings.TrimSpace(input.VariantID),
		Stock: product.Stock, StockStatus: product.StockStatus, ObservedAt: time.Now().UTC(),
	}, nil
}

func (g *dujiaoNextGateway) Balance(ctx context.Context) (BalanceSnapshot, error) {
	var response struct {
		OK       bool   `json:"ok"`
		Balance  string `json:"balance"`
		Currency string `json:"currency"`
	}
	if err := g.call(ctx, http.MethodPost, "/api/v1/upstream/ping", nil, nil, &response); err != nil {
		return BalanceSnapshot{}, err
	}
	balance, err := decimalMinorUnits(response.Balance, g.money.BalanceMinorUnit)
	if err != nil || !response.OK {
		return BalanceSnapshot{}, errors.New("supplier balance response invalid")
	}
	currencyCode, err := normalizedDujiaoCurrency(response.Currency, g.money.BalanceCurrency)
	if err != nil {
		return BalanceSnapshot{}, err
	}
	return BalanceSnapshot{Balance: balance, Currency: currencyCode, MinorUnit: g.money.BalanceMinorUnit, UpdatedAt: time.Now().UTC()}, nil
}

func (g *dujiaoNextGateway) CreateOrder(ctx context.Context, input CreateOrderRequest) (OrderResult, error) {
	skuID, err := strconv.ParseUint(strings.TrimSpace(input.ExternalProductID), 10, 64)
	if err != nil {
		return OrderResult{}, errors.New("supplier product identifier is invalid")
	}
	request := map[string]any{"sku_id": skuID, "quantity": input.Quantity, "manual_form_data": input.Parameters, "downstream_order_no": input.ClientOrderNo, "trace_id": input.ClientOrderNo, "callback_url": input.CallbackURL}
	var response struct {
		OK       bool        `json:"ok"`
		OrderID  json.Number `json:"order_id"`
		OrderNo  string      `json:"order_no"`
		Status   string      `json:"status"`
		Amount   string      `json:"amount"`
		Currency string      `json:"currency"`
	}
	if err := g.call(ctx, http.MethodPost, "/api/v1/upstream/orders", nil, request, &response); err != nil {
		return OrderResult{}, err
	}
	externalOrderNo := response.OrderID.String()
	if externalOrderNo == "" || externalOrderNo == "0" {
		externalOrderNo = strings.TrimSpace(response.OrderNo)
	}
	if externalOrderNo == "" {
		return OrderResult{}, errors.New("supplier rejected order")
	}
	cost, costCurrency, costMinorUnit, err := g.orderCost(response.Amount, response.Currency)
	if err != nil {
		return OrderResult{}, err
	}
	status := normalizeDujiaoOrderStatus(response.Status)
	if !response.OK && status != "failed" && status != "cancelled" && status != "rejected" {
		return OrderResult{}, errors.New("supplier rejected order")
	}
	return OrderResult{ExternalOrderNo: externalOrderNo, Status: status, Cost: cost, CostCurrency: costCurrency, CostMinorUnit: costMinorUnit}, nil
}

func (g *dujiaoNextGateway) Order(ctx context.Context, externalNo string) (OrderResult, error) {
	path := "/api/v1/upstream/orders/" + url.PathEscape(strings.TrimSpace(externalNo))
	var response struct {
		OK          bool        `json:"ok"`
		OrderID     json.Number `json:"order_id"`
		Status      string      `json:"status"`
		Amount      string      `json:"amount"`
		Currency    string      `json:"currency"`
		Fulfillment struct {
			Payload string `json:"payload"`
		} `json:"fulfillment"`
	}
	if err := g.call(ctx, http.MethodGet, path, nil, nil, &response); err != nil {
		return OrderResult{}, err
	}
	if !response.OK {
		return OrderResult{}, errors.New("supplier rejected order query")
	}
	cost, costCurrency, costMinorUnit, err := g.orderCost(response.Amount, response.Currency)
	if err != nil {
		return OrderResult{}, err
	}
	status := normalizeDujiaoOrderStatus(response.Status)
	return OrderResult{ExternalOrderNo: response.OrderID.String(), Status: status, Deliveries: splitDeliveries(response.Fulfillment.Payload), Cost: cost, CostCurrency: costCurrency, CostMinorUnit: costMinorUnit}, nil
}

func (g *dujiaoNextGateway) CancelOrder(ctx context.Context, externalNo string) (OrderResult, error) {
	externalNo = strings.TrimSpace(externalNo)
	if externalNo == "" {
		return OrderResult{}, errors.New("supplier order identifier is required")
	}
	path := "/api/v1/upstream/orders/" + url.PathEscape(externalNo) + "/cancel"
	var response struct {
		OK      bool        `json:"ok"`
		OrderID json.Number `json:"order_id"`
		Status  string      `json:"status"`
	}
	if err := g.call(ctx, http.MethodPost, path, nil, nil, &response); err != nil {
		return OrderResult{}, err
	}
	if !response.OK {
		return OrderResult{}, errors.New("supplier rejected order cancellation")
	}
	orderID := response.OrderID.String()
	if orderID == "" {
		orderID = externalNo
	}
	status := normalizeDujiaoOrderStatus(response.Status)
	if status == "" {
		status = "cancelled"
	}
	return OrderResult{ExternalOrderNo: orderID, Status: status}, nil
}

var _ Gateway = (*dujiaoNextGateway)(nil)
var _ ProductDetailReader = (*dujiaoNextGateway)(nil)
var _ StockReader = (*dujiaoNextGateway)(nil)
var _ OrderCanceller = (*dujiaoNextGateway)(nil)

func dujiaoSignatureForTest(method, path, timestamp, secret string, body []byte) string {
	digest := md5.Sum(body) // #nosec G401 -- test helper mirrors digest inside HMAC-SHA256
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(fmt.Sprintf("%s\n%s\n%s\n%s", method, path, timestamp, hex.EncodeToString(digest[:]))))
	return hex.EncodeToString(mac.Sum(nil))
}
