package middleware

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"
)

func TestRateLimitWindowKeyIsolatesScopeClientAndWindow(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	window := time.Minute
	baseline := rateLimitWindowKey("login", "198.51.100.10", now, window)
	for name, candidate := range map[string]string{
		"client": rateLimitWindowKey("login", "198.51.100.11", now, window),
		"scope":  rateLimitWindowKey("register", "198.51.100.10", now, window),
		"window": rateLimitWindowKey("login", "198.51.100.10", now.Add(window), window),
	} {
		if candidate == baseline {
			t.Fatalf("%s did not receive an isolated rate-limit key: %q", name, candidate)
		}
	}
}

func TestRateLimitFailsClosedWhenRedisIsUnavailable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rdb := redis.NewClient(&redis.Options{
		Addr:       "redis.invalid:6379",
		MaxRetries: -1,
		Dialer: func(context.Context, string, string) (net.Conn, error) {
			return nil, errors.New("redis unavailable")
		},
	})
	t.Cleanup(func() { _ = rdb.Close() })

	called := false
	router := gin.New()
	router.GET("/protected", RateLimit(rdb, "test", 1, time.Minute), func(c *gin.Context) {
		called = true
		c.Status(http.StatusNoContent)
	})
	request := httptest.NewRequest(http.MethodGet, "/protected", nil)
	request.RemoteAddr = "198.51.100.10:12345"
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)

	if recorder.Code != http.StatusServiceUnavailable || called {
		t.Fatalf("Redis failure must fail closed: status=%d handler_called=%v body=%s", recorder.Code, called, recorder.Body.String())
	}
}

func TestGinClientIPOnlyTrustsConfiguredProxy(t *testing.T) {
	gin.SetMode(gin.TestMode)
	clientIP := func(trusted []string, remoteAddr string) string {
		t.Helper()
		router := gin.New()
		if err := router.SetTrustedProxies(trusted); err != nil {
			t.Fatalf("configure trusted proxies: %v", err)
		}
		observed := ""
		router.GET("/", func(c *gin.Context) {
			observed = c.ClientIP()
			c.Status(http.StatusNoContent)
		})
		request := httptest.NewRequest(http.MethodGet, "/", nil)
		request.RemoteAddr = remoteAddr
		request.Header.Set("X-Forwarded-For", "203.0.113.77")
		router.ServeHTTP(httptest.NewRecorder(), request)
		return observed
	}

	if observed := clientIP(nil, "198.51.100.10:12345"); observed != "198.51.100.10" {
		t.Fatalf("untrusted forwarding header changed client IP: %q", observed)
	}
	if observed := clientIP([]string{"10.0.0.0/8"}, "10.1.2.3:12345"); observed != "203.0.113.77" {
		t.Fatalf("trusted proxy forwarding header was not honored: %q", observed)
	}
}
