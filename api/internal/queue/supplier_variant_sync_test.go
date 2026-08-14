package queue

import (
	"testing"

	"linlinqi/api/internal/supply"
)

func TestSupplierVariantProductNormalizesIdentityAndInheritsParent(t *testing.T) {
	parent := supply.Product{
		ID: "parent-id", ExternalID: "parent", Name: "Premium",
		Currency: "USD", Price: 100, Stock: 10, Minimum: 1, Maximum: 5, Status: "active",
	}
	variant := supply.ProductVariant{
		ID: "variant-id", ExternalID: "variant-1", ExternalSKU: "SKU-1",
		Name: "Large", Price: 150, Stock: 3, Maximum: 4, Status: "inactive",
	}
	item, err := supplierVariantProduct(parent, variant)
	if err != nil {
		t.Fatalf("supplierVariantProduct: %v", err)
	}
	if item.ID != variant.ID || item.ExternalID != variant.ExternalID || item.ParentExternalID != parent.ExternalID || item.ExternalSKU != variant.ExternalSKU {
		t.Fatalf("variant identity was not preserved: %#v", item)
	}
	if item.Name != "Premium / Large" || item.Price != 150 || item.Stock != 3 || item.Maximum != 4 || item.Minimum != 1 || item.Status != "inactive" {
		t.Fatalf("variant presentation/price/stock were not applied: %#v", item)
	}
	if item.Variants != nil {
		t.Fatalf("variant product must not inherit parent variant list")
	}
}

func TestSupplierVariantProductFallsBackToVariantID(t *testing.T) {
	item, err := supplierVariantProduct(supply.Product{ExternalID: "parent", Name: "P"}, supply.ProductVariant{ID: "v-2", Name: "Small", Price: 120})
	if err != nil {
		t.Fatalf("supplierVariantProduct: %v", err)
	}
	if item.ExternalID != "v-2" || item.ParentExternalID != "parent" {
		t.Fatalf("variant ID fallback failed: %#v", item)
	}
}

func TestSupplierVariantProductRejectsEmptyIdentity(t *testing.T) {
	if _, err := supplierVariantProduct(supply.Product{ExternalID: "parent", Name: "P"}, supply.ProductVariant{Name: "Broken"}); err == nil {
		t.Fatalf("empty variant identity must be rejected")
	}
}
