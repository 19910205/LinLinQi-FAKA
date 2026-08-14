package service

import (
	"encoding/json"
	"errors"
	"sort"
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"linlinqi/api/internal/model"
)

var ErrPaymentChannelNotAllowed = errors.New("payment channel is unavailable for one or more products")

// EnsurePaymentChannelAllowed is the authoritative checkout-time policy check.
// Product rows are locked in a stable order so an admin cannot replace a
// product's channel assignments between this check and order persistence.
// A product without assignment rows is intentionally unrestricted.
func EnsurePaymentChannelAllowed(tx *gorm.DB, channelID uuid.UUID, productIDs []uuid.UUID) error {
	return EnsurePaymentChannelAllowedCurrency(tx, channelID, productIDs, "")
}

func EnsurePaymentChannelAllowedCurrency(tx *gorm.DB, channelID uuid.UUID, productIDs []uuid.UUID, requestedCurrency string) error {
	if channelID == uuid.Nil || len(productIDs) == 0 {
		return ErrPaymentChannelNotAllowed
	}
	unique := make(map[uuid.UUID]struct{}, len(productIDs))
	for _, productID := range productIDs {
		if productID == uuid.Nil {
			return ErrPaymentChannelNotAllowed
		}
		unique[productID] = struct{}{}
	}
	ordered := make([]uuid.UUID, 0, len(unique))
	for productID := range unique {
		ordered = append(ordered, productID)
	}
	sort.Slice(ordered, func(left, right int) bool { return ordered[left].String() < ordered[right].String() })

	var channel model.PaymentChannel
	if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Select("id", "enabled", "supported_currencies").Where("id = ? AND enabled = ?", channelID, true).First(&channel).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ErrPaymentChannelNotAllowed
		}
		return err
	}
	var products []model.Product
	if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Select("id", "currency").Where("id IN ?", ordered).Order("id").Find(&products).Error; err != nil {
		return err
	}
	if len(products) != len(ordered) {
		return ErrPaymentChannelNotAllowed
	}
	supportedValues := []string{"CNY"}
	if len(channel.SupportedCurrencies) > 0 && json.Unmarshal(channel.SupportedCurrencies, &supportedValues) != nil {
		return ErrPaymentChannelNotAllowed
	}
	supported := make(map[string]bool, len(supportedValues))
	for _, value := range supportedValues {
		code := strings.ToUpper(strings.TrimSpace(value))
		if len(code) != 3 {
			return ErrPaymentChannelNotAllowed
		}
		supported[code] = true
	}
	requestedCurrency = strings.ToUpper(strings.TrimSpace(requestedCurrency))
	if requestedCurrency != "" && len(requestedCurrency) != 3 {
		return ErrPaymentChannelNotAllowed
	}
	if requestedCurrency != "" {
		if !supported[requestedCurrency] {
			return ErrPaymentChannelNotAllowed
		}
	}

	var assignments []model.ProductPaymentChannel
	if err := tx.Where("product_id IN ?", ordered).Find(&assignments).Error; err != nil {
		return err
	}
	restricted := make(map[uuid.UUID]bool, len(ordered))
	allowed := make(map[uuid.UUID]bool, len(ordered))
	for _, assignment := range assignments {
		restricted[assignment.ProductID] = true
		if assignment.ChannelID == channelID {
			allowed[assignment.ProductID] = true
		}
	}
	for _, productID := range ordered {
		if restricted[productID] && !allowed[productID] {
			return ErrPaymentChannelNotAllowed
		}
	}
	return nil
}
