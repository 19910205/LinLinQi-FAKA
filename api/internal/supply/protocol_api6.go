package supply

import (
	"context"
	"errors"
	"net/url"
	"strconv"
)

type api6Protocol struct{ unsupportedShopcloneProtocol }

func init() { registerShopcloneProtocol("api-6", api6Protocol{}) }

func (api6Protocol) catalog(ctx context.Context, gateway *shopcloneGateway) ([]Category, []Product, error) {
	response, err := gateway.getObject(ctx, "/api.php", url.Values{
		"apikey": {gateway.credentials["api_key"]},
		"action": {"get-services"},
	}, nil)
	if err != nil || intValue(response, "code") != 0 {
		return nil, nil, firstError(err, "supplier rejected catalog request")
	}
	products := make([]Product, 0)
	for _, raw := range array(response, "data") {
		if item, ok := raw.(map[string]any); ok {
			products = append(products, productFromObject(item, stringValue(item, "type"), defaultProductAliases, gateway.money.PriceMinorUnit))
		}
	}
	return nil, products, nil
}

func (api6Protocol) balance(ctx context.Context, gateway *shopcloneGateway) (protocolBalance, error) {
	response, err := gateway.getObject(ctx, "/api.php", url.Values{
		"apikey": {gateway.credentials["api_key"]},
		"action": {"get-balance"},
	}, nil)
	if err != nil {
		return protocolBalance{}, err
	}
	return protocolBalance{Amount: moneyValueWithExponent(response, gateway.money.BalanceMinorUnit, "balance"), Currency: currencyValue(response)}, nil
}

func (api6Protocol) createOrder(ctx context.Context, gateway *shopcloneGateway, input CreateOrderRequest) (OrderResult, error) {
	id, err := requireNumericExternalID(input.ExternalProductID)
	if err != nil {
		return OrderResult{}, err
	}
	response, err := gateway.getObject(ctx, "/api.php", url.Values{
		"apikey":     {gateway.credentials["api_key"]},
		"action":     {"create-order"},
		"service_id": {strconv.FormatInt(id, 10)},
		"amount":     {strconv.Itoa(input.Quantity)},
	}, nil)
	if err != nil {
		return OrderResult{}, err
	}
	if intValue(response, "code") != 200 {
		return OrderResult{}, errors.New("supplier rejected order")
	}
	return OrderResult{ExternalOrderNo: stringValue(response, "order_id"), Status: "processing"}, nil
}

func (api6Protocol) order(ctx context.Context, gateway *shopcloneGateway, externalNo string) (OrderResult, error) {
	response, err := gateway.getObject(ctx, "/api.php", url.Values{
		"apikey":   {gateway.credentials["api_key"]},
		"action":   {"get-order-detail"},
		"order_id": {externalNo},
	}, nil)
	if err != nil {
		return OrderResult{}, err
	}
	order := object(response, "order")
	if intValue(order, "status") != 1 {
		return OrderResult{ExternalOrderNo: externalNo, Status: "processing"}, nil
	}
	return immediateResult(externalNo, splitDeliveries(stringValue(order, "data")))
}
