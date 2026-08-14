package supply

import (
	"strings"
	"testing"
)

func TestProtocolRegistryHasUniqueValidDefinitions(t *testing.T) {
	definitions := Protocols()
	if len(definitions) < 35 {
		t.Fatalf("expected complete protocol registry, got %d definitions", len(definitions))
	}
	seen := map[string]bool{}
	for _, definition := range definitions {
		if seen[definition.Code] || definition.Code == "" || definition.Name == "" || definition.Family == "" || definition.Availability == "" {
			t.Fatalf("invalid or duplicate protocol definition: %#v", definition)
		}
		seen[definition.Code] = true
		fieldKeys := map[string]bool{}
		for _, field := range definition.CredentialFields {
			if fieldKeys[field.Key] || !credentialKeyPattern.MatchString(field.Key) || field.MinLength < 0 || field.MaxLength < field.MinLength {
				t.Fatalf("invalid credential schema for %s: %#v", definition.Code, field)
			}
			fieldKeys[field.Key] = true
		}
	}
	if item, ok := Protocol("api-16"); !ok || item.Availability != "unavailable" {
		t.Fatal("API_16 must remain explicitly unavailable")
	}
}

func TestProtocolCredentialsFollowSelectedSchema(t *testing.T) {
	credentials, err := ValidateCredentials("cmsnt", map[string]string{"username": "operator", "password": "secret"})
	if err != nil || credentials["username"] != "operator" {
		t.Fatalf("valid CMSNT credentials rejected: %#v %v", credentials, err)
	}
	for name, values := range map[string]map[string]string{
		"missing":       {"username": "operator"},
		"foreign key":   {"username": "operator", "password": "secret", "api_key": "unexpected"},
		"control value": {"username": "operator", "password": "bad\nvalue"},
	} {
		if _, err := ValidateCredentials("cmsnt", values); err == nil {
			t.Fatalf("%s credentials unexpectedly accepted", name)
		}
	}
	if _, err := ValidateCredentials("api-16", map[string]string{}); err == nil {
		t.Fatal("unavailable protocol unexpectedly accepted")
	}
	if _, err := ValidateCredentials("vendor-dujiaoka", map[string]string{}); err == nil {
		t.Fatal("reference-only protocol unexpectedly accepted")
	}
}

func TestLegacyAccountIDCredentialsNormalizeToUsername(t *testing.T) {
	for _, testCase := range []struct {
		protocol    string
		credentials map[string]string
	}{
		{protocol: "api-13", credentials: map[string]string{"account_id": "legacy-user", "api_key": "key"}},
		{protocol: "api-14", credentials: map[string]string{"account_id": "legacy-user", "token": "token"}},
	} {
		normalized, err := ValidateCredentials(testCase.protocol, testCase.credentials)
		if err != nil {
			t.Fatalf("%s legacy credentials rejected: %v", testCase.protocol, err)
		}
		if normalized["username"] != "legacy-user" || normalized["account_id"] != "" {
			t.Fatalf("%s legacy account_id was not normalized: %#v", testCase.protocol, normalized)
		}
	}
	if _, err := ValidateCredentials("api-13", map[string]string{"username": "new-user", "account_id": "old-user", "api_key": "key"}); err == nil {
		t.Fatal("conflicting canonical and legacy credentials were accepted")
	}
	if _, err := ValidateCredentials("api-14", map[string]string{"username": "", "token": "token"}); err != nil {
		t.Fatalf("empty optional API14 username was rejected: %v", err)
	}
	if _, err := ValidateCredentials("dongvanfb", map[string]string{"username": "", "api_key": "key"}); err != nil {
		t.Fatalf("empty optional DONGVANFB username was rejected: %v", err)
	}
}

func TestRegistryAvailabilityMatchesGatewayRuntime(t *testing.T) {
	credentialValue := func(field CredentialField) string {
		length := field.MinLength
		if length < 1 {
			length = 1
		}
		return strings.Repeat("x", length)
	}
	for _, definition := range Protocols() {
		if definition.Availability != "supported" {
			if Executable(definition.Code) {
				t.Errorf("non-supported protocol %s is executable", definition.Code)
			}
			continue
		}
		if !RuntimeAvailable(definition.Code) || !Executable(definition.Code) {
			t.Errorf("supported protocol %s has no executable runtime", definition.Code)
			continue
		}
		credentials := make(map[string]string, len(definition.CredentialFields))
		for _, field := range definition.CredentialFields {
			if field.Required {
				credentials[field.Key] = credentialValue(field)
			}
		}
		gateway, err := NewGateway(definition.Code, "https://supplier.example.test", credentials, false)
		if err != nil {
			t.Errorf("supported protocol %s cannot construct runtime: %v", definition.Code, err)
			continue
		}
		advanced := []struct {
			capability  string
			implemented bool
		}{
			{capability: "product_detail", implemented: func() bool { _, ok := gateway.(ProductDetailReader); return ok }()},
			{capability: "stock", implemented: func() bool { _, ok := gateway.(StockReader); return ok }()},
			{capability: "order_cancel", implemented: func() bool { _, ok := gateway.(OrderCanceller); return ok }()},
			{capability: "valuation", implemented: func() bool { _, ok := gateway.(PriceQuoter); return ok }()},
			{capability: "draft", implemented: func() bool { _, ok := gateway.(DraftCardReader); return ok }()},
		}
		for _, operation := range advanced {
			advertised := containsCapability(definition.Capabilities, operation.capability)
			if advertised != operation.implemented {
				t.Errorf("protocol %s capability %s advertised=%v implemented=%v", definition.Code, operation.capability, advertised, operation.implemented)
			}
		}
	}
	for _, code := range []string{"cmslike-autofb", "otp-thuesim-1", "vendor-card-system"} {
		definition, exists := Protocol(code)
		if !exists || definition.Availability != "unavailable" || RuntimeAvailable(code) || Executable(code) {
			t.Errorf("protocol without runtime was advertised as executable: %#v", definition)
		}
	}
}
