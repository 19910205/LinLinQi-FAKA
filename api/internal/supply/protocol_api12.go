package supply

import (
	"context"
	"errors"
	"net/http"
)

type api12Protocol struct{ unsupportedShopcloneProtocol }

func init() { registerShopcloneProtocol("api-12", api12Protocol{}) }

func api12Authorization(gateway *shopcloneGateway) http.Header {
	return http.Header{"Authorization": {gateway.credentials["token"]}}
}

func (api12Protocol) catalog(ctx context.Context, gateway *shopcloneGateway) ([]Category, []Product, error) {
	response, err := gateway.postJSONObject(ctx, "/api", nil, map[string]any{"act": "Get-Products"}, api12Authorization(gateway))
	if err != nil {
		return nil, nil, err
	}
	products := make([]Product, 0)
	for _, raw := range array(response, "products") {
		if item, ok := raw.(map[string]any); ok {
			products = append(products, productFromObject(item, "", defaultProductAliases, gateway.money.PriceMinorUnit))
		}
	}
	return nil, products, nil
}

func (api12Protocol) balance(ctx context.Context, gateway *shopcloneGateway) (protocolBalance, error) {
	response, err := gateway.postJSONObject(ctx, "/api", nil, map[string]any{"act": "Me"}, api12Authorization(gateway))
	if err != nil {
		return protocolBalance{}, err
	}
	if intValue(response, "error_code") != 0 {
		return protocolBalance{}, errors.New("supplier rejected balance request")
	}
	user := object(response, "user")
	return protocolBalance{Amount: moneyValueWithExponent(user, gateway.money.BalanceMinorUnit, "balance"), Currency: currencyValue(user, response)}, nil
}

func (api12Protocol) createOrder(ctx context.Context, gateway *shopcloneGateway, input CreateOrderRequest) (OrderResult, error) {
	id, err := requireNumericExternalID(input.ExternalProductID)
	if err != nil {
		return OrderResult{}, err
	}
	response, err := gateway.postJSONObject(ctx, "/api", nil, map[string]any{
		"act":  "Create-Order",
		"data": map[string]any{"service_id": id, "quantity": input.Quantity},
	}, api12Authorization(gateway))
	if err != nil {
		return OrderResult{}, err
	}
	if intValue(response, "error_code") != 0 {
		return OrderResult{}, errors.New("supplier rejected order")
	}
	return OrderResult{ExternalOrderNo: stringValue(response, "order_id"), Status: "processing"}, nil
}

func (api12Protocol) order(ctx context.Context, gateway *shopcloneGateway, externalNo string) (OrderResult, error) {
	response, err := gateway.postJSONObject(ctx, "/api", nil, map[string]any{
		"act":  "Get-Order",
		"data": map[string]any{"order_id": externalNo},
	}, api12Authorization(gateway))
	if err != nil {
		return OrderResult{}, err
	}
	if intValue(response, "error_code") != 0 {
		return OrderResult{}, errors.New("supplier rejected order query")
	}
	data := object(response, "data")
	return immediateResult(externalNo, splitDeliveries(stringValue(data, "data")))
}
