package supply

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"linlinqi/api/internal/security"
)

type Client struct {
	baseURL, key, secret string
	httpClient           *http.Client
}
type Product struct {
	ID                 string              `json:"id"`
	ExternalID         string              `json:"external_id"`
	ParentExternalID   string              `json:"parent_external_id,omitempty"`
	ExternalCategoryID string              `json:"external_category_id,omitempty"`
	ExternalSKU        string              `json:"external_sku"`
	Name               string              `json:"name"`
	Summary            string              `json:"summary,omitempty"`
	Description        string              `json:"description,omitempty"`
	CoverURL           string              `json:"cover_url,omitempty"`
	ImageURLs          []string            `json:"image_urls,omitempty"`
	Country            string              `json:"country,omitempty"`
	Tags               []string            `json:"tags,omitempty"`
	Currency           string              `json:"currency,omitempty"`
	Price              int64               `json:"price"`
	OriginalPrice      int64               `json:"original_price,omitempty"`
	MemberPrice        int64               `json:"member_price,omitempty"`
	WholesalePrices    json.RawMessage     `json:"wholesale_prices,omitempty"`
	Stock              int64               `json:"stock"`
	StockStatus        string              `json:"stock_status,omitempty"`
	Minimum            int                 `json:"minimum,omitempty"`
	Maximum            int                 `json:"maximum,omitempty"`
	FulfillmentType    string              `json:"fulfillment_type,omitempty"`
	Status             string              `json:"status,omitempty"`
	UpstreamCreatedAt  *time.Time          `json:"upstream_created_at,omitempty"`
	UpstreamUpdatedAt  *time.Time          `json:"upstream_updated_at,omitempty"`
	Variants           []ProductVariant    `json:"variants,omitempty"`
	InputFields        []ProductInputField `json:"input_fields,omitempty"`
}
type Category struct {
	ExternalID       string `json:"external_id"`
	ExternalParentID string `json:"external_parent_id,omitempty"`
	Name             string `json:"name"`
	Description      string `json:"description,omitempty"`
	ImageURL         string `json:"image_url,omitempty"`
	Sort             int    `json:"sort,omitempty"`
	Status           string `json:"status,omitempty"`
}
type ProductInputField struct {
	ID                string   `json:"id"`
	Key               string   `json:"key"`
	ExternalKey       string   `json:"external_key,omitempty"`
	Label             string   `json:"label"`
	InputType         string   `json:"input_type"`
	Required          bool     `json:"required"`
	Sensitive         bool     `json:"sensitive"`
	Placeholder       string   `json:"placeholder"`
	HelpText          string   `json:"help_text"`
	Options           []string `json:"options"`
	ValidationPattern string   `json:"validation_pattern"`
	MinLength         int      `json:"min_length"`
	MaxLength         int      `json:"max_length"`
}
type ProductVariant struct {
	ID          string `json:"id"`
	ExternalID  string `json:"external_id"`
	ExternalSKU string `json:"external_sku"`
	Name        string `json:"name"`
	Price       int64  `json:"price"`
	Stock       int64  `json:"stock"`
	Minimum     int    `json:"minimum,omitempty"`
	Maximum     int    `json:"maximum,omitempty"`
	Status      string `json:"status,omitempty"`
}
type CreateOrderRequest struct {
	ClientOrderNo     string            `json:"client_order_no"`
	ExternalProductID string            `json:"external_product_id"`
	Quantity          int               `json:"quantity"`
	Email             string            `json:"email"`
	PaymentMethod     string            `json:"payment_method"`
	CallbackURL       string            `json:"callback_url"`
	Parameters        map[string]string `json:"parameters,omitempty"`
}
type OrderResult struct {
	ExternalOrderNo string   `json:"external_order_no"`
	Status          string   `json:"status"`
	Deliveries      []string `json:"deliveries"`
	Cost            int64    `json:"cost"`
	CostCurrency    string   `json:"cost_currency,omitempty"`
	CostMinorUnit   int      `json:"cost_minor_unit,omitempty"`
}
type BalanceSnapshot struct {
	Balance   int64     `json:"balance"`
	Currency  string    `json:"currency"`
	MinorUnit int       `json:"minor_unit,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

func NewClient(baseURL, key, secret string, allowPrivate bool) *Client {
	return &Client{baseURL: strings.TrimRight(baseURL, "/"), key: key, secret: secret, httpClient: security.NewOutboundHTTPClient(15*time.Second, allowPrivate)}
}
func (c *Client) Categories(ctx context.Context) ([]Category, error) {
	var categories []Category
	if err := c.call(ctx, http.MethodGet, "/openapi/v1/categories", nil, &categories); err != nil {
		return nil, err
	}
	return categories, nil
}
func (c *Client) Products(ctx context.Context) ([]Product, error) {
	var catalog []Product
	if err := c.call(ctx, http.MethodGet, "/openapi/v1/products", nil, &catalog); err != nil {
		return nil, err
	}
	flattened := make([]Product, 0, len(catalog))
	for _, product := range catalog {
		if strings.TrimSpace(product.ExternalID) == "" {
			product.ExternalID = strings.TrimSpace(product.ID)
		}
		variants := product.Variants
		product.Variants = nil
		if len(variants) == 0 {
			flattened = append(flattened, product)
			continue
		}
		for _, variant := range variants {
			externalID := strings.TrimSpace(variant.ExternalID)
			if externalID == "" {
				externalID = strings.TrimSpace(variant.ID)
			}
			item := product
			parentExternalID := strings.TrimSpace(product.ExternalID)
			if parentExternalID == "" {
				parentExternalID = strings.TrimSpace(product.ID)
			}
			item.ParentExternalID = parentExternalID
			item.ID, item.ExternalID, item.ExternalSKU = variant.ID, externalID, variant.ExternalSKU
			item.Name = strings.TrimSpace(product.Name + " / " + variant.Name)
			item.Price, item.Stock = variant.Price, variant.Stock
			item.Minimum = variant.Minimum
			if item.Minimum == 0 {
				item.Minimum = product.Minimum
			}
			item.Maximum = variant.Maximum
			if item.Maximum == 0 {
				item.Maximum = product.Maximum
			}
			if strings.TrimSpace(variant.Status) != "" {
				item.Status = variant.Status
			}
			item.ImageURLs = append([]string(nil), product.ImageURLs...)
			item.InputFields = append([]ProductInputField(nil), product.InputFields...)
			item.Variants = nil
			flattened = append(flattened, item)
		}
	}
	return normalizeProducts(flattened)
}

func (c *Client) Product(ctx context.Context, input ProductDetailRequest) (Product, error) {
	externalID := strings.TrimSpace(input.ExternalProductID)
	if externalID == "" {
		return Product{}, fmt.Errorf("upstream product identifier is required")
	}
	parentExternalID := strings.TrimSpace(input.ParentExternalID)
	pathID := externalID
	if parentExternalID != "" {
		pathID = parentExternalID
	}
	var product Product
	if err := c.call(ctx, http.MethodGet, "/openapi/v1/products/"+url.PathEscape(pathID), nil, &product); err != nil {
		return Product{}, err
	}
	if strings.TrimSpace(product.ExternalID) == "" {
		product.ExternalID = pathID
	}
	if parentExternalID != "" && externalID != parentExternalID {
		return linlinqiVariantProduct(product, externalID)
	}
	return normalizeProduct(product)
}

func linlinqiVariantProduct(product Product, identifier string) (Product, error) {
	identifier = strings.TrimSpace(identifier)
	parentExternalID := strings.TrimSpace(product.ExternalID)
	if parentExternalID == "" {
		parentExternalID = strings.TrimSpace(product.ID)
	}
	for _, variant := range product.Variants {
		if identifier != strings.TrimSpace(variant.ExternalID) && identifier != strings.TrimSpace(variant.ID) && identifier != strings.TrimSpace(variant.ExternalSKU) {
			continue
		}
		item := product
		item.ID = variant.ID
		item.ExternalID = strings.TrimSpace(variant.ExternalID)
		if item.ExternalID == "" {
			item.ExternalID = strings.TrimSpace(variant.ID)
		}
		item.ParentExternalID = parentExternalID
		item.ExternalSKU = variant.ExternalSKU
		item.Name = strings.TrimSpace(product.Name + " / " + variant.Name)
		item.Price, item.Stock = variant.Price, variant.Stock
		item.Minimum = variant.Minimum
		if item.Minimum == 0 {
			item.Minimum = product.Minimum
		}
		item.Maximum = variant.Maximum
		if item.Maximum == 0 {
			item.Maximum = product.Maximum
		}
		if strings.TrimSpace(variant.Status) != "" {
			item.Status = variant.Status
		}
		item.Variants = nil
		return normalizeProduct(item)
	}
	return Product{}, fmt.Errorf("upstream product variant was not found")
}

func (c *Client) Stock(ctx context.Context, input StockRequest) (StockSnapshot, error) {
	input.ExternalProductID = strings.TrimSpace(input.ExternalProductID)
	if input.ExternalProductID == "" {
		return StockSnapshot{}, fmt.Errorf("upstream product identifier is required")
	}
	pathID := input.ExternalProductID
	if input.ParentExternalID = strings.TrimSpace(input.ParentExternalID); input.ParentExternalID != "" {
		pathID = input.ParentExternalID
		if strings.TrimSpace(input.VariantID) == "" && input.ExternalProductID != input.ParentExternalID {
			input.VariantID = input.ExternalProductID
		}
	}
	query := url.Values{}
	if input.VariantID = strings.TrimSpace(input.VariantID); input.VariantID != "" {
		query.Set("variant_id", input.VariantID)
	}
	path := "/openapi/v1/products/" + url.PathEscape(pathID) + "/stock"
	if encoded := query.Encode(); encoded != "" {
		path += "?" + encoded
	}
	var result StockSnapshot
	if err := c.call(ctx, http.MethodGet, path, nil, &result); err != nil {
		return StockSnapshot{}, err
	}
	result.ExternalProductID = strings.TrimSpace(result.ExternalProductID)
	result.StockStatus = strings.ToLower(strings.TrimSpace(result.StockStatus))
	if result.ExternalProductID == "" || result.Stock < 0 || (result.StockStatus != "in_stock" && result.StockStatus != "out_of_stock") || result.ObservedAt.IsZero() {
		return StockSnapshot{}, fmt.Errorf("upstream stock response invalid")
	}
	return result, nil
}

func (c *Client) Quote(ctx context.Context, input QuoteRequest) (QuoteSnapshot, error) {
	input.ExternalProductID = strings.TrimSpace(input.ExternalProductID)
	if input.ExternalProductID == "" || input.Quantity < 1 || input.Quantity > 1000 {
		return QuoteSnapshot{}, fmt.Errorf("upstream quote request is invalid")
	}
	pathID := input.ExternalProductID
	variantID := strings.TrimSpace(input.VariantID)
	if input.ParentExternalID = strings.TrimSpace(input.ParentExternalID); input.ParentExternalID != "" {
		pathID = input.ParentExternalID
		if variantID == "" && input.ExternalProductID != input.ParentExternalID {
			variantID = input.ExternalProductID
		}
	}
	request := struct {
		VariantID string `json:"variant_id,omitempty"`
		Quantity  int    `json:"quantity"`
		Currency  string `json:"currency,omitempty"`
	}{VariantID: variantID, Quantity: input.Quantity, Currency: strings.ToUpper(strings.TrimSpace(input.Currency))}
	var result QuoteSnapshot
	if err := c.call(ctx, http.MethodPost, "/openapi/v1/products/"+url.PathEscape(pathID)+"/quote", request, &result); err != nil {
		return QuoteSnapshot{}, err
	}
	result.Currency = strings.ToUpper(strings.TrimSpace(result.Currency))
	if result.Amount < 0 || len(result.Currency) != 3 || strings.IndexFunc(result.Currency, func(character rune) bool { return character < 'A' || character > 'Z' }) >= 0 || result.MinorUnit < 0 || result.MinorUnit > 6 || result.QuotedAt.IsZero() {
		return QuoteSnapshot{}, fmt.Errorf("upstream quote response invalid")
	}
	return result, nil
}
func (c *Client) CreateOrder(ctx context.Context, input CreateOrderRequest) (OrderResult, error) {
	var out OrderResult
	err := c.call(ctx, http.MethodPost, "/openapi/v1/orders", input, &out)
	return out, err
}
func (c *Client) Balance(ctx context.Context) (BalanceSnapshot, error) {
	var out BalanceSnapshot
	if err := c.call(ctx, http.MethodGet, "/openapi/v1/account/balance", nil, &out); err != nil {
		return BalanceSnapshot{}, err
	}
	out.Currency = strings.ToUpper(strings.TrimSpace(out.Currency))
	if len(out.Currency) != 3 || strings.IndexFunc(out.Currency, func(character rune) bool { return character < 'A' || character > 'Z' }) >= 0 || out.Balance < 0 || out.UpdatedAt.IsZero() {
		return BalanceSnapshot{}, fmt.Errorf("upstream balance response invalid")
	}
	return out, nil
}

func ValidateDeliveries(values []string, quantity int) bool {
	if quantity < 1 || quantity > 20 || len(values) != quantity {
		return false
	}
	totalBytes := 0
	for _, value := range values {
		if value == "" || len(value) > 64<<10 || strings.ContainsRune(value, '\x00') {
			return false
		}
		totalBytes += len(value)
		if totalBytes > 1<<20 {
			return false
		}
	}
	return true
}
func (c *Client) Order(ctx context.Context, no string) (OrderResult, error) {
	var out OrderResult
	err := c.call(ctx, http.MethodGet, "/openapi/v1/orders/"+url.PathEscape(no), nil, &out)
	return out, err
}
func (c *Client) call(ctx context.Context, method, path string, input, out any) error {
	var body []byte
	var err error
	if input != nil {
		body, err = json.Marshal(input)
		if err != nil {
			return err
		}
	}
	timestamp := fmt.Sprintf("%d", time.Now().Unix())
	nonceBytes := make([]byte, 16)
	if _, err := rand.Read(nonceBytes); err != nil {
		return err
	}
	nonce := hex.EncodeToString(nonceBytes)
	digest := sha256.Sum256(body)
	canonical := timestamp + "." + nonce + "." + method + "." + path + "." + hex.EncodeToString(digest[:])
	mac := hmac.New(sha256.New, []byte(c.secret))
	mac.Write([]byte(canonical))
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", c.key)
	req.Header.Set("X-Timestamp", timestamp)
	req.Header.Set("X-Nonce", nonce)
	req.Header.Set("X-Signature", hex.EncodeToString(mac.Sum(nil)))
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("upstream status %d", resp.StatusCode)
	}
	var envelope struct {
		Code    int             `json:"code"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return err
	}
	if envelope.Code != 0 {
		return fmt.Errorf("upstream business error %d", envelope.Code)
	}
	return json.Unmarshal(envelope.Data, out)
}

var _ Gateway = (*Client)(nil)
var _ ProductDetailReader = (*Client)(nil)
var _ StockReader = (*Client)(nil)
var _ PriceQuoter = (*Client)(nil)
