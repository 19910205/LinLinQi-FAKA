package middleware

import (
	"log/slog"
	"net/netip"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"linlinqi/api/internal/model"
	"linlinqi/api/pkg/response"
)

// IPBlocklist caches parsed prefixes briefly so an attacker cannot turn the
// blocklist into one PostgreSQL query per request. Configuration changes become
// effective within ttl; a stale valid snapshot is retained during DB outages.
func IPBlocklist(db *gorm.DB, scope string, ttl time.Duration) gin.HandlerFunc {
	if ttl < time.Second {
		ttl = 15 * time.Second
	}
	var mu sync.RWMutex
	var prefixes []netip.Prefix
	var loadedAt time.Time
	loaded := false

	refresh := func() error {
		mu.RLock()
		fresh := loaded && time.Since(loadedAt) < ttl
		mu.RUnlock()
		if fresh {
			return nil
		}
		mu.Lock()
		defer mu.Unlock()
		if loaded && time.Since(loadedAt) < ttl {
			return nil
		}
		var records []model.IPBlocklist
		if err := db.Select("cidr").Where("enabled = ? AND scope IN ? AND (expires_at IS NULL OR expires_at > ?)", true, []string{scope, "all"}, time.Now()).Find(&records).Error; err != nil {
			if loaded {
				slog.Error("refresh IP blocklist; using stale snapshot", "error", err)
				return nil
			}
			return err
		}
		next := make([]netip.Prefix, 0, len(records))
		for _, record := range records {
			value := strings.TrimSpace(record.CIDR)
			prefix, err := netip.ParsePrefix(value)
			if err != nil {
				if address, addressErr := netip.ParseAddr(value); addressErr == nil {
					prefix = netip.PrefixFrom(address, address.BitLen())
				} else {
					slog.Error("ignore invalid IP blocklist record", "cidr", value)
					continue
				}
			}
			next = append(next, prefix.Masked())
		}
		prefixes, loadedAt, loaded = next, time.Now(), true
		return nil
	}

	return func(c *gin.Context) {
		if err := refresh(); err != nil {
			response.Error(c, 503, 50390, "error.security_policy_unavailable")
			return
		}
		address, err := netip.ParseAddr(strings.TrimSpace(c.ClientIP()))
		if err != nil {
			response.Error(c, 400, 40090, "error.invalid_client_address")
			return
		}
		address = address.Unmap()
		mu.RLock()
		blocked := false
		for _, prefix := range prefixes {
			candidate := address
			if prefix.Addr().Is4() {
				candidate = address.Unmap()
			}
			if prefix.Contains(candidate) {
				blocked = true
				break
			}
		}
		mu.RUnlock()
		if blocked {
			response.Error(c, 403, 40390, "error.request_blocked_by_security_policy")
			return
		}
		c.Next()
	}
}
