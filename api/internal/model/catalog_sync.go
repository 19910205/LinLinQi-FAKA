package model

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// CatalogMedia binds an immutable media asset to a catalog record. SourceURL
// is retained for provenance; clients receive only the mirrored PublicURL.
type CatalogMedia struct {
	Base
	OwnerType    string     `json:"owner_type" gorm:"size:20;index;not null"`
	OwnerID      uuid.UUID  `json:"owner_id" gorm:"type:uuid;index;not null"`
	AssetID      uuid.UUID  `json:"asset_id" gorm:"type:uuid;index;not null"`
	Asset        MediaAsset `json:"asset" gorm:"foreignKey:AssetID"`
	Role         string     `json:"role" gorm:"size:20;index;not null"`
	Sort         int        `json:"sort" gorm:"not null;default:0"`
	AltText      string     `json:"alt_text" gorm:"size:300"`
	SourceURL    string     `json:"-" gorm:"size:1000"`
	SourceHash   string     `json:"-" gorm:"size:64;index"`
	SourceType   string     `json:"source_type" gorm:"size:30;not null;default:manual"`
	MirrorStatus string     `json:"mirror_status" gorm:"size:24;index;not null;default:ready"`
	MirroredAt   *time.Time `json:"mirrored_at,omitempty"`
}

// SupplierSyncPolicy is deliberately explicit so operators can audit and
// control every field that an upstream is allowed to change.
type SupplierSyncPolicy struct {
	Base
	SupplierID           uuid.UUID `json:"supplier_id" gorm:"type:uuid;uniqueIndex;not null"`
	Enabled              bool      `json:"enabled" gorm:"not null;default:true"`
	AutoSyncCategories   bool      `json:"auto_sync_categories" gorm:"not null;default:false"`
	AutoCreateCategories bool      `json:"auto_create_categories" gorm:"not null;default:false"`
	AutoSyncProducts     bool      `json:"auto_sync_products" gorm:"not null;default:false"`
	AutoCreateProducts   bool      `json:"auto_create_products" gorm:"not null;default:false"`
	SyncTitle            bool      `json:"sync_title" gorm:"not null;default:false"`
	SyncSummary          bool      `json:"sync_summary" gorm:"not null;default:false"`
	SyncDescription      bool      `json:"sync_description" gorm:"not null;default:false"`
	SyncMedia            bool      `json:"sync_media" gorm:"not null;default:false"`
	MirrorRemoteMedia    bool      `json:"mirror_remote_media" gorm:"not null;default:true"`
	SyncPrice            bool      `json:"sync_price" gorm:"not null;default:true"`
	SyncStock            bool      `json:"sync_stock" gorm:"not null;default:true"`
	SyncVariants         bool      `json:"sync_variants" gorm:"not null;default:false"`
	SyncStatus           bool      `json:"sync_status" gorm:"not null;default:false"`
	SyncPurchaseLimits   bool      `json:"sync_purchase_limits" gorm:"not null;default:false"`
	MissingProductAction string    `json:"missing_product_action" gorm:"size:24;not null;default:keep"`
}

// SupplierCatalogImportJob is the durable operator-facing record for a batch
// catalog import. RequestSnapshot contains catalog identifiers and switches,
// never supplier credentials.  The queue task carries only this record's ID so
// retries always execute the exact immutable request that was approved.
type SupplierCatalogImportJob struct {
	Base
	SupplierID         uuid.UUID       `json:"supplier_id" gorm:"type:uuid;index;not null"`
	RequestedBy        *uuid.UUID      `json:"requested_by,omitempty" gorm:"type:uuid;index"`
	TaskID             string          `json:"task_id,omitempty" gorm:"size:100;index"`
	Status             string          `json:"status" gorm:"size:24;index;not null;default:queued"`
	Attempts           int             `json:"attempts" gorm:"not null;default:0"`
	RequestedCount     int             `json:"requested_count" gorm:"not null;default:0"`
	ImportedCount      int             `json:"imported_count" gorm:"not null;default:0"`
	SkippedCount       int             `json:"skipped_count" gorm:"not null;default:0"`
	CategoriesCreated  int             `json:"categories_created" gorm:"not null;default:0"`
	MappingsConfigured int             `json:"mappings_configured" gorm:"not null;default:0"`
	RequestSnapshot    json.RawMessage `json:"-" gorm:"type:jsonb;not null;default:'{}'"`
	ResultSnapshot     json.RawMessage `json:"result" gorm:"type:jsonb;not null;default:'{}'"`
	ErrorSummary       string          `json:"error_summary" gorm:"type:text"`
	StartedAt          *time.Time      `json:"started_at,omitempty" gorm:"index"`
	CompletedAt        *time.Time      `json:"completed_at,omitempty" gorm:"index"`
	NextAttemptAt      *time.Time      `json:"next_attempt_at,omitempty" gorm:"index"`
}

type SupplierCategory struct {
	Base
	SupplierID       uuid.UUID       `json:"supplier_id" gorm:"type:uuid;index;not null"`
	ExternalID       string          `json:"external_id" gorm:"size:180;not null"`
	ExternalParentID string          `json:"external_parent_id" gorm:"size:180"`
	Name             string          `json:"name" gorm:"size:200;not null"`
	Description      string          `json:"description" gorm:"type:text"`
	ImageURL         string          `json:"image_url" gorm:"size:1000"`
	Sort             int             `json:"sort" gorm:"not null;default:0"`
	Status           string          `json:"status" gorm:"size:24;index;not null;default:active"`
	RawSnapshot      json.RawMessage `json:"-" gorm:"type:jsonb;not null;default:'{}'"`
	SnapshotHash     string          `json:"snapshot_hash" gorm:"size:64;not null"`
	LastSeenAt       time.Time       `json:"last_seen_at" gorm:"index;not null"`
}

type SupplierCatalogProduct struct {
	Base
	SupplierID         uuid.UUID       `json:"supplier_id" gorm:"type:uuid;index;not null"`
	ExternalID         string          `json:"external_id" gorm:"size:180;not null"`
	ParentExternalID   string          `json:"parent_external_id" gorm:"size:180;index"`
	ExternalCategoryID string          `json:"external_category_id" gorm:"size:180;index"`
	ExternalSKU        string          `json:"external_sku" gorm:"size:180"`
	Name               string          `json:"name" gorm:"size:240;not null"`
	Summary            string          `json:"summary" gorm:"size:1000"`
	Description        string          `json:"description" gorm:"type:text"`
	CoverURL           string          `json:"cover_url" gorm:"size:1000"`
	ImageURLs          json.RawMessage `json:"image_urls" gorm:"type:jsonb;not null;default:'[]'"`
	Country            string          `json:"country" gorm:"size:8;index"`
	Tags               json.RawMessage `json:"tags" gorm:"type:jsonb;not null;default:'[]'"`
	Currency           string          `json:"currency" gorm:"size:3;index;not null;default:CNY"`
	Price              int64           `json:"price" gorm:"not null;default:0"`
	OriginalPrice      int64           `json:"original_price" gorm:"not null;default:0"`
	MemberPrice        int64           `json:"member_price" gorm:"not null;default:0"`
	WholesalePrices    json.RawMessage `json:"wholesale_prices" gorm:"type:jsonb;not null;default:'{}'"`
	Stock              int64           `json:"stock" gorm:"not null;default:0"`
	StockStatus        string          `json:"stock_status" gorm:"size:24;index;not null;default:unknown"`
	Minimum            int             `json:"minimum" gorm:"not null;default:1"`
	Maximum            int             `json:"maximum" gorm:"not null;default:0"`
	FulfillmentType    string          `json:"fulfillment_type" gorm:"size:24;index"`
	Status             string          `json:"status" gorm:"size:24;index;not null;default:active"`
	UpstreamCreatedAt  *time.Time      `json:"upstream_created_at,omitempty" gorm:"index"`
	UpstreamUpdatedAt  *time.Time      `json:"upstream_updated_at,omitempty" gorm:"index"`
	Variants           json.RawMessage `json:"variants" gorm:"type:jsonb;not null;default:'[]'"`
	InputFields        json.RawMessage `json:"input_fields" gorm:"type:jsonb;not null;default:'[]'"`
	RawSnapshot        json.RawMessage `json:"-" gorm:"type:jsonb;not null;default:'{}'"`
	SnapshotHash       string          `json:"snapshot_hash" gorm:"size:64;not null"`
	LastSeenAt         time.Time       `json:"last_seen_at" gorm:"index;not null"`
}

type SupplierCategoryMapping struct {
	Base
	SupplierID           uuid.UUID  `json:"supplier_id" gorm:"type:uuid;index;not null"`
	ExternalCategoryID   string     `json:"external_category_id" gorm:"size:180;not null"`
	ExternalCategoryName string     `json:"external_category_name" gorm:"size:200"`
	CategoryID           *uuid.UUID `json:"category_id,omitempty" gorm:"type:uuid;index"`
	DefaultCoverURL      string     `json:"default_cover_url" gorm:"size:1000"`
	AutoCreate           bool       `json:"auto_create" gorm:"not null;default:false"`
	AutoPublish          bool       `json:"auto_publish" gorm:"not null;default:false"`
	SyncName             bool       `json:"sync_name" gorm:"not null;default:false"`
	SyncTitle            bool       `json:"sync_title" gorm:"not null;default:false"`
	SyncDescription      bool       `json:"sync_description" gorm:"not null;default:false"`
	SyncImage            bool       `json:"sync_image" gorm:"not null;default:false"`
	MirrorRemoteImage    bool       `json:"mirror_remote_image" gorm:"not null;default:true"`
	SyncParent           bool       `json:"sync_parent" gorm:"not null;default:false"`
	SyncPrice            bool       `json:"sync_price" gorm:"not null;default:true"`
	SyncStock            bool       `json:"sync_stock" gorm:"not null;default:true"`
	PriceMode            string     `json:"price_mode" gorm:"size:24;not null;default:fixed_markup"`
	MarkupBasisPoint     int        `json:"markup_basis_point" gorm:"not null;default:0"`
	MarkupAmount         int64      `json:"markup_amount" gorm:"not null;default:0"`
	MarkupCurrency       string     `json:"markup_currency" gorm:"size:3;not null;default:CNY"`
	Sort                 int        `json:"sort" gorm:"not null;default:0"`
	Enabled              bool       `json:"enabled" gorm:"index;not null;default:true"`
	LastSyncedAt         *time.Time `json:"last_synced_at,omitempty"`
	LastError            string     `json:"last_error" gorm:"type:text"`
}

type SupplierSyncRun struct {
	Base
	SupplierID      uuid.UUID  `json:"supplier_id" gorm:"type:uuid;index;not null"`
	Trigger         string     `json:"trigger" gorm:"size:24;index;not null"`
	Status          string     `json:"status" gorm:"size:24;index;not null"`
	Protocol        string     `json:"protocol" gorm:"size:60;not null"`
	CategoriesSeen  int        `json:"categories_seen" gorm:"not null;default:0"`
	CategoriesMade  int        `json:"categories_created" gorm:"column:categories_created;not null;default:0"`
	ProductsSeen    int        `json:"products_seen" gorm:"not null;default:0"`
	ProductsMade    int        `json:"products_created" gorm:"column:products_created;not null;default:0"`
	ProductsUpdated int        `json:"products_updated" gorm:"not null;default:0"`
	MediaMirrored   int        `json:"media_mirrored" gorm:"not null;default:0"`
	FXSnapshotID    *uuid.UUID `json:"fx_snapshot_id,omitempty" gorm:"type:uuid;index"`
	Warnings        int        `json:"warnings" gorm:"not null;default:0"`
	ErrorSummary    string     `json:"error_summary" gorm:"type:text"`
	StartedAt       time.Time  `json:"started_at" gorm:"index;not null"`
	CompletedAt     *time.Time `json:"completed_at,omitempty"`
}

type SupplierSyncChange struct {
	Base
	RunID         uuid.UUID       `json:"run_id" gorm:"type:uuid;index;not null"`
	EntityType    string          `json:"entity_type" gorm:"size:24;index;not null"`
	ExternalID    string          `json:"external_id" gorm:"size:180;index;not null"`
	LocalID       *uuid.UUID      `json:"local_id,omitempty" gorm:"type:uuid;index"`
	Action        string          `json:"action" gorm:"size:24;index;not null"`
	ChangedFields json.RawMessage `json:"changed_fields" gorm:"type:jsonb;not null;default:'[]'"`
	Applied       bool            `json:"applied" gorm:"not null;default:false"`
	Message       string          `json:"message" gorm:"size:1000"`
}

// ResellerCategoryRule and ResellerProductPresentation implement inherited
// storefront presentation without duplicating the platform catalog.
type ResellerCategoryRule struct {
	Base
	ResellerID uuid.UUID `json:"reseller_id" gorm:"type:uuid;index;not null"`
	CategoryID uuid.UUID `json:"category_id" gorm:"type:uuid;index;not null"`
	Enabled    bool      `json:"enabled" gorm:"not null;default:true"`
	Name       string    `json:"name" gorm:"size:100"`
	ImageURL   string    `json:"image_url" gorm:"size:1000"`
	Sort       int       `json:"sort" gorm:"not null;default:0"`
}

type ResellerProductPresentation struct {
	Base
	ResellerID  uuid.UUID `json:"reseller_id" gorm:"type:uuid;index;not null"`
	ProductID   uuid.UUID `json:"product_id" gorm:"type:uuid;index;not null"`
	Name        string    `json:"name" gorm:"size:160"`
	Summary     string    `json:"summary" gorm:"size:500"`
	Description string    `json:"description" gorm:"type:text"`
	CoverURL    string    `json:"cover_url" gorm:"size:1000"`
}
