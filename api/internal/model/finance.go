package model

import (
	"time"

	"github.com/google/uuid"
)

type WalletAccount struct {
	Base
	OwnerType string    `json:"owner_type" gorm:"uniqueIndex:idx_wallet_owner;size:24;not null"`
	OwnerID   uuid.UUID `json:"owner_id" gorm:"type:uuid;uniqueIndex:idx_wallet_owner;not null"`
	Currency  string    `json:"currency" gorm:"uniqueIndex:idx_wallet_owner;size:8;not null;default:CNY"`
	Balance   int64     `json:"balance" gorm:"not null;default:0"`
	Frozen    int64     `json:"frozen" gorm:"not null;default:0"`
	Version   int64     `json:"version" gorm:"not null;default:0"`
}

type WalletEntry struct {
	Base
	AccountID     uuid.UUID  `json:"account_id" gorm:"type:uuid;index;not null"`
	EntryNo       string     `json:"entry_no" gorm:"uniqueIndex;size:80;not null"`
	Type          string     `json:"type" gorm:"index;size:30;not null"`
	Amount        int64      `json:"amount" gorm:"not null"`
	BalanceAfter  int64      `json:"balance_after" gorm:"not null"`
	ReferenceType string     `json:"reference_type" gorm:"size:30"`
	ReferenceID   *uuid.UUID `json:"reference_id" gorm:"type:uuid;index"`
	Description   string     `json:"description" gorm:"size:500"`
}

type RechargeOrder struct {
	Base
	RechargeNo         string     `json:"recharge_no" gorm:"uniqueIndex;size:80;not null"`
	IntentNo           string     `json:"intent_no" gorm:"index;size:80;not null;default:''"`
	IdempotencyKeyHash string     `json:"-" gorm:"index;size:64;not null;default:''"`
	UserID             uuid.UUID  `json:"user_id" gorm:"type:uuid;index;not null"`
	Amount             int64      `json:"amount" gorm:"not null"`
	Bonus              int64      `json:"bonus" gorm:"not null;default:0"`
	Currency           string     `json:"currency" gorm:"size:8;not null;default:CNY"`
	CreditAmount       int64      `json:"credit_amount" gorm:"not null;default:0"`
	CreditCurrency     string     `json:"credit_currency" gorm:"size:3;index;not null;default:CNY"`
	FXSnapshotID       *uuid.UUID `json:"fx_snapshot_id,omitempty" gorm:"type:uuid;index"`
	ChannelID          uuid.UUID  `json:"channel_id" gorm:"type:uuid;index;not null"`
	Status             string     `json:"status" gorm:"index;size:24;not null"`
	ProviderTradeNo    string     `json:"provider_trade_no" gorm:"index;size:160"`
	CheckoutURL        string     `json:"checkout_url" gorm:"type:text"`
	ExpiresAt          time.Time  `json:"expires_at" gorm:"index"`
	PaidAt             *time.Time `json:"paid_at"`
}

type RechargeTransaction struct {
	Base
	RechargeOrderID     uuid.UUID  `json:"recharge_order_id" gorm:"type:uuid;index;not null"`
	ProviderEventID     string     `json:"provider_event_id" gorm:"uniqueIndex;size:190;not null"`
	ProviderTradeNo     string     `json:"provider_trade_no" gorm:"index;size:160;not null"`
	Amount              int64      `json:"amount" gorm:"not null"`
	Currency            string     `json:"currency" gorm:"size:3;index;not null;default:CNY"`
	ExpectedAmount      int64      `json:"expected_amount" gorm:"not null;default:0"`
	ExpectedCurrency    string     `json:"expected_currency" gorm:"size:3;not null;default:CNY"`
	Status              string     `json:"status" gorm:"index;size:24;not null"`
	Disposition         string     `json:"disposition" gorm:"index;size:24;not null;default:credited"`
	MismatchReason      string     `json:"mismatch_reason,omitempty" gorm:"size:500"`
	RefundNo            string     `json:"refund_no,omitempty" gorm:"size:80"`
	ProviderRefundNo    string     `json:"provider_refund_no,omitempty" gorm:"size:160"`
	RefundAttempts      int        `json:"refund_attempts" gorm:"not null;default:0"`
	RefundNextAttemptAt *time.Time `json:"refund_next_attempt_at,omitempty" gorm:"index"`
	RefundLastError     string     `json:"refund_last_error,omitempty" gorm:"size:1000"`
	RawPayload          string     `json:"-" gorm:"type:jsonb;default:'{}'"`
	PaidAt              *time.Time `json:"paid_at"`
	RefundedAt          *time.Time `json:"refunded_at,omitempty"`
}

type AffiliateProfile struct {
	Base
	UserID               uuid.UUID  `json:"user_id" gorm:"type:uuid;uniqueIndex;not null"`
	ReferralCode         string     `json:"referral_code" gorm:"uniqueIndex;size:40;not null"`
	CommissionBasisPoint int        `json:"commission_basis_point" gorm:"not null;default:500"`
	Status               string     `json:"status" gorm:"index;size:24;not null"`
	TotalCommission      int64      `json:"total_commission" gorm:"not null;default:0"`
	AvailableCommission  int64      `json:"available_commission" gorm:"not null;default:0"`
	FrozenCommission     int64      `json:"frozen_commission" gorm:"not null;default:0"`
	AppliedAt            time.Time  `json:"applied_at" gorm:"index"`
	ApprovedAt           *time.Time `json:"approved_at,omitempty"`
	RejectedAt           *time.Time `json:"rejected_at,omitempty"`
}

// AffiliateBalance isolates commission ledgers by ISO currency. The aggregate
// fields on AffiliateProfile are retained only for legacy migration and must
// not be used for new financial mutations.
type AffiliateBalance struct {
	Base
	AffiliateID         uuid.UUID `json:"affiliate_id" gorm:"type:uuid;uniqueIndex:idx_affiliate_balance_currency;not null"`
	Currency            string    `json:"currency" gorm:"size:3;uniqueIndex:idx_affiliate_balance_currency;not null"`
	TotalCommission     int64     `json:"total_commission" gorm:"not null;default:0"`
	AvailableCommission int64     `json:"available_commission" gorm:"not null;default:0"`
	FrozenCommission    int64     `json:"frozen_commission" gorm:"not null;default:0"`
}

type AffiliateReferral struct {
	Base
	AffiliateID    uuid.UUID `json:"affiliate_id" gorm:"type:uuid;index;not null"`
	ReferredUserID uuid.UUID `json:"referred_user_id" gorm:"type:uuid;uniqueIndex;not null"`
	ReferralCode   string    `json:"referral_code" gorm:"size:40;not null"`
	AttributedAt   time.Time `json:"attributed_at" gorm:"index;not null"`
}

type AffiliateCommission struct {
	Base
	AffiliateID    uuid.UUID  `json:"affiliate_id" gorm:"type:uuid;index;not null"`
	OrderID        uuid.UUID  `json:"order_id" gorm:"type:uuid;uniqueIndex;not null"`
	BuyerID        *uuid.UUID `json:"buyer_id" gorm:"type:uuid;index"`
	OrderAmount    int64      `json:"order_amount" gorm:"not null"`
	Commission     int64      `json:"commission" gorm:"not null"`
	Currency       string     `json:"currency" gorm:"size:3;index;not null;default:CNY"`
	ReversedAmount int64      `json:"reversed_amount" gorm:"not null;default:0"`
	Status         string     `json:"status" gorm:"index;size:24;not null"`
	SettlesAt      time.Time  `json:"settles_at" gorm:"index"`
	SettledAt      *time.Time `json:"settled_at,omitempty" gorm:"index"`
}

type AffiliateWithdrawal struct {
	Base
	WithdrawalNo    string     `json:"withdrawal_no" gorm:"uniqueIndex;size:80;not null"`
	AffiliateID     uuid.UUID  `json:"affiliate_id" gorm:"type:uuid;index;not null"`
	Amount          int64      `json:"amount" gorm:"not null"`
	Fee             int64      `json:"fee" gorm:"not null;default:0"`
	Currency        string     `json:"currency" gorm:"size:3;index;not null;default:CNY"`
	Method          string     `json:"method" gorm:"size:30;not null;default:bank"`
	Account         string     `json:"-" gorm:"size:255"`
	AccountCipher   []byte     `json:"-"`
	AccountNonce    []byte     `json:"-"`
	AccountPreview  string     `json:"account_preview" gorm:"size:100"`
	Status          string     `json:"status" gorm:"index;size:24;not null"`
	PayoutReference string     `json:"payout_reference" gorm:"size:160"`
	ProcessedBy     *uuid.UUID `json:"processed_by,omitempty" gorm:"type:uuid;index"`
	Reason          string     `json:"reason" gorm:"size:500"`
	ProcessedAt     *time.Time `json:"processed_at"`
}

type ResellerProfile struct {
	Base
	UserID         uuid.UUID  `json:"user_id" gorm:"type:uuid;uniqueIndex;not null"`
	Name           string     `json:"name" gorm:"size:160;not null"`
	Code           string     `json:"code" gorm:"uniqueIndex;size:60;not null"`
	Status         string     `json:"status" gorm:"index;size:24;not null"`
	CreditLimit    int64      `json:"credit_limit" gorm:"not null;default:0"`
	WholesaleLevel int        `json:"wholesale_level" gorm:"not null;default:0"`
	AppliedAt      time.Time  `json:"applied_at" gorm:"index"`
	VerifiedAt     *time.Time `json:"verified_at"`
	RejectedAt     *time.Time `json:"rejected_at,omitempty"`
}

// ResellerCreditPolicy scopes an operator-approved credit limit to one
// currency so exposure in USD can never consume a CNY limit (or vice versa).
type ResellerCreditPolicy struct {
	Base
	ResellerID  uuid.UUID `json:"reseller_id" gorm:"type:uuid;uniqueIndex:idx_reseller_credit_currency;not null"`
	Currency    string    `json:"currency" gorm:"size:3;uniqueIndex:idx_reseller_credit_currency;not null"`
	CreditLimit int64     `json:"credit_limit" gorm:"not null;default:0"`
}

// ResellerWholesaleTier is an operator-owned settlement policy. A tier never
// changes the buyer-facing floor price; it only changes the platform
// settlement price used to calculate the reseller's margin on new orders.
type ResellerWholesaleTier struct {
	Base
	Level              int    `json:"level" gorm:"uniqueIndex;not null"`
	Name               string `json:"name" gorm:"size:100;not null"`
	DiscountBasisPoint int    `json:"discount_basis_point" gorm:"not null;default:0"`
	Enabled            bool   `json:"enabled" gorm:"index;not null"`
}

// ResellerCreditEvent is the immutable audit snapshot produced when a refund
// clawback consumes more credit than the reseller is allowed. EventKey makes
// retrying the same refund transition idempotent.
type ResellerCreditEvent struct {
	Base
	EventKey      string     `json:"event_key" gorm:"uniqueIndex;size:100;not null"`
	ResellerID    uuid.UUID  `json:"reseller_id" gorm:"type:uuid;index;not null"`
	OrderID       *uuid.UUID `json:"order_id,omitempty" gorm:"type:uuid;index"`
	WalletEntryID *uuid.UUID `json:"wallet_entry_id,omitempty" gorm:"type:uuid;index"`
	Type          string     `json:"type" gorm:"size:32;index;not null"`
	Balance       int64      `json:"balance" gorm:"not null"`
	Frozen        int64      `json:"frozen" gorm:"not null"`
	Exposure      int64      `json:"exposure" gorm:"not null"`
	CreditLimit   int64      `json:"credit_limit" gorm:"not null"`
	Currency      string     `json:"currency" gorm:"size:3;index;not null;default:CNY"`
	Action        string     `json:"action" gorm:"size:32;not null"`
}

type ResellerDomain struct {
	Base
	ResellerID        uuid.UUID  `json:"reseller_id" gorm:"type:uuid;index;not null"`
	Domain            string     `json:"domain" gorm:"size:253;not null"`
	Status            string     `json:"status" gorm:"index;size:24;not null"`
	TLSStatus         string     `json:"tls_status" gorm:"size:24;not null"`
	VerificationToken string     `json:"verification_token" gorm:"size:100"`
	VerifiedAt        *time.Time `json:"verified_at"`
}

type ResellerSite struct {
	Base
	ResellerID uuid.UUID `json:"reseller_id" gorm:"type:uuid;uniqueIndex;not null"`
	SiteName   string    `json:"site_name" gorm:"size:160;not null"`
	LogoURL    string    `json:"logo_url" gorm:"size:500"`
	Theme      string    `json:"theme" gorm:"type:jsonb;default:'{}'"`
	SEO        string    `json:"seo" gorm:"type:jsonb;default:'{}'"`
	Support    string    `json:"support" gorm:"type:jsonb;default:'{}'"`
}

type ResellerProductRule struct {
	Base
	ResellerID       uuid.UUID  `json:"reseller_id" gorm:"type:uuid;index;not null"`
	ProductID        uuid.UUID  `json:"product_id" gorm:"type:uuid;index;not null"`
	VariantID        *uuid.UUID `json:"variant_id,omitempty" gorm:"type:uuid;index"`
	Enabled          bool       `json:"enabled" gorm:"not null;default:true"`
	PricingMode      string     `json:"pricing_mode" gorm:"size:24;not null"`
	Currency         string     `json:"currency" gorm:"size:3;index;not null;default:CNY"`
	MarkupBasisPoint int        `json:"markup_basis_point" gorm:"not null;default:0"`
	FixedPrice       int64      `json:"fixed_price" gorm:"not null;default:0"`
}

type ResellerWithdrawal struct {
	Base
	WithdrawalNo    string     `json:"withdrawal_no" gorm:"uniqueIndex;size:80;not null"`
	ResellerID      uuid.UUID  `json:"reseller_id" gorm:"type:uuid;index;not null"`
	Amount          int64      `json:"amount" gorm:"not null"`
	Currency        string     `json:"currency" gorm:"size:3;index;not null;default:CNY"`
	Fee             int64      `json:"fee" gorm:"not null;default:0"`
	Method          string     `json:"method" gorm:"size:30;not null"`
	AccountCipher   []byte     `json:"-"`
	AccountNonce    []byte     `json:"-"`
	AccountPreview  string     `json:"account_preview" gorm:"size:100"`
	Status          string     `json:"status" gorm:"index;size:24;not null"`
	PayoutReference string     `json:"payout_reference" gorm:"size:160"`
	ProcessedBy     *uuid.UUID `json:"processed_by,omitempty" gorm:"type:uuid;index"`
	Reason          string     `json:"reason" gorm:"size:500"`
	ProcessedAt     *time.Time `json:"processed_at"`
}
