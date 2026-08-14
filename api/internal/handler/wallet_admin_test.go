package handler

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestAdminWalletCustomerDTOExcludesCustomerOperations(t *testing.T) {
	for name, value := range map[string]any{
		"list":   adminWalletCustomerListItem{},
		"detail": adminWalletUserSummary{},
	} {
		payload, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		encoded := string(payload)
		for _, forbidden := range []string{"last_login_at", "updated_at", "order_count", "net_spend", "sessions", "login_events", "membership"} {
			if strings.Contains(encoded, forbidden) {
				t.Fatalf("wallet-only %s DTO leaked %q: %s", name, forbidden, encoded)
			}
		}
	}
}
