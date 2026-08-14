package supply

import (
	"context"
	"errors"
	"net/http"
	"strings"
)

type api23Protocol struct{ unsupportedShopcloneProtocol }

func init() { registerShopcloneProtocol("api-23", api23Protocol{}) }

func (api23Protocol) catalog(ctx context.Context, gateway *shopcloneGateway) ([]Category, []Product, error) {
	payload, _, err := gateway.transport.do(ctx, http.MethodGet, "/api/get_services", nil, nil, "", nil)
	if err != nil {
		return nil, nil, err
	}
	items, err := decodeArray(payload)
	if err != nil {
		return nil, nil, err
	}
	products := make([]Product, 0)
	for _, raw := range items {
		if item, ok := raw.(map[string]any); ok {
			product := productFromObject(item, "", map[string][]string{
				"id": {"name_service"}, "name": {"name_service"}, "price": {"price"}, "stock": {"quantity"},
			}, gateway.money.PriceMinorUnit)
			if product.Stock == 0 {
				product.Stock = 1_000_000_000
			}
			products = append(products, product)
		}
	}
	return nil, products, nil
}

func (api23Protocol) createOrder(ctx context.Context, gateway *shopcloneGateway, input CreateOrderRequest) (OrderResult, error) {
	response, err := gateway.postJSONObject(ctx, "/api/create_order", nil, map[string]any{
		"service":  input.ExternalProductID,
		"quantity": input.Quantity,
		"api_key":  gateway.credentials["api_key"],
	}, nil)
	if err != nil {
		return OrderResult{}, err
	}
	if !strings.EqualFold(stringValue(response, "status"), "success") {
		return OrderResult{}, errors.New("supplier rejected order")
	}
	return immediateResult(input.ClientOrderNo, splitDeliveries(stringValue(response, "data")))
}
