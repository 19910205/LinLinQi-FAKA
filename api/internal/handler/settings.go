package handler

import (
	"fmt"
	"net/mail"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gin-gonic/gin"

	"linlinqi/api/pkg/response"
)

type settingSpecification struct {
	group      string
	allowEmpty bool
	validate   func(string) bool
}

var isoCurrencyCodePattern = regexp.MustCompile(`^[A-Z]{3}$`)

func runeLengthBetween(minimum, maximum int) func(string) bool {
	return func(value string) bool {
		length := utf8.RuneCountInString(value)
		return length >= minimum && length <= maximum
	}
}

func integerBetween(minimum, maximum int64) func(string) bool {
	return func(value string) bool {
		parsed, err := strconv.ParseInt(value, 10, 64)
		return err == nil && parsed >= minimum && parsed <= maximum
	}
}

func validImageURL(value string) bool {
	_, err := normalizeImageURL(value, false)
	return err == nil
}

func validSupportEmail(value string) bool {
	address, err := mail.ParseAddress(value)
	return err == nil && strings.EqualFold(address.Address, value) && len(value) <= 190
}

var adminSettingSpecifications = map[string]settingSpecification{
	"store_name":                     {group: "store", validate: runeLengthBetween(2, 80)},
	"store_tagline":                  {group: "store", validate: runeLengthBetween(2, 200)},
	"store_currency":                 {group: "store", validate: isoCurrencyCodePattern.MatchString},
	"store_logo_url":                 {group: "store", allowEmpty: true, validate: validImageURL},
	"store_support_email":            {group: "store", allowEmpty: true, validate: validSupportEmail},
	"store_seo_title":                {group: "store", allowEmpty: true, validate: runeLengthBetween(2, 100)},
	"store_seo_description":          {group: "store", allowEmpty: true, validate: runeLengthBetween(2, 300)},
	"order_timeout_minutes":          {group: "order", validate: integerBetween(5, 1440)},
	"inventory_warning_threshold":    {group: "inventory", validate: integerBetween(1, 100000)},
	"affiliate_default_basis_points": {group: "affiliate", validate: integerBetween(1, 3000)},
	"affiliate_hold_days":            {group: "affiliate", validate: integerBetween(1, 90)},
	"affiliate_withdrawal_minimum":   {group: "affiliate", validate: integerBetween(1, 1_000_000_000_000_000)},
}

func normalizeAdminSettings(values map[string]string) (map[string]string, map[string]string, error) {
	if len(values) == 0 {
		return nil, nil, fmt.Errorf("settings are empty")
	}
	normalized := make(map[string]string, len(values))
	groups := make(map[string]string, len(values))
	for key, raw := range values {
		specification, ok := adminSettingSpecifications[key]
		if !ok {
			return nil, nil, fmt.Errorf("unsupported setting %s", key)
		}
		value := strings.TrimSpace(raw)
		if value == "" {
			if !specification.allowEmpty {
				return nil, nil, fmt.Errorf("setting %s cannot be empty", key)
			}
		} else if specification.validate == nil || !specification.validate(value) {
			return nil, nil, fmt.Errorf("invalid setting %s", key)
		}
		normalized[key] = value
		groups[key] = specification.group
	}
	return normalized, groups, nil
}

// UploadSettingsMedia stores a validated image for store branding (for
// example the shop logo) and returns its public media URL. Requires
// system.manage; the caller must supply an audit change reason.
func (h Handler) UploadSettingsMedia(c *gin.Context) {
	dto, ok := h.uploadAdminImage(c, "上传店铺设置图片", "settings_media.upload")
	if !ok {
		return
	}
	response.Created(c, dto)
}
