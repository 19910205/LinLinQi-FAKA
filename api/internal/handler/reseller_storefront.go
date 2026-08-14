package handler

import (
	"encoding/json"
	"errors"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"linlinqi/api/internal/content"
	"linlinqi/api/internal/model"
	"linlinqi/api/internal/service"
)

type resellerStorefront struct {
	Profile model.ResellerProfile
	Site    model.ResellerSite
}

// resolveStorefront maps the browser hostname to an active, ownership-verified
// reseller domain. X-Reseller-Code is a deliberate preview path for the
// reseller console; it never permits a price below the platform price.
func (h Handler) resolveStorefront(c *gin.Context) (*resellerStorefront, error) {
	host := strings.TrimSpace(c.GetHeader("X-Storefront-Host"))
	code := strings.ToUpper(strings.TrimSpace(c.GetHeader("X-Reseller-Code")))
	var profile model.ResellerProfile
	if host != "" {
		normalized, err := normalizeResellerDomain(host)
		if err == nil {
			err = h.DB.Model(&model.ResellerProfile{}).
				Joins("JOIN reseller_domains rd ON rd.reseller_id = reseller_profiles.id AND rd.deleted_at IS NULL").
				Where("rd.domain = ? AND rd.status = ? AND rd.tls_status = ? AND reseller_profiles.status = ?", normalized, "active", "active", "active").
				First(&profile).Error
			if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, err
			}
		}
	}
	if profile.ID == uuid.Nil && code != "" {
		if err := h.DB.Where("UPPER(code) = ? AND status = ?", code, "active").First(&profile).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return nil, nil
			}
			return nil, err
		}
	}
	if profile.ID == uuid.Nil {
		return nil, nil
	}
	storefront := resellerStorefront{Profile: profile, Site: model.ResellerSite{ResellerID: profile.ID, SiteName: profile.Name, Theme: `{"mode":"system","density":"comfortable"}`, SEO: `{}`, Support: `{}`}}
	if err := h.DB.Where("reseller_id = ?", profile.ID).First(&storefront.Site).Error; err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	return &storefront, nil
}

func storefrontResellerID(storefront *resellerStorefront) *uuid.UUID {
	if storefront == nil {
		return nil
	}
	id := storefront.Profile.ID
	return &id
}

func sameStorefront(left, right *uuid.UUID) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func decodeJSONMap(raw string) map[string]any {
	result := map[string]any{}
	if json.Unmarshal([]byte(raw), &result) != nil {
		return map[string]any{}
	}
	return result
}

func applyResellerCatalogScope(query *gorm.DB, resellerID uuid.UUID) *gorm.DB {
	return query.Where(`(
		(NOT EXISTS (
			SELECT 1 FROM product_variants pv
			WHERE pv.product_id = products.id AND pv.deleted_at IS NULL AND pv.status = 'active'
		) AND EXISTS (
			SELECT 1 FROM reseller_product_rules base_rule
			WHERE base_rule.reseller_id = ? AND base_rule.product_id = products.id
				AND base_rule.variant_id IS NULL AND base_rule.deleted_at IS NULL AND base_rule.enabled = TRUE
		)) OR EXISTS (
			SELECT 1 FROM product_variants pv
			LEFT JOIN reseller_product_rules exact_rule
				ON exact_rule.reseller_id = ? AND exact_rule.product_id = products.id
				AND exact_rule.variant_id = pv.id AND exact_rule.deleted_at IS NULL
			LEFT JOIN reseller_product_rules base_rule
				ON base_rule.reseller_id = ? AND base_rule.product_id = products.id
				AND base_rule.variant_id IS NULL AND base_rule.deleted_at IS NULL
			WHERE pv.product_id = products.id AND pv.deleted_at IS NULL AND pv.status = 'active'
				AND COALESCE(exact_rule.enabled, base_rule.enabled, FALSE) = TRUE
		)
	)`, resellerID, resellerID, resellerID)
}

// applyResellerProductPresentation keeps reseller overrides on the same
// public-output security boundary as the platform catalog. In particular,
// rich descriptions are stored for editing but are always sanitized before
// a storefront can render them with v-html.
func applyResellerProductPresentation(dto publicProductDTO, presentation model.ResellerProductPresentation) publicProductDTO {
	if strings.TrimSpace(presentation.Name) != "" {
		dto.Name = presentation.Name
	}
	if strings.TrimSpace(presentation.Summary) != "" {
		dto.Summary = presentation.Summary
	}
	if strings.TrimSpace(presentation.Description) != "" {
		dto.Description = content.SanitizeRichHTML(presentation.Description)
	}
	if strings.TrimSpace(presentation.CoverURL) != "" {
		dto.CoverURL = presentation.CoverURL
	}
	return dto
}

// resellerProductPresentation returns only variants the reseller explicitly
// enabled. For variant products the card price is the lowest enabled variant,
// which keeps listing and product-detail prices consistent.
func (h Handler) resellerProductPresentation(db *gorm.DB, product model.Product, resellerID uuid.UUID, includeVariants bool) (publicProductDTO, []publicProductVariantDTO, int64, bool, error) {
	dto := toPublicProductDTO(product)
	var presentation model.ResellerProductPresentation
	if err := db.Where("reseller_id = ? AND product_id = ?", resellerID, product.ID).First(&presentation).Error; err == nil {
		dto = applyResellerProductPresentation(dto, presentation)
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return dto, nil, 0, false, err
	}
	var variants []model.ProductVariant
	if err := db.Where("product_id = ? AND status = ?", product.ID, "active").Order("sort DESC, created_at ASC").Find(&variants).Error; err != nil {
		return dto, nil, 0, false, err
	}
	if len(variants) == 0 {
		price, enabled, err := service.ResolveResellerSalePrice(db, resellerID, product.ID, nil, product.Price)
		if err != nil || !enabled {
			return dto, nil, 0, false, err
		}
		dto.Price = price
		if dto.ComparePrice < price {
			dto.ComparePrice = price
		}
		return dto, nil, h.productStockForVariant(product, nil), true, nil
	}
	publicVariants := make([]publicProductVariantDTO, 0, len(variants))
	stock := int64(0)
	minimumPrice := int64(0)
	found := false
	for _, variant := range variants {
		price, enabled, err := service.ResolveResellerSalePrice(db, resellerID, product.ID, &variant.ID, variant.Price)
		if err != nil {
			return dto, nil, 0, false, err
		}
		if !enabled {
			continue
		}
		variantStock := h.productStockForVariant(product, &variant.ID)
		stock += variantStock
		if !found || price < minimumPrice {
			minimumPrice = price
		}
		found = true
		if includeVariants {
			variantDTO := toPublicProductVariantDTO(variant, variantStock)
			variantDTO.Price = price
			if variantDTO.ComparePrice < price {
				variantDTO.ComparePrice = price
			}
			publicVariants = append(publicVariants, variantDTO)
		}
	}
	if !found {
		return dto, nil, 0, false, nil
	}
	dto.Price = minimumPrice
	if dto.ComparePrice < minimumPrice {
		dto.ComparePrice = minimumPrice
	}
	return dto, publicVariants, stock, true, nil
}
