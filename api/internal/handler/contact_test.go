package handler

import "testing"

func TestNormalizeCheckoutContactExamples(t *testing.T) {
	valid := []string{"a2456836", "86256hfikg", "86256321", "hfikghij", "buyer@example.com"}
	for _, value := range valid {
		if normalized, ok := normalizeCheckoutContact(value); !ok || normalized == "" {
			t.Fatalf("expected %q to be accepted, got %q/%v", value, normalized, ok)
		}
	}
	invalid := []string{"a123456", "abcdefg", "12345678", "abc123456", "a123456"}
	for _, value := range invalid {
		if normalized, ok := normalizeCheckoutContact(value); ok {
			t.Fatalf("expected %q to be rejected, got %q", value, normalized)
		}
	}
}
