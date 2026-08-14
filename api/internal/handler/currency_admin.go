package handler

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"linlinqi/api/internal/currency"
	"linlinqi/api/internal/model"
	"linlinqi/api/pkg/response"
)

var exactFXDecimalPattern = regexp.MustCompile(`^(?:0|[1-9][0-9]{0,19})(?:\.[0-9]{1,18})?$`)

type adminCurrencyDTO struct {
	ID          uuid.UUID `json:"id"`
	Code        string    `json:"code"`
	NumericCode string    `json:"numeric_code"`
	Name        string    `json:"name"`
	Symbol      string    `json:"symbol"`
	MinorUnit   int       `json:"minor_unit"`
	Enabled     bool      `json:"enabled"`
	Settlement  bool      `json:"settlement"`
	DisplaySort int       `json:"display_sort"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type adminFXProviderDTO struct {
	ID             uuid.UUID  `json:"id"`
	Code           string     `json:"code"`
	Name           string     `json:"name"`
	Driver         string     `json:"driver"`
	ProviderKey    string     `json:"provider_key,omitempty"`
	BaseURL        string     `json:"base_url"`
	Priority       int        `json:"priority"`
	Enabled        bool       `json:"enabled"`
	TimeoutSeconds int        `json:"timeout_seconds"`
	FailureCount   int        `json:"failure_count"`
	LastSuccessAt  *time.Time `json:"last_success_at,omitempty"`
	LastFailureAt  *time.Time `json:"last_failure_at,omitempty"`
	HasError       bool       `json:"has_error"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

type adminFXManualRateDTO struct {
	ID        uuid.UUID  `json:"id"`
	BaseCode  string     `json:"base_code"`
	QuoteCode string     `json:"quote_code"`
	Rate      string     `json:"rate"`
	Enabled   bool       `json:"enabled"`
	ValidFrom time.Time  `json:"valid_from"`
	ValidTo   *time.Time `json:"valid_to,omitempty"`
	Reason    string     `json:"reason"`
	UpdatedBy uuid.UUID  `json:"updated_by"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

type adminFXSnapshotDTO struct {
	ID               uuid.UUID  `json:"id"`
	BaseCode         string     `json:"base_code"`
	QuoteCode        string     `json:"quote_code"`
	Rate             string     `json:"rate"`
	SourceTier       string     `json:"source_tier"`
	ProviderID       *uuid.UUID `json:"provider_id,omitempty"`
	ProviderCode     string     `json:"provider_code,omitempty"`
	ManualRateID     *uuid.UUID `json:"manual_rate_id,omitempty"`
	ParentSnapshotID *uuid.UUID `json:"parent_snapshot_id,omitempty"`
	ObservedAt       time.Time  `json:"observed_at"`
	SelectedAt       time.Time  `json:"selected_at"`
	ExpiresAt        time.Time  `json:"expires_at"`
	StaleAfter       time.Time  `json:"stale_after"`
	ConsensusCount   int        `json:"consensus_count"`
	Decision         string     `json:"decision"`
	CreatedAt        time.Time  `json:"created_at"`
}

func toAdminCurrencyDTO(item model.CurrencyDefinition) adminCurrencyDTO {
	return adminCurrencyDTO{
		ID: item.ID, Code: item.Code, NumericCode: item.NumericCode, Name: item.Name, Symbol: item.Symbol,
		MinorUnit: item.MinorUnit, Enabled: item.Enabled, Settlement: item.Settlement,
		DisplaySort: item.DisplaySort, CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt,
	}
}

func toAdminFXProviderDTO(item model.FXProviderConfig) adminFXProviderDTO {
	return adminFXProviderDTO{
		ID: item.ID, Code: item.Code, Name: item.Name, Driver: item.Driver, ProviderKey: item.ProviderKey,
		BaseURL: item.BaseURL, Priority: item.Priority, Enabled: item.Enabled, TimeoutSeconds: item.TimeoutSeconds,
		FailureCount: item.FailureCount, LastSuccessAt: item.LastSuccessAt, LastFailureAt: item.LastFailureAt,
		HasError: strings.TrimSpace(item.LastError) != "", CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt,
	}
}

func toAdminFXManualRateDTO(item model.FXManualRate) adminFXManualRateDTO {
	return adminFXManualRateDTO{
		ID: item.ID, BaseCode: item.BaseCode, QuoteCode: item.QuoteCode, Rate: item.Rate,
		Enabled: item.Enabled, ValidFrom: item.ValidFrom, ValidTo: item.ValidTo, Reason: item.Reason,
		UpdatedBy: item.UpdatedBy, CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt,
	}
}

func toAdminFXSnapshotDTO(item model.FXRateSnapshot, providerCode string) adminFXSnapshotDTO {
	return adminFXSnapshotDTO{
		ID: item.ID, BaseCode: item.BaseCode, QuoteCode: item.QuoteCode, Rate: item.Rate,
		SourceTier: item.SourceTier, ProviderID: item.ProviderID, ProviderCode: providerCode,
		ManualRateID: item.ManualRateID, ParentSnapshotID: item.ParentSnapshotID,
		ObservedAt: item.ObservedAt, SelectedAt: item.SelectedAt, ExpiresAt: item.ExpiresAt,
		StaleAfter: item.StaleAfter, ConsensusCount: item.ConsensusCount,
		Decision: item.Decision, CreatedAt: item.CreatedAt,
	}
}

func normalizeISOCode(value string) (string, error) {
	code := strings.ToUpper(strings.TrimSpace(value))
	if !isoCurrencyCodePattern.MatchString(code) {
		return "", errors.New("currency code is invalid")
	}
	return code, nil
}

func normalizeExactFXDecimal(value string) (string, error) {
	value = strings.TrimSpace(value)
	if !exactFXDecimalPattern.MatchString(value) {
		return "", errors.New("exchange rate must be an exact decimal")
	}
	if _, err := currency.ParseRate(value); err != nil {
		return "", err
	}
	if strings.Contains(value, ".") {
		value = strings.TrimRight(strings.TrimRight(value, "0"), ".")
	}
	return value, nil
}

func currentStoreCurrency(db *gorm.DB) (string, error) {
	var item model.Setting
	if err := db.Where("key = ?", "store_currency").First(&item).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "CNY", nil
		}
		return "", err
	}
	return normalizeISOCode(item.Value)
}

func (h Handler) AdminCurrencies(c *gin.Context) {
	var items []model.CurrencyDefinition
	if err := h.DB.Order("display_sort DESC, code ASC").Find(&items).Error; err != nil {
		response.Error(c, http.StatusInternalServerError, 500120, "error.currency_list_fetch_failed")
		return
	}
	storeCurrency, err := currentStoreCurrency(h.DB)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, 500121, "error.store_currency_fetch_failed")
		return
	}
	dtos := make([]adminCurrencyDTO, 0, len(items))
	for _, item := range items {
		dtos = append(dtos, toAdminCurrencyDTO(item))
	}
	response.OK(c, gin.H{"items": dtos, "store_currency": storeCurrency})
}

type adminCurrencyPatchRequest struct {
	Enabled     *bool `json:"enabled"`
	DisplaySort *int  `json:"display_sort"`
	MinorUnit   *int  `json:"minor_unit"`
}

func (request adminCurrencyPatchRequest) validate() error {
	if request.Enabled == nil && request.DisplaySort == nil && request.MinorUnit == nil {
		return errors.New("currency update is empty")
	}
	if request.DisplaySort != nil && (*request.DisplaySort < -100_000 || *request.DisplaySort > 100_000) {
		return errors.New("currency display sort is invalid")
	}
	if request.MinorUnit != nil && (*request.MinorUnit < 0 || *request.MinorUnit > 6) {
		return errors.New("currency minor unit is invalid")
	}
	return nil
}

func currencyHasFinancialRecords(db *gorm.DB, code string) (bool, error) {
	checks := []struct {
		model any
		where string
	}{
		{&model.Order{}, "currency = ?"},
		{&model.OrderItem{}, "currency = ? OR upstream_currency = ?"},
		{&model.WalletAccount{}, "currency = ?"},
		{&model.PaymentIntent{}, "currency = ? OR order_currency = ?"},
		{&model.RechargeOrder{}, "currency = ? OR credit_currency = ?"},
		{&model.Refund{}, "currency = ? OR order_currency = ?"},
		{&model.GiftCardBatch{}, "currency = ?"},
		{&model.GiftCard{}, "currency = ?"},
		{&model.ProcurementOrder{}, "cost_currency = ? OR upstream_currency = ?"},
		{&model.RechargeTransaction{}, "currency = ?"},
		{&model.ResellerWithdrawal{}, "currency = ?"},
		{&model.ReconciliationBatch{}, "currency = ?"},
		{&model.ReconciliationItem{}, "currency = ?"},
		{&model.AffiliateBalance{}, "currency = ?"},
		{&model.AffiliateCommission{}, "currency = ?"},
		{&model.AffiliateWithdrawal{}, "currency = ?"},
		{&model.Promotion{}, "currency = ?"},
		{&model.Coupon{}, "currency = ?"},
		{&model.ResellerProductRule{}, "currency = ?"},
		{&model.ResellerCreditPolicy{}, "currency = ?"},
		{&model.ResellerCreditEvent{}, "currency = ?"},
		{&model.MemberLevel{}, "currency = ?"},
		{&model.ProductMapping{}, "fixed_price_currency = ?"},
	}
	for _, check := range checks {
		var count int64
		arguments := []any{code}
		if strings.Count(check.where, "?") == 2 {
			arguments = append(arguments, code)
		}
		if err := db.Unscoped().Model(check.model).Where(check.where, arguments...).Limit(1).Count(&count).Error; err != nil {
			return false, err
		}
		if count > 0 {
			return true, nil
		}
	}
	return false, nil
}

func (h Handler) UpdateAdminCurrency(c *gin.Context) {
	code, err := normalizeISOCode(c.Param("code"))
	if err != nil {
		response.Error(c, http.StatusUnprocessableEntity, 422120, "error.currency_code_invalid")
		return
	}
	var request adminCurrencyPatchRequest
	if decodeStrictJSON(c, &request) != nil || request.validate() != nil {
		response.Error(c, http.StatusUnprocessableEntity, 422121, "error.currency_update_invalid")
		return
	}
	reason, ok := requireAdminChangeReason(c, "修改币种配置")
	if !ok {
		return
	}
	var item model.CurrencyDefinition
	if err := h.DB.Where("code = ?", code).First(&item).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			response.Error(c, http.StatusNotFound, 404120, "error.currency_not_found")
			return
		}
		response.Error(c, http.StatusInternalServerError, 500122, "error.currency_fetch_failed")
		return
	}
	if request.Enabled != nil && !*request.Enabled {
		storeCurrency, err := currentStoreCurrency(h.DB)
		if err != nil {
			response.Error(c, http.StatusInternalServerError, 500121, "error.store_currency_fetch_failed")
			return
		}
		if storeCurrency == code {
			response.Error(c, http.StatusConflict, 409120, "error.store_currency_disable_forbidden")
			return
		}
	}
	if request.MinorUnit != nil && *request.MinorUnit != item.MinorUnit {
		used, err := currencyHasFinancialRecords(h.DB, code)
		if err != nil {
			response.Error(c, http.StatusInternalServerError, 500123, "error.currency_usage_check_failed")
			return
		}
		if used {
			response.Error(c, http.StatusConflict, 409121, "error.currency_minor_unit_immutable")
			return
		}
	}
	updates := map[string]any{}
	if request.Enabled != nil {
		updates["enabled"] = *request.Enabled
	}
	if request.DisplaySort != nil {
		updates["display_sort"] = *request.DisplaySort
	}
	if request.MinorUnit != nil {
		updates["minor_unit"] = *request.MinorUnit
	}
	if err := h.DB.Model(&item).Updates(updates).Error; err != nil {
		response.Error(c, http.StatusInternalServerError, 500124, "error.currency_update_failed")
		return
	}
	if err := h.DB.Where("id = ?", item.ID).First(&item).Error; err != nil {
		response.Error(c, http.StatusInternalServerError, 500122, "error.currency_fetch_failed")
		return
	}
	h.audit(c, "currency.update", "currency_definition", item.ID.String(), reason+"；code="+item.Code)
	response.OK(c, toAdminCurrencyDTO(item))
}

func (h Handler) AdminFXProviders(c *gin.Context) {
	var items []model.FXProviderConfig
	if err := h.DB.Order("priority ASC, code ASC").Find(&items).Error; err != nil {
		response.Error(c, http.StatusInternalServerError, 500125, "error.fx_provider_list_fetch_failed")
		return
	}
	dtos := make([]adminFXProviderDTO, 0, len(items))
	for _, item := range items {
		dtos = append(dtos, toAdminFXProviderDTO(item))
	}
	response.OK(c, gin.H{"items": dtos})
}

type adminFXProviderPatchRequest struct {
	Enabled        *bool `json:"enabled"`
	Priority       *int  `json:"priority"`
	TimeoutSeconds *int  `json:"timeout_seconds"`
}

func (request adminFXProviderPatchRequest) validate() error {
	if request.Enabled == nil && request.Priority == nil && request.TimeoutSeconds == nil {
		return errors.New("provider update is empty")
	}
	if request.Priority != nil && (*request.Priority < 0 || *request.Priority > 100_000) {
		return errors.New("provider priority is invalid")
	}
	if request.TimeoutSeconds != nil && (*request.TimeoutSeconds < 1 || *request.TimeoutSeconds > 30) {
		return errors.New("provider timeout is invalid")
	}
	return nil
}

func (h Handler) UpdateAdminFXProvider(c *gin.Context) {
	id, err := uuid.Parse(strings.TrimSpace(c.Param("id")))
	if err != nil {
		response.Error(c, http.StatusUnprocessableEntity, 422122, "error.fx_provider_id_invalid")
		return
	}
	var request adminFXProviderPatchRequest
	if decodeStrictJSON(c, &request) != nil || request.validate() != nil {
		response.Error(c, http.StatusUnprocessableEntity, 422123, "error.fx_provider_update_invalid")
		return
	}
	reason, ok := requireAdminChangeReason(c, "修改汇率源配置")
	if !ok {
		return
	}
	var item model.FXProviderConfig
	if err := h.DB.Where("id = ?", id).First(&item).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			response.Error(c, http.StatusNotFound, 404121, "error.fx_provider_not_found")
			return
		}
		response.Error(c, http.StatusInternalServerError, 500126, "error.fx_provider_fetch_failed")
		return
	}
	updates := map[string]any{}
	if request.Enabled != nil {
		updates["enabled"] = *request.Enabled
	}
	if request.Priority != nil {
		updates["priority"] = *request.Priority
	}
	if request.TimeoutSeconds != nil {
		updates["timeout_seconds"] = *request.TimeoutSeconds
	}
	if err := h.DB.Model(&item).Updates(updates).Error; err != nil {
		response.Error(c, http.StatusInternalServerError, 500127, "error.fx_provider_update_failed")
		return
	}
	if err := h.DB.Where("id = ?", id).First(&item).Error; err != nil {
		response.Error(c, http.StatusInternalServerError, 500126, "error.fx_provider_fetch_failed")
		return
	}
	h.audit(c, "fx.provider.update", "fx_provider_config", item.ID.String(), reason+"；code="+item.Code)
	response.OK(c, toAdminFXProviderDTO(item))
}

type optionalFXTime struct {
	Set   bool
	Value *time.Time
}

func (value *optionalFXTime) UnmarshalJSON(data []byte) error {
	value.Set = true
	if bytes.Equal(bytes.TrimSpace(data), []byte("null")) {
		value.Value = nil
		return nil
	}
	var parsed time.Time
	if err := json.Unmarshal(data, &parsed); err != nil {
		return err
	}
	parsed = parsed.UTC()
	value.Value = &parsed
	return nil
}

type adminFXManualRateCreateRequest struct {
	BaseCode  string     `json:"base_code"`
	QuoteCode string     `json:"quote_code"`
	Rate      string     `json:"rate"`
	Enabled   *bool      `json:"enabled"`
	ValidFrom time.Time  `json:"valid_from"`
	ValidTo   *time.Time `json:"valid_to"`
	Reason    string     `json:"reason"`
}

type adminFXManualRatePatchRequest struct {
	Rate      *string        `json:"rate"`
	Enabled   *bool          `json:"enabled"`
	ValidFrom *time.Time     `json:"valid_from"`
	ValidTo   optionalFXTime `json:"valid_to"`
	Reason    *string        `json:"reason"`
}

func normalizeManualRateValidity(validFrom time.Time, validTo *time.Time, now time.Time) (time.Time, *time.Time, error) {
	validFrom = validFrom.UTC()
	if validFrom.IsZero() || validFrom.Before(time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)) || validFrom.After(now.UTC().AddDate(1, 0, 0)) {
		return time.Time{}, nil, errors.New("manual rate start is invalid")
	}
	if validTo == nil {
		return validFrom, nil, nil
	}
	normalizedTo := validTo.UTC()
	if !normalizedTo.After(validFrom) || normalizedTo.After(validFrom.AddDate(10, 0, 0)) {
		return time.Time{}, nil, errors.New("manual rate end is invalid")
	}
	return validFrom, &normalizedTo, nil
}

func normalizeManualRateReason(value string) (string, error) {
	value = strings.TrimSpace(value)
	if length := len([]rune(value)); length < 4 || length > 500 {
		return "", errors.New("manual rate reason is invalid")
	}
	return value, nil
}

func validateCurrencyPair(db *gorm.DB, baseCode, quoteCode string) (string, string, error) {
	baseCode, err := normalizeISOCode(baseCode)
	if err != nil {
		return "", "", err
	}
	quoteCode, err = normalizeISOCode(quoteCode)
	if err != nil || baseCode == quoteCode {
		return "", "", errors.New("currency pair is invalid")
	}
	var count int64
	if err := db.Model(&model.CurrencyDefinition{}).Where("code IN ? AND enabled = ?", []string{baseCode, quoteCode}, true).Count(&count).Error; err != nil {
		return "", "", err
	}
	if count != 2 {
		return "", "", gorm.ErrRecordNotFound
	}
	return baseCode, quoteCode, nil
}

func (h Handler) AdminFXManualRates(c *gin.Context) {
	page, pageSize := pagination(c)
	query := h.DB.Model(&model.FXManualRate{})
	for parameter, column := range map[string]string{"base_code": "base_code", "quote_code": "quote_code"} {
		if raw := strings.TrimSpace(c.Query(parameter)); raw != "" {
			code, err := normalizeISOCode(raw)
			if err != nil {
				response.Error(c, http.StatusUnprocessableEntity, 422124, "error.fx_rate_filter_invalid")
				return
			}
			query = query.Where(column+" = ?", code)
		}
	}
	if enabled := strings.TrimSpace(c.Query("enabled")); enabled != "" {
		if enabled != "true" && enabled != "false" {
			response.Error(c, http.StatusUnprocessableEntity, 422124, "error.fx_rate_filter_invalid")
			return
		}
		query = query.Where("enabled = ?", enabled == "true")
	}
	if raw := strings.TrimSpace(c.Query("active_at")); raw != "" {
		activeAt, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			response.Error(c, http.StatusUnprocessableEntity, 422124, "error.fx_rate_filter_invalid")
			return
		}
		query = query.Where("valid_from <= ? AND (valid_to IS NULL OR valid_to > ?)", activeAt.UTC(), activeAt.UTC())
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		response.Error(c, http.StatusInternalServerError, 500128, "error.fx_manual_rate_list_fetch_failed")
		return
	}
	var items []model.FXManualRate
	if err := query.Order("valid_from DESC, created_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&items).Error; err != nil {
		response.Error(c, http.StatusInternalServerError, 500128, "error.fx_manual_rate_list_fetch_failed")
		return
	}
	dtos := make([]adminFXManualRateDTO, 0, len(items))
	for _, item := range items {
		dtos = append(dtos, toAdminFXManualRateDTO(item))
	}
	response.Page(c, dtos, total, page, pageSize)
}

func (h Handler) CreateAdminFXManualRate(c *gin.Context) {
	var request adminFXManualRateCreateRequest
	if decodeStrictJSON(c, &request) != nil {
		response.Error(c, http.StatusUnprocessableEntity, 422125, "error.fx_manual_rate_params_invalid")
		return
	}
	reason, ok := requireAdminChangeReason(c, "创建手工汇率")
	if !ok {
		return
	}
	adminID, ok := currentAccessAdminID(c)
	if !ok {
		return
	}
	baseCode, quoteCode, err := validateCurrencyPair(h.DB, request.BaseCode, request.QuoteCode)
	if err != nil {
		response.Error(c, http.StatusUnprocessableEntity, 422126, "error.fx_currency_pair_invalid")
		return
	}
	rate, err := normalizeExactFXDecimal(request.Rate)
	if err != nil {
		response.Error(c, http.StatusUnprocessableEntity, 422127, "error.fx_rate_decimal_invalid")
		return
	}
	domainReason, err := normalizeManualRateReason(request.Reason)
	if err != nil {
		response.Error(c, http.StatusUnprocessableEntity, 422128, "error.fx_manual_rate_reason_invalid")
		return
	}
	validFrom, validTo, err := normalizeManualRateValidity(request.ValidFrom, request.ValidTo, time.Now().UTC())
	if err != nil {
		response.Error(c, http.StatusUnprocessableEntity, 422129, "error.fx_manual_rate_validity_invalid")
		return
	}
	enabled := true
	if request.Enabled != nil {
		enabled = *request.Enabled
	}
	item := model.FXManualRate{BaseCode: baseCode, QuoteCode: quoteCode, Rate: rate, Enabled: enabled, ValidFrom: validFrom, ValidTo: validTo, Reason: domainReason, UpdatedBy: adminID}
	if err := createWithExplicitColumns(h.DB, &item, map[string]any{"enabled": enabled}); err != nil {
		response.Error(c, http.StatusConflict, 409122, "error.fx_manual_rate_create_failed")
		return
	}
	h.audit(c, "fx.manual-rate.create", "fx_manual_rate", item.ID.String(), reason+"；pair="+baseCode+"/"+quoteCode)
	response.Created(c, toAdminFXManualRateDTO(item))
}

func (h Handler) UpdateAdminFXManualRate(c *gin.Context) {
	id, err := uuid.Parse(strings.TrimSpace(c.Param("id")))
	if err != nil {
		response.Error(c, http.StatusUnprocessableEntity, 422130, "error.fx_manual_rate_id_invalid")
		return
	}
	var request adminFXManualRatePatchRequest
	if decodeStrictJSON(c, &request) != nil || (request.Rate == nil && request.Enabled == nil && request.ValidFrom == nil && !request.ValidTo.Set && request.Reason == nil) {
		response.Error(c, http.StatusUnprocessableEntity, 422131, "error.fx_manual_rate_update_invalid")
		return
	}
	reason, ok := requireAdminChangeReason(c, "修改手工汇率")
	if !ok {
		return
	}
	adminID, ok := currentAccessAdminID(c)
	if !ok {
		return
	}
	var item model.FXManualRate
	if err := h.DB.Where("id = ?", id).First(&item).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			response.Error(c, http.StatusNotFound, 404122, "error.fx_manual_rate_not_found")
			return
		}
		response.Error(c, http.StatusInternalServerError, 500129, "error.fx_manual_rate_fetch_failed")
		return
	}
	if request.Rate != nil {
		item.Rate, err = normalizeExactFXDecimal(*request.Rate)
		if err != nil {
			response.Error(c, http.StatusUnprocessableEntity, 422127, "error.fx_rate_decimal_invalid")
			return
		}
	}
	if request.Enabled != nil {
		item.Enabled = *request.Enabled
	}
	if request.ValidFrom != nil {
		item.ValidFrom = request.ValidFrom.UTC()
	}
	if request.ValidTo.Set {
		item.ValidTo = request.ValidTo.Value
	}
	if request.Reason == nil {
		response.Error(c, http.StatusUnprocessableEntity, 422128, "error.fx_manual_rate_reason_invalid")
		return
	}
	item.Reason, err = normalizeManualRateReason(*request.Reason)
	if err != nil {
		response.Error(c, http.StatusUnprocessableEntity, 422128, "error.fx_manual_rate_reason_invalid")
		return
	}
	item.ValidFrom, item.ValidTo, err = normalizeManualRateValidity(item.ValidFrom, item.ValidTo, time.Now().UTC())
	if err != nil {
		response.Error(c, http.StatusUnprocessableEntity, 422129, "error.fx_manual_rate_validity_invalid")
		return
	}
	updates := map[string]any{
		"rate": item.Rate, "enabled": item.Enabled, "valid_from": item.ValidFrom,
		"valid_to": item.ValidTo, "reason": item.Reason, "updated_by": adminID,
	}
	if err := h.DB.Model(&model.FXManualRate{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		response.Error(c, http.StatusConflict, 409123, "error.fx_manual_rate_update_failed")
		return
	}
	if err := h.DB.Where("id = ?", id).First(&item).Error; err != nil {
		response.Error(c, http.StatusInternalServerError, 500129, "error.fx_manual_rate_fetch_failed")
		return
	}
	h.audit(c, "fx.manual-rate.update", "fx_manual_rate", item.ID.String(), reason+"；pair="+item.BaseCode+"/"+item.QuoteCode)
	response.OK(c, toAdminFXManualRateDTO(item))
}

func parseFXSnapshotTimeFilter(c *gin.Context, parameter string) (*time.Time, error) {
	raw := strings.TrimSpace(c.Query(parameter))
	if raw == "" {
		return nil, nil
	}
	parsed, err := time.Parse(time.RFC3339, raw)
	if err != nil {
		return nil, err
	}
	parsed = parsed.UTC()
	return &parsed, nil
}

func (h Handler) AdminFXSnapshots(c *gin.Context) {
	page, pageSize := pagination(c)
	query := h.DB.Model(&model.FXRateSnapshot{})
	for parameter, column := range map[string]string{"base_code": "base_code", "quote_code": "quote_code"} {
		if raw := strings.TrimSpace(c.Query(parameter)); raw != "" {
			code, err := normalizeISOCode(raw)
			if err != nil {
				response.Error(c, http.StatusUnprocessableEntity, 422132, "error.fx_snapshot_filter_invalid")
				return
			}
			query = query.Where(column+" = ?", code)
		}
	}
	if sourceTier := strings.ToLower(strings.TrimSpace(c.Query("source_tier"))); sourceTier != "" {
		if sourceTier != "live" && sourceTier != "manual" && sourceTier != "cached" && sourceTier != "system" {
			response.Error(c, http.StatusUnprocessableEntity, 422132, "error.fx_snapshot_filter_invalid")
			return
		}
		query = query.Where("source_tier = ?", sourceTier)
	}
	if raw := strings.TrimSpace(c.Query("provider_id")); raw != "" {
		providerID, err := uuid.Parse(raw)
		if err != nil {
			response.Error(c, http.StatusUnprocessableEntity, 422132, "error.fx_snapshot_filter_invalid")
			return
		}
		query = query.Where("provider_id = ?", providerID)
	}
	selectedFrom, fromErr := parseFXSnapshotTimeFilter(c, "selected_from")
	selectedTo, toErr := parseFXSnapshotTimeFilter(c, "selected_to")
	if fromErr != nil || toErr != nil || (selectedFrom != nil && selectedTo != nil && !selectedTo.After(*selectedFrom)) {
		response.Error(c, http.StatusUnprocessableEntity, 422132, "error.fx_snapshot_filter_invalid")
		return
	}
	if selectedFrom != nil {
		query = query.Where("selected_at >= ?", *selectedFrom)
	}
	if selectedTo != nil {
		query = query.Where("selected_at < ?", *selectedTo)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		response.Error(c, http.StatusInternalServerError, 500130, "error.fx_snapshot_list_fetch_failed")
		return
	}
	var items []model.FXRateSnapshot
	if err := query.Order("selected_at DESC, id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&items).Error; err != nil {
		response.Error(c, http.StatusInternalServerError, 500130, "error.fx_snapshot_list_fetch_failed")
		return
	}
	providerIDs := make([]uuid.UUID, 0, len(items))
	for _, item := range items {
		if item.ProviderID != nil {
			providerIDs = append(providerIDs, *item.ProviderID)
		}
	}
	providerCodes := map[uuid.UUID]string{}
	if len(providerIDs) > 0 {
		var providers []model.FXProviderConfig
		if err := h.DB.Select("id", "code").Where("id IN ?", providerIDs).Find(&providers).Error; err != nil {
			response.Error(c, http.StatusInternalServerError, 500130, "error.fx_snapshot_list_fetch_failed")
			return
		}
		for _, provider := range providers {
			providerCodes[provider.ID] = provider.Code
		}
	}
	dtos := make([]adminFXSnapshotDTO, 0, len(items))
	for _, item := range items {
		code := ""
		if item.ProviderID != nil {
			code = providerCodes[*item.ProviderID]
		}
		dtos = append(dtos, toAdminFXSnapshotDTO(item, code))
	}
	response.Page(c, dtos, total, page, pageSize)
}

type adminFXRefreshRequest struct {
	BaseCode  string `json:"base_code"`
	QuoteCode string `json:"quote_code"`
}

func (h Handler) RefreshAdminFXRate(c *gin.Context) {
	var request adminFXRefreshRequest
	if decodeStrictJSON(c, &request) != nil {
		response.Error(c, http.StatusUnprocessableEntity, 422133, "error.fx_refresh_params_invalid")
		return
	}
	reason, ok := requireAdminChangeReason(c, "手动刷新汇率")
	if !ok {
		return
	}
	baseCode, quoteCode, err := validateCurrencyPair(h.DB, request.BaseCode, request.QuoteCode)
	if err != nil {
		response.Error(c, http.StatusUnprocessableEntity, 422126, "error.fx_currency_pair_invalid")
		return
	}
	manager := currency.Manager{DB: h.DB, AllowPrivate: h.Cfg.Env != "production"}
	snapshot, err := manager.Resolve(c.Request.Context(), baseCode, quoteCode)
	if err != nil {
		if errors.Is(err, currency.ErrRateUnavailable) {
			response.Error(c, http.StatusServiceUnavailable, 503120, "error.fx_rate_unavailable")
			return
		}
		response.Error(c, http.StatusInternalServerError, 500131, "error.fx_refresh_failed")
		return
	}
	providerCode := ""
	if snapshot.ProviderID != nil {
		var provider model.FXProviderConfig
		if err := h.DB.Select("id", "code").Where("id = ?", *snapshot.ProviderID).First(&provider).Error; err == nil {
			providerCode = provider.Code
		}
	}
	h.audit(c, "fx.rate.refresh", "fx_rate_snapshot", snapshot.ID.String(), reason+"；pair="+baseCode+"/"+quoteCode+"；tier="+snapshot.SourceTier)
	response.OK(c, toAdminFXSnapshotDTO(snapshot, providerCode))
}
