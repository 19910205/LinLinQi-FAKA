package service_test

import (
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"linlinqi/api/internal/database"
	"linlinqi/api/internal/model"
	"linlinqi/api/internal/service"
)

func TestPostgresMembershipLifecycle(t *testing.T) {
	db := membershipPostgresTestDB(t)
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatalf("begin transaction: %v", tx.Error)
	}
	defer tx.Rollback()

	now := time.Now().UTC().Truncate(time.Second)
	suffix := uuid.NewString()
	user := model.User{Email: "membership-" + suffix + "@example.test", PasswordHash: "test", Status: "active"}
	if err := tx.Create(&user).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}
	admin := model.Admin{Username: "membership-" + suffix, PasswordHash: "test", Name: "Membership Test", Role: "operator", Status: "active"}
	if err := tx.Create(&admin).Error; err != nil {
		t.Fatalf("create admin: %v", err)
	}
	base := model.MemberLevel{Code: "base-" + suffix, Name: "Base", MinimumSpend: 0, DiscountBasisPoint: 100, Priority: 10, Enabled: true}
	gold := model.MemberLevel{Code: "gold-" + suffix, Name: "Gold", MinimumSpend: 10_000, DiscountBasisPoint: 800, Priority: 20, Enabled: true}
	if err := tx.Create(&base).Error; err != nil {
		t.Fatalf("create base level: %v", err)
	}
	if err := tx.Create(&gold).Error; err != nil {
		t.Fatalf("create gold level: %v", err)
	}
	order := model.Order{
		OrderNo: "MEM-" + suffix, UserID: &user.ID, Email: user.Email,
		Status: "delivered", PaymentStatus: "paid", Subtotal: 15_000, Total: 15_000,
	}
	if err := tx.Create(&order).Error; err != nil {
		t.Fatalf("create settled order: %v", err)
	}

	automatic, changed, err := service.ReconcileUserMembershipTx(tx, user.ID, now)
	if err != nil || !changed || automatic == nil || automatic.MemberLevelID != gold.ID || automatic.Source != "automatic" {
		t.Fatalf("automatic upgrade mismatch: membership=%#v changed=%v err=%v", automatic, changed, err)
	}

	expiresAt := now.Add(2 * time.Hour)
	manual, err := service.GrantManualMembershipTx(tx, user.ID, base.ID, admin.ID, &expiresAt, now.Add(time.Minute))
	if err != nil || manual.Source != "manual" || manual.MemberLevelID != base.ID {
		t.Fatalf("manual grant mismatch: membership=%#v err=%v", manual, err)
	}
	effective, level, err := service.EffectiveUserMembershipTx(tx, user.ID, now.Add(30*time.Minute))
	if err != nil || effective == nil || level == nil || effective.Source != "manual" || level.ID != base.ID {
		t.Fatalf("live manual grant was not preserved: membership=%#v level=%#v err=%v", effective, level, err)
	}

	effective, level, err = service.EffectiveUserMembershipTx(tx, user.ID, now.Add(3*time.Hour))
	if err != nil || effective == nil || level == nil || effective.Source != "automatic" || level.ID != gold.ID || effective.ExpiresAt != nil {
		t.Fatalf("expired manual grant was not recalculated: membership=%#v level=%#v err=%v", effective, level, err)
	}

	refund := model.Refund{
		RefundNo: "MEM-R-" + suffix, OrderID: order.ID, Amount: 12_000,
		OrderAmount: order.Total, OrderCurrency: order.Currency,
		Reason: "membership lifecycle integration", Status: "succeeded", RequestedBy: "system",
	}
	if err := tx.Create(&refund).Error; err != nil {
		t.Fatalf("create refund: %v", err)
	}
	downgraded, changed, err := service.ReconcileUserMembershipTx(tx, user.ID, now.Add(4*time.Hour))
	if err != nil || !changed || downgraded == nil || downgraded.MemberLevelID != base.ID || downgraded.Source != "automatic" {
		t.Fatalf("refund downgrade mismatch: membership=%#v changed=%v err=%v", downgraded, changed, err)
	}

	if err := tx.Model(&base).Update("enabled", false).Error; err != nil {
		t.Fatalf("disable base level: %v", err)
	}
	removed, changed, err := service.ReconcileUserMembershipTx(tx, user.ID, now.Add(5*time.Hour))
	if err != nil || !changed || removed != nil {
		t.Fatalf("disabled ineligible level was not removed: membership=%#v changed=%v err=%v", removed, changed, err)
	}
	var count int64
	if err := tx.Model(&model.UserLevelMembership{}).Where("user_id = ?", user.ID).Count(&count).Error; err != nil || count != 0 {
		t.Fatalf("membership row still present: count=%d err=%v", count, err)
	}
}

func membershipPostgresTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := os.Getenv("LINLINQI_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("LINLINQI_TEST_DATABASE_URL is not set")
	}
	adminDB, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("connect postgres: %v", err)
	}
	schemaName := "linlinqi_membership_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if err := adminDB.Exec(`CREATE SCHEMA "` + schemaName + `"`).Error; err != nil {
		t.Fatalf("create schema: %v", err)
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
		t.Fatalf("open isolated membership schema: %v", err)
	}
	if err := database.Migrate(db); err != nil {
		t.Fatalf("migrate isolated membership schema: %v", err)
	}
	return db
}
