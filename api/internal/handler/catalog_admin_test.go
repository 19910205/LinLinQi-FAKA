package handler

import (
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func cents(value int64) *int64 { return &value }

func TestCatalogCategoryRequestNormalizesAndRejectsUnsafeFields(t *testing.T) {
	req := categoryCatalogRequest{Name: " 数字礼品卡 ", Slug: " Gift-Cards ", Description: " 自动交付 ", Sort: 20, Enabled: true}
	if err := req.normalizeAndValidate(); err != nil {
		t.Fatalf("valid category rejected: %v", err)
	}
	if req.Name != "数字礼品卡" || req.Slug != "gift-cards" || req.Description != "自动交付" {
		t.Fatalf("category was not normalized: %#v", req)
	}
	for _, invalid := range []categoryCatalogRequest{
		{Name: "商品", Slug: "../escape", Enabled: true},
		{Name: "商品", Slug: "UPPER", Enabled: true, Sort: -1},
		{Name: "", Slug: "empty", Enabled: true},
	} {
		if err := invalid.normalizeAndValidate(); err == nil {
			t.Fatalf("invalid category accepted: %#v", invalid)
		}
	}
}

func TestCatalogProductRequestValidatesOperationalInvariants(t *testing.T) {
	req := catalogProductRequest{
		CategoryID: "8d02d80d-ea58-4425-bef0-593462408f7b",
		Name:       " 云服务兑换码 ", Slug: " Cloud-Credit ", Price: cents(9900),
		ComparePrice: 12900, CostPrice: 7000, DeliveryType: "AUTO", InventoryMode: "LOCAL", Status: "ON_SALE",
	}
	if err := req.normalizeAndValidate(); err != nil {
		t.Fatalf("valid product rejected: %v", err)
	}
	if req.Name != "云服务兑换码" || req.Slug != "cloud-credit" || req.Status != "on_sale" || req.InventoryMode != "local" {
		t.Fatalf("product was not normalized: %#v", req)
	}
	badComparison := req
	badComparison.ComparePrice = 100
	if err := badComparison.normalizeAndValidate(); err == nil {
		t.Fatal("compare price below sale price was accepted")
	}
	badMode := req
	badMode.InventoryMode = "warehouse"
	if err := badMode.normalizeAndValidate(); err == nil {
		t.Fatal("unsupported inventory mode was accepted")
	}
	negativeCost := req
	negativeCost.CostPrice = -1
	if err := negativeCost.normalizeAndValidate(); err == nil {
		t.Fatal("negative cost was accepted")
	}
}

func TestCatalogVariantRequestUsesFieldedAttributesAndAllowsFreeProducts(t *testing.T) {
	attributes := map[string]string{" Region ": " CN-1 ", "Duration": "12 months"}
	req := productVariantCatalogRequest{
		ProductID: "8d02d80d-ea58-4425-bef0-593462408f7b", SKU: " sku.cn-12 ", Name: " 中国区一年 ",
		Attributes: &attributes, Price: cents(0), Status: "ACTIVE", PurchaseLimit: 5,
	}
	if err := req.normalizeAndValidate(); err != nil {
		t.Fatalf("valid variant rejected: %v", err)
	}
	if req.SKU != "SKU.CN-12" || req.Name != "中国区一年" || (*req.Attributes)["Region"] != "CN-1" {
		t.Fatalf("variant was not normalized: %#v", req)
	}
	badSKU := req
	badSKU.SKU = "SKU WITH SPACES"
	if err := badSKU.normalizeAndValidate(); err == nil {
		t.Fatal("unsafe SKU was accepted")
	}
	badLimit := req
	badLimit.PurchaseLimit = -1
	if err := badLimit.normalizeAndValidate(); err == nil {
		t.Fatal("negative purchase limit was accepted")
	}
}

func TestCatalogPriceTierRequestValidatesScopeAndWindow(t *testing.T) {
	start := time.Now().UTC().Truncate(time.Second)
	end := start.Add(24 * time.Hour)
	variant := "b337db7b-d54e-462f-b928-c76ccf674904"
	level := "5bd92b8b-690f-49c0-b295-a12dbdb82c41"
	req := productPriceTierCatalogRequest{
		ProductID: "8d02d80d-ea58-4425-bef0-593462408f7b", VariantID: &variant, MemberLevelID: &level,
		MinQuantity: 5, UnitPrice: cents(8800), StartsAt: &start, EndsAt: &end,
	}
	productID, variantID, levelID, err := req.normalizeAndValidate()
	if err != nil || productID.String() != req.ProductID || variantID == nil || levelID == nil {
		t.Fatalf("valid price tier rejected: product=%v variant=%v level=%v err=%v", productID, variantID, levelID, err)
	}
	reversed := req
	reversed.StartsAt, reversed.EndsAt = &end, &start
	if _, _, _, err := reversed.normalizeAndValidate(); err == nil {
		t.Fatal("reversed validity window was accepted")
	}
	zero := req
	zero.UnitPrice = cents(0)
	if _, _, _, err := zero.normalizeAndValidate(); err == nil {
		t.Fatal("zero tier price was accepted")
	}
}

func TestCatalogMemberLevelValidationAndStrictJSON(t *testing.T) {
	req := memberLevelCatalogRequest{Code: " VIP_GOLD ", Name: " 黄金会员 ", MinimumSpend: cents(100000), DiscountBasisPoint: 8500, Priority: 30, Enabled: true}
	if err := req.normalizeAndValidate(); err != nil {
		t.Fatalf("valid member level rejected: %v", err)
	}
	if req.Code != "vip_gold" || req.Name != "黄金会员" {
		t.Fatalf("member level was not normalized: %#v", req)
	}
	invalidDiscount := req
	invalidDiscount.DiscountBasisPoint = 10001
	if err := invalidDiscount.normalizeAndValidate(); err == nil {
		t.Fatal("discount above 100% was accepted")
	}

	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest("POST", "/admin/v1/operations/member-levels", strings.NewReader(`{"code":"vip","name":"VIP","minimum_spend":0,"discount_basis_point":9000,"priority":1,"enabled":true,"server_owned":true}`))
	context.Request.Header.Set("Content-Type", "application/json")
	var strict memberLevelCatalogRequest
	if err := decodeStrictJSON(context, &strict); err == nil {
		t.Fatal("unknown member-level field was accepted")
	}
}
