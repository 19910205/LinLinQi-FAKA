package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"linlinqi/api/internal/security"
)

func TestFixtureCatalogIsCompleteAndUsesBundledImages(t *testing.T) {
	if len(categories) < 5 {
		t.Fatalf("expected at least five categories, got %d", len(categories))
	}
	if len(products) < 8 {
		t.Fatalf("expected at least eight products, got %d", len(products))
	}

	categorySlugs := make(map[string]struct{}, len(categories))
	for _, category := range categories {
		if category.Slug == "" || category.Name == "" {
			t.Fatal("fixture category has no slug or name")
		}
		if _, duplicate := categorySlugs[category.Slug]; duplicate {
			t.Fatalf("duplicate category slug %s", category.Slug)
		}
		categorySlugs[category.Slug] = struct{}{}
		assertBundledImage(t, category.ImageURL)
	}

	productSlugs := make(map[string]struct{}, len(products))
	skus := make(map[string]struct{}, len(products)*2)
	for _, product := range products {
		if _, exists := categorySlugs[product.CategorySlug]; !exists {
			t.Fatalf("product %s references missing category %s", product.Slug, product.CategorySlug)
		}
		if _, duplicate := productSlugs[product.Slug]; duplicate {
			t.Fatalf("duplicate product slug %s", product.Slug)
		}
		productSlugs[product.Slug] = struct{}{}
		assertBundledImage(t, product.CoverURL)
		if len(product.Variants) < 2 {
			t.Fatalf("product %s needs multiple variants", product.Slug)
		}
		for _, variant := range product.Variants {
			if _, duplicate := skus[variant.SKU]; duplicate {
				t.Fatalf("duplicate SKU %s", variant.SKU)
			}
			skus[variant.SKU] = struct{}{}
			if variant.Price < 0 || variant.CostPrice < 0 || variant.ComparePrice < variant.Price {
				t.Fatalf("invalid price contract for SKU %s", variant.SKU)
			}
		}
	}
}

func TestFixtureCardEncryptionIsBoundAndIdempotent(t *testing.T) {
	vault, err := security.NewVault("catalog-fixture-test-key-with-at-least-24-characters")
	if err != nil {
		t.Fatal(err)
	}
	productID := uuid.New()
	plaintext := fixtureCardSecret("linlinqi-product", "LQ-TEST-SKU", 1)
	firstCipher, firstNonce, firstFingerprint, err := vault.Encrypt(plaintext, productID[:])
	if err != nil {
		t.Fatal(err)
	}
	secondCipher, secondNonce, secondFingerprint, err := vault.Encrypt(plaintext, productID[:])
	if err != nil {
		t.Fatal(err)
	}
	if firstFingerprint != secondFingerprint {
		t.Fatal("the same product and plaintext must have a stable keyed fingerprint")
	}
	if bytes.Equal(firstNonce, secondNonce) || bytes.Equal(firstCipher, secondCipher) {
		t.Fatal("re-encryption must use a fresh nonce and ciphertext")
	}
	decrypted, err := vault.Decrypt(firstCipher, firstNonce, productID[:])
	if err != nil || decrypted != plaintext {
		t.Fatalf("decrypt fixture card: value=%q error=%v", decrypted, err)
	}
	otherProductID := uuid.New()
	if _, err := vault.Decrypt(firstCipher, firstNonce, otherProductID[:]); err == nil {
		t.Fatal("card ciphertext must not decrypt under another product association")
	}
	if !strings.Contains(plaintext, "NO-REAL-VALUE") {
		t.Fatal("fixture cards must be visibly marked as valueless test data")
	}
}

func TestSandboxIsForbiddenInProduction(t *testing.T) {
	for _, environment := range []string{"production", "PRODUCTION", " production "} {
		if shouldInstallSandbox(environment) {
			t.Fatalf("sandbox must be forbidden for environment %q", environment)
		}
	}
	if !shouldInstallSandbox("development") {
		t.Fatal("sandbox should be available in development")
	}
}

func assertBundledImage(t *testing.T, publicURL string) {
	t.Helper()
	if !strings.HasPrefix(publicURL, "/assets/brand/") || strings.Contains(publicURL, "..") {
		t.Fatalf("image URL is not a safe bundled brand asset: %s", publicURL)
	}
	path := filepath.Join("..", "..", "..", "user", "public", filepath.FromSlash(strings.TrimPrefix(publicURL, "/")))
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("bundled image %s is unavailable at %s: %v", publicURL, path, err)
	}
	if info.IsDir() || info.Size() == 0 {
		t.Fatalf("bundled image %s is empty or a directory", publicURL)
	}
}
