package model

import (
	"time"

	"github.com/google/uuid"
)

type PaymentIntent struct {
	Base
	OrderID         uuid.UUID  `json:"order_id" gorm:"type:uuid;index;not null"`
	IntentNo        string     `json:"intent_no" gorm:"uniqueIndex;size:80;not null"`
	ChannelID       uuid.UUID  `json:"channel_id" gorm:"type:uuid;index;not null"`
	Amount          int64      `json:"amount" gorm:"not null"`
	Currency        string     `json:"currency" gorm:"size:8;not null;default:CNY"`
	OrderAmount     int64      `json:"order_amount" gorm:"not null;default:0"`
	OrderCurrency   string     `json:"order_currency" gorm:"size:3;not null;default:CNY"`
	FXSnapshotID    *uuid.UUID `json:"fx_snapshot_id,omitempty" gorm:"type:uuid;index"`
	Status          string     `json:"status" gorm:"index;size:24;not null"`
	ProviderTradeNo string     `json:"provider_trade_no" gorm:"index;size:160"`
	CheckoutURL     string     `json:"checkout_url" gorm:"type:text"`
	ExpiresAt       time.Time  `json:"expires_at" gorm:"index"`
	SucceededAt     *time.Time `json:"succeeded_at"`
}

// StorefrontOrderRequest is the durable idempotency receipt for public
// storefront orders. IdempotencyHash already includes the authenticated user
// or normalized guest identity, so one customer's key can never reveal or
// collide with another customer's order.
type StorefrontOrderRequest struct {
	Base
	IdempotencyHash string    `json:"-" gorm:"uniqueIndex;size:64;not null"`
	RequestHash     string    `json:"-" gorm:"size:64;not null"`
	ClientOrderNo   string    `json:"client_order_no,omitempty" gorm:"size:100"`
	OrderID         uuid.UUID `json:"order_id" gorm:"type:uuid;uniqueIndex;not null"`
}

type PaymentTransaction struct {
	Base
	PaymentIntentID uuid.UUID `json:"payment_intent_id" gorm:"type:uuid;index;not null"`
	Direction       string    `json:"direction" gorm:"size:20;not null"`
	ProviderEventID string    `json:"provider_event_id" gorm:"uniqueIndex;size:190;not null"`
	Amount          int64     `json:"amount" gorm:"not null"`
	Fee             int64     `json:"fee" gorm:"not null;default:0"`
	Currency        string    `json:"currency" gorm:"size:3;index;not null;default:CNY"`
	Status          string    `json:"status" gorm:"index;size:24;not null"`
	RawPayload      string    `json:"-" gorm:"type:jsonb;default:'{}'"`
}

type Refund struct {
	Base
	RefundNo         string     `json:"refund_no" gorm:"uniqueIndex;size:80;not null"`
	OrderID          uuid.UUID  `json:"order_id" gorm:"type:uuid;index;not null"`
	PaymentIntentID  *uuid.UUID `json:"payment_intent_id" gorm:"type:uuid;index"`
	Amount           int64      `json:"amount" gorm:"not null"`
	Currency         string     `json:"currency" gorm:"size:3;not null;default:CNY"`
	OrderAmount      int64      `json:"order_amount" gorm:"not null;default:0"`
	OrderCurrency    string     `json:"order_currency" gorm:"size:3;not null;default:CNY"`
	Reason           string     `json:"reason" gorm:"size:500"`
	Status           string     `json:"status" gorm:"index;size:24;not null"`
	Attempts         int        `json:"attempts" gorm:"not null;default:0"`
	NextAttemptAt    *time.Time `json:"next_attempt_at" gorm:"index"`
	RequestedBy      string     `json:"requested_by" gorm:"size:30;not null"`
	ProviderRefundNo string     `json:"provider_refund_no" gorm:"size:160"`
	ProcessedAt      *time.Time `json:"processed_at"`
}

type OrderEvent struct {
	Base
	OrderID    uuid.UUID  `json:"order_id" gorm:"type:uuid;index;not null"`
	FromStatus string     `json:"from_status" gorm:"size:24"`
	ToStatus   string     `json:"to_status" gorm:"size:24;not null"`
	ActorType  string     `json:"actor_type" gorm:"size:24;not null"`
	ActorID    *uuid.UUID `json:"actor_id" gorm:"type:uuid;index"`
	Reason     string     `json:"reason" gorm:"size:500"`
	Metadata   string     `json:"metadata" gorm:"type:jsonb;default:'{}'"`
}

type FulfillmentAttempt struct {
	Base
	OrderID       uuid.UUID  `json:"order_id" gorm:"type:uuid;index;not null"`
	OrderItemID   uuid.UUID  `json:"order_item_id" gorm:"type:uuid;index;not null"`
	Mode          string     `json:"mode" gorm:"size:24;not null"`
	Attempt       int        `json:"attempt" gorm:"not null"`
	Status        string     `json:"status" gorm:"index;size:24;not null"`
	SupplierID    *uuid.UUID `json:"supplier_id" gorm:"type:uuid;index"`
	ExternalOrder string     `json:"external_order" gorm:"size:160"`
	ErrorCode     string     `json:"error_code" gorm:"size:80"`
	ErrorMessage  string     `json:"error_message" gorm:"type:text"`
	StartedAt     time.Time  `json:"started_at"`
	FinishedAt    *time.Time `json:"finished_at"`
}

type SupportTicket struct {
	Base
	TicketNo      string     `json:"ticket_no" gorm:"uniqueIndex;size:80;not null"`
	UserID        *uuid.UUID `json:"user_id" gorm:"type:uuid;index"`
	OrderID       *uuid.UUID `json:"order_id" gorm:"type:uuid;index"`
	Email         string     `json:"email" gorm:"index;size:190;not null"`
	Category      string     `json:"category" gorm:"size:40;not null"`
	Subject       string     `json:"subject" gorm:"size:200;not null"`
	Priority      string     `json:"priority" gorm:"index;size:20;not null"`
	Status        string     `json:"status" gorm:"index;size:24;not null"`
	AssignedTo    *uuid.UUID `json:"assigned_to" gorm:"type:uuid;index"`
	ClosedAt      *time.Time `json:"closed_at"`
	LastMessageAt *time.Time `json:"last_message_at" gorm:"index"`
	UserUnread    int        `json:"user_unread" gorm:"not null;default:0"`
	AdminUnread   int        `json:"admin_unread" gorm:"not null;default:0"`
}

type TicketMessage struct {
	Base
	TicketID    uuid.UUID  `json:"ticket_id" gorm:"type:uuid;index;not null"`
	AuthorType  string     `json:"author_type" gorm:"size:20;not null"`
	AuthorID    *uuid.UUID `json:"author_id" gorm:"type:uuid;index"`
	Body        string     `json:"body" gorm:"type:text;not null"`
	Attachments string     `json:"attachments" gorm:"type:jsonb;default:'[]'"`
	Internal    bool       `json:"internal" gorm:"not null;default:false"`
}

type RiskRule struct {
	Base
	Code       string `json:"code" gorm:"uniqueIndex;size:80;not null"`
	Name       string `json:"name" gorm:"size:160;not null"`
	Scope      string `json:"scope" gorm:"index;size:30;not null"`
	Expression string `json:"expression" gorm:"type:text;not null"`
	Action     string `json:"action" gorm:"size:30;not null"`
	Score      int    `json:"score" gorm:"not null;default:0"`
	Enabled    bool   `json:"enabled" gorm:"index;not null;default:true"`
	Priority   int    `json:"priority" gorm:"not null;default:0"`
}

type RiskDecision struct {
	Base
	OrderID      *uuid.UUID `json:"order_id" gorm:"type:uuid;index"`
	UserID       *uuid.UUID `json:"user_id" gorm:"type:uuid;index"`
	IP           string     `json:"ip" gorm:"index;size:64"`
	Score        int        `json:"score" gorm:"index;not null"`
	Decision     string     `json:"decision" gorm:"index;size:24;not null"`
	MatchedRules string     `json:"matched_rules" gorm:"type:jsonb;default:'[]'"`
	Signals      string     `json:"signals" gorm:"type:jsonb;default:'{}'"`
	ReviewedBy   *uuid.UUID `json:"reviewed_by" gorm:"type:uuid;index"`
	ReviewedAt   *time.Time `json:"reviewed_at"`
}

type ReconciliationBatch struct {
	Base
	BatchNo       string     `json:"batch_no" gorm:"uniqueIndex;size:80;not null"`
	ChannelID     uuid.UUID  `json:"channel_id" gorm:"type:uuid;index;not null"`
	Currency      string     `json:"currency" gorm:"size:3;index;not null;default:CNY"`
	PeriodFrom    time.Time  `json:"period_from" gorm:"index"`
	PeriodTo      time.Time  `json:"period_to" gorm:"index"`
	SourceFile    string     `json:"source_file" gorm:"size:255"`
	StatementHash string     `json:"statement_hash" gorm:"size:64;index"`
	ImportedBy    uuid.UUID  `json:"imported_by" gorm:"type:uuid;index"`
	Status        string     `json:"status" gorm:"index;size:24;not null"`
	Total         int        `json:"total" gorm:"not null;default:0"`
	Matched       int        `json:"matched" gorm:"not null;default:0"`
	Mismatched    int        `json:"mismatched" gorm:"not null;default:0"`
	Resolved      int        `json:"resolved" gorm:"not null;default:0"`
	CompletedAt   *time.Time `json:"completed_at"`
}

type ReconciliationItem struct {
	Base
	BatchID            uuid.UUID  `json:"batch_id" gorm:"type:uuid;index;not null"`
	OrderID            *uuid.UUID `json:"order_id" gorm:"type:uuid;index"`
	Direction          string     `json:"direction" gorm:"size:16"`
	ProviderTradeNo    string     `json:"provider_trade_no" gorm:"index;size:160"`
	Currency           string     `json:"currency" gorm:"size:3;index;not null;default:CNY"`
	ProviderOccurredAt *time.Time `json:"provider_occurred_at,omitempty" gorm:"index"`
	SystemAmount       int64      `json:"system_amount"`
	ProviderAmount     int64      `json:"provider_amount"`
	Difference         int64      `json:"difference"`
	Status             string     `json:"status" gorm:"index;size:24;not null"`
	ResolutionCode     string     `json:"resolution_code" gorm:"size:40"`
	Resolution         string     `json:"resolution" gorm:"type:text"`
	ResolvedBy         *uuid.UUID `json:"resolved_by,omitempty" gorm:"type:uuid;index"`
	ResolvedAt         *time.Time `json:"resolved_at,omitempty"`
}
