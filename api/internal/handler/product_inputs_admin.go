package handler

import (
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"linlinqi/api/internal/model"
	"linlinqi/api/pkg/response"
)

var productInputKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

var errProductInputFieldKeyImmutable = errors.New("product input field key is immutable")

type productInputFieldRequest struct {
	Key               string   `json:"key"`
	Label             string   `json:"label"`
	InputType         string   `json:"input_type"`
	Required          bool     `json:"required"`
	Sensitive         bool     `json:"sensitive"`
	PassToSupplier    bool     `json:"pass_to_supplier"`
	Placeholder       string   `json:"placeholder"`
	HelpText          string   `json:"help_text"`
	Options           []string `json:"options"`
	ValidationPattern string   `json:"validation_pattern"`
	MinLength         int      `json:"min_length"`
	MaxLength         int      `json:"max_length"`
	Sort              int      `json:"sort"`
	Enabled           bool     `json:"enabled"`
}

func (request *productInputFieldRequest) normalizeAndValidate() error {
	request.Key = strings.ToLower(strings.TrimSpace(request.Key))
	request.Label = strings.TrimSpace(request.Label)
	request.InputType = strings.ToLower(strings.TrimSpace(request.InputType))
	request.Placeholder = strings.TrimSpace(request.Placeholder)
	request.HelpText = strings.TrimSpace(request.HelpText)
	request.ValidationPattern = strings.TrimSpace(request.ValidationPattern)
	if !productInputKeyPattern.MatchString(request.Key) || utf8.RuneCountInString(request.Label) < 1 || utf8.RuneCountInString(request.Label) > 120 || utf8.RuneCountInString(request.Placeholder) > 200 || utf8.RuneCountInString(request.HelpText) > 500 || len(request.ValidationPattern) > 300 || request.MinLength < 0 || request.MaxLength < 1 || request.MaxLength > 2000 || request.MinLength > request.MaxLength || request.Sort < 0 || request.Sort > 1_000_000 {
		return errCatalogInvalidRequest
	}
	switch request.InputType {
	case "text", "email", "number", "select", "textarea":
	default:
		return errCatalogInvalidRequest
	}
	if request.InputType == "email" && request.MaxLength > 190 {
		return errCatalogInvalidRequest
	}
	if request.InputType == "number" && request.MaxLength > 64 {
		return errCatalogInvalidRequest
	}
	if request.ValidationPattern != "" {
		if _, err := regexp.Compile("^(?:" + request.ValidationPattern + ")$"); err != nil {
			return errCatalogInvalidRequest
		}
	}
	if request.InputType != "select" {
		request.Options = []string{}
		return nil
	}
	if len(request.Options) < 1 || len(request.Options) > 50 {
		return errCatalogInvalidRequest
	}
	seen := make(map[string]struct{}, len(request.Options))
	for index, option := range request.Options {
		option = strings.TrimSpace(option)
		if utf8.RuneCountInString(option) < 1 || utf8.RuneCountInString(option) > 120 {
			return errCatalogInvalidRequest
		}
		if _, duplicate := seen[option]; duplicate {
			return errCatalogInvalidRequest
		}
		seen[option] = struct{}{}
		request.Options[index] = option
	}
	return nil
}

func (request productInputFieldRequest) values(productID uuid.UUID) (model.ProductInputField, error) {
	options, err := json.Marshal(request.Options)
	if err != nil {
		return model.ProductInputField{}, err
	}
	return model.ProductInputField{
		ProductID: productID, Key: request.Key, Label: request.Label, InputType: request.InputType,
		Required: request.Required, Sensitive: request.Sensitive, PassToSupplier: request.PassToSupplier,
		Placeholder: request.Placeholder, HelpText: request.HelpText, Options: options,
		ValidationPattern: request.ValidationPattern, MinLength: request.MinLength,
		MaxLength: request.MaxLength, Sort: request.Sort, Enabled: request.Enabled,
	}, nil
}

func (h Handler) AdminProductInputFields(c *gin.Context) {
	productID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, 422, 42320, "error.product_id_invalid")
		return
	}
	var product model.Product
	if err := h.DB.Select("id").First(&product, "id = ?", productID).Error; err != nil {
		response.Error(c, 404, 40430, "error.product_not_found")
		return
	}
	var items []model.ProductInputField
	if err := h.DB.Where("product_id = ?", productID).Order("sort DESC, created_at ASC").Find(&items).Error; err != nil {
		response.Error(c, 500, 50090, "error.product_input_fields_fetch_failed")
		return
	}
	response.OK(c, items)
}

func (h Handler) CreateProductInputField(c *gin.Context) {
	productID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, 422, 42320, "error.product_id_invalid")
		return
	}
	reason, ok := requireAdminChangeReason(c, "创建商品下单字段")
	if !ok {
		return
	}
	var request productInputFieldRequest
	if decodeStrictJSON(c, &request) != nil || request.normalizeAndValidate() != nil {
		response.Error(c, 422, 42321, "error.product_input_field_invalid")
		return
	}
	item, err := request.values(productID)
	if err != nil {
		response.Error(c, 422, 42321, "error.product_input_field_invalid")
		return
	}
	err = h.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id").First(&model.Product{}, "id = ?", productID).Error; err != nil {
			return err
		}
		var count int64
		if err := tx.Model(&model.ProductInputField{}).Where("product_id = ?", productID).Count(&count).Error; err != nil {
			return err
		}
		if count >= 20 {
			return errCatalogUnsafeChange
		}
		return createWithExplicitColumns(tx, &item, map[string]any{"enabled": request.Enabled})
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		response.Error(c, 404, 40430, "error.product_not_found")
		return
	}
	if errors.Is(err, errCatalogUnsafeChange) {
		response.Error(c, 409, 40995, "error.product_input_field_limit_reached")
		return
	}
	if err != nil {
		response.Error(c, 409, 40996, "error.product_input_field_key_exists")
		return
	}
	item.Enabled = request.Enabled
	h.audit(c, "product-input-field.create", "product_input_field", item.ID.String(), reason)
	response.Created(c, item)
}

func (h Handler) UpdateProductInputField(c *gin.Context) {
	fieldID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, 422, 42322, "error.product_input_field_id_invalid")
		return
	}
	reason, ok := requireAdminChangeReason(c, "更新商品下单字段")
	if !ok {
		return
	}
	var request productInputFieldRequest
	if decodeStrictJSON(c, &request) != nil || request.normalizeAndValidate() != nil {
		response.Error(c, 422, 42321, "error.product_input_field_invalid")
		return
	}
	var item model.ProductInputField
	err = h.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&item, "id = ?", fieldID).Error; err != nil {
			return err
		}
		if request.Key != item.Key {
			return errProductInputFieldKeyImmutable
		}
		next, valueErr := request.values(item.ProductID)
		if valueErr != nil {
			return valueErr
		}
		return tx.Model(&item).Updates(map[string]any{
			"key": next.Key, "label": next.Label, "input_type": next.InputType,
			"required": next.Required, "sensitive": next.Sensitive, "pass_to_supplier": next.PassToSupplier,
			"placeholder": next.Placeholder, "help_text": next.HelpText, "options": next.Options,
			"validation_pattern": next.ValidationPattern, "min_length": next.MinLength,
			"max_length": next.MaxLength, "sort": next.Sort, "enabled": next.Enabled,
		}).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		response.Error(c, 404, 40481, "error.product_input_field_not_found")
		return
	}
	if errors.Is(err, errProductInputFieldKeyImmutable) {
		response.Error(c, 409, 40997, "error.product_input_field_key_immutable")
		return
	}
	if err != nil {
		response.Error(c, 409, 40996, "error.product_input_field_key_exists_or_update_failed")
		return
	}
	h.audit(c, "product-input-field.update", "product_input_field", item.ID.String(), reason)
	h.DB.First(&item, "id = ?", item.ID)
	response.OK(c, item)
}

func (h Handler) DeleteProductInputField(c *gin.Context) {
	fieldID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, 422, 42322, "error.product_input_field_id_invalid")
		return
	}
	reason, ok := requireAdminChangeReason(c, "删除商品下单字段")
	if !ok {
		return
	}
	var item model.ProductInputField
	if err := h.DB.First(&item, "id = ?", fieldID).Error; err != nil {
		response.Error(c, 404, 40481, "error.product_input_field_not_found")
		return
	}
	if err := h.DB.Delete(&item).Error; err != nil {
		response.Error(c, 500, 50091, "error.product_input_field_delete_failed")
		return
	}
	h.audit(c, "product-input-field.delete", "product_input_field", item.ID.String(), reason)
	response.OK(c, gin.H{"deleted": true})
}
