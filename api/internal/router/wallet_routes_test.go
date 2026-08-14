package router

import (
	"os"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"

	"linlinqi/api/internal/config"
)

func TestWalletOnlyAdminRoutesExist(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := New(
		config.Config{Env: "development", TrustedProxies: []string{}, CORSOrigins: []string{"http://localhost:8082"}},
		&gorm.DB{},
		redis.NewClient(&redis.Options{Addr: "127.0.0.1:6379"}),
		nil,
	)
	routes := make(map[string]bool)
	for _, route := range engine.Routes() {
		routes[route.Method+" "+route.Path] = true
	}
	for _, expected := range []string{
		"GET /admin/v1/wallets/users",
		"GET /admin/v1/wallets/users/:id",
	} {
		if !routes[expected] {
			t.Errorf("missing wallet-only admin route %s", expected)
		}
	}
	routerSource, err := os.ReadFile("router.go")
	if err != nil {
		t.Fatalf("read router source: %v", err)
	}
	for _, contract := range []string{
		`protected.GET("/wallets/users", middleware.RequirePermission(db, "wallet.view"), h.AdminWalletCustomers)`,
		`protected.GET("/wallets/users/:id", middleware.RequirePermission(db, "wallet.view"), h.AdminWalletCustomerDetail)`,
	} {
		if !strings.Contains(string(routerSource), contract) {
			t.Errorf("wallet-only route lost permission contract %s", contract)
		}
	}
}
