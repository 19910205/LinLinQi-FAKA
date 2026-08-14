package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/pquerna/otp/totp"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"

	"linlinqi/api/internal/config"
	"linlinqi/api/internal/middleware"
	"linlinqi/api/internal/model"
	"linlinqi/api/internal/security"
)

func TestRecoveryCodeCanOnlyBeConsumedOnce(t *testing.T) {
	codes, hashes, err := recoveryCodes(10)
	if err != nil {
		t.Fatalf("generate recovery codes: %v", err)
	}
	if len(codes) != 10 || len(hashes) != 10 {
		t.Fatalf("unexpected recovery-code count: %d/%d", len(codes), len(hashes))
	}
	format := regexp.MustCompile(`^[0-9A-F]{6}-[0-9A-F]{6}$`)
	seen := make(map[string]struct{}, len(codes))
	for _, code := range codes {
		if !format.MatchString(code) {
			t.Fatalf("unexpected recovery-code format: %q", code)
		}
		if _, duplicate := seen[code]; duplicate {
			t.Fatalf("duplicate recovery code generated: %q", code)
		}
		seen[code] = struct{}{}
	}

	stored := strings.Join(hashes, ",")
	remaining, consumed := consumeRecoveryHash(stored, "  "+strings.ToLower(codes[0])+"  ")
	if !consumed {
		t.Fatal("expected normalized recovery code to be consumed")
	}
	if got := len(strings.Split(remaining, ",")); got != 9 {
		t.Fatalf("expected 9 recovery hashes, got %d", got)
	}
	again, consumedAgain := consumeRecoveryHash(remaining, codes[0])
	if consumedAgain {
		t.Fatal("recovery code was accepted twice")
	}
	if again != remaining {
		t.Fatal("unknown recovery code changed the stored hash set")
	}
}

func TestRefreshTokenReuseClassification(t *testing.T) {
	now := time.Now()
	hash := refreshHash("refresh-token")
	session := model.UserSession{RefreshHash: hash}
	token := model.UserSessionToken{}
	if refreshTokenWasReused(token, session, hash) {
		t.Fatal("active current refresh token was classified as reuse")
	}

	used := token
	used.UsedAt = &now
	if !refreshTokenWasReused(used, session, hash) {
		t.Fatal("used refresh token was not classified as reuse")
	}
	revoked := token
	revoked.RevokedAt = &now
	if !refreshTokenWasReused(revoked, session, hash) {
		t.Fatal("revoked refresh token was not classified as reuse")
	}
	revokedSession := session
	revokedSession.RevokedAt = &now
	if !refreshTokenWasReused(token, revokedSession, hash) {
		t.Fatal("token from a revoked session was not classified as reuse")
	}
	if !refreshTokenWasReused(token, session, refreshHash("different-token")) {
		t.Fatal("superseded refresh token was not classified as reuse")
	}
}

func TestTruncateSecurityValuePreservesUTF8(t *testing.T) {
	if got := truncateSecurityValue("设备-甲乙丙", 4); got != "设备-甲" {
		t.Fatalf("unexpected rune-safe truncation: %q", got)
	}
}

func TestConcurrentRefreshReuseRevokesAllSessionsPostgreSQL(t *testing.T) {
	db := authTestPostgreSQL(t)
	if err := db.AutoMigrate(&model.User{}, &model.UserSession{}, &model.UserSessionToken{}, &model.SecurityEvent{}); err != nil {
		t.Fatalf("migrate auth test schema: %v", err)
	}
	user := model.User{Email: uuid.NewString() + "@example.test", PasswordHash: "not-used", Status: "active"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	h := Handler{DB: db, Cfg: config.Config{JWTSecret: "unit-test-user-jwt-secret"}}
	original := createTestUserSession(t, h, user.ID)
	_ = createTestUserSession(t, h, user.ID)

	start := make(chan struct{})
	statuses := make(chan int, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			body := fmt.Sprintf(`{"refresh_token":%q}`, original)
			ctx, recorder := testContext(http.MethodPost, "/api/v1/auth/refresh", body)
			h.RefreshUserSession(ctx)
			statuses <- recorder.Code
		}()
	}
	close(start)
	wg.Wait()
	close(statuses)

	counts := map[int]int{}
	for status := range statuses {
		counts[status]++
	}
	if counts[http.StatusOK] != 1 || counts[http.StatusUnauthorized] != 1 {
		t.Fatalf("expected one rotation and one reuse rejection, got statuses %#v", counts)
	}
	var activeSessions, activeTokens, events int64
	db.Model(&model.UserSession{}).Where("user_id = ? AND revoked_at IS NULL", user.ID).Count(&activeSessions)
	db.Model(&model.UserSessionToken{}).Where("user_id = ? AND revoked_at IS NULL", user.ID).Count(&activeTokens)
	db.Model(&model.SecurityEvent{}).Where("principal_id = ? AND event_type = ?", user.ID, "auth.refresh_token_reuse").Count(&events)
	if activeSessions != 0 || activeTokens != 0 || events != 1 {
		t.Fatalf("unexpected reuse outcome: active sessions=%d active tokens=%d events=%d", activeSessions, activeTokens, events)
	}
}

func TestEnabledTOTPResetKeepsCurrentDeviceUntilPendingSecretVerifiedPostgreSQL(t *testing.T) {
	db := authTestPostgreSQL(t)
	if err := db.AutoMigrate(&model.Admin{}, &model.TOTPDevice{}, &model.AuditLog{}); err != nil {
		t.Fatalf("migrate totp test schema: %v", err)
	}
	vault, err := security.NewVault("unit-test-encryption-secret-at-least-32-bytes")
	if err != nil {
		t.Fatalf("create vault: %v", err)
	}
	password := "Strong-Admin-Password-2026"
	passwordHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	admin := model.Admin{Username: "admin-" + uuid.NewString(), PasswordHash: string(passwordHash), Role: "super_admin", Status: "active"}
	if err := db.Create(&admin).Error; err != nil {
		t.Fatalf("create admin: %v", err)
	}
	oldKey, err := totp.Generate(totp.GenerateOpts{Issuer: "LinLinQi", AccountName: admin.Username, SecretSize: 32})
	if err != nil {
		t.Fatalf("generate old totp: %v", err)
	}
	oldCipher, oldNonce, _, err := vault.Encrypt(oldKey.Secret(), admin.ID[:])
	if err != nil {
		t.Fatalf("encrypt old totp: %v", err)
	}
	device := model.TOTPDevice{Realm: "admin", PrincipalID: admin.ID, SecretCipher: oldCipher, SecretNonce: oldNonce, Enabled: true}
	if err := db.Create(&device).Error; err != nil {
		t.Fatalf("create totp device: %v", err)
	}
	h := Handler{DB: db, Vault: vault}

	unauthorizedCtx, unauthorizedRecorder := testContext(http.MethodPost, "/admin/v1/security/2fa/setup", "")
	unauthorizedCtx.Set("subject", admin.ID.String())
	h.BeginAdminTOTP(unauthorizedCtx)
	if unauthorizedRecorder.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected reset without reauthentication to fail, got %d", unauthorizedRecorder.Code)
	}

	body := fmt.Sprintf(`{"password":%q}`, password)
	beginCtx, beginRecorder := testContext(http.MethodPost, "/admin/v1/security/2fa/setup", body)
	beginCtx.Set("subject", admin.ID.String())
	h.BeginAdminTOTP(beginCtx)
	if beginRecorder.Code != http.StatusOK {
		t.Fatalf("begin authenticated reset: status=%d body=%s", beginRecorder.Code, beginRecorder.Body.String())
	}
	var beginResponse struct {
		Data struct {
			Secret string `json:"secret"`
		} `json:"data"`
	}
	if err := json.Unmarshal(beginRecorder.Body.Bytes(), &beginResponse); err != nil || beginResponse.Data.Secret == "" {
		t.Fatalf("decode pending secret: %v body=%s", err, beginRecorder.Body.String())
	}
	var afterBegin model.TOTPDevice
	if err := db.First(&afterBegin, "id = ?", device.ID).Error; err != nil {
		t.Fatalf("reload totp device: %v", err)
	}
	if !afterBegin.Enabled || !bytes.Equal(afterBegin.SecretCipher, oldCipher) || len(afterBegin.PendingSecretCipher) == 0 {
		t.Fatal("enabled TOTP was replaced before the pending secret was verified")
	}

	code, err := totp.GenerateCode(beginResponse.Data.Secret, time.Now())
	if err != nil {
		t.Fatalf("generate pending totp code: %v", err)
	}
	verifyCtx, verifyRecorder := testContext(http.MethodPost, "/admin/v1/security/2fa/verify", fmt.Sprintf(`{"code":%q}`, code))
	verifyCtx.Set("subject", admin.ID.String())
	h.VerifyAdminTOTP(verifyCtx)
	if verifyRecorder.Code != http.StatusOK {
		t.Fatalf("verify pending reset: status=%d body=%s", verifyRecorder.Code, verifyRecorder.Body.String())
	}
	var afterVerify model.TOTPDevice
	if err := db.First(&afterVerify, "id = ?", device.ID).Error; err != nil {
		t.Fatalf("reload verified device: %v", err)
	}
	if !afterVerify.Enabled || len(afterVerify.PendingSecretCipher) != 0 || bytes.Equal(afterVerify.SecretCipher, oldCipher) {
		t.Fatal("pending TOTP was not atomically promoted after verification")
	}
}

func TestAdminSessionVersionRevokesJWTImmediatelyPostgreSQL(t *testing.T) {
	db := authTestPostgreSQL(t)
	if err := db.AutoMigrate(&model.Admin{}); err != nil {
		t.Fatalf("migrate admin test schema: %v", err)
	}
	admin := model.Admin{Username: "admin-" + uuid.NewString(), PasswordHash: "not-used", Role: "operator", Status: "active"}
	if err := db.Create(&admin).Error; err != nil {
		t.Fatalf("create admin: %v", err)
	}
	const secret = "versioned-admin-jwt-test-secret"
	token, err := middleware.IssueVersionedToken(admin.ID.String(), "admin", admin.Role, secret, admin.SessionVersion, time.Minute)
	if err != nil {
		t.Fatalf("issue admin jwt: %v", err)
	}
	ginTestModeOnce.Do(func() { gin.SetMode(gin.TestMode) })
	engine := gin.New()
	engine.GET("/protected", middleware.JWT(secret, "admin"), middleware.AdminSessionVersion(db), func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})
	request := func() int {
		recorder := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "/protected", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		engine.ServeHTTP(recorder, req)
		return recorder.Code
	}
	if status := request(); status != http.StatusNoContent {
		t.Fatalf("expected current version to pass, got %d", status)
	}
	if err := db.Model(&admin).UpdateColumn("session_version", gorm.Expr("session_version + 1")).Error; err != nil {
		t.Fatalf("increment session version: %v", err)
	}
	if status := request(); status != http.StatusUnauthorized {
		t.Fatalf("expected old admin jwt to be revoked immediately, got %d", status)
	}
}

func TestValidateUserPasswordHonorsBcryptByteLimit(t *testing.T) {
	for _, password := range []string{"SafeUser#2026", "八个字符安全口令#2026"} {
		if err := validateUserPassword(password); err != nil {
			t.Fatalf("valid password rejected: %q: %v", password, err)
		}
	}
	for _, password := range []string{
		"short7",
		"password-2026",
		"Safe User#2026",
		strings.Repeat("界", 25), // 75 UTF-8 bytes: bcrypt would reject it.
	} {
		if err := validateUserPassword(password); err == nil {
			t.Fatalf("unsafe or overlong password accepted: %q", password)
		}
	}
}

func TestValidUserNickname(t *testing.T) {
	for _, nickname := range []string{"林栖", "LinLinQi Operator", "数字商品 🚀"} {
		if !validUserNickname(nickname) {
			t.Fatalf("valid nickname rejected: %q", nickname)
		}
	}
	for _, nickname := range []string{"", "A", strings.Repeat("长", 81), "invalid\nname"} {
		if validUserNickname(nickname) {
			t.Fatalf("invalid nickname accepted: %q", nickname)
		}
	}
}

func createTestUserSession(t *testing.T, h Handler, userID uuid.UUID) string {
	t.Helper()
	ctx, _ := testContext(http.MethodPost, "/test/session", "")
	token, _, err := h.createUserSession(ctx, userID)
	if err != nil {
		t.Fatalf("create user session: %v", err)
	}
	return token
}

var ginTestModeOnce sync.Once

func testContext(method, target, body string) (*gin.Context, *httptest.ResponseRecorder) {
	ginTestModeOnce.Do(func() { gin.SetMode(gin.TestMode) })
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(method, target, strings.NewReader(body))
	if body != "" {
		ctx.Request.Header.Set("Content-Type", "application/json")
	}
	return ctx, recorder
}

func authTestPostgreSQL(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("LINLINQI_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set LINLINQI_TEST_DATABASE_URL to run PostgreSQL authentication integration tests")
	}
	adminDB, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("connect to PostgreSQL: %v", err)
	}
	schemaName := "linlinqi_auth_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if err := adminDB.Exec(`CREATE SCHEMA "` + schemaName + `"`).Error; err != nil {
		t.Fatalf("create test schema: %v", err)
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{NamingStrategy: schema.NamingStrategy{TablePrefix: schemaName + "."}})
	if err != nil {
		t.Fatalf("open schema-scoped PostgreSQL connection: %v", err)
	}
	t.Cleanup(func() {
		if sqlDB, dbErr := db.DB(); dbErr == nil {
			_ = sqlDB.Close()
		}
		_ = adminDB.Exec(`DROP SCHEMA "` + schemaName + `" CASCADE`).Error
		if sqlDB, dbErr := adminDB.DB(); dbErr == nil {
			_ = sqlDB.Close()
		}
	})
	return db
}
