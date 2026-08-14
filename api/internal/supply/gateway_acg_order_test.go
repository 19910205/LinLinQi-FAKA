package supply

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestACGProductDetailUsesSharedCodeForNewAndOldProtocols(t *testing.T) {
	for _, protocol := range []string{"acg-faka-new", "acg-faka-old"} {
		t.Run(protocol, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if err := request.ParseForm(); err != nil {
					t.Fatal(err)
				}
				if request.URL.Path != "/shared/commodity/item" || request.PostForm.Get("sharedCode") != "P-1" || request.PostForm.Has("code") {
					t.Fatalf("unexpected product detail request: %s %#v", request.URL.Path, request.PostForm)
				}
				writer.Header().Set("Content-Type", "application/json")
				_, _ = writer.Write([]byte(`{"code":200,"data":{"code":"P-1","name":"Product","user_price":"1.00"}}`))
			}))
			defer server.Close()

			gateway := newACGGateway(protocol, server.URL, map[string]string{"app_id": "merchant", "app_key": "secret"}, true, MoneySpec{PriceCurrency: "USD", PriceMinorUnit: 2})
			product, err := gateway.(ProductDetailReader).Product(context.Background(), ProductDetailRequest{ExternalProductID: "P-1"})
			if err != nil || product.ExternalID != "P-1" {
				t.Fatalf("unexpected product result: %#v, %v", product, err)
			}
		})
	}
}

func TestACGCreateOrderPreservesExactCostAndAsyncOrder(t *testing.T) {
	responses := []string{
		`{"code":200,"data":{"trade_no":"UP-DELIVERED","amount":"1.23","currency":"USD","secret":"card-1\ncard-2"}}`,
		`{"code":200,"data":{"tradeNo":"UP-PROCESSING","amount":1.23,"currency_code":"USD","secret":""}}`,
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/shared/commodity/trade" {
			t.Fatalf("unexpected path %s", request.URL.Path)
		}
		if len(responses) == 0 {
			t.Fatal("unexpected extra request")
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(responses[0]))
		responses = responses[1:]
	}))
	defer server.Close()

	gateway := newACGGateway("acg-faka-new", server.URL, map[string]string{"app_id": "merchant", "app_key": "secret"}, true, MoneySpec{PriceCurrency: "USD", PriceMinorUnit: 2})
	delivered, err := gateway.CreateOrder(context.Background(), CreateOrderRequest{ClientOrderNo: "LOCAL-1", ExternalProductID: "P-1", Quantity: 2})
	if err != nil {
		t.Fatal(err)
	}
	if delivered.ExternalOrderNo != "UP-DELIVERED" || delivered.Status != "delivered" || len(delivered.Deliveries) != 2 || delivered.Cost != 123 || delivered.CostCurrency != "USD" || delivered.CostMinorUnit != 2 {
		t.Fatalf("unexpected delivered order: %#v", delivered)
	}

	processing, err := gateway.CreateOrder(context.Background(), CreateOrderRequest{ClientOrderNo: "LOCAL-2", ExternalProductID: "P-1", Quantity: 1})
	if err != nil {
		t.Fatal(err)
	}
	if processing.ExternalOrderNo != "UP-PROCESSING" || processing.Status != "processing" || len(processing.Deliveries) != 0 || processing.Cost != 123 || processing.CostCurrency != "USD" || processing.CostMinorUnit != 2 {
		t.Fatalf("unexpected processing order: %#v", processing)
	}
}

func TestACGOrderQueryPreservesCostAndDeliversOnlyValidSecret(t *testing.T) {
	responses := map[string]string{
		"UP-PENDING":   `{"code":200,"data":{"amount":"10.54","secret":null,"status":1}}`,
		"UP-DELIVERED": `{"code":200,"data":{"trade_no":"UP-DELIVERED","amount":"10.54","secret":"card"}}`,
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if err := request.ParseForm(); err != nil {
			t.Fatal(err)
		}
		orderNo := request.PostForm.Get("tradeNo")
		payload, ok := responses[orderNo]
		if request.URL.Path != "/shared/commodity/query" || !ok {
			t.Fatalf("unexpected query request: %s %#v", request.URL.Path, request.PostForm)
		}
		writer.Header().Set("Content-Type", "application/json")
		_, _ = writer.Write([]byte(payload))
	}))
	defer server.Close()

	gateway := newACGGateway("acg-faka-old", server.URL, map[string]string{"app_id": "merchant", "app_key": "secret"}, true, MoneySpec{PriceCurrency: "CNY", PriceMinorUnit: 2})
	for orderNo, expectedStatus := range map[string]string{"UP-PENDING": "processing", "UP-DELIVERED": "delivered"} {
		result, err := gateway.Order(context.Background(), orderNo)
		if err != nil {
			t.Fatal(err)
		}
		if result.ExternalOrderNo != orderNo || result.Status != expectedStatus || result.Cost != 1054 || result.CostCurrency != "CNY" || result.CostMinorUnit != 2 {
			t.Fatalf("unexpected %s result: %#v", orderNo, result)
		}
		if expectedStatus == "delivered" && len(result.Deliveries) != 1 {
			t.Fatalf("delivered order lost delivery: %#v", result)
		}
	}
}

func TestACGOrderRejectsInvalidAmountDeliveryAndCurrency(t *testing.T) {
	tests := []struct {
		name     string
		data     jsonObject
		money    MoneySpec
		contains string
	}{
		{name: "fraction beyond minor unit", data: jsonObject{"trade_no": "UP-1", "amount": "1.005"}, money: MoneySpec{PriceCurrency: "USD", PriceMinorUnit: 2}, contains: "amount"},
		{name: "invalid delivery shape", data: jsonObject{"trade_no": "UP-1", "amount": "1.00", "secret": []any{"card"}}, money: MoneySpec{PriceCurrency: "USD", PriceMinorUnit: 2}, contains: "delivery"},
		{name: "delivery contains no values", data: jsonObject{"trade_no": "UP-1", "amount": "1.00", "secret": ",,,"}, money: MoneySpec{PriceCurrency: "USD", PriceMinorUnit: 2}, contains: "delivery"},
		{name: "currency conflict", data: jsonObject{"trade_no": "UP-1", "amount": "1.00", "currency": "CNY"}, money: MoneySpec{PriceCurrency: "USD", PriceMinorUnit: 2}, contains: "currency"},
		{name: "currency unavailable", data: jsonObject{"trade_no": "UP-1", "amount": "1.00"}, money: MoneySpec{PriceMinorUnit: 2}, contains: "currency"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			gateway := &acgGateway{money: test.money}
			if _, err := gateway.orderResult(test.data, ""); err == nil || !strings.Contains(strings.ToLower(err.Error()), strings.ToLower(test.contains)) {
				t.Fatalf("expected %s error, got %v", test.contains, err)
			}
		})
	}
}
