package model

import (
	"time"

	"github.com/google/uuid"
)

type Promotion struct {
	Base
	Name      string    `json:"name" gorm:"size:160;not null"`
	Code      string    `json:"code" gorm:"uniqueIndex;size:80;not null"`
	Type      string    `json:"type" gorm:"index;size:30;not null"`
	Currency  string    `json:"currency" gorm:"size:3;index;not null;default:CNY"`
	Rules     string    `json:"rules" gorm:"type:jsonb;default:'{}'"`
	Priority  int       `json:"priority" gorm:"not null;default:0"`
	Stackable bool      `json:"stackable" gorm:"not null;default:false"`
	StartsAt  time.Time `json:"starts_at" gorm:"index"`
	EndsAt    time.Time `json:"ends_at" gorm:"index"`
	Status    string    `json:"status" gorm:"index;size:24;not null"`
}

type PromotionProduct struct {
	PromotionID uuid.UUID `json:"promotion_id" gorm:"type:uuid;primaryKey"`
	ProductID   uuid.UUID `json:"product_id" gorm:"type:uuid;primaryKey"`
}

type CouponRedemption struct {
	Base
	CouponID    uuid.UUID  `json:"coupon_id" gorm:"type:uuid;index;not null"`
	OrderID     uuid.UUID  `json:"order_id" gorm:"type:uuid;uniqueIndex;not null"`
	UserID      *uuid.UUID `json:"user_id,omitempty" gorm:"type:uuid;index"`
	RedeemerKey string     `json:"-" gorm:"index;size:64;not null"`
	Discount    int64      `json:"discount" gorm:"not null"`
	Status      string     `json:"status" gorm:"size:20;index;not null;default:reserved"`
	RedeemedAt  *time.Time `json:"redeemed_at,omitempty"`
	ReleasedAt  *time.Time `json:"released_at,omitempty"`
}

type GiftCardBatch struct {
	Base
	BatchNo    string     `json:"batch_no" gorm:"uniqueIndex;size:80;not null"`
	Name       string     `json:"name" gorm:"size:160;not null"`
	Quantity   int        `json:"quantity" gorm:"not null"`
	CardValue  int64      `json:"card_value" gorm:"not null"`
	Currency   string     `json:"currency" gorm:"size:8;not null;default:CNY"`
	Status     string     `json:"status" gorm:"index;size:24;not null;default:active"`
	ExpiresAt  *time.Time `json:"expires_at,omitempty" gorm:"index"`
	IssuedBy   uuid.UUID  `json:"issued_by" gorm:"type:uuid;index;not null"`
	DisabledAt *time.Time `json:"disabled_at,omitempty"`
}

type GiftCard struct {
	Base
	BatchID        *uuid.UUID `json:"batch_id,omitempty" gorm:"type:uuid;index"`
	CodeHash       string     `json:"-" gorm:"uniqueIndex;size:128;not null"`
	CodePreview    string     `json:"code_preview" gorm:"size:40;not null"`
	InitialBalance int64      `json:"initial_balance" gorm:"not null"`
	Balance        int64      `json:"balance" gorm:"not null"`
	Currency       string     `json:"currency" gorm:"size:8;not null;default:CNY"`
	Status         string     `json:"status" gorm:"index;size:24;not null"`
	RedeemedBy     *uuid.UUID `json:"redeemed_by" gorm:"type:uuid;index"`
	RedeemedAt     *time.Time `json:"redeemed_at"`
	ExpiresAt      *time.Time `json:"expires_at" gorm:"index"`
}

type GiftCardEntry struct {
	Base
	GiftCardID   uuid.UUID  `json:"gift_card_id" gorm:"type:uuid;index;not null"`
	UserID       *uuid.UUID `json:"user_id" gorm:"type:uuid;index"`
	OrderID      *uuid.UUID `json:"order_id" gorm:"type:uuid;index"`
	Amount       int64      `json:"amount" gorm:"not null"`
	BalanceAfter int64      `json:"balance_after" gorm:"not null"`
	Type         string     `json:"type" gorm:"size:24;not null"`
}

type Banner struct {
	Base
	Title     string     `json:"title" gorm:"size:160;not null"`
	ImageURL  string     `json:"image_url" gorm:"size:500"`
	TargetURL string     `json:"target_url" gorm:"size:500"`
	Placement string     `json:"placement" gorm:"index;size:40;not null"`
	Sort      int        `json:"sort" gorm:"not null;default:0"`
	Enabled   bool       `json:"enabled" gorm:"index;not null;default:true"`
	StartsAt  *time.Time `json:"starts_at"`
	EndsAt    *time.Time `json:"ends_at"`
}

type PostCategory struct {
	Base
	Name string `json:"name" gorm:"size:100;not null"`
	Slug string `json:"slug" gorm:"uniqueIndex;size:120;not null"`
	Sort int    `json:"sort" gorm:"not null;default:0"`
}

type Post struct {
	Base
	CategoryID  *uuid.UUID `json:"category_id" gorm:"type:uuid;index"`
	Title       string     `json:"title" gorm:"size:220;not null"`
	Slug        string     `json:"slug" gorm:"uniqueIndex;size:240;not null"`
	Summary     string     `json:"summary" gorm:"size:600"`
	Content     string     `json:"content" gorm:"type:text;not null"`
	CoverURL    string     `json:"cover_url" gorm:"size:500"`
	Status      string     `json:"status" gorm:"index;size:24;not null"`
	AuthorID    *uuid.UUID `json:"author_id" gorm:"type:uuid;index"`
	PublishedAt *time.Time `json:"published_at" gorm:"index"`
	SEO         string     `json:"seo" gorm:"type:jsonb;default:'{}'"`
}

type MediaAsset struct {
	Base
	Disk       string     `json:"disk" gorm:"size:30;not null"`
	ObjectKey  string     `json:"object_key" gorm:"uniqueIndex;size:500;not null"`
	PublicURL  string     `json:"public_url" gorm:"size:1000;index"`
	AltText    string     `json:"alt_text" gorm:"size:300"`
	FileName   string     `json:"file_name" gorm:"size:255;not null"`
	MIME       string     `json:"mime" gorm:"size:120;not null"`
	Size       int64      `json:"size" gorm:"not null"`
	SHA256     string     `json:"sha256" gorm:"index;size:64;not null"`
	UploadedBy *uuid.UUID `json:"uploaded_by" gorm:"type:uuid;index"`
	Visibility string     `json:"visibility" gorm:"size:20;not null"`
}
