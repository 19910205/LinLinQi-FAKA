package service

import (
	"testing"

	"linlinqi/api/internal/model"
)

func TestProductQuantityAllowedUsesExplicitUpstreamLimits(t *testing.T) {
	tests := []struct {
		name     string
		product  model.Product
		quantity int
		allowed  bool
	}{
		{name: "legacy zero minimum means one", product: model.Product{}, quantity: 1, allowed: true},
		{name: "below minimum", product: model.Product{MinimumPurchase: 3}, quantity: 2, allowed: false},
		{name: "at minimum", product: model.Product{MinimumPurchase: 3}, quantity: 3, allowed: true},
		{name: "unlimited maximum", product: model.Product{MinimumPurchase: 2}, quantity: 999, allowed: true},
		{name: "at maximum", product: model.Product{MinimumPurchase: 2, MaximumPurchase: 5}, quantity: 5, allowed: true},
		{name: "above maximum", product: model.Product{MinimumPurchase: 2, MaximumPurchase: 5}, quantity: 6, allowed: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := productQuantityAllowed(test.product, test.quantity); got != test.allowed {
				t.Fatalf("productQuantityAllowed() = %v, want %v", got, test.allowed)
			}
		})
	}
}
