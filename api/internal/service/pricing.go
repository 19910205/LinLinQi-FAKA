package service

import "fmt"

type PriceAdjustment struct {
	Code   string `json:"code"`
	Label  string `json:"label"`
	Amount int64  `json:"amount"`
}

type PriceQuote struct {
	UnitPrice   int64             `json:"unit_price"`
	Quantity    int               `json:"quantity"`
	Subtotal    int64             `json:"subtotal"`
	Discount    int64             `json:"discount"`
	Total       int64             `json:"total"`
	Adjustments []PriceAdjustment `json:"adjustments"`
}

type PricingInput struct {
	BaseUnitPrice            int64
	Quantity                 int
	TierUnitPrice            int64
	MemberDiscountBasisPoint int
	PromotionDiscount        int64
	CouponDiscount           int64
}

func QuotePrice(input PricingInput) (PriceQuote, error) {
	if input.BaseUnitPrice < 0 || input.TierUnitPrice < 0 || input.PromotionDiscount < 0 || input.CouponDiscount < 0 || input.MemberDiscountBasisPoint < 0 || input.MemberDiscountBasisPoint > 10000 || input.Quantity < 1 || input.Quantity > 1000 {
		return PriceQuote{}, fmt.Errorf("invalid price or quantity")
	}
	unit := input.BaseUnitPrice
	adjustments := make([]PriceAdjustment, 0, 4)
	if input.TierUnitPrice > 0 && input.TierUnitPrice < unit {
		tierSaving, err := checkedMultiplyInt64(unit-input.TierUnitPrice, int64(input.Quantity))
		if err != nil {
			return PriceQuote{}, err
		}
		adjustments = append(adjustments, PriceAdjustment{Code: "tier_price", Label: "批量价格", Amount: -tierSaving})
		unit = input.TierUnitPrice
	}
	subtotal, err := checkedMultiplyInt64(unit, int64(input.Quantity))
	if err != nil {
		return PriceQuote{}, err
	}
	discount := int64(0)
	if input.MemberDiscountBasisPoint > 0 {
		value, err := roundedRatio(subtotal, int64(input.MemberDiscountBasisPoint), 10000, false)
		if err != nil {
			return PriceQuote{}, err
		}
		discount, err = checkedAddInt64(discount, value)
		if err != nil {
			return PriceQuote{}, err
		}
		adjustments = append(adjustments, PriceAdjustment{Code: "member", Label: "会员折扣", Amount: -value})
	}
	for _, item := range []PriceAdjustment{{Code: "promotion", Label: "促销优惠", Amount: -input.PromotionDiscount}, {Code: "coupon", Label: "优惠券", Amount: -input.CouponDiscount}} {
		if item.Amount < 0 {
			var err error
			discount, err = checkedAddInt64(discount, -item.Amount)
			if err != nil {
				return PriceQuote{}, err
			}
			adjustments = append(adjustments, item)
		}
	}
	if discount > subtotal {
		discount = subtotal
	}
	return PriceQuote{UnitPrice: unit, Quantity: input.Quantity, Subtotal: subtotal, Discount: discount, Total: subtotal - discount, Adjustments: adjustments}, nil
}
