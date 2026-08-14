package handler

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func TestClaimedCartRequiresMatchingAuthenticatedUser(t *testing.T) {
	owner := uuid.New()
	other := uuid.New()
	if !cartAccessibleToUser(nil, nil) || !cartAccessibleToUser(nil, &other) {
		t.Fatal("anonymous carts must remain accessible by their bearer token")
	}
	if cartAccessibleToUser(&owner, nil) {
		t.Fatal("claimed cart remained accessible to an anonymous request")
	}
	if cartAccessibleToUser(&owner, &other) {
		t.Fatal("claimed cart remained accessible to another authenticated user")
	}
	if !cartAccessibleToUser(&owner, &owner) {
		t.Fatal("claimed cart was not accessible to its owner")
	}
}

func TestPaginationBoundsDatabaseOffsetWork(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest("GET", "/items?page=2147483647&page_size=1000000", nil)
	page, pageSize := pagination(context)
	if page != 10000 || pageSize != 100 {
		t.Fatalf("pagination was not bounded: page=%d page_size=%d", page, pageSize)
	}
}

func TestOpenAPICatalogRejectsExcessiveOffset(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest("GET", "/openapi/v1/products?page=10001&page_size=500", nil)
	if _, err := parseOpenAPICatalogQuery(context); err == nil {
		t.Fatal("OpenAPI catalog accepted an excessive database offset")
	}
}
