package supply

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestBalanceUsesStandardSupplierContract(t *testing.T) {
	updatedAt := time.Now().UTC().Truncate(time.Second)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/openapi/v1/account/balance" || request.Method != http.MethodGet {
			t.Fatalf("unexpected balance request: %s %s", request.Method, request.URL.Path)
		}
		for _, header := range []string{"X-API-Key", "X-Timestamp", "X-Nonce", "X-Signature"} {
			if strings.TrimSpace(request.Header.Get(header)) == "" {
				t.Fatalf("signed balance request omitted %s", header)
			}
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"code":0,"message":"ok","data":{"balance":123456,"currency":"CNY","updated_at":"` + updatedAt.Format(time.RFC3339) + `"}}`))
	}))
	defer server.Close()

	snapshot, err := NewClient(server.URL, "test-key", "test-secret", true).Balance(context.Background())
	if err != nil {
		t.Fatalf("fetch balance: %v", err)
	}
	if snapshot.Balance != 123456 || snapshot.Currency != "CNY" || !snapshot.UpdatedAt.Equal(updatedAt) {
		t.Fatalf("unexpected balance snapshot: %#v", snapshot)
	}
}

func TestBalanceRejectsInvalidFinancialSnapshots(t *testing.T) {
	for name, data := range map[string]string{
		"negative":         `{"balance":-1,"currency":"CNY","updated_at":"2026-08-09T12:00:00Z"}`,
		"wrong currency":   `{"balance":100,"currency":"USDT","updated_at":"2026-08-09T12:00:00Z"}`,
		"missing currency": `{"balance":100,"updated_at":"2026-08-09T12:00:00Z"}`,
		"missing time":     `{"balance":100,"currency":"CNY"}`,
	} {
		t.Run(name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.Header().Set("Content-Type", "application/json")
				_, _ = writer.Write([]byte(`{"code":0,"message":"ok","data":` + data + `}`))
			}))
			defer server.Close()
			if _, err := NewClient(server.URL, "test-key", "test-secret", true).Balance(context.Background()); err == nil {
				t.Fatal("invalid balance snapshot was accepted")
			}
		})
	}
}

func TestProductsFlattensLinLinQiProductAndVariantIdentifiers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/openapi/v1/products" {
			t.Fatalf("unexpected product path: %s", request.URL.Path)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"code":0,"message":"ok","data":[{"id":"product-id","external_id":"product-external","external_sku":"product-sku","external_category_id":"category-id","name":"Product","summary":"Summary","description":"Description","cover_url":"https://cdn.example.test/cover.png","image_urls":["https://cdn.example.test/cover.png","https://cdn.example.test/detail.png"],"currency":"USD","price":1200,"stock":8,"minimum":2,"maximum":9,"input_fields":[{"id":"field-id","key":"account_id","label":"Account ID","input_type":"text","required":true,"sensitive":true,"options":[],"min_length":4,"max_length":32}],"variants":[{"id":"variant-id","external_id":"variant-external","external_sku":"variant-sku","name":"Large","price":1500,"stock":3,"maximum":5,"status":"active"}]}]}`))
	}))
	defer server.Close()

	products, err := NewClient(server.URL, "test-key", "test-secret", true).Products(context.Background())
	if err != nil {
		t.Fatalf("fetch products: %v", err)
	}
	if len(products) != 1 || products[0].ExternalID != "variant-external" || products[0].ParentExternalID != "product-external" || products[0].Name != "Product / Large" || products[0].Stock != 3 || products[0].Currency != "USD" || products[0].ExternalCategoryID != "category-id" || products[0].Summary != "Summary" || products[0].Description != "Description" || products[0].CoverURL == "" || len(products[0].ImageURLs) != 2 || products[0].Minimum != 2 || products[0].Maximum != 5 || len(products[0].InputFields) != 1 || products[0].InputFields[0].Key != "account_id" {
		t.Fatalf("catalog was not flattened correctly: %#v", products)
	}
}

func TestClientVariantDetailStockAndQuoteUseParentProductContract(t *testing.T) {
	quotedAt := time.Now().UTC().Truncate(time.Second)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/openapi/v1/products/product-parent":
			_, _ = writer.Write([]byte(`{"code":0,"message":"ok","data":{"id":"product-parent","external_id":"product-parent","name":"Product","currency":"CNY","price":900,"stock":8,"minimum":1,"status":"active","variants":[{"id":"variant-1","external_id":"variant-1","external_sku":"sku-1","name":"Large","price":1054,"stock":3,"minimum":2,"maximum":5,"status":"active"}]}}`))
		case "/openapi/v1/products/product-parent/stock":
			if request.URL.Query().Get("variant_id") != "variant-1" {
				t.Fatalf("variant stock query mismatch: %s", request.URL.RawQuery)
			}
			_, _ = writer.Write([]byte(`{"code":0,"message":"ok","data":{"external_product_id":"product-parent","variant_id":"variant-1","stock":3,"stock_status":"in_stock","observed_at":"` + quotedAt.Format(time.RFC3339) + `"}}`))
		case "/openapi/v1/products/product-parent/quote":
			if request.Method != http.MethodPost {
				t.Fatalf("unexpected quote method %s", request.Method)
			}
			var input struct {
				VariantID string `json:"variant_id"`
				Quantity  int    `json:"quantity"`
				Currency  string `json:"currency"`
			}
			if json.NewDecoder(request.Body).Decode(&input) != nil || input.VariantID != "variant-1" || input.Quantity != 2 || input.Currency != "CNY" {
				t.Fatalf("unexpected quote request: %#v", input)
			}
			_, _ = writer.Write([]byte(`{"code":0,"message":"ok","data":{"external_product_id":"product-parent","variant_id":"variant-1","quantity":2,"unit_amount":1054,"subtotal":2108,"discount_amount":0,"amount":2108,"currency":"CNY","minor_unit":2,"quoted_at":"` + quotedAt.Format(time.RFC3339) + `"}}`))
		default:
			t.Fatalf("unexpected request: %s %s", request.Method, request.URL.RequestURI())
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, "test-key", "test-secret", true)
	product, err := client.Product(context.Background(), ProductDetailRequest{ExternalProductID: "variant-1", ParentExternalID: "product-parent"})
	if err != nil || product.ExternalID != "variant-1" || product.ParentExternalID != "product-parent" || product.Price != 1054 || product.Minimum != 2 || product.Maximum != 5 {
		t.Fatalf("variant product mismatch: %#v err=%v", product, err)
	}
	stock, err := client.Stock(context.Background(), StockRequest{ExternalProductID: "variant-1", ParentExternalID: "product-parent"})
	if err != nil || stock.Stock != 3 {
		t.Fatalf("variant stock mismatch: %#v err=%v", stock, err)
	}
	quote, err := client.Quote(context.Background(), QuoteRequest{ExternalProductID: "variant-1", ParentExternalID: "product-parent", Quantity: 2, Currency: "cny"})
	if err != nil || quote.Amount != 2108 || quote.Currency != "CNY" || quote.MinorUnit != 2 || !quote.QuotedAt.Equal(quotedAt) {
		t.Fatalf("variant quote mismatch: %#v err=%v", quote, err)
	}
}

func TestClientCategoriesUsesStandardSupplierContract(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/openapi/v1/categories" || request.Method != http.MethodGet {
			t.Fatalf("unexpected category request: %s %s", request.Method, request.URL.Path)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"code":0,"message":"ok","data":[{"external_id":"category-id","name":"Accounts","description":"Digital accounts","image_url":"https://cdn.example.test/category.png","sort":10,"status":"active"}]}`))
	}))
	defer server.Close()

	categories, err := NewClient(server.URL, "test-key", "test-secret", true).Categories(context.Background())
	if err != nil {
		t.Fatalf("fetch categories: %v", err)
	}
	if len(categories) != 1 || categories[0].ExternalID != "category-id" || categories[0].Description != "Digital accounts" || categories[0].ImageURL == "" {
		t.Fatalf("unexpected categories: %#v", categories)
	}
}

func TestClientProductDetailAndStockContracts(t *testing.T) {
	observedAt := time.Now().UTC().Truncate(time.Second)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch request.URL.Path {
		case "/openapi/v1/products/product-1":
			_, _ = writer.Write([]byte(`{"code":0,"message":"ok","data":{"id":"product-1","external_id":"product-1","external_category_id":"category-1","external_sku":"sku-1","name":"Product","description":"<p>Safe</p>","currency":"USD","price":100,"stock":3,"minimum":1,"status":"active"}}`))
		case "/openapi/v1/products/product-1/stock":
			if request.URL.Query().Get("variant_id") != "variant-1" {
				t.Fatalf("stock variant query missing: %s", request.URL.RawQuery)
			}
			_, _ = writer.Write([]byte(`{"code":0,"message":"ok","data":{"external_product_id":"product-1","variant_id":"variant-1","stock":2,"stock_status":"in_stock","observed_at":"` + observedAt.Format(time.RFC3339) + `"}}`))
		default:
			t.Fatalf("unexpected catalog detail path: %s", request.URL.RequestURI())
		}
	}))
	defer server.Close()
	client := NewClient(server.URL, "test-key", "test-secret", true)
	product, err := client.Product(context.Background(), ProductDetailRequest{ExternalProductID: "product-1"})
	if err != nil || product.ExternalID != "product-1" || product.Price != 100 {
		t.Fatalf("product detail mismatch: %#v err=%v", product, err)
	}
	stock, err := client.Stock(context.Background(), StockRequest{ExternalProductID: "product-1", VariantID: "variant-1"})
	if err != nil || stock.Stock != 2 || stock.StockStatus != "in_stock" || !stock.ObservedAt.Equal(observedAt) {
		t.Fatalf("stock snapshot mismatch: %#v err=%v", stock, err)
	}
}

func TestCreateOrderUsesStandardSupplierContract(t *testing.T) {
	var captured CreateOrderRequest
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/openapi/v1/orders" || request.Method != http.MethodPost {
			t.Fatalf("unexpected order request: %s %s", request.Method, request.URL.Path)
		}
		if err := json.NewDecoder(request.Body).Decode(&captured); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"code":0,"message":"ok","data":{"client_order_no":"LQP-1","external_order_no":"LQ-REMOTE-1","status":"delivered","deliveries":["CARD-1"],"cost":998}}`))
	}))
	defer server.Close()

	input := CreateOrderRequest{ClientOrderNo: "LQP-1", ExternalProductID: "variant-external", Quantity: 1, Email: "buyer@example.com", PaymentMethod: "supplier_balance", CallbackURL: "https://buyer.example.com/api/v1/supplier-callbacks/node", Parameters: map[string]string{"account_id": "123456"}}
	result, err := NewClient(server.URL, "test-key", "test-secret", true).CreateOrder(context.Background(), input)
	if err != nil {
		t.Fatalf("create order: %v", err)
	}
	if !reflect.DeepEqual(captured, input) {
		t.Fatalf("supplier request mismatch: got %#v want %#v", captured, input)
	}
	if result.ExternalOrderNo != "LQ-REMOTE-1" || result.Status != "delivered" || len(result.Deliveries) != 1 || result.Cost != 998 {
		t.Fatalf("supplier response mismatch: %#v", result)
	}
}

func TestClientErrorDoesNotExposeUpstreamResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusBadGateway)
		_, _ = writer.Write([]byte(`{"secret":"upstream-private-value"}`))
	}))
	defer server.Close()

	_, err := NewClient(server.URL, "test-key", "test-secret", true).Products(context.Background())
	if err == nil {
		t.Fatal("non-success upstream response was accepted")
	}
	if strings.Contains(err.Error(), "upstream-private-value") || strings.Contains(err.Error(), "secret") {
		t.Fatalf("upstream response leaked through error: %v", err)
	}
}

func TestClientBusinessErrorDoesNotExposeUpstreamMessage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(`{"code":4201,"message":"credential test-secret rejected","data":null}`))
	}))
	defer server.Close()

	_, err := NewClient(server.URL, "test-key", "test-secret", true).Products(context.Background())
	if err == nil {
		t.Fatal("upstream business error was accepted")
	}
	if strings.Contains(err.Error(), "test-secret") || strings.Contains(err.Error(), "credential") {
		t.Fatalf("upstream business message leaked through error: %v", err)
	}
}
