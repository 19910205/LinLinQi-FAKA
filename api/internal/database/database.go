package database

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"linlinqi/api/internal/config"
	"linlinqi/api/internal/model"
	"linlinqi/api/internal/security"
)

type Resources struct {
	DB    *gorm.DB
	Redis *redis.Client
}

type schemaMigration struct {
	Version   string    `gorm:"primaryKey;size:80"`
	Checksum  string    `gorm:"size:64;not null"`
	AppliedAt time.Time `gorm:"not null"`
}

func (schemaMigration) TableName() string { return "linlinqi_schema_migrations" }

func Connect(cfg config.Config) (*Resources, error) {
	level := logger.Warn
	if cfg.Env == "development" {
		level = logger.Info
	}
	dbLogger := logger.New(log.New(log.Writer(), "gorm ", log.LstdFlags), logger.Config{
		SlowThreshold:             200 * time.Millisecond,
		LogLevel:                  level,
		IgnoreRecordNotFoundError: true,
		ParameterizedQueries:      true,
		Colorful:                  false,
	})
	db, err := gorm.Open(postgres.Open(cfg.DatabaseURL), &gorm.Config{Logger: dbLogger})
	if err != nil {
		return nil, fmt.Errorf("connect postgres: %w", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("postgres handle: %w", err)
	}
	sqlDB.SetMaxIdleConns(10)
	sqlDB.SetMaxOpenConns(60)
	sqlDB.SetConnMaxLifetime(time.Hour)

	rdb := redis.NewClient(&redis.Options{Addr: cfg.RedisAddr, Password: cfg.RedisPassword, DB: cfg.RedisDB})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := rdb.Ping(ctx).Err(); err != nil {
		log.Printf("redis unavailable; readiness, strict rate limits and queues will fail closed: %v", err)
	}
	return &Resources{DB: db, Redis: rdb}, nil
}

func Migrate(db *gorm.DB) error {
	return db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtext(?))", "linlinqi-schema-migration").Error; err != nil {
			return err
		}
		if err := tx.AutoMigrate(&schemaMigration{}); err != nil {
			return err
		}
		apply := func(version, checksum string, migrate func(*gorm.DB) error) error {
			var applied schemaMigration
			if err := tx.Where("version = ?", version).First(&applied).Error; err == nil {
				if applied.Checksum != checksum {
					return fmt.Errorf("migration %s checksum mismatch", version)
				}
				return nil
			} else if err != gorm.ErrRecordNotFound {
				return err
			}
			if err := migrate(tx); err != nil {
				return fmt.Errorf("apply migration %s: %w", version, err)
			}
			return tx.Create(&schemaMigration{Version: version, Checksum: checksum, AppliedAt: time.Now().UTC()}).Error
		}

		if err := apply(
			"20260809_001_initial_operating_schema",
			"b92cf7a8f4f9d665203d2998ef4bd7355ce0ce8d29aa5f6c3b8c1d78bbcedbee",
			func(migration *gorm.DB) error {
				return migration.AutoMigrate(
					&model.User{}, &model.Admin{}, &model.Category{}, &model.Product{}, &model.Card{},
					&model.Order{}, &model.OrderItem{}, &model.Supplier{}, &model.SupplierProduct{},
					&model.PaymentChannel{}, &model.Coupon{}, &model.Announcement{}, &model.APICredential{},
					&model.WebhookEvent{}, &model.AuditLog{}, &model.Setting{},
					&model.Role{}, &model.Permission{}, &model.AdminRole{}, &model.RolePermission{},
					&model.UserSession{}, &model.PasswordResetToken{}, &model.LoginEvent{}, &model.TOTPDevice{}, &model.OAuthIdentity{},
					&model.MemberLevel{}, &model.UserLevelMembership{},
					&model.ProductVariant{}, &model.ProductPriceTier{}, &model.ProductPaymentChannel{},
					&model.InventoryBatch{}, &model.Cart{}, &model.CartItem{}, &model.ProductMapping{},
					&model.PaymentIntent{}, &model.PaymentTransaction{}, &model.Refund{}, &model.OrderEvent{},
					&model.FulfillmentAttempt{}, &model.SupportTicket{}, &model.TicketMessage{},
					&model.RiskRule{}, &model.RiskDecision{}, &model.ReconciliationBatch{}, &model.ReconciliationItem{},
					&model.ProcurementOrder{}, &model.SiteConnection{}, &model.CallbackRoute{},
					&model.Promotion{}, &model.PromotionProduct{}, &model.GiftCard{}, &model.GiftCardEntry{},
					&model.Banner{}, &model.PostCategory{}, &model.Post{}, &model.MediaAsset{},
					&model.WalletAccount{}, &model.WalletEntry{}, &model.RechargeOrder{},
					&model.AffiliateProfile{}, &model.AffiliateCommission{}, &model.AffiliateWithdrawal{},
					&model.ResellerProfile{}, &model.ResellerDomain{}, &model.ResellerSite{},
					&model.ResellerProductRule{}, &model.ResellerWithdrawal{},
					&model.NotificationTemplate{}, &model.NotificationDelivery{}, &model.WebhookEndpoint{},
					&model.WebhookDelivery{}, &model.JobRecord{}, &model.SecurityEvent{}, &model.IPBlocklist{},
					&model.SystemMetric{},
				)
			},
		); err != nil {
			return err
		}
		if err := apply(
			"20260809_002_openapi_call_logs",
			"97b4bb5931a16d25d69eff92238632ea99af142ee452edeb2b13f964a3e2dc41",
			func(migration *gorm.DB) error { return migration.AutoMigrate(&model.APICallLog{}) },
		); err != nil {
			return err
		}
		if err := apply(
			"20260809_003_transaction_integrity_guards",
			"902c43654d67f4f615d8f09bd23ee87c5598aedf41ad8fee9bef7477a227a8c6",
			func(migration *gorm.DB) error {
				if err := migration.AutoMigrate(&model.Order{}, &model.NotificationDelivery{}, &model.Refund{}, &model.ProcurementOrder{}); err != nil {
					return err
				}
				statements := []string{
					`UPDATE api_credentials SET status = 'suspended', updated_at = NOW() WHERE owner_id IS NULL AND status = 'active'`,
					`CREATE UNIQUE INDEX IF NOT EXISTS idx_payment_intents_active_order ON payment_intents (order_id) WHERE deleted_at IS NULL AND status IN ('creating', 'pending')`,
					`CREATE UNIQUE INDEX IF NOT EXISTS idx_payment_channel_trade ON payment_intents (channel_id, provider_trade_no) WHERE deleted_at IS NULL AND provider_trade_no <> ''`,
					`CREATE UNIQUE INDEX IF NOT EXISTS idx_procurement_order_item ON procurement_orders (order_item_id)`,
					`CREATE UNIQUE INDEX IF NOT EXISTS idx_webhook_endpoint_event ON webhook_deliveries (endpoint_id, event_id)`,
				}
				for _, statement := range statements {
					if err := migration.Exec(statement).Error; err != nil {
						return err
					}
				}
				return nil
			},
		); err != nil {
			return err
		}
		if err := apply(
			"20260809_004_auth_session_hardening",
			"a4051b339cb940c6ee5d864aec36e7eec1a2e0e7459297619a5d787a6ac45888",
			func(migration *gorm.DB) error {
				return migration.AutoMigrate(&model.UserSessionToken{}, &model.Admin{}, &model.TOTPDevice{})
			},
		); err != nil {
			return err
		}
		if err := apply(
			"20260809_005_variant_pricing_coupon_integrity",
			"e6f2f0f8118fca03a978a517247be42264c9986ce6516f1971fe2a84437fc9a9",
			func(migration *gorm.DB) error {
				if err := migration.AutoMigrate(
					&model.Card{}, &model.Order{}, &model.OrderItem{}, &model.SupplierProduct{},
					&model.ProductMapping{}, &model.CartItem{}, &model.CouponRedemption{},
				); err != nil {
					return err
				}
				statements := []string{
					`CREATE INDEX IF NOT EXISTS idx_cards_sellable_variant ON cards (product_id, variant_id, status) WHERE deleted_at IS NULL`,
					`CREATE UNIQUE INDEX IF NOT EXISTS idx_coupon_codes_casefold ON coupons (UPPER(code)) WHERE deleted_at IS NULL`,
					`CREATE UNIQUE INDEX IF NOT EXISTS idx_coupon_redemptions_active_redeemer ON coupon_redemptions (coupon_id, redeemer_key) WHERE deleted_at IS NULL AND status IN ('reserved', 'consumed')`,
					`CREATE UNIQUE INDEX IF NOT EXISTS idx_product_mapping_base ON product_mappings (supplier_id, product_id) WHERE deleted_at IS NULL AND variant_id IS NULL`,
					`CREATE UNIQUE INDEX IF NOT EXISTS idx_product_mapping_variant ON product_mappings (supplier_id, product_id, variant_id) WHERE deleted_at IS NULL AND variant_id IS NOT NULL`,
					`CREATE UNIQUE INDEX IF NOT EXISTS idx_supplier_product_base ON supplier_products (supplier_id, product_id) WHERE deleted_at IS NULL AND variant_id IS NULL`,
					`CREATE UNIQUE INDEX IF NOT EXISTS idx_supplier_product_variant ON supplier_products (supplier_id, product_id, variant_id) WHERE deleted_at IS NULL AND variant_id IS NOT NULL`,
					`CREATE UNIQUE INDEX IF NOT EXISTS idx_cart_item_base ON cart_items (cart_id, product_id) WHERE deleted_at IS NULL AND variant_id IS NULL`,
					`CREATE UNIQUE INDEX IF NOT EXISTS idx_cart_item_variant ON cart_items (cart_id, product_id, variant_id) WHERE deleted_at IS NULL AND variant_id IS NOT NULL`,
					`ALTER TABLE product_variants ADD CONSTRAINT chk_product_variants_prices CHECK (price >= 0 AND compare_price >= 0 AND cost_price >= 0 AND purchase_limit >= 0)`,
					`ALTER TABLE coupons ADD CONSTRAINT chk_coupons_values CHECK (value > 0 AND min_amount >= 0 AND usage_limit >= 0 AND used_count >= 0 AND (usage_limit = 0 OR used_count <= usage_limit))`,
				}
				for _, statement := range statements {
					if err := migration.Exec(statement).Error; err != nil {
						return err
					}
				}
				return nil
			},
		); err != nil {
			return err
		}
		if err := apply(
			"20260809_006_risk_enforcement",
			"c0ea6c2aa3c77b71d9045b8d0fc56da7e7147ad64ef4c0cf2b54d4e4938ad4c4",
			func(migration *gorm.DB) error {
				if err := migration.AutoMigrate(&model.IPBlocklist{}, &model.RiskRule{}, &model.RiskDecision{}); err != nil {
					return err
				}
				statements := []string{
					`DROP INDEX IF EXISTS idx_ip_blocklists_cidr`,
					`CREATE UNIQUE INDEX IF NOT EXISTS idx_ip_block_scope ON ip_blocklists (cidr, scope) WHERE deleted_at IS NULL`,
					`ALTER TABLE ip_blocklists ADD CONSTRAINT chk_ip_blocklist_scope CHECK (scope IN ('public', 'admin', 'openapi', 'all'))`,
					`ALTER TABLE risk_decisions ADD CONSTRAINT chk_risk_decision CHECK (decision IN ('allow', 'review', 'challenge', 'deny'))`,
				}
				for _, statement := range statements {
					if err := migration.Exec(statement).Error; err != nil {
						return err
					}
				}
				return nil
			},
		); err != nil {
			return err
		}
		if err := apply(
			"20260809_007_ticket_workflow",
			"f6374946bba828fe5f6b1375ff993255db9452a66e86790715fcb16b617d2934",
			func(migration *gorm.DB) error {
				if err := migration.AutoMigrate(&model.SupportTicket{}, &model.TicketMessage{}); err != nil {
					return err
				}
				statements := []string{
					`ALTER TABLE support_tickets ADD CONSTRAINT chk_support_ticket_status CHECK (status IN ('open', 'in_progress', 'waiting_user', 'resolved', 'closed'))`,
					`ALTER TABLE support_tickets ADD CONSTRAINT chk_support_ticket_priority CHECK (priority IN ('low', 'normal', 'high', 'urgent'))`,
					`ALTER TABLE support_tickets ADD CONSTRAINT chk_support_ticket_unread CHECK (user_unread >= 0 AND admin_unread >= 0)`,
					`ALTER TABLE ticket_messages ADD CONSTRAINT chk_ticket_message_author CHECK (author_type IN ('user', 'admin', 'system'))`,
				}
				for _, statement := range statements {
					if err := migration.Exec(statement).Error; err != nil {
						return err
					}
				}
				return nil
			},
		); err != nil {
			return err
		}
		if err := apply(
			"20260809_008_gift_card_workflow",
			"72023239862d63037b1fe7cd3ce404aab382a77b62c87d08cd8b1475c00ff9fb",
			func(migration *gorm.DB) error {
				if err := migration.AutoMigrate(&model.GiftCardBatch{}, &model.GiftCard{}, &model.GiftCardEntry{}, &model.WalletAccount{}, &model.WalletEntry{}); err != nil {
					return err
				}
				statements := []string{
					`ALTER TABLE gift_card_batches ADD CONSTRAINT chk_gift_card_batch_values CHECK (quantity > 0 AND card_value > 0 AND currency = 'CNY' AND status IN ('active', 'disabled'))`,
					`ALTER TABLE gift_cards ADD CONSTRAINT chk_gift_card_values CHECK (initial_balance > 0 AND balance >= 0 AND balance <= initial_balance AND currency = 'CNY' AND status IN ('active', 'disabled', 'redeemed', 'expired'))`,
					`ALTER TABLE gift_card_entries ADD CONSTRAINT chk_gift_card_entry_type CHECK (type IN ('redeem', 'spend', 'refund', 'adjustment'))`,
					`CREATE INDEX IF NOT EXISTS idx_gift_cards_operating ON gift_cards (batch_id, status, expires_at) WHERE deleted_at IS NULL`,
				}
				for _, statement := range statements {
					if err := migration.Exec(statement).Error; err != nil {
						return err
					}
				}
				return nil
			},
		); err != nil {
			return err
		}
		if err := apply(
			"20260809_009_marketing_workflow",
			"4caf3a056888593457048c8b74ead7a4ad1b69636c28e4d33ee46087dc974d65",
			func(migration *gorm.DB) error {
				if err := migration.AutoMigrate(&model.Promotion{}, &model.PromotionProduct{}, &model.Coupon{}, &model.CouponRedemption{}); err != nil {
					return err
				}
				statements := []string{
					`CREATE UNIQUE INDEX IF NOT EXISTS idx_promotion_codes_casefold ON promotions (UPPER(code)) WHERE deleted_at IS NULL`,
					`ALTER TABLE promotions ADD CONSTRAINT chk_promotion_type CHECK (type IN ('percentage', 'fixed', 'threshold_fixed', 'flash_price'))`,
					`ALTER TABLE promotions ADD CONSTRAINT chk_promotion_status CHECK (status IN ('draft', 'active', 'paused', 'archived'))`,
					`ALTER TABLE promotions ADD CONSTRAINT chk_promotion_period CHECK (ends_at > starts_at)`,
					`ALTER TABLE coupons ADD CONSTRAINT chk_coupon_type CHECK (type IN ('fixed', 'percentage'))`,
					`ALTER TABLE coupons ADD CONSTRAINT chk_coupon_period CHECK (ends_at > starts_at)`,
				}
				for _, statement := range statements {
					if err := migration.Exec(statement).Error; err != nil {
						return err
					}
				}
				return nil
			},
		); err != nil {
			return err
		}
		if err := apply(
			"20260809_010_affiliate_workflow",
			"e5d00c5662b3e16b99b229239188467a21024583a3fc21c4e3f581468077a505",
			func(migration *gorm.DB) error {
				if err := migration.AutoMigrate(&model.AffiliateProfile{}, &model.AffiliateReferral{}, &model.AffiliateCommission{}, &model.AffiliateWithdrawal{}); err != nil {
					return err
				}
				statements := []string{
					`UPDATE affiliate_profiles SET applied_at = created_at WHERE applied_at IS NULL OR applied_at < '2000-01-01'`,
					`CREATE UNIQUE INDEX IF NOT EXISTS idx_affiliate_referral_codes_casefold ON affiliate_profiles (UPPER(referral_code)) WHERE deleted_at IS NULL`,
					`ALTER TABLE affiliate_profiles ADD CONSTRAINT chk_affiliate_profile_values CHECK (commission_basis_point BETWEEN 1 AND 3000 AND total_commission >= 0 AND frozen_commission >= 0 AND status IN ('pending', 'active', 'suspended', 'rejected'))`,
					`ALTER TABLE affiliate_commissions ADD CONSTRAINT chk_affiliate_commission_values CHECK (order_amount > 0 AND commission > 0 AND reversed_amount >= 0 AND reversed_amount <= commission AND status IN ('pending', 'available', 'partially_reversed', 'reversed'))`,
					`ALTER TABLE affiliate_withdrawals ADD CONSTRAINT chk_affiliate_withdrawal_values CHECK (amount > 0 AND fee >= 0 AND fee < amount AND method IN ('alipay', 'bank', 'usdt') AND status IN ('pending', 'processing', 'completed', 'rejected'))`,
					`CREATE INDEX IF NOT EXISTS idx_affiliate_commissions_settlement ON affiliate_commissions (settles_at) WHERE deleted_at IS NULL AND settled_at IS NULL`,
				}
				for _, statement := range statements {
					if err := migration.Exec(statement).Error; err != nil {
						return err
					}
				}
				return nil
			},
		); err != nil {
			return err
		}
		if err := apply(
			"20260809_011_reconciliation_workflow",
			"4e74804242342207fcc31c1fd754f69b26b701d7e6d8ab4be729c5aacbd5a58b",
			func(migration *gorm.DB) error {
				if err := migration.AutoMigrate(&model.ReconciliationBatch{}, &model.ReconciliationItem{}); err != nil {
					return err
				}
				statements := []string{
					`UPDATE reconciliation_batches SET statement_hash = MD5(batch_no || id::text) WHERE statement_hash IS NULL OR statement_hash = ''`,
					`UPDATE reconciliation_items SET direction = 'payment' WHERE direction IS NULL OR direction = ''`,
					`ALTER TABLE reconciliation_batches ALTER COLUMN statement_hash SET NOT NULL`,
					`ALTER TABLE reconciliation_items ALTER COLUMN direction SET NOT NULL`,
					`CREATE UNIQUE INDEX IF NOT EXISTS idx_reconciliation_statement ON reconciliation_batches (channel_id, statement_hash) WHERE deleted_at IS NULL`,
					`CREATE UNIQUE INDEX IF NOT EXISTS idx_reconciliation_item_trade ON reconciliation_items (batch_id, direction, provider_trade_no) WHERE deleted_at IS NULL AND provider_trade_no <> ''`,
					`ALTER TABLE reconciliation_batches ADD CONSTRAINT chk_reconciliation_batch_values CHECK (period_to > period_from AND total >= 0 AND matched >= 0 AND mismatched >= 0 AND resolved >= 0 AND status IN ('pending', 'processing', 'completed', 'differences_found', 'failed'))`,
					`ALTER TABLE reconciliation_items ADD CONSTRAINT chk_reconciliation_item_values CHECK (direction IN ('payment', 'refund') AND system_amount >= 0 AND provider_amount >= 0 AND status IN ('matched', 'amount_mismatch', 'missing_system', 'missing_provider', 'resolved'))`,
				}
				for _, statement := range statements {
					if err := migration.Exec(statement).Error; err != nil {
						return err
					}
				}
				return nil
			},
		); err != nil {
			return err
		}
		if err := apply(
			"20260809_012_reseller_management",
			"8a38faf44e75959871fc76f16705fffa163c4c12d8818f80fb72b14c70a81ca4",
			func(migration *gorm.DB) error {
				if err := migration.AutoMigrate(&model.ResellerProfile{}, &model.ResellerDomain{}, &model.ResellerSite{}, &model.ResellerProductRule{}, &model.Order{}, &model.OrderItem{}); err != nil {
					return err
				}
				statements := []string{
					`UPDATE reseller_profiles SET applied_at = created_at WHERE applied_at IS NULL OR applied_at < '2000-01-01'`,
					`UPDATE reseller_domains SET verification_token = CONCAT('legacy-verification-required=', REPLACE(id::text, '-', '')) WHERE verification_token IS NULL OR verification_token = ''`,
					`ALTER TABLE reseller_domains ALTER COLUMN verification_token SET NOT NULL`,
					`CREATE UNIQUE INDEX IF NOT EXISTS idx_reseller_rule_base ON reseller_product_rules (reseller_id, product_id) WHERE deleted_at IS NULL AND variant_id IS NULL`,
					`CREATE UNIQUE INDEX IF NOT EXISTS idx_reseller_rule_variant ON reseller_product_rules (reseller_id, product_id, variant_id) WHERE deleted_at IS NULL AND variant_id IS NOT NULL`,
					`ALTER TABLE reseller_profiles ADD CONSTRAINT chk_reseller_profile_values CHECK (credit_limit >= 0 AND wholesale_level BETWEEN 0 AND 10 AND status IN ('pending', 'active', 'suspended', 'rejected'))`,
					`ALTER TABLE reseller_domains ADD CONSTRAINT chk_reseller_domain_values CHECK (status IN ('pending_verification', 'verified', 'active', 'suspended', 'rejected') AND tls_status IN ('pending', 'provisioning', 'active', 'failed', 'disabled'))`,
					`ALTER TABLE reseller_product_rules ADD CONSTRAINT chk_reseller_rule_values CHECK (pricing_mode IN ('markup', 'fixed') AND markup_basis_point BETWEEN 0 AND 10000 AND fixed_price >= 0)`,
					`ALTER TABLE orders ADD CONSTRAINT chk_order_reseller_margin CHECK (reseller_margin >= 0)`,
					`ALTER TABLE order_items ADD CONSTRAINT chk_order_item_reseller_values CHECK (platform_unit_price >= 0 AND reseller_margin >= 0)`,
				}
				for _, statement := range statements {
					if err := migration.Exec(statement).Error; err != nil {
						return err
					}
				}
				return nil
			},
		); err != nil {
			return err
		}
		if err := apply(
			"20260809_013_reseller_storefront",
			"44d870d9d7704db88f6123055430ffec6de333f785a3251a74d1a9f1a15f406d",
			func(migration *gorm.DB) error {
				if err := migration.AutoMigrate(&model.ResellerDomain{}, &model.Cart{}, &model.Order{}, &model.OrderItem{}, &model.WalletAccount{}, &model.WalletEntry{}); err != nil {
					return err
				}
				statements := []string{
					`DROP INDEX IF EXISTS idx_reseller_domains_domain`,
					`CREATE UNIQUE INDEX IF NOT EXISTS idx_reseller_domain_active ON reseller_domains (domain) WHERE deleted_at IS NULL`,
					`UPDATE orders SET reseller_margin_reversed = 0 WHERE reseller_margin_reversed IS NULL`,
					`ALTER TABLE orders ADD CONSTRAINT chk_order_reseller_margin_reversal CHECK (reseller_margin_reversed >= 0 AND reseller_margin_reversed <= reseller_margin)`,
				}
				for _, statement := range statements {
					if err := migration.Exec(statement).Error; err != nil {
						return err
					}
				}
				return nil
			},
		); err != nil {
			return err
		}
		if err := apply(
			"20260809_014_content_media_public_url",
			"c6408f45bf947f7f6525464963772630e4487cf9b60237f286c2afb745181860",
			func(migration *gorm.DB) error {
				if err := migration.AutoMigrate(&model.MediaAsset{}); err != nil {
					return err
				}
				statements := []string{
					`CREATE UNIQUE INDEX IF NOT EXISTS idx_media_assets_public_url_active ON media_assets (public_url) WHERE deleted_at IS NULL AND public_url <> ''`,
					`UPDATE posts SET status = 'draft', published_at = NULL WHERE status NOT IN ('draft', 'published')`,
					`ALTER TABLE posts ADD CONSTRAINT chk_posts_status CHECK (status IN ('draft', 'published'))`,
					`UPDATE announcements SET level = 'info' WHERE level NOT IN ('info', 'important', 'warning')`,
					`ALTER TABLE announcements ADD CONSTRAINT chk_announcements_level CHECK (level IN ('info', 'important', 'warning'))`,
					`UPDATE banners SET ends_at = NULL WHERE starts_at IS NOT NULL AND ends_at IS NOT NULL AND ends_at <= starts_at`,
					`ALTER TABLE banners ADD CONSTRAINT chk_banners_window CHECK (starts_at IS NULL OR ends_at IS NULL OR ends_at > starts_at)`,
					`ALTER TABLE media_assets ADD CONSTRAINT chk_media_assets_external_url CHECK (public_url = '' OR public_url LIKE 'https://%')`,
				}
				for _, statement := range statements {
					if err := migration.Exec(statement).Error; err != nil {
						return err
					}
				}
				return nil
			},
		); err != nil {
			return err
		}
		if err := apply(
			"20260809_015_financial_auth_workflows",
			"8b78c4de8afce65416c563d78630b0c7d40f4ea3835bd5dd44b5dc5a820df8c7",
			func(migration *gorm.DB) error {
				if err := migration.AutoMigrate(
					&model.RechargeOrder{}, &model.RechargeTransaction{}, &model.ResellerWithdrawal{},
					&model.OAuthIdentity{}, &model.WalletAccount{}, &model.WalletEntry{},
				); err != nil {
					return err
				}
				statements := []string{
					`UPDATE recharge_orders SET intent_no = CONCAT('LQRC-LEGACY-', REPLACE(id::text, '-', '')) WHERE intent_no IS NULL OR intent_no = ''`,
					`UPDATE recharge_orders SET idempotency_key_hash = MD5(CONCAT('legacy:', id::text)) WHERE idempotency_key_hash IS NULL OR idempotency_key_hash = ''`,
					`UPDATE recharge_orders SET currency = 'CNY' WHERE currency IS NULL OR currency = ''`,
					`UPDATE recharge_orders SET expires_at = created_at + INTERVAL '15 minutes' WHERE expires_at IS NULL OR expires_at < '2000-01-01'`,
					`UPDATE reseller_withdrawals SET method = 'bank' WHERE method IS NULL OR method = ''`,
					`CREATE UNIQUE INDEX IF NOT EXISTS idx_recharge_intent_active ON recharge_orders (intent_no) WHERE deleted_at IS NULL`,
					`CREATE UNIQUE INDEX IF NOT EXISTS idx_recharge_user_idempotency ON recharge_orders (user_id, idempotency_key_hash) WHERE deleted_at IS NULL`,
					`CREATE UNIQUE INDEX IF NOT EXISTS idx_recharge_channel_trade ON recharge_orders (channel_id, provider_trade_no) WHERE deleted_at IS NULL AND provider_trade_no <> ''`,
					`CREATE INDEX IF NOT EXISTS idx_recharge_user_history ON recharge_orders (user_id, created_at DESC) WHERE deleted_at IS NULL`,
					`CREATE INDEX IF NOT EXISTS idx_reseller_withdrawal_queue ON reseller_withdrawals (status, created_at) WHERE deleted_at IS NULL`,
					`ALTER TABLE recharge_orders ADD CONSTRAINT chk_recharge_order_values CHECK (amount > 0 AND bonus >= 0 AND currency = 'CNY' AND status IN ('creating', 'pending', 'succeeded', 'failed', 'expired', 'cancelled'))`,
					`ALTER TABLE recharge_transactions ADD CONSTRAINT chk_recharge_transaction_values CHECK (amount > 0 AND status IN ('succeeded', 'ignored'))`,
					`ALTER TABLE reseller_withdrawals ADD CONSTRAINT chk_reseller_withdrawal_values CHECK (amount > 0 AND fee >= 0 AND fee < amount AND method IN ('alipay', 'bank', 'usdt') AND status IN ('pending', 'processing', 'completed', 'rejected'))`,
				}
				for _, statement := range statements {
					if err := migration.Exec(statement).Error; err != nil {
						return err
					}
				}
				return nil
			},
		); err != nil {
			return err
		}
		if err := apply(
			"20260809_016_api_credential_lifecycle",
			"358a1287c787eef798b8ef0d8d09aaca153d0319703f72027f64bc9e6151f70d",
			func(migration *gorm.DB) error {
				if err := migration.AutoMigrate(&model.APICredential{}); err != nil {
					return err
				}
				statements := []string{
					`UPDATE api_credentials SET status = 'suspended', revoked_at = NULL WHERE status NOT IN ('pending', 'active', 'suspended', 'revoked')`,
					`UPDATE api_credentials SET revoked_at = COALESCE(revoked_at, updated_at, NOW()) WHERE status = 'revoked'`,
					`UPDATE api_credentials
					SET secret_cipher = decode(md5('revoked-cipher:' || id::text), 'hex'),
						secret_nonce = decode(md5('revoked-nonce:' || id::text), 'hex')
					WHERE status = 'revoked'`,
					`UPDATE api_credentials SET revoked_at = NULL WHERE status <> 'revoked'`,
					`WITH ranked AS (
						SELECT id, ROW_NUMBER() OVER (PARTITION BY owner_id, LOWER(name) ORDER BY created_at, id) AS duplicate_number
						FROM api_credentials
						WHERE deleted_at IS NULL AND owner_type = 'user' AND owner_id IS NOT NULL AND status <> 'revoked'
					) UPDATE api_credentials AS credential
					SET name = LEFT(credential.name, 62) || ' #' || REPLACE(credential.id::text, '-', '')
					FROM ranked WHERE credential.id = ranked.id AND ranked.duplicate_number > 1`,
					`CREATE INDEX IF NOT EXISTS idx_api_credentials_owner_status ON api_credentials (owner_type, owner_id, status) WHERE deleted_at IS NULL`,
					`CREATE UNIQUE INDEX IF NOT EXISTS idx_api_credentials_user_active_name ON api_credentials (owner_id, LOWER(name)) WHERE deleted_at IS NULL AND owner_type = 'user' AND owner_id IS NOT NULL AND status <> 'revoked'`,
					`ALTER TABLE api_credentials ADD CONSTRAINT chk_api_credentials_status CHECK (status IN ('pending', 'active', 'suspended', 'revoked'))`,
					`ALTER TABLE api_credentials ADD CONSTRAINT chk_api_credentials_revoked_at CHECK ((status = 'revoked' AND revoked_at IS NOT NULL) OR (status <> 'revoked' AND revoked_at IS NULL))`,
				}
				for _, statement := range statements {
					if err := migration.Exec(statement).Error; err != nil {
						return err
					}
				}
				return nil
			},
		); err != nil {
			return err
		}
		if err := apply(
			"20260809_017_supplier_callback_interop",
			"745da81591ac0befbe8d6195573a88f7757cc531d87ade27f4cc5ee6d8694b71",
			func(migration *gorm.DB) error {
				if err := migration.AutoMigrate(&model.Order{}, &model.OrderItem{}, &model.ProcurementOrder{}, &model.WebhookDelivery{}, &model.WebhookEvent{}); err != nil {
					return err
				}
				statements := []string{
					`UPDATE webhook_events SET status = 'failed', processed_at = COALESCE(processed_at, NOW()), response = 'legacy event status normalized' WHERE status NOT IN ('queued', 'processing', 'processed', 'failed', 'ignored')`,
					`WITH ranked AS (
						SELECT id, ROW_NUMBER() OVER (PARTITION BY owner_id, url ORDER BY created_at, id) AS duplicate_number
						FROM webhook_endpoints
						WHERE deleted_at IS NULL AND owner_type = 'api_credential'
					) UPDATE webhook_endpoints AS endpoint
					SET enabled = FALSE, disabled_at = COALESCE(endpoint.disabled_at, NOW()), deleted_at = NOW()
					FROM ranked WHERE endpoint.id = ranked.id AND ranked.duplicate_number > 1`,
					`CREATE UNIQUE INDEX IF NOT EXISTS idx_webhook_api_credential_url ON webhook_endpoints (owner_id, url) WHERE deleted_at IS NULL AND owner_type = 'api_credential'`,
					`CREATE INDEX IF NOT EXISTS idx_webhook_events_procurement_queue ON webhook_events (procurement_order_id, status, created_at) WHERE deleted_at IS NULL AND procurement_order_id IS NOT NULL`,
					`CREATE INDEX IF NOT EXISTS idx_orders_callback_endpoint ON orders (callback_endpoint_id) WHERE deleted_at IS NULL AND callback_endpoint_id IS NOT NULL`,
					`ALTER TABLE orders ADD CONSTRAINT fk_orders_callback_endpoint FOREIGN KEY (callback_endpoint_id) REFERENCES webhook_endpoints(id) ON DELETE SET NULL`,
					`ALTER TABLE webhook_events ADD CONSTRAINT fk_webhook_events_supplier FOREIGN KEY (supplier_id) REFERENCES suppliers(id) ON DELETE SET NULL`,
					`ALTER TABLE webhook_events ADD CONSTRAINT fk_webhook_events_procurement FOREIGN KEY (procurement_order_id) REFERENCES procurement_orders(id) ON DELETE SET NULL`,
					`ALTER TABLE webhook_events ADD CONSTRAINT chk_webhook_events_status CHECK (status IN ('queued', 'processing', 'processed', 'failed', 'ignored'))`,
					`ALTER TABLE webhook_events ADD CONSTRAINT chk_supplier_callback_encryption CHECK (supplier_id IS NULL OR (procurement_order_id IS NOT NULL AND OCTET_LENGTH(payload_cipher) > 0 AND OCTET_LENGTH(payload_nonce) > 0 AND payload = '{}'))`,
					`ALTER TABLE webhook_deliveries ADD CONSTRAINT chk_webhook_delivery_payload_encryption CHECK ((payload_cipher IS NULL AND payload_nonce IS NULL) OR (OCTET_LENGTH(payload_cipher) > 0 AND OCTET_LENGTH(payload_nonce) > 0 AND payload = '{}'))`,
					`ALTER TABLE order_items ADD CONSTRAINT chk_order_item_delivery_encryption CHECK ((delivery_items_cipher IS NULL AND delivery_items_nonce IS NULL) OR (OCTET_LENGTH(delivery_items_cipher) > 0 AND OCTET_LENGTH(delivery_items_nonce) > 0))`,
					`ALTER TABLE procurement_orders ADD CONSTRAINT chk_procurement_callback_secret CHECK ((callback_secret_cipher IS NULL AND callback_secret_nonce IS NULL) OR (OCTET_LENGTH(callback_secret_cipher) > 0 AND OCTET_LENGTH(callback_secret_nonce) > 0))`,
				}
				for _, statement := range statements {
					if err := migration.Exec(statement).Error; err != nil {
						return err
					}
				}
				return nil
			},
		); err != nil {
			return err
		}
		if err := apply(
			"20260809_018_product_checkout_inputs",
			"2f5fa3d74a41bbb811ef1c2ef5b03ddbe3ff163465ec85029648e6a4c16729c3",
			func(migration *gorm.DB) error {
				if err := migration.AutoMigrate(&model.ProductInputField{}, &model.OrderInputValue{}); err != nil {
					return err
				}
				statements := []string{
					`UPDATE product_input_fields SET options = '[]'::jsonb WHERE options IS NULL OR jsonb_typeof(options) <> 'array'`,
					`CREATE UNIQUE INDEX IF NOT EXISTS idx_product_input_field_key ON product_input_fields (product_id, LOWER(key)) WHERE deleted_at IS NULL`,
					`CREATE UNIQUE INDEX IF NOT EXISTS idx_order_input_value_field ON order_input_values (order_id, product_id, COALESCE(variant_id, '00000000-0000-0000-0000-000000000000'::uuid), product_input_field_id) WHERE deleted_at IS NULL AND product_input_field_id IS NOT NULL`,
					`CREATE INDEX IF NOT EXISTS idx_order_input_values_supplier ON order_input_values (order_id, product_id, variant_id) WHERE deleted_at IS NULL AND pass_to_supplier = TRUE`,
					`ALTER TABLE product_input_fields ADD CONSTRAINT fk_product_input_fields_product FOREIGN KEY (product_id) REFERENCES products(id) ON DELETE RESTRICT`,
					`ALTER TABLE order_input_values ADD CONSTRAINT fk_order_input_values_order FOREIGN KEY (order_id) REFERENCES orders(id) ON DELETE CASCADE`,
					`ALTER TABLE order_input_values ADD CONSTRAINT fk_order_input_values_product FOREIGN KEY (product_id) REFERENCES products(id) ON DELETE RESTRICT`,
					`ALTER TABLE order_input_values ADD CONSTRAINT fk_order_input_values_variant FOREIGN KEY (variant_id) REFERENCES product_variants(id) ON DELETE SET NULL`,
					`ALTER TABLE order_input_values ADD CONSTRAINT fk_order_input_values_field FOREIGN KEY (product_input_field_id) REFERENCES product_input_fields(id) ON DELETE SET NULL`,
					`ALTER TABLE product_input_fields ADD CONSTRAINT chk_product_input_field_values CHECK (key ~ '^[a-z][a-z0-9_]{0,63}$' AND input_type IN ('text', 'email', 'number', 'select', 'textarea') AND min_length >= 0 AND max_length BETWEEN 1 AND 2000 AND min_length <= max_length AND sort BETWEEN 0 AND 1000000 AND jsonb_typeof(options) = 'array')`,
					`ALTER TABLE order_input_values ADD CONSTRAINT chk_order_input_value_snapshot CHECK (input_type IN ('text', 'email', 'number', 'select', 'textarea') AND OCTET_LENGTH(value_cipher) > 0 AND OCTET_LENGTH(value_nonce) > 0 AND key ~ '^[a-z][a-z0-9_]{0,63}$')`,
				}
				for _, statement := range statements {
					if err := migration.Exec(statement).Error; err != nil {
						return err
					}
				}
				return nil
			},
		); err != nil {
			return err
		}
		if err := apply(
			"20260809_019_supplier_parameter_mapping",
			"ddc803290dcea3a7e5b80225e9051d757c98becca1d38db91a9e5bb744b4d2df",
			func(migration *gorm.DB) error {
				if err := migration.AutoMigrate(&model.ProductMapping{}); err != nil {
					return err
				}
				statements := []string{
					`UPDATE product_mappings SET parameter_mapping = '{}'::jsonb WHERE parameter_mapping IS NULL OR jsonb_typeof(parameter_mapping) <> 'object'`,
					`ALTER TABLE product_mappings ADD CONSTRAINT chk_product_mapping_parameters CHECK (jsonb_typeof(parameter_mapping) = 'object' AND OCTET_LENGTH(parameter_mapping::text) <= 4096)`,
				}
				for _, statement := range statements {
					if err := migration.Exec(statement).Error; err != nil {
						return err
					}
				}
				return nil
			},
		); err != nil {
			return err
		}
		if err := apply(
			"20260809_020_membership_lifecycle",
			"dbd196049829fb52f73cd68fb5b5be9620d0136b75986488b28dd17960aa84bf",
			func(migration *gorm.DB) error {
				// Install Source without a default first when upgrading an existing
				// deployment. Otherwise PostgreSQL would backfill historical manual
				// grants with the model's "automatic" default before we can classify
				// them, silently allowing the scheduler to overwrite operator grants.
				if !migration.Migrator().HasColumn(&model.UserLevelMembership{}, "Source") {
					if err := migration.Exec(`ALTER TABLE user_level_memberships ADD COLUMN source varchar(20)`).Error; err != nil {
						return err
					}
					if err := migration.Exec(`UPDATE user_level_memberships SET source = 'manual'`).Error; err != nil {
						return err
					}
				}
				if err := migration.AutoMigrate(&model.UserLevelMembership{}); err != nil {
					return err
				}
				statements := []string{
					`UPDATE user_level_memberships SET source = 'manual' WHERE source IS NULL OR source = ''`,
					`UPDATE user_level_memberships SET evaluated_at = COALESCE(NULLIF(evaluated_at, '0001-01-01 00:00:00+00'::timestamptz), granted_at, NOW())`,
					`ALTER TABLE user_level_memberships ALTER COLUMN source SET DEFAULT 'automatic'`,
					`ALTER TABLE user_level_memberships ALTER COLUMN source SET NOT NULL`,
					`ALTER TABLE user_level_memberships ALTER COLUMN evaluated_at SET NOT NULL`,
					`CREATE INDEX IF NOT EXISTS idx_user_level_memberships_due ON user_level_memberships (evaluated_at, expires_at)`,
					`ALTER TABLE user_level_memberships ADD CONSTRAINT chk_user_level_membership_source CHECK (source IN ('automatic', 'manual'))`,
					`ALTER TABLE user_level_memberships ADD CONSTRAINT chk_user_level_membership_expiry CHECK (expires_at IS NULL OR expires_at > granted_at)`,
					`ALTER TABLE user_level_memberships ADD CONSTRAINT fk_user_level_membership_granted_by FOREIGN KEY (granted_by) REFERENCES admins(id) ON DELETE SET NULL`,
				}
				for _, statement := range statements {
					if err := migration.Exec(statement).Error; err != nil {
						return err
					}
				}
				return nil
			},
		); err != nil {
			return err
		}
		if err := apply(
			"20260809_021_supplier_balance_sync",
			"7f6714086625586695a69783348f549107411cff1f0221a36bc97cbe51125425",
			func(migration *gorm.DB) error {
				if err := migration.AutoMigrate(&model.Supplier{}); err != nil {
					return err
				}
				return migration.Exec(`UPDATE suppliers SET balance_currency = 'CNY' WHERE balance_currency IS NULL OR balance_currency = ''`).Error
			},
		); err != nil {
			return err
		}
		if err := apply(
			"20260809_022_reseller_business_policy",
			"1453400d6f19d143bb5dfce569c054926f0a5bea8caca157013838971242a76f",
			func(migration *gorm.DB) error {
				if err := migration.AutoMigrate(&model.ResellerWholesaleTier{}, &model.ResellerCreditEvent{}); err != nil {
					return err
				}

				// A non-CNY or negative supplier balance is not a value this
				// application can safely interpret. Balance is an upstream cache,
				// so invalidate such legacy snapshots instead of relabelling them.
				statements := []string{
					`UPDATE suppliers SET balance_currency = 'CNY' WHERE balance_currency IS NULL OR BTRIM(balance_currency) = ''`,
					`UPDATE suppliers SET balance = 0, balance_currency = 'CNY', balance_synced_at = NULL WHERE balance < 0 OR UPPER(BTRIM(balance_currency)) <> 'CNY'`,
					`ALTER TABLE suppliers ADD CONSTRAINT chk_supplier_balance_nonnegative CHECK (balance >= 0)`,
					`ALTER TABLE suppliers ADD CONSTRAINT chk_supplier_balance_currency CHECK (balance_currency = 'CNY')`,
					`ALTER TABLE reseller_profiles ADD CONSTRAINT chk_reseller_profile_credit_limit CHECK (credit_limit >= 0)`,
					`ALTER TABLE reseller_profiles ADD CONSTRAINT chk_reseller_profile_wholesale_level CHECK (wholesale_level BETWEEN 0 AND 10)`,
					`ALTER TABLE reseller_wholesale_tiers ADD CONSTRAINT chk_reseller_wholesale_tier_level CHECK (level BETWEEN 0 AND 10)`,
					`ALTER TABLE reseller_wholesale_tiers ADD CONSTRAINT chk_reseller_wholesale_tier_discount CHECK (discount_basis_point BETWEEN 0 AND 10000)`,
					`ALTER TABLE reseller_credit_events ADD CONSTRAINT chk_reseller_credit_event_snapshot CHECK (frozen >= 0 AND exposure >= 0 AND credit_limit >= 0)`,
				}
				for _, statement := range statements {
					if err := migration.Exec(statement).Error; err != nil {
						return err
					}
				}

				var levels []int
				if err := migration.Model(&model.ResellerProfile{}).Distinct("wholesale_level").Pluck("wholesale_level", &levels).Error; err != nil {
					return err
				}
				levels = append(levels, 0)
				for _, level := range levels {
					var count int64
					if err := migration.Model(&model.ResellerWholesaleTier{}).Unscoped().Where("level = ?", level).Count(&count).Error; err != nil {
						return err
					}
					if count > 0 {
						continue
					}
					name := "基础结算"
					if level > 0 {
						// Existing numeric levels never had a discount policy. Keep
						// their behaviour unchanged and make that fact explicit.
						name = fmt.Sprintf("历史等级 L%d（零折扣）", level)
					}
					if err := migration.Create(&model.ResellerWholesaleTier{Level: level, Name: name, DiscountBasisPoint: 0, Enabled: true}).Error; err != nil {
						return err
					}
				}
				return nil
			},
		); err != nil {
			return err
		}
		if err := apply(
			"20260809_023_payment_procurement_integrity",
			"6eda096bb7012c6a6d12b42d6d12c5ff6e0265aecc0b7b4fd67ce3dac303d4d0",
			func(migration *gorm.DB) error {
				if err := migration.AutoMigrate(&model.PaymentIntent{}, &model.ProcurementOrder{}, &model.APICredential{}); err != nil {
					return err
				}
				statements := []string{
					// Old installations may have already recorded migration 003 before
					// these guards existed. Re-apply them under a new immutable version.
					`UPDATE api_credentials
					 SET status = 'suspended', revoked_at = NULL, updated_at = NOW()
					 WHERE owner_id IS NULL AND status IN ('pending', 'active')`,
					`WITH ranked AS (
						SELECT id, ROW_NUMBER() OVER (
							PARTITION BY order_id
							ORDER BY
								CASE WHEN status = 'pending' AND expires_at > NOW() THEN 0 WHEN status = 'creating' THEN 1 ELSE 2 END,
								updated_at DESC, id
						) AS duplicate_number
						FROM payment_intents
						WHERE deleted_at IS NULL AND status IN ('creating', 'pending')
					) UPDATE payment_intents AS intent
					  SET status = 'expired', updated_at = NOW()
					  FROM ranked
					  WHERE intent.id = ranked.id AND ranked.duplicate_number > 1`,
					`UPDATE payment_intents
					 SET succeeded_at = COALESCE(succeeded_at, updated_at, created_at, NOW())
					 WHERE status IN ('succeeded', 'requires_refund', 'partially_refunded', 'refunded')
					   AND succeeded_at IS NULL`,
					`DO $$
					 BEGIN
						IF EXISTS (
							SELECT 1 FROM payment_intents
							WHERE deleted_at IS NULL AND COALESCE(provider_trade_no, '') <> ''
							GROUP BY channel_id, provider_trade_no HAVING COUNT(*) > 1
						) THEN
							RAISE EXCEPTION 'duplicate provider trade numbers require operator reconciliation before migration';
						END IF;
						IF EXISTS (
							SELECT 1 FROM procurement_orders GROUP BY order_item_id HAVING COUNT(*) > 1
						) THEN
							RAISE EXCEPTION 'duplicate procurement orders require operator reconciliation before migration';
						END IF;
					 END $$`,
					`DROP INDEX IF EXISTS idx_payment_intents_active_order`,
					`CREATE UNIQUE INDEX idx_payment_intents_active_order ON payment_intents (order_id) WHERE deleted_at IS NULL AND status IN ('creating', 'pending')`,
					`DROP INDEX IF EXISTS idx_payment_channel_trade`,
					`CREATE UNIQUE INDEX idx_payment_channel_trade ON payment_intents (channel_id, provider_trade_no) WHERE deleted_at IS NULL AND COALESCE(provider_trade_no, '') <> ''`,
					`DROP INDEX IF EXISTS idx_procurement_order_item`,
					`CREATE UNIQUE INDEX idx_procurement_order_item ON procurement_orders (order_item_id)`,
					`ALTER TABLE api_credentials ADD CONSTRAINT chk_api_credentials_live_owner CHECK (status NOT IN ('pending', 'active') OR (owner_id IS NOT NULL AND COALESCE(owner_type, '') <> ''))`,
					`ALTER TABLE payment_intents ADD CONSTRAINT chk_payment_intent_state CHECK (
						amount > 0 AND currency = 'CNY'
						AND status IN ('creating', 'pending', 'succeeded', 'requires_refund', 'partially_refunded', 'refunded', 'failed', 'expired', 'cancelled')
						AND (status <> 'creating' OR (COALESCE(provider_trade_no, '') = '' AND succeeded_at IS NULL))
						AND (status <> 'pending' OR COALESCE(provider_trade_no, '') <> '')
						AND (status NOT IN ('succeeded', 'requires_refund', 'partially_refunded', 'refunded') OR (COALESCE(provider_trade_no, '') <> '' AND succeeded_at IS NOT NULL))
					)`,
				}
				for _, statement := range statements {
					if err := migration.Exec(statement).Error; err != nil {
						return err
					}
				}
				return nil
			},
		); err != nil {
			return err
		}
		if err := apply(
			"20260809_024_catalog_media_supplier_sync",
			"d43c3c17e4198c87b006948c59a23f00c3ebb974bbeac149dddf8be9e155dad2",
			func(migration *gorm.DB) error {
				if err := migration.AutoMigrate(
					&model.Category{}, &model.Product{}, &model.Supplier{}, &model.ProductMapping{},
					&model.CatalogMedia{}, &model.SupplierSyncPolicy{}, &model.SupplierCategory{},
					&model.SupplierCatalogProduct{}, &model.SupplierCategoryMapping{},
					&model.SupplierSyncRun{}, &model.SupplierSyncChange{},
					&model.ResellerCategoryRule{}, &model.ResellerProductPresentation{},
				); err != nil {
					return err
				}
				statements := []string{
					`CREATE UNIQUE INDEX IF NOT EXISTS idx_catalog_media_owner_position ON catalog_media (owner_type, owner_id, role, sort) WHERE deleted_at IS NULL`,
					`CREATE UNIQUE INDEX IF NOT EXISTS idx_supplier_categories_external ON supplier_categories (supplier_id, external_id) WHERE deleted_at IS NULL`,
					`CREATE UNIQUE INDEX IF NOT EXISTS idx_supplier_catalog_products_external ON supplier_catalog_products (supplier_id, external_id) WHERE deleted_at IS NULL`,
					`CREATE UNIQUE INDEX IF NOT EXISTS idx_supplier_category_mappings_external ON supplier_category_mappings (supplier_id, external_category_id) WHERE deleted_at IS NULL`,
					`CREATE UNIQUE INDEX IF NOT EXISTS idx_reseller_category_rules_catalog ON reseller_category_rules (reseller_id, category_id) WHERE deleted_at IS NULL`,
					`CREATE UNIQUE INDEX IF NOT EXISTS idx_reseller_product_presentations_catalog ON reseller_product_presentations (reseller_id, product_id) WHERE deleted_at IS NULL`,
					`ALTER TABLE categories DROP CONSTRAINT IF EXISTS chk_categories_parent_not_self`,
					`ALTER TABLE categories ADD CONSTRAINT chk_categories_parent_not_self CHECK (parent_id IS NULL OR parent_id <> id)`,
					`ALTER TABLE catalog_media DROP CONSTRAINT IF EXISTS chk_catalog_media_values`,
					`ALTER TABLE catalog_media ADD CONSTRAINT chk_catalog_media_values CHECK (owner_type IN ('category', 'product') AND role IN ('cover', 'gallery', 'detail') AND sort >= 0 AND mirror_status IN ('pending', 'ready', 'failed', 'quarantined'))`,
					`ALTER TABLE supplier_sync_policies DROP CONSTRAINT IF EXISTS chk_supplier_sync_policy_values`,
					`ALTER TABLE supplier_sync_policies ADD CONSTRAINT chk_supplier_sync_policy_values CHECK (missing_product_action IN ('keep', 'unpublish', 'disable_mapping'))`,
					`ALTER TABLE supplier_categories DROP CONSTRAINT IF EXISTS chk_supplier_category_values`,
					`ALTER TABLE supplier_categories ADD CONSTRAINT chk_supplier_category_values CHECK (sort >= 0 AND status IN ('active', 'inactive', 'missing'))`,
					`ALTER TABLE supplier_catalog_products DROP CONSTRAINT IF EXISTS chk_supplier_catalog_product_values`,
					`ALTER TABLE supplier_catalog_products ADD CONSTRAINT chk_supplier_catalog_product_values CHECK (price >= 0 AND stock >= 0 AND minimum >= 1 AND maximum >= 0 AND status IN ('active', 'inactive', 'missing'))`,
					`ALTER TABLE supplier_category_mappings DROP CONSTRAINT IF EXISTS chk_supplier_category_mapping_values`,
					`ALTER TABLE supplier_category_mappings ADD CONSTRAINT chk_supplier_category_mapping_values CHECK (price_mode IN ('fixed_markup', 'percent_markup', 'exchange_rate') AND markup_basis_point BETWEEN -10000 AND 1000000)`,
					`ALTER TABLE supplier_sync_runs DROP CONSTRAINT IF EXISTS chk_supplier_sync_run_values`,
					`ALTER TABLE supplier_sync_runs ADD CONSTRAINT chk_supplier_sync_run_values CHECK (trigger IN ('manual', 'schedule', 'webhook', 'recovery') AND status IN ('queued', 'running', 'succeeded', 'partial', 'failed', 'cancelled'))`,
					`ALTER TABLE supplier_sync_changes DROP CONSTRAINT IF EXISTS chk_supplier_sync_change_values`,
					`ALTER TABLE supplier_sync_changes ADD CONSTRAINT chk_supplier_sync_change_values CHECK (entity_type IN ('category', 'product', 'variant', 'media') AND action IN ('discover', 'create', 'update', 'unpublish', 'skip', 'error'))`,
					`ALTER TABLE suppliers DROP CONSTRAINT IF EXISTS chk_supplier_sync_interval`,
					`ALTER TABLE suppliers ADD CONSTRAINT chk_supplier_sync_interval CHECK (sync_interval_minutes BETWEEN 5 AND 10080)`,
				}
				for _, statement := range statements {
					if err := migration.Exec(statement).Error; err != nil {
						return err
					}
				}
				return nil
			},
		); err != nil {
			return err
		}
		if err := apply(
			"20260809_025_multi_currency_fx",
			"be7950665f4b61464460518fe9e4e2fe4d275b7e6d13cd5438a6583545b11b0e",
			func(migration *gorm.DB) error {
				if err := migration.AutoMigrate(
					&model.CurrencyDefinition{}, &model.FXProviderConfig{}, &model.FXRateObservation{},
					&model.FXManualRate{}, &model.FXRateSnapshot{}, &model.Product{}, &model.Order{},
					&model.OrderItem{}, &model.Supplier{}, &model.SupplierCatalogProduct{},
					&model.SupplierSyncRun{}, &model.ProductMapping{}, &model.ProcurementOrder{},
					&model.Refund{}, &model.PaymentIntent{}, &model.PaymentTransaction{}, &model.PaymentChannel{}, &model.WalletAccount{}, &model.RechargeOrder{}, &model.RechargeTransaction{},
					&model.GiftCardBatch{}, &model.GiftCard{},
					&model.ReconciliationBatch{}, &model.ReconciliationItem{},
					&model.ResellerWithdrawal{}, &model.AffiliateBalance{},
					&model.AffiliateCommission{}, &model.AffiliateWithdrawal{},
					&model.Promotion{}, &model.Coupon{}, &model.ResellerProductRule{},
					&model.ResellerCreditPolicy{}, &model.ResellerCreditEvent{},
					&model.MemberLevel{},
				); err != nil {
					return err
				}
				currencies := []model.CurrencyDefinition{
					{Code: "CNY", NumericCode: "156", Name: "Chinese Yuan", Symbol: "¥", MinorUnit: 2, Enabled: true, Settlement: true, DisplaySort: 100},
					{Code: "USD", NumericCode: "840", Name: "US Dollar", Symbol: "$", MinorUnit: 2, Enabled: true, DisplaySort: 90},
					{Code: "EUR", NumericCode: "978", Name: "Euro", Symbol: "€", MinorUnit: 2, Enabled: true, DisplaySort: 80},
					{Code: "GBP", NumericCode: "826", Name: "Pound Sterling", Symbol: "£", MinorUnit: 2, Enabled: true, DisplaySort: 70},
					{Code: "JPY", NumericCode: "392", Name: "Japanese Yen", Symbol: "¥", MinorUnit: 0, Enabled: true, DisplaySort: 60},
					{Code: "KRW", NumericCode: "410", Name: "South Korean Won", Symbol: "₩", MinorUnit: 0, Enabled: true, DisplaySort: 50},
					{Code: "HKD", NumericCode: "344", Name: "Hong Kong Dollar", Symbol: "HK$", MinorUnit: 2, Enabled: true, DisplaySort: 40},
					{Code: "TWD", NumericCode: "901", Name: "New Taiwan Dollar", Symbol: "NT$", MinorUnit: 2, Enabled: true, DisplaySort: 30},
					{Code: "VND", NumericCode: "704", Name: "Vietnamese Dong", Symbol: "₫", MinorUnit: 0, Enabled: true, DisplaySort: 20},
					{Code: "THB", NumericCode: "764", Name: "Thai Baht", Symbol: "฿", MinorUnit: 2, Enabled: true, DisplaySort: 10},
					{Code: "SGD", NumericCode: "702", Name: "Singapore Dollar", Symbol: "S$", MinorUnit: 2, Enabled: true},
					{Code: "MYR", NumericCode: "458", Name: "Malaysian Ringgit", Symbol: "RM", MinorUnit: 2, Enabled: true},
					{Code: "IDR", NumericCode: "360", Name: "Indonesian Rupiah", Symbol: "Rp", MinorUnit: 2, Enabled: true},
					{Code: "PHP", NumericCode: "608", Name: "Philippine Peso", Symbol: "₱", MinorUnit: 2, Enabled: true},
					{Code: "AUD", NumericCode: "036", Name: "Australian Dollar", Symbol: "A$", MinorUnit: 2, Enabled: true},
					{Code: "CAD", NumericCode: "124", Name: "Canadian Dollar", Symbol: "C$", MinorUnit: 2, Enabled: true},
					{Code: "CHF", NumericCode: "756", Name: "Swiss Franc", Symbol: "CHF", MinorUnit: 2, Enabled: true},
					{Code: "INR", NumericCode: "356", Name: "Indian Rupee", Symbol: "₹", MinorUnit: 2, Enabled: true},
					{Code: "RUB", NumericCode: "643", Name: "Russian Ruble", Symbol: "₽", MinorUnit: 2, Enabled: true},
				}
				for _, currency := range currencies {
					if err := migration.Where("code = ?", currency.Code).Assign(map[string]any{"numeric_code": currency.NumericCode, "name": currency.Name, "symbol": currency.Symbol, "minor_unit": currency.MinorUnit, "enabled": currency.Enabled, "display_sort": currency.DisplaySort}).FirstOrCreate(&currency).Error; err != nil {
						return err
					}
				}
				providerKeys := []string{"ECB", "BOC", "BOE", "BOJ", "CNB", "NBP", "RBA", "RB", "NB", "MAS", "HKMA", "IMF", "FRED", "BCB", "BANXICO", "BOT", "BNM", "NBU", "CBR", "SARB", "TCMB", "BI", "BDI", "BSP", "MNB"}
				providers := []model.FXProviderConfig{{Code: "frankfurter-blended", Name: "Frankfurter blended official rates", Driver: "frankfurter-v2", BaseURL: "https://api.frankfurter.dev", Priority: 10, Enabled: true, TimeoutSeconds: 8}}
				for index, key := range providerKeys {
					providers = append(providers, model.FXProviderConfig{Code: "frankfurter-" + strings.ToLower(key), Name: "Frankfurter official source " + key, Driver: "frankfurter-v2", ProviderKey: key, BaseURL: "https://api.frankfurter.dev", Priority: 20 + index, Enabled: true, TimeoutSeconds: 8})
				}
				providers = append(providers, model.FXProviderConfig{Code: "exchangerate-api-open", Name: "ExchangeRate-API open access", Driver: "exchangerate-api-open", BaseURL: "https://open.er-api.com", Priority: 80, Enabled: true, TimeoutSeconds: 8})
				for _, provider := range providers {
					if err := migration.Where("code = ?", provider.Code).Assign(map[string]any{"name": provider.Name, "driver": provider.Driver, "provider_key": provider.ProviderKey, "base_url": provider.BaseURL, "priority": provider.Priority, "timeout_seconds": provider.TimeoutSeconds}).FirstOrCreate(&provider).Error; err != nil {
						return err
					}
				}
				var affiliateProfiles []model.AffiliateProfile
				if err := migration.Find(&affiliateProfiles).Error; err != nil {
					return err
				}
				for _, profile := range affiliateProfiles {
					balance := model.AffiliateBalance{
						AffiliateID: profile.ID, Currency: "CNY", TotalCommission: profile.TotalCommission,
						AvailableCommission: profile.AvailableCommission, FrozenCommission: profile.FrozenCommission,
					}
					if err := migration.Where("affiliate_id = ? AND currency = ?", profile.ID, "CNY").FirstOrCreate(&balance).Error; err != nil {
						return err
					}
				}
				var resellerProfiles []model.ResellerProfile
				if err := migration.Find(&resellerProfiles).Error; err != nil {
					return err
				}
				for _, profile := range resellerProfiles {
					policy := model.ResellerCreditPolicy{ResellerID: profile.ID, Currency: "CNY", CreditLimit: profile.CreditLimit}
					if err := migration.Where("reseller_id = ? AND currency = ?", profile.ID, "CNY").FirstOrCreate(&policy).Error; err != nil {
						return err
					}
				}
				statements := []string{
					`ALTER TABLE suppliers DROP CONSTRAINT IF EXISTS chk_supplier_balance_currency`,
					`ALTER TABLE payment_intents DROP CONSTRAINT IF EXISTS chk_payment_intent_state`,
					`ALTER TABLE payment_transactions DROP CONSTRAINT IF EXISTS chk_payment_transaction_currency`,
					`ALTER TABLE recharge_orders DROP CONSTRAINT IF EXISTS chk_recharge_order_values`,
					`ALTER TABLE gift_card_batches DROP CONSTRAINT IF EXISTS chk_gift_card_batch_values`,
					`ALTER TABLE gift_cards DROP CONSTRAINT IF EXISTS chk_gift_card_values`,
					`UPDATE payment_transactions pt SET currency = pi.currency FROM payment_intents pi WHERE pi.id = pt.payment_intent_id AND pt.currency IS DISTINCT FROM pi.currency`,
					`ALTER TABLE suppliers ADD CONSTRAINT chk_supplier_currency_values CHECK (balance_currency ~ '^[A-Z]{3}$' AND price_currency ~ '^[A-Z]{3}$' AND price_minor_unit BETWEEN 0 AND 6 AND currency_mode IN ('auto', 'manual'))`,
					`ALTER TABLE products ADD CONSTRAINT chk_product_currency CHECK (currency ~ '^[A-Z]{3}$')`,
					`ALTER TABLE orders ADD CONSTRAINT chk_order_currency CHECK (currency ~ '^[A-Z]{3}$')`,
					`ALTER TABLE order_items ADD CONSTRAINT chk_order_item_currency CHECK (currency ~ '^[A-Z]{3}$' AND (upstream_currency = '' OR upstream_currency ~ '^[A-Z]{3}$'))`,
					`ALTER TABLE procurement_orders ADD CONSTRAINT chk_procurement_currency CHECK (cost_currency ~ '^[A-Z]{3}$' AND (upstream_currency = '' OR upstream_currency ~ '^[A-Z]{3}$'))`,
					`ALTER TABLE refunds ADD CONSTRAINT chk_refund_currency CHECK (currency ~ '^[A-Z]{3}$')`,
					`ALTER TABLE wallet_accounts ADD CONSTRAINT chk_wallet_currency CHECK (currency ~ '^[A-Z]{3}$')`,
					`ALTER TABLE payment_intents ADD CONSTRAINT chk_payment_intent_state CHECK (amount > 0 AND currency ~ '^[A-Z]{3}$' AND status IN ('creating', 'pending', 'succeeded', 'requires_refund', 'partially_refunded', 'refunded', 'failed', 'expired', 'cancelled') AND (status <> 'creating' OR (COALESCE(provider_trade_no, '') = '' AND succeeded_at IS NULL)) AND (status <> 'pending' OR COALESCE(provider_trade_no, '') <> '') AND (status NOT IN ('succeeded', 'requires_refund', 'partially_refunded', 'refunded') OR (COALESCE(provider_trade_no, '') <> '' AND succeeded_at IS NOT NULL)))`,
					`ALTER TABLE payment_transactions ADD CONSTRAINT chk_payment_transaction_currency CHECK (currency ~ '^[A-Z]{3}$')`,
					`ALTER TABLE recharge_orders ADD CONSTRAINT chk_recharge_order_values CHECK (amount > 0 AND bonus >= 0 AND currency ~ '^[A-Z]{3}$' AND status IN ('creating', 'pending', 'succeeded', 'failed', 'expired', 'cancelled'))`,
					`ALTER TABLE gift_card_batches ADD CONSTRAINT chk_gift_card_batch_values CHECK (quantity > 0 AND card_value > 0 AND currency ~ '^[A-Z]{3}$' AND status IN ('active', 'disabled'))`,
					`ALTER TABLE gift_cards ADD CONSTRAINT chk_gift_card_values CHECK (initial_balance > 0 AND balance >= 0 AND balance <= initial_balance AND currency ~ '^[A-Z]{3}$' AND status IN ('active', 'disabled', 'redeemed', 'expired'))`,
					`ALTER TABLE reconciliation_batches ADD CONSTRAINT chk_reconciliation_batch_currency CHECK (currency ~ '^[A-Z]{3}$')`,
					`ALTER TABLE reconciliation_items ADD CONSTRAINT chk_reconciliation_item_currency CHECK (currency ~ '^[A-Z]{3}$')`,
					`ALTER TABLE reseller_withdrawals ADD CONSTRAINT chk_reseller_withdrawal_currency CHECK (currency ~ '^[A-Z]{3}$')`,
					`ALTER TABLE recharge_transactions ADD CONSTRAINT chk_recharge_transaction_currency CHECK (currency ~ '^[A-Z]{3}$')`,
					`ALTER TABLE affiliate_balances ADD CONSTRAINT chk_affiliate_balance_currency CHECK (currency ~ '^[A-Z]{3}$' AND total_commission >= 0 AND frozen_commission >= 0)`,
					`ALTER TABLE affiliate_commissions ADD CONSTRAINT chk_affiliate_commission_currency CHECK (currency ~ '^[A-Z]{3}$')`,
					`ALTER TABLE affiliate_withdrawals ADD CONSTRAINT chk_affiliate_withdrawal_currency CHECK (currency ~ '^[A-Z]{3}$')`,
					`ALTER TABLE promotions ADD CONSTRAINT chk_promotion_currency CHECK (currency ~ '^[A-Z]{3}$')`,
					`ALTER TABLE coupons ADD CONSTRAINT chk_coupon_currency CHECK (currency ~ '^[A-Z]{3}$')`,
					`ALTER TABLE reseller_product_rules ADD CONSTRAINT chk_reseller_product_rule_currency CHECK (currency ~ '^[A-Z]{3}$')`,
					`ALTER TABLE reseller_credit_policies ADD CONSTRAINT chk_reseller_credit_policy_values CHECK (currency ~ '^[A-Z]{3}$' AND credit_limit >= 0)`,
					`ALTER TABLE reseller_credit_events ADD CONSTRAINT chk_reseller_credit_event_currency CHECK (currency ~ '^[A-Z]{3}$')`,
					`ALTER TABLE member_levels ADD CONSTRAINT chk_member_level_currency CHECK (currency ~ '^[A-Z]{3}$')`,
					`ALTER TABLE product_mappings ADD CONSTRAINT chk_product_mapping_fixed_currency CHECK (fixed_price_currency ~ '^[A-Z]{3}$')`,
					`ALTER TABLE currency_definitions ADD CONSTRAINT chk_currency_definition_values CHECK (code ~ '^[A-Z]{3}$' AND (numeric_code = '' OR numeric_code ~ '^[0-9]{3}$') AND minor_unit BETWEEN 0 AND 6)`,
					`ALTER TABLE fx_provider_configs ADD CONSTRAINT chk_fx_provider_values CHECK (priority BETWEEN 0 AND 100000 AND timeout_seconds BETWEEN 1 AND 30 AND failure_count >= 0)`,
					`ALTER TABLE fx_rate_observations ADD CONSTRAINT chk_fx_observation_values CHECK (base_code ~ '^[A-Z]{3}$' AND quote_code ~ '^[A-Z]{3}$' AND base_code <> quote_code AND rate > 0)`,
					`ALTER TABLE fx_manual_rates ADD CONSTRAINT chk_fx_manual_rate_values CHECK (base_code ~ '^[A-Z]{3}$' AND quote_code ~ '^[A-Z]{3}$' AND base_code <> quote_code AND rate > 0 AND (valid_to IS NULL OR valid_to > valid_from))`,
					`ALTER TABLE fx_rate_snapshots ADD CONSTRAINT chk_fx_snapshot_values CHECK (base_code ~ '^[A-Z]{3}$' AND quote_code ~ '^[A-Z]{3}$' AND rate > 0 AND source_tier IN ('live', 'manual', 'cached', 'system') AND expires_at >= selected_at AND stale_after >= expires_at AND consensus_count >= 1)`,
					`CREATE UNIQUE INDEX IF NOT EXISTS idx_fx_manual_rate_version ON fx_manual_rates (base_code, quote_code, valid_from) WHERE deleted_at IS NULL`,
					`CREATE INDEX IF NOT EXISTS idx_fx_snapshot_pair_fresh ON fx_rate_snapshots (base_code, quote_code, selected_at DESC) WHERE deleted_at IS NULL`,
					`CREATE INDEX IF NOT EXISTS idx_fx_observation_pair_time ON fx_rate_observations (base_code, quote_code, fetched_at DESC) WHERE deleted_at IS NULL`,
				}
				for _, statement := range statements {
					if err := migration.Exec(statement).Error; err != nil {
						return err
					}
				}
				return nil
			},
		); err != nil {
			return err
		}
		if err := apply(
			"20260809_026_storefront_idempotency_user_sessions",
			"f2424ba6e5f9d84847ec83f5f2065425503323de90eea3abfa67a782509551bb",
			func(migration *gorm.DB) error {
				if err := migration.AutoMigrate(&model.StorefrontOrderRequest{}, &model.User{}); err != nil {
					return err
				}
				statements := []string{
					`ALTER TABLE storefront_order_requests DROP CONSTRAINT IF EXISTS chk_storefront_order_request_hashes`,
					`ALTER TABLE storefront_order_requests ADD CONSTRAINT chk_storefront_order_request_hashes CHECK (idempotency_hash ~ '^[0-9a-f]{64}$' AND request_hash ~ '^[0-9a-f]{64}$' AND length(client_order_no) <= 100)`,
					`CREATE UNIQUE INDEX IF NOT EXISTS idx_storefront_order_request_hash ON storefront_order_requests (idempotency_hash) WHERE deleted_at IS NULL`,
				}
				for _, statement := range statements {
					if err := migration.Exec(statement).Error; err != nil {
						return err
					}
				}
				return nil
			},
		); err != nil {
			return err
		}
		if err := apply(
			"20260809_027_payment_settlement_currency",
			"5df783ddc1b8135b4ff8a851ee75ea537740044174988899783b9a947d55ba9b",
			func(migration *gorm.DB) error {
				if err := migration.AutoMigrate(&model.PaymentChannel{}, &model.PaymentIntent{}, &model.RechargeOrder{}, &model.Refund{}); err != nil {
					return err
				}
				statements := []string{
					`UPDATE payment_channels SET settlement_currency = CASE WHEN supported_currencies @> '["CNY"]'::jsonb THEN 'CNY' ELSE COALESCE(NULLIF(supported_currencies->>0, ''), 'CNY') END`,
					`UPDATE payment_intents SET order_amount = amount WHERE order_amount = 0`,
					`UPDATE payment_intents SET order_currency = currency WHERE order_currency = ''`,
					`UPDATE recharge_orders SET credit_amount = amount WHERE credit_amount = 0`,
					`UPDATE recharge_orders SET credit_currency = currency WHERE credit_currency = ''`,
					`UPDATE refunds SET order_amount = amount WHERE order_amount = 0`,
					`UPDATE refunds SET order_currency = currency WHERE order_currency = ''`,
					`ALTER TABLE payment_channels DROP CONSTRAINT IF EXISTS chk_payment_channel_settlement_currency`,
					`ALTER TABLE payment_channels ADD CONSTRAINT chk_payment_channel_settlement_currency CHECK (settlement_currency ~ '^[A-Z]{3}$' AND supported_currencies @> jsonb_build_array(settlement_currency))`,
					`ALTER TABLE payment_intents DROP CONSTRAINT IF EXISTS chk_payment_intent_currency_chain`,
					`ALTER TABLE payment_intents ADD CONSTRAINT chk_payment_intent_currency_chain CHECK (currency ~ '^[A-Z]{3}$' AND order_currency ~ '^[A-Z]{3}$' AND amount > 0 AND order_amount > 0 AND ((currency = order_currency AND fx_snapshot_id IS NULL) OR currency <> order_currency))`,
					`ALTER TABLE recharge_orders DROP CONSTRAINT IF EXISTS chk_recharge_currency_chain`,
					`ALTER TABLE recharge_orders ADD CONSTRAINT chk_recharge_currency_chain CHECK (currency ~ '^[A-Z]{3}$' AND credit_currency ~ '^[A-Z]{3}$' AND amount > 0 AND credit_amount > 0 AND ((currency = credit_currency AND fx_snapshot_id IS NULL) OR currency <> credit_currency))`,
					`ALTER TABLE refunds DROP CONSTRAINT IF EXISTS chk_refund_currency_chain`,
					`ALTER TABLE refunds ADD CONSTRAINT chk_refund_currency_chain CHECK (currency ~ '^[A-Z]{3}$' AND order_currency ~ '^[A-Z]{3}$' AND amount > 0 AND order_amount > 0)`,
				}
				for _, statement := range statements {
					if err := migration.Exec(statement).Error; err != nil {
						return err
					}
				}
				return nil
			},
		); err != nil {
			return err
		}
		if err := apply(
			"20260810_028_user_avatar",
			"c4f9a7ddf1d6a4b7f35f94b7cc2b30e6d7c8a2e9b1f9c6d2a4b8e7f1c0d3a5b6",
			func(migration *gorm.DB) error { return migration.AutoMigrate(&model.User{}) },
		); err != nil {
			return err
		}
		if err := apply(
			"20260810_029_notification_automation",
			"c981dc7b12c08f5086a26b1d4d76ade91d7d3be3da16d4f095a179687d61b4ce",
			func(migration *gorm.DB) error {
				if err := migration.AutoMigrate(&model.NotificationConnector{}, &model.NotificationSubscription{}); err != nil {
					return err
				}
				return migration.Exec(`ALTER TABLE notification_connectors ADD CONSTRAINT chk_notification_connector_channel CHECK (channel IN ('email', 'telegram', 'wecom'))`).Error
			},
		); err != nil {
			return err
		}
		if err := apply(
			"20260810_030_notification_audiences",
			"2a5c0dc5f1e7bdb2b6f71a9a8f10e74b05d9acff83e7b9a5cd1e6674ea1c913b",
			func(migration *gorm.DB) error {
				if err := migration.AutoMigrate(&model.NotificationTemplate{}, &model.NotificationSubscription{}, &model.UserNotification{}); err != nil {
					return err
				}
				statements := []string{
					`UPDATE notification_templates SET audience = 'admin' WHERE audience IS NULL OR audience = ''`,
					`UPDATE notification_subscriptions SET audience = 'admin' WHERE audience IS NULL OR audience = ''`,
					`ALTER TABLE notification_templates DROP CONSTRAINT IF EXISTS chk_notification_template_audience`,
					`ALTER TABLE notification_templates ADD CONSTRAINT chk_notification_template_audience CHECK (audience IN ('admin','user'))`,
					`ALTER TABLE notification_subscriptions DROP CONSTRAINT IF EXISTS chk_notification_subscription_audience`,
					`ALTER TABLE notification_subscriptions ADD CONSTRAINT chk_notification_subscription_audience CHECK (audience IN ('admin','user'))`,
					`DROP INDEX IF EXISTS idx_notification_subscription_destination`,
					`CREATE UNIQUE INDEX IF NOT EXISTS idx_notification_subscription_destination ON notification_subscriptions (audience, event_code, channel, recipient) WHERE deleted_at IS NULL`,
				}
				for _, statement := range statements {
					if err := migration.Exec(statement).Error; err != nil {
						return err
					}
				}
				return seedNotificationAudienceDefaults(migration)
			},
		); err != nil {
			return err
		}
		if err := apply(
			"20260810_031_notification_admin_defaults",
			"8f0b6f7c2a4d9e1b3c5f7a9d2e4b6c8d0f1a3e5c7b9d2f4a6c8e0b1d3f5a7c9e",
			func(migration *gorm.DB) error { return seedNotificationAdminSubscriptions(migration) },
		); err != nil {
			return err
		}
		if err := apply(
			"20260810_032_localized_notification_templates",
			"76a3c50ad38c833ab2c41b93d05d58b331dc9b062247d8c2e5be95e82380cb17",
			func(migration *gorm.DB) error {
				if err := migration.AutoMigrate(&model.User{}, &model.NotificationTemplate{}, &model.NotificationSubscription{}); err != nil {
					return err
				}
				statements := []string{
					`UPDATE users SET preferred_locale = 'zh-CN' WHERE preferred_locale IS NULL OR preferred_locale = ''`,
					`ALTER TABLE users DROP CONSTRAINT IF EXISTS chk_user_preferred_locale`,
					`ALTER TABLE users ADD CONSTRAINT chk_user_preferred_locale CHECK (preferred_locale IN ('zh-CN','zh-TW','en','vi','ru','ja','ko','th'))`,
					`DROP INDEX IF EXISTS idx_notification_subscription_destination`,
					`CREATE UNIQUE INDEX idx_notification_subscription_destination ON notification_subscriptions (audience, event_code, channel, recipient, locale) WHERE deleted_at IS NULL`,
				}
				for _, statement := range statements {
					if err := migration.Exec(statement).Error; err != nil {
						return err
					}
				}
				return seedDetailedNotificationTemplates(migration)
			},
		); err != nil {
			return err
		}
		if err := apply(
			"20260810_033_notification_event_code_cleanup",
			"301ef0ff7d6f8f0eecaf6d0db4e134be99f4a342f91c90c1f6d75ac10b3a4198",
			func(migration *gorm.DB) error {
				return migration.Where("audience = ? AND recipient = ? AND event_code IN ?", "admin", "all", []string{"inventory.low.stock", "inventory.out.of.stock", "security.high.risk"}).Delete(&model.NotificationSubscription{}).Error
			},
		); err != nil {
			return err
		}
		if err := apply(
			"20260810_034_notification_channel_templates_v2",
			"ccbd877b5e93dc0b856111aadab82ec2e37ddd741e1421f2d8adab508fb7ae29",
			func(migration *gorm.DB) error { return seedDetailedNotificationTemplates(migration) },
		); err != nil {
			return err
		}
		if err := apply(
			"20260810_035_supplier_order_binding_v1",
			"3a04703a0b98e0dda1d9d1ff843a5ddbd4300f89bd42b43a5b41c25bf92b1302",
			func(migration *gorm.DB) error {
				if err := migration.AutoMigrate(&model.OrderItem{}); err != nil {
					return err
				}
				statements := []string{
					`UPDATE order_items SET parameter_mapping = '{}'::jsonb WHERE parameter_mapping IS NULL`,
					`ALTER TABLE order_items DROP CONSTRAINT IF EXISTS chk_order_item_supplier_binding`,
					`ALTER TABLE order_items ADD CONSTRAINT chk_order_item_supplier_binding CHECK (
						(supplier_id IS NULL AND product_mapping_id IS NULL AND COALESCE(external_product_id, '') = '')
						OR (supplier_id IS NOT NULL AND product_mapping_id IS NOT NULL AND COALESCE(external_product_id, '') <> '')
					)`,
					`ALTER TABLE order_items DROP CONSTRAINT IF EXISTS chk_order_item_parameter_mapping`,
					`ALTER TABLE order_items ADD CONSTRAINT chk_order_item_parameter_mapping CHECK (jsonb_typeof(parameter_mapping) = 'object')`,
					`CREATE INDEX IF NOT EXISTS idx_order_items_supplier_binding ON order_items (supplier_id, product_mapping_id) WHERE deleted_at IS NULL AND supplier_id IS NOT NULL`,
				}
				for _, statement := range statements {
					if err := migration.Exec(statement).Error; err != nil {
						return err
					}
				}
				return nil
			},
		); err != nil {
			return err
		}
		if err := apply(
			"20260810_036_supplier_fixed_amount_markup_v1",
			"1287c98af0197e5db580b1c400ab8a55c143319f59bd7e65996037b2f5c85d73",
			func(migration *gorm.DB) error {
				if err := migration.AutoMigrate(&model.ProductMapping{}, &model.SupplierCategoryMapping{}); err != nil {
					return err
				}
				statements := []string{
					`UPDATE product_mappings SET markup_currency = fixed_price_currency WHERE markup_currency IS NULL OR markup_currency = ''`,
					`UPDATE supplier_category_mappings SET markup_currency = 'CNY' WHERE markup_currency IS NULL OR markup_currency = ''`,
					`ALTER TABLE product_mappings DROP CONSTRAINT IF EXISTS chk_product_mapping_price_mode`,
					`ALTER TABLE product_mappings ADD CONSTRAINT chk_product_mapping_price_mode CHECK (price_mode IN ('fixed_markup','fixed_amount','fixed_price'))`,
					`ALTER TABLE product_mappings DROP CONSTRAINT IF EXISTS chk_product_mapping_markup_amount`,
					`ALTER TABLE product_mappings ADD CONSTRAINT chk_product_mapping_markup_amount CHECK (markup_amount >= 0 AND markup_amount <= 100000000)`,
					`ALTER TABLE supplier_category_mappings DROP CONSTRAINT IF EXISTS chk_supplier_category_mapping_values`,
					`ALTER TABLE supplier_category_mappings DROP CONSTRAINT IF EXISTS chk_supplier_category_price_mode`,
					`ALTER TABLE supplier_category_mappings ADD CONSTRAINT chk_supplier_category_price_mode CHECK (price_mode IN ('fixed_markup','fixed_amount','fixed_price') AND markup_basis_point BETWEEN 0 AND 100000)`,
					`ALTER TABLE supplier_category_mappings DROP CONSTRAINT IF EXISTS chk_supplier_category_markup_amount`,
					`ALTER TABLE supplier_category_mappings ADD CONSTRAINT chk_supplier_category_markup_amount CHECK (markup_amount >= 0 AND markup_amount <= 100000000)`,
					`ALTER TABLE supplier_sync_changes DROP CONSTRAINT IF EXISTS chk_supplier_sync_change_values`,
					`ALTER TABLE supplier_sync_changes ADD CONSTRAINT chk_supplier_sync_change_values CHECK (entity_type IN ('category','product','variant','media','product_mapping') AND action IN ('discover','create','update','unpublish','disable','skip','error'))`,
				}
				for _, statement := range statements {
					if err := migration.Exec(statement).Error; err != nil {
						return err
					}
				}
				return nil
			},
		); err != nil {
			return err
		}
		if err := apply(
			"20260810_037_supplier_inventory_reservations_v1",
			"7cc3fe68bf6559904ec3acfbdd7350720f6928df43886562db2f3fc8469704be",
			func(migration *gorm.DB) error {
				if err := migration.AutoMigrate(&model.SupplierInventoryReservation{}); err != nil {
					return err
				}
				statements := []string{
					`ALTER TABLE supplier_inventory_reservations DROP CONSTRAINT IF EXISTS chk_supplier_inventory_reservation_values`,
					`ALTER TABLE supplier_inventory_reservations ADD CONSTRAINT chk_supplier_inventory_reservation_values CHECK (
						quantity > 0
						AND status IN ('reserved','consumed','released')
						AND expires_at >= created_at
						AND ((status = 'reserved' AND consumed_at IS NULL AND released_at IS NULL)
							OR (status = 'consumed' AND consumed_at IS NOT NULL AND released_at IS NULL)
							OR (status = 'released' AND released_at IS NOT NULL AND consumed_at IS NULL))
					)`,
					`CREATE INDEX IF NOT EXISTS idx_supplier_inventory_reservation_available ON supplier_inventory_reservations (product_mapping_id, status, expires_at) WHERE deleted_at IS NULL`,
					`CREATE INDEX IF NOT EXISTS idx_supplier_inventory_reservation_order ON supplier_inventory_reservations (order_id, status) WHERE deleted_at IS NULL`,
				}
				for _, statement := range statements {
					if err := migration.Exec(statement).Error; err != nil {
						return err
					}
				}
				return nil
			},
		); err != nil {
			return err
		}
		if err := apply(
			"20260810_038_supplier_health_probe_v1",
			"90a38a18884dbe5216f61a3199bbf725a22b5ef42c0331132b376955641427fe",
			func(migration *gorm.DB) error {
				if err := migration.AutoMigrate(&model.Supplier{}); err != nil {
					return err
				}
				statements := []string{
					`UPDATE suppliers SET health_status = 'unknown' WHERE health_status IS NULL OR health_status = ''`,
					`ALTER TABLE suppliers DROP CONSTRAINT IF EXISTS chk_supplier_health_probe`,
					`ALTER TABLE suppliers ADD CONSTRAINT chk_supplier_health_probe CHECK (health_status IN ('unknown','healthy','degraded','unreachable') AND last_probe_latency_ms BETWEEN 0 AND 300000)`,
					`CREATE INDEX IF NOT EXISTS idx_suppliers_health_probe ON suppliers (health_status, last_probe_at DESC) WHERE deleted_at IS NULL`,
				}
				for _, statement := range statements {
					if err := migration.Exec(statement).Error; err != nil {
						return err
					}
				}
				return nil
			},
		); err != nil {
			return err
		}
		if err := apply(
			"20260810_039_supplier_inventory_reservation_identity_v1",
			"e89c02ba63070cb5dcdce18f95cd17d351bda9489eb027c41c81b1b08f763260",
			func(migration *gorm.DB) error {
				statements := []string{
					`ALTER TABLE supplier_inventory_reservations ADD COLUMN IF NOT EXISTS supplier_product_id uuid`,
					`ALTER TABLE supplier_inventory_reservations ADD COLUMN IF NOT EXISTS external_product_id varchar(180)`,
					`UPDATE supplier_inventory_reservations r
					 SET supplier_product_id = sp.id
					 FROM product_mappings pm
					 JOIN supplier_products sp
					   ON sp.supplier_id = pm.supplier_id
					  AND sp.product_id = pm.product_id
					  AND sp.variant_id IS NOT DISTINCT FROM pm.variant_id
					  AND sp.external_id = pm.external_product_id
					 WHERE r.product_mapping_id = pm.id AND r.supplier_product_id IS NULL`,
					`UPDATE supplier_inventory_reservations r
					 SET external_product_id = pm.external_product_id
					 FROM product_mappings pm
					 WHERE r.product_mapping_id = pm.id AND (r.external_product_id IS NULL OR r.external_product_id = '')`,
					`DO $$ BEGIN
					 IF EXISTS (SELECT 1 FROM supplier_inventory_reservations WHERE supplier_product_id IS NULL OR external_product_id IS NULL OR external_product_id = '') THEN
					  RAISE EXCEPTION 'supplier inventory reservation identity backfill failed';
					 END IF;
					 END $$`,
					`ALTER TABLE supplier_inventory_reservations ALTER COLUMN supplier_product_id SET NOT NULL`,
					`ALTER TABLE supplier_inventory_reservations ALTER COLUMN external_product_id SET NOT NULL`,
					`DROP INDEX IF EXISTS idx_supplier_inventory_reservations_order_id`,
					`CREATE INDEX IF NOT EXISTS idx_supplier_inventory_reservations_order_id ON supplier_inventory_reservations (order_id)`,
					`CREATE INDEX IF NOT EXISTS idx_supplier_inventory_reservation_available_v2 ON supplier_inventory_reservations (supplier_product_id, status, expires_at) WHERE deleted_at IS NULL`,
					`CREATE INDEX IF NOT EXISTS idx_supplier_inventory_reservation_capacity ON supplier_inventory_reservations (supplier_id, external_product_id, status) WHERE deleted_at IS NULL`,
					`ALTER TABLE supplier_inventory_reservations DROP CONSTRAINT IF EXISTS chk_supplier_inventory_reservation_values`,
					`ALTER TABLE supplier_inventory_reservations ADD CONSTRAINT chk_supplier_inventory_reservation_values CHECK (
						quantity > 0 AND external_product_id <> ''
						AND status IN ('reserved','consumed','released')
						AND expires_at >= created_at
						AND ((status = 'reserved' AND consumed_at IS NULL AND released_at IS NULL)
							OR (status = 'consumed' AND consumed_at IS NOT NULL AND released_at IS NULL)
							OR (status = 'released' AND released_at IS NOT NULL AND consumed_at IS NULL))
					)`,
				}
				for _, statement := range statements {
					if err := migration.Exec(statement).Error; err != nil {
						return err
					}
				}
				return nil
			},
		); err != nil {
			return err
		}
		if err := apply(
			"20260810_040_product_purchase_limits_v1",
			"15d9fd42b6ebab6a60c7e9ff1dab42bfce45e7b2da76bae1cc613a63fbb6e8ff",
			func(migration *gorm.DB) error {
				if err := migration.AutoMigrate(&model.Product{}); err != nil {
					return err
				}
				statements := []string{
					`UPDATE products SET minimum_purchase = 1 WHERE minimum_purchase IS NULL OR minimum_purchase < 1`,
					`UPDATE products SET maximum_purchase = 0 WHERE maximum_purchase IS NULL OR maximum_purchase < 0`,
					`ALTER TABLE products DROP CONSTRAINT IF EXISTS chk_product_purchase_limits`,
					`ALTER TABLE products ADD CONSTRAINT chk_product_purchase_limits CHECK (
						minimum_purchase BETWEEN 1 AND 1000000
						AND maximum_purchase BETWEEN 0 AND 1000000
						AND (maximum_purchase = 0 OR maximum_purchase >= minimum_purchase)
					)`,
				}
				for _, statement := range statements {
					if err := migration.Exec(statement).Error; err != nil {
						return err
					}
				}
				return nil
			},
		); err != nil {
			return err
		}
		if err := apply(
			"20260810_041_supplier_catalog_rich_fields_v1",
			"e27d26b7b39e7f39c4dd26f553b358a0812a454a4bab8b49d3512276fa62811e",
			func(migration *gorm.DB) error {
				if err := migration.AutoMigrate(&model.SupplierCatalogProduct{}); err != nil {
					return err
				}
				statements := []string{
					`UPDATE supplier_catalog_products SET tags = '[]'::jsonb WHERE tags IS NULL OR jsonb_typeof(tags) <> 'array'`,
					`UPDATE supplier_catalog_products SET wholesale_prices = '{}'::jsonb WHERE wholesale_prices IS NULL OR jsonb_typeof(wholesale_prices) <> 'object'`,
					`UPDATE supplier_catalog_products SET stock_status = CASE WHEN stock > 0 THEN 'in_stock' ELSE 'out_of_stock' END WHERE stock_status IS NULL OR stock_status NOT IN ('unknown','in_stock','out_of_stock')`,
					`ALTER TABLE supplier_catalog_products DROP CONSTRAINT IF EXISTS chk_supplier_catalog_rich_fields`,
					`ALTER TABLE supplier_catalog_products ADD CONSTRAINT chk_supplier_catalog_rich_fields CHECK (
						original_price >= 0 AND member_price >= 0
						AND jsonb_typeof(tags) = 'array'
						AND jsonb_typeof(wholesale_prices) = 'object'
						AND stock_status IN ('unknown','in_stock','out_of_stock')
						AND fulfillment_type IN ('','auto','manual')
					)`,
				}
				for _, statement := range statements {
					if err := migration.Exec(statement).Error; err != nil {
						return err
					}
				}
				return nil
			},
		); err != nil {
			return err
		}
		if err := apply(
			"20260810_042_recharge_callback_exception_refunds_v1",
			"bfa1e421353514e65c5e47aff0652bdc2a1c0a74c4d274bd1416a923e24196a2",
			func(migration *gorm.DB) error {
				if err := migration.AutoMigrate(&model.RechargeOrder{}, &model.RechargeTransaction{}); err != nil {
					return err
				}
				statements := []string{
					`UPDATE recharge_transactions SET expected_amount = amount WHERE expected_amount IS NULL OR expected_amount <= 0`,
					`UPDATE recharge_transactions SET expected_currency = currency WHERE expected_currency IS NULL OR expected_currency = ''`,
					`UPDATE recharge_transactions SET disposition = CASE WHEN status = 'succeeded' THEN 'credited' ELSE 'ignored' END WHERE disposition IS NULL OR disposition = ''`,
					`DO $$
					 BEGIN
						IF EXISTS (
							SELECT 1 FROM recharge_transactions
							WHERE deleted_at IS NULL AND COALESCE(provider_trade_no, '') <> ''
							GROUP BY recharge_order_id, provider_trade_no HAVING COUNT(*) > 1
						) THEN
							RAISE EXCEPTION 'duplicate recharge provider receipts require operator reconciliation before migration';
						END IF;
					 END $$`,
					`CREATE UNIQUE INDEX IF NOT EXISTS idx_recharge_transaction_provider_receipt ON recharge_transactions (recharge_order_id, provider_trade_no) WHERE deleted_at IS NULL AND provider_trade_no <> ''`,
					`CREATE UNIQUE INDEX IF NOT EXISTS idx_recharge_transaction_refund_no ON recharge_transactions (refund_no) WHERE deleted_at IS NULL AND refund_no <> ''`,
					`CREATE INDEX IF NOT EXISTS idx_recharge_transaction_refund_queue ON recharge_transactions (disposition, refund_next_attempt_at) WHERE deleted_at IS NULL AND disposition IN ('refund_pending', 'refund_retrying', 'refund_processing')`,
					`ALTER TABLE recharge_orders DROP CONSTRAINT IF EXISTS chk_recharge_order_values`,
					`ALTER TABLE recharge_orders ADD CONSTRAINT chk_recharge_order_values CHECK (
						amount > 0 AND bonus >= 0 AND currency ~ '^[A-Z]{3}$'
						AND status IN ('creating', 'pending', 'succeeded', 'failed', 'expired', 'cancelled', 'requires_refund', 'refunded', 'refund_failed')
					)`,
					`ALTER TABLE recharge_transactions DROP CONSTRAINT IF EXISTS chk_recharge_transaction_values`,
					`ALTER TABLE recharge_transactions ADD CONSTRAINT chk_recharge_transaction_values CHECK (
						amount > 0 AND expected_amount > 0
						AND currency ~ '^[A-Z]{3}$' AND expected_currency ~ '^[A-Z]{3}$'
						AND status IN ('succeeded', 'ignored')
						AND disposition IN ('credited', 'ignored', 'refund_pending', 'refund_processing', 'refund_retrying', 'refunded', 'refund_failed')
						AND refund_attempts >= 0
						AND (disposition NOT IN ('refund_pending', 'refund_processing', 'refund_retrying', 'refunded', 'refund_failed') OR (refund_no <> '' AND mismatch_reason <> ''))
						AND (disposition <> 'refunded' OR (provider_refund_no <> '' AND refunded_at IS NOT NULL))
					)`,
				}
				for _, statement := range statements {
					if err := migration.Exec(statement).Error; err != nil {
						return err
					}
				}
				return nil
			},
		); err != nil {
			return err
		}
		if err := apply(
			"20260810_043_supplier_catalog_operations_v1",
			"bb95053004534422d17509b86235a44df5eaf8d608028619634379c8eff765ab",
			func(migration *gorm.DB) error {
				if err := migration.AutoMigrate(&model.SupplierSyncPolicy{}, &model.SupplierCatalogImportJob{}); err != nil {
					return err
				}
				statements := []string{
					`UPDATE supplier_sync_policies SET enabled = true WHERE enabled IS NULL`,
					`CREATE UNIQUE INDEX IF NOT EXISTS idx_supplier_catalog_import_jobs_task ON supplier_catalog_import_jobs (task_id) WHERE deleted_at IS NULL AND task_id <> ''`,
					`CREATE INDEX IF NOT EXISTS idx_supplier_catalog_import_jobs_recovery ON supplier_catalog_import_jobs (status, next_attempt_at) WHERE deleted_at IS NULL AND status IN ('queued','running','retrying')`,
					`ALTER TABLE supplier_catalog_import_jobs DROP CONSTRAINT IF EXISTS chk_supplier_catalog_import_job_values`,
					`ALTER TABLE supplier_catalog_import_jobs ADD CONSTRAINT chk_supplier_catalog_import_job_values CHECK (
						status IN ('queued','running','retrying','succeeded','failed','cancelled')
						AND attempts >= 0 AND requested_count BETWEEN 1 AND 500
						AND imported_count >= 0 AND skipped_count >= 0
						AND categories_created >= 0 AND mappings_configured >= 0
						AND jsonb_typeof(request_snapshot) = 'object' AND jsonb_typeof(result_snapshot) = 'object'
						AND imported_count + skipped_count <= requested_count
					)`,
				}
				for _, statement := range statements {
					if err := migration.Exec(statement).Error; err != nil {
						return err
					}
				}
				return nil
			},
		); err != nil {
			return err
		}
		if err := apply(
			"20260811_044_supplier_sync_run_column_names_v1",
			"8cd254869c3d5f95fea6227632f88cee6434f8b423a0475d3d339e951cbc601c",
			func(migration *gorm.DB) error {
				statements := []string{
					`ALTER TABLE supplier_sync_runs ADD COLUMN IF NOT EXISTS categories_created integer NOT NULL DEFAULT 0`,
					`ALTER TABLE supplier_sync_runs ADD COLUMN IF NOT EXISTS products_created integer NOT NULL DEFAULT 0`,
					`DO $$
					 BEGIN
						IF EXISTS (
							SELECT 1 FROM information_schema.columns
							WHERE table_schema = current_schema()
							  AND table_name = 'supplier_sync_runs'
							  AND column_name = 'categories_made'
						) THEN
							EXECUTE 'UPDATE supplier_sync_runs SET categories_created = categories_made WHERE categories_created = 0 AND categories_made <> 0';
							EXECUTE 'ALTER TABLE supplier_sync_runs DROP COLUMN categories_made';
						END IF;
						IF EXISTS (
							SELECT 1 FROM information_schema.columns
							WHERE table_schema = current_schema()
							  AND table_name = 'supplier_sync_runs'
							  AND column_name = 'products_made'
						) THEN
							EXECUTE 'UPDATE supplier_sync_runs SET products_created = products_made WHERE products_created = 0 AND products_made <> 0';
							EXECUTE 'ALTER TABLE supplier_sync_runs DROP COLUMN products_made';
						END IF;
					 END $$`,
				}
				for _, statement := range statements {
					if err := migration.Exec(statement).Error; err != nil {
						return err
					}
				}
				return nil
			},
		); err != nil {
			return err
		}
		if err := apply(
			"20260811_045_supplier_category_bindings_v1",
			"a2fb7bd5ce9072aa5d9942357f4e9d5a061463990534b342fe13c72cd739ec51",
			func(migration *gorm.DB) error {
				if err := migration.AutoMigrate(&model.SupplierCategoryMapping{}, &model.ProductMapping{}); err != nil {
					return err
				}
				statements := []string{
					`DO $$
					 BEGIN
						IF EXISTS (
							SELECT 1 FROM supplier_category_mappings
							WHERE deleted_at IS NULL AND price_mode = 'fixed_price'
						) THEN
							RAISE EXCEPTION 'active fixed_price category mappings must be converted to fixed_markup or fixed_amount before migration';
						END IF;
					 END $$`,
					`UPDATE supplier_category_mappings scm
					 SET external_category_name = sc.name
					 FROM supplier_categories sc
					 WHERE sc.supplier_id = scm.supplier_id
					   AND sc.external_id = scm.external_category_id
					   AND sc.deleted_at IS NULL
					   AND COALESCE(scm.external_category_name, '') = ''`,
					`UPDATE supplier_category_mappings SET sync_price = true WHERE sync_price IS NULL`,
					`UPDATE supplier_category_mappings SET sync_stock = true WHERE sync_stock IS NULL`,
					`UPDATE supplier_category_mappings SET enabled = true WHERE enabled IS NULL`,
					`UPDATE supplier_category_mappings SET sync_title = sync_name`,
					`UPDATE supplier_category_mappings SET sync_parent = true WHERE auto_create = true`,
					`CREATE INDEX IF NOT EXISTS idx_supplier_category_bindings_status_sort ON supplier_category_mappings (enabled, sort, created_at DESC) WHERE deleted_at IS NULL`,
					`CREATE INDEX IF NOT EXISTS idx_product_mappings_category_policy ON product_mappings (supplier_category_mapping_id, inherit_category_policy) WHERE deleted_at IS NULL AND inherit_category_policy = true`,
					`ALTER TABLE supplier_category_mappings DROP CONSTRAINT IF EXISTS chk_supplier_category_price_mode`,
					`ALTER TABLE supplier_category_mappings ADD CONSTRAINT chk_supplier_category_price_mode CHECK (
						deleted_at IS NOT NULL OR (price_mode IN ('fixed_markup','fixed_amount') AND markup_basis_point BETWEEN 0 AND 100000)
					)`,
					`ALTER TABLE supplier_category_mappings DROP CONSTRAINT IF EXISTS chk_supplier_category_binding_operations`,
					`ALTER TABLE supplier_category_mappings ADD CONSTRAINT chk_supplier_category_binding_operations CHECK (
						external_category_id <> ''
						AND sort BETWEEN 0 AND 1000000
						AND char_length(default_cover_url) <= 1000
					)`,
					`ALTER TABLE product_mappings DROP CONSTRAINT IF EXISTS chk_product_mapping_category_policy`,
					`ALTER TABLE product_mappings ADD CONSTRAINT chk_product_mapping_category_policy CHECK (
						NOT inherit_category_policy OR supplier_category_mapping_id IS NOT NULL
					)`,
				}
				for _, statement := range statements {
					if err := migration.Exec(statement).Error; err != nil {
						return err
					}
				}
				return nil
			},
		); err != nil {
			return err
		}
		if err := apply(
			"20260812_046_allow_loopback_media_urls_v1",
			"b5b9ffc3ed11b8ebb7b22f0bf1cd7c2f4739f388dedcb22adfd84df003d03713",
			func(migration *gorm.DB) error {
				// Uploaded media is served from the configured media base URL.
				// Native self-hosted deployments legitimately use loopback HTTP
				// (for example http://127.0.0.1:8080/media/...), so the strict
				// HTTPS-only check on media_assets.public_url must also accept
				// loopback addresses. External CDN URLs remain HTTPS-only.
				statements := []string{
					`ALTER TABLE media_assets DROP CONSTRAINT IF EXISTS chk_media_assets_external_url`,
					`ALTER TABLE media_assets ADD CONSTRAINT chk_media_assets_external_url CHECK (
						public_url = '' OR public_url LIKE 'https://%'
						OR public_url LIKE 'http://127.0.0.1%'
						OR public_url LIKE 'http://localhost%'
						OR public_url LIKE 'http://[::1]%'
					)`,
				}
				for _, statement := range statements {
					if err := migration.Exec(statement).Error; err != nil {
						return err
					}
				}
				return nil
			},
		); err != nil {
			return err
		}
		if err := apply(
			"20260812_047_fix_loopback_media_url_check_v1",
			"66c6a51b1e621852b28d4ba855651e1d4a1938569951346aa4856eadc8e57fe9",
			func(migration *gorm.DB) error {
				// The media base URL includes a port in native deployments
				// (http://127.0.0.1:8080/media/...), so the loopback patterns
				// must not require a slash immediately after the hostname.
				statements := []string{
					`ALTER TABLE media_assets DROP CONSTRAINT IF EXISTS chk_media_assets_external_url`,
					`ALTER TABLE media_assets ADD CONSTRAINT chk_media_assets_external_url CHECK (
						public_url = '' OR public_url LIKE 'https://%'
						OR public_url LIKE 'http://127.0.0.1%'
						OR public_url LIKE 'http://localhost%'
						OR public_url LIKE 'http://[::1]%'
					)`,
				}
				for _, statement := range statements {
					if err := migration.Exec(statement).Error; err != nil {
						return err
					}
				}
				return nil
			},
		); err != nil {
			return err
		}
		return nil
	})
}

func seedNotificationAudienceDefaults(db *gorm.DB) error {
	adminEvents := []string{"user.registered", "user.login.succeeded", "user.login.failed", "admin.login.failed", "order.created", "order.paid", "order.processing", "order.delivered", "order.failed", "order.refunded", "recharge.created", "recharge.succeeded", "recharge.failed", "openapi.credential.created", "openapi.call.succeeded", "openapi.call.failed", "openapi.order.created", "inventory.low_stock", "inventory.out_of_stock", "inventory.restocked", "supplier.sync.succeeded", "supplier.sync.failed", "procurement.created", "procurement.succeeded", "procurement.failed", "risk.blocked", "security.high_risk"}
	userEvents := []string{"user.registered", "user.login.succeeded", "user.login.failed", "order.created", "order.paid", "order.processing", "order.delivered", "order.failed", "order.refunded", "recharge.created", "recharge.succeeded", "recharge.failed", "openapi.order.created"}
	for _, code := range adminEvents {
		name := "管理员 · " + code
		template := model.NotificationTemplate{Code: "admin." + strings.ReplaceAll(code, ".", "_") + ".inbox", Name: name, Audience: "admin", Channel: "admin", Locale: "zh-CN", Subject: "LinLinQi 运营事件：{{event}}", Body: "{{summary}}\n状态：{{status}}\n订单：{{order_no}}\n商品：{{product_name}}\n库存：{{stock}}", Variables: `["event","summary","status","order_no","product_name","stock"]`, Enabled: true, Version: 1}
		if err := db.Where("code = ? AND locale = ?", template.Code, template.Locale).FirstOrCreate(&template).Error; err != nil {
			return err
		}
	}
	for _, code := range userEvents {
		name := "用户 · " + code
		template := model.NotificationTemplate{Code: "user." + strings.ReplaceAll(code, ".", "_") + ".inapp", Name: name, Audience: "user", Channel: "in_app", Locale: "zh-CN", Subject: "{{summary}}", Body: "{{summary}}\n订单：{{order_no}}\n状态：{{status}}", Variables: `["summary","order_no","status"]`, Enabled: true, Version: 1}
		if err := db.Where("code = ? AND locale = ?", template.Code, template.Locale).FirstOrCreate(&template).Error; err != nil {
			return err
		}
		var stored model.NotificationTemplate
		if err := db.Where("code = ? AND locale = ?", template.Code, template.Locale).First(&stored).Error; err != nil {
			return err
		}
		var count int64
		destination := "event_user"
		if err := db.Model(&model.NotificationSubscription{}).Where("audience = ? AND event_code = ? AND channel = ? AND recipient = ?", "user", code, "in_app", destination).Count(&count).Error; err != nil {
			return err
		}
		if count == 0 {
			subscription := model.NotificationSubscription{Audience: "user", EventCode: code, Channel: "in_app", Recipient: destination, TemplateID: stored.ID, Locale: "zh-CN", Enabled: true}
			if err := db.Create(&subscription).Error; err != nil {
				return err
			}
		}
	}
	return nil
}

func seedNotificationAdminSubscriptions(db *gorm.DB) error {
	for _, eventCode := range notificationAdminEventCodes {
		var template model.NotificationTemplate
		templateCode := "admin." + strings.ReplaceAll(eventCode, ".", "_") + ".inbox"
		if err := db.Where("code = ? AND audience = ? AND channel = ? AND locale = ? AND enabled = ?", templateCode, "admin", "admin", "zh-CN", true).First(&template).Error; err != nil {
			return err
		}
		var count int64
		if err := db.Model(&model.NotificationSubscription{}).Where("audience = ? AND event_code = ? AND channel = ? AND recipient = ?", "admin", eventCode, "admin", "all").Count(&count).Error; err != nil {
			return err
		}
		if count == 0 {
			subscription := model.NotificationSubscription{Audience: "admin", EventCode: eventCode, Channel: "admin", Recipient: "all", TemplateID: template.ID, Locale: "zh-CN", Enabled: true}
			if err := db.Create(&subscription).Error; err != nil {
				return err
			}
		}
	}
	return nil
}

func BootstrapAdmin(db *gorm.DB, cfg config.Config) error {
	var admin model.Admin
	if err := db.Where("username = ?", "admin").First(&admin).Error; err == gorm.ErrRecordNotFound {
		if cfg.BootstrapAdminPassword == "" {
			return fmt.Errorf("BOOTSTRAP_ADMIN_PASSWORD is required to bootstrap the initial administrator")
		}
		hash, _ := bcrypt.GenerateFromPassword([]byte(cfg.BootstrapAdminPassword), bcrypt.DefaultCost)
		admin = model.Admin{Username: "admin", PasswordHash: string(hash), Name: "超级管理员", Role: "super_admin", Status: "active"}
		if err := db.Create(&admin).Error; err != nil {
			return err
		}
	} else if err != nil {
		return err
	}
	return seedBaselines(db, admin.ID)
}

func SeedDevelopmentData(db *gorm.DB, cfg config.Config, vault *security.Vault) error {
	if cfg.Env == "production" {
		return fmt.Errorf("development seed data is forbidden in production")
	}
	var count int64
	if err := db.Model(&model.Category{}).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	categories := []model.Category{
		{Name: "AI 与效率", Slug: "ai-tools", Description: "AI 会员与创作效率工具", Icon: "sparkles", Sort: 100, Enabled: true},
		{Name: "开发与云服务", Slug: "developer", Description: "云主机、代码托管和 API 服务", Icon: "code", Sort: 90, Enabled: true},
		{Name: "影音娱乐", Slug: "entertainment", Description: "主流影音会员与数字娱乐", Icon: "play", Sort: 80, Enabled: true},
		{Name: "游戏点卡", Slug: "gaming", Description: "全球游戏充值卡与激活码", Icon: "gamepad", Sort: 70, Enabled: true},
	}
	if err := db.Create(&categories).Error; err != nil {
		return err
	}
	products := []model.Product{
		{CategoryID: categories[0].ID, Name: "AI Pro 月度会员", Slug: "ai-pro-monthly", Summary: "独享账号，即买即用，支持售后", Description: "自动交付账号与使用说明，适合高频创作与办公。", Price: 12900, ComparePrice: 15900, CostPrice: 8200, Status: "on_sale", DeliveryType: "auto", InventoryMode: "local", Featured: true, SoldCount: 1286, Tags: "热门,自动发货"},
		{CategoryID: categories[1].ID, Name: "云服务器基础版", Slug: "cloud-starter", Summary: "2C 4G / 80G SSD / 30 天", Description: "适合个人站点、接口服务和开发测试。", Price: 4900, ComparePrice: 6900, CostPrice: 2900, Status: "on_sale", DeliveryType: "auto", InventoryMode: "supplier", Featured: true, SoldCount: 869, Tags: "云服务,供应商直充"},
		{CategoryID: categories[2].ID, Name: "流媒体家庭组季卡", Slug: "streaming-family-quarter", Summary: "稳定车位，到期提醒，可续费", Description: "人工复核后自动发出家庭组邀请。", Price: 8800, ComparePrice: 10800, CostPrice: 6000, Status: "on_sale", DeliveryType: "auto", InventoryMode: "local", Featured: true, SoldCount: 2158, Tags: "影音,畅销"},
		{CategoryID: categories[3].ID, Name: "Steam 100 元充值卡", Slug: "steam-cny-100", Summary: "国区可用，秒级自动发卡", Description: "下单后系统自动交付卡密，请在 Steam 钱包中兑换。", Price: 9700, ComparePrice: 10000, CostPrice: 9200, Status: "on_sale", DeliveryType: "auto", InventoryMode: "local", Featured: false, SoldCount: 5320, Tags: "游戏,秒发"},
		{CategoryID: categories[0].ID, Name: "设计协作专业版", Slug: "design-team-pro", Summary: "团队席位，30 天质保", Description: "支持在线设计、团队资产与协作审阅。", Price: 3900, ComparePrice: 5900, CostPrice: 2100, Status: "on_sale", DeliveryType: "auto", InventoryMode: "local", Featured: false, SoldCount: 740, Tags: "设计,团队"},
		{CategoryID: categories[1].ID, Name: "Git 托管年度增强版", Slug: "git-pro-yearly", Summary: "个人账户升级，全年有效", Description: "适合私有仓库、自动化构建和开发协作。", Price: 23800, ComparePrice: 28800, CostPrice: 16500, Status: "on_sale", DeliveryType: "manual", InventoryMode: "supplier", Featured: false, SoldCount: 326, Tags: "开发,年度"},
	}
	if err := db.Create(&products).Error; err != nil {
		return err
	}
	for _, product := range products {
		for i := 1; i <= 12; i++ {
			content := fmt.Sprintf("%s-CARD-%04d|PIN-%06d", product.Slug, i, 100000+i)
			ciphertext, nonce, fingerprint, err := vault.Encrypt(content, product.ID[:])
			if err != nil {
				return err
			}
			card := model.Card{ProductID: product.ID, EncryptedContent: ciphertext, Nonce: nonce, Fingerprint: fingerprint, Preview: security.SecretPreview(content), Status: "available"}
			if err := db.Create(&card).Error; err != nil {
				return err
			}
		}
	}
	channels := []model.PaymentChannel{{Name: "LinLinQi 开发沙箱", Code: "sandbox", Provider: "sandbox", FeeRate: 0, Enabled: cfg.Env != "production", Sort: 100}}
	if err := db.Create(&channels).Error; err != nil {
		return err
	}
	if err := db.Create(&model.Announcement{Title: "全站商品支持自动发货", Content: "支付成功后卡密将立即展示，并同步发送到下单邮箱。", Level: "info", Enabled: true, Sort: 100}).Error; err != nil {
		return err
	}
	return nil
}

func seedBaselines(db *gorm.DB, adminID uuid.UUID) error {
	role := model.Role{Code: "super_admin", Name: "超级管理员", Description: "拥有全部运营权限", System: true}
	if err := db.Where("code = ?", role.Code).FirstOrCreate(&role).Error; err != nil {
		return err
	}
	permissions := []model.Permission{
		{Code: "dashboard.read", Name: "查看经营概览", Module: "dashboard", Description: "查看经营概览与数据分析"},
		{Code: "catalog.view", Name: "查看商品目录", Module: "catalog", Description: "查看商品、分类、规格与媒体"},
		{Code: "catalog.manage", Name: "管理商品目录", Module: "catalog", Description: "创建、编辑与删除商品、分类、规格与媒体"},
		{Code: "inventory.view", Name: "查看卡密库存", Module: "inventory", Description: "查看卡密与库存批次"},
		{Code: "inventory.manage", Name: "管理卡密库存", Module: "inventory", Description: "导入卡密与变更库存状态"},
		{Code: "order.view", Name: "查看订单售后", Module: "order", Description: "查看订单与工单"},
		{Code: "order.manage", Name: "管理订单售后", Module: "order", Description: "处理订单流转、退款与工单回复"},
		{Code: "customer.view", Name: "查看客户资料", Module: "customer", Description: "查看客户详情与统计"},
		{Code: "customer.manage", Name: "管理客户资料", Module: "customer", Description: "变更客户状态、会员与钱包"},
		{Code: "wallet.view", Name: "查看钱包账本", Module: "wallet", Description: "查看钱包余额与流水"},
		{Code: "wallet.manage", Name: "管理钱包账本", Module: "wallet", Description: "人工调整钱包余额"},
		{Code: "payment.view", Name: "查看支付与对账", Module: "payment", Description: "查看支付渠道、充值、退款与对账"},
		{Code: "payment.manage", Name: "管理支付与对账", Module: "payment", Description: "配置支付渠道、发起退款与处理对账"},
		{Code: "supplier.view", Name: "查看供货采购", Module: "supplier", Description: "查看供应商、映射与采购单"},
		{Code: "supplier.manage", Name: "管理供货采购", Module: "supplier", Description: "配置供应商、映射与采购操作"},
		{Code: "marketing.view", Name: "查看营销内容", Module: "marketing", Description: "查看活动、优惠券、内容与推广"},
		{Code: "marketing.manage", Name: "管理营销内容", Module: "marketing", Description: "创建、编辑与删除营销与内容"},
		{Code: "reseller.view", Name: "查看分销站群", Module: "reseller", Description: "查看分销商、站点与提现"},
		{Code: "reseller.manage", Name: "管理分销站群", Module: "reseller", Description: "管理分销商、批发等级与提现审核"},
		{Code: "security.view", Name: "查看安全风控", Module: "security", Description: "查看风控规则、决策与安全事件"},
		{Code: "security.manage", Name: "管理安全风控", Module: "security", Description: "管理风控规则、事件处置与 IP 黑名单"},
		{Code: "system.view", Name: "查看系统设置", Module: "system", Description: "查看系统设置、任务、通知与审计"},
		{Code: "system.manage", Name: "管理系统设置", Module: "system", Description: "修改系统设置、权限、任务与通知"},
	}
	for i := range permissions {
		if err := db.Where("code = ?", permissions[i].Code).FirstOrCreate(&permissions[i]).Error; err != nil {
			return err
		}
	}
	// The system super_admin role always owns every permission in the
	// table, including any added outside this seed list, so an existing
	// deployment regains full access after an upgrade and a read-only
	// permission can never leave the super administrator locked out.
	var allPermissions []model.Permission
	if err := db.Where("deleted_at IS NULL").Find(&allPermissions).Error; err != nil {
		return err
	}
	for i := range allPermissions {
		if err := db.Where("role_id = ? AND permission_id = ?", role.ID, allPermissions[i].ID).FirstOrCreate(&model.RolePermission{RoleID: role.ID, PermissionID: allPermissions[i].ID}).Error; err != nil {
			return err
		}
	}
	if err := db.Where("admin_id = ? AND role_id = ?", adminID, role.ID).FirstOrCreate(&model.AdminRole{AdminID: adminID, RoleID: role.ID}).Error; err != nil {
		return err
	}
	levels := []model.MemberLevel{
		{Code: "standard", Name: "标准会员", MinimumSpend: 0, DiscountBasisPoint: 0, Priority: 0, Enabled: true},
		{Code: "silver", Name: "银卡会员", MinimumSpend: 100000, DiscountBasisPoint: 200, Priority: 10, Enabled: true},
		{Code: "gold", Name: "金卡会员", MinimumSpend: 500000, DiscountBasisPoint: 500, Priority: 20, Enabled: true},
		{Code: "enterprise", Name: "企业会员", MinimumSpend: 2000000, DiscountBasisPoint: 800, Priority: 30, Enabled: true},
	}
	for i := range levels {
		if err := db.Where("code = ?", levels[i].Code).FirstOrCreate(&levels[i]).Error; err != nil {
			return err
		}
	}
	rules := []model.RiskRule{
		{Code: "ip_order_velocity", Name: "IP 订单频率", Scope: "checkout", Expression: `orders(ip,10m) > 12`, Action: "challenge", Score: 40, Enabled: true, Priority: 100},
		{Code: "email_failure_rate", Name: "邮箱支付失败率", Scope: "payment", Expression: `failures(email,1h) > 5`, Action: "review", Score: 55, Enabled: true, Priority: 90},
		{Code: "high_value_guest", Name: "游客高额订单", Scope: "checkout", Expression: `guest && total > 100000`, Action: "challenge", Score: 35, Enabled: true, Priority: 80},
	}
	for i := range rules {
		if err := db.Where("code = ?", rules[i].Code).FirstOrCreate(&rules[i]).Error; err != nil {
			return err
		}
	}
	templates := []model.NotificationTemplate{
		{Code: "order_paid_email", Name: "订单支付成功", Channel: "email", Locale: "zh-CN", Subject: "订单 {{order_no}} 支付成功", Body: "您的订单已经支付成功，系统正在交付。", Variables: `["order_no"]`, Enabled: true, Version: 1},
		{Code: "order_delivered_email", Name: "订单交付完成", Channel: "email", Locale: "zh-CN", Subject: "订单 {{order_no}} 已交付", Body: "登录 LinLinQi 或使用订单查询查看交付内容。", Variables: `["order_no"]`, Enabled: true, Version: 1},
		{Code: "stock_low_admin", Name: "库存不足提醒", Channel: "admin", Locale: "zh-CN", Subject: "商品库存不足", Body: "{{product_name}} 当前可用库存为 {{stock}}。", Variables: `["product_name","stock"]`, Enabled: true, Version: 1},
	}
	for i := range templates {
		if err := db.Where("code = ? AND locale = ?", templates[i].Code, templates[i].Locale).FirstOrCreate(&templates[i]).Error; err != nil {
			return err
		}
	}
	settings := []model.Setting{
		{Key: "affiliate_default_basis_points", Value: "500", Group: "affiliate"},
		{Key: "affiliate_hold_days", Value: "7", Group: "affiliate"},
		{Key: "affiliate_withdrawal_minimum", Value: "10000", Group: "affiliate"},
	}
	for i := range settings {
		if err := db.Where("key = ?", settings[i].Key).FirstOrCreate(&settings[i]).Error; err != nil {
			return err
		}
	}
	return nil
}
