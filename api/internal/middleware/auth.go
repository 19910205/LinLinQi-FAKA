package middleware

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"

	"linlinqi/api/internal/model"
	"linlinqi/api/internal/security"
	"linlinqi/api/pkg/response"
)

type Claims struct {
	Realm          string `json:"realm"`
	Role           string `json:"role,omitempty"`
	SessionVersion uint64 `json:"session_version,omitempty"`
	SessionID      string `json:"session_id,omitempty"`
	jwt.RegisteredClaims
}

func IssueToken(subject, realm, role, secret string, ttl time.Duration) (string, error) {
	claims := Claims{Realm: realm, Role: role, RegisteredClaims: jwt.RegisteredClaims{
		Subject: subject, IssuedAt: jwt.NewNumericDate(time.Now()), ExpiresAt: jwt.NewNumericDate(time.Now().Add(ttl)),
	}}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
}

func IssueVersionedToken(subject, realm, role, secret string, sessionVersion uint64, ttl time.Duration) (string, error) {
	claims := Claims{Realm: realm, Role: role, SessionVersion: sessionVersion, RegisteredClaims: jwt.RegisteredClaims{
		Subject: subject, IssuedAt: jwt.NewNumericDate(time.Now()), ExpiresAt: jwt.NewNumericDate(time.Now().Add(ttl)),
	}}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
}

func IssueUserToken(subject, secret, sessionID string, sessionVersion uint64, ttl time.Duration) (string, error) {
	claims := Claims{Realm: "user", SessionID: sessionID, SessionVersion: sessionVersion, RegisteredClaims: jwt.RegisteredClaims{
		Subject: subject, IssuedAt: jwt.NewNumericDate(time.Now()), ExpiresAt: jwt.NewNumericDate(time.Now().Add(ttl)),
	}}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
}

func JWT(secret, realm string) gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		if !strings.HasPrefix(header, "Bearer ") {
			response.Error(c, 401, 40101, "error.login_required")
			return
		}
		token, err := jwt.ParseWithClaims(strings.TrimPrefix(header, "Bearer "), &Claims{}, func(token *jwt.Token) (interface{}, error) {
			return []byte(secret), nil
		}, jwt.WithValidMethods([]string{"HS256"}), jwt.WithExpirationRequired())
		if err != nil || !token.Valid {
			response.Error(c, 401, 40102, "error.login_session_expired")
			return
		}
		claims, ok := token.Claims.(*Claims)
		if !ok || claims.Realm != realm {
			response.Error(c, 403, 40301, "error.resource_access_denied")
			return
		}
		c.Set("subject", claims.Subject)
		c.Set("role", claims.Role)
		c.Set("session_version", claims.SessionVersion)
		c.Set("session_id", claims.SessionID)
		c.Next()
	}
}

// AdminSessionVersion makes administrator JWT revocation immediate. Disabling
// the administrator or incrementing session_version invalidates every token on
// the next request instead of waiting for its normal expiry.
func AdminSessionVersion(db *gorm.DB) gin.HandlerFunc {
	return func(c *gin.Context) {
		adminID, err := uuid.Parse(c.GetString("subject"))
		if err != nil {
			response.Error(c, 401, 40103, "error.invalid_admin_identity")
			return
		}
		var admin struct {
			Status         string
			SessionVersion uint64
		}
		if err := db.Model(&model.Admin{}).Select("status", "session_version").First(&admin, "id = ?", adminID).Error; err != nil || admin.Status != "active" {
			response.Error(c, 401, 40104, "error.admin_session_revoked")
			return
		}
		version, ok := c.Get("session_version")
		tokenVersion, versionOK := version.(uint64)
		if !ok || !versionOK || tokenVersion != admin.SessionVersion {
			response.Error(c, 401, 40104, "error.admin_session_revoked")
			return
		}
		c.Next()
	}
}

// ActiveUser makes customer suspension effective for already-issued access
// tokens. Refresh sessions are revoked by the administrator workflow, while
// this database check closes the remaining access-token validity window.
func ActiveUser(db *gorm.DB) gin.HandlerFunc {
	return activeUser(db, false)
}

// OptionalActiveUser validates an authenticated customer when OptionalJWT has
// attached one, while preserving anonymous storefront access.
func OptionalActiveUser(db *gorm.DB) gin.HandlerFunc {
	return activeUser(db, true)
}

func activeUser(db *gorm.DB, optional bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		subject := strings.TrimSpace(c.GetString("subject"))
		if subject == "" && optional {
			c.Next()
			return
		}
		userID, err := uuid.Parse(subject)
		if err != nil {
			response.Error(c, 401, 40115, "error.invalid_user_identity")
			return
		}
		var user struct {
			Status          string
			SessionVersion  uint64
			PreferredLocale string
		}
		if err := db.Model(&model.User{}).Select("status", "session_version", "preferred_locale").First(&user, "id = ?", userID).Error; err != nil || user.Status != "active" {
			response.Error(c, 401, 40116, "error.user_session_revoked")
			return
		}
		version, versionOK := c.Get("session_version")
		tokenVersion, typeOK := version.(uint64)
		sessionID, sessionErr := uuid.Parse(strings.TrimSpace(c.GetString("session_id")))
		if !versionOK || !typeOK || tokenVersion != user.SessionVersion || sessionErr != nil {
			response.Error(c, 401, 40116, "error.user_session_revoked")
			return
		}
		var activeSession int64
		if err := db.Model(&model.UserSession{}).Where("id = ? AND user_id = ? AND revoked_at IS NULL AND expires_at > ?", sessionID, userID, time.Now()).Count(&activeSession).Error; err != nil || activeSession != 1 {
			response.Error(c, 401, 40116, "error.user_session_revoked")
			return
		}
		locale := normalizeUserLocale(c.GetHeader("X-LinLinQi-Locale"))
		if locale != "" && locale != user.PreferredLocale {
			if err := db.Model(&model.User{}).Where("id = ?", userID).Update("preferred_locale", locale).Error; err != nil {
				response.Error(c, 503, 50301, "error.user_locale_save_failed")
				return
			}
			user.PreferredLocale = locale
		}
		c.Set("user_locale", user.PreferredLocale)
		c.Next()
	}
}

func normalizeUserLocale(value string) string {
	value = strings.TrimSpace(value)
	for _, locale := range []string{"zh-CN", "zh-TW", "en", "vi", "ru", "ja", "ko", "th"} {
		if strings.EqualFold(value, locale) {
			return locale
		}
	}
	return ""
}

// OptionalJWT attaches an authenticated principal to public commerce routes.
// Missing credentials remain anonymous; malformed or expired credentials fail
// explicitly so clients can rotate their access token before retrying an order.
func OptionalJWT(secret, realm string) gin.HandlerFunc {
	return func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		if header == "" {
			c.Next()
			return
		}
		if !strings.HasPrefix(header, "Bearer ") {
			response.Error(c, 401, 40101, "error.invalid_credential_format")
			return
		}
		token, err := jwt.ParseWithClaims(strings.TrimPrefix(header, "Bearer "), &Claims{}, func(token *jwt.Token) (interface{}, error) {
			return []byte(secret), nil
		}, jwt.WithValidMethods([]string{"HS256"}), jwt.WithExpirationRequired())
		claims, ok := tokenClaims(token)
		if err != nil || !ok || claims.Realm != realm {
			response.Error(c, 401, 40102, "error.login_session_expired")
			return
		}
		c.Set("subject", claims.Subject)
		c.Set("role", claims.Role)
		c.Set("session_version", claims.SessionVersion)
		c.Set("session_id", claims.SessionID)
		c.Next()
	}
}

func tokenClaims(token *jwt.Token) (*Claims, bool) {
	if token == nil || !token.Valid {
		return nil, false
	}
	claims, ok := token.Claims.(*Claims)
	return claims, ok
}

func RequirePermission(db *gorm.DB, permission string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !requireAdminPermission(c, db, permission) {
			return
		}
		c.Next()
	}
}

// RequireAnyPermission authorizes a narrowly shared read surface for any of
// the listed business permissions. Each *.view requirement also accepts the
// matching *.manage permission, consistent with RequirePermission.
func RequireAnyPermission(db *gorm.DB, permissions ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !requireAdminAnyPermission(c, db, permissions...) {
			return
		}
		c.Next()
	}
}

// RequireOperationPermission keeps the generic read/write transport while
// enforcing the domain permission associated with each resource. Reads use the
// module's view permission while mutations require the module's manage
// permission, so a read-only operator is never forced to hold system.manage.
func RequireOperationPermission(db *gorm.DB) gin.HandlerFunc {
	modules := map[string]string{
		"refunds": "payment", "reconciliations": "payment", "payment-intents": "payment", "payment-transactions": "payment",
		"wallets": "wallet", "wallet-entries": "wallet",
		"tickets":    "order",
		"risk-rules": "security", "risk-decisions": "security", "login-events": "security", "security-events": "security", "ip-blocklist": "security",
		"procurements": "supplier", "mappings": "supplier",
		"promotions": "marketing", "coupons": "marketing", "gift-cards": "marketing", "affiliates": "marketing", "affiliate-withdrawals": "marketing", "affiliate-commissions": "marketing", "posts": "marketing", "banners": "marketing",
		"resellers": "reseller", "reseller-profiles": "reseller", "reseller-domains": "reseller", "reseller-sites": "reseller", "reseller-product-rules": "reseller", "reseller-withdrawals": "reseller",
		"variants": "catalog", "price-tiers": "catalog", "member-levels": "catalog",
		"inventory-batches": "inventory",
		"notifications":     "system", "notification-templates": "system", "webhook-deliveries": "system", "webhook-endpoints": "system", "roles": "system", "audit-logs": "system", "jobs": "system",
	}
	return func(c *gin.Context) {
		// Manual-order operators need the read-only SKU selector but do not
		// otherwise receive catalog administration access.
		if c.Request.Method == http.MethodGet && c.Param("resource") == "variants" {
			if !requireAdminAnyPermission(c, db, "catalog.view", "order.manage") {
				return
			}
			c.Next()
			return
		}
		module := modules[c.Param("resource")]
		if module == "" {
			module = "system"
		}
		permission := module + ".view"
		if c.Request.Method != http.MethodGet {
			permission = module + ".manage"
		}
		if !requireAdminPermission(c, db, permission) {
			return
		}
		c.Next()
	}
}

func requireAdminPermission(c *gin.Context, db *gorm.DB, permission string) bool {
	return requireAdminAnyPermission(c, db, permission)
}

func requireAdminAnyPermission(c *gin.Context, db *gorm.DB, permissions ...string) bool {
	adminID, err := uuid.Parse(c.GetString("subject"))
	if err != nil {
		response.Error(c, 401, 40103, "error.invalid_admin_identity")
		return false
	}
	// A module's manage permission implies its view permission, so an
	// operator holding manage can read the module even before the finer
	// view permission is assigned.
	codes := make([]string, 0, len(permissions)*2)
	seen := make(map[string]struct{}, len(permissions)*2)
	for _, permission := range permissions {
		permission = strings.TrimSpace(permission)
		if permission == "" {
			continue
		}
		candidates := []string{permission}
		if strings.HasSuffix(permission, ".view") {
			candidates = append(candidates, strings.TrimSuffix(permission, ".view")+".manage")
		}
		for _, candidate := range candidates {
			if _, exists := seen[candidate]; exists {
				continue
			}
			seen[candidate] = struct{}{}
			codes = append(codes, candidate)
		}
	}
	if len(codes) == 0 {
		response.Error(c, 403, 40302, "error.permission")
		return false
	}
	var count int64
	err = db.Table("admin_roles ar").
		Joins("JOIN role_permissions rp ON rp.role_id = ar.role_id").
		Joins("JOIN permissions p ON p.id = rp.permission_id AND p.deleted_at IS NULL").
		Where("ar.admin_id = ? AND p.code IN ?", adminID, codes).Count(&count).Error
	if err != nil || count == 0 {
		response.Error(c, 403, 40302, "error.permission", map[string]interface{}{"Permission": strings.Join(permissions, "|")})
		return false
	}
	return true
}

func requiredOpenAPIPermission(method, path string) string {
	if path == "/openapi/v1/account/balance" {
		return "orders:write"
	}
	if strings.HasPrefix(path, "/openapi/v1/orders") {
		if method == http.MethodPost {
			return "orders:write"
		}
		return "orders:read"
	}
	return "products:read"
}

func OpenAPI(db *gorm.DB, rdb *redis.Client, vault *security.Vault) gin.HandlerFunc {
	return func(c *gin.Context) {
		key := c.GetHeader("X-API-Key")
		timestamp := c.GetHeader("X-Timestamp")
		signature := c.GetHeader("X-Signature")
		nonce := strings.TrimSpace(c.GetHeader("X-Nonce"))
		if len(nonce) < 16 || len(nonce) > 128 {
			response.Error(c, 401, 40123, "error.invalid_request_nonce")
			return
		}
		unix, err := strconv.ParseInt(timestamp, 10, 64)
		if err != nil || time.Since(time.Unix(unix, 0)) > 5*time.Minute || time.Until(time.Unix(unix, 0)) > time.Minute {
			response.Error(c, 401, 40120, "error.invalid_request_timestamp")
			return
		}
		var credential model.APICredential
		if err := db.Where("key = ? AND status = ?", key, "active").First(&credential).Error; err != nil {
			response.Error(c, 401, 40121, "error.invalid_api_credential")
			return
		}
		started := time.Now()
		defer func() {
			db.Create(&model.APICallLog{CredentialID: credential.ID, Method: c.Request.Method, Path: c.Request.URL.Path, StatusCode: c.Writer.Status(), DurationMS: time.Since(started).Milliseconds(), RequestID: c.GetString("request_id"), IP: c.ClientIP()})
		}()
		requiredPermission := requiredOpenAPIPermission(c.Request.Method, c.Request.URL.Path)
		allowed := false
		for _, value := range strings.Split(credential.Permissions, ",") {
			if strings.TrimSpace(value) == requiredPermission {
				allowed = true
				break
			}
		}
		if !allowed {
			response.Error(c, 403, 40320, "error.permission_scope", map[string]interface{}{"Permission": requiredPermission})
			return
		}
		secret, err := vault.Decrypt(credential.SecretCipher, credential.SecretNonce, credential.ID[:])
		if err != nil {
			response.Error(c, 401, 40121, "error.invalid_api_credential")
			return
		}
		body, err := io.ReadAll(c.Request.Body)
		if err != nil {
			response.Error(c, 400, 40020, "error.request_unreadable")
			return
		}
		c.Request.Body = io.NopCloser(bytes.NewReader(body))
		bodySum := sha256.Sum256(body)
		// The exact escaped path and raw query are authenticated. Without the
		// query string, currency and other GET filters could be modified in
		// transit while retaining a valid signature. Requests without a query
		// keep the historical canonical value unchanged.
		requestTarget := c.Request.URL.EscapedPath()
		if c.Request.URL.RawQuery != "" {
			requestTarget += "?" + c.Request.URL.RawQuery
		}
		canonical := timestamp + "." + nonce + "." + c.Request.Method + "." + requestTarget + "." + hex.EncodeToString(bodySum[:])
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write([]byte(canonical))
		expected := hex.EncodeToString(mac.Sum(nil))
		if !hmac.Equal([]byte(expected), []byte(strings.ToLower(signature))) {
			response.Error(c, 401, 40122, "error.invalid_request_signature")
			return
		}
		accepted, err := rdb.SetNX(c, "linlinqi:openapi:nonce:"+credential.ID.String()+":"+nonce, "1", 6*time.Minute).Result()
		if err != nil {
			response.Error(c, 503, 50320, "error.replay_protection_unavailable")
			return
		}
		if !accepted {
			response.Error(c, 409, 40920, "error.duplicate_request_detected")
			return
		}
		now := time.Now()
		db.Model(&credential).Update("last_used_at", &now)
		c.Set("api_credential_id", credential.ID.String())
		c.Next()
	}
}
