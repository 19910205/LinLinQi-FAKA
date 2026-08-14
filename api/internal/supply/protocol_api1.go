package supply

import (
	"context"
	"errors"
	"net/url"
	"strconv"
)

type api1Protocol struct{ unsupportedShopcloneProtocol }

func init() { registerShopcloneProtocol("api-1", api1Protocol{}) }

func (api1Protocol) catalog(ctx context.Context, gateway *shopcloneGateway) ([]Category, []Product, error) {
	response, err := gateway.getObject(ctx, "/api/v1/categories", nil, nil)
	if err != nil {
		return nil, nil, err
	}
	categories, products := make([]Category, 0), make([]Product, 0)
	for _, raw := range array(response, "data") {
		item, ok := raw.(map[string]any)
		if !ok {
			return nil, nil, errors.New("supplier category response invalid")
		}
		category := categoryFromObject(item)
		categories = append(categories, category)
		productResponse, err := gateway.getObject(ctx, "/api/v1/category/"+url.PathEscape(category.ExternalID), nil, nil)
		if err != nil {
			return nil, nil, err
		}
		for _, productRaw := range array(productResponse, "data") {
			productItem, ok := productRaw.(map[string]any)
			if !ok {
				return nil, nil, errors.New("supplier product response invalid")
			}
			products = append(products, productFromObject(productItem, category.ExternalID, defaultProductAliases, gateway.money.PriceMinorUnit))
		}
	}
	return categories, products, nil
}

func (api1Protocol) balance(ctx context.Context, gateway *shopcloneGateway) (protocolBalance, error) {
	response, err := gateway.postFormObject(ctx, "/api/v1/balance", url.Values{"api_key": {gateway.credentials["api_key"]}}, nil)
	if err != nil {
		return protocolBalance{}, err
	}
	if !boolValue(response, "status") {
		return protocolBalance{}, errors.New("supplier rejected balance request")
	}
	return protocolBalance{Amount: moneyValueWithExponent(response, gateway.money.BalanceMinorUnit, "balance"), Currency: currencyValue(response)}, nil
}

func (api1Protocol) createOrder(ctx context.Context, gateway *shopcloneGateway, input CreateOrderRequest) (OrderResult, error) {
	id, err := requireNumericExternalID(input.ExternalProductID)
	if err != nil {
		return OrderResult{}, err
	}
	response, err := gateway.postFormObject(ctx, "/api/v1/buy", url.Values{
		"api_key":    {gateway.credentials["api_key"]},
		"id_product": {strconv.FormatInt(id, 10)},
		"quantity":   {strconv.Itoa(input.Quantity)},
	}, nil)
	if err != nil {
		return OrderResult{}, err
	}
	if !boolValue(response, "status") {
		return OrderResult{}, errors.New("supplier rejected order")
	}
	return OrderResult{ExternalOrderNo: stringValue(response, "order_id"), Status: "processing"}, nil
}

func (api1Protocol) order(ctx context.Context, gateway *shopcloneGateway, externalNo string) (OrderResult, error) {
	response, err := gateway.postFormObject(ctx, "/api/v1/order", url.Values{
		"api_key":  {gateway.credentials["api_key"]},
		"order_id": {externalNo},
	}, nil)
	if err != nil {
		return OrderResult{}, err
	}
	if !boolValue(response, "status") {
		return OrderResult{}, errors.New("supplier rejected order query")
	}
	return immediateResult(externalNo, stringArray(objectValue(response, "data")))
}
