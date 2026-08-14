package handler

import (
	"crypto/sha256"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"linlinqi/api/internal/config"
	"linlinqi/api/internal/database"
	"linlinqi/api/internal/model"
	"linlinqi/api/internal/security"
)

func TestAdminManualOrderTransitionsCannotForgeMoneyMovement(t *testing.T) {
	for _, transition := range [][2]string{
		{"pending", "paid"},
		{"paid", "refunding"},
		{"delivered", "refunding"},
		{"refunding", "refunded"},
		{"processing", "delivered"},
	} {
		if validAdminManualOrderTransition(transition[0], transition[1]) {
			t.Fatalf("manual transition unexpectedly allowed %s -> %s", transition[0], transition[1])
		}
	}
	for _, transition := range [][2]string{
		{"pending_payment", "cancelled"},
		{"risk_review", "pending"},
		{"failed", "processing"},
		{"delivered", "completed"},
	} {
		if !validAdminManualOrderTransition(transition[0], transition[1]) {
			t.Fatalf("legitimate manual transition rejected %s -> %s", transition[0], transition[1])
		}
	}
}

func TestAdminOrderJSONContainsNoDeliveryOrLookupSecrets(t *testing.T) {
	order := model.Order{
		Base:              model.Base{ID: uuid.New()},
		OrderNo:           "LLQ-TEST",
		LookupTokenHash:   "lookup-hash",
		LookupTokenCipher: []byte("lookup-cipher"),
		LookupTokenNonce:  []byte("lookup-nonce"),
		Items: []model.OrderItem{{
			Base:           model.Base{ID: uuid.New()},
			ProductName:    "商品",
			CardCiphertext: []byte("card-cipher"),
			CardNonce:      []byte("card-nonce"),
			CardPreview:    "secret-preview",
		}},
	}
	payload, err := json.Marshal(order)
	if err != nil {
		t.Fatalf("marshal admin order: %v", err)
	}
	body := string(payload)
	for _, forbidden := range []string{"lookup-hash", "lookup-cipher", "lookup-nonce", "card-cipher", "card-nonce", "secret-preview"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("admin order response leaked %q: %s", forbidden, body)
		}
	}
}

func TestAdminManualOrderRequestValidation(t *testing.T) {
	productID := uuid.New()
	variantID := uuid.New()
	valid := adminManualOrderRequest{
		ProductID: productID.String(), VariantID: variantID.String(), Quantity: 2,
		Email: "  Customer@Example.com ", PaymentReference: " BANK-20260809-0001 ",
	}
	parsedProduct, parsedVariant, err := valid.normalizeAndValidate()
	if err != nil || *parsedProduct != productID || parsedVariant == nil || *parsedVariant != variantID {
		t.Fatalf("valid manual order rejected: product=%v variant=%v err=%v", parsedProduct, parsedVariant, err)
	}
	if valid.Email != "customer@example.com" || valid.PaymentReference != "BANK-20260809-0001" {
		t.Fatalf("manual order fields were not normalized: %#v", valid)
	}
	for name, request := range map[string]adminManualOrderRequest{
		"display email": {ProductID: productID.String(), Quantity: 1, Email: "Customer <customer@example.com>", PaymentReference: "PAY-0001"},
		"bad product":   {ProductID: "bad", Quantity: 1, Email: "customer@example.com", PaymentReference: "PAY-0001"},
		"too many":      {ProductID: productID.String(), Quantity: 21, Email: "customer@example.com", PaymentReference: "PAY-0001"},
		"control ref":   {ProductID: productID.String(), Quantity: 1, Email: "customer@example.com", PaymentReference: "PAY\n0001"},
	} {
		if _, _, err := request.normalizeAndValidate(); err == nil {
			t.Fatalf("%s request was accepted", name)
		}
	}
}

func TestAdminManualOrderReplayMustMatchOriginalPayload(t *testing.T) {
	productID, otherProductID, variantID := uuid.New(), uuid.New(), uuid.New()
	request := adminManualOrderRequest{Email: "buyer@example.com", Quantity: 2}
	order := model.Order{Email: request.Email, Items: []model.OrderItem{
		{ProductID: productID, VariantID: &variantID, Quantity: 1},
		{ProductID: productID, VariantID: &variantID, Quantity: 1},
	}}
	if !manualOrderMatches(order, request, productID, &variantID) {
		t.Fatal("matching idempotent replay was rejected")
	}
	if manualOrderMatches(order, request, otherProductID, &variantID) {
		t.Fatal("conflicting product reused an existing payment reference")
	}
	request.Email = "other@example.com"
	if manualOrderMatches(order, request, productID, &variantID) {
		t.Fatal("conflicting customer reused an existing payment reference")
	}
}

func TestAdminManualOrderDTOContainsNoDeliverySecret(t *testing.T) {
	order := model.Order{
		Base: model.Base{ID: uuid.New()}, OrderNo: "LQ-MANUAL-1", Email: "buyer@example.com",
		LookupToken: "lookup-plain", LookupTokenCipher: []byte("lookup-cipher"),
		Items: []model.OrderItem{{
			ProductID: uuid.New(), ProductName: "数字商品", Quantity: 1,
			CardContent: "card-plain", CardCiphertext: []byte("card-cipher"), CardPreview: "card-preview",
		}},
	}
	payload, err := json.Marshal(toAdminManualOrderDTO(order))
	if err != nil {
		t.Fatalf("marshal manual order DTO: %v", err)
	}
	body := string(payload)
	for _, forbidden := range []string{"lookup-plain", "lookup-cipher", "card-plain", "card-cipher", "card-preview"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("manual order DTO leaked %q: %s", forbidden, body)
		}
	}
}

func TestAdminManualOrderIsIdempotentPostgreSQL(t *testing.T) {
	dsn := os.Getenv("LINLINQI_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set LINLINQI_TEST_DATABASE_URL to run PostgreSQL manual-order integration test")
	}
	adminDB, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("connect to PostgreSQL: %v", err)
	}
	schemaName := "linlinqi_manual_order_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if err := adminDB.Exec(`CREATE SCHEMA "` + schemaName + `"`).Error; err != nil {
		t.Fatalf("create manual-order test schema: %v", err)
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
		t.Fatalf("open isolated manual-order schema: %v", err)
	}
	if err := database.Migrate(db); err != nil {
		t.Fatalf("migrate manual-order schema: %v", err)
	}
	vault, err := security.NewVault("manual-order-integration-encryption-key-2026")
	if err != nil {
		t.Fatalf("create vault: %v", err)
	}
	category := model.Category{Name: "测试分类", Slug: "manual-test-" + uuid.NewString(), Enabled: true}
	if err := db.Create(&category).Error; err != nil {
		t.Fatalf("create category: %v", err)
	}
	product := model.Product{
		CategoryID: category.ID, Name: "手工订单测试商品", Slug: "manual-product-" + uuid.NewString(),
		Price: 1234, DeliveryType: "auto", InventoryMode: "local", Status: "on_sale",
	}
	if err := db.Create(&product).Error; err != nil {
		t.Fatalf("create product: %v", err)
	}
	cardContent := "MANUAL-CARD-SECRET"
	ciphertext, nonce, fingerprint, err := vault.Encrypt(cardContent, product.ID[:])
	if err != nil {
		t.Fatalf("encrypt card: %v", err)
	}
	if fingerprint == "" {
		digest := sha256.Sum256([]byte(cardContent))
		fingerprint = string(digest[:])
	}
	card := model.Card{ProductID: product.ID, EncryptedContent: ciphertext, Nonce: nonce, Fingerprint: fingerprint, Preview: "MAN***CRET", Status: "available"}
	if err := db.Create(&card).Error; err != nil {
		t.Fatalf("create card: %v", err)
	}
	redisAddr := os.Getenv("REDIS_ADDR")
	if redisAddr == "" {
		redisAddr = "127.0.0.1:1"
	}
	h := Handler{DB: db, Vault: vault, Cfg: config.Config{Env: "test", RedisAddr: redisAddr, UserAppURL: "http://localhost:5173"}}
	requestBody, _ := json.Marshal(adminManualOrderRequest{
		ProductID: product.ID.String(), Quantity: 1, Email: "manual@example.com", PaymentReference: "BANK-MANUAL-IDEMPOTENT-1",
	})
	type manualOutcome struct {
		Order       adminManualOrderDTO `json:"order"`
		LookupToken string              `json:"lookup_token"`
		Replayed    bool                `json:"replayed"`
		Status      int
		Body        string
		Err         error
	}
	request := func() manualOutcome {
		ctx, recorder := testContext(http.MethodPost, "/admin/v1/orders/manual", string(requestBody))
		ctx.Set("subject", uuid.NewString())
		ctx.Request.Header.Set("X-Change-Reason", "财务测试流水已核验")
		h.CreateAdminManualOrder(ctx)
		var envelope struct {
			Data manualOutcome `json:"data"`
		}
		decodeErr := json.Unmarshal(recorder.Body.Bytes(), &envelope)
		envelope.Data.Status = recorder.Code
		envelope.Data.Body = recorder.Body.String()
		envelope.Data.Err = decodeErr
		return envelope.Data
	}
	start := make(chan struct{})
	outcomes := make(chan manualOutcome, 2)
	var wait sync.WaitGroup
	for range 2 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			outcomes <- request()
		}()
	}
	close(start)
	wait.Wait()
	close(outcomes)
	results := make([]manualOutcome, 0, 2)
	for outcome := range outcomes {
		if outcome.Err != nil || (outcome.Status != http.StatusCreated && outcome.Status != http.StatusOK) {
			t.Fatalf("manual order request failed: status=%d err=%v body=%s", outcome.Status, outcome.Err, outcome.Body)
		}
		results = append(results, outcome)
	}
	var created, replay manualOutcome
	for _, outcome := range results {
		if outcome.Replayed {
			replay = outcome
		} else {
			created = outcome
		}
	}
	if created.Status != http.StatusCreated || created.Order.ID == uuid.Nil || created.LookupToken == "" || created.Order.Currency != product.Currency {
		t.Fatalf("unexpected created manual order response: %#v", created)
	}
	if len(created.Order.Items) != 1 || created.Order.Items[0].Currency != product.Currency {
		t.Fatalf("manual order item currency missing: %#v", created.Order.Items)
	}
	if replay.Status != http.StatusOK || replay.Order.ID != created.Order.ID || replay.LookupToken != created.LookupToken {
		t.Fatalf("concurrent manual order retry was not idempotent: created=%#v replay=%#v", created, replay)
	}
	for name, target := range map[string]any{
		"orders": &model.Order{}, "payment intents": &model.PaymentIntent{},
		"payment transactions": &model.PaymentTransaction{},
	} {
		var count int64
		if err := db.Model(target).Count(&count).Error; err != nil || count != 1 {
			t.Fatalf("expected one %s record, count=%d err=%v", name, count, err)
		}
	}
	var storedTransaction model.PaymentTransaction
	if err := db.First(&storedTransaction).Error; err != nil || storedTransaction.Currency != product.Currency {
		t.Fatalf("manual payment transaction currency missing: %#v err=%v", storedTransaction, err)
	}
	var storedCard model.Card
	if err := db.First(&storedCard, "id = ?", card.ID).Error; err != nil || storedCard.Status != "sold" || storedCard.OrderID == nil || *storedCard.OrderID != created.Order.ID {
		t.Fatalf("manual order did not atomically allocate inventory: %#v err=%v", storedCard, err)
	}
}
