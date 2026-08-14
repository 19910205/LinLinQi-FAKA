package router

import (
	"os"
	"strings"
	"testing"
)

func TestSharedReadSelectorsKeepFineGrainedPermissionContracts(t *testing.T) {
	routerSource, err := os.ReadFile("router.go")
	if err != nil {
		t.Fatalf("read router source: %v", err)
	}
	source := string(routerSource)
	if !strings.Contains(source, `api.GET("/currency-directory", h.PublicCurrencyDirectory)`) {
		t.Error("public currency directory route contract missing")
	}
	for _, contract := range []string{
		`protected.GET("/categories", middleware.RequireAnyPermission(db, "catalog.view", "supplier.view"), h.AdminCategories)`,
		`protected.GET("/products", middleware.RequireAnyPermission(db, "catalog.view", "order.manage", "marketing.manage"), h.AdminProducts)`,
		`protected.GET("/products/:id/input-fields", middleware.RequireAnyPermission(db, "catalog.view", "order.manage"), h.AdminProductInputFields)`,
		`protected.GET("/orders", middleware.RequireAnyPermission(db, "order.view", "payment.manage"), h.AdminOrders)`,
		`protected.POST("/supplier-category-mappings/media/upload", middleware.RequirePermission(db, "supplier.manage"), h.UploadCatalogMedia)`,
		`protected.GET("/wallets/users/export", middleware.RequirePermission(db, "wallet.view"), h.ExportAdminWalletCustomers)`,
	} {
		if !strings.Contains(source, contract) {
			t.Errorf("fine-grained route permission contract missing: %s", contract)
		}
	}

	middlewareSource, err := os.ReadFile("../middleware/auth.go")
	if err != nil {
		t.Fatalf("read middleware source: %v", err)
	}
	if !strings.Contains(string(middlewareSource), `c.Param("resource") == "variants"`) ||
		!strings.Contains(string(middlewareSource), `"catalog.view", "order.manage"`) {
		t.Error("manual-order variant selector lost its order.manage alternative")
	}
}
