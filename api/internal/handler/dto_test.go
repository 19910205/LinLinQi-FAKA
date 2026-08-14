package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"linlinqi/api/internal/model"
)

func TestPublicProductDTOsNeverSerializeCostFields(t *testing.T) {
	category := model.Category{Base: model.Base{ID: uuid.New()}, Name: "软件", Slug: "software"}
	product := model.Product{
		Base:          model.Base{ID: uuid.New()},
		CategoryID:    category.ID,
		Category:      category,
		Name:          "会员",
		Slug:          "membership",
		Price:         1000,
		ComparePrice:  1200,
		CostPrice:     800,
		InventoryMode: "supplier",
	}
	variant := model.ProductVariant{
		Base:         model.Base{ID: uuid.New()},
		ProductID:    product.ID,
		Name:         "一年",
		Price:        1000,
		ComparePrice: 1200,
		CostPrice:    800,
	}

	payload, err := json.Marshal(struct {
		Product  publicProductDTO          `json:"product"`
		Variants []publicProductVariantDTO `json:"variants"`
	}{Product: toPublicProductDTO(product), Variants: []publicProductVariantDTO{toPublicProductVariantDTO(variant, 8)}})
	if err != nil {
		t.Fatalf("marshal public product response: %v", err)
	}
	body := string(payload)
	for _, forbidden := range []string{"cost_price", "inventory_mode", "product_id"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("public product response leaked %q: %s", forbidden, body)
		}
	}
	if !strings.Contains(body, `"compare_price":1200`) {
		t.Fatalf("public comparison price was unexpectedly removed: %s", body)
	}
}

func TestDashboardRecentOrderDTOContainsNoCustomerOrDeliverySecrets(t *testing.T) {
	payload, err := json.Marshal(dashboardRecentOrderDTO{
		OrderNo:   "LQ123",
		Status:    "delivered",
		Total:     1000,
		CreatedAt: time.Unix(1, 0).UTC(),
		Items:     []dashboardRecentOrderItemDTO{{ProductName: "会员"}},
	})
	if err != nil {
		t.Fatalf("marshal dashboard order: %v", err)
	}
	body := string(payload)
	for _, forbidden := range []string{"email", "client_ip", "card_preview", "card_ciphertext", "card_content", "encrypted"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("dashboard response leaked %q: %s", forbidden, body)
		}
	}
}

func TestPublicOrderDTOOmitsInternalRoutingAndKeepsDeliveredContent(t *testing.T) {
	order := model.Order{
		Base: model.Base{ID: uuid.New()}, OrderNo: "LQ123", LookupToken: "one-time-token",
		UserID: func() *uuid.UUID { value := uuid.New(); return &value }(), Email: "buyer@example.com",
		ClientIP: "192.0.2.10", ResellerMargin: 900, Status: "delivered", PaymentStatus: "paid",
		Adjustments: json.RawMessage(`[]`), Total: 1000,
		Items: []model.OrderItem{{Base: model.Base{ID: uuid.New()}, ProductID: uuid.New(), ProductName: "会员", Quantity: 1, UnitPrice: 1000, CardContent: "DELIVERED-CARD", CardPreview: "DE***RD", CardCiphertext: []byte("cipher")}},
	}
	payload, err := json.Marshal(toPublicOrderDTO(order))
	if err != nil {
		t.Fatalf("marshal public order response: %v", err)
	}
	body := string(payload)
	for _, forbidden := range []string{"client_ip", "user_id", "coupon_id", "reseller_id", "reseller_margin", "platform_unit_price", "card_preview", "card_ciphertext", "cipher"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("public order response leaked %q: %s", forbidden, body)
		}
	}
	for _, expected := range []string{`"lookup_token":"one-time-token"`, `"card_content":"DELIVERED-CARD"`} {
		if !strings.Contains(body, expected) {
			t.Fatalf("public order response omitted %s: %s", expected, body)
		}
	}
}

func TestUserAccountDTOsOmitOwnershipAndSecretFields(t *testing.T) {
	identifier := uuid.New()
	now := time.Unix(10, 0).UTC()
	payload, err := json.Marshal(struct {
		Profile    userAffiliateProfileDTO    `json:"profile"`
		Commission userAffiliateCommissionDTO `json:"commission"`
		Withdrawal userAffiliateWithdrawalDTO `json:"withdrawal"`
		Session    userSessionDTO             `json:"session"`
		Credential userAPICredentialDTO       `json:"credential"`
		Webhook    userWebhookEndpointDTO     `json:"webhook"`
		Wallet     userWalletAccountDTO       `json:"wallet"`
	}{
		Profile: userAffiliateProfileDTO{ID: identifier, ReferralCode: "LQREF", Status: "active"},
		Commission: userAffiliateCommissionDTO{
			ID: uuid.New(), OrderID: uuid.New(), Status: "available", SettlesAt: now,
		},
		Withdrawal: userAffiliateWithdrawalDTO{ID: uuid.New(), WithdrawalNo: "WD1", AccountPreview: "***1234"},
		Session:    userSessionDTO{ID: uuid.New(), Device: "Browser", IP: "192.0.2.10", ExpiresAt: now},
		Credential: userAPICredentialDTO{ID: uuid.New(), Name: "integration", Key: "linlinqi_public"},
		Webhook:    userWebhookEndpointDTO{ID: uuid.New(), URL: "https://example.test/hook", Events: `["order.delivered"]`},
		Wallet:     userWalletAccountDTO{ID: uuid.New(), Currency: "CNY", Balance: 100},
	})
	if err != nil {
		t.Fatalf("marshal user account DTOs: %v", err)
	}
	body := string(payload)
	for _, forbidden := range []string{
		"user_id", "buyer_id", "affiliate_id", "processed_by", "owner_id", "owner_type",
		"refresh_hash", "secret_cipher", "secret_nonce", "account_cipher", "account_nonce", "version",
	} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("user account response leaked %q: %s", forbidden, body)
		}
	}
	for _, expected := range []string{`"referral_code":"LQREF"`, `"account_preview":"***1234"`, `"key":"linlinqi_public"`, `"events":"[\"order.delivered\"]"`} {
		if !strings.Contains(body, expected) {
			t.Fatalf("user account response omitted %s: %s", expected, body)
		}
	}
}

func TestDecodeStrictJSONRejectsUnknownProductFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest(http.MethodPatch, "/products/1", strings.NewReader(`{"price":100,"private_note":"not allowed"}`))
	var request updateProductRequest
	if err := decodeStrictJSON(context, &request); err == nil {
		t.Fatal("strict product decoder accepted an unknown field")
	}
}

func TestUpdateProductValidationAndZeroValueUpdates(t *testing.T) {
	negative := int64(-1)
	invalidStatus := "deleted"
	for name, request := range map[string]updateProductRequest{
		"negative price": {Price: &negative},
		"invalid status": {Status: &invalidStatus},
	} {
		t.Run(name, func(t *testing.T) {
			if err := request.normalizeAndValidate(); err == nil {
				t.Fatal("invalid product update was accepted")
			}
		})
	}

	zero := int64(0)
	featured := false
	request := updateProductRequest{Price: &zero, ComparePrice: &zero, CostPrice: &zero, Featured: &featured}
	if err := request.normalizeAndValidate(); err != nil {
		t.Fatalf("zero-valued product update should be valid: %v", err)
	}
	updates := request.updates()
	for _, key := range []string{"price", "compare_price", "cost_price", "featured"} {
		if _, exists := updates[key]; !exists {
			t.Fatalf("zero-valued field %q was dropped from the update", key)
		}
	}
}

func TestValidateProductValues(t *testing.T) {
	if err := validateProductValues(0, 0, 0, "auto", "local", "draft"); err != nil {
		t.Fatalf("valid product values rejected: %v", err)
	}
	for name, values := range map[string]struct {
		price, comparePrice, costPrice int64
		delivery, inventory, status    string
	}{
		"negative cost":     {0, 0, -1, "auto", "local", "draft"},
		"bad delivery":      {0, 0, 0, "instant", "local", "draft"},
		"bad inventory":     {0, 0, 0, "auto", "mixed", "draft"},
		"bad product state": {0, 0, 0, "auto", "local", "deleted"},
	} {
		t.Run(name, func(t *testing.T) {
			if err := validateProductValues(values.price, values.comparePrice, values.costPrice, values.delivery, values.inventory, values.status); err == nil {
				t.Fatal("invalid product values were accepted")
			}
		})
	}
}

func TestCreateProductRequiresExplicitNonNegativePrice(t *testing.T) {
	request := productRequest{CategoryID: uuid.NewString(), Name: "商品", Slug: "product"}
	if err := request.normalizeAndValidate(); err == nil {
		t.Fatal("create product request without price was accepted")
	}

	zero := int64(0)
	request.Price = &zero
	if err := request.normalizeAndValidate(); err != nil {
		t.Fatalf("explicit zero price should be accepted: %v", err)
	}
	if request.DeliveryType != "auto" || request.InventoryMode != "local" || request.Status != "draft" {
		t.Fatalf("unexpected product defaults: %#v", request)
	}
}
