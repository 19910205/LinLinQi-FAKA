package supply

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestACGProductDetailAndStockCapabilities(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost {
			t.Fatalf("unexpected method %s", request.Method)
		}
		if err := request.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if request.PostForm.Get("app_id") != "merchant" || request.PostForm.Get("app_key") != "secret" || request.PostForm.Get("sign") != acgSignature(request.PostForm, "secret") {
			t.Fatalf("invalid ACG authentication form: %#v", request.PostForm)
		}
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/shared/commodity/item":
			if request.PostForm.Get("sharedCode") != "P-1" || request.PostForm.Has("code") {
				t.Fatalf("unexpected product detail form %#v", request.PostForm)
			}
			_ = json.NewEncoder(writer).Encode(map[string]any{"code": 200, "data": map[string]any{"code": "P-1", "name": "Product", "user_price": "1.00", "stock": 9, "minimum": 2, "maximum": 5}})
		case "/shared/commodity/stock":
			_ = json.NewEncoder(writer).Encode(map[string]any{"code": 200, "data": map[string]any{"stock": 9}})
		case "/shared/commodity/valuation":
			if request.PostForm.Get("code") != "P-1" || request.PostForm.Get("num") != "2" {
				t.Fatalf("unexpected valuation form: %#v", request.PostForm)
			}
			_ = json.NewEncoder(writer).Encode(map[string]any{"code": 200, "data": map[string]any{"price": "1.23"}})
		case "/shared/commodity/draft":
			_ = json.NewEncoder(writer).Encode(map[string]any{"code": 200, "data": []any{map[string]any{"id": 7, "draft": "****1234"}}})
		default:
			t.Fatalf("unexpected path %s", request.URL.Path)
		}
	}))
	defer server.Close()

	gateway := newACGGateway("acg-faka-new", server.URL, map[string]string{"app_id": "merchant", "app_key": "secret"}, true, MoneySpec{PriceCurrency: "USD", PriceMinorUnit: 2, BalanceCurrency: "USD", BalanceMinorUnit: 2})
	productReader, ok := gateway.(ProductDetailReader)
	if !ok {
		t.Fatal("ACG product detail capability missing")
	}
	product, err := productReader.Product(context.Background(), ProductDetailRequest{ExternalProductID: "P-1"})
	if err != nil {
		t.Fatal(err)
	}
	if product.Price != 100 || product.Currency != "USD" || product.Stock != 9 || product.Minimum != 2 || product.Maximum != 5 {
		t.Fatalf("unexpected normalized product: %#v", product)
	}
	stockReader, ok := gateway.(StockReader)
	if !ok {
		t.Fatal("ACG stock capability missing")
	}
	stock, err := stockReader.Stock(context.Background(), StockRequest{ExternalProductID: "P-1"})
	if err != nil {
		t.Fatal(err)
	}
	if stock.Stock != 9 || stock.StockStatus != "in_stock" || stock.ObservedAt.IsZero() {
		t.Fatalf("unexpected normalized stock: %#v", stock)
	}
	quote, err := gateway.(PriceQuoter).Quote(context.Background(), QuoteRequest{ExternalProductID: "P-1", Quantity: 2})
	if err != nil || quote.Amount != 123 || quote.Currency != "USD" || quote.MinorUnit != 2 {
		t.Fatalf("unexpected ACG quote: %#v %v", quote, err)
	}
	drafts, err := gateway.(DraftCardReader).DraftCards(context.Background(), DraftCardRequest{ExternalProductID: "P-1", Page: 1, PageSize: 20})
	if err != nil || len(drafts.Items) != 1 || drafts.Items[0].ID != "7" || drafts.Items[0].Preview != "****1234" {
		t.Fatalf("unexpected ACG draft page: %#v %v", drafts, err)
	}
}

func TestACGOrderParametersBuildTypedSKUAndProtectReservedFields(t *testing.T) {
	values, err := acgOrderParameters(map[string]string{
		"race": "US", "sku.region": "US", "sku": `{"duration":"30d"}`, "账号": "buyer",
	})
	if err != nil {
		t.Fatal(err)
	}
	if values.Get("race") != "US" || values.Get("sku[region]") != "US" || values.Get("sku[duration]") != "30d" || values.Get("账号") != "buyer" {
		t.Fatalf("ACG typed parameters were not preserved: %#v", values)
	}
	for _, invalid := range []map[string]string{
		{"shared_code": "override"},
		{"sign": "override"},
		{"sku.region": "US", "sku": `{"region":"JP"}`},
		{"sku": `{"region":"US","region":"JP"}`},
	} {
		if _, err := acgOrderParameters(invalid); err == nil {
			t.Fatalf("unsafe ACG parameters accepted: %#v", invalid)
		}
	}
}

func TestDujiaoOrderCancellationCapability(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost || request.URL.Path != "/api/v1/upstream/orders/42/cancel" {
			t.Fatalf("unexpected cancellation request %s %s", request.Method, request.URL.Path)
		}
		for _, header := range []string{"Dujiao-Next-Api-Key", "Dujiao-Next-Timestamp", "Dujiao-Next-Signature"} {
			if request.Header.Get(header) == "" {
				t.Fatalf("missing %s", header)
			}
		}
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(map[string]any{"ok": true, "order_id": 42, "status": "canceled"})
	}))
	defer server.Close()

	gateway := newDujiaoNextGateway(server.URL, map[string]string{"api_key": "key", "api_secret": "secret"}, true, MoneySpec{PriceMinorUnit: 2, BalanceMinorUnit: 2})
	result, err := gateway.CancelOrder(context.Background(), "42")
	if err != nil {
		t.Fatal(err)
	}
	if result.ExternalOrderNo != "42" || result.Status != "cancelled" {
		t.Fatalf("unexpected cancellation result: %#v", result)
	}
}

func TestDujiaoRichProductAndSchemaNormalization(t *testing.T) {
	gateway := newDujiaoNextGateway("https://supplier.example.test", map[string]string{"api_key": "key", "api_secret": "secret"}, false, MoneySpec{PriceCurrency: "USD", PriceMinorUnit: 2, BalanceCurrency: "USD", BalanceMinorUnit: 2})
	item := dujiaoProduct{
		ID: json.Number("10"), CategoryID: json.Number("3"), Title: dujiaoLocalized{"zh-CN": "账号"},
		Description: dujiaoLocalized{"zh-CN": "摘要"}, Content: dujiaoLocalized{"zh-CN": "<p>详情</p>"},
		Images: []string{"https://cdn.example.test/a.png"}, Tags: []string{"US", "2FA"},
		PriceAmount: "1.00", OriginalPrice: "1.50", MemberPrice: "0.90", WholesalePrices: json.RawMessage(`{"10":"0.80"}`),
		Currency: "USD", FulfillmentType: "manual", Active: true, CreatedAt: "2026-08-09T00:00:00Z", UpdatedAt: "2026-08-10T00:00:00Z",
		ManualFormSchema: map[string]any{
			"required": []any{"账号邮箱", "region"},
			"properties": map[string]any{
				"账号邮箱":   map[string]any{"type": "string", "format": "email", "title": "账号邮箱", "description": "用于交付", "x-sensitive": true, "maxLength": float64(190)},
				"region": map[string]any{"type": "string", "title": "地区", "enum": []any{"US", "JP"}},
			},
		},
	}
	item.SKUs = append(item.SKUs, struct {
		ID            json.Number       `json:"id"`
		Code          string            `json:"sku_code"`
		Specs         map[string]string `json:"spec_values"`
		PriceAmount   string            `json:"price_amount"`
		OriginalPrice string            `json:"original_price"`
		MemberPrice   string            `json:"member_price"`
		StockStatus   string            `json:"stock_status"`
		StockQuantity int64             `json:"stock_quantity"`
		Active        bool              `json:"is_active"`
	}{ID: json.Number("101"), Code: "ACC-US", Specs: map[string]string{"地区": "US"}, PriceAmount: "1.00", OriginalPrice: "1.50", MemberPrice: "0.90", StockStatus: "in_stock", StockQuantity: 8, Active: true})

	products, err := gateway.normalizeDujiaoProducts(item)
	if err != nil {
		t.Fatal(err)
	}
	if len(products) != 1 {
		t.Fatalf("unexpected product count %d", len(products))
	}
	product, err := normalizeProduct(products[0])
	if err != nil {
		t.Fatal(err)
	}
	if product.ExternalID != "101" || product.ParentExternalID != "10" || product.Price != 100 || product.OriginalPrice != 150 || product.MemberPrice != 90 || product.Stock != 8 || product.StockStatus != "in_stock" || product.FulfillmentType != "manual" || product.UpstreamUpdatedAt == nil {
		t.Fatalf("rich product fields were lost: %#v", product)
	}
	if len(product.Tags) != 2 || len(product.InputFields) != 2 {
		t.Fatalf("tags/schema were lost: %#v", product)
	}
	for _, field := range product.InputFields {
		if field.Label == "账号邮箱" && (!field.Required || !field.Sensitive || field.InputType != "email" || field.Key == "账号邮箱" || field.ExternalKey != "账号邮箱") {
			t.Fatalf("unsafe localized field was not normalized: %#v", field)
		}
		if field.Key == "region" && (field.InputType != "select" || len(field.Options) != 2) {
			t.Fatalf("enum schema was not preserved: %#v", field)
		}
	}
}
