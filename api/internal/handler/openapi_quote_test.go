package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"linlinqi/api/internal/model"
	"linlinqi/api/internal/service"
)

func TestOpenAPIProductQuoteRejectsUnknownFieldsBeforeDatabase(t *testing.T) {
	gin.SetMode(gin.TestMode)
	request := httptest.NewRequest(http.MethodPost, "/openapi/v1/products/product-1/quote", bytes.NewBufferString(`{"quantity":1,"unit_amount":1}`))
	request.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = request
	context.Params = gin.Params{{Key: "product_id", Value: "product-1"}}

	(Handler{}).OpenAPIProductQuote(context)
	if recorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want %d; body=%s", recorder.Code, http.StatusUnprocessableEntity, recorder.Body.String())
	}
	var envelope struct {
		Code int `json:"code"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if envelope.Code != 42207 {
		t.Fatalf("business code = %d, want 42207", envelope.Code)
	}
}

func TestOpenAPICredentialPricingContextIsOwnerBound(t *testing.T) {
	ownerID := uuid.New()
	for _, test := range []struct {
		name       string
		ownerType  string
		wantUser   bool
		wantSeller bool
	}{
		{name: "legacy user", ownerType: "", wantUser: true},
		{name: "user", ownerType: "USER", wantUser: true},
		{name: "reseller", ownerType: " reseller ", wantSeller: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			userID, resellerID, err := openAPICredentialPricingContext(model.APICredential{OwnerType: test.ownerType, OwnerID: &ownerID})
			if err != nil {
				t.Fatalf("pricing context: %v", err)
			}
			if (userID != nil) != test.wantUser || (resellerID != nil) != test.wantSeller {
				t.Fatalf("unexpected owner context: user=%v reseller=%v", userID, resellerID)
			}
			if userID != nil && *userID != ownerID {
				t.Fatalf("user id = %s, want %s", *userID, ownerID)
			}
			if resellerID != nil && *resellerID != ownerID {
				t.Fatalf("reseller id = %s, want %s", *resellerID, ownerID)
			}
		})
	}
	if _, _, err := openAPICredentialPricingContext(model.APICredential{}); err == nil {
		t.Fatal("credential without an owner was accepted")
	}
	if _, _, err := openAPICredentialPricingContext(model.APICredential{OwnerType: "admin", OwnerID: &ownerID}); err == nil {
		t.Fatal("unsupported credential owner type was accepted")
	}
}

func TestOpenAPIProductQuoteUsesExactMinorUnitConversionWithoutCostLeak(t *testing.T) {
	productID, variantID := uuid.New(), uuid.New()
	quotedAt := time.Date(2026, 8, 10, 13, 14, 15, 0, time.UTC)
	resolved := service.ResolvedLine{
		Product:   model.Product{Base: model.Base{ID: productID}, Currency: "USD", CostPrice: 99_999},
		VariantID: &variantID, PlatformUnitPrice: 88_888, ResellerMargin: 77_777,
		Quote: service.PriceQuote{UnitPrice: 150, Quantity: 1, Subtotal: 150, Total: 150},
	}
	snapshot := model.FXRateSnapshot{Base: model.Base{ID: uuid.New()}, BaseCode: "USD", QuoteCode: "CNY", Rate: "7.0267", SourceTier: "manual", ExpiresAt: quotedAt.Add(time.Hour)}
	conversion := service.CheckoutCurrencyConversion{
		Source:   model.CurrencyDefinition{Code: "USD", MinorUnit: 2},
		Target:   model.CurrencyDefinition{Code: "CNY", MinorUnit: 2},
		Snapshot: &snapshot,
	}
	result, err := openAPIProductQuoteFromResolved(resolved, conversion, quotedAt)
	if err != nil {
		t.Fatalf("convert quote: %v", err)
	}
	if result.ExternalProductID != productID.String() || result.ProductID != productID || result.VariantID == nil || *result.VariantID != variantID {
		t.Fatalf("unexpected product identity: %#v", result)
	}
	if result.UnitAmount != 1054 || result.Subtotal != 1054 || result.DiscountAmount != 0 || result.Amount != 1054 {
		t.Fatalf("exact USD to CNY quote = %#v, want 1054 minor units", result)
	}
	if result.Currency != "CNY" || result.MinorUnit != 2 || !result.QuotedAt.Equal(quotedAt) || result.FX.Rate != "7.0267" {
		t.Fatalf("unexpected money metadata: %#v", result)
	}
	payload, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal quote: %v", err)
	}
	serialized := string(payload)
	for _, forbidden := range []string{"cost_price", "platform_unit_price", "reseller_margin", "99999", "88888", "77777"} {
		if strings.Contains(serialized, forbidden) {
			t.Fatalf("public quote leaked %q: %s", forbidden, serialized)
		}
	}
}

func TestOpenAPIProductQuoteRejectsCurrencySourceMismatch(t *testing.T) {
	_, err := openAPIProductQuoteFromResolved(
		service.ResolvedLine{Product: model.Product{Currency: "USD"}, Quote: service.PriceQuote{UnitPrice: 1, Quantity: 1, Subtotal: 1, Total: 1}},
		service.CheckoutCurrencyConversion{Source: model.CurrencyDefinition{Code: "CNY", MinorUnit: 2}, Target: model.CurrencyDefinition{Code: "CNY", MinorUnit: 2}},
		time.Now(),
	)
	if err == nil {
		t.Fatal("mismatched product/store currency was accepted")
	}
}
