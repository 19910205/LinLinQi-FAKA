package supply

import (
	"context"
	"errors"
	"net/url"
	"strconv"
)

type api15Protocol struct{ unsupportedShopcloneProtocol }

func init() { registerShopcloneProtocol("api-15", api15Protocol{}) }

func (api15Protocol) catalog(ctx context.Context, gateway *shopcloneGateway) ([]Category, []Product, error) {
	response, err := gateway.getObject(ctx, "/api/v1/account.php", url.Values{"apikey": {gateway.credentials["api_key"]}}, nil)
	if err != nil || !boolValue(response, "status") {
		return nil, nil, firstError(err, "supplier rejected catalog request")
	}
	categories, products := make([]Category, 0), make([]Product, 0)
	if err := appendNestedCatalog(&categories, &products, array(response, "categories"), gateway.money.PriceMinorUnit, "accounts"); err != nil {
		return nil, nil, err
	}
	return categories, products, nil
}

func (api15Protocol) balance(ctx context.Context, gateway *shopcloneGateway) (protocolBalance, error) {
	response, err := gateway.getObject(ctx, "/api/v1/login.php", url.Values{"password": {gateway.credentials["api_key"]}}, nil)
	if err != nil {
		return protocolBalance{}, err
	}
	if !boolValue(response, "status") {
		return protocolBalance{}, errors.New("supplier rejected balance request")
	}
	return protocolBalance{Amount: moneyValueWithExponent(response, gateway.money.BalanceMinorUnit, "price"), Currency: currencyValue(response)}, nil
}

func (api15Protocol) createOrder(ctx context.Context, gateway *shopcloneGateway, input CreateOrderRequest) (OrderResult, error) {
	id, err := requireNumericExternalID(input.ExternalProductID)
	if err != nil {
		return OrderResult{}, err
	}
	response, err := gateway.getObject(ctx, "/api/v1/buy.php", url.Values{
		"apikey":       {gateway.credentials["api_key"]},
		"account_type": {strconv.FormatInt(id, 10)},
		"quantity":     {strconv.Itoa(input.Quantity)},
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
