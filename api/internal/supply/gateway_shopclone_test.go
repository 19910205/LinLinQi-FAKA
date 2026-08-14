package supply

import (
	"context"
	"encoding/json"
	"testing"
)

type currencyAwareTestProtocol struct{ unsupportedShopcloneProtocol }

func (currencyAwareTestProtocol) balance(context.Context, *shopcloneGateway) (protocolBalance, error) {
	return protocolBalance{Amount: 123, Currency: " usd "}, nil
}

func TestEveryExecutableShopcloneProtocolHasAnIsolatedAdapter(t *testing.T) {
	expected := map[string][]string{
		"cmsnt": {"username", "password"}, "shopclone7": {"coupon", "api_key"},
		"api-1": {"api_key"}, "api-4": {"username", "password"}, "dongvanfb": {"username", "api_key"},
		"api-6": {"api_key"}, "api-7": {"token"}, "api-8": {"token"}, "api-9": {"api_key"},
		"api-10": {"api_key"}, "api-11": {"api_key"}, "api-12": {"token"},
		"api-13": {"username", "api_key"}, "api-14": {"username", "token"},
		"api-15": {"username", "api_key"}, "api-17": {"username", "password"}, "api-23": {"api_key"},
	}
	if len(shopcloneProtocols) != len(expected) {
		t.Fatalf("expected %d isolated shopclone adapters, got %d", len(expected), len(shopcloneProtocols))
	}
	for code, credentialKeys := range expected {
		if shopcloneProtocols[code] == nil {
			t.Errorf("shopclone protocol %s has no runtime adapter", code)
		}
		definition, exists := Protocol(code)
		if !exists || definition.Family != "shopclone" || definition.Availability != "supported" {
			t.Errorf("shopclone protocol %s has invalid registry metadata: %#v", code, definition)
		}
		actualKeys := CredentialKeys(code)
		if len(actualKeys) != len(credentialKeys) {
			t.Errorf("shopclone protocol %s credential schema mismatch: %v", code, actualKeys)
			continue
		}
		for index := range credentialKeys {
			if actualKeys[index] != credentialKeys[index] {
				t.Errorf("shopclone protocol %s credential schema mismatch: %v", code, actualKeys)
				break
			}
		}
	}
}

func TestLegacyProtocolCurrencyExtractionAndExplicitFallback(t *testing.T) {
	product := productFromObject(jsonObject{"id": "1", "name": "test", "price": 1, "stock": 1, "currency_code": "vnd"}, "", defaultProductAliases)
	if product.Currency != "VND" {
		t.Fatalf("product currency was not normalized: %q", product.Currency)
	}
	product = productFromObject(jsonObject{"id": "1", "name": "test", "price": 1, "stock": 1, "currency": "UPSTREAM"}, "", defaultProductAliases)
	if product.Currency != "" {
		t.Fatalf("invalid product currency must use configured fallback, got %q", product.Currency)
	}
	gateway := &shopcloneGateway{implementation: currencyAwareTestProtocol{}}
	balance, err := gateway.Balance(context.Background())
	if err != nil || balance.Currency != "USD" || balance.Balance != 123 {
		t.Fatalf("balance currency was not normalized: %#v %v", balance, err)
	}
}

func TestLegacyDecimalAmountsNeverSilentlyBecomeZero(t *testing.T) {
	decimal := jsonObject{"amount": json.Number("1.50")}
	if value := intValue(decimal, "amount"); value == 0 {
		t.Fatal("fractional json.Number silently became zero")
	}
	if value := legacyMoneyValue(decimal, "amount"); value != 150 {
		t.Fatalf("decimal amount was not converted to minor units: %d", value)
	}
	product := productFromObject(jsonObject{"id": "1", "name": "test", "price": json.Number("1.50"), "stock": 1}, "", defaultProductAliases)
	if product.Price != 150 {
		t.Fatalf("legacy product decimal price mismatch: %d", product.Price)
	}
	integerProduct := productFromObject(jsonObject{"id": "2", "name": "test", "price": json.Number("150"), "stock": 1}, "", defaultProductAliases)
	if integerProduct.Price != 15_000 {
		t.Fatalf("integer major-unit price was not converted: %d", integerProduct.Price)
	}
	oneDollar := productFromObject(jsonObject{"id": "3", "name": "test", "price": json.Number("1"), "stock": 1}, "", defaultProductAliases)
	if oneDollar.Price != 100 {
		t.Fatalf("one-unit upstream price was not converted to minor units: %d", oneDollar.Price)
	}
	plain, err := parsePlainBalance([]byte("USD 1.5"))
	if err != nil || plain != 150 {
		t.Fatalf("plain decimal balance mismatch: %d, %v", plain, err)
	}
}
