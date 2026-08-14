package handler

import (
	"net/mail"
	"regexp"
	"strings"
	"unicode"
)

var checkoutContactEmailPattern = regexp.MustCompile(`^\S+@\S+\.\S+$`)

// normalizeCheckoutContact accepts a real email for backward compatibility,
// or a non-email contact handle such as a2456836 / 86256hfikg. Handles must
// be at least eight characters and must not be an obvious sequential
// placeholder. Numeric-only and alphabetic-only handles are supported.
func normalizeCheckoutContact(raw string) (string, bool) {
	value := strings.ToLower(strings.TrimSpace(raw))
	if value == "" || len([]rune(value)) < 8 || len([]rune(value)) > 190 {
		return "", false
	}
	for _, r := range value {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return "", false
		}
	}
	if checkoutContactEmailPattern.MatchString(value) {
		address, err := mail.ParseAddress(value)
		if err == nil && strings.EqualFold(address.Address, value) {
			return value, true
		}
	}
	compact := []rune(value)
	for i := 1; i < len(compact); i++ {
		if compact[i] != compact[i-1]+1 && compact[i] != compact[i-1]-1 {
			continue
		}
		run := 2
		for i+run < len(compact) && (compact[i+run] == compact[i+run-1]+1 || compact[i+run] == compact[i+run-1]-1) {
			run++
		}
		if run >= 6 {
			return "", false
		}
	}
	return value, true
}

func isCheckoutEmail(value string) bool {
	address, err := mail.ParseAddress(strings.TrimSpace(value))
	return err == nil && strings.EqualFold(address.Address, strings.TrimSpace(value))
}
