package handler

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	fx "linlinqi/api/internal/currency"
	"linlinqi/api/internal/model"
	"linlinqi/api/internal/payment"
	"linlinqi/api/internal/queue"
	"linlinqi/api/internal/security"
	"linlinqi/api/internal/service"
	"linlinqi/api/pkg/response"
)

type createPaymentRequest struct {
	Contact     string `json:"contact"`
	Email       string `json:"email"`
	ChannelCode string `json:"channel_code" binding:"required"`
	ReturnURL   string `json:"return_url"`
}

type paymentDriverConfig struct {
	BaseURL    string `json:"base_url"`
	MerchantID string `json:"merchant_id"`
	Secret     string `json:"secret"`
	// BEpusdt connector fields. BEpusdt signs with the API auth token instead
	// of a merchant id + long secret pair.
	APIToken  string `json:"api_token"`
	TradeType string `json:"trade_type"`
	Fiat      string `json:"fiat"`
	Timeout   int    `json:"timeout"`
}

var paymentChannelCodePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{1,49}$`)

const maximumPaymentCallbackAmount = int64(1_000_000_000_000)

type adminPaymentChannel struct {
	ID                  uuid.UUID `json:"id"`
	Name                string    `json:"name"`
	Code                string    `json:"code"`
	Provider            string    `json:"provider"`
	FeeRate             int       `json:"fee_rate"`
	Enabled             bool      `json:"enabled"`
	Sort                int       `json:"sort"`
	SupportedCurrencies []string  `json:"supported_currencies"`
	SettlementCurrency  string    `json:"settlement_currency"`
	Configured          bool      `json:"configured"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

func toAdminPaymentChannel(channel model.PaymentChannel) adminPaymentChannel {
	currencies, _ := paymentChannelCurrencies(channel)
	return adminPaymentChannel{
		ID: channel.ID, Name: channel.Name, Code: channel.Code, Provider: channel.Provider,
		FeeRate: channel.FeeRate, Enabled: channel.Enabled, Sort: channel.Sort, SupportedCurrencies: currencies, SettlementCurrency: channel.SettlementCurrency,
		Configured: channel.Provider == "sandbox" || (len(channel.ConfigCipher) > 0 && len(channel.ConfigNonce) > 0),
		CreatedAt:  channel.CreatedAt, UpdatedAt: channel.UpdatedAt,
	}
}

func normalizePaymentCurrencies(values []string) ([]string, error) {
	if len(values) == 0 || len(values) > 32 {
		return nil, errors.New("payment currencies are invalid")
	}
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, raw := range values {
		code := strings.ToUpper(strings.TrimSpace(raw))
		if !isoCurrencyCodePattern.MatchString(code) {
			return nil, errors.New("payment currency is invalid")
		}
		if _, exists := seen[code]; exists {
			continue
		}
		seen[code] = struct{}{}
		result = append(result, code)
	}
	if len(result) == 0 {
		return nil, errors.New("payment currencies are empty")
	}
	sort.Strings(result)
	return result, nil
}

func paymentChannelCurrencies(channel model.PaymentChannel) ([]string, error) {
	values := []string{"CNY"}
	if len(channel.SupportedCurrencies) > 0 && json.Unmarshal(channel.SupportedCurrencies, &values) != nil {
		return nil, errors.New("payment channel currencies are invalid")
	}
	return normalizePaymentCurrencies(values)
}

func paymentChannelSupportsCurrency(channel model.PaymentChannel, currencyCode string) bool {
	values, err := paymentChannelCurrencies(channel)
	if err != nil {
		return false
	}
	currencyCode = strings.ToUpper(strings.TrimSpace(currencyCode))
	return slices.Contains(values, currencyCode)
}

func paymentChannelSettlementCurrency(channel model.PaymentChannel) (string, error) {
	code := strings.ToUpper(strings.TrimSpace(channel.SettlementCurrency))
	if !isoCurrencyCodePattern.MatchString(code) || !paymentChannelSupportsCurrency(channel, code) {
		return "", errors.New("payment settlement currency is invalid")
	}
	return code, nil
}

func (h Handler) paymentCurrencyConversion(c *gin.Context, sourceCode, targetCode string) (service.CheckoutCurrencyConversion, error) {
	sourceCode = strings.ToUpper(strings.TrimSpace(sourceCode))
	targetCode = strings.ToUpper(strings.TrimSpace(targetCode))
	var source, target model.CurrencyDefinition
	if err := h.DB.Where("code = ? AND enabled = ?", sourceCode, true).First(&source).Error; err != nil {
		return service.CheckoutCurrencyConversion{}, err
	}
	if err := h.DB.Where("code = ? AND enabled = ?", targetCode, true).First(&target).Error; err != nil {
		return service.CheckoutCurrencyConversion{}, err
	}
	conversion := service.CheckoutCurrencyConversion{Source: source, Target: target}
	if source.Code != target.Code {
		manager := fx.Manager{DB: h.DB, AllowPrivate: h.Cfg.Env != "production"}
		snapshot, err := manager.Resolve(c.Request.Context(), source.Code, target.Code)
		if err != nil {
			return service.CheckoutCurrencyConversion{}, err
		}
		conversion.Snapshot = &snapshot
	}
	return conversion, nil
}

func (h Handler) PaymentChannels(c *gin.Context) {
	requestedCurrency, specified, err := optionalCurrencyQuery(c)
	if err != nil {
		response.Error(c, 422, 42266, "error.currency_code_invalid")
		return
	}
	if specified {
		_, currencyErr := resolveEnabledCurrencyDefinition(h.DB, requestedCurrency, false)
		if errors.Is(currencyErr, errCurrencySelectionInvalid) {
			response.Error(c, 422, 42266, "error.currency_code_invalid")
			return
		}
		if errors.Is(currencyErr, errCurrencySelectionUnavailable) {
			response.Error(c, 422, 42266, "error.currency_unavailable")
			return
		}
		if currencyErr != nil {
			response.Error(c, 500, 50063, "error.currency_definition_fetch_failed")
			return
		}
	}
	productIDs, err := parsePublicPaymentChannelProductIDs(c)
	if err != nil {
		response.Error(c, 422, 42266, "error.payment_channel_product_filter_invalid")
		return
	}
	channels, err := availablePaymentChannelsForProducts(h.DB, productIDs)
	if err != nil {
		response.Error(c, 500, 50063, "error.payment_channel_fetch_failed")
		return
	}
	items := make([]catalogPaymentChannelDTO, 0, len(channels))
	for _, channel := range channels {
		items = append(items, toCatalogPaymentChannelDTO(channel))
	}
	if optionalUserID(c) != nil {
		storeCurrency := requestedCurrency
		if !specified {
			storeCurrency, _ = service.StoreCurrency(h.DB)
		}
		if storeCurrency != "" {
			items = append(items, catalogPaymentChannelDTO{ID: uuid.Nil, Name: "账户余额", Code: "balance", Enabled: true, SupportedCurrencies: []string{strings.ToUpper(storeCurrency)}, SettlementCurrency: strings.ToUpper(storeCurrency)})
		}
	}
	response.OK(c, items)
}

func (h Handler) paymentDriver(channel model.PaymentChannel) (payment.Driver, error) {
	if channel.Code == "sandbox" && h.Cfg.Env != "production" {
		return payment.SandboxDriver{Secret: h.Cfg.OpenAPISecret}, nil
	}
	var cfg paymentDriverConfig
	plaintext, err := h.Vault.Decrypt(channel.ConfigCipher, channel.ConfigNonce, channel.ID[:])
	if err != nil {
		return nil, fmt.Errorf("decrypt payment channel configuration: %w", err)
	}
	if err := json.Unmarshal([]byte(plaintext), &cfg); err != nil {
		return nil, fmt.Errorf("decode payment channel configuration: %w", err)
	}
	switch channel.Provider {
	case "signed_http":
		if cfg.BaseURL == "" || cfg.MerchantID == "" || len(cfg.Secret) < 24 {
			return nil, fmt.Errorf("payment channel configuration is incomplete")
		}
		return payment.NewSignedHTTPDriver(channel.Code, cfg.BaseURL, cfg.MerchantID, cfg.Secret, h.Cfg.Env != "production"), nil
	case "bepusdt":
		if cfg.BaseURL == "" || cfg.APIToken == "" || !payment.ValidBepusdtTradeType(cfg.TradeType) || !payment.BepusdtFiats[strings.ToUpper(cfg.Fiat)] {
			return nil, fmt.Errorf("bepusdt payment channel configuration is incomplete")
		}
		minorUnit, unitErr := currencyMinorUnit(h.DB, strings.ToUpper(cfg.Fiat))
		if unitErr != nil {
			return nil, fmt.Errorf("resolve bepusdt settlement currency: %w", unitErr)
		}
		return payment.NewBepusdtDriver(payment.BepusdtConfig{
			Code: channel.Code, BaseURL: cfg.BaseURL, APIToken: cfg.APIToken,
			TradeType: cfg.TradeType, Fiat: strings.ToUpper(cfg.Fiat), MinorUnit: minorUnit,
			Timeout: cfg.Timeout, AllowPrivate: h.Cfg.Env != "production",
		}), nil
	default:
		return nil, fmt.Errorf("payment channel provider %q is not supported by the independent connector", channel.Provider)
	}
}

// currencyMinorUnit resolves the enabled currency definition's minor unit so
// gateway fiat amounts can be converted to and from integer minor units.
func currencyMinorUnit(db *gorm.DB, code string) (int, error) {
	var definition model.CurrencyDefinition
	if err := db.Where("code = ? AND enabled = ?", code, true).First(&definition).Error; err != nil {
		return 0, err
	}
	if definition.MinorUnit < 0 || definition.MinorUnit > 6 {
		return 0, fmt.Errorf("currency %s minor unit %d is invalid", code, definition.MinorUnit)
	}
	return definition.MinorUnit, nil
}

// validateBepusdtConfig validates a BEpusdt connector identity. The fiat
// currency must equal the channel settlement currency so callback amounts
// always reconcile with the stored payment intent.
func validateBepusdtConfig(c *gin.Context, cfg *paymentDriverConfig, settlementCurrency string, allowPrivate bool) bool {
	cfg.APIToken = strings.TrimSpace(cfg.APIToken)
	cfg.TradeType = strings.ToLower(strings.TrimSpace(cfg.TradeType))
	cfg.Fiat = strings.ToUpper(strings.TrimSpace(cfg.Fiat))
	if cfg.APIToken == "" || !payment.ValidBepusdtTradeType(cfg.TradeType) {
		response.Error(c, 422, 42266, "error.bepusdt_config_incomplete")
		return false
	}
	if !payment.BepusdtFiats[cfg.Fiat] || cfg.Fiat != strings.ToUpper(strings.TrimSpace(settlementCurrency)) {
		response.Error(c, 422, 42266, "error.bepusdt_fiat_mismatch")
		return false
	}
	if cfg.Timeout != 0 && (cfg.Timeout < 120 || cfg.Timeout > 86400) {
		response.Error(c, 422, 42266, "error.bepusdt_timeout_invalid")
		return false
	}
	parsed, err := security.ValidateOutboundURL(c.Request.Context(), strings.TrimSpace(cfg.BaseURL), allowPrivate)
	if err != nil || parsed.RawQuery != "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		response.Error(c, 422, 42266, "error.payment_service_url_invalid")
		return false
	}
	cfg.BaseURL = strings.TrimRight(parsed.String(), "/")
	return true
}

type paymentChannelRequest struct {
	Name                string              `json:"name" binding:"required"`
	Code                string              `json:"code" binding:"required"`
	Provider            string              `json:"provider" binding:"required"`
	FeeRate             int                 `json:"fee_rate"`
	Enabled             bool                `json:"enabled"`
	Sort                int                 `json:"sort"`
	SupportedCurrencies []string            `json:"supported_currencies"`
	SettlementCurrency  string              `json:"settlement_currency"`
	Config              paymentDriverConfig `json:"config"`
}

func (h Handler) CreatePaymentChannel(c *gin.Context) {
	var req paymentChannelRequest
	if decodeStrictJSON(c, &req) != nil || req.FeeRate < 0 || req.FeeRate > 10000 {
		response.Error(c, 422, 42262, "error.payment_channel_parameters_invalid")
		return
	}
	if len(req.SupportedCurrencies) == 0 {
		storeCurrency, err := service.StoreCurrency(h.DB)
		if err != nil {
			response.Error(c, 500, 50063, "error.store_currency_fetch_failed")
			return
		}
		req.SupportedCurrencies = []string{storeCurrency}
	}
	currencies, err := normalizePaymentCurrencies(req.SupportedCurrencies)
	if err != nil {
		response.Error(c, 422, 42262, "error.payment_channel_currencies_invalid")
		return
	}
	req.SettlementCurrency = strings.ToUpper(strings.TrimSpace(req.SettlementCurrency))
	if req.SettlementCurrency == "" {
		req.SettlementCurrency = currencies[0]
	}
	if !slices.Contains(currencies, req.SettlementCurrency) {
		response.Error(c, 422, 42262, "error.payment_channel_settlement_currency_invalid")
		return
	}
	var currencyCount int64
	if h.DB.Model(&model.CurrencyDefinition{}).Where("code IN ? AND enabled = ?", currencies, true).Count(&currencyCount).Error != nil || currencyCount != int64(len(currencies)) {
		response.Error(c, 422, 42262, "error.payment_channel_currencies_invalid")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	req.Code = strings.ToLower(strings.TrimSpace(req.Code))
	if len([]rune(req.Name)) < 2 || len([]rune(req.Name)) > 100 || !paymentChannelCodePattern.MatchString(req.Code) {
		response.Error(c, 422, 42262, "error.channel_name_and_code_invalid")
		return
	}
	reason, ok := requireAdminChangeReason(c, "创建支付渠道")
	if !ok {
		return
	}
	switch req.Provider {
	case "signed_http":
		req.Config.MerchantID = strings.TrimSpace(req.Config.MerchantID)
		if req.Config.MerchantID == "" || len(req.Config.Secret) < 24 {
			response.Error(c, 422, 42266, "error.merchant_and_secret_required")
			return
		}
		parsed, err := security.ValidateOutboundURL(c.Request.Context(), strings.TrimSpace(req.Config.BaseURL), h.Cfg.Env != "production")
		if err != nil || parsed.RawQuery != "" {
			response.Error(c, 422, 42266, "error.payment_service_url_invalid")
			return
		}
		req.Config.BaseURL = strings.TrimRight(parsed.String(), "/")
	case "bepusdt":
		if !validateBepusdtConfig(c, &req.Config, req.SettlementCurrency, h.Cfg.Env != "production") {
			return
		}
	case "sandbox":
		if h.Cfg.Env == "production" {
			response.Error(c, 422, 42263, "error.payment_connector_unsupported")
			return
		}
	default:
		response.Error(c, 422, 42263, "error.payment_connector_unsupported")
		return
	}
	currencyJSON, _ := json.Marshal(currencies)
	channel := model.PaymentChannel{Base: model.Base{ID: uuid.New()}, Name: req.Name, Code: req.Code, Provider: req.Provider, FeeRate: req.FeeRate, Enabled: req.Enabled, Sort: req.Sort, SupportedCurrencies: currencyJSON, SettlementCurrency: req.SettlementCurrency}
	if req.Provider == "signed_http" || req.Provider == "bepusdt" {
		// The secret is marshaled only as input to authenticated encryption; the
		// plaintext/config object is never returned or persisted.
		payload, _ := json.Marshal(req.Config) // #nosec G117 -- encrypted immediately below
		ciphertext, nonce, _, err := h.Vault.Encrypt(string(payload), channel.ID[:])
		if err != nil {
			response.Error(c, 500, 50062, "error.payment_config_encrypt_failed")
			return
		}
		channel.ConfigCipher, channel.ConfigNonce = ciphertext, nonce
	}
	if err := h.DB.Transaction(func(tx *gorm.DB) error {
		return createWithExplicitColumns(tx, &channel, map[string]any{"enabled": req.Enabled})
	}); err != nil {
		response.Error(c, 409, 40962, "error.payment_channel_slug_exists")
		return
	}
	channel.Enabled = req.Enabled
	h.audit(c, "payment-channel.create", "payment-channel", channel.ID.String(), reason+"；code="+channel.Code)
	response.Created(c, toAdminPaymentChannel(channel))
}

type updatePaymentChannelRequest struct {
	Name                *string              `json:"name"`
	FeeRate             *int                 `json:"fee_rate"`
	Enabled             *bool                `json:"enabled"`
	Sort                *int                 `json:"sort"`
	SupportedCurrencies *[]string            `json:"supported_currencies"`
	SettlementCurrency  *string              `json:"settlement_currency"`
	Config              *paymentDriverConfig `json:"config"`
}

// UpdatePaymentChannel changes operational fields without ever returning or
// requiring the existing signing secret. Supplying an empty config field keeps
// its current value, while a non-empty secret rotates it atomically.
func (h Handler) UpdatePaymentChannel(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, 422, 42264, "error.payment_channel_id_invalid")
		return
	}
	var req updatePaymentChannelRequest
	if decodeStrictJSON(c, &req) != nil || (req.Name == nil && req.FeeRate == nil && req.Enabled == nil && req.Sort == nil && req.SupportedCurrencies == nil && req.SettlementCurrency == nil && req.Config == nil) {
		response.Error(c, 422, 42262, "error.channel_change_required")
		return
	}
	reason, ok := requireAdminChangeReason(c, "修改支付渠道")
	if !ok {
		return
	}
	var channel model.PaymentChannel
	if err := h.DB.First(&channel, "id = ?", id).Error; err != nil {
		response.Error(c, 404, 40462, "error.payment_channel_not_found")
		return
	}
	updates := map[string]any{}
	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if len([]rune(name)) < 2 || len([]rune(name)) > 100 {
			response.Error(c, 422, 42262, "error.channel_name_length")
			return
		}
		updates["name"] = name
		channel.Name = name
	}
	if req.FeeRate != nil {
		if *req.FeeRate < 0 || *req.FeeRate > 10000 {
			response.Error(c, 422, 42262, "error.channel_fee_basis_points")
			return
		}
		updates["fee_rate"] = *req.FeeRate
		channel.FeeRate = *req.FeeRate
	}
	if req.Enabled != nil {
		updates["enabled"] = *req.Enabled
		channel.Enabled = *req.Enabled
	}
	if req.Sort != nil {
		updates["sort"] = *req.Sort
		channel.Sort = *req.Sort
	}
	if req.SupportedCurrencies != nil {
		currencies, currencyErr := normalizePaymentCurrencies(*req.SupportedCurrencies)
		if currencyErr != nil {
			response.Error(c, 422, 42262, "error.payment_channel_currencies_invalid")
			return
		}
		var count int64
		if h.DB.Model(&model.CurrencyDefinition{}).Where("code IN ? AND enabled = ?", currencies, true).Count(&count).Error != nil || count != int64(len(currencies)) {
			response.Error(c, 422, 42262, "error.payment_channel_currencies_invalid")
			return
		}
		payload, _ := json.Marshal(currencies)
		updates["supported_currencies"] = payload
		channel.SupportedCurrencies = payload
	}
	if req.SettlementCurrency != nil {
		settlement := strings.ToUpper(strings.TrimSpace(*req.SettlementCurrency))
		if !paymentChannelSupportsCurrency(channel, settlement) {
			response.Error(c, 422, 42262, "error.payment_channel_settlement_currency_invalid")
			return
		}
		updates["settlement_currency"] = settlement
		channel.SettlementCurrency = settlement
	} else if _, err := paymentChannelSettlementCurrency(channel); err != nil {
		response.Error(c, 422, 42262, "error.payment_channel_settlement_currency_invalid")
		return
	}
	configChanged := false
	if req.Config != nil {
		if channel.Provider != "signed_http" && channel.Provider != "bepusdt" {
			response.Error(c, 422, 42265, "error.channel_connector_config_fixed")
			return
		}
		var current paymentDriverConfig
		if len(channel.ConfigCipher) > 0 || len(channel.ConfigNonce) > 0 {
			plaintext, decryptErr := h.Vault.Decrypt(channel.ConfigCipher, channel.ConfigNonce, channel.ID[:])
			if decryptErr != nil || json.Unmarshal([]byte(plaintext), &current) != nil {
				response.Error(c, 409, 40963, "error.payment_config_unreadable_check_keys")
				return
			}
		}
		previous := current
		switch channel.Provider {
		case "signed_http":
			if value := strings.TrimSpace(req.Config.BaseURL); value != "" {
				current.BaseURL = value
			}
			if value := strings.TrimSpace(req.Config.MerchantID); value != "" {
				current.MerchantID = value
			}
			if req.Config.Secret != "" {
				current.Secret = req.Config.Secret
			}
			parsed, validateErr := security.ValidateOutboundURL(c.Request.Context(), current.BaseURL, h.Cfg.Env != "production")
			if validateErr != nil || parsed.RawQuery != "" || current.MerchantID == "" || len(current.Secret) < 24 {
				response.Error(c, 422, 42266, "error.payment_config_incomplete")
				return
			}
			current.BaseURL = strings.TrimRight(parsed.String(), "/")
		case "bepusdt":
			if value := strings.TrimSpace(req.Config.BaseURL); value != "" {
				current.BaseURL = value
			}
			if value := strings.TrimSpace(req.Config.APIToken); value != "" {
				current.APIToken = value
			}
			if value := strings.TrimSpace(req.Config.TradeType); value != "" {
				current.TradeType = value
			}
			if value := strings.TrimSpace(req.Config.Fiat); value != "" {
				current.Fiat = value
			}
			if req.Config.Timeout != 0 {
				current.Timeout = req.Config.Timeout
			}
			if !validateBepusdtConfig(c, &current, channel.SettlementCurrency, h.Cfg.Env != "production") {
				return
			}
		}
		configChanged = paymentDriverConfigChanged(previous, current)
		if configChanged {
			hasHistory, historyErr := paymentChannelHasFinancialHistory(h.DB, channel.ID)
			if historyErr != nil {
				response.Error(c, 409, 40962, "error.payment_channel_update_failed")
				return
			}
			if hasHistory {
				response.Error(c, 409, 40964, "error.payment_channel_config_has_history")
				return
			}
		}
		// The secret is marshaled only as input to authenticated encryption; the
		// admin response is the redacted adminPaymentChannel DTO.
		payload, _ := json.Marshal(current) // #nosec G117 -- encrypted immediately below
		ciphertext, nonce, _, encryptErr := h.Vault.Encrypt(string(payload), channel.ID[:])
		if encryptErr != nil {
			response.Error(c, 500, 50062, "error.payment_config_encrypt_failed")
			return
		}
		updates["config_cipher"] = ciphertext
		updates["config_nonce"] = nonce
		channel.ConfigCipher, channel.ConfigNonce = ciphertext, nonce
	}
	updateErr := h.DB.Transaction(func(tx *gorm.DB) error {
		if configChanged {
			if err := lockPaymentChannelIdentityTx(tx, channel.ID); err != nil {
				return err
			}
			hasHistory, err := paymentChannelHasFinancialHistory(tx, channel.ID)
			if err != nil {
				return err
			}
			if hasHistory {
				return errPaymentChannelConfigHasHistory
			}
		}
		return tx.Model(&model.PaymentChannel{}).Where("id = ?", channel.ID).Updates(updates).Error
	})
	if errors.Is(updateErr, errPaymentChannelConfigHasHistory) {
		response.Error(c, 409, 40964, "error.payment_channel_config_has_history")
		return
	}
	if updateErr != nil {
		response.Error(c, 409, 40962, "error.payment_channel_update_failed")
		return
	}
	h.audit(c, "payment-channel.update", "payment-channel", channel.ID.String(), reason)
	response.OK(c, toAdminPaymentChannel(channel))
}

func (h Handler) CreatePayment(c *gin.Context) {
	var req createPaymentRequest
	if c.ShouldBindJSON(&req) != nil {
		response.Error(c, 422, 42260, "error.payment_parameters_incomplete")
		return
	}
	contact := req.Contact
	if strings.TrimSpace(contact) == "" {
		contact = req.Email
	}
	if userID := optionalUserID(c); userID != nil {
		var account model.User
		if err := h.DB.Select("email").First(&account, "id = ? AND status = ?", *userID, "active").Error; err != nil {
			response.Error(c, 401, 40140, "error.invalid_login_state")
			return
		}
		contact = account.Email
	}
	contact, validContact := normalizeCheckoutContact(contact)
	if !validContact {
		response.Error(c, 422, 42260, "error.order_parameters_incomplete")
		return
	}
	var order model.Order
	if err := h.DB.Where("order_no = ? AND email = ?", c.Param("order_no"), contact).First(&order).Error; err != nil {
		response.Error(c, 404, 40460, "error.order_not_found")
		return
	}
	if order.UserID != nil {
		userID := optionalUserID(c)
		if userID == nil || *userID != *order.UserID {
			response.Error(c, 404, 40460, "error.order_not_found")
			return
		}
	} else {
		lookupToken := strings.TrimSpace(c.GetHeader("X-Order-Token"))
		if len(lookupToken) < 40 || len(lookupToken) > 100 || service.HashOrderLookupToken(lookupToken) != order.LookupTokenHash {
			response.Error(c, 404, 40460, "error.order_not_found")
			return
		}
	}
	if order.Status != "pending_payment" || order.PaymentStatus != "pending" {
		response.Error(c, 409, 40960, "error.order_not_payable")
		return
	}
	if order.PaymentMethod != req.ChannelCode {
		response.Error(c, 409, 40962, "error.payment_channel_must_match_order")
		return
	}
	if req.ChannelCode == "balance" {
		h.payOrderWithWallet(c, order)
		return
	}
	var channel model.PaymentChannel
	if err := h.DB.Where("code = ? AND enabled = ?", req.ChannelCode, true).First(&channel).Error; err != nil {
		response.Error(c, 422, 42261, "error.payment_channel_unavailable")
		return
	}
	settlementCurrency, settlementErr := paymentChannelSettlementCurrency(channel)
	if settlementErr != nil {
		response.Error(c, 422, 42261, "error.payment_channel_settlement_currency_invalid")
		return
	}
	conversion, conversionErr := h.paymentCurrencyConversion(c, order.Currency, settlementCurrency)
	if conversionErr != nil {
		response.Error(c, 503, 50361, "error.payment_settlement_rate_unavailable")
		return
	}
	settlementAmount, conversionErr := conversion.Amount(order.Total)
	if conversionErr != nil || settlementAmount < 1 {
		response.Error(c, 503, 50361, "error.payment_settlement_rate_unavailable")
		return
	}
	driver, err := h.paymentDriver(channel)
	if err != nil {
		response.Error(c, 503, 50360, "error.payment_channel_not_production_configured")
		return
	}
	var intent model.PaymentIntent
	var returnExisting, creationInProgress bool
	err = h.DB.Transaction(func(tx *gorm.DB) error {
		if err := lockCurrentPaymentChannelTx(tx, channel); err != nil {
			return err
		}
		var lockedOrder model.Order
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&lockedOrder, "id = ?", order.ID).Error; err != nil {
			return err
		}
		if lockedOrder.Status != "pending_payment" || lockedOrder.PaymentStatus != "pending" {
			return gorm.ErrInvalidTransaction
		}
		var orderProducts []uuid.UUID
		if err := tx.Model(&model.OrderItem{}).Where("order_id = ?", lockedOrder.ID).Distinct("product_id").Pluck("product_id", &orderProducts).Error; err != nil {
			return err
		}
		if err := service.EnsurePaymentChannelAllowedCurrency(tx, channel.ID, orderProducts, ""); err != nil {
			return err
		}
		now := time.Now()
		var active model.PaymentIntent
		findErr := tx.Where("order_id = ? AND status IN ?", lockedOrder.ID, []string{"creating", "pending"}).Order("created_at DESC").First(&active).Error
		if findErr == nil {
			if active.Status == "pending" && active.ExpiresAt.After(now) {
				intent, returnExisting = active, true
				return nil
			}
			if active.Status == "creating" && active.UpdatedAt.After(now.Add(-30*time.Second)) {
				intent, creationInProgress = active, true
				return nil
			}
			if active.Status == "creating" {
				intent = active
				return tx.Model(&active).Update("updated_at", now).Error
			}
			if err := tx.Model(&active).Update("status", "expired").Error; err != nil {
				return err
			}
		} else if findErr != gorm.ErrRecordNotFound {
			return findErr
		}
		intent = model.PaymentIntent{OrderID: lockedOrder.ID, IntentNo: fmt.Sprintf("LQI%d", time.Now().UnixNano()), ChannelID: channel.ID, Amount: settlementAmount, Currency: settlementCurrency, OrderAmount: lockedOrder.Total, OrderCurrency: lockedOrder.Currency, Status: "creating", ExpiresAt: time.Now().Add(15 * time.Minute)}
		if conversion.Snapshot != nil {
			id := conversion.Snapshot.ID
			intent.FXSnapshotID = &id
		}
		return tx.Create(&intent).Error
	})
	if errors.Is(err, service.ErrPaymentChannelNotAllowed) {
		response.Error(c, 422, 42265, "error.channel_not_applicable_to_items")
		return
	}
	if errors.Is(err, errPaymentChannelChanged) {
		response.Error(c, 409, 40964, "error.payment_channel_changed_retry")
		return
	}
	if err != nil {
		response.Error(c, 500, 50060, "error.payment_intent_create_failed")
		return
	}
	if returnExisting {
		response.OK(c, gin.H{"intent": intent})
		return
	}
	if creationInProgress {
		response.Error(c, 409, 40963, "error.payment_transaction_creating_retry")
		return
	}
	returnURL := strings.TrimRight(h.Cfg.UserAppURL, "/") + "/orders"
	// The provider must receive the immutable settlement snapshot stored on the
	// intent. The storefront order can be denominated in a different currency.
	result, err := driver.Create(c, payment.CreateRequest{IntentNo: intent.IntentNo, OrderNo: order.OrderNo, Amount: intent.Amount, Currency: intent.Currency, Subject: "LinLinQi 数字商品订单 " + order.OrderNo, NotifyURL: h.Cfg.AppURL + "/api/v1/payments/" + channel.Code + "/callback", ReturnURL: returnURL})
	if err != nil {
		// The provider may have accepted the idempotent intent even when its
		// response was lost. Keep it recoverable under the same IntentNo.
		h.DB.Model(&intent).Update("updated_at", time.Now())
		response.Error(c, 502, 50260, "error.payment_response_uncertain_retry_order")
		return
	}
	if len(result.ProviderTradeNo) == 0 || len(result.ProviderTradeNo) > 160 || !validCheckoutURL(result.CheckoutURL, h.Cfg.Env != "production") {
		h.DB.Model(&intent).Update("updated_at", time.Now())
		response.Error(c, 502, 50261, "error.payment_unsafe_incomplete_txn")
		return
	}
	if result.ExpiresAt.IsZero() {
		result.ExpiresAt = time.Now().Add(15 * time.Minute)
	}
	intent, err = finalizePaymentIntentCreation(h.DB, intent.ID, result)
	if err != nil {
		response.Error(c, 500, 50061, "error.payment_intent_save_failed")
		return
	}
	response.Created(c, gin.H{"intent": intent, "qr_code": result.QRCode})
}

// payOrderWithWallet settles a pending storefront order from the authenticated
// user's wallet. The order and wallet rows are locked in one transaction so a
// retry can never double-debit or deliver without a ledger entry.
func (h Handler) payOrderWithWallet(c *gin.Context, order model.Order) {
	userID := optionalUserID(c)
	if userID == nil || order.UserID == nil || *order.UserID != *userID {
		response.Error(c, 404, 40460, "error.order_not_found")
		return
	}
	now := time.Now()
	var intent model.PaymentIntent
	err := h.DB.Transaction(func(tx *gorm.DB) error {
		var locked model.Order
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&locked, "id = ?", order.ID).Error; err != nil {
			return err
		}
		if locked.Status != "pending_payment" || locked.PaymentStatus != "pending" {
			return gorm.ErrInvalidTransaction
		}
		var wallet model.WalletAccount
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("owner_type = ? AND owner_id = ? AND currency = ?", "user", *userID, locked.Currency).First(&wallet).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return service.ErrInsufficientBalance
			}
			return err
		}
		if _, err := service.ApplyWalletMutation(tx, service.WalletMutation{EntryNo: "LQW-STORE-" + locked.ID.String(), AccountID: wallet.ID, Amount: -locked.Total, Type: "order_payment", ReferenceType: "order", ReferenceID: &locked.ID, Description: "余额支付订单 " + locked.OrderNo}); err != nil {
			return err
		}
		settlement, err := service.EnsureWalletOrderPaymentAuditTx(tx, locked, now)
		if err != nil {
			return err
		}
		if err := tx.First(&intent, "id = ?", settlement.PaymentIntentID).Error; err != nil {
			return err
		}
		return service.FulfillOrderTx(tx, locked.ID)
	})
	if errors.Is(err, service.ErrInsufficientBalance) {
		response.Error(c, 402, 40201, "error.insufficient_balance")
		return
	}
	if err != nil {
		response.Error(c, 409, 40960, "error.order_not_payable")
		return
	}
	values := map[string]string{"order_no": order.OrderNo, "email": order.Email, "status": "paid", "amount": strconv.FormatInt(order.Total, 10), "currency": order.Currency, "channel": "balance", "summary": "订单已使用账户余额完成支付"}
	if order.UserID != nil {
		values["user_id"] = order.UserID.String()
	}
	_ = h.createOperationalNotifications(h.DB, "order.paid", order.ID.String(), values)
	_ = h.enqueueSupplierOrder(order.ID)
	_ = h.dispatchOrderDelivery(order.ID)
	response.OK(c, gin.H{"intent": intent, "wallet": gin.H{"charged": order.Total, "currency": order.Currency}})
}

func validCheckoutURL(raw string, allowHTTP bool) bool {
	raw = strings.TrimSpace(raw)
	parsed, err := url.Parse(raw)
	if err != nil || parsed.User != nil {
		return false
	}
	if allowHTTP && parsed.Host == "" && parsed.Scheme == "" {
		return strings.HasPrefix(parsed.Path, "/sandbox/pay/") && !strings.HasPrefix(raw, "//") && parsed.RawQuery == "" && parsed.Fragment == ""
	}
	if parsed.Hostname() == "" {
		return false
	}
	return parsed.Scheme == "https" || (allowHTTP && parsed.Scheme == "http")
}

// finalizePaymentIntentCreation serializes the provider response with callback
// processing. A fast provider callback can move the intent to a terminal state
// before Create returns; this function must never regress that state to pending.
func finalizePaymentIntentCreation(db *gorm.DB, intentID uuid.UUID, result payment.CreateResult) (model.PaymentIntent, error) {
	var intent model.PaymentIntent
	err := db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&intent, "id = ?", intentID).Error; err != nil {
			return err
		}
		if intent.ProviderTradeNo != "" && intent.ProviderTradeNo != result.ProviderTradeNo {
			return fmt.Errorf("provider trade number changed while finalizing payment intent")
		}
		switch intent.Status {
		case "creating":
			if err := tx.Model(&intent).Updates(map[string]any{
				"status": "pending", "provider_trade_no": result.ProviderTradeNo,
				"checkout_url": result.CheckoutURL, "expires_at": result.ExpiresAt,
			}).Error; err != nil {
				return err
			}
			return tx.First(&intent, "id = ?", intent.ID).Error
		case "pending", "succeeded", "requires_refund", "partially_refunded", "refunded":
			// A retry or callback already persisted the authoritative state.
			if intent.ProviderTradeNo == "" {
				return fmt.Errorf("payment intent %s has no provider trade number", intent.Status)
			}
			return nil
		default:
			return fmt.Errorf("payment intent cannot accept provider result from %s", intent.Status)
		}
	})
	return intent, err
}

func normalizedPaymentCallbackEventID(callback payment.CallbackResult) string {
	if callback.EventID != "" {
		return callback.EventID
	}
	digest := sha256.Sum256([]byte(callback.ProviderTradeNo + "\x00" + callback.IntentNo + "\x00" + callback.Status + "\x00" + callback.Currency + "\x00" + strconv.FormatInt(callback.Amount, 10)))
	return "derived-" + hex.EncodeToString(digest[:])
}

func paymentCallbackCanFulfill(order model.Order, intent model.PaymentIntent, callback payment.CallbackResult, paidAt time.Time) bool {
	return callback.Amount == intent.Amount && callback.Currency == intent.Currency &&
		intent.OrderAmount == order.Total && intent.OrderCurrency == order.Currency &&
		order.Status == "pending_payment" && order.PaymentStatus == "pending" &&
		(intent.Status == "pending" || intent.Status == "creating") && intent.ExpiresAt.After(paidAt)
}

func automaticPaymentRefundReason(order model.Order, intent model.PaymentIntent, callback payment.CallbackResult) string {
	if callback.Currency != intent.Currency {
		return "支付币种与订单应付币种不一致，系统自动原路退款"
	}
	if callback.Amount != intent.Amount {
		return "支付金额与订单应付金额不一致，系统自动原路退款"
	}
	if order.Status == "expired" {
		return "支付在订单超时释放库存后到账，系统自动原路退款"
	}
	return "订单已不可履约或支付意图已失效，系统自动原路退款"
}

func recordOrphanPaymentSecurityEvent(tx *gorm.DB, channel model.PaymentChannel, callback payment.CallbackResult, eventID, clientIP, userAgent string) error {
	// Serialize the check/create pair so provider retries create one durable
	// unresolved incident, even though SecurityEvent is a generic audit model.
	if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 20260809))", "linlinqi-orphan-payment:"+eventID).Error; err != nil {
		return err
	}
	var existing int64
	if err := tx.Model(&model.SecurityEvent{}).
		Where("event_type = ? AND details ->> 'event_id' = ?", "payment.orphan_received", eventID).
		Count(&existing).Error; err != nil {
		return err
	}
	if existing > 0 {
		return nil
	}
	details, err := json.Marshal(map[string]any{
		"event_id": eventID, "channel_id": channel.ID, "channel_code": channel.Code,
		"intent_no": callback.IntentNo, "provider_trade_no": callback.ProviderTradeNo,
		"amount": callback.Amount, "currency": callback.Currency, "status": callback.Status,
	})
	if err != nil {
		return err
	}
	return tx.Create(&model.SecurityEvent{
		EventType: "payment.orphan_received", Severity: "critical", Realm: "system",
		IP: clientIP, UserAgent: truncateSecurityValue(userAgent, 500), Details: string(details),
	}).Error
}

func (h Handler) PaymentCallback(c *gin.Context) {
	var channel model.PaymentChannel
	// Disabling a channel stops new checkouts, but callbacks for money already
	// accepted by that provider must remain processable.
	if err := h.DB.Where("code = ?", c.Param("channel")).First(&channel).Error; err != nil {
		response.Error(c, 404, 40461, "error.payment_channel_not_found")
		return
	}
	driver, err := h.paymentDriver(channel)
	if err != nil {
		response.Error(c, 503, 50360, "error.payment_channel_not_configured")
		return
	}
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, 2<<20))
	if err != nil {
		response.Error(c, 400, 40060, "error.invalid_callback_content")
		return
	}
	headers := map[string]string{"X-Timestamp": c.GetHeader("X-Timestamp"), "X-Signature": c.GetHeader("X-Signature")}
	callback, err := driver.VerifyCallback(headers, body)
	if err != nil {
		response.Error(c, http.StatusUnauthorized, 40160, "error.payment_callback_verification_failed")
		return
	}
	callback.Currency = strings.ToUpper(strings.TrimSpace(callback.Currency))
	if len(callback.ProviderTradeNo) == 0 || len(callback.ProviderTradeNo) > 160 || len(callback.EventID) > 140 || callback.Amount < 1 || callback.Amount > maximumPaymentCallbackAmount ||
		!isoCurrencyCodePattern.MatchString(callback.Currency) || strings.IndexFunc(callback.ProviderTradeNo, unicode.IsControl) >= 0 || strings.IndexFunc(callback.EventID, unicode.IsControl) >= 0 {
		response.Error(c, 422, 42264, "error.payment_callback_fields_invalid")
		return
	}
	if strings.TrimSpace(callback.IntentNo) == "" || len(callback.IntentNo) > 80 {
		response.Error(c, 422, 42267, "error.callback_missing_payment_intent")
		return
	}
	callback.EventID = normalizedPaymentCallbackEventID(callback)
	eventID := channel.ID.String() + ":" + callback.EventID
	var lateRefundID *uuid.UUID
	var rechargeRefundTransactionID *uuid.UUID
	var paidOrderID uuid.UUID
	var orphanPayment bool
	err = h.DB.Transaction(func(tx *gorm.DB) error {
		var locatedIntent model.PaymentIntent
		if err := tx.Select("id", "order_id").Where("intent_no = ? AND channel_id = ?", callback.IntentNo, channel.ID).First(&locatedIntent).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				rechargeOutcome, rechargeErr := h.processRechargeCallback(tx, channel, callback, eventID, body)
				if errors.Is(rechargeErr, gorm.ErrRecordNotFound) && callback.Status == "succeeded" {
					if recordErr := recordOrphanPaymentSecurityEvent(tx, channel, callback, eventID, c.ClientIP(), c.Request.UserAgent()); recordErr != nil {
						return recordErr
					}
					orphanPayment = true
					return nil
				}
				if rechargeOutcome.RefundTransactionID != nil {
					rechargeRefundTransactionID = rechargeOutcome.RefundTransactionID
				}
				return rechargeErr
			}
			return err
		}
		// Keep the global payment lock order consistent with CreatePayment:
		// order first, intent second. This avoids a callback/create deadlock.
		var order model.Order
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&order, "id = ?", locatedIntent.OrderID).Error; err != nil {
			return err
		}
		var intent model.PaymentIntent
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND channel_id = ?", locatedIntent.ID, channel.ID).First(&intent).Error; err != nil {
			return err
		}
		if intent.OrderID != order.ID {
			return fmt.Errorf("payment intent order changed during callback")
		}
		if intent.ProviderTradeNo != "" && intent.ProviderTradeNo != callback.ProviderTradeNo {
			return fmt.Errorf("provider trade number mismatch")
		}
		if intent.ProviderTradeNo == "" {
			// Keep the creating -> terminal transition atomic. The schema guard
			// deliberately rejects a creating intent with a provider identity,
			// because that intermediate state could otherwise be mistaken for an
			// unfinished checkout after money has already arrived.
			intent.ProviderTradeNo = callback.ProviderTradeNo
		}
		var duplicate int64
		if err := tx.Model(&model.PaymentTransaction{}).Where("provider_event_id = ?", eventID).Count(&duplicate).Error; err != nil {
			return err
		}
		if duplicate > 0 || intent.Status == "succeeded" || intent.Status == "requires_refund" || intent.Status == "partially_refunded" || intent.Status == "refunded" {
			return nil
		}
		if callback.Status != "succeeded" {
			return fmt.Errorf("payment callback state or amount mismatch")
		}
		now := time.Now()
		if callback.PaidAt != nil {
			now = *callback.PaidAt
		}
		eligible := paymentCallbackCanFulfill(order, intent, callback, now)
		transactionStatus := "succeeded"
		if !eligible {
			transactionStatus = "requires_refund"
		}
		transaction := model.PaymentTransaction{PaymentIntentID: intent.ID, Direction: "payment", ProviderEventID: eventID, Amount: callback.Amount, Currency: callback.Currency, Status: transactionStatus, RawPayload: string(body)}
		if err := tx.Create(&transaction).Error; err != nil {
			return err
		}
		if !eligible {
			reason := automaticPaymentRefundReason(order, intent, callback)
			refund := model.Refund{
				RefundNo:         fmt.Sprintf("LQR%s%s", time.Now().Format("20060102150405"), strings.ToUpper(uuid.NewString()[:8])),
				OrderID:          order.ID,
				PaymentIntentID:  &intent.ID,
				Amount:           callback.Amount,
				Currency:         callback.Currency,
				OrderAmount:      order.Total,
				OrderCurrency:    order.Currency,
				Reason:           reason,
				Status:           "pending",
				RequestedBy:      "system",
				ProviderRefundNo: "",
			}
			if err := tx.Create(&refund).Error; err != nil {
				return err
			}
			lateRefundID = &refund.ID
			return tx.Model(&intent).Updates(map[string]any{"status": "requires_refund", "provider_trade_no": callback.ProviderTradeNo, "succeeded_at": &now}).Error
		}
		if err := tx.Model(&intent).Updates(map[string]any{"status": "succeeded", "provider_trade_no": callback.ProviderTradeNo, "succeeded_at": &now}).Error; err != nil {
			return err
		}
		if err := service.FulfillOrderTx(tx, intent.OrderID); err != nil {
			return err
		}
		values := map[string]string{"order_no": order.OrderNo, "email": order.Email, "status": "paid", "amount": strconv.FormatInt(order.Total, 10), "currency": order.Currency, "channel": channel.Code, "summary": "支付回调验签成功，订单已付款"}
		if order.UserID != nil {
			values["user_id"] = order.UserID.String()
		}
		if err := h.createOperationalNotifications(tx, "order.paid", order.ID.String(), values); err != nil {
			return err
		}
		paidOrderID = intent.OrderID
		return nil
	})
	if err != nil {
		response.Error(c, 409, 40961, "error.payment_callback_apply_failed")
		return
	}
	if orphanPayment {
		response.OK(c, gin.H{"accepted": true, "manual_review": true})
		return
	}
	if rechargeRefundTransactionID != nil {
		client := queue.NewClient(h.Cfg, h.DB)
		_, _ = client.Enqueue(queue.TypeRechargeRefundProcess, map[string]string{"recharge_transaction_id": rechargeRefundTransactionID.String()})
		_ = client.Close()
	} else if lateRefundID != nil {
		client := queue.NewClient(h.Cfg, h.DB)
		_, _ = client.Enqueue(queue.TypeRefundProcess, map[string]string{"refund_id": lateRefundID.String()})
		_ = client.Close()
	} else if paidOrderID != uuid.Nil {
		_ = h.enqueueSupplierOrder(paidOrderID)
		_ = h.dispatchOrderDelivery(paidOrderID)
	}
	response.OK(c, gin.H{"accepted": true})
}
