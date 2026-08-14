package handler

import "testing"

func TestNormalizeAdminSettingsAcceptsOperationalRangesAndClearingOptionalBranding(t *testing.T) {
	values, groups, err := normalizeAdminSettings(map[string]string{
		"store_name":                     " LinLinQi 企业店 ",
		"store_logo_url":                 "",
		"store_support_email":            "ops@example.com",
		"order_timeout_minutes":          "30",
		"inventory_warning_threshold":    "20",
		"affiliate_default_basis_points": "500",
		"affiliate_hold_days":            "7",
		"affiliate_withdrawal_minimum":   "10000",
		"store_currency":                 "USD",
	})
	if err != nil {
		t.Fatalf("valid settings rejected: %v", err)
	}
	if values["store_name"] != "LinLinQi 企业店" || values["store_logo_url"] != "" {
		t.Fatalf("settings were not normalized: %#v", values)
	}
	if groups["order_timeout_minutes"] != "order" || groups["affiliate_hold_days"] != "affiliate" {
		t.Fatalf("settings were assigned to incorrect groups: %#v", groups)
	}
}

func TestNormalizeAdminSettingsAcceptsUploadedLogoMediaURLs(t *testing.T) {
	// The native local deployment serves uploaded media over loopback HTTP,
	// and a deployed installation may use an application-owned relative path.
	for _, logo := range []string{
		"/media/sha256/abc/def.jpg",
		"http://127.0.0.1:8080/media/sha256/abc/def.jpg",
		"http://localhost:8080/media/sha256/abc/def.jpg",
		"https://cdn.example.com/logo.svg",
	} {
		values, _, err := normalizeAdminSettings(map[string]string{"store_logo_url": logo})
		if err != nil || values["store_logo_url"] != logo {
			t.Fatalf("uploaded logo media URL rejected: input=%q values=%#v err=%v", logo, values, err)
		}
	}
}

func TestNormalizeAdminSettingsRejectsUnknownOrUnsafeValues(t *testing.T) {
	tests := []map[string]string{
		{"arbitrary_secret": "value"},
		{"store_logo_url": "javascript:alert(1)"},
		{"store_logo_url": "http://cdn.example.com/logo.png"},
		{"store_logo_url": "//evil.example.com/logo.png"},
		{"store_logo_url": "/admin/../../etc/passwd"},
		{"store_logo_url": "http://10.0.0.3/media/logo.png"},
		{"store_support_email": "Display Name <ops@example.com>"},
		{"order_timeout_minutes": "4"},
		{"inventory_warning_threshold": "0"},
		{"affiliate_default_basis_points": "3001"},
		{"affiliate_hold_days": "91"},
		{"affiliate_withdrawal_minimum": "0"},
		{"store_currency": "usd"},
		{"store_currency": "USDT"},
	}
	for _, values := range tests {
		if _, _, err := normalizeAdminSettings(values); err == nil {
			t.Fatalf("unsafe settings accepted: %#v", values)
		}
	}
}
