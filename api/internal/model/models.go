package model

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type Base struct {
	ID        uuid.UUID      `json:"id" gorm:"type:uuid;primaryKey"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `json:"-" gorm:"index"`
}

func (b *Base) BeforeCreate(_ *gorm.DB) error {
	if b.ID == uuid.Nil {
		b.ID = uuid.New()
	}
	return nil
}

type User struct {
	Base
	Email           string    `json:"email" gorm:"uniqueIndex;size:190;not null"`
	PasswordHash    string    `json:"-" gorm:"not null"`
	Nickname        string    `json:"nickname" gorm:"size:80"`
	AvatarURL       string    `json:"avatar_url" gorm:"size:1000"`
	Balance         int64     `json:"balance" gorm:"not null;default:0"`
	Status          string    `json:"status" gorm:"size:20;not null;default:active"`
	SessionVersion  uint64    `json:"-" gorm:"not null;default:0"`
	PreferredLocale string    `json:"preferred_locale" gorm:"size:16;not null;default:zh-CN"`
	LastLoginAt     time.Time `json:"last_login_at"`
}

type Admin struct {
	Base
	Username       string    `json:"username" gorm:"uniqueIndex;size:80;not null"`
	PasswordHash   string    `json:"-" gorm:"not null"`
	Name           string    `json:"name" gorm:"size:80"`
	Role           string    `json:"role" gorm:"size:40;not null;default:operator"`
	Status         string    `json:"status" gorm:"size:20;not null;default:active"`
	SessionVersion uint64    `json:"-" gorm:"not null;default:0"`
	LastLoginAt    time.Time `json:"last_login_at"`
}

type Category struct {
	Base
	ParentID     *uuid.UUID  `json:"parent_id,omitempty" gorm:"type:uuid;index"`
	Parent       *Category   `json:"parent,omitempty" gorm:"foreignKey:ParentID"`
	Name         string      `json:"name" gorm:"size:100;not null"`
	Slug         string      `json:"slug" gorm:"uniqueIndex;size:120;not null"`
	Description  string      `json:"description" gorm:"size:2000"`
	Icon         string      `json:"icon" gorm:"size:40"`
	ImageAssetID *uuid.UUID  `json:"image_asset_id,omitempty" gorm:"type:uuid;index"`
	ImageAsset   *MediaAsset `json:"image_asset,omitempty" gorm:"foreignKey:ImageAssetID"`
	ImageURL     string      `json:"image_url" gorm:"size:1000"`
	Sort         int         `json:"sort" gorm:"not null;default:0"`
	Enabled      bool        `json:"enabled" gorm:"not null;default:true"`
}

type Product struct {
	Base
	CategoryID      uuid.UUID   `json:"category_id" gorm:"type:uuid;index;not null"`
	Category        Category    `json:"category"`
	Name            string      `json:"name" gorm:"size:160;not null"`
	Slug            string      `json:"slug" gorm:"uniqueIndex;size:180;not null"`
	Summary         string      `json:"summary" gorm:"size:500"`
	Description     string      `json:"description" gorm:"type:text"`
	CoverAssetID    *uuid.UUID  `json:"cover_asset_id,omitempty" gorm:"type:uuid;index"`
	CoverAsset      *MediaAsset `json:"cover_asset,omitempty" gorm:"foreignKey:CoverAssetID"`
	CoverURL        string      `json:"cover_url" gorm:"size:1000"`
	Currency        string      `json:"currency" gorm:"size:3;index;not null;default:CNY"`
	Price           int64       `json:"price" gorm:"not null"`
	ComparePrice    int64       `json:"compare_price" gorm:"not null;default:0"`
	CostPrice       int64       `json:"cost_price" gorm:"not null;default:0"`
	DeliveryType    string      `json:"delivery_type" gorm:"size:30;not null;default:auto"`
	InventoryMode   string      `json:"inventory_mode" gorm:"size:30;not null;default:local"`
	MinimumPurchase int         `json:"minimum_purchase" gorm:"not null;default:1"`
	MaximumPurchase int         `json:"maximum_purchase" gorm:"not null;default:0"`
	Status          string      `json:"status" gorm:"size:30;index;not null;default:on_sale"`
	SoldCount       int64       `json:"sold_count" gorm:"not null;default:0"`
	Sort            int         `json:"sort" gorm:"not null;default:0"`
	Featured        bool        `json:"featured" gorm:"not null;default:false"`
	Tags            string      `json:"tags" gorm:"size:500"`
}

type Card struct {
	Base
	ProductID        uuid.UUID  `json:"product_id" gorm:"type:uuid;uniqueIndex:idx_card_product_fingerprint;not null"`
	VariantID        *uuid.UUID `json:"variant_id,omitempty" gorm:"type:uuid;index"`
	EncryptedContent []byte     `json:"-" gorm:"not null"`
	Nonce            []byte     `json:"-" gorm:"not null"`
	Fingerprint      string     `json:"-" gorm:"size:64;uniqueIndex:idx_card_product_fingerprint;not null"`
	Preview          string     `json:"preview" gorm:"size:120;not null"`
	Status           string     `json:"status" gorm:"size:20;index;not null;default:available"`
	OrderID          *uuid.UUID `json:"order_id" gorm:"type:uuid;index"`
	SoldAt           *time.Time `json:"sold_at"`
}

type Order struct {
	Base
	OrderNo                string          `json:"order_no" gorm:"uniqueIndex;size:40;not null"`
	LookupTokenHash        string          `json:"-" gorm:"index;size:64"`
	LookupTokenCipher      []byte          `json:"-"`
	LookupTokenNonce       []byte          `json:"-"`
	LookupToken            string          `json:"lookup_token,omitempty" gorm:"-"`
	ExternalOrderNo        *string         `json:"external_order_no,omitempty" gorm:"uniqueIndex:idx_order_api_external;size:100"`
	APICredentialID        *uuid.UUID      `json:"-" gorm:"type:uuid;uniqueIndex:idx_order_api_external"`
	CallbackEndpointID     *uuid.UUID      `json:"-" gorm:"type:uuid;index"`
	CouponID               *uuid.UUID      `json:"coupon_id,omitempty" gorm:"type:uuid;index"`
	ResellerID             *uuid.UUID      `json:"reseller_id,omitempty" gorm:"type:uuid;index"`
	ResellerMargin         int64           `json:"-" gorm:"not null;default:0"`
	ResellerMarginReversed int64           `json:"-" gorm:"not null;default:0"`
	UserID                 *uuid.UUID      `json:"user_id" gorm:"type:uuid;index"`
	Email                  string          `json:"email" gorm:"index;size:190;not null"`
	Status                 string          `json:"status" gorm:"size:30;index;not null"`
	PaymentStatus          string          `json:"payment_status" gorm:"size:30;index;not null"`
	Subtotal               int64           `json:"subtotal" gorm:"not null"`
	Discount               int64           `json:"discount" gorm:"not null;default:0"`
	Adjustments            json.RawMessage `json:"adjustments" gorm:"type:jsonb;default:'[]'"`
	Total                  int64           `json:"total" gorm:"not null"`
	Currency               string          `json:"currency" gorm:"size:3;index;not null;default:CNY"`
	FXSnapshotID           *uuid.UUID      `json:"fx_snapshot_id,omitempty" gorm:"type:uuid;index"`
	PaymentMethod          string          `json:"payment_method" gorm:"size:40"`
	PaidAt                 *time.Time      `json:"paid_at"`
	DeliveredAt            *time.Time      `json:"delivered_at"`
	ClientIP               string          `json:"client_ip" gorm:"size:64"`
	Items                  []OrderItem     `json:"items"`
}

type OrderItem struct {
	Base
	OrderID             uuid.UUID  `json:"order_id" gorm:"type:uuid;index;not null"`
	ProductID           uuid.UUID  `json:"product_id" gorm:"type:uuid;index;not null"`
	VariantID           *uuid.UUID `json:"variant_id,omitempty" gorm:"type:uuid;index"`
	SupplierID          *uuid.UUID `json:"-" gorm:"type:uuid;index"`
	ProductMappingID    *uuid.UUID `json:"-" gorm:"type:uuid;index"`
	ExternalProductID   string     `json:"-" gorm:"size:180"`
	ParameterMapping    string     `json:"-" gorm:"type:jsonb;not null;default:'{}'"`
	VariantName         string     `json:"variant_name,omitempty" gorm:"size:160"`
	ProductName         string     `json:"product_name" gorm:"size:160;not null"`
	UnitPrice           int64      `json:"unit_price" gorm:"not null"`
	Currency            string     `json:"currency" gorm:"size:3;not null;default:CNY"`
	UpstreamUnitPrice   int64      `json:"upstream_unit_price" gorm:"not null;default:0"`
	UpstreamCurrency    string     `json:"upstream_currency" gorm:"size:3"`
	FXSnapshotID        *uuid.UUID `json:"fx_snapshot_id,omitempty" gorm:"type:uuid;index"`
	PlatformUnitPrice   int64      `json:"-" gorm:"not null;default:0"`
	ResellerMargin      int64      `json:"-" gorm:"not null;default:0"`
	Quantity            int        `json:"quantity" gorm:"not null"`
	CardCiphertext      []byte     `json:"-"`
	CardNonce           []byte     `json:"-"`
	DeliveryItemsCipher []byte     `json:"-"`
	DeliveryItemsNonce  []byte     `json:"-"`
	CardPreview         string     `json:"-" gorm:"size:120"`
	CardContent         string     `json:"card_content,omitempty" gorm:"-"`
}

type Supplier struct {
	Base
	Name                string     `json:"name" gorm:"size:120;not null"`
	Code                string     `json:"code" gorm:"uniqueIndex;size:60;not null"`
	BaseURL             string     `json:"base_url" gorm:"size:500;not null"`
	APIKeyCipher        []byte     `json:"-"`
	APIKeyNonce         []byte     `json:"-"`
	APISecretCipher     []byte     `json:"-"`
	APISecretNonce      []byte     `json:"-"`
	CredentialsCipher   []byte     `json:"-"`
	CredentialsNonce    []byte     `json:"-"`
	Protocol            string     `json:"protocol" gorm:"size:40;not null;default:standard"`
	Status              string     `json:"status" gorm:"size:20;index;not null;default:active"`
	Balance             int64      `json:"balance" gorm:"not null;default:0"`
	BalanceCurrency     string     `json:"balance_currency" gorm:"size:8;not null;default:CNY"`
	PriceCurrency       string     `json:"price_currency" gorm:"size:3;index;not null;default:CNY"`
	PriceMinorUnit      int        `json:"price_minor_unit" gorm:"not null;default:2"`
	CurrencyMode        string     `json:"currency_mode" gorm:"size:16;not null;default:auto"`
	BalanceSyncedAt     *time.Time `json:"balance_synced_at"`
	HealthStatus        string     `json:"health_status" gorm:"size:20;index;not null;default:unknown"`
	LastProbeAt         *time.Time `json:"last_probe_at,omitempty" gorm:"index"`
	LastProbeLatencyMS  int        `json:"last_probe_latency_ms" gorm:"not null;default:0"`
	LastProbeError      string     `json:"last_probe_error" gorm:"size:500"`
	LastSyncAt          *time.Time `json:"last_sync_at"`
	SyncIntervalMinutes int        `json:"sync_interval_minutes" gorm:"not null;default:15"`
}

type SupplierProduct struct {
	Base
	SupplierID      uuid.UUID  `json:"supplier_id" gorm:"type:uuid;index;not null"`
	ProductID       uuid.UUID  `json:"product_id" gorm:"type:uuid;index;not null"`
	VariantID       *uuid.UUID `json:"variant_id,omitempty" gorm:"type:uuid;index"`
	ExternalID      string     `json:"external_id" gorm:"size:160;not null"`
	ExternalPrice   int64      `json:"external_price" gorm:"not null"`
	ExternalStock   int64      `json:"external_stock" gorm:"not null;default:0"`
	PriceMarkupRate int        `json:"price_markup_rate" gorm:"not null;default:0"`
	AutoSync        bool       `json:"auto_sync" gorm:"not null;default:true"`
}

type PaymentChannel struct {
	Base
	Name                string          `json:"name" gorm:"size:100;not null"`
	Code                string          `json:"code" gorm:"uniqueIndex;size:50;not null"`
	Provider            string          `json:"provider" gorm:"size:80;not null"`
	FeeRate             int             `json:"fee_rate" gorm:"not null;default:0"`
	Enabled             bool            `json:"enabled" gorm:"not null;default:true"`
	Sort                int             `json:"sort" gorm:"not null;default:0"`
	SupportedCurrencies json.RawMessage `json:"supported_currencies" gorm:"type:jsonb;not null;default:'[\"CNY\"]'"`
	SettlementCurrency  string          `json:"settlement_currency" gorm:"size:3;index;not null;default:CNY"`
	ConfigCipher        []byte          `json:"-"`
	ConfigNonce         []byte          `json:"-"`
}

type Coupon struct {
	Base
	Code       string    `json:"code" gorm:"uniqueIndex;size:80;not null"`
	Type       string    `json:"type" gorm:"size:20;not null"`
	Currency   string    `json:"currency" gorm:"size:3;index;not null;default:CNY"`
	Value      int64     `json:"value" gorm:"not null"`
	MinAmount  int64     `json:"min_amount" gorm:"not null;default:0"`
	UsageLimit int       `json:"usage_limit" gorm:"not null;default:0"`
	UsedCount  int       `json:"used_count" gorm:"not null;default:0"`
	StartsAt   time.Time `json:"starts_at"`
	EndsAt     time.Time `json:"ends_at"`
	Enabled    bool      `json:"enabled" gorm:"not null;default:true"`
}

type Announcement struct {
	Base
	Title   string `json:"title" gorm:"size:160;not null"`
	Content string `json:"content" gorm:"type:text"`
	Level   string `json:"level" gorm:"size:20;not null;default:info"`
	Enabled bool   `json:"enabled" gorm:"not null;default:true"`
	Sort    int    `json:"sort" gorm:"not null;default:0"`
}

type APICredential struct {
	Base
	OwnerType    string     `json:"owner_type" gorm:"index;size:24"`
	OwnerID      *uuid.UUID `json:"owner_id" gorm:"type:uuid;index"`
	Name         string     `json:"name" gorm:"size:100;not null"`
	Key          string     `json:"key" gorm:"uniqueIndex;size:100;not null"`
	SecretCipher []byte     `json:"-" gorm:"not null"`
	SecretNonce  []byte     `json:"-" gorm:"not null"`
	Permissions  string     `json:"permissions" gorm:"size:500"`
	Status       string     `json:"status" gorm:"size:20;not null;default:active"`
	LastUsedAt   *time.Time `json:"last_used_at"`
	RevokedAt    *time.Time `json:"revoked_at,omitempty" gorm:"index"`
}

type WebhookEvent struct {
	Base
	EventID            string     `json:"event_id" gorm:"uniqueIndex;size:100;not null"`
	EventType          string     `json:"event_type" gorm:"index;size:80;not null"`
	Endpoint           string     `json:"endpoint" gorm:"size:500;not null"`
	Payload            string     `json:"-" gorm:"type:text;not null"`
	PayloadCipher      []byte     `json:"-"`
	PayloadNonce       []byte     `json:"-"`
	SupplierID         *uuid.UUID `json:"supplier_id,omitempty" gorm:"type:uuid;index"`
	ProcurementOrderID *uuid.UUID `json:"procurement_order_id,omitempty" gorm:"type:uuid;index"`
	Status             string     `json:"status" gorm:"size:20;index;not null"`
	Attempts           int        `json:"attempts" gorm:"not null;default:0"`
	Response           string     `json:"response" gorm:"type:text"`
	ProcessedAt        *time.Time `json:"processed_at"`
}

type AuditLog struct {
	Base
	AdminID    *uuid.UUID `json:"admin_id" gorm:"type:uuid;index"`
	Action     string     `json:"action" gorm:"index;size:100;not null"`
	Resource   string     `json:"resource" gorm:"size:100"`
	ResourceID string     `json:"resource_id" gorm:"size:100"`
	IP         string     `json:"ip" gorm:"size:64"`
	Detail     string     `json:"detail" gorm:"type:text"`
}

type Setting struct {
	Key       string    `json:"key" gorm:"primaryKey;size:120"`
	Value     string    `json:"value" gorm:"type:text"`
	Group     string    `json:"group" gorm:"index;size:60"`
	UpdatedAt time.Time `json:"updated_at"`
}
