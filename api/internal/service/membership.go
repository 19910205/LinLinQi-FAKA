package service

import (
	"errors"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"linlinqi/api/internal/model"
)

var ErrMembershipLevelUnavailable = errors.New("membership level is unavailable")

// UserNetSpend returns settled order value net of successful refunds. It is
// the single source used by customer reporting and automatic level decisions.
func UserNetSpend(tx *gorm.DB, userID uuid.UUID) (int64, error) {
	currencyCode, err := StoreCurrency(tx)
	if err != nil {
		return 0, err
	}
	var spend int64
	err = tx.Raw(`
		SELECT COALESCE(SUM(GREATEST(o.total - COALESCE(r.refunded, 0), 0)), 0)
		FROM orders o
		LEFT JOIN (
			SELECT order_id, SUM(order_amount) AS refunded
			FROM refunds
			WHERE deleted_at IS NULL AND status = 'succeeded' AND order_currency = ?
			GROUP BY order_id
		) r ON r.order_id = o.id
		WHERE o.deleted_at IS NULL AND o.user_id = ?
		  AND o.currency = ?
		  AND o.payment_status IN ('paid', 'partially_refunded', 'refunded')
		  AND o.status IN ('delivered', 'completed', 'refunding', 'refunded')`, currencyCode, userID, currencyCode).Scan(&spend).Error
	return spend, err
}

// ReconcileUserMembershipTx upgrades or downgrades an automatically managed
// membership from settled net spend. A live manual grant is never overwritten.
func ReconcileUserMembershipTx(tx *gorm.DB, userID uuid.UUID, at time.Time) (*model.UserLevelMembership, bool, error) {
	if userID == uuid.Nil {
		return nil, false, gorm.ErrRecordNotFound
	}
	if at.IsZero() {
		at = time.Now().UTC()
	}
	var user model.User
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").First(&user, "id = ?", userID).Error; err != nil {
		return nil, false, err
	}
	var current model.UserLevelMembership
	currentErr := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ?", userID).First(&current).Error
	if currentErr != nil && !errors.Is(currentErr, gorm.ErrRecordNotFound) {
		return nil, false, currentErr
	}
	if currentErr == nil && current.Source == "manual" && (current.ExpiresAt == nil || current.ExpiresAt.After(at)) {
		return &current, false, nil
	}
	spend, err := UserNetSpend(tx, userID)
	if err != nil {
		return nil, false, err
	}
	currencyCode, err := StoreCurrency(tx)
	if err != nil {
		return nil, false, err
	}
	var level model.MemberLevel
	levelErr := tx.Where("enabled = ? AND currency = ? AND minimum_spend <= ?", true, currencyCode, spend).
		Order("minimum_spend DESC, priority DESC, created_at ASC").First(&level).Error
	if errors.Is(levelErr, gorm.ErrRecordNotFound) {
		if currentErr == nil {
			if err := tx.Where("user_id = ?", userID).Delete(&model.UserLevelMembership{}).Error; err != nil {
				return nil, false, err
			}
			return nil, true, nil
		}
		return nil, false, nil
	}
	if levelErr != nil {
		return nil, false, levelErr
	}
	changed := currentErr != nil || current.MemberLevelID != level.ID || current.Source != "automatic"
	grantedAt := current.GrantedAt
	if changed || grantedAt.IsZero() {
		grantedAt = at
	}
	next := model.UserLevelMembership{
		UserID: userID, MemberLevelID: level.ID, GrantedAt: grantedAt,
		Source: "automatic", EvaluatedAt: at,
	}
	err = tx.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "user_id"}},
		DoUpdates: clause.Assignments(map[string]any{
			"member_level_id": next.MemberLevelID, "granted_at": next.GrantedAt,
			"expires_at": nil, "source": next.Source, "granted_by": nil, "evaluated_at": next.EvaluatedAt,
		}),
	}).Create(&next).Error
	if err != nil {
		return nil, false, err
	}
	return &next, changed, nil
}

func ReconcileUserMembership(db *gorm.DB, userID uuid.UUID, at time.Time) (*model.UserLevelMembership, bool, error) {
	var result *model.UserLevelMembership
	var changed bool
	err := db.Transaction(func(tx *gorm.DB) error {
		var err error
		result, changed, err = ReconcileUserMembershipTx(tx, userID, at)
		return err
	})
	return result, changed, err
}

// EffectiveUserMembershipTx returns a usable membership and repairs missing,
// expired, disabled-level or legacy state before pricing is calculated.
func EffectiveUserMembershipTx(tx *gorm.DB, userID uuid.UUID, at time.Time) (*model.UserLevelMembership, *model.MemberLevel, error) {
	var membership model.UserLevelMembership
	var level model.MemberLevel
	err := tx.Table("user_level_memberships ulm").
		Select("ulm.*").
		Joins("JOIN member_levels ml ON ml.id = ulm.member_level_id AND ml.deleted_at IS NULL AND ml.enabled = ?", true).
		Where("ulm.user_id = ? AND (ulm.expires_at IS NULL OR ulm.expires_at > ?)", userID, at).
		First(&membership).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		resolved, _, reconcileErr := ReconcileUserMembershipTx(tx, userID, at)
		if reconcileErr != nil || resolved == nil {
			return resolved, nil, reconcileErr
		}
		membership = *resolved
	} else if err != nil {
		return nil, nil, err
	}
	if err := tx.Where("id = ? AND enabled = ?", membership.MemberLevelID, true).First(&level).Error; err != nil {
		return nil, nil, err
	}
	return &membership, &level, nil
}

func GrantManualMembershipTx(tx *gorm.DB, userID, levelID, adminID uuid.UUID, expiresAt *time.Time, at time.Time) (*model.UserLevelMembership, error) {
	if userID == uuid.Nil || levelID == uuid.Nil || adminID == uuid.Nil {
		return nil, ErrMembershipLevelUnavailable
	}
	var user model.User
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").First(&user, "id = ?", userID).Error; err != nil {
		return nil, err
	}
	var level model.MemberLevel
	if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("id = ? AND enabled = ?", levelID, true).First(&level).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrMembershipLevelUnavailable
		}
		return nil, err
	}
	next := model.UserLevelMembership{
		UserID: userID, MemberLevelID: level.ID, GrantedAt: at, ExpiresAt: expiresAt,
		Source: "manual", GrantedBy: &adminID, EvaluatedAt: at,
	}
	err := tx.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "user_id"}},
		DoUpdates: clause.Assignments(map[string]any{
			"member_level_id": next.MemberLevelID, "granted_at": next.GrantedAt,
			"expires_at": next.ExpiresAt, "source": next.Source,
			"granted_by": next.GrantedBy, "evaluated_at": next.EvaluatedAt,
		}),
	}).Create(&next).Error
	return &next, err
}

// ReconcileDueMemberships keeps expiry and automatic downgrades operational
// even when a user does not sign in. Each user is isolated in its own tx.
func ReconcileDueMemberships(db *gorm.DB, at time.Time, limit int) (int, error) {
	if limit < 1 || limit > 1000 {
		limit = 200
	}
	staleBefore := at.Add(-time.Hour)
	var userIDs []uuid.UUID
	if err := db.Model(&model.UserLevelMembership{}).
		Where("(expires_at IS NOT NULL AND expires_at <= ?) OR (source = ? AND evaluated_at < ?)", at, "automatic", staleBefore).
		Order("evaluated_at ASC").Limit(limit).Pluck("user_id", &userIDs).Error; err != nil {
		return 0, err
	}
	processed := 0
	for _, userID := range userIDs {
		if _, _, err := ReconcileUserMembership(db, userID, at); err != nil {
			return processed, err
		}
		processed++
	}
	return processed, nil
}
