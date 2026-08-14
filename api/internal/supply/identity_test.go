package supply

import (
	"strings"
	"testing"
)

func TestNormalizeExternalIDPreservesUnicodeOpaqueIdentity(t *testing.T) {
	want := "商品/套餐:甲-01✨"
	got, err := NormalizeExternalID("  " + want + "  ")
	if err != nil {
		t.Fatalf("valid Unicode identifier rejected: %v", err)
	}
	if got != want {
		t.Fatalf("identifier changed: got %q want %q", got, want)
	}
	maximum := strings.Repeat("界", MaxExternalIDRunes)
	if got, err := NormalizeExternalID(maximum); err != nil || got != maximum {
		t.Fatalf("maximum-length Unicode identifier was not preserved: len=%d err=%v", len([]rune(got)), err)
	}
}

func TestNormalizeExternalIDRejectsUnsafeOrLossyValues(t *testing.T) {
	for name, value := range map[string]string{
		"empty":             "  ",
		"embedded space":    "product one",
		"control":           "product\nname",
		"format control":    "product\u202ename",
		"literal traversal": "catalog/../secret",
		"encoded traversal": "catalog/%2e%2e/secret",
		"double encoded":    "catalog/%252e%252e/secret",
		"windows traversal": `catalog\..\secret`,
		"absolute path":     "/catalog/product",
		"too long":          strings.Repeat("界", MaxExternalIDRunes+1),
	} {
		t.Run(name, func(t *testing.T) {
			if got, err := NormalizeExternalID(value); err == nil {
				t.Fatalf("unsafe identifier accepted as %q", got)
			}
		})
	}
}

func TestNormalizeExternalIDRejectsCollisionPrefixesInsteadOfTruncating(t *testing.T) {
	prefix := strings.Repeat("a", MaxExternalIDRunes)
	for _, value := range []string{prefix + "甲", prefix + "乙"} {
		if got, err := NormalizeExternalID(value); err == nil {
			t.Fatalf("overlong collision candidate was truncated/accepted as %q", got)
		}
	}
}

func TestNormalizeOptionalExternalID(t *testing.T) {
	if got, err := NormalizeOptionalExternalID(" \t "); err != nil || got != "" {
		t.Fatalf("optional empty identifier: got %q err=%v", got, err)
	}
	if got, err := NormalizeOptionalExternalID(" 分类-一 "); err != nil || got != "分类-一" {
		t.Fatalf("optional Unicode identifier: got %q err=%v", got, err)
	}
}

func TestNormalizeProductUsesSameIdentityBoundaryForReferences(t *testing.T) {
	product, err := normalizeProduct(Product{
		ID: "ignored", ExternalID: " 商品-甲 ", ParentExternalID: " 商品-父 ",
		ExternalCategoryID: " 分类-一 ", Name: "Product", Minimum: 1, Status: "active",
	})
	if err != nil {
		t.Fatalf("Unicode gateway product rejected: %v", err)
	}
	if product.ExternalID != "商品-甲" || product.ParentExternalID != "商品-父" || product.ExternalCategoryID != "分类-一" {
		t.Fatalf("gateway product references were not canonicalized consistently: %#v", product)
	}
	if _, err := normalizeProduct(Product{ExternalID: "product", ExternalCategoryID: "../category", Name: "Product", Minimum: 1, Status: "active"}); err == nil {
		t.Fatal("unsafe gateway category reference was accepted")
	}
}
