package router

import (
	"testing"

	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"

	"linlinqi/api/internal/config"
)

func TestSupplyRoutesUseDedicatedHandlers(t *testing.T) {
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
		"GET /admin/v1/auth/session",
		"GET /openapi/v1/account/balance",
		"GET /admin/v1/suppliers",
		"POST /admin/v1/suppliers",
		"POST /admin/v1/suppliers/sync-all",
		"PATCH /admin/v1/suppliers/:id",
		"DELETE /admin/v1/suppliers/:id",
		"POST /admin/v1/suppliers/:id/sync",
		"POST /admin/v1/suppliers/:id/import",
		"GET /admin/v1/suppliers/:id/import-jobs",
		"GET /admin/v1/suppliers/:id/import-jobs/:job_id",
		"POST /admin/v1/suppliers/:id/import-jobs/:job_id/retry",
		"GET /admin/v1/supplier-category-mappings",
		"GET /admin/v1/supplier-category-mappings/summary",
		"POST /admin/v1/supplier-category-mappings",
		"PATCH /admin/v1/supplier-category-mappings/batch-status",
		"DELETE /admin/v1/supplier-category-mappings/batch",
		"PUT /admin/v1/supplier-category-mappings/:id",
		"DELETE /admin/v1/supplier-category-mappings/:id",
		"POST /admin/v1/supplier-category-mappings/media/upload",
		"GET /admin/v1/supply/catalog",
		"GET /admin/v1/operations/mappings",
		"POST /admin/v1/operations/mappings",
		"PATCH /admin/v1/operations/mappings/:id",
		"DELETE /admin/v1/operations/mappings/:id",
		"GET /admin/v1/operations/procurements",
		"GET /admin/v1/operations/procurements/:id",
	} {
		if !routes[expected] {
			t.Errorf("missing dedicated supply route %s", expected)
		}
	}
}
