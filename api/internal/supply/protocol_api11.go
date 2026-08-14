package supply

import (
	"context"
	"errors"
	"net/url"
	"strconv"
)

type api11Protocol struct{ unsupportedShopcloneProtocol }

func init() { registerShopcloneProtocol("api-11", api11Protocol{}) }

func (api11Protocol) catalog(ctx context.Context, gateway *shopcloneGateway) ([]Category, []Product, error) {
	response, err := gateway.getObject(ctx, "/user/account_type_facebook", url.Values{"apikey": {gateway.credentials["api_key"]}}, nil)
	if err != nil {
		return nil, nil, err
	}
	products := make([]Product, 0)
	for _, raw := range array(response, "data") {
		if item, ok := raw.(map[string]any); ok {
			products = append(products, productFromObject(item, "", defaultProductAliases, gateway.money.PriceMinorUnit))
		}
	}
	return nil, products, nil
}

func (api11Protocol) balance(ctx context.Context, gateway *shopcloneGateway) (protocolBalance, error) {
	response, err := gateway.getObject(ctx, "/user/balance", url.Values{"apikey": {gateway.credentials["api_key"]}}, nil)
	if err != nil {
		return protocolBalance{}, err
	}
	return protocolBalance{Amount: moneyValueWithExponent(response, gateway.money.BalanceMinorUnit, "balance_facebook"), Currency: currencyValue(response)}, nil
}

func (api11Protocol) createOrder(ctx context.Context, gateway *shopcloneGateway, input CreateOrderRequest) (OrderResult, error) {
	id, err := requireNumericExternalID(input.ExternalProductID)
	if err != nil {
		return OrderResult{}, err
	}
	response, err := gateway.getObject(ctx, "/user/buy_facebook", url.Values{
		"apikey":       {gateway.credentials["api_key"]},
		"account_type": {strconv.FormatInt(id, 10)},
		"quality":      {strconv.Itoa(input.Quantity)},
	}, nil)
	if err != nil {
		return OrderResult{}, err
	}
	if !boolValue(response, "status") {
		return OrderResult{}, errors.New("supplier rejected order")
	}
	data := object(response, "data")
	return immediateResult(stringValue(data, "order_code"), stringArray(objectValue(data, "list_data")))
}
