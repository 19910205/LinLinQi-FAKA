package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"linlinqi/api/internal/model"
)

var (
	ErrVariantRequired    = errors.New("product variant is required")
	ErrVariantUnavailable = errors.New("product variant is unavailable")
	ErrCouponUnavailable  = errors.New("coupon is unavailable")
)

type ResolvedLine struct {
	Product           model.Product
	VariantID         *uuid.UUID
	VariantName       string
	PlatformUnitPrice int64
	ResellerMargin    int64
	Quote             PriceQuote
}

type CheckoutLineInput struct {
	ProductID uuid.UUID
	VariantID *uuid.UUID
	Quantity  int
}

type CheckoutLineQuote struct {
	ProductID   uuid.UUID  `json:"product_id"`
	VariantID   *uuid.UUID `json:"variant_id,omitempty"`
	ProductName string     `json:"product_name"`
	VariantName string     `json:"variant_name,omitempty"`
	Quote       PriceQuote `json:"quote"`
	Stock       int64      `json:"stock"`
	Available   bool       `json:"available"`
}

type CheckoutQuote struct {
	Lines          []CheckoutLineQuote `json:"lines"`
	Subtotal       int64               `json:"subtotal"`
	Discount       int64               `json:"discount"`
	CouponDiscount int64               `json:"coupon_discount"`
	Fee            int64               `json:"fee"`
	Total          int64               `json:"total"`
	Currency       string              `json:"currency"`
	FX             CheckoutFX          `json:"fx"`
	Adjustments    []PriceAdjustment   `json:"adjustments"`
	ResellerMargin int64               `json:"-"`
	FeeRate        int                 `json:"-"`
}

// ResolveCheckoutQuote is a non-mutating preview of the exact pricing rules
// used by order creation. It still runs in a short transaction so tier,
// membership, promotion and coupon reads form one consistent quote.
func ResolveCheckoutQuote(tx *gorm.DB, userID *uuid.UUID, email, couponCode string, feeRate int, lines []CheckoutLineInput) (CheckoutQuote, error) {
	return ResolveCheckoutQuoteForReseller(tx, userID, nil, email, couponCode, feeRate, lines)
}

// ResolveCheckoutQuoteForReseller keeps storefront pricing and order creation
// on the same server-authoritative path. Platform membership, campaign and
// coupon subsidies are intentionally excluded from reseller orders so they
// cannot reduce the amount collected below the platform wholesale price.
func ResolveCheckoutQuoteForReseller(tx *gorm.DB, userID, resellerID *uuid.UUID, email, couponCode string, feeRate int, lines []CheckoutLineInput) (CheckoutQuote, error) {
	if feeRate < 0 || feeRate > 10000 {
		return CheckoutQuote{}, fmt.Errorf("invalid payment fee")
	}
	if resellerID != nil && strings.TrimSpace(couponCode) != "" {
		return CheckoutQuote{}, ErrCouponUnavailable
	}
	aggregated := make(map[string]CheckoutLineInput)
	totalQuantity := 0
	for _, line := range lines {
		if line.ProductID == uuid.Nil || line.Quantity < 1 || line.Quantity > 20 {
			return CheckoutQuote{}, fmt.Errorf("invalid checkout line")
		}
		key := line.ProductID.String() + ":"
		if line.VariantID != nil {
			key += line.VariantID.String()
		}
		existing := aggregated[key]
		if existing.ProductID == uuid.Nil {
			existing.ProductID, existing.VariantID = line.ProductID, line.VariantID
		}
		existing.Quantity += line.Quantity
		if existing.Quantity > 20 {
			return CheckoutQuote{}, fmt.Errorf("checkout line quantity exceeds limit")
		}
		aggregated[key] = existing
		totalQuantity += line.Quantity
	}
	if len(aggregated) == 0 || totalQuantity > 20 {
		return CheckoutQuote{}, fmt.Errorf("checkout must contain between 1 and 20 items")
	}
	keys := make([]string, 0, len(aggregated))
	for key := range aggregated {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := CheckoutQuote{Lines: make([]CheckoutLineQuote, 0, len(keys)), Adjustments: make([]PriceAdjustment, 0), FeeRate: feeRate}
	for _, key := range keys {
		requestLine := aggregated[key]
		resolved, err := ResolveLinePricingForReseller(tx, requestLine.ProductID, requestLine.VariantID, userID, resellerID, requestLine.Quantity)
		if err != nil {
			return CheckoutQuote{}, err
		}
		lineCurrency, err := normalizedProductCurrency(resolved.Product)
		if err != nil {
			return CheckoutQuote{}, err
		}
		if result.Currency == "" {
			result.Currency = lineCurrency
		} else if result.Currency != lineCurrency {
			return CheckoutQuote{}, ErrCurrencyMismatch
		}
		result.Lines = append(result.Lines, CheckoutLineQuote{ProductID: resolved.Product.ID, VariantID: resolved.VariantID, ProductName: resolved.Product.Name, VariantName: resolved.VariantName, Quote: resolved.Quote})
		result.Subtotal, err = checkedAddInt64(result.Subtotal, resolved.Quote.Subtotal)
		if err != nil {
			return CheckoutQuote{}, err
		}
		result.Discount, err = checkedAddInt64(result.Discount, resolved.Quote.Discount)
		if err != nil {
			return CheckoutQuote{}, err
		}
		result.ResellerMargin, err = checkedAddInt64(result.ResellerMargin, resolved.ResellerMargin)
		if err != nil {
			return CheckoutQuote{}, err
		}
		for _, adjustment := range resolved.Quote.Adjustments {
			adjustment.Label = resolved.Product.Name + " · " + adjustment.Label
			result.Adjustments = append(result.Adjustments, adjustment)
		}
	}
	coupon, err := ResolveCoupon(tx, couponCode, userID, email, result.Currency, result.Subtotal-result.Discount)
	if err != nil {
		return CheckoutQuote{}, err
	}
	result.CouponDiscount = coupon.Discount
	result.Discount, err = checkedAddInt64(result.Discount, coupon.Discount)
	if err != nil {
		return CheckoutQuote{}, err
	}
	if coupon.Discount > 0 {
		result.Adjustments = append(result.Adjustments, PriceAdjustment{Code: "coupon", Label: "优惠券", Amount: -coupon.Discount})
	}
	netAmount := result.Subtotal - result.Discount
	if netAmount < 1 {
		return CheckoutQuote{}, ErrCouponUnavailable
	}
	result.Fee, err = roundedNearestRatio(netAmount, int64(feeRate), 10000)
	if err != nil {
		return CheckoutQuote{}, err
	}
	result.Total, err = checkedAddInt64(netAmount, result.Fee)
	if err != nil {
		return CheckoutQuote{}, err
	}
	return result, nil
}

type promotionRule struct {
	BasisPoints int   `json:"basis_points"`
	Amount      int64 `json:"amount"`
	MinAmount   int64 `json:"min_amount"`
	MaxDiscount int64 `json:"max_discount"`
	UnitPrice   int64 `json:"unit_price"`
}

// ResolveLinePricing computes a server-authoritative quote while the caller's
// transaction is active. Order creation persists this exact quote; prices are
// never accepted from a browser or OpenAPI client.
func ResolveLinePricing(tx *gorm.DB, productID uuid.UUID, variantID *uuid.UUID, userID *uuid.UUID, quantity int) (ResolvedLine, error) {
	return ResolveLinePricingForReseller(tx, productID, variantID, userID, nil, quantity)
}

func productQuantityAllowed(product model.Product, quantity int) bool {
	minimum := product.MinimumPurchase
	if minimum < 1 {
		minimum = 1
	}
	return quantity >= minimum && (product.MaximumPurchase <= 0 || quantity <= product.MaximumPurchase)
}

func ResolveLinePricingForReseller(tx *gorm.DB, productID uuid.UUID, variantID *uuid.UUID, userID, resellerID *uuid.UUID, quantity int) (ResolvedLine, error) {
	if quantity < 1 || quantity > 1000 {
		return ResolvedLine{}, fmt.Errorf("invalid quantity")
	}
	var product model.Product
	if err := tx.First(&product, "id = ?", productID).Error; err != nil {
		return ResolvedLine{}, err
	}
	if product.Status != "on_sale" || product.Price < 0 {
		return ResolvedLine{}, ErrProductUnavailable
	}
	if !productQuantityAllowed(product, quantity) {
		return ResolvedLine{}, ErrProductUnavailable
	}
	basePrice := product.Price
	variantName := ""
	if variantID != nil {
		var variant model.ProductVariant
		if err := tx.Where("id = ? AND product_id = ? AND status = ?", *variantID, product.ID, "active").First(&variant).Error; err != nil {
			return ResolvedLine{}, ErrVariantUnavailable
		}
		if variant.Price < 0 || (variant.PurchaseLimit > 0 && quantity > variant.PurchaseLimit) {
			return ResolvedLine{}, ErrVariantUnavailable
		}
		basePrice, variantName = variant.Price, variant.Name
	} else {
		var variants int64
		if err := tx.Model(&model.ProductVariant{}).Where("product_id = ? AND status = ?", product.ID, "active").Count(&variants).Error; err != nil {
			return ResolvedLine{}, err
		}
		if variants > 0 {
			return ResolvedLine{}, ErrVariantRequired
		}
	}
	if resellerID != nil {
		salePrice, enabled, err := ResolveResellerSalePrice(tx, *resellerID, product.ID, variantID, basePrice)
		if err != nil {
			return ResolvedLine{}, err
		}
		if !enabled {
			return ResolvedLine{}, ErrResellerProductUnavailable
		}
		policy, err := EffectiveResellerWholesalePolicy(tx, *resellerID)
		if err != nil {
			return ResolvedLine{}, err
		}
		settlementPrice, err := WholesaleSettlementPrice(basePrice, policy.DiscountBasisPoint)
		if err != nil || settlementPrice > salePrice {
			return ResolvedLine{}, ErrResellerProductUnavailable
		}
		quote, err := QuotePrice(PricingInput{BaseUnitPrice: salePrice, Quantity: quantity})
		if err != nil {
			return ResolvedLine{}, err
		}
		marginPerUnit := salePrice - settlementPrice
		margin, err := checkedMultiplyInt64(marginPerUnit, int64(quantity))
		if err != nil {
			return ResolvedLine{}, ErrResellerProductUnavailable
		}
		return ResolvedLine{Product: product, VariantID: variantID, VariantName: variantName, PlatformUnitPrice: basePrice, ResellerMargin: margin, Quote: quote}, nil
	}

	memberDiscount := 0
	var memberLevelID *uuid.UUID
	if userID != nil {
		membership, level, err := EffectiveUserMembershipTx(tx, *userID, time.Now())
		if err != nil {
			return ResolvedLine{}, err
		}
		if membership != nil && level != nil && strings.EqualFold(level.Currency, product.Currency) {
			memberLevelID = &membership.MemberLevelID
			memberDiscount = max(0, min(level.DiscountBasisPoint, 10000))
		}
	}

	now := time.Now()
	query := tx.Model(&model.ProductPriceTier{}).
		Where("product_id = ? AND min_quantity <= ?", product.ID, quantity).
		Where("(starts_at IS NULL OR starts_at <= ?) AND (ends_at IS NULL OR ends_at >= ?)", now, now)
	if variantID == nil {
		query = query.Where("variant_id IS NULL")
	} else {
		query = query.Where("variant_id IS NULL OR variant_id = ?", *variantID)
	}
	if memberLevelID == nil {
		query = query.Where("member_level_id IS NULL")
	} else {
		query = query.Where("member_level_id IS NULL OR member_level_id = ?", *memberLevelID)
	}
	var tier model.ProductPriceTier
	tierPrice := int64(0)
	if err := query.Order("(variant_id IS NOT NULL) DESC, (member_level_id IS NOT NULL) DESC, min_quantity DESC").First(&tier).Error; err == nil {
		if tier.UnitPrice >= 0 && tier.UnitPrice < basePrice {
			tierPrice = tier.UnitPrice
		}
	} else if err != gorm.ErrRecordNotFound {
		return ResolvedLine{}, err
	}

	priceForPromotion := basePrice
	if tierPrice > 0 {
		priceForPromotion = tierPrice
	}
	promotionSubtotal, err := checkedMultiplyInt64(priceForPromotion, int64(quantity))
	if err != nil {
		return ResolvedLine{}, err
	}
	promotionDiscount, promotionAdjustments, err := resolvePromotions(tx, product.ID, product.Currency, promotionSubtotal, quantity, priceForPromotion)
	if err != nil {
		return ResolvedLine{}, err
	}
	quote, err := QuotePrice(PricingInput{BaseUnitPrice: basePrice, Quantity: quantity, TierUnitPrice: tierPrice, MemberDiscountBasisPoint: memberDiscount, PromotionDiscount: promotionDiscount})
	if err != nil {
		return ResolvedLine{}, err
	}
	if len(promotionAdjustments) > 0 {
		filtered := quote.Adjustments[:0]
		for _, adjustment := range quote.Adjustments {
			if adjustment.Code != "promotion" {
				filtered = append(filtered, adjustment)
			}
		}
		quote.Adjustments = append(filtered, promotionAdjustments...)
	}
	return ResolvedLine{Product: product, VariantID: variantID, VariantName: variantName, PlatformUnitPrice: basePrice, Quote: quote}, nil
}

func resolvePromotions(tx *gorm.DB, productID uuid.UUID, currencyCode string, subtotal int64, quantity int, unitPrice int64) (int64, []PriceAdjustment, error) {
	now := time.Now()
	var promotions []model.Promotion
	err := tx.Model(&model.Promotion{}).
		Joins("JOIN promotion_products pp ON pp.promotion_id = promotions.id AND pp.product_id = ?", productID).
		Where("promotions.status = ? AND promotions.currency = ? AND promotions.starts_at <= ? AND promotions.ends_at >= ?", "active", currencyCode, now, now).
		Order("promotions.priority DESC, promotions.created_at ASC").Find(&promotions).Error
	if err != nil {
		return 0, nil, err
	}
	total := int64(0)
	adjustments := make([]PriceAdjustment, 0, len(promotions))
	stacking := true
	for _, promotion := range promotions {
		if !stacking {
			break
		}
		var rule promotionRule
		if json.Unmarshal([]byte(promotion.Rules), &rule) != nil || subtotal < rule.MinAmount {
			continue
		}
		discount := int64(0)
		switch strings.ToLower(promotion.Type) {
		case "percentage", "percent":
			if rule.BasisPoints > 0 && rule.BasisPoints <= 10000 {
				discount, err = roundedRatio(subtotal, int64(rule.BasisPoints), 10000, false)
				if err != nil {
					return 0, nil, err
				}
			}
		case "fixed", "threshold_fixed":
			if rule.Amount > 0 {
				discount = rule.Amount
			}
		case "flash_price":
			if rule.UnitPrice >= 0 && rule.UnitPrice < unitPrice {
				discount, err = checkedMultiplyInt64(unitPrice-rule.UnitPrice, int64(quantity))
				if err != nil {
					return 0, nil, err
				}
			}
		}
		if rule.MaxDiscount > 0 && discount > rule.MaxDiscount {
			discount = rule.MaxDiscount
		}
		if discount <= 0 {
			continue
		}
		if discount > subtotal-total {
			discount = subtotal - total
		}
		total, err = checkedAddInt64(total, discount)
		if err != nil {
			return 0, nil, err
		}
		adjustments = append(adjustments, PriceAdjustment{Code: "promotion:" + promotion.Code, Label: promotion.Name, Amount: -discount})
		if !promotion.Stackable {
			stacking = false
		}
	}
	return total, adjustments, nil
}

type ResolvedCoupon struct {
	Coupon      *model.Coupon
	Discount    int64
	RedeemerKey string
}

func ResolveCoupon(tx *gorm.DB, code string, userID *uuid.UUID, email, currencyCode string, amount int64) (ResolvedCoupon, error) {
	code = strings.ToUpper(strings.TrimSpace(code))
	if code == "" {
		return ResolvedCoupon{}, nil
	}
	var coupon model.Coupon
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("UPPER(code) = ?", code).First(&coupon).Error; err != nil {
		return ResolvedCoupon{}, ErrCouponUnavailable
	}
	now := time.Now()
	if coupon.Currency != strings.ToUpper(strings.TrimSpace(currencyCode)) || !coupon.Enabled || (!coupon.StartsAt.IsZero() && now.Before(coupon.StartsAt)) || (!coupon.EndsAt.IsZero() && now.After(coupon.EndsAt)) || amount < coupon.MinAmount || (coupon.UsageLimit > 0 && coupon.UsedCount >= coupon.UsageLimit) {
		return ResolvedCoupon{}, ErrCouponUnavailable
	}
	redeemer := strings.ToLower(strings.TrimSpace(email))
	if userID != nil {
		redeemer = userID.String()
	}
	digest := sha256.Sum256([]byte("coupon-redeemer:" + redeemer))
	redeemerKey := hex.EncodeToString(digest[:])
	var used int64
	if err := tx.Model(&model.CouponRedemption{}).Where("coupon_id = ? AND redeemer_key = ? AND status IN ?", coupon.ID, redeemerKey, []string{"reserved", "consumed"}).Count(&used).Error; err != nil {
		return ResolvedCoupon{}, err
	}
	if used > 0 {
		return ResolvedCoupon{}, ErrCouponUnavailable
	}
	discount := int64(0)
	switch strings.ToLower(coupon.Type) {
	case "fixed":
		discount = coupon.Value
	case "percentage", "percent":
		if coupon.Value > 0 && coupon.Value <= 10000 {
			var err error
			discount, err = roundedRatio(amount, coupon.Value, 10000, false)
			if err != nil {
				return ResolvedCoupon{}, ErrCouponUnavailable
			}
		}
	}
	if discount <= 0 {
		return ResolvedCoupon{}, ErrCouponUnavailable
	}
	if discount >= amount {
		discount = amount - 1
	}
	if discount <= 0 {
		return ResolvedCoupon{}, ErrCouponUnavailable
	}
	return ResolvedCoupon{Coupon: &coupon, Discount: discount, RedeemerKey: redeemerKey}, nil
}

func ConsumeCoupon(tx *gorm.DB, resolved ResolvedCoupon, orderID uuid.UUID, userID *uuid.UUID, status string) error {
	if resolved.Coupon == nil {
		return nil
	}
	if status != "reserved" && status != "consumed" {
		return fmt.Errorf("invalid coupon redemption status")
	}
	now := time.Now()
	redemption := model.CouponRedemption{CouponID: resolved.Coupon.ID, OrderID: orderID, UserID: userID, RedeemerKey: resolved.RedeemerKey, Discount: resolved.Discount, Status: status}
	if status == "consumed" {
		redemption.RedeemedAt = &now
	}
	if err := tx.Create(&redemption).Error; err != nil {
		return ErrCouponUnavailable
	}
	result := tx.Model(&model.Coupon{}).Where("id = ? AND (usage_limit = 0 OR used_count < usage_limit)", resolved.Coupon.ID).UpdateColumn("used_count", gorm.Expr("used_count + 1"))
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrCouponUnavailable
	}
	return nil
}

// CompleteCouponReservation converts an unpaid-order reservation into an
// immutable use. It shares the order transaction so payment and coupon state
// cannot diverge after a crash.
func CompleteCouponReservation(tx *gorm.DB, orderID uuid.UUID) error {
	now := time.Now()
	return tx.Model(&model.CouponRedemption{}).
		Where("order_id = ? AND status = ?", orderID, "reserved").
		Updates(map[string]any{"status": "consumed", "redeemed_at": &now}).Error
}

// ReleaseCouponReservation returns capacity for an unpaid cancelled/expired
// order. A row lock and status predicate make repeated expiry jobs idempotent.
func ReleaseCouponReservation(tx *gorm.DB, orderID uuid.UUID) error {
	var redemption model.CouponRedemption
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("order_id = ? AND status = ?", orderID, "reserved").First(&redemption).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	now := time.Now()
	result := tx.Model(&model.CouponRedemption{}).Where("id = ? AND status = ?", redemption.ID, "reserved").Updates(map[string]any{"status": "released", "released_at": &now})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return nil
	}
	return tx.Model(&model.Coupon{}).Where("id = ?", redemption.CouponID).UpdateColumn("used_count", gorm.Expr("GREATEST(used_count - 1, 0)")).Error
}
