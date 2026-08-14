package middleware

import (
	"context"
	"fmt"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"

	"linlinqi/api/pkg/response"
)

var incrementRateLimit = redis.NewScript(`
local count = redis.call("INCR", KEYS[1])
-- Repair a legacy/orphaned counter if an older non-atomic INCR succeeded
-- without its EXPIRE. New counters and all no-TTL counters receive a bound.
if count == 1 or redis.call("PTTL", KEYS[1]) < 0 then
  redis.call("PEXPIRE", KEYS[1], ARGV[1])
end
return count
`)

func rateLimitWindowKey(scope, clientIP string, now time.Time, window time.Duration) string {
	bucket := now.Unix() / int64(window.Seconds())
	return fmt.Sprintf("ratelimit:%s:%s:%d", scope, clientIP, bucket)
}

// RateLimit uses a fixed Redis window and fails closed when Redis is unavailable.
// Every protected route mutates state or handles credentials, so bypassing its
// abuse control would be less safe than returning a temporary 503.
func RateLimit(rdb *redis.Client, scope string, limit int, window time.Duration) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 300*time.Millisecond)
		defer cancel()
		key := rateLimitWindowKey(scope, c.ClientIP(), time.Now(), window)
		count, err := incrementRateLimit.Run(ctx, rdb, []string{key}, (window + time.Second).Milliseconds()).Int64()
		if err != nil {
			response.Error(c, 503, 50303, "error.request_guard_service_unavailable")
			return
		}
		if count > int64(limit) {
			response.Error(c, 429, 42901, "error.request_too_frequent")
			return
		}
		c.Next()
	}
}
