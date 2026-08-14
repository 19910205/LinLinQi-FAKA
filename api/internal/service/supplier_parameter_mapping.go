package service

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

const SupplierParameterMappingLimit = 20

var (
	ErrSupplierParameterMappingInvalid = errors.New("supplier parameter mapping is invalid")
	localSupplierParameterKeyPattern   = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
)

func validRemoteSupplierParameterKey(value string) bool {
	if value == "" || utf8.RuneCountInString(value) > 64 {
		return false
	}
	for index, character := range []rune(value) {
		if index == 0 {
			if !unicode.IsLetter(character) {
				return false
			}
			continue
		}
		if !unicode.IsLetter(character) && !unicode.IsDigit(character) && character != '_' && character != '.' && character != ':' && character != '-' {
			return false
		}
	}
	return true
}

// NormalizeSupplierParameterMapping validates the local checkout-field key to
// upstream parameter-key mapping. Local keys follow the checkout field
// contract; upstream keys allow the conservative interoperable subset used by
// the supplier OpenAPI protocol.
func NormalizeSupplierParameterMapping(mapping map[string]string) (map[string]string, error) {
	if len(mapping) > SupplierParameterMappingLimit {
		return nil, fmt.Errorf("%w: at most %d entries are allowed", ErrSupplierParameterMappingInvalid, SupplierParameterMappingLimit)
	}
	normalized := make(map[string]string, len(mapping))
	usedTargets := make(map[string]struct{}, len(mapping))
	for source, target := range mapping {
		source = strings.TrimSpace(source)
		target = strings.TrimSpace(target)
		if !localSupplierParameterKeyPattern.MatchString(source) || !validRemoteSupplierParameterKey(target) {
			return nil, fmt.Errorf("%w: unsafe source or target key", ErrSupplierParameterMappingInvalid)
		}
		if _, duplicate := normalized[source]; duplicate {
			return nil, fmt.Errorf("%w: duplicate normalized source key", ErrSupplierParameterMappingInvalid)
		}
		if _, duplicate := usedTargets[target]; duplicate {
			return nil, fmt.Errorf("%w: duplicate target key", ErrSupplierParameterMappingInvalid)
		}
		normalized[source] = target
		usedTargets[target] = struct{}{}
	}
	return normalized, nil
}

// EncodeSupplierParameterMapping returns canonical object JSON. encoding/json
// sorts string map keys, keeping API responses, migrations and audit diffs
// deterministic.
func EncodeSupplierParameterMapping(mapping map[string]string) (json.RawMessage, error) {
	normalized, err := NormalizeSupplierParameterMapping(mapping)
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(normalized)
	if err != nil {
		return nil, fmt.Errorf("%w: encode mapping: %v", ErrSupplierParameterMappingInvalid, err)
	}
	return json.RawMessage(encoded), nil
}

// DecodeSupplierParameterMapping accepts only a JSON object whose values are
// strings. It rejects duplicate JSON members instead of silently keeping the
// last value, which makes PATCH validation unambiguous.
func DecodeSupplierParameterMapping(raw json.RawMessage) (map[string]string, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return map[string]string{}, nil
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	opening, err := decoder.Token()
	if err != nil {
		return nil, fmt.Errorf("%w: malformed JSON object", ErrSupplierParameterMappingInvalid)
	}
	delimiter, ok := opening.(json.Delim)
	if !ok || delimiter != '{' {
		return nil, fmt.Errorf("%w: mapping must be an object", ErrSupplierParameterMappingInvalid)
	}
	decoded := make(map[string]string)
	for decoder.More() {
		keyToken, tokenErr := decoder.Token()
		if tokenErr != nil {
			return nil, fmt.Errorf("%w: malformed object key", ErrSupplierParameterMappingInvalid)
		}
		key, ok := keyToken.(string)
		if !ok {
			return nil, fmt.Errorf("%w: object key must be a string", ErrSupplierParameterMappingInvalid)
		}
		if _, duplicate := decoded[key]; duplicate {
			return nil, fmt.Errorf("%w: duplicate source key", ErrSupplierParameterMappingInvalid)
		}
		var value string
		if decodeErr := decoder.Decode(&value); decodeErr != nil {
			return nil, fmt.Errorf("%w: target key must be a string", ErrSupplierParameterMappingInvalid)
		}
		decoded[key] = value
	}
	closing, err := decoder.Token()
	if err != nil || closing != json.Delim('}') {
		return nil, fmt.Errorf("%w: malformed JSON object", ErrSupplierParameterMappingInvalid)
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("%w: trailing JSON data", ErrSupplierParameterMappingInvalid)
	}
	return NormalizeSupplierParameterMapping(decoded)
}

// ApplySupplierParameterMapping changes only parameter names. Values are never
// copied into configuration or audit data, and a mapped key may not overwrite
// another submitted parameter.
func ApplySupplierParameterMapping(parameters map[string]string, raw json.RawMessage) (map[string]string, error) {
	mapping, err := DecodeSupplierParameterMapping(raw)
	if err != nil {
		return nil, err
	}
	if len(parameters) > SupplierParameterMappingLimit {
		return nil, fmt.Errorf("%w: too many submitted parameters", ErrSupplierParameterMappingInvalid)
	}
	result := make(map[string]string, len(parameters))
	for source, value := range parameters {
		if !localSupplierParameterKeyPattern.MatchString(source) {
			return nil, fmt.Errorf("%w: unsafe submitted source key", ErrSupplierParameterMappingInvalid)
		}
		target := source
		if mapped, exists := mapping[source]; exists {
			target = mapped
		}
		if _, duplicate := result[target]; duplicate {
			return nil, fmt.Errorf("%w: mapped parameter key collision", ErrSupplierParameterMappingInvalid)
		}
		result[target] = value
	}
	return result, nil
}
