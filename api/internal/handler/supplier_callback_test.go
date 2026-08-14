package handler

import (
	"bytes"
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/schema"

	"linlinqi/api/internal/config"
	"linlinqi/api/internal/database"
	"linlinqi/api/internal/model"
	"linlinqi/api/internal/security"
	"linlinqi/api/internal/service"
	"linlinqi/api/internal/supply"
)

func signSupplierCallback(secret string, timestamp int64, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(fmt.Sprintf("%d.", timestamp)))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func TestVerifySupplierCallbackSignature(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	body := []byte(`{"event_id":"evt-1"}`)
	secret := "supplier-callback-unit-secret"
	timestamp := now.Unix()
	signature := signSupplierCallback(secret, timestamp, body)
	if err := verifySupplierCallbackSignature(fmt.Sprintf("%d", timestamp), signature, secret, body, now); err != nil {
		t.Fatalf("valid signature rejected: %v", err)
	}
	if err := verifySupplierCallbackSignature(fmt.Sprintf("%d", timestamp), signature, secret, append(body, ' '), now); err == nil {
		t.Fatal("tampered callback accepted")
	}
	stale := now.Add(-6 * time.Minute).Unix()
	if err := verifySupplierCallbackSignature(fmt.Sprintf("%d", stale), signSupplierCallback(secret, stale, body), secret, body, now); err == nil {
		t.Fatal("stale callback accepted")
	}
}

func TestDecodeSupplierCallbackStrictContract(t *testing.T) {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	valid := []byte(fmt.Sprintf(`{"event_id":"evt-1","event":"order.delivered","occurred_at":%q,"data":{"client_order_no":"LQP1","external_order_no":"LQ1","status":"delivered","deliveries":["CARD-1"],"cost":100}}`, now))
	payload, err := decodeSupplierCallback(valid)
	if err != nil || payload.Data.ClientOrderNo != "LQP1" || len(payload.Data.Deliveries) != 1 {
		t.Fatalf("valid callback rejected: %#v %v", payload, err)
	}
	for name, body := range map[string][]byte{
		"unknown field": []byte(fmt.Sprintf(`{"event_id":"evt-1","event":"order.delivered","occurred_at":%q,"unexpected":true,"data":{"client_order_no":"LQP1","external_order_no":"LQ1","status":"delivered","deliveries":["CARD-1"],"cost":100}}`, now)),
		"pending":       []byte(fmt.Sprintf(`{"event_id":"evt-1","event":"order.delivered","occurred_at":%q,"data":{"client_order_no":"LQP1","external_order_no":"LQ1","status":"pending","deliveries":["CARD-1"],"cost":100}}`, now)),
		"too many":      []byte(fmt.Sprintf(`{"event_id":"evt-1","event":"order.delivered","occurred_at":%q,"data":{"client_order_no":"LQP1","external_order_no":"LQ1","status":"delivered","deliveries":[%s],"cost":100}}`, now, strings.TrimSuffix(strings.Repeat(`"CARD",`, 21), ","))),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := decodeSupplierCallback(body); err == nil {
				t.Fatal("invalid callback accepted")
			}
		})
	}
}

func TestDecodeAndVerifyDujiaoCallback(t *testing.T) {
	gin.SetMode(gin.TestMode)
	now := time.Now().UTC().Truncate(time.Second)
	body := []byte(fmt.Sprintf(`{"event":"order.status_changed","order_id":100,"order_no":"DJ-100","downstream_order_no":"LQP-100","status":"delivered","fulfillment":{"type":"auto","status":"delivered","payload":"CARD-A\nCARD-B","delivery_data":{},"delivered_at":%q},"timestamp":%d}`, now.Format(time.RFC3339), now.Unix()))
	payload, delivery, err := decodeDujiaoCallback(body)
	if err != nil || !delivery || payload.Data.ExternalOrderNo != "100" || len(payload.Data.Deliveries) != 2 {
		t.Fatalf("valid Dujiao callback rejected: %#v delivery=%v err=%v", payload, delivery, err)
	}
	secret := "dujiao-callback-secret"
	digest := md5.Sum(body)
	message := http.MethodPost + "\n/api/v1/supplier-callbacks/node\n" + strconv.FormatInt(now.Unix(), 10) + "\n" + hex.EncodeToString(digest[:])
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(message))
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodPost, "/api/v1/supplier-callbacks/node", bytes.NewReader(body))
	context.Request.Header.Set("Dujiao-Next-Api-Key", "key-1")
	context.Request.Header.Set("Dujiao-Next-Timestamp", strconv.FormatInt(now.Unix(), 10))
	context.Request.Header.Set("Dujiao-Next-Signature", hex.EncodeToString(mac.Sum(nil)))
	if err := verifyDujiaoCallbackSignature(context, secret, body, payload.OccurredAt, now); err != nil {
		t.Fatalf("valid Dujiao signature rejected: %v", err)
	}
	if err := verifyDujiaoCallbackSignature(context, secret, append(body, ' '), payload.OccurredAt, now); err == nil {
		t.Fatal("tampered Dujiao callback accepted")
	}
}

func TestDecodeDujiaoCallbackRecordsNonDeliveryWithoutSecrets(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	body := []byte(fmt.Sprintf(`{"event":"order.status_changed","order_id":"101","order_no":"DJ-101","downstream_order_no":"LQP-101","status":"paid","fulfillment":{"type":"auto","status":"pending","payload":"","delivery_data":{},"delivered_at":null},"timestamp":%d}`, now.Unix()))
	payload, delivery, err := decodeDujiaoCallback(body)
	if err != nil || delivery || payload.Data.Status != "processing" || len(payload.Data.Deliveries) != 0 {
		t.Fatalf("non-delivery status mismatch: %#v delivery=%v err=%v", payload, delivery, err)
	}
}

func TestCallbackEventIDIsSupplierScopedAndBounded(t *testing.T) {
	left := callbackEventID(uuid.New(), "upstream-event")
	right := callbackEventID(uuid.New(), "upstream-event")
	if left == right || len(left) > 100 || !strings.HasPrefix(left, "supplier:") {
		t.Fatalf("invalid durable callback event IDs: %q %q", left, right)
	}
}

func TestSupplierCallbackDurableInboxPostgreSQL(t *testing.T) {
	dsn := os.Getenv("LINLINQI_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set LINLINQI_TEST_DATABASE_URL to run PostgreSQL supplier callback integration test")
	}
	adminDB, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("connect PostgreSQL: %v", err)
	}
	schemaName := "linlinqi_supplier_callback_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if err := adminDB.Exec(`CREATE SCHEMA "` + schemaName + `"`).Error; err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() {
		_ = adminDB.Exec(`DROP SCHEMA "` + schemaName + `" CASCADE`).Error
		if sqlDB, dbErr := adminDB.DB(); dbErr == nil {
			_ = sqlDB.Close()
		}
	})
	parsedDSN, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse DSN: %v", err)
	}
	query := parsedDSN.Query()
	query.Set("search_path", schemaName)
	parsedDSN.RawQuery = query.Encode()
	db, err := gorm.Open(postgres.Open(parsedDSN.String()), &gorm.Config{NamingStrategy: schema.NamingStrategy{}})
	if err != nil {
		t.Fatalf("open isolated schema: %v", err)
	}
	if err := database.Migrate(db); err != nil {
		t.Fatalf("migrate isolated schema: %v", err)
	}
	vault, err := security.NewVault("supplier-callback-integration-encryption-key")
	if err != nil {
		t.Fatalf("create vault: %v", err)
	}
	supplierSecret := "supplier-callback-integration-secret"
	supplierModel := model.Supplier{Base: model.Base{ID: uuid.New()}, Name: "Callback Supplier", Code: "callback-supplier", BaseURL: "https://supplier.example.com", Protocol: "linlinqi-standard", Status: "active"}
	supplierModel.APIKeyCipher, supplierModel.APIKeyNonce, _, err = vault.Encrypt("supplier-integration-key", append(supplierModel.ID[:], []byte("api-key")...))
	if err != nil {
		t.Fatalf("encrypt supplier key: %v", err)
	}
	supplierModel.APISecretCipher, supplierModel.APISecretNonce, _, err = vault.Encrypt(supplierSecret, append(supplierModel.ID[:], []byte("api-secret")...))
	if err != nil {
		t.Fatalf("encrypt supplier secret: %v", err)
	}
	if err := db.Create(&supplierModel).Error; err != nil {
		t.Fatalf("create supplier: %v", err)
	}
	category := model.Category{Name: "Callback", Slug: "callback", Enabled: true}
	if err := db.Create(&category).Error; err != nil {
		t.Fatalf("create category: %v", err)
	}
	product := model.Product{CategoryID: category.ID, Name: "Callback Product", Slug: "callback-product", Price: 100, CostPrice: 50, DeliveryType: "auto", InventoryMode: "supplier", Status: "on_sale"}
	if err := db.Create(&product).Error; err != nil {
		t.Fatalf("create product: %v", err)
	}
	now := time.Now().UTC().Truncate(time.Second)
	order := model.Order{Base: model.Base{ID: uuid.New()}, OrderNo: "LQ-CALLBACK-1", Email: "buyer@example.com", Status: "processing", PaymentStatus: "paid", Subtotal: 100, Total: 100, PaymentMethod: "supplier_balance", PaidAt: &now, Adjustments: []byte(`[]`)}
	if err := db.Create(&order).Error; err != nil {
		t.Fatalf("create order: %v", err)
	}
	item := model.OrderItem{Base: model.Base{ID: uuid.New()}, OrderID: order.ID, ProductID: product.ID, ProductName: product.Name, UnitPrice: 100, PlatformUnitPrice: 100, Quantity: 1}
	if err := db.Create(&item).Error; err != nil {
		t.Fatalf("create order item: %v", err)
	}
	procurement := model.ProcurementOrder{Base: model.Base{ID: uuid.New()}, ProcurementNo: "LQP-CALLBACK-1", SupplierID: supplierModel.ID, OrderID: order.ID, OrderItemID: item.ID, ExternalProductID: "remote-product", Quantity: 1, Status: "processing"}
	procurement.CallbackSecretCipher, procurement.CallbackSecretNonce, err = service.EncryptProcurementCallbackSecret(vault, procurement.ID, supplierSecret)
	if err != nil {
		t.Fatalf("snapshot callback secret: %v", err)
	}
	if err := db.Create(&procurement).Error; err != nil {
		t.Fatalf("create procurement: %v", err)
	}
	rotatedCipher, rotatedNonce, _, err := vault.Encrypt("rotated-supplier-secret-value", append(supplierModel.ID[:], []byte("api-secret")...))
	if err != nil {
		t.Fatalf("encrypt rotated supplier secret: %v", err)
	}
	if err := db.Model(&supplierModel).Updates(map[string]any{"api_secret_cipher": rotatedCipher, "api_secret_nonce": rotatedNonce}).Error; err != nil {
		t.Fatalf("rotate supplier secret: %v", err)
	}
	payload := []byte(fmt.Sprintf(`{"event_id":"evt-delivered-1","event":"order.delivered","occurred_at":%q,"data":{"client_order_no":%q,"external_order_no":"UPSTREAM-1","status":"delivered","deliveries":["CARD-SECRET-1"],"cost":75}}`, now.Format(time.RFC3339Nano), procurement.ProcurementNo))
	h := Handler{DB: db, Vault: vault, Cfg: config.Config{Env: "test", RedisAddr: "127.0.0.1:1"}}
	invoke := func(body []byte, signature string) *httptest.ResponseRecorder {
		recorder := httptest.NewRecorder()
		context, _ := gin.CreateTestContext(recorder)
		context.Params = gin.Params{{Key: "supplier", Value: supplierModel.ID.String()}}
		context.Request = httptest.NewRequest("POST", supplierCallbackEndpoint(supplierModel.ID.String()), bytes.NewReader(body))
		context.Request.Header.Set("Content-Type", "application/json")
		context.Request.Header.Set("X-LinLinQi-Timestamp", fmt.Sprintf("%d", now.Unix()))
		context.Request.Header.Set("X-LinLinQi-Signature", signature)
		h.SupplierCallback(context)
		return recorder
	}
	signature := signSupplierCallback(supplierSecret, now.Unix(), payload)
	if recorder := invoke(payload, signature); recorder.Code != 200 {
		t.Fatalf("callback response %d: %s", recorder.Code, recorder.Body.String())
	}
	if recorder := invoke(payload, signature); recorder.Code != 200 || !strings.Contains(recorder.Body.String(), `"replayed":true`) {
		t.Fatalf("callback replay was not idempotent: %d %s", recorder.Code, recorder.Body.String())
	}
	if recorder := invoke(append(payload, ' '), signature); recorder.Code != 401 {
		t.Fatalf("tampered callback status = %d, want 401", recorder.Code)
	}
	var events []model.WebhookEvent
	if err := db.Find(&events).Error; err != nil || len(events) != 1 {
		t.Fatalf("durable event count = %d, err = %v", len(events), err)
	}
	event := events[0]
	if event.Payload != `{}` || strings.Contains(event.Payload, "CARD-SECRET-1") || len(event.PayloadCipher) == 0 || len(event.PayloadNonce) == 0 || event.Status != "queued" {
		t.Fatalf("callback payload was not safely queued: %#v", event)
	}
	plaintext, err := vault.Decrypt(event.PayloadCipher, event.PayloadNonce, event.ID[:])
	if err != nil {
		t.Fatalf("decrypt callback event: %v", err)
	}
	var result supply.OrderResult
	if err := json.Unmarshal([]byte(plaintext), &result); err != nil || result.ExternalOrderNo != "UPSTREAM-1" || len(result.Deliveries) != 1 {
		t.Fatalf("stored callback contract mismatch: %#v %v", result, err)
	}
}
