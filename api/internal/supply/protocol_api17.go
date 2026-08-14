package supply

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

type api17Protocol struct{ unsupportedShopcloneProtocol }

func init() { registerShopcloneProtocol("api-17", api17Protocol{}) }

func (api17Protocol) catalog(ctx context.Context, gateway *shopcloneGateway) ([]Category, []Product, error) {
	response, err := gateway.getObject(ctx, "/api/CategoryList.php", url.Values{
		"username": {gateway.credentials["username"]},
		"password": {gateway.credentials["password"]},
	}, nil)
	if err != nil || !strings.EqualFold(stringValue(response, "status"), "success") {
		return nil, nil, firstError(err, "supplier rejected catalog request")
	}
	products := make([]Product, 0)
	for _, raw := range array(response, "data") {
		if item, ok := raw.(map[string]any); ok {
			products = append(products, productFromObject(item, stringValue(item, "quocgia"), defaultProductAliases, gateway.money.PriceMinorUnit))
		}
	}
	return nil, products, nil
}

func (api17Protocol) balance(ctx context.Context, gateway *shopcloneGateway) (protocolBalance, error) {
	payload, _, err := gateway.transport.do(ctx, http.MethodGet, "/api/GetBalance.php", url.Values{
		"username": {gateway.credentials["username"]},
		"password": {gateway.credentials["password"]},
	}, nil, "", nil)
	if err != nil {
		return protocolBalance{}, err
	}
	amount, err := parsePlainBalance(payload, gateway.money.BalanceMinorUnit)
	return protocolBalance{Amount: amount}, err
}

func (api17Protocol) createOrder(ctx context.Context, gateway *shopcloneGateway, input CreateOrderRequest) (OrderResult, error) {
	id, err := requireNumericExternalID(input.ExternalProductID)
	if err != nil {
		return OrderResult{}, err
	}
	response, err := gateway.getObject(ctx, "/api/BuyProduct.php", url.Values{
		"username": {gateway.credentials["username"]},
		"password": {gateway.credentials["password"]},
		"id":       {strconv.FormatInt(id, 10)},
		"amount":   {strconv.Itoa(input.Quantity)},
	}, nil)
	if err != nil {
		return OrderResult{}, err
	}
	if !strings.EqualFold(stringValue(response, "status"), "success") {
		return OrderResult{}, errors.New("supplier rejected order")
	}
	data := object(response, "data")
	return immediateResult(stringValue(data, "trans_id"), stringArray(objectValue(data, "lists")))
}
