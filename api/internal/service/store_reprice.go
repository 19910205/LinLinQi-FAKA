package service

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	fx "linlinqi/api/internal/currency"
	"linlinqi/api/internal/model"
)

type StoreRepriceResult struct {
	Products               int `json:"products"`
	Variants               int `json:"variants"`
	PriceTiers             int `json:"price_tiers"`
	Promotions             int `json:"promotions"`
	Coupons                int `json:"coupons"`
	MemberLevels           int `json:"member_levels"`
	ResellerRules          int `json:"reseller_rules"`
	ResellerCreditPolicies int `json:"reseller_credit_policies"`
}

type storePromotionRule struct {
	BasisPoints int   `json:"basis_points"`
	Amount      int64 `json:"amount"`
	MinAmount   int64 `json:"min_amount"`
	MaxDiscount int64 `json:"max_discount"`
	UnitPrice   int64 `json:"unit_price"`
}

func convertStoreAmount(amount int64, source, target model.CurrencyDefinition, rate string) (int64, error) {
	if amount == 0 {
		return 0, nil
	}
	return fx.Convert(amount, source.MinorUnit, target.MinorUnit, rate)
}

// RepriceStoreCurrencyTx changes mutable pricing policies only. Immutable
// financial facts (orders, refunds, wallets, gift cards, commissions and
// procurement records) intentionally remain in their original currency.
func RepriceStoreCurrencyTx(tx *gorm.DB, source, target model.CurrencyDefinition, snapshot model.FXRateSnapshot) (StoreRepriceResult, error) {
	result := StoreRepriceResult{}
	if source.Code == target.Code {
		return result, nil
	}
	if !strings.EqualFold(snapshot.BaseCode, source.Code) || !strings.EqualFold(snapshot.QuoteCode, target.Code) || strings.TrimSpace(snapshot.Rate) == "" {
		return result, ErrCurrencyMismatch
	}

	var products []model.Product
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("currency = ?", source.Code).Find(&products).Error; err != nil {
		return result, err
	}
	for _, product := range products {
		price, err := convertStoreAmount(product.Price, source, target, snapshot.Rate)
		if err != nil {
			return result, err
		}
		comparePrice, err := convertStoreAmount(product.ComparePrice, source, target, snapshot.Rate)
		if err != nil {
			return result, err
		}
		costPrice, err := convertStoreAmount(product.CostPrice, source, target, snapshot.Rate)
		if err != nil {
			return result, err
		}
		if err := tx.Model(&product).Updates(map[string]any{"price": price, "compare_price": comparePrice, "cost_price": costPrice, "currency": target.Code}).Error; err != nil {
			return result, err
		}
		result.Products++

		var variants []model.ProductVariant
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("product_id = ?", product.ID).Find(&variants).Error; err != nil {
			return result, err
		}
		for _, variant := range variants {
			variantPrice, err := convertStoreAmount(variant.Price, source, target, snapshot.Rate)
			if err != nil {
				return result, err
			}
			variantCompare, err := convertStoreAmount(variant.ComparePrice, source, target, snapshot.Rate)
			if err != nil {
				return result, err
			}
			variantCost, err := convertStoreAmount(variant.CostPrice, source, target, snapshot.Rate)
			if err != nil {
				return result, err
			}
			if err := tx.Model(&variant).Updates(map[string]any{"price": variantPrice, "compare_price": variantCompare, "cost_price": variantCost}).Error; err != nil {
				return result, err
			}
			result.Variants++
		}

		var tiers []model.ProductPriceTier
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("product_id = ?", product.ID).Find(&tiers).Error; err != nil {
			return result, err
		}
		for _, tier := range tiers {
			unitPrice, err := convertStoreAmount(tier.UnitPrice, source, target, snapshot.Rate)
			if err != nil {
				return result, err
			}
			if err := tx.Model(&tier).Update("unit_price", unitPrice).Error; err != nil {
				return result, err
			}
			result.PriceTiers++
		}
	}

	var promotions []model.Promotion
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("currency = ?", source.Code).Find(&promotions).Error; err != nil {
		return result, err
	}
	for _, promotion := range promotions {
		var rule storePromotionRule
		if err := json.Unmarshal([]byte(promotion.Rules), &rule); err != nil {
			return result, err
		}
		var err error
		if rule.Amount, err = convertStoreAmount(rule.Amount, source, target, snapshot.Rate); err != nil {
			return result, err
		}
		if rule.MinAmount, err = convertStoreAmount(rule.MinAmount, source, target, snapshot.Rate); err != nil {
			return result, err
		}
		if rule.MaxDiscount, err = convertStoreAmount(rule.MaxDiscount, source, target, snapshot.Rate); err != nil {
			return result, err
		}
		if rule.UnitPrice, err = convertStoreAmount(rule.UnitPrice, source, target, snapshot.Rate); err != nil {
			return result, err
		}
		rules, err := json.Marshal(rule)
		if err != nil {
			return result, err
		}
		if err := tx.Model(&promotion).Updates(map[string]any{"currency": target.Code, "rules": string(rules)}).Error; err != nil {
			return result, err
		}
		result.Promotions++
	}

	var coupons []model.Coupon
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("currency = ?", source.Code).Find(&coupons).Error; err != nil {
		return result, err
	}
	for _, coupon := range coupons {
		minimum, err := convertStoreAmount(coupon.MinAmount, source, target, snapshot.Rate)
		if err != nil {
			return result, err
		}
		value := coupon.Value
		if strings.EqualFold(coupon.Type, "fixed") {
			value, err = convertStoreAmount(coupon.Value, source, target, snapshot.Rate)
			if err != nil {
				return result, err
			}
		}
		if err := tx.Model(&coupon).Updates(map[string]any{"currency": target.Code, "min_amount": minimum, "value": value}).Error; err != nil {
			return result, err
		}
		result.Coupons++
	}

	var levels []model.MemberLevel
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("currency = ?", source.Code).Find(&levels).Error; err != nil {
		return result, err
	}
	for _, level := range levels {
		minimum, err := convertStoreAmount(level.MinimumSpend, source, target, snapshot.Rate)
		if err != nil {
			return result, err
		}
		if err := tx.Model(&level).Updates(map[string]any{"currency": target.Code, "minimum_spend": minimum}).Error; err != nil {
			return result, err
		}
		result.MemberLevels++
	}

	var rules []model.ResellerProductRule
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("currency = ?", source.Code).Find(&rules).Error; err != nil {
		return result, err
	}
	for _, rule := range rules {
		fixedPrice := rule.FixedPrice
		var err error
		if rule.PricingMode == "fixed" {
			fixedPrice, err = convertStoreAmount(rule.FixedPrice, source, target, snapshot.Rate)
			if err != nil {
				return result, err
			}
		}
		if err := tx.Model(&rule).Updates(map[string]any{"currency": target.Code, "fixed_price": fixedPrice}).Error; err != nil {
			return result, err
		}
		result.ResellerRules++
	}

	var policies []model.ResellerCreditPolicy
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("currency = ?", source.Code).Find(&policies).Error; err != nil {
		return result, err
	}
	for _, policy := range policies {
		limit, err := convertStoreAmount(policy.CreditLimit, source, target, snapshot.Rate)
		if err != nil {
			return result, err
		}
		targetPolicy := model.ResellerCreditPolicy{ResellerID: policy.ResellerID, Currency: target.Code, CreditLimit: limit}
		created := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "reseller_id"}, {Name: "currency"}}, DoNothing: true}).Create(&targetPolicy)
		if created.Error != nil {
			return result, created.Error
		}
		if created.RowsAffected > 0 {
			result.ResellerCreditPolicies++
		}
	}

	var withdrawalMinimum model.Setting
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("key = ?", "affiliate_withdrawal_minimum").First(&withdrawalMinimum).Error; err == nil {
		value, scanErr := strconv.ParseInt(strings.TrimSpace(withdrawalMinimum.Value), 10, 64)
		if scanErr != nil {
			return result, scanErr
		}
		converted, convertErr := convertStoreAmount(value, source, target, snapshot.Rate)
		if convertErr != nil {
			return result, convertErr
		}
		if err := tx.Model(&withdrawalMinimum).Update("value", strconv.FormatInt(converted, 10)).Error; err != nil {
			return result, err
		}
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return result, err
	}

	return result, nil
}
