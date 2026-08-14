package handler

import (
	"context"
	"errors"
	"net/mail"
	"strings"
	"unicode"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"linlinqi/api/internal/model"
	"linlinqi/api/internal/service"
)

var errOpenAPICallbackLimit = errors.New("OpenAPI callback endpoint limit reached")

const maxOpenAPICallbackEndpoints = 10

type openAPISupplyOrderResult struct {
	ClientOrderNo   string   `json:"client_order_no"`
	ExternalOrderNo string   `json:"external_order_no"`
	Status          string   `json:"status"`
	Deliveries      []string `json:"deliveries"`
	Cost            int64    `json:"cost"`
	CostCurrency    string   `json:"cost_currency"`
	CostMinorUnit   int      `json:"cost_minor_unit"`
}

func validOpenAPIEmail(value string) bool {
	if value == "" || len(value) > 190 || strings.TrimSpace(value) != value {
		return false
	}
	address, err := mail.ParseAddress(value)
	return err == nil && strings.EqualFold(address.Address, value)
}

func validOpenAPIIdentifier(value string, maximum int) bool {
	value = strings.TrimSpace(value)
	if value == "" || len([]rune(value)) > maximum {
		return false
	}
	return strings.IndexFunc(value, unicode.IsControl) < 0
}

func resolveOpenAPIProduct(db *gorm.DB, productIdentifier, variantIdentifier string) (uuid.UUID, *uuid.UUID, error) {
	productIdentifier = strings.TrimSpace(productIdentifier)
	variantIdentifier = strings.TrimSpace(variantIdentifier)
	if !validOpenAPIIdentifier(productIdentifier, 180) {
		return uuid.Nil, nil, errors.New("invalid external product identifier")
	}
	var product model.Product
	if parsed, err := uuid.Parse(productIdentifier); err == nil {
		if db.Select("id").First(&product, "id = ?", parsed).Error == nil {
			if variantIdentifier == "" {
				return product.ID, nil, nil
			}
		} else if variantIdentifier == "" {
			var variant model.ProductVariant
			if err := db.Select("id", "product_id").First(&variant, "id = ? AND status = ?", parsed, "active").Error; err == nil {
				return variant.ProductID, &variant.ID, nil
			}
		}
	}
	if product.ID == uuid.Nil {
		if err := db.Select("id").First(&product, "slug = ?", productIdentifier).Error; err != nil {
			if variantIdentifier != "" {
				return uuid.Nil, nil, err
			}
			var variant model.ProductVariant
			if variantErr := db.Select("id", "product_id").First(&variant, "sku = ? AND status = ?", productIdentifier, "active").Error; variantErr != nil {
				return uuid.Nil, nil, variantErr
			}
			return variant.ProductID, &variant.ID, nil
		}
	}
	if variantIdentifier == "" {
		return product.ID, nil, nil
	}
	var variant model.ProductVariant
	variantQuery := db.Select("id", "product_id").Where("product_id = ? AND status = ?", product.ID, "active")
	if parsed, err := uuid.Parse(variantIdentifier); err == nil {
		variantQuery = variantQuery.Where("id = ?", parsed)
	} else {
		variantQuery = variantQuery.Where("sku = ?", variantIdentifier)
	}
	if err := variantQuery.First(&variant).Error; err != nil {
		return uuid.Nil, nil, err
	}
	return product.ID, &variant.ID, nil
}

func normalizeOpenAPICallbackURL(ctx context.Context, raw, environment string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	// Callback URLs create WebhookEndpoint records, so they share the same
	// untrusted egress policy as user-managed webhook destinations. Keep the
	// environment parameter for call-site compatibility; the policy must not be
	// weakened by a development runtime.
	_ = environment
	return normalizeWebhookEndpointURL(ctx, raw)
}

func (h Handler) ensureOpenAPICallbackEndpoint(tx *gorm.DB, credential model.APICredential, secret, callbackURL string) (*uuid.UUID, error) {
	if callbackURL == "" {
		return nil, nil
	}
	if tx.Dialector.Name() == "postgres" {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtext(?))", "openapi-callbacks:"+credential.ID.String()).Error; err != nil {
			return nil, err
		}
	}
	var existing model.WebhookEndpoint
	if err := tx.Where("owner_type = ? AND owner_id = ? AND url = ?", "api_credential", credential.ID, callbackURL).First(&existing).Error; err == nil {
		return &existing.ID, nil
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	var count int64
	if err := tx.Model(&model.WebhookEndpoint{}).Where("owner_type = ? AND owner_id = ?", "api_credential", credential.ID).Count(&count).Error; err != nil {
		return nil, err
	}
	if count >= maxOpenAPICallbackEndpoints {
		return nil, errOpenAPICallbackLimit
	}
	endpoint := model.WebhookEndpoint{
		Base: model.Base{ID: uuid.New()}, OwnerType: "api_credential", OwnerID: credential.ID,
		URL: callbackURL, Events: `["order.delivered"]`, Enabled: true,
	}
	ciphertext, nonce, _, err := h.Vault.Encrypt(secret, endpoint.ID[:])
	if err != nil {
		return nil, err
	}
	endpoint.SecretCipher, endpoint.SecretNonce = ciphertext, nonce
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&endpoint).Error; err != nil {
		return nil, err
	}
	if err := tx.Where("owner_type = ? AND owner_id = ? AND url = ?", "api_credential", credential.ID, callbackURL).First(&endpoint).Error; err != nil {
		return nil, err
	}
	return &endpoint.ID, nil
}

func (h Handler) toOpenAPISupplyOrderResult(order model.Order) (openAPISupplyOrderResult, error) {
	currencyCode := strings.ToUpper(strings.TrimSpace(order.Currency))
	var currencyDefinition model.CurrencyDefinition
	if len(currencyCode) != 3 || h.DB.Select("minor_unit").First(&currencyDefinition, "code = ?", currencyCode).Error != nil {
		return openAPISupplyOrderResult{}, errors.New("order currency snapshot is invalid")
	}
	result := openAPISupplyOrderResult{
		ExternalOrderNo: order.OrderNo, Status: strings.ToLower(strings.TrimSpace(order.Status)), Cost: order.Total,
		CostCurrency: currencyCode, CostMinorUnit: currencyDefinition.MinorUnit,
		Deliveries: []string{},
	}
	if order.ExternalOrderNo != nil {
		result.ClientOrderNo = *order.ExternalOrderNo
	}
	if (result.Status != "delivered" && result.Status != "completed") || order.PaymentStatus != "paid" {
		return result, nil
	}
	for _, item := range order.Items {
		values, err := service.DecryptDeliveryItems(h.Vault, item)
		if err != nil {
			return openAPISupplyOrderResult{}, err
		}
		result.Deliveries = append(result.Deliveries, values...)
	}
	return result, nil
}
