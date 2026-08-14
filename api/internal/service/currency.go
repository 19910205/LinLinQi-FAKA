package service

import (
	"errors"
	"strings"

	"gorm.io/gorm"

	"linlinqi/api/internal/model"
)

// StoreCurrency returns the enabled ISO currency used for newly created
// catalog, cart, wallet and payment records. Historical records always keep
// their own currency snapshot and must never call this helper retroactively.
func StoreCurrency(db *gorm.DB) (string, error) {
	code := "CNY"
	var setting model.Setting
	if err := db.Select("value").Where("key = ?", "store_currency").First(&setting).Error; err == nil {
		if strings.TrimSpace(setting.Value) != "" {
			code = strings.ToUpper(strings.TrimSpace(setting.Value))
		}
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return "", err
	}
	if len(code) != 3 {
		return "", ErrCurrencyMismatch
	}
	var count int64
	if err := db.Model(&model.CurrencyDefinition{}).Where("code = ? AND enabled = ?", code, true).Count(&count).Error; err != nil {
		return "", err
	}
	if count != 1 {
		return "", ErrCurrencyMismatch
	}
	return code, nil
}
