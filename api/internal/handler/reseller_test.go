package handler

import "testing"

func TestNormalizeResellerDomain(t *testing.T) {
	for input, want := range map[string]string{
		"Shop.Example.COM.": "shop.example.com",
		"商城.中国":             "xn--czru2d.xn--fiqs8s",
	} {
		got, err := normalizeResellerDomain(input)
		if err != nil || got != want {
			t.Fatalf("normalize %q = %q, %v; want %q", input, got, err, want)
		}
	}
	for _, input := range []string{"localhost", "127.0.0.1", "example.local", "not-a-domain", "https://example.com"} {
		if _, err := normalizeResellerDomain(input); err == nil {
			t.Fatalf("unsafe domain %q was accepted", input)
		}
	}
}

func TestValidateOptionalPublicHTTPS(t *testing.T) {
	for _, value := range []string{"", "https://cdn.example.com/logo.svg", "https://support.example.com/help?lang=zh"} {
		if !validateOptionalPublicHTTPS(value) {
			t.Fatalf("valid HTTPS URL rejected: %s", value)
		}
	}
	for _, value := range []string{"http://example.com/logo.png", "https://user:pass@example.com/x", "javascript:alert(1)", "//example.com/x"} {
		if validateOptionalPublicHTTPS(value) {
			t.Fatalf("unsafe URL accepted: %s", value)
		}
	}
}
