package service

import (
	"errors"
	"strings"

	"gorm.io/gorm"

	"linlinqi/api/internal/model"
)

// RefundProviderCapacityTx returns the actual provider-currency amount that
// may be refunded for an intent. Normal checkouts use the immutable expected
// intent amount. System refunds created for a signed but ineligible callback
// use the captured payment transaction instead, because an over/under-payment
// is precisely why that refund exists.
func RefundProviderCapacityTx(tx *gorm.DB, intent model.PaymentIntent, requestedBy, currency string) (int64, error) {
	if intent.Amount < 1 {
		return 0, errors.New("payment intent refund capacity is invalid")
	}
	if strings.TrimSpace(requestedBy) != "system" {
		return intent.Amount, nil
	}
	var captured int64
	err := tx.Model(&model.PaymentTransaction{}).
		Where("payment_intent_id = ? AND direction = ? AND currency = ? AND status IN ?", intent.ID, "payment", strings.ToUpper(strings.TrimSpace(currency)), []string{"succeeded", "requires_refund"}).
		Select("COALESCE(MAX(amount), 0)").Scan(&captured).Error
	if err != nil {
		return 0, err
	}
	if captured < 1 {
		return 0, errors.New("captured payment refund capacity is unavailable")
	}
	return captured, nil
}
