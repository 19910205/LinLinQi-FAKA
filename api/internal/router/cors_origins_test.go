package router

import "testing"

func TestResellerOriginDomainRequiresAnOriginNotAnArbitraryURL(t *testing.T) {
	for _, origin := range []string{
		"https://shop.example.com/path",
		"https://shop.example.com?redirect=https://evil.example",
		"https://user@shop.example.com",
		"javascript://shop.example.com",
		"http://shop.example.com",
		" https://shop.example.com",
	} {
		if _, ok := resellerOriginDomain(origin, true); ok {
			t.Errorf("invalid production origin accepted: %q", origin)
		}
	}
	domain, ok := resellerOriginDomain("https://SHOP.EXAMPLE.COM.:8443", true)
	if !ok || domain != "shop.example.com" {
		t.Fatalf("valid reseller origin was not normalized: domain=%q ok=%v", domain, ok)
	}
	if domain, ok = resellerOriginDomain("http://shop.example.com", false); !ok || domain != "shop.example.com" {
		t.Fatalf("development HTTP reseller origin was rejected: domain=%q ok=%v", domain, ok)
	}
}
