package handler

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"linlinqi/api/internal/database"
	"linlinqi/api/internal/model"
	"linlinqi/api/internal/security"
)

func TestNormalizeAPICredentialName(t *testing.T) {
	name, ok := normalizeAPICredentialName("  ERP   生产环境  ")
	if !ok || name != "ERP 生产环境" {
		t.Fatalf("valid credential name was not normalized: %q, %t", name, ok)
	}
	for label, value := range map[string]string{
		"empty":    " ",
		"one rune": "A",
		"control":  "ERP\n生产",
		"too long": strings.Repeat("凭", 101),
	} {
		if _, valid := normalizeAPICredentialName(value); valid {
			t.Fatalf("%s credential name was accepted", label)
		}
	}
}

func TestAPICredentialLifecycleIsConcurrentAndTerminalPostgreSQL(t *testing.T) {
	dsn := os.Getenv("LINLINQI_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set LINLINQI_TEST_DATABASE_URL to run PostgreSQL API credential integration test")
	}
	adminDB, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("connect to PostgreSQL: %v", err)
	}
	schemaName := "linlinqi_api_credential_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if err := adminDB.Exec(`CREATE SCHEMA "` + schemaName + `"`).Error; err != nil {
		t.Fatalf("create API credential test schema: %v", err)
	}
	t.Cleanup(func() {
		_ = adminDB.Exec(`DROP SCHEMA "` + schemaName + `" CASCADE`).Error
		if sqlDB, dbErr := adminDB.DB(); dbErr == nil {
			_ = sqlDB.Close()
		}
	})
	parsedDSN, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse test database URL: %v", err)
	}
	query := parsedDSN.Query()
	query.Set("search_path", schemaName)
	parsedDSN.RawQuery = query.Encode()
	db, err := gorm.Open(postgres.Open(parsedDSN.String()), &gorm.Config{})
	if err != nil {
		t.Fatalf("open isolated API credential schema: %v", err)
	}
	if err := database.Migrate(db); err != nil {
		t.Fatalf("migrate API credential schema: %v", err)
	}
	vault, err := security.NewVault("api-credential-integration-encryption-key-2026")
	if err != nil {
		t.Fatalf("create vault: %v", err)
	}
	user := model.User{Email: "credential-test@example.com", PasswordHash: "not-used", Nickname: "凭证测试", Status: "active"}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create API credential test user: %v", err)
	}
	h := Handler{DB: db, Vault: vault}

	statuses := make(chan int, maxUserAPICredentials+2)
	var workers sync.WaitGroup
	for index := 0; index < maxUserAPICredentials+2; index++ {
		workers.Add(1)
		go func(index int) {
			defer workers.Done()
			body, _ := json.Marshal(apiCredentialRequest{Name: fmt.Sprintf("系统接入-%02d", index)})
			context, recorder := testContext(http.MethodPost, "/api/v1/me/api-credentials", string(body))
			context.Set("subject", user.ID.String())
			h.CreateMyAPICredential(context)
			statuses <- recorder.Code
		}(index)
	}
	workers.Wait()
	close(statuses)
	created, limited := 0, 0
	for status := range statuses {
		switch status {
		case http.StatusCreated:
			created++
		case http.StatusConflict:
			limited++
		default:
			t.Fatalf("unexpected concurrent create status %d", status)
		}
	}
	if created != maxUserAPICredentials || limited != 2 {
		t.Fatalf("credential limit was not serialized: created=%d limited=%d", created, limited)
	}

	var credential model.APICredential
	if err := db.Where("owner_type = ? AND owner_id = ?", "user", user.ID).Order("created_at").First(&credential).Error; err != nil {
		t.Fatalf("load created credential: %v", err)
	}
	originalCipher := append([]byte(nil), credential.SecretCipher...)
	originalNonce := append([]byte(nil), credential.SecretNonce...)

	foreignContext, foreignRecorder := testContext(http.MethodDelete, "/api/v1/me/api-credentials/"+credential.ID.String(), "")
	foreignContext.Set("subject", uuid.NewString())
	foreignContext.Params = gin.Params{{Key: "id", Value: credential.ID.String()}}
	h.RevokeMyAPICredential(foreignContext)
	if foreignRecorder.Code != http.StatusNotFound {
		t.Fatalf("another user could address the credential: %d %s", foreignRecorder.Code, foreignRecorder.Body.String())
	}

	revokeContext, revokeRecorder := testContext(http.MethodDelete, "/api/v1/me/api-credentials/"+credential.ID.String(), "")
	revokeContext.Set("subject", user.ID.String())
	revokeContext.Params = gin.Params{{Key: "id", Value: credential.ID.String()}}
	h.RevokeMyAPICredential(revokeContext)
	if revokeRecorder.Code != http.StatusOK {
		t.Fatalf("revoke credential failed: %d %s", revokeRecorder.Code, revokeRecorder.Body.String())
	}
	if err := db.First(&credential, "id = ?", credential.ID).Error; err != nil {
		t.Fatalf("reload revoked credential: %v", err)
	}
	if credential.Status != "revoked" || credential.RevokedAt == nil {
		t.Fatalf("credential was not terminally revoked: %#v", credential)
	}
	if bytes.Equal(originalCipher, credential.SecretCipher) || bytes.Equal(originalNonce, credential.SecretNonce) {
		t.Fatal("revocation retained recoverable credential encryption material")
	}
	if _, err := vault.Decrypt(credential.SecretCipher, credential.SecretNonce, credential.ID[:]); err == nil {
		t.Fatal("revoked credential secret remained decryptable")
	}

	replacementBody, _ := json.Marshal(apiCredentialRequest{Name: "吊销后的替代接入"})
	replacementContext, replacementRecorder := testContext(http.MethodPost, "/api/v1/me/api-credentials", string(replacementBody))
	replacementContext.Set("subject", user.ID.String())
	h.CreateMyAPICredential(replacementContext)
	if replacementRecorder.Code != http.StatusCreated {
		t.Fatalf("revoked slot was not reusable: %d %s", replacementRecorder.Code, replacementRecorder.Body.String())
	}

	adminContext, adminRecorder := testContext(http.MethodPatch, "/admin/v1/api-credentials/"+credential.ID.String(), `{"status":"active"}`)
	adminContext.Set("subject", uuid.NewString())
	adminContext.Params = gin.Params{{Key: "id", Value: credential.ID.String()}}
	adminContext.Request.Header.Set("X-Change-Reason", "误操作恢复测试")
	h.UpdateAPICredential(adminContext)
	if adminRecorder.Code != http.StatusConflict {
		t.Fatalf("admin restored a revoked credential: %d %s", adminRecorder.Code, adminRecorder.Body.String())
	}
	if err := db.First(&credential, "id = ?", credential.ID).Error; err != nil || credential.Status != "revoked" {
		t.Fatalf("revoked credential terminal state changed: status=%s err=%v", credential.Status, err)
	}
}
