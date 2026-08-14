package model

import (
	"time"

	"github.com/google/uuid"
)

type NotificationTemplate struct {
	Base
	Code      string `json:"code" gorm:"uniqueIndex;size:100;not null"`
	Name      string `json:"name" gorm:"size:160;not null"`
	Audience  string `json:"audience" gorm:"index;size:16;not null;default:admin"`
	Channel   string `json:"channel" gorm:"index;size:24;not null"`
	Locale    string `json:"locale" gorm:"size:16;not null;default:zh-CN"`
	Subject   string `json:"subject" gorm:"size:255"`
	Body      string `json:"body" gorm:"type:text;not null"`
	Variables string `json:"variables" gorm:"type:jsonb;default:'[]'"`
	Enabled   bool   `json:"enabled" gorm:"not null;default:true"`
	Version   int    `json:"version" gorm:"not null;default:1"`
}

type NotificationDelivery struct {
	Base
	IdempotencyKey string     `json:"idempotency_key" gorm:"uniqueIndex;size:160"`
	TemplateID     *uuid.UUID `json:"template_id" gorm:"type:uuid;index"`
	Channel        string     `json:"channel" gorm:"index;size:24;not null"`
	Recipient      string     `json:"recipient" gorm:"index;size:255;not null"`
	Subject        string     `json:"subject" gorm:"size:255"`
	BodyCipher     []byte     `json:"-"`
	BodyNonce      []byte     `json:"-"`
	Status         string     `json:"status" gorm:"index;size:24;not null"`
	Attempts       int        `json:"attempts" gorm:"not null;default:0"`
	ProviderID     string     `json:"provider_id" gorm:"size:160"`
	LastError      string     `json:"last_error" gorm:"type:text"`
	NextAttemptAt  *time.Time `json:"next_attempt_at" gorm:"index"`
	SentAt         *time.Time `json:"sent_at"`
}

// NotificationConnector stores an operator-managed delivery provider. Secrets
// are encrypted with the application vault and are never serialized.
type NotificationConnector struct {
	Base
	Name         string `json:"name" gorm:"size:120;not null"`
	Channel      string `json:"channel" gorm:"uniqueIndex;size:24;not null"`
	Endpoint     string `json:"endpoint" gorm:"size:500"`
	Username     string `json:"username" gorm:"size:255"`
	Sender       string `json:"sender" gorm:"size:255"`
	SecretCipher []byte `json:"-"`
	SecretNonce  []byte `json:"-"`
	Enabled      bool   `json:"enabled" gorm:"index;not null;default:false"`
}

// NotificationSubscription binds a business event to one template and one
// destination. A unique event/channel/recipient tuple prevents accidental
// duplicate alerts while still allowing different teams to subscribe.
type NotificationSubscription struct {
	Base
	Audience   string    `json:"audience" gorm:"uniqueIndex:idx_notification_subscription_destination;index;size:16;not null;default:admin"`
	EventCode  string    `json:"event_code" gorm:"uniqueIndex:idx_notification_subscription_destination;index;size:100;not null"`
	Channel    string    `json:"channel" gorm:"uniqueIndex:idx_notification_subscription_destination;index;size:24;not null"`
	Recipient  string    `json:"recipient" gorm:"uniqueIndex:idx_notification_subscription_destination;size:255;not null"`
	TemplateID uuid.UUID `json:"template_id" gorm:"type:uuid;index;not null"`
	Locale     string    `json:"locale" gorm:"uniqueIndex:idx_notification_subscription_destination;size:16;not null;default:zh-CN"`
	Enabled    bool      `json:"enabled" gorm:"index;not null;default:true"`
}

// UserNotification is the private in-app inbox. Its body is encrypted with
// the application vault and is never exposed to administrators or other users.
type UserNotification struct {
	Base
	UserID         uuid.UUID  `json:"user_id" gorm:"type:uuid;index;not null"`
	EventCode      string     `json:"event_code" gorm:"index;size:100;not null"`
	EntityID       string     `json:"entity_id" gorm:"size:100;not null"`
	IdempotencyKey string     `json:"-" gorm:"uniqueIndex;size:190;not null"`
	Title          string     `json:"title" gorm:"size:255;not null"`
	BodyCipher     []byte     `json:"-"`
	BodyNonce      []byte     `json:"-"`
	ReadAt         *time.Time `json:"read_at,omitempty" gorm:"index"`
}

type WebhookEndpoint struct {
	Base
	OwnerType    string     `json:"owner_type" gorm:"index;size:24;not null"`
	OwnerID      uuid.UUID  `json:"owner_id" gorm:"type:uuid;index;not null"`
	URL          string     `json:"url" gorm:"size:500;not null"`
	Events       string     `json:"events" gorm:"type:jsonb;default:'[]'"`
	SecretCipher []byte     `json:"-"`
	SecretNonce  []byte     `json:"-"`
	Enabled      bool       `json:"enabled" gorm:"index;not null;default:true"`
	FailureCount int        `json:"failure_count" gorm:"not null;default:0"`
	DisabledAt   *time.Time `json:"disabled_at"`
}

type WebhookDelivery struct {
	Base
	EndpointID    uuid.UUID  `json:"endpoint_id" gorm:"type:uuid;index;uniqueIndex:idx_webhook_endpoint_event;not null"`
	EventID       string     `json:"event_id" gorm:"index;uniqueIndex:idx_webhook_endpoint_event;size:100;not null"`
	EventType     string     `json:"event_type" gorm:"index;size:80;not null"`
	Payload       string     `json:"payload" gorm:"type:jsonb;not null"`
	PayloadCipher []byte     `json:"-"`
	PayloadNonce  []byte     `json:"-"`
	Status        string     `json:"status" gorm:"index;size:24;not null"`
	Attempts      int        `json:"attempts" gorm:"not null;default:0"`
	ResponseCode  int        `json:"response_code"`
	ResponseBody  string     `json:"response_body" gorm:"type:text"`
	NextAttemptAt *time.Time `json:"next_attempt_at" gorm:"index"`
	DeliveredAt   *time.Time `json:"delivered_at"`
}

type JobRecord struct {
	Base
	TaskID      string     `json:"task_id" gorm:"uniqueIndex;size:120;not null"`
	TaskType    string     `json:"task_type" gorm:"index;size:120;not null"`
	Queue       string     `json:"queue" gorm:"index;size:40;not null"`
	Status      string     `json:"status" gorm:"index;size:24;not null"`
	Attempts    int        `json:"attempts" gorm:"not null;default:0"`
	Payload     string     `json:"payload" gorm:"type:jsonb;default:'{}'"`
	LastError   string     `json:"last_error" gorm:"type:text"`
	ScheduledAt *time.Time `json:"scheduled_at" gorm:"index"`
	FinishedAt  *time.Time `json:"finished_at"`
}

type SecurityEvent struct {
	Base
	EventType   string     `json:"event_type" gorm:"index;size:80;not null"`
	Severity    string     `json:"severity" gorm:"index;size:20;not null"`
	Realm       string     `json:"realm" gorm:"size:20"`
	PrincipalID *uuid.UUID `json:"principal_id" gorm:"type:uuid;index"`
	IP          string     `json:"ip" gorm:"index;size:64"`
	UserAgent   string     `json:"user_agent" gorm:"size:500"`
	Details     string     `json:"details" gorm:"type:jsonb;default:'{}'"`
	Resolved    bool       `json:"resolved" gorm:"index;not null;default:false"`
	ResolvedBy  *uuid.UUID `json:"resolved_by" gorm:"type:uuid;index"`
	ResolvedAt  *time.Time `json:"resolved_at"`
}

type IPBlocklist struct {
	Base
	CIDR      string     `json:"cidr" gorm:"column:cidr;index;size:64;not null"`
	Scope     string     `json:"scope" gorm:"index;size:20;not null;default:public"`
	Reason    string     `json:"reason" gorm:"size:500"`
	Source    string     `json:"source" gorm:"size:40;not null"`
	Enabled   bool       `json:"enabled" gorm:"index;not null;default:true"`
	ExpiresAt *time.Time `json:"expires_at" gorm:"index"`
	CreatedBy *uuid.UUID `json:"created_by" gorm:"type:uuid;index"`
}

type SystemMetric struct {
	ID         uint64    `json:"id" gorm:"primaryKey;autoIncrement"`
	Name       string    `json:"name" gorm:"index;size:120;not null"`
	Labels     string    `json:"labels" gorm:"type:jsonb;default:'{}'"`
	Value      float64   `json:"value" gorm:"not null"`
	RecordedAt time.Time `json:"recorded_at" gorm:"index;not null"`
}
