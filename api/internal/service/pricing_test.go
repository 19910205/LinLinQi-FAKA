package service

import "testing"

func TestQuotePriceComposesDiscounts(t *testing.T) {
	quote, err := QuotePrice(PricingInput{BaseUnitPrice: 10000, TierUnitPrice: 9000, Quantity: 2, MemberDiscountBasisPoint: 500, PromotionDiscount: 500, CouponDiscount: 300})
	if err != nil {
		t.Fatal(err)
	}
	if quote.Subtotal != 18000 || quote.Discount != 1700 || quote.Total != 16300 {
		t.Fatalf("unexpected quote: %#v", quote)
	}
}

func TestEvaluateCheckoutRisk(t *testing.T) {
	result := EvaluateCheckoutRisk(CheckoutSignals{OrdersFromIP10Minutes: 20, ProxyDetected: true, DisposableEmail: true})
	if result.Decision != "deny" || result.Score != 80 {
		t.Fatalf("unexpected risk result: %#v", result)
	}
}
