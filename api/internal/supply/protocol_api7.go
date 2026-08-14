package supply

import (
	"context"
	"errors"
	"net/url"
	"strconv"
	"strings"
)

type api7Protocol struct{ unsupportedShopcloneProtocol }

func init() { registerShopcloneProtocol("api-7", api7Protocol{}) }

func (api7Protocol) catalog(ctx context.Context, gateway *shopcloneGateway) ([]Category, []Product, error) {
	response, err := gateway.getObject(ctx, "/api/san-pham/tat-ca", nil, bearerHeaders(gateway.credentials["token"]))
	if err != nil {
		return nil, nil, err
	}
	products := make([]Product, 0)
	for _, raw := range array(response, "success") {
		if item, ok := raw.(map[string]any); ok {
			products = append(products, productFromObject(item, "", defaultProductAliases, gateway.money.PriceMinorUnit))
		}
	}
	return nil, products, nil
}

func (api7Protocol) createOrder(ctx context.Context, gateway *shopcloneGateway, input CreateOrderRequest) (OrderResult, error) {
	id, err := requireNumericExternalID(input.ExternalProductID)
	if err != nil {
		return OrderResult{}, err
	}
	response, err := gateway.postQueryObject(ctx, "/api/san-pham/mua", url.Values{
		"category": {strconv.FormatInt(id, 10)},
		"quantity": {strconv.Itoa(input.Quantity)},
	}, bearerHeaders(gateway.credentials["token"]))
	if err != nil {
		return OrderResult{}, err
	}
	data := object(response, "success")
	if !strings.EqualFold(stringValue(data, "status"), "completed") {
		return OrderResult{}, errors.New("supplier rejected order")
	}
	return immediateResult(input.ClientOrderNo, stringArray(objectValue(data, "products")))
}
