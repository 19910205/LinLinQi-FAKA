package supply

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

type cmsntProtocol struct{ unsupportedShopcloneProtocol }

func init() { registerShopcloneProtocol("cmsnt", cmsntProtocol{}) }

func (cmsntProtocol) catalog(ctx context.Context, gateway *shopcloneGateway) ([]Category, []Product, error) {
	response, err := gateway.getObject(ctx, "/api/ListResource.php", url.Values{
		"username": {gateway.credentials["username"]},
		"password": {gateway.credentials["password"]},
	}, nil)
	if err != nil || !strings.EqualFold(stringValue(response, "status"), "success") {
		return nil, nil, firstError(err, "supplier rejected catalog request")
	}
	categories, products := make([]Category, 0), make([]Product, 0)
	if err := appendNestedCatalog(&categories, &products, array(response, "categories"), gateway.money.PriceMinorUnit, "accounts"); err != nil {
		return nil, nil, err
	}
	return categories, products, nil
}

func (cmsntProtocol) balance(ctx context.Context, gateway *shopcloneGateway) (protocolBalance, error) {
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

func (cmsntProtocol) createOrder(ctx context.Context, gateway *shopcloneGateway, input CreateOrderRequest) (OrderResult, error) {
	id, err := requireNumericExternalID(input.ExternalProductID)
	if err != nil {
		return OrderResult{}, err
	}
	response, err := gateway.getObject(ctx, "/api/BResource.php", url.Values{
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
