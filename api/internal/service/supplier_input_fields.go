package service

import (
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"linlinqi/api/internal/model"
	"linlinqi/api/internal/supply"
)

// NormalizeSupplierInputFields turns a protocol schema into the local,
// server-validated checkout schema and its local-key -> upstream-key mapping.
// Customer values are deliberately not part of either returned object.
func NormalizeSupplierInputFields(productID uuid.UUID, upstream []supply.ProductInputField) ([]model.ProductInputField, map[string]string, error) {
	if productID == uuid.Nil || len(upstream) > SupplierParameterMappingLimit {
		return nil, nil, ErrSupplierParameterMappingInvalid
	}
	result := make([]model.ProductInputField, 0, len(upstream))
	mapping := make(map[string]string, len(upstream))
	seen := make(map[string]struct{}, len(upstream))
	for index, field := range upstream {
		field.Key = strings.ToLower(strings.TrimSpace(field.Key))
		field.ExternalKey = strings.TrimSpace(field.ExternalKey)
		field.Label = strings.TrimSpace(field.Label)
		field.InputType = strings.ToLower(strings.TrimSpace(field.InputType))
		field.Placeholder = strings.TrimSpace(field.Placeholder)
		field.HelpText = strings.TrimSpace(field.HelpText)
		field.ValidationPattern = strings.TrimSpace(field.ValidationPattern)
		if field.InputType == "" {
			field.InputType = "text"
		}
		if field.MaxLength == 0 {
			field.MaxLength = 200
		}
		if !localSupplierParameterKeyPattern.MatchString(field.Key) || utf8.RuneCountInString(field.Label) < 1 || utf8.RuneCountInString(field.Label) > 120 || utf8.RuneCountInString(field.Placeholder) > 200 || utf8.RuneCountInString(field.HelpText) > 500 || len(field.ValidationPattern) > 300 || field.MinLength < 0 || field.MaxLength < 1 || field.MaxLength > 2000 || field.MinLength > field.MaxLength {
			return nil, nil, ErrSupplierParameterMappingInvalid
		}
		switch field.InputType {
		case "text", "email", "number", "select", "textarea":
		default:
			return nil, nil, ErrSupplierParameterMappingInvalid
		}
		if field.InputType == "email" && field.MaxLength > 190 || field.InputType == "number" && field.MaxLength > 64 {
			return nil, nil, ErrSupplierParameterMappingInvalid
		}
		if field.ValidationPattern != "" {
			if _, err := regexp.Compile("^(?:" + field.ValidationPattern + ")$"); err != nil {
				return nil, nil, ErrSupplierParameterMappingInvalid
			}
		}
		options := []string{}
		if field.InputType == "select" {
			if len(field.Options) < 1 || len(field.Options) > 50 {
				return nil, nil, ErrSupplierParameterMappingInvalid
			}
			optionSeen := map[string]struct{}{}
			for _, option := range field.Options {
				option = strings.TrimSpace(option)
				if utf8.RuneCountInString(option) < 1 || utf8.RuneCountInString(option) > 120 {
					return nil, nil, ErrSupplierParameterMappingInvalid
				}
				if _, duplicate := optionSeen[option]; duplicate {
					return nil, nil, ErrSupplierParameterMappingInvalid
				}
				optionSeen[option] = struct{}{}
				options = append(options, option)
			}
		}
		if _, duplicate := seen[field.Key]; duplicate {
			return nil, nil, ErrSupplierParameterMappingInvalid
		}
		seen[field.Key] = struct{}{}
		externalKey := field.ExternalKey
		if externalKey == "" {
			externalKey = field.Key
		}
		mapping[field.Key] = externalKey
		encodedOptions, err := json.Marshal(options)
		if err != nil {
			return nil, nil, errors.Join(ErrSupplierParameterMappingInvalid, err)
		}
		result = append(result, model.ProductInputField{
			ProductID: productID, Key: field.Key, Label: field.Label, InputType: field.InputType,
			Required: field.Required, Sensitive: field.Sensitive, PassToSupplier: true,
			Placeholder: field.Placeholder, HelpText: field.HelpText, Options: encodedOptions,
			ValidationPattern: field.ValidationPattern, MinLength: field.MinLength, MaxLength: field.MaxLength,
			Sort: len(upstream) - index, Enabled: true,
		})
	}
	if _, err := EncodeSupplierParameterMapping(mapping); err != nil {
		return nil, nil, err
	}
	return result, mapping, nil
}
