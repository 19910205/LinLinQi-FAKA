package middleware

import (
	"crypto/hmac"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// AccessLogger records the matched route template rather than the raw URL.
// This retains useful endpoint-level observability without copying bearer-like
// path parameters (for example a guest cart token) or query credentials into
// application logs.
func AccessLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		startedAt := time.Now()
		c.Next()
		route := c.FullPath()
		if route == "" {
			route = "unmatched"
		}
		slog.Info("HTTP request",
			"request_id", c.GetString("request_id"),
			"method", c.Request.Method,
			"route", route,
			"status", c.Writer.Status(),
			"latency_ms", time.Since(startedAt).Milliseconds(),
			"response_bytes", c.Writer.Size(),
			"client_ip", c.ClientIP(),
		)
	}
}

func StaticBearer(secret string) gin.HandlerFunc {
	return func(c *gin.Context) {
		provided := strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")
		if provided == "" || !hmac.Equal([]byte(provided), []byte(secret)) {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}
		c.Next()
	}
}

// NoStore disables response caching for authenticated endpoints so that a
// client never renders stale data after a save or state change.
func NoStore() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
		c.Header("Pragma", "no-cache")
		c.Header("Expires", "0")
		c.Next()
	}
}

func RequestContext(maxBodyBytes int64) gin.HandlerFunc {
	return RequestContextWithLimits(maxBodyBytes, nil)
}

func RequestContextWithLimits(maxBodyBytes int64, routeLimits map[string]int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		requestID := strings.TrimSpace(c.GetHeader("X-Request-ID"))
		if _, err := uuid.Parse(requestID); err != nil {
			requestID = uuid.NewString()
		}
		c.Set("request_id", requestID)
		c.Header("X-Request-ID", requestID)
		bodyLimit := maxBodyBytes
		if override, exists := routeLimits[c.FullPath()]; exists {
			bodyLimit = override
		} else if override, exists := routeLimits[c.Request.URL.Path]; exists {
			bodyLimit = override
		}
		if c.Request.Body != nil && bodyLimit > 0 {
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, bodyLimit)
		}
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "DENY")
		c.Header("Referrer-Policy", "strict-origin-when-cross-origin")
		c.Header("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		c.Header("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		c.Next()
	}
}
