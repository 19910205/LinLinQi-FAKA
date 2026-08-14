package service

import (
	"errors"
	"fmt"
	"math"
	"math/big"
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"linlinqi/api/internal/model"
)

var (
	ErrResellerProductUnavailable       = errors.New("product is not enabled for this reseller")
	ErrResellerWholesaleTierUnavailable = errors.New("reseller wholesale tier is not configured or enabled")
)

type ResellerWholesalePolicy struct {
	Level              int    `json:"level"`
	Name               string `json:"name"`
	DiscountBasisPoint int    `json:"discount_basis_point"`
	Configured         bool   `json:"configured"`
	Enabled            bool   `json:"enabled"`
}

type ResellerCreditState struct {
	Balance   int64 `json:"balance"`
	Frozen    int64 `json:"frozen"`
	Limit     int64 `json:"limit"`
	Exposure  int64 `json:"exposure"`
	Remaining int64 `json:"remaining"`
	Breached  bool  `json:"breached"`
}

// CalculateResellerCreditState defines credit exposure as the reseller's
// negative available balance. Frozen funds still belong to pending payouts,
// so they are included in the exposure calculation.
func CalculateResellerCreditState(balance, frozen, limit int64) (ResellerCreditState, error) {
	if frozen < 0 || limit < 0 {
		return ResellerCreditState{}, fmt.Errorf("invalid reseller credit state")
	}
	state := ResellerCreditState{Balance: balance, Frozen: frozen, Limit: limit, Remaining: limit}
	if frozen <= balance {
		return state, nil
	}
	exposure := new(big.Int).Sub(big.NewInt(frozen), big.NewInt(balance))
	overflowed := false
	if exposure.IsInt64() {
		state.Exposure = exposure.Int64()
	} else {
		// A corrupt extreme wallet must still fail closed without making a
		// customer refund fail. Saturation preserves the breach decision.
		state.Exposure = math.MaxInt64
		overflowed = true
	}
	if state.Exposure >= limit {
		state.Remaining = 0
	} else {
		state.Remaining = limit - state.Exposure
	}
	state.Breached = overflowed || state.Exposure > limit
	return state, nil
}

func LoadResellerWholesalePolicy(tx *gorm.DB, level int) (ResellerWholesalePolicy, error) {
	policy := ResellerWholesalePolicy{Level: level}
	var tier model.ResellerWholesaleTier
	if err := tx.Where("level = ?", level).First(&tier).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		return policy, nil
	} else if err != nil {
		return policy, err
	}
	policy.Name = tier.Name
	policy.DiscountBasisPoint = tier.DiscountBasisPoint
	policy.Configured = true
	policy.Enabled = tier.Enabled
	return policy, nil
}

func EffectiveResellerWholesalePolicy(tx *gorm.DB, resellerID uuid.UUID) (ResellerWholesalePolicy, error) {
	var profile model.ResellerProfile
	if err := tx.Where("id = ? AND status = ?", resellerID, "active").First(&profile).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ResellerWholesalePolicy{}, fmt.Errorf("%w: %v", ErrResellerProductUnavailable, ErrResellerWholesaleTierUnavailable)
		}
		return ResellerWholesalePolicy{}, err
	}
	policy, err := LoadResellerWholesalePolicy(tx, profile.WholesaleLevel)
	if err != nil {
		return ResellerWholesalePolicy{}, err
	}
	if !policy.Configured || !policy.Enabled || policy.DiscountBasisPoint < 0 || policy.DiscountBasisPoint > 10000 {
		return ResellerWholesalePolicy{}, fmt.Errorf("%w: %v", ErrResellerProductUnavailable, ErrResellerWholesaleTierUnavailable)
	}
	return policy, nil
}

// WholesaleSettlementPrice applies an explicit tier policy to the public list
// price. Rounding up ensures the configured discount can never exceed the
// operator-approved basis points because of integer currency arithmetic.
func WholesaleSettlementPrice(publicPrice int64, discountBasisPoint int) (int64, error) {
	if publicPrice < 0 || discountBasisPoint < 0 || discountBasisPoint > 10000 {
		return 0, ErrResellerWholesaleTierUnavailable
	}
	return roundedRatio(publicPrice, int64(10000-discountBasisPoint), 10000, true)
}

// ResolveResellerSalePrice resolves an exact variant rule first and then the
// product-wide fallback. An explicit disabled variant rule intentionally masks
// the fallback so a reseller can remove one variant without removing the
// entire product.
func ResolveResellerSalePrice(tx *gorm.DB, resellerID, productID uuid.UUID, variantID *uuid.UUID, platformPrice int64) (int64, bool, error) {
	if resellerID == uuid.Nil || productID == uuid.Nil || platformPrice < 0 {
		return 0, false, ErrResellerProductUnavailable
	}
	query := tx.Model(&model.ResellerProductRule{}).
		Joins("JOIN reseller_profiles rp ON rp.id = reseller_product_rules.reseller_id AND rp.deleted_at IS NULL AND rp.status = ?", "active").
		Where("reseller_product_rules.reseller_id = ? AND reseller_product_rules.product_id = ?", resellerID, productID)
	if variantID == nil {
		query = query.Where("reseller_product_rules.variant_id IS NULL")
	} else {
		query = query.Where("reseller_product_rules.variant_id IS NULL OR reseller_product_rules.variant_id = ?", *variantID).
			Order("(reseller_product_rules.variant_id IS NOT NULL) DESC")
	}
	var rule model.ResellerProductRule
	if err := query.Take(&rule).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, false, nil
	} else if err != nil {
		return 0, false, err
	}
	if !rule.Enabled {
		return 0, false, nil
	}

	var price int64
	switch rule.PricingMode {
	case "fixed":
		var productCurrency string
		if err := tx.Model(&model.Product{}).Where("id = ?", productID).Pluck("currency", &productCurrency).Error; err != nil || strings.ToUpper(strings.TrimSpace(rule.Currency)) != strings.ToUpper(strings.TrimSpace(productCurrency)) {
			return 0, false, ErrResellerProductUnavailable
		}
		price = rule.FixedPrice
	case "markup":
		if rule.MarkupBasisPoint < 0 || rule.MarkupBasisPoint > 10000 {
			return 0, false, ErrResellerProductUnavailable
		}
		markup, err := roundedRatio(platformPrice, int64(rule.MarkupBasisPoint), 10000, true)
		if err != nil || markup > math.MaxInt64-platformPrice {
			return 0, false, ErrResellerProductUnavailable
		}
		price = platformPrice + markup
	default:
		return 0, false, ErrResellerProductUnavailable
	}
	if price < platformPrice {
		return 0, false, ErrResellerProductUnavailable
	}
	return price, true, nil
}

// CreditResellerMarginTx credits the reseller only after the whole order has
// been delivered. The unique ledger number makes callback and worker recovery
// retries safe.
func CreditResellerMarginTx(tx *gorm.DB, order model.Order) error {
	if order.ID == uuid.Nil {
		return nil
	}
	var lockedOrder model.Order
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&lockedOrder, "id = ?", order.ID).Error; err != nil {
		return err
	}
	order = lockedOrder
	if order.ResellerID == nil || order.ResellerMargin <= 0 || order.PaymentStatus != "paid" || (order.Status != "delivered" && order.Status != "completed") {
		return nil
	}
	entryNo := "reseller-margin:" + order.ID.String()
	var existing model.WalletEntry
	if err := tx.Where("entry_no = ?", entryNo).First(&existing).Error; err == nil {
		return nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	creditAmount := order.ResellerMargin - order.ResellerMarginReversed
	if creditAmount <= 0 {
		return nil
	}
	account, err := lockedResellerWallet(tx, *order.ResellerID, order.Currency)
	if err != nil {
		return err
	}
	balance, err := checkedAddInt64(account.Balance, creditAmount)
	if err != nil {
		return err
	}
	referenceID := order.ID
	entry := model.WalletEntry{AccountID: account.ID, EntryNo: entryNo, Type: "reseller_margin", Amount: creditAmount, BalanceAfter: balance, ReferenceType: "order", ReferenceID: &referenceID, Description: "分销订单利润入账"}
	if err := tx.Create(&entry).Error; err != nil {
		return err
	}
	return tx.Model(&account).Updates(map[string]any{"balance": balance, "version": gorm.Expr("version + 1")}).Error
}

// ReverseResellerMarginTx applies the aggregate successful-refund amount to
// the already credited margin. The reseller balance may become negative: this
// represents a recoverable receivable and prevents the platform from silently
// absorbing a refund after the reseller has withdrawn their earnings.
func ReverseResellerMarginTx(tx *gorm.DB, order model.Order, refundedAmount int64) error {
	if order.ResellerID == nil || refundedAmount <= 0 {
		return nil
	}
	var locked model.Order
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&locked, "id = ?", order.ID).Error; err != nil {
		return err
	}
	if locked.ResellerID == nil || locked.ResellerMargin <= 0 || locked.Total <= 0 {
		return nil
	}
	target, err := roundedRatio(locked.ResellerMargin, min(refundedAmount, locked.Total), locked.Total, true)
	if err != nil {
		return err
	}
	if target > locked.ResellerMargin {
		target = locked.ResellerMargin
	}
	delta := target - locked.ResellerMarginReversed
	if delta <= 0 {
		return nil
	}
	var creditEntry model.WalletEntry
	creditEntryNo := "reseller-margin:" + locked.ID.String()
	if err := tx.Where("entry_no = ?", creditEntryNo).First(&creditEntry).Error; errors.Is(err, gorm.ErrRecordNotFound) {
		return tx.Model(&locked).Update("reseller_margin_reversed", target).Error
	} else if err != nil {
		return err
	}
	entryNo := fmt.Sprintf("reseller-rev:%s:%d", locked.ID, target)
	var existing model.WalletEntry
	if err := tx.Where("entry_no = ?", entryNo).First(&existing).Error; err == nil {
		return tx.Model(&locked).Update("reseller_margin_reversed", target).Error
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	var profile model.ResellerProfile
	if err := tx.Unscoped().Clauses(clause.Locking{Strength: "UPDATE"}).First(&profile, "id = ?", *locked.ResellerID).Error; err != nil {
		return err
	}
	account, err := lockedResellerWallet(tx, *locked.ResellerID, locked.Currency)
	if err != nil {
		return err
	}
	balance, err := checkedAddInt64(account.Balance, -delta)
	if err != nil {
		return err
	}
	referenceID := locked.ID
	entry := model.WalletEntry{AccountID: account.ID, EntryNo: entryNo, Type: "reseller_margin_reversal", Amount: -delta, BalanceAfter: balance, ReferenceType: "order_refund", ReferenceID: &referenceID, Description: "分销订单退款利润冲回"}
	if err := tx.Create(&entry).Error; err != nil {
		return err
	}
	if err := tx.Model(&account).Updates(map[string]any{"balance": balance, "version": gorm.Expr("version + 1")}).Error; err != nil {
		return err
	}
	creditLimit, err := ResellerCreditLimit(tx, profile.ID, locked.Currency)
	if err != nil {
		return err
	}
	credit, err := CalculateResellerCreditState(balance, account.Frozen, creditLimit)
	if err != nil {
		return err
	}
	if credit.Breached {
		action := "breach_recorded"
		if profile.Status == "active" {
			if err := tx.Model(&profile).Update("status", "suspended").Error; err != nil {
				return err
			}
			action = "auto_suspended"
		}
		orderID, entryID := locked.ID, entry.ID
		event := model.ResellerCreditEvent{
			EventKey:      "reseller-credit:" + entry.ID.String(),
			ResellerID:    profile.ID,
			OrderID:       &orderID,
			WalletEntryID: &entryID,
			Type:          "credit_limit_breached",
			Balance:       credit.Balance,
			Frozen:        credit.Frozen,
			Exposure:      credit.Exposure,
			CreditLimit:   credit.Limit,
			Currency:      locked.Currency,
			Action:        action,
		}
		if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "event_key"}}, DoNothing: true}).Create(&event).Error; err != nil {
			return err
		}
	}
	return tx.Model(&locked).Update("reseller_margin_reversed", target).Error
}

func lockedResellerWallet(tx *gorm.DB, resellerID uuid.UUID, currency string) (model.WalletAccount, error) {
	currency = strings.ToUpper(strings.TrimSpace(currency))
	if len(currency) != 3 {
		return model.WalletAccount{}, ErrCurrencyMismatch
	}
	lookup := model.WalletAccount{OwnerType: "reseller", OwnerID: resellerID, Currency: currency}
	if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "owner_type"}, {Name: "owner_id"}, {Name: "currency"}}, DoNothing: true}).Create(&lookup).Error; err != nil {
		return model.WalletAccount{}, err
	}
	var account model.WalletAccount
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("owner_type = ? AND owner_id = ? AND currency = ?", "reseller", resellerID, currency).First(&account).Error
	return account, err
}

func ResellerCreditLimit(db *gorm.DB, resellerID uuid.UUID, currencyCode string) (int64, error) {
	currencyCode = strings.ToUpper(strings.TrimSpace(currencyCode))
	if resellerID == uuid.Nil || len(currencyCode) != 3 {
		return 0, ErrCurrencyMismatch
	}
	var policy model.ResellerCreditPolicy
	if err := db.Where("reseller_id = ? AND currency = ?", resellerID, currencyCode).First(&policy).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return 0, nil
		}
		return 0, err
	}
	return policy.CreditLimit, nil
}

func LockResellerCreditPolicy(tx *gorm.DB, resellerID uuid.UUID, currencyCode string) (model.ResellerCreditPolicy, error) {
	currencyCode = strings.ToUpper(strings.TrimSpace(currencyCode))
	if resellerID == uuid.Nil || len(currencyCode) != 3 {
		return model.ResellerCreditPolicy{}, ErrCurrencyMismatch
	}
	seed := model.ResellerCreditPolicy{ResellerID: resellerID, Currency: currencyCode}
	if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "reseller_id"}, {Name: "currency"}}, DoNothing: true}).Create(&seed).Error; err != nil {
		return model.ResellerCreditPolicy{}, err
	}
	var policy model.ResellerCreditPolicy
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("reseller_id = ? AND currency = ?", resellerID, currencyCode).First(&policy).Error
	return policy, err
}

func roundedRatio(value, multiplier, divisor int64, roundUp bool) (int64, error) {
	if value < 0 || multiplier < 0 || divisor <= 0 {
		return 0, fmt.Errorf("invalid ratio")
	}
	numerator := new(big.Int).Mul(big.NewInt(value), big.NewInt(multiplier))
	if roundUp && numerator.Sign() > 0 {
		numerator.Add(numerator, big.NewInt(divisor-1))
	}
	result := numerator.Quo(numerator, big.NewInt(divisor))
	if !result.IsInt64() {
		return 0, fmt.Errorf("ratio overflows int64")
	}
	return result.Int64(), nil
}

func roundedNearestRatio(value, multiplier, divisor int64) (int64, error) {
	if value < 0 || multiplier < 0 || divisor <= 0 {
		return 0, fmt.Errorf("invalid ratio")
	}
	numerator := new(big.Int).Mul(big.NewInt(value), big.NewInt(multiplier))
	numerator.Add(numerator, big.NewInt(divisor/2))
	result := numerator.Quo(numerator, big.NewInt(divisor))
	if !result.IsInt64() {
		return 0, fmt.Errorf("ratio overflows int64")
	}
	return result.Int64(), nil
}

func checkedMultiplyInt64(left, right int64) (int64, error) {
	result := new(big.Int).Mul(big.NewInt(left), big.NewInt(right))
	if !result.IsInt64() {
		return 0, fmt.Errorf("multiplication overflows int64")
	}
	return result.Int64(), nil
}

func checkedAddInt64(left, right int64) (int64, error) {
	result := new(big.Int).Add(big.NewInt(left), big.NewInt(right))
	if !result.IsInt64() {
		return 0, fmt.Errorf("wallet balance overflows int64")
	}
	return result.Int64(), nil
}
