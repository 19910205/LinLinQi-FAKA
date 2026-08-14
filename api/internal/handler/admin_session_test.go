package handler

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func TestAdminSessionProfileNeverSerializesAuthenticationState(t *testing.T) {
	profile := adminSessionProfile{
		ID: uuid.New(), Username: "catalog.operator", Name: "Catalog Operator",
		Role: "catalog", Permissions: []string{"catalog.manage", "catalog.view"},
	}
	payload, err := json.Marshal(profile)
	if err != nil {
		t.Fatalf("marshal admin session profile: %v", err)
	}
	body := string(payload)
	for _, forbidden := range []string{"password", "session_version", "secret", "totp", "recovery"} {
		if strings.Contains(strings.ToLower(body), forbidden) {
			t.Fatalf("admin session profile leaked %q: %s", forbidden, body)
		}
	}
	for _, expected := range []string{`"username":"catalog.operator"`, `"permissions":["catalog.manage","catalog.view"]`} {
		if !strings.Contains(body, expected) {
			t.Fatalf("admin session profile omitted %s: %s", expected, body)
		}
	}
}
