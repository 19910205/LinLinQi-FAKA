package handler

import "testing"

func TestNotificationEventCatalogIsUniqueAndComplete(t *testing.T) {
	want := []string{
		"openapi.call.succeeded", "openapi.call.failed", "supplier.sync.succeeded", "supplier.sync.failed",
		"procurement.created", "procurement.succeeded", "procurement.failed", "risk.blocked", "security.high_risk",
	}
	seen := make(map[string]notificationEventDefinition, len(notificationEventCatalog))
	for _, event := range notificationEventCatalog {
		if event.Code == "" || event.Group == "" || event.Name == "" || event.Severity == "" {
			t.Fatalf("incomplete notification event: %#v", event)
		}
		if _, exists := seen[event.Code]; exists {
			t.Fatalf("duplicate notification event code: %s", event.Code)
		}
		seen[event.Code] = event
		variables := make(map[string]bool, len(event.Variables))
		for _, variable := range event.Variables {
			variables[variable] = true
		}
		for _, variable := range notificationCommonVariables {
			if !variables[variable] {
				t.Fatalf("event %s is missing template variable %s", event.Code, variable)
			}
		}
	}
	for _, code := range want {
		if _, exists := seen[code]; !exists {
			t.Fatalf("required operational event is missing: %s", code)
		}
	}
}

func TestNotificationConnectorRejectsInvalidSMTPPortAndURLCredentials(t *testing.T) {
	enabled := true
	for _, request := range []notificationConnectorRequest{
		{Name: "邮件渠道", Channel: "email", Endpoint: "smtp.example.com:99999", Sender: "sender@example.com", Secret: "long-secret", Enabled: &enabled},
		{Name: "邮件渠道", Channel: "email", Endpoint: "smtp.example.com:not-a-port", Sender: "sender@example.com", Secret: "long-secret", Enabled: &enabled},
		{Name: "机器人渠道", Channel: "telegram", Endpoint: "https://token@notify.example.com", Secret: "long-secret", Enabled: &enabled},
	} {
		if err := request.normalizeAndValidate(true); err == nil {
			t.Fatalf("unsafe notification connector was accepted: %#v", request)
		}
	}
}
