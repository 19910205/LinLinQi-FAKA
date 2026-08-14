package database

import (
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"linlinqi/api/internal/model"
)

func migrationPostgresTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("LINLINQI_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set LINLINQI_TEST_DATABASE_URL to run PostgreSQL migration integration tests")
	}
	adminDB, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("connect PostgreSQL: %v", err)
	}
	schemaName := "linlinqi_migration_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if err := adminDB.Exec(`CREATE SCHEMA "` + schemaName + `"`).Error; err != nil {
		t.Fatalf("create migration test schema: %v", err)
	}
	t.Cleanup(func() {
		_ = adminDB.Exec(`DROP SCHEMA "` + schemaName + `" CASCADE`).Error
		if sqlDB, dbErr := adminDB.DB(); dbErr == nil {
			_ = sqlDB.Close()
		}
	})
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("parse PostgreSQL URL: %v", err)
	}
	query := parsed.Query()
	query.Set("search_path", schemaName)
	parsed.RawQuery = query.Encode()
	db, err := gorm.Open(postgres.Open(parsed.String()), &gorm.Config{})
	if err != nil {
		t.Fatalf("open isolated migration schema: %v", err)
	}
	if err := Migrate(db); err != nil {
		t.Fatalf("migrate isolated schema: %v", err)
	}
	return db
}

func TestMigration023UpgradesDatabasesThatAlreadyAppliedMigration003PostgreSQL(t *testing.T) {
	db := migrationPostgresTestDB(t)
	statements := []string{
		`DELETE FROM linlinqi_schema_migrations WHERE version = '20260809_023_payment_procurement_integrity'`,
		`ALTER TABLE api_credentials DROP CONSTRAINT IF EXISTS chk_api_credentials_live_owner`,
		`ALTER TABLE payment_intents DROP CONSTRAINT IF EXISTS chk_payment_intent_state`,
		`ALTER TABLE payment_intents DROP CONSTRAINT IF EXISTS chk_payment_intent_currency_chain`,
		`DROP INDEX idx_payment_intents_active_order`,
		`DROP INDEX idx_payment_channel_trade`,
		`DROP INDEX idx_procurement_order_item`,
	}
	for _, statement := range statements {
		if err := db.Exec(statement).Error; err != nil {
			t.Fatalf("prepare simulated legacy schema: %v", err)
		}
	}
	credential := model.APICredential{
		Base: model.Base{ID: uuid.New()}, OwnerType: "user", Name: "orphan legacy key", Key: "linlinqi_legacy_" + uuid.NewString(),
		SecretCipher: []byte("legacy-cipher"), SecretNonce: []byte("legacy-nonce"), Status: "active",
	}
	if err := db.Create(&credential).Error; err != nil {
		t.Fatalf("create legacy ownerless credential: %v", err)
	}
	orderID, channelID := uuid.New(), uuid.New()
	for index := range 2 {
		intent := model.PaymentIntent{
			Base: model.Base{ID: uuid.New()}, OrderID: orderID, IntentNo: "legacy-active-" + uuid.NewString(),
			ChannelID: channelID, Amount: 1000, Currency: "CNY", Status: "creating",
			ExpiresAt: time.Now().Add(time.Duration(index+1) * time.Minute),
		}
		if err := db.Create(&intent).Error; err != nil {
			t.Fatalf("create duplicate legacy active intent: %v", err)
		}
	}

	if err := Migrate(db); err != nil {
		t.Fatalf("apply migration 023 to simulated legacy schema: %v", err)
	}
	if err := db.First(&credential, "id = ?", credential.ID).Error; err != nil || credential.Status != "suspended" {
		t.Fatalf("ownerless legacy credential was not suspended: status=%q err=%v", credential.Status, err)
	}
	var active int64
	if err := db.Model(&model.PaymentIntent{}).Where("order_id = ? AND status IN ?", orderID, []string{"creating", "pending"}).Count(&active).Error; err != nil || active != 1 {
		t.Fatalf("legacy active intents were not normalized: active=%d err=%v", active, err)
	}
	var migrationCount int64
	if err := db.Model(&schemaMigration{}).Where("version = ?", "20260809_023_payment_procurement_integrity").Count(&migrationCount).Error; err != nil || migrationCount != 1 {
		t.Fatalf("migration 023 was not recorded: count=%d err=%v", migrationCount, err)
	}

	silent := db.Session(&gorm.Session{Logger: logger.Default.LogMode(logger.Silent)})
	invalidCredential := model.APICredential{
		Base: model.Base{ID: uuid.New()}, OwnerType: "user", Name: "invalid orphan", Key: "linlinqi_invalid_" + uuid.NewString(),
		SecretCipher: []byte("cipher"), SecretNonce: []byte("nonce"), Status: "pending",
	}
	if err := silent.Create(&invalidCredential).Error; err == nil {
		t.Fatal("ownerless pending credential bypassed migration 023 constraint")
	}
	invalidRefundState := model.PaymentIntent{
		Base: model.Base{ID: uuid.New()}, OrderID: uuid.New(), IntentNo: "invalid-refund-" + uuid.NewString(),
		ChannelID: channelID, Amount: 1000, Currency: "CNY", Status: "requires_refund", ExpiresAt: time.Now().Add(time.Minute),
	}
	if err := silent.Create(&invalidRefundState).Error; err == nil {
		t.Fatal("requires_refund intent without settled provider identity bypassed state constraint")
	}
	invalidCreatingState := model.PaymentIntent{
		Base: model.Base{ID: uuid.New()}, OrderID: uuid.New(), IntentNo: "invalid-creating-" + uuid.NewString(),
		ChannelID: channelID, Amount: 1000, Currency: "CNY", Status: "creating",
		ProviderTradeNo: "trade-too-early", ExpiresAt: time.Now().Add(time.Minute),
	}
	if err := silent.Create(&invalidCreatingState).Error; err == nil {
		t.Fatal("creating intent with provider trade number bypassed state constraint")
	}
	duplicateActive := model.PaymentIntent{
		Base: model.Base{ID: uuid.New()}, OrderID: orderID, IntentNo: "duplicate-active-" + uuid.NewString(),
		ChannelID: channelID, Amount: 1000, Currency: "CNY", Status: "creating", ExpiresAt: time.Now().Add(time.Minute),
	}
	if err := silent.Create(&duplicateActive).Error; err == nil {
		t.Fatal("duplicate active intent bypassed migration 023 unique index")
	}

	succeededAt := time.Now().UTC()
	firstTrade := model.PaymentIntent{
		Base: model.Base{ID: uuid.New()}, OrderID: uuid.New(), IntentNo: "trade-first-" + uuid.NewString(),
		ChannelID: channelID, Amount: 1000, Currency: "CNY", Status: "succeeded", ProviderTradeNo: "provider-duplicate", SucceededAt: &succeededAt,
	}
	if err := db.Create(&firstTrade).Error; err != nil {
		t.Fatalf("create first provider trade: %v", err)
	}
	secondTrade := firstTrade
	secondTrade.ID, secondTrade.OrderID, secondTrade.IntentNo = uuid.New(), uuid.New(), "trade-second-"+uuid.NewString()
	if err := silent.Create(&secondTrade).Error; err == nil {
		t.Fatal("duplicate provider trade bypassed migration 023 unique index")
	}

	orderItemID := uuid.New()
	firstProcurement := model.ProcurementOrder{
		Base: model.Base{ID: uuid.New()}, ProcurementNo: "LQP-" + uuid.NewString(), SupplierID: uuid.New(), OrderID: uuid.New(), OrderItemID: orderItemID,
		ExternalProductID: "external-product", Quantity: 1, CostAmount: 100, Status: "creating",
	}
	if err := db.Create(&firstProcurement).Error; err != nil {
		t.Fatalf("create first procurement order: %v", err)
	}
	secondProcurement := firstProcurement
	secondProcurement.ID, secondProcurement.ProcurementNo = uuid.New(), "LQP-"+uuid.NewString()
	if err := silent.Create(&secondProcurement).Error; err == nil {
		t.Fatal("duplicate procurement order item bypassed migration 023 unique index")
	}
}
