package queue

import (
	"strings"
	"testing"

	"linlinqi/api/internal/supply"
)

func TestNormalizeSupplierSnapshotIdentitiesPreservesReferences(t *testing.T) {
	categories := []supply.Category{
		{ExternalID: " 分类-根 ", Name: "Root"},
		{ExternalID: "分类-子", ExternalParentID: " 分类-根 ", Name: "Child"},
	}
	products := []supply.Product{{
		ExternalID: " 商品-甲 ", ExternalCategoryID: " 分类-子 ", Name: "Product",
		Variants: []supply.ProductVariant{{ID: " 规格-大 ", Name: "Large"}},
	}}
	if err := normalizeSupplierSnapshotIdentities(categories, products); err != nil {
		t.Fatalf("valid Unicode snapshot identities rejected: %v", err)
	}
	if categories[0].ExternalID != "分类-根" || categories[1].ExternalParentID != categories[0].ExternalID {
		t.Fatalf("category reference changed inconsistently: %#v", categories)
	}
	if products[0].ExternalID != "商品-甲" || products[0].ExternalCategoryID != categories[1].ExternalID {
		t.Fatalf("product/category reference changed inconsistently: %#v", products[0])
	}
	if products[0].Variants[0].ExternalID != "规格-大" {
		t.Fatalf("variant fallback identity was not canonicalized: %#v", products[0].Variants[0])
	}
}

func TestNormalizeSupplierSnapshotIdentitiesRejectsInvalidReferences(t *testing.T) {
	for name, testCase := range map[string]struct {
		categories []supply.Category
		products   []supply.Product
	}{
		"category parent traversal": {
			categories: []supply.Category{{ExternalID: "category", ExternalParentID: "../parent", Name: "Category"}},
		},
		"product category control": {
			products: []supply.Product{{ExternalID: "product", ExternalCategoryID: "category\nother", Name: "Product"}},
		},
		"product parent self reference": {
			products: []supply.Product{{ExternalID: "product", ParentExternalID: "product", Name: "Product"}},
		},
		"variant traversal": {
			products: []supply.Product{{ExternalID: "product", Name: "Product", Variants: []supply.ProductVariant{{ExternalID: "variants/../secret"}}}},
		},
	} {
		t.Run(name, func(t *testing.T) {
			if err := normalizeSupplierSnapshotIdentities(testCase.categories, testCase.products); err == nil {
				t.Fatal("unsafe supplier snapshot identity was accepted")
			}
		})
	}
}

func TestNormalizeSupplierSnapshotIdentitiesRejectsDuplicatesAndCollisionPrefixes(t *testing.T) {
	if err := normalizeSupplierSnapshotIdentities(nil, []supply.Product{
		{ExternalID: "product", Name: "First"},
		{ExternalID: " product ", Name: "Second"},
	}); err == nil {
		t.Fatal("product identifiers that canonicalize to the same value were accepted")
	}
	if err := normalizeSupplierSnapshotIdentities(nil, []supply.Product{{
		ExternalID: "product", Name: "Product",
		Variants: []supply.ProductVariant{{ExternalID: "variant"}, {ExternalID: " variant "}},
	}}); err == nil {
		t.Fatal("duplicate variant identifiers were accepted")
	}
	prefix := strings.Repeat("x", supply.MaxExternalIDRunes)
	if err := normalizeSupplierSnapshotIdentities([]supply.Category{
		{ExternalID: prefix + "甲", Name: "First"},
		{ExternalID: prefix + "乙", Name: "Second"},
	}, nil); err == nil {
		t.Fatal("overlong category identifiers with colliding prefixes were truncated")
	}
}
