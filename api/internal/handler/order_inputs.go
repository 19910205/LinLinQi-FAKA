package handler

import (
	"errors"
	"strings"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"linlinqi/api/internal/model"
	"linlinqi/api/internal/service"
)

type checkoutInputValueRequest struct {
	ProductID string `json:"product_id"`
	VariantID string `json:"variant_id"`
	FieldID   string `json:"field_id"`
	Value     string `json:"value"`
}

func sameOptionalUUIDValue(left, right *uuid.UUID) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func parseRequestVariant(raw string) (*uuid.UUID, error) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	value, err := uuid.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, err
	}
	return &value, nil
}

func parseSingleOrderInputValues(db *gorm.DB, productID uuid.UUID, variantID *uuid.UUID, requests []checkoutInputValueRequest, parameters map[string]string) ([]service.SubmittedInputValue, error) {
	if len(requests)+len(parameters) > 20 {
		return nil, service.ErrInputValuesInvalid
	}
	result := make([]service.SubmittedInputValue, 0, len(requests)+len(parameters))
	seen := make(map[uuid.UUID]struct{}, len(requests)+len(parameters))
	for _, request := range requests {
		requestProductID := productID
		if strings.TrimSpace(request.ProductID) != "" {
			parsed, err := uuid.Parse(strings.TrimSpace(request.ProductID))
			if err != nil || parsed != productID {
				return nil, service.ErrInputValuesInvalid
			}
			requestProductID = parsed
		}
		requestVariantID := variantID
		if strings.TrimSpace(request.VariantID) != "" {
			parsed, err := parseRequestVariant(request.VariantID)
			if err != nil || !sameOptionalUUIDValue(parsed, variantID) {
				return nil, service.ErrInputValuesInvalid
			}
			requestVariantID = parsed
		}
		fieldID, err := uuid.Parse(strings.TrimSpace(request.FieldID))
		if err != nil {
			return nil, service.ErrInputValuesInvalid
		}
		if _, duplicate := seen[fieldID]; duplicate {
			return nil, service.ErrInputValuesInvalid
		}
		seen[fieldID] = struct{}{}
		result = append(result, service.SubmittedInputValue{ProductID: requestProductID, VariantID: requestVariantID, FieldID: fieldID, Value: request.Value})
	}
	if len(parameters) == 0 {
		return result, nil
	}
	keys := make([]string, 0, len(parameters))
	for key := range parameters {
		key = strings.ToLower(strings.TrimSpace(key))
		if !productInputKeyPattern.MatchString(key) {
			return nil, service.ErrInputValuesInvalid
		}
		keys = append(keys, key)
	}
	var fields []model.ProductInputField
	if err := db.Where("product_id = ? AND enabled = ? AND key IN ?", productID, true, keys).Find(&fields).Error; err != nil {
		return nil, err
	}
	fieldByKey := make(map[string]model.ProductInputField, len(fields))
	for _, field := range fields {
		fieldByKey[strings.ToLower(field.Key)] = field
	}
	if len(fieldByKey) != len(parameters) {
		return nil, service.ErrInputValuesInvalid
	}
	for rawKey, value := range parameters {
		field := fieldByKey[strings.ToLower(strings.TrimSpace(rawKey))]
		if _, duplicate := seen[field.ID]; duplicate {
			return nil, service.ErrInputValuesInvalid
		}
		seen[field.ID] = struct{}{}
		result = append(result, service.SubmittedInputValue{ProductID: productID, VariantID: variantID, FieldID: field.ID, Value: value})
	}
	return result, nil
}

func parseCartOrderInputValues(requests []checkoutInputValueRequest) ([]service.SubmittedInputValue, error) {
	if len(requests) > 400 {
		return nil, service.ErrInputValuesInvalid
	}
	result := make([]service.SubmittedInputValue, 0, len(requests))
	for _, request := range requests {
		productID, err := uuid.Parse(strings.TrimSpace(request.ProductID))
		if err != nil {
			return nil, service.ErrInputValuesInvalid
		}
		variantID, err := parseRequestVariant(request.VariantID)
		if err != nil {
			return nil, service.ErrInputValuesInvalid
		}
		fieldID, err := uuid.Parse(strings.TrimSpace(request.FieldID))
		if err != nil {
			return nil, service.ErrInputValuesInvalid
		}
		result = append(result, service.SubmittedInputValue{ProductID: productID, VariantID: variantID, FieldID: fieldID, Value: request.Value})
	}
	return result, nil
}

func isOrderInputValidationError(err error) bool {
	return errors.Is(err, service.ErrInputValuesInvalid) || errors.Is(err, service.ErrInputValueRequired)
}
