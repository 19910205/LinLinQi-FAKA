package supply

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestDujiaoOrderUsesResponseCurrencyAndPreservesActualCost(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		switch {
		case request.Method == http.MethodPost && request.URL.Path == "/api/v1/upstream/orders":
			_ = json.NewEncoder(writer).Encode(map[string]any{"ok": true, "order_id": 100, "order_no": "DJ-100", "status": "paid", "amount": "1.50", "currency": "USD"})
		case request.Method == http.MethodGet && request.URL.Path == "/api/v1/upstream/orders/100":
			_ = json.NewEncoder(writer).Encode(map[string]any{"ok": true, "order_id": 100, "status": "delivered", "amount": "1.50", "currency": "USD", "fulfillment": map[string]any{"payload": "CARD-1"}})
		default:
			t.Fatalf("unexpected request %s %s", request.Method, request.URL.Path)
		}
	}))
	defer server.Close()
	gateway := newDujiaoNextGateway(server.URL, map[string]string{"api_key": "key", "api_secret": "secret"}, true, MoneySpec{PriceCurrency: "USD", PriceMinorUnit: 2, BalanceCurrency: "USD", BalanceMinorUnit: 2})
	created, err := gateway.CreateOrder(context.Background(), CreateOrderRequest{ClientOrderNo: "LOCAL-1", ExternalProductID: "10", Quantity: 1})
	if err != nil || created.ExternalOrderNo != "100" || created.Status != "processing" || created.Cost != 150 || created.CostCurrency != "USD" || created.CostMinorUnit != 2 {
		t.Fatalf("unexpected create result: %#v err=%v", created, err)
	}
	queried, err := gateway.Order(context.Background(), "100")
	if err != nil || queried.Status != "delivered" || queried.Cost != 150 || queried.CostCurrency != "USD" || len(queried.Deliveries) != 1 {
		t.Fatalf("unexpected query result: %#v err=%v", queried, err)
	}
}

func TestDujiaoOrderRejectsCurrencyMismatchInsteadOfMisbookingCost(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(map[string]any{"ok": true, "order_id": 100, "status": "paid", "amount": "9.90", "currency": "CNY"})
	}))
	defer server.Close()
	gateway := newDujiaoNextGateway(server.URL, map[string]string{"api_key": "key", "api_secret": "secret"}, true, MoneySpec{PriceCurrency: "USD", PriceMinorUnit: 2, BalanceCurrency: "USD", BalanceMinorUnit: 2})
	if _, err := gateway.CreateOrder(context.Background(), CreateOrderRequest{ClientOrderNo: "LOCAL-1", ExternalProductID: "10", Quantity: 1}); err == nil {
		t.Fatal("currency mismatch was silently accepted")
	}
}

func TestDujiaoTerminalStatusSpellingIsCanonical(t *testing.T) {
	for _, status := range []string{"canceled", "cancelled", "refunded"} {
		if normalized := normalizeDujiaoOrderStatus(status); normalized != "cancelled" {
			t.Fatalf("%q normalized to %q", status, normalized)
		}
	}
}
