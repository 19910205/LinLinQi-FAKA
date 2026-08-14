package supply

import (
	"context"
	"errors"
	"net/url"
	"strconv"
)

type api10Protocol struct{ unsupportedShopcloneProtocol }

func init() { registerShopcloneProtocol("api-10", api10Protocol{}) }

func (api10Protocol) catalog(ctx context.Context, gateway *shopcloneGateway) ([]Category, []Product, error) {
	response, err := gateway.getObject(ctx, "/mail/currentstock", nil, nil)
	if err != nil {
		return nil, nil, err
	}
	products := make([]Product, 0)
	for _, raw := range array(response, "Data") {
		if item, ok := raw.(map[string]any); ok {
			products = append(products, productFromObject(item, "", defaultProductAliases, gateway.money.PriceMinorUnit))
		}
	}
	return nil, products, nil
}

func (api10Protocol) balance(ctx context.Context, gateway *shopcloneGateway) (protocolBalance, error) {
	response, err := gateway.getObject(ctx, "/user/balance", url.Values{"apikey": {gateway.credentials["api_key"]}}, nil)
	if err != nil {
		return protocolBalance{}, err
	}
	if intValue(response, "Code") != 0 {
		return protocolBalance{}, errors.New("supplier rejected balance request")
	}
	return protocolBalance{Amount: moneyValueWithExponent(response, gateway.money.BalanceMinorUnit, "Balance"), Currency: currencyValue(response)}, nil
}

func (api10Protocol) createOrder(ctx context.Context, gateway *shopcloneGateway, input CreateOrderRequest) (OrderResult, error) {
	response, err := gateway.getObject(ctx, "/mail/buy", url.Values{
		"apikey":   {gateway.credentials["api_key"]},
		"mailcode": {input.ExternalProductID},
		"quantity": {strconv.Itoa(input.Quantity)},
	}, nil)
	if err != nil {
		return OrderResult{}, err
	}
	if intValue(response, "Code") != 0 {
		return OrderResult{}, errors.New("supplier rejected order")
	}
	data := object(response, "Data")
	return immediateResult(stringValue(data, "TransId"), stringArray(objectValue(data, "Emails")))
}
