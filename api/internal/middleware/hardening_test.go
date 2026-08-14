package middleware

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestAccessLoggerUsesRouteTemplateAndOmitsURLSecrets(t *testing.T) {
	gin.SetMode(gin.TestMode)
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&output, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })

	router := gin.New()
	router.Use(RequestContext(1<<20), AccessLogger())
	router.GET("/api/v1/carts/:token", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	request := httptest.NewRequest(http.MethodGet, "/api/v1/carts/cart-bearer-secret?oauth_code=query-secret", nil)
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	logged := output.String()
	if !strings.Contains(logged, "/api/v1/carts/:token") {
		t.Fatalf("route template was not logged: %s", logged)
	}
	for _, secret := range []string{"cart-bearer-secret", "query-secret", "oauth_code"} {
		if strings.Contains(logged, secret) {
			t.Fatalf("access log exposed URL secret %q: %s", secret, logged)
		}
	}
}
