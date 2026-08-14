package handler

import (
	"errors"
	"fmt"
	"strings"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"linlinqi/api/internal/model"
	"linlinqi/api/internal/service"
)

var (
	errCurrencySelectionInvalid     = errors.New("currency selection is invalid")
	errCurrencySelectionUnavailable = errors.New("currency selection is unavailable")
)

// resolveEnabledCurrencyDefinition normalizes an explicitly selected ISO
// currency and verifies that new financial activity is enabled for it. Empty
// values may safely fall back to the store currency for backward-compatible
// clients, but an explicitly empty query parameter is rejected by
// optionalCurrencyQuery before reaching this helper.
func resolveEnabledCurrencyDefinition(db *gorm.DB, value string, defaultToStore bool) (model.CurrencyDefinition, error) {
	code := strings.TrimSpace(value)
	if code == "" {
		if !defaultToStore {
			return model.CurrencyDefinition{}, errCurrencySelectionInvalid
		}
		var err error
		code, err = service.StoreCurrency(db)
		if err != nil {
			return model.CurrencyDefinition{}, err
		}
	}
	code = strings.ToUpper(code)
	if !isoCurrencyCodePattern.MatchString(code) {
		return model.CurrencyDefinition{}, errCurrencySelectionInvalid
	}
	var definition model.CurrencyDefinition
	if err := db.Where("code = ? AND enabled = ?", code, true).First(&definition).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return model.CurrencyDefinition{}, errCurrencySelectionUnavailable
		}
		return model.CurrencyDefinition{}, err
	}
	return definition, nil
}

// optionalCurrencyQuery distinguishes an omitted currency (safe default) from
// an explicitly empty or repeated parameter (ambiguous and therefore invalid).
func optionalCurrencyQuery(c *gin.Context) (string, bool, error) {
	values, specified := c.Request.URL.Query()["currency"]
	if !specified {
		return "", false, nil
	}
	if len(values) != 1 || strings.TrimSpace(values[0]) == "" {
		return "", true, errCurrencySelectionInvalid
	}
	return values[0], true, nil
}

func currencyMinorScale(definition model.CurrencyDefinition) (int64, error) {
	if definition.MinorUnit < 0 || definition.MinorUnit > 6 {
		return 0, fmt.Errorf("currency minor unit is outside the supported range")
	}
	scale := int64(1)
	for index := 0; index < definition.MinorUnit; index++ {
		scale *= 10
	}
	return scale, nil
}
