package handler

import (
	"errors"
	"fmt"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"linlinqi/api/internal/model"
)

var (
	errPaymentChannelConfigHasHistory = errors.New("payment channel connector config has financial history")
	errPaymentChannelChanged          = errors.New("payment channel changed while checkout was being created")
)

func lockPaymentChannelIdentityTx(tx *gorm.DB, channelID uuid.UUID) error {
	return tx.Exec(
		"SELECT pg_advisory_xact_lock(hashtextextended(?, 20260810))",
		"linlinqi-payment-channel:"+channelID.String(),
	).Error
}

// lockCurrentPaymentChannelTx serializes a new financial fact with connector
// rotation and rejects a stale handler snapshot. Without the re-read, a
// checkout could load the old secret, wait behind a rotation, then persist a
// new intent that only the old (now unavailable) secret can verify.
func lockCurrentPaymentChannelTx(tx *gorm.DB, snapshot model.PaymentChannel) error {
	if err := lockPaymentChannelIdentityTx(tx, snapshot.ID); err != nil {
		return err
	}
	var current model.PaymentChannel
	if err := tx.Select("id", "enabled", "updated_at").First(&current, "id = ?", snapshot.ID).Error; err != nil {
		return err
	}
	if !current.Enabled || !current.UpdatedAt.Equal(snapshot.UpdatedAt) {
		return fmt.Errorf("%w: channel snapshot is stale", errPaymentChannelChanged)
	}
	return nil
}

// paymentChannelHasFinancialHistory treats a connector identity as immutable
// after it has been referenced by a checkout or recharge. Historical callbacks
// and refunds must keep using the merchant, endpoint and signing secret that
// accepted the original payment; operators rotate by creating a new channel
// and disabling (not deleting) the old one.
func paymentChannelHasFinancialHistory(db *gorm.DB, channelID uuid.UUID) (bool, error) {
	var count int64
	if err := db.Unscoped().Model(&model.PaymentIntent{}).Where("channel_id = ?", channelID).Limit(1).Count(&count).Error; err != nil {
		return false, err
	}
	if count > 0 {
		return true, nil
	}
	if err := db.Unscoped().Model(&model.RechargeOrder{}).Where("channel_id = ?", channelID).Limit(1).Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

func paymentDriverConfigChanged(before, after paymentDriverConfig) bool {
	return before.BaseURL != after.BaseURL ||
		before.MerchantID != after.MerchantID || before.Secret != after.Secret ||
		before.APIToken != after.APIToken || before.TradeType != after.TradeType ||
		before.Fiat != after.Fiat || before.Timeout != after.Timeout
}
