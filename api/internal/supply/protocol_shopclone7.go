package supply

import (
	"context"
	"errors"
	"net/url"
	"strconv"
	"strings"
)

type shopclone7Protocol struct{ unsupportedShopcloneProtocol }

func init() { registerShopcloneProtocol("shopclone7", shopclone7Protocol{}) }

func (shopclone7Protocol) catalog(ctx context.Context, gateway *shopcloneGateway) ([]Category, []Product, error) {
	response, err := gateway.getObject(ctx, "/api/products.php", url.Values{"api_key": {gateway.credentials["api_key"]}}, nil)
	if err != nil || !strings.EqualFold(stringValue(response, "status"), "success") {
		return nil, nil, firstError(err, "supplier rejected catalog request")
	}
	categories, products := make([]Category, 0), make([]Product, 0)
	if err := appendNestedCatalog(&categories, &products, array(response, "categories"), gateway.money.PriceMinorUnit, "products"); err != nil {
		return nil, nil, err
	}
	return categories, products, nil
}

func (shopclone7Protocol) balance(ctx context.Context, gateway *shopcloneGateway) (protocolBalance, error) {
	response, err := gateway.getObject(ctx, "/api/profile.php", url.Values{"api_key": {gateway.credentials["api_key"]}}, nil)
	if err != nil {
		return protocolBalance{}, err
	}
	if !strings.EqualFold(stringValue(response, "status"), "success") {
		return protocolBalance{}, errors.New("supplier rejected balance request")
	}
	data := object(response, "data")
	return protocolBalance{Amount: moneyValueWithExponent(data, gateway.money.BalanceMinorUnit, "money"), Currency: currencyValue(data, response)}, nil
}

func (shopclone7Protocol) createOrder(ctx context.Context, gateway *shopcloneGateway, input CreateOrderRequest) (OrderResult, error) {
	id, err := requireNumericExternalID(input.ExternalProductID)
	if err != nil {
		return OrderResult{}, err
	}
	response, err := gateway.postFormObject(ctx, "/ajaxs/client/product.php", url.Values{
		"action":  {"buyProduct"},
		"id":      {strconv.FormatInt(id, 10)},
		"amount":  {strconv.Itoa(input.Quantity)},
		"coupon":  {gateway.credentials["coupon"]},
		"api_key": {gateway.credentials["api_key"]},
	}, nil)
	if err != nil {
		return OrderResult{}, err
	}
	if !strings.EqualFold(stringValue(response, "status"), "success") {
		return OrderResult{}, errors.New("supplier rejected order")
	}
	return immediateResult(stringValue(response, "trans_id"), stringArray(objectValue(response, "data")))
}
