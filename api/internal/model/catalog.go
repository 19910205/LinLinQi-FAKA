package model

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// ProductInputField defines one piece of information that must be collected
// for a product before an order can be created. Options is used only by select
// fields and is kept as JSON so the public, admin and OpenAPI contracts share
// exactly the same ordered values.
type ProductInputField struct {
	Base
	ProductID         uuid.UUID       `json:"product_id" gorm:"type:uuid;index;not null"`
	Key               string          `json:"key" gorm:"size:64;not null"`
	Label             string          `json:"label" gorm:"size:120;not null"`
	InputType         string          `json:"input_type" gorm:"size:20;not null;default:text"`
	Required          bool            `json:"required" gorm:"not null;default:false"`
	Sensitive         bool            `json:"sensitive" gorm:"not null;default:false"`
	PassToSupplier    bool            `json:"pass_to_supplier" gorm:"not null;default:false"`
	Placeholder       string          `json:"placeholder" gorm:"size:200"`
	HelpText          string          `json:"help_text" gorm:"size:500"`
	Options           json.RawMessage `json:"options" gorm:"type:jsonb;not null;default:'[]'"`
	ValidationPattern string          `json:"validation_pattern" gorm:"size:300"`
	MinLength         int             `json:"min_length" gorm:"not null;default:0"`
	MaxLength         int             `json:"max_length" gorm:"not null;default:200"`
	Sort              int             `json:"sort" gorm:"not null;default:0"`
	Enabled           bool            `json:"enabled" gorm:"not null;default:true"`
}

// OrderInputValue is an immutable encrypted snapshot of a submitted checkout
// value. The product field may later be changed or deleted without changing
// what was collected for the order.
type OrderInputValue struct {
	Base
	OrderID             uuid.UUID  `json:"order_id" gorm:"type:uuid;index;not null"`
	ProductID           uuid.UUID  `json:"product_id" gorm:"type:uuid;index;not null"`
	VariantID           *uuid.UUID `json:"variant_id,omitempty" gorm:"type:uuid;index"`
	ProductInputFieldID *uuid.UUID `json:"product_input_field_id,omitempty" gorm:"type:uuid;index"`
	Key                 string     `json:"key" gorm:"size:64;not null"`
	Label               string     `json:"label" gorm:"size:120;not null"`
	InputType           string     `json:"input_type" gorm:"size:20;not null"`
	Sensitive           bool       `json:"sensitive" gorm:"not null;default:false"`
	PassToSupplier      bool       `json:"pass_to_supplier" gorm:"not null;default:false"`
	ValueCipher         []byte     `json:"-" gorm:"not null"`
	ValueNonce          []byte     `json:"-" gorm:"not null"`
	ValuePreview        string     `json:"value_preview" gorm:"size:200;not null"`
}

type ProductVariant struct {
	Base
	ProductID     uuid.UUID `json:"product_id" gorm:"type:uuid;index;not null"`
	SKU           string    `json:"sku" gorm:"uniqueIndex;size:100;not null"`
	Name          string    `json:"name" gorm:"size:160;not null"`
	Attributes    string    `json:"attributes" gorm:"type:jsonb;default:'{}'"`
	Price         int64     `json:"price" gorm:"not null"`
	ComparePrice  int64     `json:"compare_price" gorm:"not null;default:0"`
	CostPrice     int64     `json:"cost_price" gorm:"not null;default:0"`
	Status        string    `json:"status" gorm:"index;size:24;not null;default:active"`
	Sort          int       `json:"sort" gorm:"not null;default:0"`
	PurchaseLimit int       `json:"purchase_limit" gorm:"not null;default:0"`
}

type ProductPriceTier struct {
	Base
	ProductID     uuid.UUID  `json:"product_id" gorm:"type:uuid;index;not null"`
	VariantID     *uuid.UUID `json:"variant_id" gorm:"type:uuid;index"`
	MemberLevelID *uuid.UUID `json:"member_level_id" gorm:"type:uuid;index"`
	MinQuantity   int        `json:"min_quantity" gorm:"not null;default:1"`
	UnitPrice     int64      `json:"unit_price" gorm:"not null"`
	StartsAt      *time.Time `json:"starts_at"`
	EndsAt        *time.Time `json:"ends_at"`
}

type ProductPaymentChannel struct {
	ProductID uuid.UUID `json:"product_id" gorm:"type:uuid;primaryKey"`
	ChannelID uuid.UUID `json:"channel_id" gorm:"type:uuid;primaryKey"`
}

type InventoryBatch struct {
	Base
	ProductID    uuid.UUID  `json:"product_id" gorm:"type:uuid;index;not null"`
	VariantID    *uuid.UUID `json:"variant_id" gorm:"type:uuid;index"`
	BatchNo      string     `json:"batch_no" gorm:"uniqueIndex;size:80;not null"`
	Source       string     `json:"source" gorm:"size:30;not null"`
	TotalCount   int        `json:"total_count" gorm:"not null"`
	ValidCount   int        `json:"valid_count" gorm:"not null"`
	InvalidCount int        `json:"invalid_count" gorm:"not null;default:0"`
	ImportedBy   *uuid.UUID `json:"imported_by" gorm:"type:uuid;index"`
	ExpiresAt    *time.Time `json:"expires_at"`
}

type Cart struct {
	Base
	UserID     *uuid.UUID `json:"user_id" gorm:"type:uuid;index"`
	ResellerID *uuid.UUID `json:"reseller_id,omitempty" gorm:"type:uuid;index"`
	GuestToken string     `json:"guest_token" gorm:"uniqueIndex;size:100"`
	Currency   string     `json:"currency" gorm:"size:8;not null;default:CNY"`
	ExpiresAt  time.Time  `json:"expires_at" gorm:"index"`
	Items      []CartItem `json:"items"`
}

type CartItem struct {
	Base
	CartID          uuid.UUID  `json:"cart_id" gorm:"type:uuid;uniqueIndex:idx_cart_item;not null"`
	ProductID       uuid.UUID  `json:"product_id" gorm:"type:uuid;uniqueIndex:idx_cart_item;not null"`
	VariantID       *uuid.UUID `json:"variant_id" gorm:"type:uuid;uniqueIndex:idx_cart_item"`
	Quantity        int        `json:"quantity" gorm:"not null"`
	SelectedCardIDs string     `json:"selected_card_ids" gorm:"type:jsonb;default:'[]'"`
}

type ProductMapping struct {
	Base
	SupplierID                uuid.UUID       `json:"supplier_id" gorm:"type:uuid;index;not null"`
	SupplierCategoryMappingID *uuid.UUID      `json:"supplier_category_mapping_id,omitempty" gorm:"type:uuid;index"`
	InheritCategoryPolicy     bool            `json:"inherit_category_policy" gorm:"index;not null;default:false"`
	ProductID                 uuid.UUID       `json:"product_id" gorm:"type:uuid;index;not null"`
	VariantID                 *uuid.UUID      `json:"variant_id" gorm:"type:uuid;index"`
	ExternalProductID         string          `json:"external_product_id" gorm:"size:180;not null"`
	ParameterMapping          json.RawMessage `json:"parameter_mapping" gorm:"type:jsonb;not null;default:'{}'"`
	PriceMode                 string          `json:"price_mode" gorm:"size:24;not null;default:fixed_markup"`
	MarkupBasisPoint          int             `json:"markup_basis_point" gorm:"not null;default:0"`
	MarkupAmount              int64           `json:"markup_amount" gorm:"not null;default:0"`
	MarkupCurrency            string          `json:"markup_currency" gorm:"size:3;index;not null;default:CNY"`
	FixedPrice                int64           `json:"fixed_price" gorm:"not null;default:0"`
	FixedPriceCurrency        string          `json:"fixed_price_currency" gorm:"size:3;index;not null;default:CNY"`
	LastUpstreamPrice         int64           `json:"last_upstream_price" gorm:"not null;default:0"`
	LastUpstreamCurrency      string          `json:"last_upstream_currency" gorm:"size:3"`
	LastConvertedCost         int64           `json:"last_converted_cost" gorm:"not null;default:0"`
	LastFXSnapshotID          *uuid.UUID      `json:"last_fx_snapshot_id,omitempty" gorm:"type:uuid;index"`
	LastPriceFXSnapshotID     *uuid.UUID      `json:"last_price_fx_snapshot_id,omitempty" gorm:"type:uuid;index"`
	AutoSyncPrice             bool            `json:"auto_sync_price" gorm:"not null;default:true"`
	AutoSyncStock             bool            `json:"auto_sync_stock" gorm:"not null;default:true"`
	AutoSyncTitle             bool            `json:"auto_sync_title" gorm:"not null;default:false"`
	AutoSyncSummary           bool            `json:"auto_sync_summary" gorm:"not null;default:false"`
	AutoSyncDescription       bool            `json:"auto_sync_description" gorm:"not null;default:false"`
	AutoSyncMedia             bool            `json:"auto_sync_media" gorm:"not null;default:false"`
	MirrorRemoteMedia         bool            `json:"mirror_remote_media" gorm:"not null;default:true"`
	AutoSyncCategory          bool            `json:"auto_sync_category" gorm:"not null;default:false"`
	AutoSyncVariants          bool            `json:"auto_sync_variants" gorm:"not null;default:false"`
	AutoSyncStatus            bool            `json:"auto_sync_status" gorm:"not null;default:false"`
	AutoSyncLimits            bool            `json:"auto_sync_limits" gorm:"not null;default:false"`
	LastSyncedAt              *time.Time      `json:"last_synced_at"`
	LastError                 string          `json:"last_error" gorm:"type:text"`
}
