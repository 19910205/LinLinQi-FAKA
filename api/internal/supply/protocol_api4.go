package supply

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
)

type api4Protocol struct{ unsupportedShopcloneProtocol }

func init() { registerShopcloneProtocol("api-4", api4Protocol{}) }

func (api4Protocol) login(ctx context.Context, gateway *shopcloneGateway) (string, int64, string, error) {
	response, err := gateway.postFormObject(ctx, "/v1/user/login", url.Values{
		"username": {gateway.credentials["username"]},
		"password": {gateway.credentials["password"]},
	}, nil)
	if err != nil {
		return "", 0, "", err
	}
	data := object(response, "data")
	token := stringValue(data, "accessToken")
	balance := moneyValueWithExponent(object(data, "userDetail"), gateway.money.BalanceMinorUnit, "coin")
	if token == "" || balance < 0 {
		return "", 0, "", errors.New("supplier login response invalid")
	}
	return token, balance, currencyValue(object(data, "userDetail"), data, response), nil
}

func (protocol api4Protocol) catalog(ctx context.Context, gateway *shopcloneGateway) ([]Category, []Product, error) {
	if _, _, _, err := protocol.login(ctx, gateway); err != nil {
		return nil, nil, err
	}
	categoryResponse, err := gateway.getObject(ctx, "/v1/public/productcategory/list", nil, nil)
	if err != nil {
		return nil, nil, err
	}
	categories := make([]Category, 0)
	categoryData := object(categoryResponse, "data")
	for _, raw := range array(categoryData, "data") {
		if item, ok := raw.(map[string]any); ok {
			categories = append(categories, categoryFromObject(item))
		}
	}
	productResponse, err := gateway.getObject(ctx, "/v1/public/category/list", nil, nil)
	if err != nil {
		return nil, nil, err
	}
	products := make([]Product, 0)
	for _, raw := range array(productResponse, "data") {
		item, ok := raw.(map[string]any)
		if !ok {
			return nil, nil, errors.New("supplier product response invalid")
		}
		item = object(item, "category")
		products = append(products, productFromObject(item, stringValue(item, "category"), defaultProductAliases, gateway.money.PriceMinorUnit))
	}
	return categories, products, nil
}

func (protocol api4Protocol) balance(ctx context.Context, gateway *shopcloneGateway) (protocolBalance, error) {
	_, balance, currency, err := protocol.login(ctx, gateway)
	return protocolBalance{Amount: balance, Currency: currency}, err
}

func (protocol api4Protocol) createOrder(ctx context.Context, gateway *shopcloneGateway, input CreateOrderRequest) (OrderResult, error) {
	id, err := requireNumericExternalID(input.ExternalProductID)
	if err != nil {
		return OrderResult{}, err
	}
	accessToken, _, _, err := protocol.login(ctx, gateway)
	if err != nil {
		return OrderResult{}, err
	}
	response, err := gateway.postFormObject(ctx, "/v1/user/partnerbuy", url.Values{
		"amount":     {strconv.Itoa(input.Quantity)},
		"categoryId": {strconv.FormatInt(id, 10)},
	}, http.Header{"Authorization": {accessToken}})
	if err != nil {
		return OrderResult{}, err
	}
	return immediateResult(input.ClientOrderNo, stringArray(objectValue(response, "data")))
}
