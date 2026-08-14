package model

import (
	"time"

	"github.com/google/uuid"
)

type ProcurementOrder struct {
	Base
	ProcurementNo        string     `json:"procurement_no" gorm:"uniqueIndex;size:80;not null"`
	SupplierID           uuid.UUID  `json:"supplier_id" gorm:"type:uuid;index;not null"`
	OrderID              uuid.UUID  `json:"order_id" gorm:"type:uuid;index;not null"`
	OrderItemID          uuid.UUID  `json:"order_item_id" gorm:"type:uuid;uniqueIndex:idx_procurement_order_item;not null"`
	ExternalOrderNo      string     `json:"external_order_no" gorm:"index;size:160"`
	ExternalProductID    string     `json:"external_product_id" gorm:"size:180;not null"`
	Quantity             int        `json:"quantity" gorm:"not null"`
	CostAmount           int64      `json:"cost_amount" gorm:"not null"`
	CostCurrency         string     `json:"cost_currency" gorm:"size:3;not null;default:CNY"`
	UpstreamCostAmount   int64      `json:"upstream_cost_amount" gorm:"not null;default:0"`
	UpstreamCurrency     string     `json:"upstream_currency" gorm:"size:3"`
	FXSnapshotID         *uuid.UUID `json:"fx_snapshot_id,omitempty" gorm:"type:uuid;index"`
	Status               string     `json:"status" gorm:"index;size:24;not null"`
	Attempts             int        `json:"attempts" gorm:"not null;default:0"`
	RequestBody          string     `json:"-" gorm:"type:jsonb;default:'{}'"`
	ResponseBody         string     `json:"-" gorm:"type:jsonb;default:'{}'"`
	CallbackSecretCipher []byte     `json:"-"`
	CallbackSecretNonce  []byte     `json:"-"`
	NextPollAt           *time.Time `json:"next_poll_at" gorm:"index"`
	CompletedAt          *time.Time `json:"completed_at"`
}

// SupplierInventoryReservation is a durable local hold against the current
// upstream stock observation. It prevents concurrent paid/pending orders from
// selecting the same unit. Consumption atomically debits SupplierProduct's
// current observation; the next authoritative supplier sync replaces that
// remaining-stock snapshot instead of subtracting the historical consumption
// again.
type SupplierInventoryReservation struct {
	Base
	OrderID           uuid.UUID  `json:"order_id" gorm:"type:uuid;index;not null"`
	OrderItemID       uuid.UUID  `json:"order_item_id" gorm:"type:uuid;uniqueIndex;not null"`
	SupplierID        uuid.UUID  `json:"supplier_id" gorm:"type:uuid;index;not null"`
	SupplierProductID uuid.UUID  `json:"supplier_product_id" gorm:"type:uuid;index;not null"`
	ProductMappingID  uuid.UUID  `json:"product_mapping_id" gorm:"type:uuid;index;not null"`
	ExternalProductID string     `json:"external_product_id" gorm:"size:180;index;not null"`
	Quantity          int        `json:"quantity" gorm:"not null"`
	Status            string     `json:"status" gorm:"size:16;index;not null;default:reserved"`
	ExpiresAt         time.Time  `json:"expires_at" gorm:"index;not null"`
	ConsumedAt        *time.Time `json:"consumed_at,omitempty"`
	ReleasedAt        *time.Time `json:"released_at,omitempty"`
	ReleaseReason     string     `json:"release_reason,omitempty" gorm:"size:120"`
}

type SiteConnection struct {
	Base
	Name             string     `json:"name" gorm:"size:140;not null"`
	Kind             string     `json:"kind" gorm:"index;size:30;not null"`
	BaseURL          string     `json:"base_url" gorm:"size:500;not null"`
	CredentialCipher []byte     `json:"-"`
	CredentialNonce  []byte     `json:"-"`
	Status           string     `json:"status" gorm:"index;size:24;not null"`
	Capabilities     string     `json:"capabilities" gorm:"type:jsonb;default:'[]'"`
	LastCheckedAt    *time.Time `json:"last_checked_at"`
	LastError        string     `json:"last_error" gorm:"type:text"`
}

type CallbackRoute struct {
	Base
	Name          string     `json:"name" gorm:"size:140;not null"`
	EventType     string     `json:"event_type" gorm:"index;size:80;not null"`
	Endpoint      string     `json:"endpoint" gorm:"size:500;not null"`
	SecretCipher  []byte     `json:"-"`
	SecretNonce   []byte     `json:"-"`
	Enabled       bool       `json:"enabled" gorm:"index;not null;default:true"`
	MaxAttempts   int        `json:"max_attempts" gorm:"not null;default:12"`
	TimeoutSecond int        `json:"timeout_second" gorm:"not null;default:10"`
	LastSuccessAt *time.Time `json:"last_success_at"`
}
