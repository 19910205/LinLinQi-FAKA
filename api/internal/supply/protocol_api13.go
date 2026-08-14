package supply

import (
	"context"
	"errors"
	"net/http"
	"net/url"
)

type api13Protocol struct{ unsupportedShopcloneProtocol }

func init() { registerShopcloneProtocol("api-13", api13Protocol{}) }

func api13Headers(key string) http.Header {
	return http.Header{"ApiKey": {key}, "Cookie": {"language=vi"}}
}

func (api13Protocol) catalog(ctx context.Context, gateway *shopcloneGateway) ([]Category, []Product, error) {
	response, err := gateway.getObject(ctx, "/api/Service/GetAll", nil, api13Headers(gateway.credentials["api_key"]))
	if err != nil || !boolValue(response, "IsSuccessed") {
		return nil, nil, firstError(err, "supplier rejected catalog request")
	}
	products := make([]Product, 0)
	for _, raw := range array(response, "Data") {
		if item, ok := raw.(map[string]any); ok {
			products = append(products, productFromObject(item, "", defaultProductAliases, gateway.money.PriceMinorUnit))
		}
	}
	return nil, products, nil
}

func (api13Protocol) createOrder(ctx context.Context, gateway *shopcloneGateway, input CreateOrderRequest) (OrderResult, error) {
	id, err := requireNumericExternalID(input.ExternalProductID)
	if err != nil {
		return OrderResult{}, err
	}
	account := gateway.credentials["username"]
	response, err := gateway.postJSONObject(ctx, "/api/Service/Buy", nil, map[string]any{
		"serviceId":        id,
		"quantity":         input.Quantity,
		"voucherCode":      account,
		"custom_UserId":    account,
		"custom_OrderId":   input.ClientOrderNo,
		"custom_ExtraData": "<custom_ExtraData>",
	}, api13Headers(gateway.credentials["api_key"]))
	if err != nil {
		return OrderResult{}, err
	}
	if !boolValue(response, "IsSuccessed") {
		return OrderResult{}, errors.New("supplier rejected order")
	}
	return OrderResult{ExternalOrderNo: stringValue(response, "Data"), Status: "processing"}, nil
}

func (api13Protocol) order(ctx context.Context, gateway *shopcloneGateway, externalNo string) (OrderResult, error) {
	response, err := gateway.getObject(ctx, "/api/Order/GetPurchasedAccounts", url.Values{
		"OrderId":       {externalNo},
		"Custom_UserId": {gateway.credentials["username"]},
	}, api13Headers(gateway.credentials["api_key"]))
	if err != nil {
		return OrderResult{}, err
	}
	if !boolValue(response, "IsSuccessed") {
		return OrderResult{}, errors.New("supplier rejected order query")
	}
	return immediateResult(externalNo, stringArray(objectValue(response, "Data")))
}
