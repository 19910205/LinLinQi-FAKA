package supply

import (
	"context"
	"errors"
	"net/url"
	"strconv"
)

type api9Protocol struct{ unsupportedShopcloneProtocol }

func init() { registerShopcloneProtocol("api-9", api9Protocol{}) }

func (api9Protocol) catalog(ctx context.Context, gateway *shopcloneGateway) ([]Category, []Product, error) {
	response, err := gateway.getObject(ctx, "/v1/api/categories", url.Values{"api_key": {gateway.credentials["api_key"]}}, nil)
	if err != nil || intValue(response, "error") != 0 {
		return nil, nil, firstError(err, "supplier rejected catalog request")
	}
	categories, products := make([]Category, 0), make([]Product, 0)
	for index, raw := range array(response, "data") {
		group, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		category := Category{ExternalID: stringValue(group, "id"), Name: stringValue(group, "category_name"), Status: "active"}
		if category.ExternalID == "" {
			category.ExternalID = "category-" + strconv.Itoa(index+1)
		}
		categories = append(categories, category)
		for _, childRaw := range array(group, "products") {
			if item, ok := childRaw.(map[string]any); ok {
				products = append(products, productFromObject(item, category.ExternalID, defaultProductAliases, gateway.money.PriceMinorUnit))
			}
		}
	}
	return categories, products, nil
}

func (api9Protocol) balance(ctx context.Context, gateway *shopcloneGateway) (protocolBalance, error) {
	response, err := gateway.getObject(ctx, "/v1/api/me", url.Values{"api_key": {gateway.credentials["api_key"]}}, nil)
	if err != nil {
		return protocolBalance{}, err
	}
	if intValue(response, "error") != 0 {
		return protocolBalance{}, errors.New("supplier rejected balance request")
	}
	data := object(response, "data")
	return protocolBalance{Amount: moneyValueWithExponent(data, gateway.money.BalanceMinorUnit, "balance"), Currency: currencyValue(data, response)}, nil
}

func (api9Protocol) createOrder(ctx context.Context, gateway *shopcloneGateway, input CreateOrderRequest) (OrderResult, error) {
	id, err := requireNumericExternalID(input.ExternalProductID)
	if err != nil {
		return OrderResult{}, err
	}
	response, err := gateway.postJSONObject(ctx, "/v1/api/buy", url.Values{"api_key": {gateway.credentials["api_key"]}}, map[string]any{
		"type_id":  id,
		"quantity": input.Quantity,
	}, nil)
	if err != nil {
		return OrderResult{}, err
	}
	if intValue(response, "error") != 0 {
		return OrderResult{}, errors.New("supplier rejected order")
	}
	data := object(response, "data")
	return immediateResult(stringValue(data, "buy_id"), stringArray(objectValue(data, "data")))
}
