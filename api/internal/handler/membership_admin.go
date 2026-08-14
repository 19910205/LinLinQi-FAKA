package handler

import (
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"linlinqi/api/internal/model"
	"linlinqi/api/internal/service"
	"linlinqi/api/pkg/response"
)

var errManualMembershipRequired = errors.New("manual membership override is required")

type adminMembershipLevelItem struct {
	ID                 uuid.UUID `json:"id"`
	Code               string    `json:"code"`
	Name               string    `json:"name"`
	Currency           string    `json:"currency"`
	MinimumSpend       int64     `json:"minimum_spend"`
	DiscountBasisPoint int       `json:"discount_basis_point"`
	Priority           int       `json:"priority"`
}

type adminMembershipItem struct {
	UserID             uuid.UUID  `json:"user_id"`
	MemberLevelID      uuid.UUID  `json:"member_level_id"`
	LevelCode          string     `json:"level_code"`
	LevelName          string     `json:"level_name"`
	DiscountBasisPoint int        `json:"discount_basis_point"`
	Source             string     `json:"source"`
	GrantedAt          time.Time  `json:"granted_at"`
	GrantedBy          *uuid.UUID `json:"granted_by"`
	GrantedByName      string     `json:"granted_by_name"`
	ExpiresAt          *time.Time `json:"expires_at"`
	EvaluatedAt        time.Time  `json:"evaluated_at"`
}

func loadAdminMembership(db *gorm.DB, userID uuid.UUID) (*adminMembershipItem, error) {
	var item adminMembershipItem
	result := db.Table("user_level_memberships ulm").
		Select(`ulm.user_id, ulm.member_level_id, ml.code AS level_code, ml.name AS level_name,
			ml.discount_basis_point, ulm.source, ulm.granted_at, ulm.granted_by,
			COALESCE(a.name, '') AS granted_by_name, ulm.expires_at, ulm.evaluated_at`).
		Joins("JOIN member_levels ml ON ml.id = ulm.member_level_id AND ml.deleted_at IS NULL").
		Joins("LEFT JOIN admins a ON a.id = ulm.granted_by AND a.deleted_at IS NULL").
		Where("ulm.user_id = ?", userID).Take(&item)
	if result.Error != nil {
		return nil, result.Error
	}
	return &item, nil
}

func (h Handler) AdminCustomerMembershipLevels(c *gin.Context) {
	currencyCode, err := service.StoreCurrency(h.DB)
	if err != nil {
		response.Error(c, 500, 50601, "error.store_currency_fetch_failed")
		return
	}
	var levels []model.MemberLevel
	if err := h.DB.Where("enabled = ? AND currency = ?", true, currencyCode).Order("minimum_spend ASC, priority ASC, created_at ASC").Find(&levels).Error; err != nil {
		response.Error(c, 500, 50601, "error.customer_membership_levels_fetch_failed")
		return
	}
	items := make([]adminMembershipLevelItem, 0, len(levels))
	for _, level := range levels {
		items = append(items, adminMembershipLevelItem{
			ID: level.ID, Code: level.Code, Name: level.Name, Currency: level.Currency, MinimumSpend: level.MinimumSpend,
			DiscountBasisPoint: level.DiscountBasisPoint, Priority: level.Priority,
		})
	}
	response.OK(c, items)
}

type adminMembershipGrantRequest struct {
	MemberLevelID string     `json:"member_level_id"`
	ExpiresAt     *time.Time `json:"expires_at"`
	Evidence      string     `json:"evidence"`
}

type adminMembershipActionRequest struct {
	Evidence string `json:"evidence"`
}

func validMembershipEvidence(value string) (string, bool) {
	value = strings.TrimSpace(value)
	length := utf8.RuneCountInString(value)
	return value, length >= 4 && length <= 1000 && !strings.ContainsRune(value, '\x00')
}

func (h Handler) GrantAdminCustomerMembership(c *gin.Context) {
	userID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, 422, 42250, "error.customer_number_invalid")
		return
	}
	reason, ok := requireAdminChangeReason(c, "授予客户会员等级")
	if !ok {
		return
	}
	var req adminMembershipGrantRequest
	if decodeStrictJSON(c, &req) != nil {
		response.Error(c, 422, 42601, "error.customer_membership_grant_fields_invalid")
		return
	}
	levelID, err := uuid.Parse(strings.TrimSpace(req.MemberLevelID))
	if err != nil {
		response.Error(c, 422, 42601, "error.customer_membership_level_id_invalid")
		return
	}
	req.Evidence, ok = validMembershipEvidence(req.Evidence)
	now := time.Now().UTC()
	if !ok || (req.ExpiresAt != nil && (!req.ExpiresAt.After(now.Add(time.Minute)) || req.ExpiresAt.After(now.AddDate(10, 0, 0)))) {
		response.Error(c, 422, 42601, "error.customer_membership_grant_fields_invalid")
		return
	}
	adminID, err := uuid.Parse(c.GetString("subject"))
	if err != nil {
		response.Error(c, 401, 40103, "error.invalid_admin_identity")
		return
	}
	err = h.DB.Transaction(func(tx *gorm.DB) error {
		_, err := service.GrantManualMembershipTx(tx, userID, levelID, adminID, req.ExpiresAt, now)
		return err
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		response.Error(c, 404, 40441, "error.customer_not_found")
		return
	}
	if errors.Is(err, service.ErrMembershipLevelUnavailable) {
		response.Error(c, 422, 42602, "error.customer_membership_level_unavailable")
		return
	}
	if err != nil {
		response.Error(c, 500, 50602, "error.customer_membership_grant_failed")
		return
	}
	item, err := loadAdminMembership(h.DB, userID)
	if err != nil {
		response.Error(c, 500, 50603, "error.customer_membership_fetch_failed")
		return
	}
	h.audit(c, "customer.membership.grant", "user", userID.String(), reason+"；evidence="+req.Evidence+"；level_id="+levelID.String())
	response.OK(c, item)
}

func (h Handler) RevokeAdminCustomerMembership(c *gin.Context) {
	userID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, 422, 42250, "error.customer_number_invalid")
		return
	}
	reason, ok := requireAdminChangeReason(c, "撤销客户会员等级")
	if !ok {
		return
	}
	var req adminMembershipActionRequest
	if decodeStrictJSON(c, &req) != nil {
		response.Error(c, 422, 42603, "error.customer_membership_action_fields_invalid")
		return
	}
	req.Evidence, ok = validMembershipEvidence(req.Evidence)
	if !ok {
		response.Error(c, 422, 42603, "error.customer_membership_evidence_invalid")
		return
	}
	deleted := false
	err = h.DB.Transaction(func(tx *gorm.DB) error {
		var user model.User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").First(&user, "id = ?", userID).Error; err != nil {
			return err
		}
		var current model.UserLevelMembership
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ?", userID).First(&current).Error; err != nil {
			return err
		}
		if current.Source != "manual" {
			return errManualMembershipRequired
		}
		result := tx.Where("user_id = ?", userID).Delete(&model.UserLevelMembership{})
		deleted = result.RowsAffected == 1
		if result.Error != nil {
			return result.Error
		}
		_, _, err := service.ReconcileUserMembershipTx(tx, userID, time.Now().UTC())
		return err
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		response.Error(c, 404, 40441, "error.customer_not_found")
		return
	}
	if errors.Is(err, errManualMembershipRequired) {
		response.Error(c, 409, 40602, "error.customer_membership_manual_override_required")
		return
	}
	if err != nil {
		response.Error(c, 500, 50604, "error.customer_membership_revoke_failed")
		return
	}
	if !deleted {
		response.Error(c, 409, 40601, "error.customer_has_no_membership")
		return
	}
	h.audit(c, "customer.membership.revoke", "user", userID.String(), reason+"；evidence="+req.Evidence)
	item, _ := loadAdminMembership(h.DB, userID)
	response.OK(c, gin.H{"revoked": true, "membership": item})
}

func (h Handler) RecalculateAdminCustomerMembership(c *gin.Context) {
	userID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, 422, 42250, "error.customer_number_invalid")
		return
	}
	reason, ok := requireAdminChangeReason(c, "重新计算客户会员等级")
	if !ok {
		return
	}
	var req adminMembershipActionRequest
	if decodeStrictJSON(c, &req) != nil {
		response.Error(c, 422, 42603, "error.customer_membership_action_fields_invalid")
		return
	}
	req.Evidence, ok = validMembershipEvidence(req.Evidence)
	if !ok {
		response.Error(c, 422, 42603, "error.customer_membership_evidence_invalid")
		return
	}
	// Explicit recalculation releases a live manual override first.
	var membership *model.UserLevelMembership
	var changed bool
	err = h.DB.Transaction(func(tx *gorm.DB) error {
		var user model.User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").First(&user, "id = ?", userID).Error; err != nil {
			return err
		}
		if err := tx.Where("user_id = ?", userID).Delete(&model.UserLevelMembership{}).Error; err != nil {
			return err
		}
		var reconcileErr error
		membership, changed, reconcileErr = service.ReconcileUserMembershipTx(tx, userID, time.Now().UTC())
		return reconcileErr
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		response.Error(c, 404, 40441, "error.customer_not_found")
		return
	}
	if err != nil {
		response.Error(c, 500, 50605, "error.customer_membership_recalculation_failed")
		return
	}
	h.audit(c, "customer.membership.recalculate", "user", userID.String(), reason+"；evidence="+req.Evidence)
	if membership == nil {
		response.OK(c, gin.H{"membership": nil, "changed": changed})
		return
	}
	item, loadErr := loadAdminMembership(h.DB, userID)
	if loadErr != nil {
		response.Error(c, 500, 50603, "error.customer_membership_fetch_failed")
		return
	}
	response.OK(c, gin.H{"membership": item, "changed": changed})
}
