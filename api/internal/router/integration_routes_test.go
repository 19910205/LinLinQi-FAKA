package router

import (
	"strings"
	"testing"

	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"

	"linlinqi/api/internal/config"
)

func TestIntegrationRoutesUseDedicatedHandlers(t *testing.T) {
	engine := New(
		config.Config{Env: "development", TrustedProxies: []string{}, CORSOrigins: []string{"http://localhost:8082"}},
		&gorm.DB{},
		redis.NewClient(&redis.Options{Addr: "127.0.0.1:6379"}),
		nil,
	)
	routes := make(map[string]string)
	for _, route := range engine.Routes() {
		routes[route.Method+" "+route.Path] = route.Handler
	}
	for _, expected := range []string{
		"GET /api/v1/me/webhooks",
		"POST /api/v1/me/webhooks",
		"DELETE /api/v1/me/webhooks/:id",
		"GET /admin/v1/integrations/summary",
		"GET /admin/v1/webhooks/endpoints",
		"PATCH /admin/v1/webhooks/endpoints/:id",
		"GET /admin/v1/webhooks/deliveries",
		"POST /admin/v1/webhooks/deliveries/:id/retry",
		"GET /admin/v1/notifications/templates",
		"POST /admin/v1/notifications/templates",
		"PUT /admin/v1/notifications/templates/:id",
		"DELETE /admin/v1/notifications/templates/:id",
		"POST /admin/v1/notifications/templates/:id/test",
		"GET /admin/v1/notifications/deliveries",
		"POST /admin/v1/notifications/deliveries/:id/retry",
	} {
		if _, exists := routes[expected]; !exists {
			t.Errorf("missing dedicated integration route %s", expected)
		}
	}
	if webhookHandler := routes["POST /api/v1/me/webhooks"]; !strings.Contains(webhookHandler, "CreateMyWebhook") {
		t.Errorf("user webhook POST route is not bound to CreateMyWebhook: %s", webhookHandler)
	}
}
