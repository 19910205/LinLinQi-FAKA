package handler

import (
	"testing"

	"linlinqi/api/internal/model"
)

func TestOpenAPIProductVariantBoundsUseStrictestProductAndVariantRules(t *testing.T) {
	tests := []struct {
		name                     string
		productMinimum           int
		productMaximum           int
		variantMaximum           int
		wantMinimum, wantMaximum int
	}{
		{name: "defaults", wantMinimum: 1, wantMaximum: 0},
		{name: "product bounds", productMinimum: 2, productMaximum: 9, wantMinimum: 2, wantMaximum: 9},
		{name: "variant is stricter", productMinimum: 2, productMaximum: 9, variantMaximum: 5, wantMinimum: 2, wantMaximum: 5},
		{name: "product is stricter", productMinimum: 2, productMaximum: 4, variantMaximum: 8, wantMinimum: 2, wantMaximum: 4},
		{name: "variant bounds unlimited product", productMinimum: 3, variantMaximum: 7, wantMinimum: 3, wantMaximum: 7},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			minimum, maximum := openAPIProductVariantBounds(
				model.Product{MinimumPurchase: test.productMinimum, MaximumPurchase: test.productMaximum},
				model.ProductVariant{PurchaseLimit: test.variantMaximum},
			)
			if minimum != test.wantMinimum || maximum != test.wantMaximum {
				t.Fatalf("bounds = (%d, %d), want (%d, %d)", minimum, maximum, test.wantMinimum, test.wantMaximum)
			}
		})
	}
}
