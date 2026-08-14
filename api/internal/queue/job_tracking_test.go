package queue

import (
	"strings"
	"testing"
)

func TestSafeJobPayloadRedactsNestedSecrets(t *testing.T) {
	payload := []byte(`{"delivery_id":"4b3788e1-d13f-4991-9dd6-3120acd54ac8","password":"unsafe","nested":{"api_secret":"unsafe","reference":"safe"}}`)
	result := safeJobPayload(payload)
	if strings.Contains(result, "unsafe") {
		t.Fatalf("job payload retained a secret: %s", result)
	}
	if !strings.Contains(result, "4b3788e1-d13f-4991-9dd6-3120acd54ac8") || !strings.Contains(result, `"reference":"safe"`) {
		t.Fatalf("job payload removed operational references: %s", result)
	}
}

func TestSafeJobPayloadRejectsOversizedValues(t *testing.T) {
	result := safeJobPayload([]byte(`{"value":"` + strings.Repeat("x", 17<<10) + `"}`))
	if result != `{}` {
		t.Fatalf("expected oversized payload to be discarded, got %d bytes", len(result))
	}
}

func TestSafeJobErrorRemovesControlCharacters(t *testing.T) {
	result := safeJobError(" upstream\x00failure\r\n")
	if strings.ContainsRune(result, '\x00') || !strings.Contains(result, "upstream") {
		t.Fatalf("unexpected safe error: %q", result)
	}
}

func TestTruncatePreservesValidUTF8(t *testing.T) {
	result := truncate("供货商错误信息", 5)
	if result != "供货商错误" {
		t.Fatalf("unexpected rune-safe truncation result: %q", result)
	}
}
