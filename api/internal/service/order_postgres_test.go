package service

import (
	"errors"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"linlinqi/api/internal/database"
	"linlinqi/api/internal/model"
)

func orderPostgresTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("LINLINQI_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("set LINLINQI_TEST_DATABASE_URL to run PostgreSQL order integration tests")
	}
	adminDB, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("connect PostgreSQL: %v", err)
	}
	schemaName := "linlinqi_order_expiry_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if err := adminDB.Exec(`CREATE SCHEMA "` + schemaName + `"`).Error; err != nil {
		t.Fatalf("create order expiry schema: %v", err)
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
		t.Fatalf("open isolated order expiry schema: %v", err)
	}
	if err := database.Migrate(db); err != nil {
		t.Fatalf("migrate isolated order expiry schema: %v", err)
	}
	return db
}

func TestExpirePendingOrderReleasesReservedInventoryPostgreSQL(t *testing.T) {
	db := orderPostgresTestDB(t)

	productID, orderID, cardID, itemID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	old := time.Now().Add(-30 * time.Minute)
	order := model.Order{
		Base: model.Base{ID: orderID, CreatedAt: old}, OrderNo: "LLQ-EXPIRY-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12],
		Email: "expiry@example.com", Status: "pending_payment", PaymentStatus: "pending",
		Subtotal: 1000, Total: 1000, PaymentMethod: "test", ClientIP: "203.0.113.10",
	}
	if err := db.Create(&order).Error; err != nil {
		t.Fatalf("create pending order: %v", err)
	}
	if err := db.Model(&order).UpdateColumn("created_at", old).Error; err != nil {
		t.Fatalf("age pending order: %v", err)
	}
	card := model.Card{
		Base: model.Base{ID: cardID}, ProductID: productID, EncryptedContent: []byte("ciphertext"), Nonce: []byte("nonce"),
		Fingerprint: strings.Repeat("a", 64), Preview: "***", Status: "locked", OrderID: &orderID,
	}
	if err := db.Create(&card).Error; err != nil {
		t.Fatalf("create locked card: %v", err)
	}
	item := model.OrderItem{
		Base: model.Base{ID: itemID}, OrderID: orderID, ProductID: productID, ProductName: "Expiry Product",
		UnitPrice: 1000, Quantity: 1, CardCiphertext: []byte("ciphertext"), CardNonce: []byte("nonce"), CardPreview: "***",
	}
	if err := db.Create(&item).Error; err != nil {
		t.Fatalf("create reserved order item: %v", err)
	}

	count, err := ExpirePendingOrders(db, time.Now().Add(-15*time.Minute))
	if err != nil || count != 1 {
		t.Fatalf("expire pending order: count=%d err=%v", count, err)
	}
	if err := db.First(&order, "id = ?", orderID).Error; err != nil || order.Status != "expired" || order.PaymentStatus != "expired" {
		t.Fatalf("pending order was not expired: %#v err=%v", order, err)
	}
	var releasedCard model.Card
	if err := db.First(&releasedCard, "id = ?", cardID).Error; err != nil || releasedCard.Status != "available" || releasedCard.OrderID != nil {
		t.Fatalf("locked card was not released: %#v err=%v", releasedCard, err)
	}
	var clearedItem model.OrderItem
	if err := db.First(&clearedItem, "id = ?", itemID).Error; err != nil || len(clearedItem.CardCiphertext) != 0 || len(clearedItem.CardNonce) != 0 || clearedItem.CardPreview != "" {
		t.Fatalf("reserved delivery snapshot was not cleared: %#v err=%v", clearedItem, err)
	}
}

func createPendingReservation(t *testing.T, db *gorm.DB, userID *uuid.UUID, email, clientIP string, quantity int) model.Order {
	t.Helper()
	order := model.Order{
		Base: model.Base{ID: uuid.New()}, OrderNo: "LQ-QUOTA-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:20],
		UserID: userID, Email: email, ClientIP: clientIP, Status: "pending_payment", PaymentStatus: "pending",
		Subtotal: int64(quantity * 100), Total: int64(quantity * 100), PaymentMethod: "test",
	}
	if err := db.Create(&order).Error; err != nil {
		t.Fatalf("create pending quota order: %v", err)
	}
	item := model.OrderItem{
		Base: model.Base{ID: uuid.New()}, OrderID: order.ID, ProductID: uuid.New(), ProductName: "Quota Product",
		UnitPrice: 100, Quantity: quantity,
	}
	if err := db.Create(&item).Error; err != nil {
		t.Fatalf("create pending quota item: %v", err)
	}
	return order
}

func TestPendingReservationQuotaLimitsAndExpiryPostgreSQL(t *testing.T) {
	db := orderPostgresTestDB(t)
	guest := createPendingReservation(t, db, nil, "guest-limit@example.com", "203.0.113.20", 20)
	if err := db.Transaction(func(tx *gorm.DB) error {
		return enforcePendingReservationQuota(tx, nil, guest.Email, guest.ClientIP, 1)
	}); !errors.Is(err, ErrPendingOrderLimit) {
		t.Fatalf("guest quota did not reject item 21: %v", err)
	}
	if err := db.Model(&guest).Update("status", "expired").Error; err != nil {
		t.Fatalf("expire guest reservation: %v", err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		return enforcePendingReservationQuota(tx, nil, guest.Email, guest.ClientIP, 20)
	}); err != nil {
		t.Fatalf("expired guest reservation still consumed quota: %v", err)
	}

	registeredID := uuid.New()
	createPendingReservation(t, db, &registeredID, "registered-a@example.com", "203.0.113.30", 100)
	if err := db.Transaction(func(tx *gorm.DB) error {
		return enforcePendingReservationQuota(tx, &registeredID, "registered-b@example.com", "203.0.113.31", 1)
	}); !errors.Is(err, ErrPendingOrderLimit) {
		t.Fatalf("registered user quota did not reject item 101: %v", err)
	}

	otherUserID := uuid.New()
	createPendingReservation(t, db, &otherUserID, "ip-owner@example.com", "203.0.113.40", 200)
	targetUserID := uuid.New()
	if err := db.Transaction(func(tx *gorm.DB) error {
		return enforcePendingReservationQuota(tx, &targetUserID, "ip-target@example.com", "203.0.113.40", 1)
	}); !errors.Is(err, ErrPendingOrderLimit) {
		t.Fatalf("registered IP quota did not reject item 201: %v", err)
	}
}

func TestPendingReservationQuotaSerializesConcurrentCheckoutPostgreSQL(t *testing.T) {
	db := orderPostgresTestDB(t)
	email, clientIP := "quota-race@example.com", "203.0.113.50"
	createPendingReservation(t, db, nil, email, clientIP, 19)

	start := make(chan struct{})
	results := make(chan error, 2)
	var wait sync.WaitGroup
	for range 2 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			results <- db.Transaction(func(tx *gorm.DB) error {
				if err := enforcePendingReservationQuota(tx, nil, email, clientIP, 1); err != nil {
					return err
				}
				order := model.Order{
					Base: model.Base{ID: uuid.New()}, OrderNo: "LQ-RACE-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:20],
					Email: email, ClientIP: clientIP, Status: "pending_payment", PaymentStatus: "pending",
					Subtotal: 100, Total: 100, PaymentMethod: "test",
				}
				if err := tx.Create(&order).Error; err != nil {
					return err
				}
				return tx.Create(&model.OrderItem{
					Base: model.Base{ID: uuid.New()}, OrderID: order.ID, ProductID: uuid.New(), ProductName: "Race Product", UnitPrice: 100, Quantity: 1,
				}).Error
			})
		}()
	}
	close(start)
	wait.Wait()
	close(results)
	var succeeded, limited int
	for err := range results {
		switch {
		case err == nil:
			succeeded++
		case errors.Is(err, ErrPendingOrderLimit):
			limited++
		default:
			t.Fatalf("concurrent quota transaction failed unexpectedly: %v", err)
		}
	}
	if succeeded != 1 || limited != 1 {
		t.Fatalf("quota race was not serialized: succeeded=%d limited=%d", succeeded, limited)
	}
	if _, err := ExpirePendingOrders(db, time.Now().Add(time.Second)); err != nil {
		t.Fatalf("expire concurrent reservations: %v", err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		return enforcePendingReservationQuota(tx, nil, email, clientIP, 20)
	}); err != nil {
		t.Fatalf("expired concurrent reservations still consumed quota: %v", err)
	}
}

func TestSupplierInventoryReservationAllowsMultipleLinesAndHasIdempotentLifecyclePostgreSQL(t *testing.T) {
	db := orderPostgresTestDB(t)
	orderID := uuid.New()
	order := model.Order{
		Base: model.Base{ID: orderID}, OrderNo: "LQ-SUPPLIER-HOLD-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:16],
		Email: "supplier-hold@example.com", Status: "processing", PaymentStatus: "paid",
		Subtotal: 200, Total: 200, Currency: "CNY", PaymentMethod: "test",
	}
	if err := db.Create(&order).Error; err != nil {
		t.Fatalf("create supplier order: %v", err)
	}
	now := time.Now().UTC()
	firstItemID, secondItemID := uuid.New(), uuid.New()
	firstSupplierID, secondSupplierID := uuid.New(), uuid.New()
	firstSnapshot := model.SupplierProduct{
		Base: model.Base{ID: uuid.New()}, SupplierID: firstSupplierID, ProductID: uuid.New(),
		ExternalID: "remote-first", ExternalPrice: 100, ExternalStock: 3, AutoSync: true,
	}
	secondSnapshot := model.SupplierProduct{
		Base: model.Base{ID: uuid.New()}, SupplierID: secondSupplierID, ProductID: uuid.New(),
		ExternalID: "remote-second", ExternalPrice: 100, ExternalStock: 4, AutoSync: true,
	}
	if err := db.Create(&[]model.SupplierProduct{firstSnapshot, secondSnapshot}).Error; err != nil {
		t.Fatalf("create supplier stock observations: %v", err)
	}
	reservations := []model.SupplierInventoryReservation{
		{Base: model.Base{ID: uuid.New()}, OrderID: orderID, OrderItemID: firstItemID, SupplierID: firstSupplierID, SupplierProductID: firstSnapshot.ID, ProductMappingID: uuid.New(), ExternalProductID: firstSnapshot.ExternalID, Quantity: 1, Status: "reserved", ExpiresAt: now.Add(time.Hour)},
		{Base: model.Base{ID: uuid.New()}, OrderID: orderID, OrderItemID: secondItemID, SupplierID: secondSupplierID, SupplierProductID: secondSnapshot.ID, ProductMappingID: uuid.New(), ExternalProductID: secondSnapshot.ExternalID, Quantity: 2, Status: "reserved", ExpiresAt: now.Add(time.Hour)},
	}
	if err := db.Create(&reservations).Error; err != nil {
		t.Fatalf("create multiple reservations for one order: %v", err)
	}
	if err := ExtendSupplierInventoryReservationsTx(db, orderID, 6*time.Hour); err != nil {
		t.Fatalf("extend reservations: %v", err)
	}
	if err := ConsumeSupplierInventoryReservationTx(db, firstItemID); err != nil {
		t.Fatalf("consume first reservation: %v", err)
	}
	if err := ConsumeSupplierInventoryReservationTx(db, firstItemID); err != nil {
		t.Fatalf("idempotently consume first reservation: %v", err)
	}
	if err := ReleaseSupplierInventoryReservationsTx(db, orderID, "terminal test"); err != nil {
		t.Fatalf("release remaining reservations: %v", err)
	}
	if err := ReleaseSupplierInventoryReservationsTx(db, orderID, "terminal test retry"); err != nil {
		t.Fatalf("idempotently release remaining reservations: %v", err)
	}
	var stored []model.SupplierInventoryReservation
	if err := db.Where("order_id = ?", orderID).Order("order_item_id ASC").Find(&stored).Error; err != nil {
		t.Fatalf("load reservation lifecycle: %v", err)
	}
	if len(stored) != 2 {
		t.Fatalf("reservation count = %d, want 2", len(stored))
	}
	states := map[uuid.UUID]string{}
	for _, reservation := range stored {
		states[reservation.OrderItemID] = reservation.Status
		if !reservation.ExpiresAt.After(now.Add(5 * time.Hour)) {
			t.Errorf("reservation %s was not extended: %s", reservation.ID, reservation.ExpiresAt)
		}
	}
	if states[firstItemID] != "consumed" || states[secondItemID] != "released" {
		t.Fatalf("unexpected reservation states: %#v", states)
	}
	if err := db.First(&firstSnapshot, "id = ?", firstSnapshot.ID).Error; err != nil {
		t.Fatalf("reload consumed supplier observation: %v", err)
	}
	if err := db.First(&secondSnapshot, "id = ?", secondSnapshot.ID).Error; err != nil {
		t.Fatalf("reload released supplier observation: %v", err)
	}
	if firstSnapshot.ExternalStock != 2 {
		t.Fatalf("consumed observation stock = %d, want 2", firstSnapshot.ExternalStock)
	}
	if secondSnapshot.ExternalStock != 4 {
		t.Fatalf("released observation stock = %d, want unchanged 4", secondSnapshot.ExternalStock)
	}
}

func TestSupplierInventoryConsumptionIsConcurrentAndIdempotentPostgreSQL(t *testing.T) {
	db := orderPostgresTestDB(t)
	orderID, orderItemID, supplierID := uuid.New(), uuid.New(), uuid.New()
	order := model.Order{
		Base: model.Base{ID: orderID}, OrderNo: "LQ-SUPPLIER-CONSUME-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12],
		Email: "supplier-consume@example.com", Status: "processing", PaymentStatus: "paid",
		Subtotal: 100, Total: 100, Currency: "CNY", PaymentMethod: "test",
	}
	if err := db.Create(&order).Error; err != nil {
		t.Fatalf("create supplier order: %v", err)
	}
	snapshot := model.SupplierProduct{
		Base: model.Base{ID: uuid.New()}, SupplierID: supplierID, ProductID: uuid.New(),
		ExternalID: "only-upstream-unit", ExternalPrice: 100, ExternalStock: 1, AutoSync: true,
	}
	if err := db.Create(&snapshot).Error; err != nil {
		t.Fatalf("create supplier stock observation: %v", err)
	}
	reservation := model.SupplierInventoryReservation{
		Base: model.Base{ID: uuid.New()}, OrderID: orderID, OrderItemID: orderItemID,
		SupplierID: supplierID, SupplierProductID: snapshot.ID, ProductMappingID: uuid.New(),
		ExternalProductID: snapshot.ExternalID, Quantity: 1, Status: "reserved", ExpiresAt: time.Now().UTC().Add(time.Hour),
	}
	if err := db.Create(&reservation).Error; err != nil {
		t.Fatalf("create supplier reservation: %v", err)
	}

	start := make(chan struct{})
	results := make(chan error, 2)
	var wait sync.WaitGroup
	for range 2 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			results <- db.Transaction(func(tx *gorm.DB) error {
				return ConsumeSupplierInventoryReservationTx(tx, orderItemID)
			})
		}()
	}
	close(start)
	wait.Wait()
	close(results)
	for err := range results {
		if err != nil {
			t.Fatalf("concurrent supplier consumption failed: %v", err)
		}
	}
	if err := db.First(&reservation, "id = ?", reservation.ID).Error; err != nil {
		t.Fatalf("reload supplier reservation: %v", err)
	}
	if err := db.First(&snapshot, "id = ?", snapshot.ID).Error; err != nil {
		t.Fatalf("reload supplier stock observation: %v", err)
	}
	if reservation.Status != "consumed" || reservation.ConsumedAt == nil {
		t.Fatalf("reservation was not consumed exactly once: %#v", reservation)
	}
	if snapshot.ExternalStock != 0 {
		t.Fatalf("concurrent consumption left stock %d, want 0", snapshot.ExternalStock)
	}
}

func TestSupplierConsumedStockCannotResellUntilAuthoritativeObservationPostgreSQL(t *testing.T) {
	db := orderPostgresTestDB(t)
	category := model.Category{Name: "Supplier Observation", Slug: "supplier-observation-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12], Enabled: true}
	if err := db.Create(&category).Error; err != nil {
		t.Fatalf("create category: %v", err)
	}
	product := model.Product{
		CategoryID: category.ID, Name: "Supplier Observation Product", Slug: "supplier-observation-product-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12],
		Currency: "CNY", Price: 100, CostPrice: 70, DeliveryType: "auto", InventoryMode: "supplier", MinimumPurchase: 1, Status: "on_sale",
	}
	if err := db.Create(&product).Error; err != nil {
		t.Fatalf("create supplier product: %v", err)
	}
	supplierModel := model.Supplier{
		Name: "Observation Supplier", Code: "observation-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12], BaseURL: "https://supplier.invalid",
		Protocol: "standard", Status: "active", BalanceCurrency: "USD", PriceCurrency: "USD", PriceMinorUnit: 2, CurrencyMode: "auto", HealthStatus: "unknown", SyncIntervalMinutes: 15,
	}
	if err := db.Create(&supplierModel).Error; err != nil {
		t.Fatalf("create supplier: %v", err)
	}
	fxSnapshotID := uuid.New()
	mapping := model.ProductMapping{
		SupplierID: supplierModel.ID, ProductID: product.ID, ExternalProductID: "one-unit-product", ParameterMapping: []byte(`{}`),
		PriceMode: "fixed_markup", MarkupCurrency: "CNY", FixedPriceCurrency: "CNY",
		LastUpstreamPrice: 100, LastUpstreamCurrency: "USD", LastConvertedCost: 703, LastFXSnapshotID: &fxSnapshotID,
		AutoSyncPrice: true, AutoSyncStock: true,
	}
	if err := db.Create(&mapping).Error; err != nil {
		t.Fatalf("create product mapping: %v", err)
	}
	snapshot := model.SupplierProduct{
		SupplierID: supplierModel.ID, ProductID: product.ID, ExternalID: mapping.ExternalProductID,
		ExternalPrice: 100, ExternalStock: 1, AutoSync: true,
	}
	if err := db.Create(&snapshot).Error; err != nil {
		t.Fatalf("create initial supplier observation: %v", err)
	}

	firstOrder := model.Order{
		OrderNo: "LQ-FIRST-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:16], Email: "first-observation@example.com",
		Status: "processing", PaymentStatus: "paid", Subtotal: 100, Total: 100, Currency: "CNY", PaymentMethod: "test",
	}
	if err := db.Create(&firstOrder).Error; err != nil {
		t.Fatalf("create first order: %v", err)
	}
	firstItem := model.OrderItem{
		OrderID: firstOrder.ID, ProductID: product.ID, SupplierID: &supplierModel.ID, ProductMappingID: &mapping.ID,
		ExternalProductID: mapping.ExternalProductID, ParameterMapping: `{}`, ProductName: product.Name,
		UnitPrice: 100, Currency: "CNY", UpstreamUnitPrice: 100, UpstreamCurrency: "USD", FXSnapshotID: &fxSnapshotID, PlatformUnitPrice: 100, Quantity: 1,
	}
	if err := db.Create(&firstItem).Error; err != nil {
		t.Fatalf("create first order item: %v", err)
	}
	firstReservation := model.SupplierInventoryReservation{
		OrderID: firstOrder.ID, OrderItemID: firstItem.ID, SupplierID: supplierModel.ID, SupplierProductID: snapshot.ID,
		ProductMappingID: mapping.ID, ExternalProductID: mapping.ExternalProductID, Quantity: 1, Status: "reserved", ExpiresAt: time.Now().UTC().Add(time.Hour),
	}
	if err := db.Create(&firstReservation).Error; err != nil {
		t.Fatalf("create first reservation: %v", err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		return ConsumeSupplierInventoryReservationTx(tx, firstItem.ID)
	}); err != nil {
		t.Fatalf("consume first supplier unit: %v", err)
	}

	line := ResolvedLine{Product: product, PlatformUnitPrice: 100, Quote: PriceQuote{UnitPrice: 100, Quantity: 1, Subtotal: 100, Total: 100}}
	secondOrder := model.Order{
		OrderNo: "LQ-SECOND-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:16], Email: "second-observation@example.com",
		Status: "pending_payment", PaymentStatus: "pending", Subtotal: 100, Total: 100, Currency: "CNY", PaymentMethod: "test",
	}
	if err := db.Create(&secondOrder).Error; err != nil {
		t.Fatalf("create second order: %v", err)
	}
	err := db.Transaction(func(tx *gorm.DB) error {
		_, reserveErr := newOrderItemWithPricingSnapshot(tx, secondOrder.ID, line, 1, 0)
		return reserveErr
	})
	if !errors.Is(err, ErrProductUnavailable) {
		t.Fatalf("consumed stale observation was resold: %v", err)
	}

	// This update represents the next successful authoritative stock sync.
	// It replaces the previous remaining-stock observation; consumed rows from
	// the old observation must not be deducted again.
	if err := db.Model(&snapshot).Updates(map[string]any{"external_stock": 2, "updated_at": time.Now().UTC()}).Error; err != nil {
		t.Fatalf("apply authoritative supplier observation: %v", err)
	}
	thirdOrder := model.Order{
		OrderNo: "LQ-THIRD-" + strings.ReplaceAll(uuid.NewString(), "-", "")[:16], Email: "third-observation@example.com",
		Status: "pending_payment", PaymentStatus: "pending", Subtotal: 200, Total: 200, Currency: "CNY", PaymentMethod: "test",
	}
	if err := db.Create(&thirdOrder).Error; err != nil {
		t.Fatalf("create third order: %v", err)
	}
	if err := db.Transaction(func(tx *gorm.DB) error {
		item, reserveErr := newOrderItemWithPricingSnapshot(tx, thirdOrder.ID, line, 2, 0)
		if reserveErr != nil {
			return reserveErr
		}
		return tx.Create(&item).Error
	}); err != nil {
		t.Fatalf("new authoritative observation remained permanently debited: %v", err)
	}
	var newReservation model.SupplierInventoryReservation
	if err := db.Where("order_id = ?", thirdOrder.ID).First(&newReservation).Error; err != nil {
		t.Fatalf("load reservation against new observation: %v", err)
	}
	if newReservation.Status != "reserved" || newReservation.Quantity != 2 {
		t.Fatalf("unexpected new-observation reservation: %#v", newReservation)
	}
}
