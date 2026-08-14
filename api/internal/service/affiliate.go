package service

import (
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"linlinqi/api/internal/model"
)

// CreateAffiliateCommissionTx records one immutable gross commission per
// delivered order. The unique order constraint makes every delivery recovery
// path idempotent.
func CreateAffiliateCommissionTx(tx *gorm.DB, order model.Order, deliveredAt time.Time) error {
	if order.UserID == nil || order.ResellerID != nil || order.PaymentStatus != "paid" {
		return nil
	}
	var referral model.AffiliateReferral
	if err := tx.Where("referred_user_id = ?", *order.UserID).First(&referral).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	} else if err != nil {
		return err
	}
	var profile model.AffiliateProfile
	if err := tx.Where("id = ? AND status = ?", referral.AffiliateID, "active").First(&profile).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	} else if err != nil {
		return err
	}
	if profile.UserID == *order.UserID || profile.CommissionBasisPoint < 1 || profile.CommissionBasisPoint > 3000 {
		return nil
	}
	orderAmount := order.Subtotal - order.Discount
	if orderAmount <= 0 {
		return nil
	}
	commissionAmount, err := roundedRatio(orderAmount, int64(profile.CommissionBasisPoint), 10000, false)
	if err != nil {
		return err
	}
	if commissionAmount <= 0 {
		return nil
	}
	currencyCode := strings.ToUpper(strings.TrimSpace(order.Currency))
	if len(currencyCode) != 3 {
		return ErrCurrencyMismatch
	}
	holdDays := 7
	var setting model.Setting
	if tx.Select("value").First(&setting, "key = ?", "affiliate_hold_days").Error == nil {
		if parsed, err := strconv.Atoi(setting.Value); err == nil && parsed >= 1 && parsed <= 90 {
			holdDays = parsed
		}
	}
	commission := model.AffiliateCommission{
		AffiliateID: profile.ID, OrderID: order.ID, BuyerID: order.UserID, OrderAmount: orderAmount,
		Commission: commissionAmount, Currency: currencyCode, Status: "pending", SettlesAt: deliveredAt.AddDate(0, 0, holdDays),
	}
	created := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "order_id"}}, DoNothing: true}).Create(&commission)
	if created.Error != nil {
		return created.Error
	}
	if created.RowsAffected == 0 {
		return nil
	}
	balance, err := LockAffiliateBalance(tx, profile.ID, currencyCode)
	if err != nil {
		return err
	}
	return tx.Model(&balance).UpdateColumn("total_commission", gorm.Expr("total_commission + ?", commissionAmount)).Error
}

// LockAffiliateBalance returns the currency-scoped affiliate ledger under a
// row lock, creating it idempotently when first used.
func LockAffiliateBalance(tx *gorm.DB, affiliateID uuid.UUID, currencyCode string) (model.AffiliateBalance, error) {
	currencyCode = strings.ToUpper(strings.TrimSpace(currencyCode))
	if affiliateID == uuid.Nil || len(currencyCode) != 3 {
		return model.AffiliateBalance{}, ErrCurrencyMismatch
	}
	seed := model.AffiliateBalance{AffiliateID: affiliateID, Currency: currencyCode}
	if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "affiliate_id"}, {Name: "currency"}}, DoNothing: true}).Create(&seed).Error; err != nil {
		return model.AffiliateBalance{}, err
	}
	var balance model.AffiliateBalance
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("affiliate_id = ? AND currency = ?", affiliateID, currencyCode).First(&balance).Error
	return balance, err
}

func SettleAffiliateCommissions(db *gorm.DB, limit int) (int64, error) {
	if limit < 1 || limit > 2000 {
		limit = 200
	}
	var candidates []model.AffiliateCommission
	if err := db.Select("id").Where("settled_at IS NULL AND settles_at <= ? AND status IN ?", time.Now(), []string{"pending", "partially_reversed"}).Order("settles_at ASC").Limit(limit).Find(&candidates).Error; err != nil {
		return 0, err
	}
	var settled int64
	for _, candidate := range candidates {
		err := db.Transaction(func(tx *gorm.DB) error {
			var commission model.AffiliateCommission
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&commission, "id = ?", candidate.ID).Error; err != nil {
				return err
			}
			if commission.SettledAt != nil || commission.SettlesAt.After(time.Now()) || (commission.Status != "pending" && commission.Status != "partially_reversed") {
				return nil
			}
			now := time.Now()
			net := commission.Commission - commission.ReversedAmount
			if net <= 0 {
				return tx.Model(&commission).Updates(map[string]any{"status": "reversed", "settled_at": &now}).Error
			}
			balance, err := LockAffiliateBalance(tx, commission.AffiliateID, commission.Currency)
			if err != nil {
				return err
			}
			if err := tx.Model(&balance).UpdateColumn("available_commission", gorm.Expr("available_commission + ?", net)).Error; err != nil {
				return err
			}
			status := "available"
			if commission.ReversedAmount > 0 {
				status = "partially_reversed"
			}
			if err := tx.Model(&commission).Updates(map[string]any{"status": status, "settled_at": &now}).Error; err != nil {
				return err
			}
			settled++
			return nil
		})
		if err != nil {
			return settled, err
		}
	}
	return settled, nil
}

// ReverseAffiliateCommissionTx applies the aggregate successful refund amount
// to the commission. Re-running a refund callback is safe because the target
// reversal is derived from the order's total refunded amount.
func ReverseAffiliateCommissionTx(tx *gorm.DB, order model.Order, refundedAmount int64) error {
	if refundedAmount <= 0 || order.Total <= 0 {
		return nil
	}
	var commission model.AffiliateCommission
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("order_id = ?", order.ID).First(&commission).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	targetReversal := affiliateReversalTarget(commission.Commission, refundedAmount, order.Total)
	delta := targetReversal - commission.ReversedAmount
	if delta <= 0 {
		return nil
	}
	updates := map[string]any{
		"total_commission": gorm.Expr("GREATEST(total_commission - ?, 0)", delta),
	}
	if commission.SettledAt != nil {
		updates["available_commission"] = gorm.Expr("available_commission - ?", delta)
	}
	balance, err := LockAffiliateBalance(tx, commission.AffiliateID, commission.Currency)
	if err != nil {
		return err
	}
	if err := tx.Model(&balance).Updates(updates).Error; err != nil {
		return err
	}
	status := "partially_reversed"
	if targetReversal >= commission.Commission {
		status = "reversed"
	}
	return tx.Model(&commission).Updates(map[string]any{"reversed_amount": targetReversal, "status": status}).Error
}

func affiliateReversalTarget(commission, refundedAmount, orderTotal int64) int64 {
	if commission <= 0 || refundedAmount <= 0 || orderTotal <= 0 {
		return 0
	}
	if refundedAmount >= orderTotal {
		return commission
	}
	// Round the clawback up to the nearest minor unit so repeated partial
	// refunds can never leave a paid-out fraction of a cent behind.
	target, err := roundedRatio(commission, refundedAmount, orderTotal, true)
	if err != nil {
		return commission
	}
	if target > commission {
		return commission
	}
	return target
}

func AffiliateProfileForReferral(tx *gorm.DB, referralCode string) (*model.AffiliateProfile, error) {
	referralCode = strings.ToUpper(strings.TrimSpace(referralCode))
	if referralCode == "" {
		return nil, nil
	}
	var profile model.AffiliateProfile
	if err := tx.Where("UPPER(referral_code) = ? AND status = ?", referralCode, "active").First(&profile).Error; err != nil {
		return nil, err
	}
	return &profile, nil
}

func CreateAffiliateReferral(tx *gorm.DB, affiliate model.AffiliateProfile, referredUserID uuid.UUID) error {
	if affiliate.ID == uuid.Nil || affiliate.UserID == referredUserID {
		return errors.New("invalid affiliate referral")
	}
	referral := model.AffiliateReferral{AffiliateID: affiliate.ID, ReferredUserID: referredUserID, ReferralCode: affiliate.ReferralCode, AttributedAt: time.Now()}
	return tx.Create(&referral).Error
}
