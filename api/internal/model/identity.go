package model

import (
	"time"

	"github.com/google/uuid"
)

type Role struct {
	Base
	Code        string `json:"code" gorm:"uniqueIndex;size:80;not null"`
	Name        string `json:"name" gorm:"size:120;not null"`
	Description string `json:"description" gorm:"size:500"`
	System      bool   `json:"system" gorm:"not null;default:false"`
}

type Permission struct {
	Base
	Code        string `json:"code" gorm:"uniqueIndex;size:120;not null"`
	Name        string `json:"name" gorm:"size:120;not null"`
	Module      string `json:"module" gorm:"index;size:80;not null"`
	Description string `json:"description" gorm:"size:500"`
}

type AdminRole struct {
	AdminID uuid.UUID `json:"admin_id" gorm:"type:uuid;primaryKey"`
	RoleID  uuid.UUID `json:"role_id" gorm:"type:uuid;primaryKey"`
}

type RolePermission struct {
	RoleID       uuid.UUID `json:"role_id" gorm:"type:uuid;primaryKey"`
	PermissionID uuid.UUID `json:"permission_id" gorm:"type:uuid;primaryKey"`
}

type APICallLog struct {
	Base
	CredentialID uuid.UUID `json:"credential_id" gorm:"type:uuid;index;not null"`
	Method       string    `json:"method" gorm:"size:12;not null"`
	Path         string    `json:"path" gorm:"index;size:500;not null"`
	StatusCode   int       `json:"status_code" gorm:"index;not null"`
	DurationMS   int64     `json:"duration_ms" gorm:"not null"`
	RequestID    string    `json:"request_id" gorm:"index;size:64"`
	IP           string    `json:"ip" gorm:"size:64"`
}

type UserSession struct {
	Base
	UserID       uuid.UUID  `json:"user_id" gorm:"type:uuid;index;not null"`
	RefreshHash  string     `json:"-" gorm:"uniqueIndex;size:128;not null"`
	Device       string     `json:"device" gorm:"size:200"`
	IP           string     `json:"ip" gorm:"size:64"`
	UserAgent    string     `json:"user_agent" gorm:"size:500"`
	LastActiveAt time.Time  `json:"last_active_at" gorm:"index"`
	ExpiresAt    time.Time  `json:"expires_at" gorm:"index;not null"`
	RevokedAt    *time.Time `json:"revoked_at"`
}

// UserSessionToken keeps the complete refresh-token family history. A used or
// revoked row is intentionally retained until normal data-retention cleanup so
// reuse of an old token can invalidate every active session for that user.
type UserSessionToken struct {
	Base
	UserSessionID uuid.UUID  `json:"-" gorm:"type:uuid;index;not null"`
	UserID        uuid.UUID  `json:"-" gorm:"type:uuid;index;not null"`
	TokenHash     string     `json:"-" gorm:"uniqueIndex;size:64;not null"`
	ExpiresAt     time.Time  `json:"expires_at" gorm:"index;not null"`
	UsedAt        *time.Time `json:"-" gorm:"index"`
	RevokedAt     *time.Time `json:"-" gorm:"index"`
}

type PasswordResetToken struct {
	Base
	UserID    uuid.UUID  `json:"-" gorm:"type:uuid;index;not null"`
	TokenHash string     `json:"-" gorm:"uniqueIndex;size:64;not null"`
	ExpiresAt time.Time  `json:"expires_at" gorm:"index;not null"`
	UsedAt    *time.Time `json:"used_at"`
}

type LoginEvent struct {
	Base
	Realm       string     `json:"realm" gorm:"index;size:20;not null"`
	PrincipalID *uuid.UUID `json:"principal_id" gorm:"type:uuid;index"`
	Account     string     `json:"account" gorm:"index;size:190"`
	IP          string     `json:"ip" gorm:"index;size:64"`
	Country     string     `json:"country" gorm:"size:80"`
	City        string     `json:"city" gorm:"size:100"`
	UserAgent   string     `json:"user_agent" gorm:"size:500"`
	Succeeded   bool       `json:"succeeded" gorm:"index;not null"`
	Reason      string     `json:"reason" gorm:"size:200"`
}

type TOTPDevice struct {
	Base
	Realm          string     `json:"realm" gorm:"uniqueIndex:idx_totp_owner;size:20;not null"`
	PrincipalID    uuid.UUID  `json:"principal_id" gorm:"type:uuid;uniqueIndex:idx_totp_owner;not null"`
	SecretCipher   []byte     `json:"-" gorm:"not null"`
	SecretNonce    []byte     `json:"-" gorm:"not null"`
	RecoveryHashes string     `json:"-" gorm:"type:text"`
	Enabled        bool       `json:"enabled" gorm:"not null;default:false"`
	VerifiedAt     *time.Time `json:"verified_at"`
	// Pending fields let an enabled device remain active until a replacement
	// secret has been proven, avoiding a 2FA bypass window during reset.
	PendingSecretCipher   []byte     `json:"-"`
	PendingSecretNonce    []byte     `json:"-"`
	PendingRecoveryHashes string     `json:"-" gorm:"type:text"`
	PendingCreatedAt      *time.Time `json:"-"`
}

type OAuthIdentity struct {
	Base
	UserID         uuid.UUID `json:"user_id" gorm:"type:uuid;index;not null"`
	Provider       string    `json:"provider" gorm:"uniqueIndex:idx_oauth_subject;size:40;not null"`
	ProviderUserID string    `json:"provider_user_id" gorm:"uniqueIndex:idx_oauth_subject;size:190;not null"`
	Email          string    `json:"email" gorm:"size:190"`
	Metadata       string    `json:"metadata" gorm:"type:jsonb;default:'{}'"`
}

type MemberLevel struct {
	Base
	Code               string `json:"code" gorm:"uniqueIndex;size:60;not null"`
	Name               string `json:"name" gorm:"size:100;not null"`
	Currency           string `json:"currency" gorm:"size:3;index;not null;default:CNY"`
	MinimumSpend       int64  `json:"minimum_spend" gorm:"not null;default:0"`
	DiscountBasisPoint int    `json:"discount_basis_point" gorm:"not null;default:0"`
	Priority           int    `json:"priority" gorm:"not null;default:0"`
	Enabled            bool   `json:"enabled" gorm:"not null;default:true"`
}

type UserLevelMembership struct {
	UserID        uuid.UUID  `json:"user_id" gorm:"type:uuid;primaryKey"`
	MemberLevelID uuid.UUID  `json:"member_level_id" gorm:"type:uuid;index;not null"`
	GrantedAt     time.Time  `json:"granted_at"`
	ExpiresAt     *time.Time `json:"expires_at"`
	Source        string     `json:"source" gorm:"index;size:20;not null;default:automatic"`
	GrantedBy     *uuid.UUID `json:"granted_by" gorm:"type:uuid;index"`
	EvaluatedAt   time.Time  `json:"evaluated_at" gorm:"index"`
}
