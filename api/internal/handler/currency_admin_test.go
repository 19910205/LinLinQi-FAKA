package handler

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"linlinqi/api/internal/model"
)

func TestNormalizeExactFXDecimalRejectsNonDecimalAndNormalizesScale(t *testing.T) {
	for input, expected := range map[string]string{
		"7.026700000000000000": "7.0267",
		"1.000000000000000000": "1",
		"0.125":                "0.125",
		"99999999999999999999": "99999999999999999999",
	} {
		actual, err := normalizeExactFXDecimal(input)
		if err != nil || actual != expected {
			t.Fatalf("normalize %q: got=%q err=%v", input, actual, err)
		}
	}
	for _, input := range []string{
		"", "0", "-1", "+1", "1/2", "1e2", "01.5", ".5", "1.",
		"1.1234567890123456789", "100000000000000000000",
	} {
		if _, err := normalizeExactFXDecimal(input); err == nil {
			t.Fatalf("invalid exact decimal accepted: %q", input)
		}
	}
}

func TestNormalizeManualRateValidityUsesUTCAndBoundsWindow(t *testing.T) {
	now := time.Date(2026, time.August, 9, 8, 0, 0, 0, time.UTC)
	zone := time.FixedZone("CST", 8*60*60)
	from := time.Date(2026, time.August, 10, 8, 0, 0, 0, zone)
	to := from.Add(30 * 24 * time.Hour)
	normalizedFrom, normalizedTo, err := normalizeManualRateValidity(from, &to, now)
	if err != nil || normalizedFrom.Location() != time.UTC || normalizedTo == nil || normalizedTo.Location() != time.UTC {
		t.Fatalf("valid window rejected or not normalized: from=%s to=%v err=%v", normalizedFrom, normalizedTo, err)
	}
	invalidEnd := from
	if _, _, err := normalizeManualRateValidity(from, &invalidEnd, now); err == nil {
		t.Fatal("non-increasing manual-rate validity window was accepted")
	}
	tooFar := now.AddDate(1, 0, 1)
	if _, _, err := normalizeManualRateValidity(tooFar, nil, now); err == nil {
		t.Fatal("manual rate starting too far in the future was accepted")
	}
}

func TestManualRatePatchDistinguishesMissingAndNullValidTo(t *testing.T) {
	var missing adminFXManualRatePatchRequest
	if err := json.Unmarshal([]byte(`{"enabled":true,"reason":"planned activation"}`), &missing); err != nil {
		t.Fatalf("decode missing valid_to: %v", err)
	}
	if missing.ValidTo.Set {
		t.Fatal("missing valid_to was marked as present")
	}
	var cleared adminFXManualRatePatchRequest
	if err := json.Unmarshal([]byte(`{"valid_to":null,"reason":"approved indefinite fallback"}`), &cleared); err != nil {
		t.Fatalf("decode null valid_to: %v", err)
	}
	if !cleared.ValidTo.Set || cleared.ValidTo.Value != nil {
		t.Fatalf("null valid_to was not preserved: %#v", cleared.ValidTo)
	}
}

func TestAdminFXProviderDTONeverSerializesCredentialsOrRawError(t *testing.T) {
	item := model.FXProviderConfig{
		Code: "provider", Name: "Provider", Driver: "driver", BaseURL: "https://rates.example.com",
		CredentialCipher: []byte("cipher-secret"), CredentialNonce: []byte("nonce-secret"), LastError: "upstream-secret-error",
	}
	encoded, err := json.Marshal(toAdminFXProviderDTO(item))
	if err != nil {
		t.Fatalf("marshal safe provider DTO: %v", err)
	}
	serialized := string(encoded)
	for _, forbidden := range []string{"cipher-secret", "nonce-secret", "upstream-secret-error", "credential_cipher", "credential_nonce", "last_error"} {
		if strings.Contains(serialized, forbidden) {
			t.Fatalf("safe provider DTO leaked %q: %s", forbidden, serialized)
		}
	}
	if !strings.Contains(serialized, `"has_error":true`) {
		t.Fatalf("provider health flag is missing: %s", serialized)
	}
}

func TestCurrencyAdminRequestsRejectUnknownJSONAndUnsafeBounds(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest("PATCH", "/admin/v1/currencies/USD", strings.NewReader(`{"enabled":true,"server_owned":true}`))
	context.Request.Header.Set("Content-Type", "application/json")
	var request adminCurrencyPatchRequest
	if err := decodeStrictJSON(context, &request); err == nil {
		t.Fatal("unknown currency update field was accepted")
	}

	badMinorUnit := 7
	if err := (adminCurrencyPatchRequest{MinorUnit: &badMinorUnit}).validate(); err == nil {
		t.Fatal("minor unit above the supported exact-money range was accepted")
	}
	badTimeout := 31
	if err := (adminFXProviderPatchRequest{TimeoutSeconds: &badTimeout}).validate(); err == nil {
		t.Fatal("provider timeout above the operational bound was accepted")
	}
}
