package model

import (
	"time"

	"github.com/google/uuid"
)

// CurrencyDefinition uses ISO 4217 codes and records the minor-unit exponent
// used by all integer amount columns. The exponent is immutable once the
// currency has financial records.
type CurrencyDefinition struct {
	Base
	Code        string `json:"code" gorm:"uniqueIndex;size:3;not null"`
	NumericCode string `json:"numeric_code" gorm:"size:3"`
	Name        string `json:"name" gorm:"size:100;not null"`
	Symbol      string `json:"symbol" gorm:"size:16;not null"`
	MinorUnit   int    `json:"minor_unit" gorm:"not null"`
	Enabled     bool   `json:"enabled" gorm:"index;not null;default:true"`
	Settlement  bool   `json:"settlement" gorm:"index;not null;default:false"`
	DisplaySort int    `json:"display_sort" gorm:"not null;default:0"`
}

// FXProviderConfig is a credential-free or encrypted-credential provider
// definition. ProviderKey identifies an official source when a free gateway
// exposes multiple central-bank feeds.
type FXProviderConfig struct {
	Base
	Code             string     `json:"code" gorm:"uniqueIndex;size:80;not null"`
	Name             string     `json:"name" gorm:"size:160;not null"`
	Driver           string     `json:"driver" gorm:"size:60;index;not null"`
	ProviderKey      string     `json:"provider_key" gorm:"size:60"`
	BaseURL          string     `json:"base_url" gorm:"size:500;not null"`
	CredentialCipher []byte     `json:"-"`
	CredentialNonce  []byte     `json:"-"`
	Priority         int        `json:"priority" gorm:"index;not null;default:100"`
	Enabled          bool       `json:"enabled" gorm:"index;not null;default:true"`
	TimeoutSeconds   int        `json:"timeout_seconds" gorm:"not null;default:8"`
	FailureCount     int        `json:"failure_count" gorm:"not null;default:0"`
	LastSuccessAt    *time.Time `json:"last_success_at,omitempty"`
	LastFailureAt    *time.Time `json:"last_failure_at,omitempty"`
	LastError        string     `json:"last_error" gorm:"size:1000"`
}

// FXRateObservation stores every normalized provider response. Rate is quote
// major units for one base major unit and is persisted as an exact decimal.
type FXRateObservation struct {
	Base
	ProviderID uuid.UUID `json:"provider_id" gorm:"type:uuid;index;not null"`
	BaseCode   string    `json:"base_code" gorm:"size:3;index;not null"`
	QuoteCode  string    `json:"quote_code" gorm:"size:3;index;not null"`
	Rate       string    `json:"rate" gorm:"type:numeric(38,18);not null"`
	ObservedAt time.Time `json:"observed_at" gorm:"index;not null"`
	FetchedAt  time.Time `json:"fetched_at" gorm:"index;not null"`
	RawHash    string    `json:"raw_hash" gorm:"size:64;not null"`
	Accepted   bool      `json:"accepted" gorm:"index;not null;default:false"`
	RejectCode string    `json:"reject_code" gorm:"size:60"`
}

type FXManualRate struct {
	Base
	BaseCode  string     `json:"base_code" gorm:"size:3;index;not null"`
	QuoteCode string     `json:"quote_code" gorm:"size:3;index;not null"`
	Rate      string     `json:"rate" gorm:"type:numeric(38,18);not null"`
	Enabled   bool       `json:"enabled" gorm:"index;not null;default:true"`
	ValidFrom time.Time  `json:"valid_from" gorm:"index;not null"`
	ValidTo   *time.Time `json:"valid_to,omitempty" gorm:"index"`
	Reason    string     `json:"reason" gorm:"size:500;not null"`
	UpdatedBy uuid.UUID  `json:"updated_by" gorm:"type:uuid;index;not null"`
}

// FXRateSnapshot is immutable and referenced by product sync and orders. A
// cached fallback creates a new snapshot pointing to the trusted parent so the
// exact fallback decision remains auditable.
type FXRateSnapshot struct {
	Base
	BaseCode         string     `json:"base_code" gorm:"size:3;index;not null"`
	QuoteCode        string     `json:"quote_code" gorm:"size:3;index;not null"`
	Rate             string     `json:"rate" gorm:"type:numeric(38,18);not null"`
	SourceTier       string     `json:"source_tier" gorm:"size:20;index;not null"`
	ProviderID       *uuid.UUID `json:"provider_id,omitempty" gorm:"type:uuid;index"`
	ManualRateID     *uuid.UUID `json:"manual_rate_id,omitempty" gorm:"type:uuid;index"`
	ParentSnapshotID *uuid.UUID `json:"parent_snapshot_id,omitempty" gorm:"type:uuid;index"`
	ObservedAt       time.Time  `json:"observed_at" gorm:"index;not null"`
	SelectedAt       time.Time  `json:"selected_at" gorm:"index;not null"`
	ExpiresAt        time.Time  `json:"expires_at" gorm:"index;not null"`
	StaleAfter       time.Time  `json:"stale_after" gorm:"index;not null"`
	ConsensusCount   int        `json:"consensus_count" gorm:"not null;default:1"`
	Decision         string     `json:"decision" gorm:"size:500;not null"`
}
