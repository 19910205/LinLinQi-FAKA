package router

import (
	"context"
	"net/url"
	"strings"
	"sync"
	"time"

	"gorm.io/gorm"
)

const resellerOriginCacheTTL = 30 * time.Second

type resellerOriginCache struct {
	db         *gorm.DB
	production bool
	mu         sync.Mutex
	expiresAt  time.Time
	domains    map[string]struct{}
}

func newResellerOriginChecker(db *gorm.DB, production bool) func(string) bool {
	cache := &resellerOriginCache{db: db, production: production}
	return cache.allowed
}

func resellerOriginDomain(origin string, production bool) (string, bool) {
	if len(origin) == 0 || len(origin) > 512 || strings.TrimSpace(origin) != origin {
		return "", false
	}
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Opaque != "" || parsed.User != nil || parsed.Hostname() == "" || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return "", false
	}
	if parsed.Scheme != "https" && (production || parsed.Scheme != "http") {
		return "", false
	}
	domain := strings.TrimSuffix(strings.ToLower(parsed.Hostname()), ".")
	if domain == "" || strings.ContainsAny(domain, "\x00\r\n") {
		return "", false
	}
	return domain, true
}

func (cache *resellerOriginCache) allowed(origin string) bool {
	domain, valid := resellerOriginDomain(origin, cache.production)
	if !valid || cache.db == nil {
		return false
	}
	domains := cache.snapshot()
	_, allowed := domains[domain]
	return allowed
}

// snapshot refreshes the complete reseller-domain allowlist at most once per
// TTL. Previously every arbitrary Origin value issued its own database query,
// allowing unauthenticated preflight traffic to exhaust the connection pool.
func (cache *resellerOriginCache) snapshot() map[string]struct{} {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	now := time.Now()
	if cache.domains != nil && now.Before(cache.expiresAt) {
		return cache.domains
	}
	queryContext, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	var records []string
	err := cache.db.WithContext(queryContext).Table("reseller_domains rd").
		Joins("JOIN reseller_profiles rp ON rp.id = rd.reseller_id AND rp.deleted_at IS NULL AND rp.status = ?", "active").
		Where("rd.deleted_at IS NULL AND rd.status = ? AND rd.tls_status = ?", "active", "active").
		Pluck("rd.domain", &records).Error
	domains := make(map[string]struct{}, len(records))
	if err == nil {
		for _, record := range records {
			domain := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(record)), ".")
			if domain != "" {
				domains[domain] = struct{}{}
			}
		}
		cache.expiresAt = now.Add(resellerOriginCacheTTL)
	} else {
		// Fail closed, but retry sooner than the normal refresh interval.
		cache.expiresAt = now.Add(5 * time.Second)
	}
	cache.domains = domains
	return cache.domains
}
