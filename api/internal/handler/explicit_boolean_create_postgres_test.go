package handler

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"linlinqi/api/internal/model"
)

func TestCatalogCreatePreservesExplicitDisabledPostgreSQL(t *testing.T) {
	db := isolatedSupplyAdminDB(t, "linlinqi_explicit_bool_test_")
	handler := Handler{DB: db}
	gin.SetMode(gin.TestMode)

	request := func(path, body string, invoke func(*gin.Context)) *httptest.ResponseRecorder {
		t.Helper()
		recorder := httptest.NewRecorder()
		context, _ := gin.CreateTestContext(recorder)
		context.Request = httptest.NewRequest("POST", path, strings.NewReader(body))
		context.Request.Header.Set("Content-Type", "application/json")
		context.Request.Header.Set("X-Change-Reason", "verify explicit disabled persistence")
		invoke(context)
		return recorder
	}

	categoryResponse := request(
		"/admin/v1/catalog/categories",
		`{"name":"Disabled category","slug":"disabled-category","description":"","icon":"","sort":0,"enabled":false}`,
		handler.CreateCatalogCategory,
	)
	if categoryResponse.Code != 201 {
		t.Fatalf("create disabled category status = %d, body = %s", categoryResponse.Code, categoryResponse.Body.String())
	}
	var category model.Category
	if err := db.Where("slug = ?", "disabled-category").First(&category).Error; err != nil {
		t.Fatalf("load disabled category: %v", err)
	}
	if category.Enabled {
		t.Fatal("explicitly disabled category was persisted as enabled")
	}

	activeCategory := model.Category{Name: "Input category", Slug: "input-category", Enabled: true}
	if err := db.Create(&activeCategory).Error; err != nil {
		t.Fatalf("create product category fixture: %v", err)
	}
	product := model.Product{
		CategoryID: activeCategory.ID, Name: "Input product", Slug: "input-product",
		Price: 100, DeliveryType: "manual", InventoryMode: "unlimited", Status: "draft",
	}
	if err := db.Create(&product).Error; err != nil {
		t.Fatalf("create product fixture: %v", err)
	}
	fieldResponse := request(
		"/admin/v1/catalog/products/"+product.ID.String()+"/input-fields",
		`{"key":"account_name","label":"Account name","input_type":"text","required":false,"sensitive":false,"pass_to_supplier":false,"placeholder":"","help_text":"","options":[],"validation_pattern":"","min_length":0,"max_length":100,"sort":0,"enabled":false}`,
		func(context *gin.Context) {
			context.Params = gin.Params{{Key: "id", Value: product.ID.String()}}
			handler.CreateProductInputField(context)
		},
	)
	if fieldResponse.Code != 201 {
		t.Fatalf("create disabled product input status = %d, body = %s", fieldResponse.Code, fieldResponse.Body.String())
	}
	var field model.ProductInputField
	if err := db.Where("product_id = ? AND key = ?", product.ID, "account_name").First(&field).Error; err != nil {
		t.Fatalf("load disabled product input: %v", err)
	}
	if field.Enabled {
		t.Fatal("explicitly disabled product input was persisted as enabled")
	}

	levelResponse := request(
		"/admin/v1/catalog/member-levels",
		`{"code":"disabled_level","name":"Disabled level","minimum_spend":0,"discount_basis_point":0,"priority":1,"enabled":false}`,
		handler.CreateCatalogMemberLevel,
	)
	if levelResponse.Code != 201 {
		t.Fatalf("create disabled member level status = %d, body = %s", levelResponse.Code, levelResponse.Body.String())
	}
	var level model.MemberLevel
	if err := db.Where("code = ?", "disabled_level").First(&level).Error; err != nil {
		t.Fatalf("load disabled member level: %v", err)
	}
	if level.Enabled {
		t.Fatal("explicitly disabled member level was persisted as enabled")
	}
}
