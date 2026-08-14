package service

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"linlinqi/api/internal/model"
)

func testCurrency(code string, minorUnit int) model.CurrencyDefinition {
	return model.CurrencyDefinition{Base: model.Base{ID: uuid.New()}, Code: code, MinorUnit: minorUnit, Enabled: true}
}

func TestConvertCheckoutQuoteUsesExactMinorUnitsAndKeepsTotalsBalanced(t *testing.T) {
	now := time.Now().UTC()
	snapshot := model.FXRateSnapshot{Base: model.Base{ID: uuid.New()}, BaseCode: "USD", QuoteCode: "CNY", Rate: "7.0267", SourceTier: "live", ExpiresAt: now.Add(time.Hour), StaleAfter: now.Add(24 * time.Hour)}
	conversion := CheckoutCurrencyConversion{Source: testCurrency("USD", 2), Target: testCurrency("CNY", 2), Snapshot: &snapshot}
	source := CheckoutQuote{
		Lines:    []CheckoutLineQuote{{ProductID: uuid.New(), Quote: PriceQuote{UnitPrice: 100, Quantity: 2, Subtotal: 200, Discount: 20, Total: 180, Adjustments: []PriceAdjustment{{Code: "member", Amount: -20}}}}},
		Subtotal: 200, Discount: 30, CouponDiscount: 10, Fee: 4, Total: 174, Currency: "USD", FeeRate: 250,
		Adjustments: []PriceAdjustment{{Code: "member", Amount: -20}, {Code: "coupon", Amount: -10}},
	}
	result, err := ConvertCheckoutQuote(source, conversion)
	if err != nil {
		t.Fatal(err)
	}
	if result.Currency != "CNY" || result.FX.SnapshotID == nil || *result.FX.SnapshotID != snapshot.ID {
		t.Fatalf("unexpected currency metadata: %#v", result.FX)
	}
	if result.Lines[0].Quote.UnitPrice != 703 || result.Subtotal != 1405 || result.Discount != 211 {
		t.Fatalf("unexpected exact conversion: %#v", result)
	}
	if result.Fee != 30 || result.Total != 1224 || result.Total != result.Subtotal-result.Discount+result.Fee {
		t.Fatalf("unbalanced converted quote: %#v", result)
	}
}

func TestCheckoutCurrencySameCurrencyDoesNotRequireSnapshot(t *testing.T) {
	currency := testCurrency("JPY", 0)
	conversion := CheckoutCurrencyConversion{Source: currency, Target: currency}
	amount, err := conversion.Amount(1234)
	if err != nil || amount != 1234 {
		t.Fatalf("same currency changed amount: %d, %v", amount, err)
	}
	if conversion.FX().Rate != "1" || conversion.FX().SnapshotID != nil {
		t.Fatalf("unexpected same-currency metadata: %#v", conversion.FX())
	}
}
