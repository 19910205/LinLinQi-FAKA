package handler

import (
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"linlinqi/api/internal/model"
	"linlinqi/api/internal/service"
	"linlinqi/api/pkg/response"
)

type publicCurrencyDTO struct {
	Code      string `json:"code"`
	Name      string `json:"name"`
	Symbol    string `json:"symbol"`
	MinorUnit int    `json:"minor_unit"`
	Enabled   bool   `json:"enabled,omitempty"`
}

func loadPublicCurrencies(db *gorm.DB) ([]publicCurrencyDTO, error) {
	var stored []model.CurrencyDefinition
	if err := db.Select("code", "name", "symbol", "minor_unit").Where("enabled = ?", true).Order("display_sort DESC, code ASC").Find(&stored).Error; err != nil {
		return nil, err
	}
	items := make([]publicCurrencyDTO, 0, len(stored))
	for _, currency := range stored {
		items = append(items, publicCurrencyDTO{Code: currency.Code, Name: currency.Name, Symbol: currency.Symbol, MinorUnit: currency.MinorUnit})
	}
	return items, nil
}

func (h Handler) PublicCurrencies(c *gin.Context) {
	items, err := loadPublicCurrencies(h.DB)
	if err != nil {
		response.Error(c, 500, 50024, "error.currency_list_fetch_failed")
		return
	}
	c.Header("Cache-Control", "public, max-age=300")
	response.OK(c, items)
}

// PublicCurrencyDirectory provides storefront-safe currency metadata together
// with the active store currency. It is deliberately separate from
// PublicCurrencies so the established array response remains compatible.
func (h Handler) PublicCurrencyDirectory(c *gin.Context) {
	items, err := loadPublicCurrencies(h.DB)
	if err != nil {
		response.Error(c, 500, 50024, "error.currency_list_fetch_failed")
		return
	}
	storeCurrency, err := service.StoreCurrency(h.DB)
	if err != nil {
		response.Error(c, 500, 50041, "error.store_currency_fetch_failed")
		return
	}
	for index := range items {
		items[index].Enabled = true
	}
	c.Header("Cache-Control", "public, max-age=300")
	response.OK(c, gin.H{"items": items, "store_currency": storeCurrency})
}
