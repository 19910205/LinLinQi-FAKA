package supply

import (
	"context"
	"errors"
	"strconv"
)

type api8Protocol struct{ unsupportedShopcloneProtocol }

func init() { registerShopcloneProtocol("api-8", api8Protocol{}) }

func (api8Protocol) catalog(ctx context.Context, gateway *shopcloneGateway) ([]Category, []Product, error) {
	response, err := gateway.postJSONObject(ctx, "/api/v1/s3/services", nil, map[string]any{}, bearerHeaders(gateway.credentials["token"]))
	if err != nil || !boolValue(response, "success") {
		return nil, nil, firstError(err, "supplier rejected catalog request")
	}
	categories, products := make([]Category, 0), make([]Product, 0)
	for index, raw := range array(response, "data") {
		group, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		category := Category{ExternalID: "group-" + strconv.Itoa(index+1), Name: stringValue(group, "name_big"), Status: "active"}
		categories = append(categories, category)
		for _, childRaw := range array(group, "group_childrens") {
			if item, ok := childRaw.(map[string]any); ok {
				products = append(products, productFromObject(item, category.ExternalID, defaultProductAliases, gateway.money.PriceMinorUnit))
			}
		}
	}
	return categories, products, nil
}

func (api8Protocol) balance(ctx context.Context, gateway *shopcloneGateway) (protocolBalance, error) {
	response, err := gateway.postJSONObject(ctx, "/api/v1/s3/get_wallet", nil, map[string]any{}, bearerHeaders(gateway.credentials["token"]))
	if err != nil {
		return protocolBalance{}, err
	}
	data := object(response, "data")
	amount := moneyValueWithExponent(response, gateway.money.BalanceMinorUnit, "data")
	if data != nil {
		amount = moneyValueWithExponent(data, gateway.money.BalanceMinorUnit, "balance", "amount", "money")
	}
	return protocolBalance{Amount: amount, Currency: currencyValue(data, response)}, nil
}

func (api8Protocol) createOrder(ctx context.Context, gateway *shopcloneGateway, input CreateOrderRequest) (OrderResult, error) {
	id, err := requireNumericExternalID(input.ExternalProductID)
	if err != nil {
		return OrderResult{}, err
	}
	response, err := gateway.postJSONObject(ctx, "/api/v1/s3/buy", nil, map[string]any{
		"is_agency": 0,
		"group_id":  id,
		"quantity":  input.Quantity,
	}, bearerHeaders(gateway.credentials["token"]))
	if err != nil {
		return OrderResult{}, err
	}
	if !boolValue(response, "success") {
		return OrderResult{}, errors.New("supplier rejected order")
	}
	data := object(response, "data")
	return immediateResult(stringValue(data, "order_id"), stringArray(objectValue(data, "full_accounts")))
}
