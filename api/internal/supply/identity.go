package supply

import (
	"errors"
	"net/url"
	"strings"
	"unicode"
	"unicode/utf8"
)

const MaxExternalIDRunes = 180

var ErrExternalIDInvalid = errors.New("supplier external identifier is invalid")

// NormalizeExternalID validates an opaque identifier at a supplier boundary.
// It intentionally permits printable Unicode and punctuation, while rejecting
// values that cannot be stored losslessly or could be interpreted as a path.
func NormalizeExternalID(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "" || !utf8.ValidString(value) || utf8.RuneCountInString(value) > MaxExternalIDRunes {
		return "", ErrExternalIDInvalid
	}
	for _, character := range value {
		if unicode.IsControl(character) || unicode.IsSpace(character) || unicode.Is(unicode.Cf, character) {
			return "", ErrExternalIDInvalid
		}
	}
	if externalIDHasUnsafePath(value) {
		return "", ErrExternalIDInvalid
	}
	return value, nil
}

// NormalizeOptionalExternalID applies the same canonical representation to an
// optional parent/category reference. Empty input remains empty.
func NormalizeOptionalExternalID(value string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", nil
	}
	return NormalizeExternalID(value)
}

func externalIDHasUnsafePath(value string) bool {
	// Treat both separators as path syntax and also inspect one decoded form so
	// encoded dot segments cannot bypass the boundary check.
	for attempt := 0; attempt < 4; attempt++ {
		normalized := strings.ReplaceAll(value, `\`, "/")
		if strings.HasPrefix(normalized, "/") {
			return true
		}
		for _, segment := range strings.Split(normalized, "/") {
			if segment == "." || segment == ".." {
				return true
			}
		}
		decoded, err := url.PathUnescape(value)
		if err != nil || decoded == value {
			break
		}
		value = decoded
	}
	return false
}
