package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"linlinqi/api/internal/i18n"
	"linlinqi/api/internal/model"
	"linlinqi/api/internal/queue"
	"linlinqi/api/internal/security"
	"linlinqi/api/internal/service"
	"linlinqi/api/internal/supply"
	"linlinqi/api/pkg/response"
)

var (
	supplierCodePattern              = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{1,59}$`)
	errSupplyPriceSourceConflict     = errors.New("automatic price source already exists")
	errSupplyParameterFieldInvalid   = errors.New("supplier parameter mapping references an unavailable checkout field")
	errSupplyCredentialsInvalid      = errors.New("supplier credentials are invalid")
	errSupplyConnectionInUse         = errors.New("supplier connection has in-flight procurements")
	errSupplyActivationRequiresProbe = errors.New("supplier activation requires a successful read-only probe")
	errSupplyDeleteRequiresDisabled  = errors.New("supplier must be disabled before deletion")
	errSupplyDeleteHasHistory        = errors.New("supplier has operational history")
)

type supplySupplierCreateRequest struct {
	Name                string            `json:"name"`
	Code                string            `json:"code"`
	BaseURL             string            `json:"base_url"`
	APIKey              string            `json:"api_key,omitempty"`
	APISecret           string            `json:"api_secret,omitempty"`
	Credentials         map[string]string `json:"credentials"`
	Protocol            string            `json:"protocol"`
	PriceCurrency       string            `json:"price_currency"`
	PriceMinorUnit      int               `json:"price_minor_unit"`
	BalanceCurrency     string            `json:"balance_currency"`
	CurrencyMode        string            `json:"currency_mode"`
	SyncIntervalMinutes int               `json:"sync_interval_minutes"`
}

type supplySupplierUpdateRequest struct {
	Name                *string            `json:"name"`
	Code                *string            `json:"code"`
	BaseURL             *string            `json:"base_url"`
	APIKey              *string            `json:"api_key,omitempty"`
	APISecret           *string            `json:"api_secret,omitempty"`
	Credentials         *map[string]string `json:"credentials"`
	Protocol            *string            `json:"protocol"`
	Status              *string            `json:"status"`
	PriceCurrency       *string            `json:"price_currency"`
	PriceMinorUnit      *int               `json:"price_minor_unit"`
	BalanceCurrency     *string            `json:"balance_currency"`
	CurrencyMode        *string            `json:"currency_mode"`
	SyncIntervalMinutes *int               `json:"sync_interval_minutes"`
}

type adminSupplySupplierItem struct {
	ID                    uuid.UUID  `json:"id"`
	Name                  string     `json:"name"`
	Code                  string     `json:"code"`
	BaseURL               string     `json:"base_url"`
	Protocol              string     `json:"protocol"`
	Status                string     `json:"status"`
	Balance               int64      `json:"balance"`
	BalanceCurrency       string     `json:"balance_currency"`
	PriceCurrency         string     `json:"price_currency"`
	PriceMinorUnit        int        `json:"price_minor_unit"`
	CurrencyMode          string     `json:"currency_mode"`
	BalanceSyncedAt       *time.Time `json:"balance_synced_at"`
	HealthStatus          string     `json:"health_status"`
	LastProbeAt           *time.Time `json:"last_probe_at,omitempty"`
	LastProbeLatencyMS    int        `json:"last_probe_latency_ms"`
	LastProbeError        string     `json:"last_probe_error"`
	CredentialsConfigured bool       `json:"credentials_configured"`
	CredentialFields      []string   `json:"credential_fields"`
	CallbackURL           string     `json:"callback_url,omitempty"`
	LastSyncAt            *time.Time `json:"last_sync_at"`
	SyncIntervalMinutes   int        `json:"sync_interval_minutes"`
	SyncPrice             bool       `json:"sync_price"`
	SyncStock             bool       `json:"sync_stock"`
	CreatedAt             time.Time  `json:"created_at"`
	UpdatedAt             time.Time  `json:"updated_at"`
}

func supplierProtocol(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" || value == "standard" {
		return "linlinqi-standard", nil
	}
	definition, exists := supply.Protocol(value)
	if !exists || definition.Availability == "unavailable" || definition.Availability == "reference_only" || !supply.Executable(value) {
		return "", errors.New("unsupported supplier protocol")
	}
	return value, nil
}

func (r *supplySupplierCreateRequest) normalizeAndValidate() error {
	r.Name = strings.TrimSpace(r.Name)
	r.Code = strings.ToLower(strings.TrimSpace(r.Code))
	r.BaseURL = strings.TrimRight(strings.TrimSpace(r.BaseURL), "/")
	protocol, err := supplierProtocol(r.Protocol)
	if err != nil {
		return err
	}
	r.Protocol = protocol
	r.PriceCurrency = strings.ToUpper(strings.TrimSpace(r.PriceCurrency))
	r.BalanceCurrency = strings.ToUpper(strings.TrimSpace(r.BalanceCurrency))
	if r.BalanceCurrency == "" {
		r.BalanceCurrency = r.PriceCurrency
	}
	r.CurrencyMode = strings.ToLower(strings.TrimSpace(r.CurrencyMode))
	if r.CurrencyMode == "" {
		r.CurrencyMode = "auto"
	}
	if r.SyncIntervalMinutes == 0 {
		r.SyncIntervalMinutes = 15
	}
	if r.APIKey != "" || r.APISecret != "" {
		r.Credentials = map[string]string{"api_key": r.APIKey, "api_secret": r.APISecret}
	}
	credentials, credentialErr := supply.ValidateCredentials(r.Protocol, r.Credentials)
	if credentialErr != nil {
		return credentialErr
	}
	r.Credentials = credentials
	r.APIKey, r.APISecret = "", ""
	if len([]rune(r.Name)) < 2 || len([]rune(r.Name)) > 120 || !supplierCodePattern.MatchString(r.Code) {
		return errors.New("invalid supplier identity")
	}
	if r.BaseURL == "" || r.SyncIntervalMinutes < 5 || r.SyncIntervalMinutes > 10080 || !isoCurrencyCodePattern.MatchString(r.PriceCurrency) || !isoCurrencyCodePattern.MatchString(r.BalanceCurrency) || r.PriceMinorUnit < 0 || r.PriceMinorUnit > 6 || (r.CurrencyMode != "auto" && r.CurrencyMode != "manual") {
		return errors.New("invalid supplier connection")
	}
	return nil
}

func (r *supplySupplierUpdateRequest) normalizeAndValidate() error {
	if r.Name == nil && r.Code == nil && r.BaseURL == nil && r.APIKey == nil && r.APISecret == nil && r.Credentials == nil && r.Protocol == nil && r.Status == nil && r.PriceCurrency == nil && r.PriceMinorUnit == nil && r.BalanceCurrency == nil && r.CurrencyMode == nil && r.SyncIntervalMinutes == nil {
		return errors.New("empty supplier update")
	}
	if r.Name != nil {
		value := strings.TrimSpace(*r.Name)
		if len([]rune(value)) < 2 || len([]rune(value)) > 120 {
			return errors.New("invalid supplier name")
		}
		r.Name = &value
	}
	if r.Code != nil {
		value := strings.ToLower(strings.TrimSpace(*r.Code))
		if !supplierCodePattern.MatchString(value) {
			return errors.New("invalid supplier code")
		}
		r.Code = &value
	}
	if r.BaseURL != nil {
		value := strings.TrimRight(strings.TrimSpace(*r.BaseURL), "/")
		if value == "" {
			return errors.New("invalid supplier URL")
		}
		r.BaseURL = &value
	}
	if r.Protocol != nil {
		value, err := supplierProtocol(*r.Protocol)
		if err != nil {
			return err
		}
		r.Protocol = &value
	}
	if r.Status != nil {
		value := strings.ToLower(strings.TrimSpace(*r.Status))
		if value != "active" && value != "disabled" {
			return errors.New("invalid supplier status")
		}
		r.Status = &value
	}
	if r.SyncIntervalMinutes != nil && (*r.SyncIntervalMinutes < 5 || *r.SyncIntervalMinutes > 10080) {
		return errors.New("invalid supplier sync interval")
	}
	for _, value := range []*string{r.PriceCurrency, r.BalanceCurrency} {
		if value != nil {
			normalized := strings.ToUpper(strings.TrimSpace(*value))
			if !isoCurrencyCodePattern.MatchString(normalized) {
				return errors.New("invalid supplier currency")
			}
			*value = normalized
		}
	}
	if r.PriceMinorUnit != nil && (*r.PriceMinorUnit < 0 || *r.PriceMinorUnit > 6) {
		return errors.New("invalid supplier currency minor unit")
	}
	if r.CurrencyMode != nil {
		value := strings.ToLower(strings.TrimSpace(*r.CurrencyMode))
		if value != "auto" && value != "manual" {
			return errors.New("invalid supplier currency mode")
		}
		r.CurrencyMode = &value
	}
	if (r.APIKey == nil) != (r.APISecret == nil) {
		return errors.New("supplier credentials must be rotated together")
	}
	if r.Credentials != nil && r.APIKey != nil {
		return errors.New("use either credentials or legacy key fields")
	}
	if r.APIKey != nil {
		legacy := map[string]string{"api_key": *r.APIKey, "api_secret": *r.APISecret}
		r.Credentials = &legacy
	}
	return nil
}

func validateSupplierBaseURL(ctx context.Context, raw, environment string) (string, error) {
	raw = strings.TrimRight(strings.TrimSpace(raw), "/")
	validationContext, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	parsed, err := security.ValidateOutboundURL(validationContext, raw, environment != "production")
	if err != nil {
		return "", err
	}
	if parsed.RawQuery != "" {
		return "", errors.New("supplier base URL must not contain a query string")
	}
	if parsed.Port() != "" {
		port, portErr := strconv.Atoi(parsed.Port())
		if portErr != nil || port < 1 || port > 65535 {
			return "", errors.New("supplier base URL contains an invalid port")
		}
	}
	return raw, nil
}

func toAdminSupplySupplier(item model.Supplier, callbackBaseURLs ...string) adminSupplySupplierItem {
	protocol, err := supplierProtocol(item.Protocol)
	if err != nil {
		protocol = item.Protocol
	}
	balanceCurrency := strings.ToUpper(strings.TrimSpace(item.BalanceCurrency))
	if balanceCurrency == "" {
		balanceCurrency = strings.ToUpper(strings.TrimSpace(item.PriceCurrency))
	}
	callbackURL := ""
	if len(callbackBaseURLs) > 0 && strings.TrimSpace(callbackBaseURLs[0]) != "" {
		callbackURL = strings.TrimRight(strings.TrimSpace(callbackBaseURLs[0]), "/") + supplierCallbackEndpoint(item.ID.String())
	}
	credentialFields := supply.CredentialKeys(protocol)
	return adminSupplySupplierItem{
		ID: item.ID, Name: item.Name, Code: item.Code, BaseURL: item.BaseURL,
		Protocol: protocol, Status: item.Status, Balance: item.Balance,
		BalanceCurrency: balanceCurrency, BalanceSyncedAt: item.BalanceSyncedAt,
		HealthStatus: item.HealthStatus, LastProbeAt: item.LastProbeAt,
		LastProbeLatencyMS: item.LastProbeLatencyMS, LastProbeError: item.LastProbeError,
		PriceCurrency: item.PriceCurrency, PriceMinorUnit: item.PriceMinorUnit, CurrencyMode: item.CurrencyMode,
		CredentialsConfigured: supplierCredentialsConfigured(item, protocol),
		CredentialFields:      credentialFields,
		CallbackURL:           callbackURL,
		LastSyncAt:            item.LastSyncAt, SyncIntervalMinutes: item.SyncIntervalMinutes,
		SyncPrice: true, SyncStock: true,
		CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt,
	}
}

func applyAdminSupplierSyncPolicy(item *adminSupplySupplierItem, policy model.SupplierSyncPolicy) {
	item.SyncPrice = policy.SyncPrice
	item.SyncStock = policy.SyncStock
}

func supplierCredentialsConfigured(item model.Supplier, protocol string) bool {
	definition, exists := supply.Protocol(protocol)
	if !exists {
		return false
	}
	if len(definition.CredentialFields) == 0 {
		return true
	}
	if len(item.CredentialsCipher) > 0 && len(item.CredentialsNonce) > 0 {
		return true
	}
	return protocol == "linlinqi-standard" && len(item.APIKeyCipher) > 0 && len(item.APIKeyNonce) > 0 && len(item.APISecretCipher) > 0 && len(item.APISecretNonce) > 0
}

func encryptSupplierCredentials(vault *security.Vault, supplierID uuid.UUID, protocol string, credentials map[string]string) (ciphertext, nonce []byte, err error) {
	validated, err := supply.ValidateCredentials(protocol, credentials)
	if err != nil {
		return nil, nil, err
	}
	payload, err := json.Marshal(validated)
	if err != nil {
		return nil, nil, err
	}
	ciphertext, nonce, _, err = vault.Encrypt(string(payload), append(supplierID[:], []byte("supplier-credentials-v1")...))
	return ciphertext, nonce, err
}

func (h Handler) AdminSupplyProtocols(c *gin.Context) {
	c.Header("Cache-Control", "private, max-age=300")
	response.OK(c, supply.Protocols())
}

func (h Handler) AdminSupplySuppliers(c *gin.Context) {
	page, pageSize := pagination(c)
	query := h.DB.Model(&model.Supplier{})
	if status := strings.ToLower(strings.TrimSpace(c.Query("status"))); status != "" {
		if status != "active" && status != "disabled" {
			response.Error(c, 422, 42501, "error.supplier_status_filter_invalid")
			return
		}
		query = query.Where("status = ?", status)
	}
	if keyword := strings.TrimSpace(c.Query("q")); keyword != "" {
		like := "%" + keyword + "%"
		query = query.Where("name ILIKE ? OR code ILIKE ? OR base_url ILIKE ?", like, like, like)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		response.Error(c, 500, 50501, "error.supplier_list_fetch_failed")
		return
	}
	var stored []model.Supplier
	if err := query.Order("created_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&stored).Error; err != nil {
		response.Error(c, 500, 50501, "error.supplier_list_fetch_failed")
		return
	}
	policies := make(map[uuid.UUID]model.SupplierSyncPolicy, len(stored))
	if len(stored) > 0 {
		supplierIDs := make([]uuid.UUID, 0, len(stored))
		for _, item := range stored {
			supplierIDs = append(supplierIDs, item.ID)
		}
		var storedPolicies []model.SupplierSyncPolicy
		if err := h.DB.Where("supplier_id IN ?", supplierIDs).Find(&storedPolicies).Error; err != nil {
			response.Error(c, 500, 50501, "error.supplier_list_fetch_failed")
			return
		}
		for _, policy := range storedPolicies {
			policies[policy.SupplierID] = policy
		}
	}
	items := make([]adminSupplySupplierItem, 0, len(stored))
	for _, item := range stored {
		dto := toAdminSupplySupplier(item, h.Cfg.SupplierCallbackURL)
		if policy, exists := policies[item.ID]; exists {
			applyAdminSupplierSyncPolicy(&dto, policy)
		}
		items = append(items, dto)
	}
	c.Header("Cache-Control", "no-store")
	response.Page(c, items, total, page, pageSize)
}

func (h Handler) CreateSupplySupplier(c *gin.Context) {
	reason, ok := requireAdminChangeReason(c, "创建供货商")
	if !ok {
		return
	}
	var req supplySupplierCreateRequest
	if decodeStrictJSON(c, &req) != nil || req.normalizeAndValidate() != nil {
		response.Error(c, 422, 42502, "error.supplier_fields_invalid")
		return
	}
	baseURL, err := validateSupplierBaseURL(c.Request.Context(), req.BaseURL, h.Cfg.Env)
	if err != nil {
		response.Error(c, 422, 42503, "error.supplier_connection_address_invalid")
		return
	}
	var priceDefinition, balanceDefinition model.CurrencyDefinition
	if h.DB.Where("code = ? AND enabled = ?", req.PriceCurrency, true).First(&priceDefinition).Error != nil || h.DB.Where("code = ? AND enabled = ?", req.BalanceCurrency, true).First(&balanceDefinition).Error != nil || priceDefinition.MinorUnit != req.PriceMinorUnit {
		response.Error(c, 422, 42502, "error.supplier_currency_invalid")
		return
	}
	item := model.Supplier{Base: model.Base{ID: uuid.New()}, Name: req.Name, Code: req.Code, BaseURL: baseURL, Protocol: req.Protocol, Status: "disabled", PriceCurrency: req.PriceCurrency, PriceMinorUnit: req.PriceMinorUnit, BalanceCurrency: req.BalanceCurrency, CurrencyMode: req.CurrencyMode, HealthStatus: "unknown", LastProbeError: "probe_required", SyncIntervalMinutes: req.SyncIntervalMinutes}
	credentialsCipher, credentialsNonce, err := encryptSupplierCredentials(h.Vault, item.ID, req.Protocol, req.Credentials)
	if err != nil {
		response.Error(c, 500, 50502, "error.supplier_credential_encrypt_failed")
		return
	}
	item.CredentialsCipher, item.CredentialsNonce = credentialsCipher, credentialsNonce
	if req.Protocol == "linlinqi-standard" {
		keyCipher, keyNonce, _, keyErr := h.Vault.Encrypt(req.Credentials["api_key"], append(item.ID[:], []byte("api-key")...))
		secretCipher, secretNonce, _, secretErr := h.Vault.Encrypt(req.Credentials["api_secret"], append(item.ID[:], []byte("api-secret")...))
		if keyErr != nil || secretErr != nil {
			response.Error(c, 500, 50502, "error.supplier_credential_encrypt_failed")
			return
		}
		item.APIKeyCipher, item.APIKeyNonce = keyCipher, keyNonce
		item.APISecretCipher, item.APISecretNonce = secretCipher, secretNonce
	}
	if err := h.DB.Create(&item).Error; err != nil {
		response.Error(c, 409, 40501, "error.supplier_code_conflict")
		return
	}
	h.audit(c, "supplier.create", "supplier", item.ID.String(), reason)
	c.Header("Cache-Control", "no-store")
	response.Created(c, toAdminSupplySupplier(item, h.Cfg.SupplierCallbackURL))
}

func (h Handler) UpdateSupplySupplier(c *gin.Context) {
	supplierID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, 422, 42504, "error.supplier_id_invalid")
		return
	}
	reason, ok := requireAdminChangeReason(c, "更新供货商")
	if !ok {
		return
	}
	var req supplySupplierUpdateRequest
	if decodeStrictJSON(c, &req) != nil || req.normalizeAndValidate() != nil {
		response.Error(c, 422, 42505, "error.supplier_update_fields_invalid")
		return
	}
	if req.BaseURL != nil {
		validated, validationErr := validateSupplierBaseURL(c.Request.Context(), *req.BaseURL, h.Cfg.Env)
		if validationErr != nil {
			response.Error(c, 422, 42503, "error.supplier_connection_address_invalid")
			return
		}
		req.BaseURL = &validated
	}
	updates := map[string]any{}
	if req.Name != nil {
		updates["name"] = *req.Name
	}
	if req.Code != nil {
		updates["code"] = *req.Code
	}
	if req.BaseURL != nil {
		updates["base_url"] = *req.BaseURL
	}
	if req.Protocol != nil {
		updates["protocol"] = *req.Protocol
	}
	if req.Status != nil {
		updates["status"] = *req.Status
	}
	if req.SyncIntervalMinutes != nil {
		updates["sync_interval_minutes"] = *req.SyncIntervalMinutes
	}
	if req.PriceCurrency != nil {
		updates["price_currency"] = *req.PriceCurrency
	}
	if req.PriceMinorUnit != nil {
		updates["price_minor_unit"] = *req.PriceMinorUnit
	}
	if req.BalanceCurrency != nil {
		updates["balance_currency"] = *req.BalanceCurrency
	}
	if req.CurrencyMode != nil {
		updates["currency_mode"] = *req.CurrencyMode
	}
	var item model.Supplier
	err = h.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&item, "id = ?", supplierID).Error; err != nil {
			return err
		}
		connectionMutation := mutatesSupplierConnection(item, req)
		if connectionMutation {
			var inFlight int64
			if err := tx.Model(&model.ProcurementOrder{}).
				Where("supplier_id = ? AND status IN ?", item.ID, []string{"creating", "dispatching", "processing", "retrying"}).
				Count(&inFlight).Error; err != nil {
				return err
			}
			if inFlight > 0 {
				return errSupplyConnectionInUse
			}
		}
		targetProtocol := item.Protocol
		if req.Protocol != nil {
			targetProtocol = *req.Protocol
		}
		if targetProtocol != item.Protocol && req.Credentials == nil {
			return errSupplyCredentialsInvalid
		}
		targetPriceCurrency, targetMinorUnit := item.PriceCurrency, item.PriceMinorUnit
		if req.PriceCurrency != nil {
			targetPriceCurrency = *req.PriceCurrency
		}
		if req.PriceMinorUnit != nil {
			targetMinorUnit = *req.PriceMinorUnit
		}
		var priceDefinition model.CurrencyDefinition
		if err := tx.Where("code = ? AND enabled = ?", targetPriceCurrency, true).First(&priceDefinition).Error; err != nil || priceDefinition.MinorUnit != targetMinorUnit {
			return errors.New("invalid supplier price currency")
		}
		if req.BalanceCurrency != nil {
			var definition model.CurrencyDefinition
			if err := tx.Where("code = ? AND enabled = ?", *req.BalanceCurrency, true).First(&definition).Error; err != nil {
				return errors.New("invalid supplier balance currency")
			}
		}
		if req.Credentials != nil {
			validated, validationErr := supply.ValidateCredentials(targetProtocol, *req.Credentials)
			if validationErr != nil {
				return errSupplyCredentialsInvalid
			}
			credentialsCipher, credentialsNonce, encryptionErr := encryptSupplierCredentials(h.Vault, supplierID, targetProtocol, validated)
			if encryptionErr != nil {
				return encryptionErr
			}
			updates["credentials_cipher"], updates["credentials_nonce"] = credentialsCipher, credentialsNonce
			if targetProtocol == "linlinqi-standard" {
				keyCipher, keyNonce, _, keyErr := h.Vault.Encrypt(validated["api_key"], append(supplierID[:], []byte("api-key")...))
				secretCipher, secretNonce, _, secretErr := h.Vault.Encrypt(validated["api_secret"], append(supplierID[:], []byte("api-secret")...))
				if keyErr != nil || secretErr != nil {
					return errors.New("encrypt legacy supplier credentials")
				}
				updates["api_key_cipher"], updates["api_key_nonce"] = keyCipher, keyNonce
				updates["api_secret_cipher"], updates["api_secret_nonce"] = secretCipher, secretNonce
			} else {
				updates["api_key_cipher"], updates["api_key_nonce"] = []byte(nil), []byte(nil)
				updates["api_secret_cipher"], updates["api_secret_nonce"] = []byte(nil), []byte(nil)
			}
		}
		if connectionMutation {
			// A connection identity change invalidates all earlier health evidence.
			// Persist the editable connection, but never leave it active until the
			// operator completes a fresh read-only probe.
			updates["status"] = "disabled"
			updates["health_status"] = "unknown"
			updates["last_probe_at"] = nil
			updates["last_probe_latency_ms"] = 0
			updates["last_probe_error"] = "probe_required"
		} else if req.Status != nil && *req.Status == "active" && !supplierCanActivate(item) {
			return errSupplyActivationRequiresProbe
		}
		if err := tx.Model(&item).Updates(updates).Error; err != nil {
			return err
		}
		// Scan into a zero-value destination. Reusing item would leave pointer
		// fields such as LastProbeAt populated when PostgreSQL returns NULL after
		// a connection mutation invalidates the previous probe evidence.
		var refreshed model.Supplier
		if err := tx.First(&refreshed, "id = ?", item.ID).Error; err != nil {
			return err
		}
		item = refreshed
		return nil
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		response.Error(c, 404, 40502, "error.supplier_not_found")
		return
	}
	if errors.Is(err, errSupplyCredentialsInvalid) {
		response.Error(c, 422, 42506, "error.supplier_credentials_invalid_for_protocol")
		return
	}
	if errors.Is(err, errSupplyConnectionInUse) {
		response.Error(c, 409, 42516, "error.supplier_connection_has_inflight_procurements")
		return
	}
	if errors.Is(err, errSupplyActivationRequiresProbe) {
		response.Error(c, 409, 42517, "error.supplier_probe_failed")
		return
	}
	if err != nil {
		response.Error(c, 409, 40503, "error.supplier_code_conflict_on_update")
		return
	}
	action := "supplier.update"
	if req.Credentials != nil {
		action = "supplier.credentials.rotate"
	} else if req.Status != nil {
		action = "supplier.status.update"
	}
	h.audit(c, action, "supplier", item.ID.String(), reason)
	c.Header("Cache-Control", "no-store")
	response.OK(c, toAdminSupplySupplier(item, h.Cfg.SupplierCallbackURL))
}

func supplierHasOperationalHistory(tx *gorm.DB, supplierID uuid.UUID) (bool, error) {
	models := []any{
		&model.ProductMapping{}, &model.SupplierProduct{}, &model.SupplierInventoryReservation{},
		&model.SupplierCategoryMapping{}, &model.ProcurementOrder{},
		&model.SupplierCategory{}, &model.SupplierCatalogProduct{}, &model.SupplierCatalogImportJob{},
		&model.SupplierSyncRun{},
	}
	for _, value := range models {
		var count int64
		if err := tx.Unscoped().Model(value).Where("supplier_id = ?", supplierID).Limit(1).Count(&count).Error; err != nil {
			return false, err
		}
		if count > 0 {
			return true, nil
		}
	}
	return false, nil
}

// DeleteSupplySupplier intentionally supports only unused, disabled
// connections. Once catalog mappings, sync evidence, or procurements exist,
// disabling preserves the immutable operational history and join integrity.
func (h Handler) DeleteSupplySupplier(c *gin.Context) {
	supplierID, err := uuid.Parse(c.Param("id"))
	if err != nil || supplierID == uuid.Nil {
		response.Error(c, 422, 42504, "error.supplier_id_invalid")
		return
	}
	reason, ok := requireAdminChangeReason(c, "删除未使用的供货商连接")
	if !ok {
		return
	}
	var item model.Supplier
	err = h.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&item, "id = ?", supplierID).Error; err != nil {
			return err
		}
		if item.Status != "disabled" {
			return errSupplyDeleteRequiresDisabled
		}
		hasHistory, err := supplierHasOperationalHistory(tx, item.ID)
		if err != nil {
			return err
		}
		if hasHistory {
			return errSupplyDeleteHasHistory
		}
		if err := tx.Where("supplier_id = ?", item.ID).Delete(&model.SupplierSyncPolicy{}).Error; err != nil {
			return err
		}
		return tx.Delete(&item).Error
	})
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
		response.Error(c, 404, 40502, "error.supplier_not_found")
		return
	case errors.Is(err, errSupplyDeleteRequiresDisabled):
		response.Error(c, 409, 40520, "error.supplier_delete_requires_disabled")
		return
	case errors.Is(err, errSupplyDeleteHasHistory):
		response.Error(c, 409, 40521, "error.supplier_delete_has_history")
		return
	case err != nil:
		response.Error(c, 500, 50501, "error.supplier_delete_failed")
		return
	}
	h.audit(c, "supplier.delete", "supplier", item.ID.String(), reason)
	response.OK(c, gin.H{"deleted": true})
}

func mutatesSupplierConnection(item model.Supplier, req supplySupplierUpdateRequest) bool {
	return req.Credentials != nil ||
		(req.BaseURL != nil && *req.BaseURL != item.BaseURL) ||
		(req.Protocol != nil && *req.Protocol != item.Protocol) ||
		(req.PriceCurrency != nil && *req.PriceCurrency != item.PriceCurrency) ||
		(req.PriceMinorUnit != nil && *req.PriceMinorUnit != item.PriceMinorUnit) ||
		(req.BalanceCurrency != nil && *req.BalanceCurrency != item.BalanceCurrency) ||
		(req.CurrencyMode != nil && *req.CurrencyMode != item.CurrencyMode)
}

func supplierCanActivate(item model.Supplier) bool {
	return item.LastProbeAt != nil && item.HealthStatus == "healthy" && supplierCredentialsConfigured(item, item.Protocol)
}

func isDuplicateSupplyTask(err error) bool {
	return errors.Is(err, asynq.ErrDuplicateTask) || errors.Is(err, asynq.ErrTaskIDConflict)
}

func (h Handler) EnqueueSupplySupplierSync(c *gin.Context) {
	supplierID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, 422, 42504, "error.supplier_id_invalid")
		return
	}
	reason, ok := requireAdminChangeReason(c, "手动同步供货商")
	if !ok {
		return
	}
	var item model.Supplier
	if err := h.DB.First(&item, "id = ?", supplierID).Error; err != nil {
		response.Error(c, 404, 40502, "error.supplier_not_found")
		return
	}
	if item.Status != "active" || !supplierCredentialsConfigured(item, item.Protocol) {
		response.Error(c, 409, 40504, "error.sync_requires_enabled_supplier")
		return
	}
	client := queue.NewClient(h.Cfg, h.DB)
	defer client.Close()
	_, enqueueErr := client.Enqueue(queue.TypeSupplierSync, map[string]string{"supplier_id": item.ID.String(), "trigger": "manual"}, asynq.Queue("default"), asynq.Unique(4*time.Minute))
	state := "queued"
	if isDuplicateSupplyTask(enqueueErr) {
		state = "already_queued"
	} else if enqueueErr != nil {
		response.Error(c, 503, 50392, "error.sync_queue_unavailable_retry")
		return
	}
	h.audit(c, "supplier.sync.enqueue", "supplier", item.ID.String(), fmt.Sprintf("%s；queue_state=%s", reason, state))
	c.JSON(http.StatusAccepted, response.Envelope{Code: 0, Message: "accepted", Data: gin.H{"status": state}})
}

func (h Handler) EnqueueAllSupplySupplierSync(c *gin.Context) {
	reason, ok := requireAdminChangeReason(c, "批量同步全部启用供货商")
	if !ok {
		return
	}
	var suppliers []model.Supplier
	if err := h.DB.Where("status = ?", "active").Order("created_at ASC").Find(&suppliers).Error; err != nil {
		response.Error(c, 500, 50501, "error.supplier_list_fetch_failed")
		return
	}
	client := queue.NewClient(h.Cfg, h.DB)
	defer client.Close()
	queued, alreadyQueued, skipped, failed := 0, 0, 0, 0
	for _, item := range suppliers {
		if !supplierCredentialsConfigured(item, item.Protocol) {
			skipped++
			continue
		}
		_, enqueueErr := client.Enqueue(queue.TypeSupplierSync, map[string]string{"supplier_id": item.ID.String(), "trigger": "manual"}, asynq.Queue("default"), asynq.Unique(4*time.Minute))
		switch {
		case enqueueErr == nil:
			queued++
		case isDuplicateSupplyTask(enqueueErr):
			alreadyQueued++
		default:
			failed++
		}
	}
	if queued+alreadyQueued == 0 && skipped == 0 && failed == 0 {
		response.Error(c, 409, 40504, "error.sync_requires_enabled_supplier")
		return
	}
	state := "queued"
	if failed > 0 {
		state = "partial"
	}
	h.audit(c, "supplier.sync.enqueue-all", "supplier", "batch", fmt.Sprintf("%s；queued=%d；already_queued=%d；skipped=%d；failed=%d", reason, queued, alreadyQueued, skipped, failed))
	if queued+alreadyQueued == 0 && failed > 0 {
		response.Error(c, 503, 50392, "error.sync_queue_unavailable_retry")
		return
	}
	c.JSON(http.StatusAccepted, response.Envelope{Code: 0, Message: "accepted", Data: gin.H{
		"status": state, "queued": queued, "already_queued": alreadyQueued, "skipped": skipped, "failed": failed,
	}})
}

type adminSupplyCatalogVariant struct {
	ID        uuid.UUID `json:"id"`
	ProductID uuid.UUID `json:"product_id"`
	SKU       string    `json:"sku"`
	Name      string    `json:"name"`
	Price     int64     `json:"price"`
	Currency  string    `json:"currency"`
	Status    string    `json:"status"`
}

type adminSupplyCatalogInputField struct {
	ID             uuid.UUID `json:"id"`
	Key            string    `json:"key"`
	Label          string    `json:"label"`
	InputType      string    `json:"input_type"`
	Required       bool      `json:"required"`
	PassToSupplier bool      `json:"pass_to_supplier"`
}

type adminSupplyCatalogProduct struct {
	ID          uuid.UUID                      `json:"id"`
	Name        string                         `json:"name"`
	Slug        string                         `json:"slug"`
	Price       int64                          `json:"price"`
	Currency    string                         `json:"currency"`
	Status      string                         `json:"status"`
	Variants    []adminSupplyCatalogVariant    `json:"variants"`
	InputFields []adminSupplyCatalogInputField `json:"input_fields"`
}

func (h Handler) AdminSupplyCatalog(c *gin.Context) {
	page, pageSize := pagination(c)
	query := h.DB.Model(&model.Product{}).Where("inventory_mode = ?", "supplier")
	if keyword := strings.TrimSpace(c.Query("q")); keyword != "" {
		like := "%" + keyword + "%"
		query = query.Where("name ILIKE ? OR slug ILIKE ?", like, like)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		response.Error(c, 500, 50503, "error.supply_product_list_fetch_failed")
		return
	}
	var products []model.Product
	if err := query.Order("sort DESC, created_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&products).Error; err != nil {
		response.Error(c, 500, 50503, "error.supply_product_list_fetch_failed")
		return
	}
	productIDs := make([]uuid.UUID, 0, len(products))
	productCurrencies := make(map[uuid.UUID]string, len(products))
	for _, item := range products {
		productIDs = append(productIDs, item.ID)
		productCurrencies[item.ID] = item.Currency
	}
	variantsByProduct := make(map[uuid.UUID][]adminSupplyCatalogVariant, len(products))
	inputFieldsByProduct := make(map[uuid.UUID][]adminSupplyCatalogInputField, len(products))
	if len(productIDs) > 0 {
		var variants []model.ProductVariant
		if err := h.DB.Where("product_id IN ?", productIDs).Order("sort DESC, created_at ASC").Find(&variants).Error; err != nil {
			response.Error(c, 500, 50503, "error.supply_product_sku_list_fetch_failed")
			return
		}
		for _, variant := range variants {
			variantsByProduct[variant.ProductID] = append(variantsByProduct[variant.ProductID], adminSupplyCatalogVariant{ID: variant.ID, ProductID: variant.ProductID, SKU: variant.SKU, Name: variant.Name, Price: variant.Price, Currency: productCurrencies[variant.ProductID], Status: variant.Status})
		}
		var inputFields []model.ProductInputField
		if err := h.DB.Select("id", "product_id", "key", "label", "input_type", "required", "pass_to_supplier").Where("product_id IN ? AND enabled = ?", productIDs, true).Order("sort DESC, created_at ASC").Find(&inputFields).Error; err != nil {
			response.Error(c, 500, 50503, "error.supply_product_list_fetch_failed")
			return
		}
		for _, field := range inputFields {
			inputFieldsByProduct[field.ProductID] = append(inputFieldsByProduct[field.ProductID], adminSupplyCatalogInputField{
				ID: field.ID, Key: field.Key, Label: field.Label, InputType: field.InputType,
				Required: field.Required, PassToSupplier: field.PassToSupplier,
			})
		}
	}
	items := make([]adminSupplyCatalogProduct, 0, len(products))
	for _, product := range products {
		variants := variantsByProduct[product.ID]
		if variants == nil {
			variants = []adminSupplyCatalogVariant{}
		}
		inputFields := inputFieldsByProduct[product.ID]
		if inputFields == nil {
			inputFields = []adminSupplyCatalogInputField{}
		}
		items = append(items, adminSupplyCatalogProduct{ID: product.ID, Name: product.Name, Slug: product.Slug, Price: product.Price, Currency: product.Currency, Status: product.Status, Variants: variants, InputFields: inputFields})
	}
	response.Page(c, items, total, page, pageSize)
}

type optionalSupplyUUID struct {
	Set   bool
	Value *uuid.UUID
}

type optionalSupplyParameterMapping struct {
	Set   bool
	Value map[string]string
}

func (value *optionalSupplyParameterMapping) UnmarshalJSON(raw []byte) error {
	value.Set = true
	parsed, err := service.DecodeSupplierParameterMapping(json.RawMessage(raw))
	if err != nil {
		return err
	}
	value.Value = parsed
	return nil
}

func (value *optionalSupplyUUID) UnmarshalJSON(raw []byte) error {
	value.Set = true
	if string(raw) == "null" {
		value.Value = nil
		return nil
	}
	var text string
	if len(raw) < 2 || raw[0] != '"' || raw[len(raw)-1] != '"' {
		return errors.New("UUID must be a string or null")
	}
	text = strings.Trim(string(raw), `"`)
	parsed, err := uuid.Parse(text)
	if err != nil || parsed == uuid.Nil {
		return errors.New("invalid UUID")
	}
	value.Value = &parsed
	return nil
}

type supplyMappingCreateRequest struct {
	SupplierID          uuid.UUID                      `json:"supplier_id"`
	ProductID           uuid.UUID                      `json:"product_id"`
	VariantID           *uuid.UUID                     `json:"variant_id"`
	ExternalProductID   string                         `json:"external_product_id"`
	ParameterMapping    optionalSupplyParameterMapping `json:"parameter_mapping"`
	PriceMode           string                         `json:"price_mode"`
	MarkupBasisPoint    int                            `json:"markup_basis_point"`
	MarkupAmount        int64                          `json:"markup_amount"`
	FixedPrice          int64                          `json:"fixed_price"`
	AutoSyncPrice       bool                           `json:"auto_sync_price"`
	AutoSyncStock       bool                           `json:"auto_sync_stock"`
	AutoSyncTitle       bool                           `json:"auto_sync_title"`
	AutoSyncSummary     bool                           `json:"auto_sync_summary"`
	AutoSyncDescription bool                           `json:"auto_sync_description"`
	AutoSyncMedia       bool                           `json:"auto_sync_media"`
	MirrorRemoteMedia   bool                           `json:"mirror_remote_media"`
	AutoSyncCategory    bool                           `json:"auto_sync_category"`
	AutoSyncVariants    bool                           `json:"auto_sync_variants"`
	AutoSyncStatus      bool                           `json:"auto_sync_status"`
	AutoSyncLimits      bool                           `json:"auto_sync_limits"`
}

type supplyMappingUpdateRequest struct {
	SupplierID          *uuid.UUID                     `json:"supplier_id"`
	ProductID           *uuid.UUID                     `json:"product_id"`
	VariantID           optionalSupplyUUID             `json:"variant_id"`
	ExternalProductID   *string                        `json:"external_product_id"`
	ParameterMapping    optionalSupplyParameterMapping `json:"parameter_mapping"`
	PriceMode           *string                        `json:"price_mode"`
	MarkupBasisPoint    *int                           `json:"markup_basis_point"`
	MarkupAmount        *int64                         `json:"markup_amount"`
	FixedPrice          *int64                         `json:"fixed_price"`
	AutoSyncPrice       *bool                          `json:"auto_sync_price"`
	AutoSyncStock       *bool                          `json:"auto_sync_stock"`
	AutoSyncTitle       *bool                          `json:"auto_sync_title"`
	AutoSyncSummary     *bool                          `json:"auto_sync_summary"`
	AutoSyncDescription *bool                          `json:"auto_sync_description"`
	AutoSyncMedia       *bool                          `json:"auto_sync_media"`
	MirrorRemoteMedia   *bool                          `json:"mirror_remote_media"`
	AutoSyncCategory    *bool                          `json:"auto_sync_category"`
	AutoSyncVariants    *bool                          `json:"auto_sync_variants"`
	AutoSyncStatus      *bool                          `json:"auto_sync_status"`
	AutoSyncLimits      *bool                          `json:"auto_sync_limits"`
}

func normalizeSupplyMapping(item *model.ProductMapping) error {
	externalProductID, identityErr := supply.NormalizeExternalID(item.ExternalProductID)
	if identityErr != nil {
		return errors.New("invalid mapping identity")
	}
	item.ExternalProductID = externalProductID
	item.PriceMode = strings.ToLower(strings.TrimSpace(item.PriceMode))
	parameterMapping, err := service.DecodeSupplierParameterMapping(item.ParameterMapping)
	if err != nil {
		return err
	}
	item.ParameterMapping, err = service.EncodeSupplierParameterMapping(parameterMapping)
	if err != nil {
		return err
	}
	if item.SupplierID == uuid.Nil || item.ProductID == uuid.Nil {
		return errors.New("invalid mapping identity")
	}
	if item.VariantID != nil && *item.VariantID == uuid.Nil {
		return errors.New("invalid mapping variant")
	}
	switch item.PriceMode {
	case "fixed_markup":
		if item.MarkupBasisPoint < 0 || item.MarkupBasisPoint > 100_000 {
			return errors.New("invalid supplier markup")
		}
		item.FixedPrice = 0
		item.MarkupAmount = 0
	case "fixed_amount":
		if item.MarkupAmount < 0 || item.MarkupAmount > 100_000_000 {
			return errors.New("invalid supplier fixed amount markup")
		}
		item.MarkupBasisPoint = 0
		item.FixedPrice = 0
	case "fixed_price":
		if item.FixedPrice < 1 || item.FixedPrice > 100_000_000 {
			return errors.New("invalid supplier fixed price")
		}
		item.MarkupBasisPoint = 0
		item.MarkupAmount = 0
	default:
		return errors.New("invalid supplier price mode")
	}
	return nil
}

func applySupplyMappingUpdate(current model.ProductMapping, request supplyMappingUpdateRequest) (model.ProductMapping, error) {
	if request.SupplierID == nil && request.ProductID == nil && !request.VariantID.Set && request.ExternalProductID == nil && !request.ParameterMapping.Set && request.PriceMode == nil && request.MarkupBasisPoint == nil && request.MarkupAmount == nil && request.FixedPrice == nil && request.AutoSyncPrice == nil && request.AutoSyncStock == nil && request.AutoSyncTitle == nil && request.AutoSyncSummary == nil && request.AutoSyncDescription == nil && request.AutoSyncMedia == nil && request.MirrorRemoteMedia == nil && request.AutoSyncCategory == nil && request.AutoSyncVariants == nil && request.AutoSyncStatus == nil && request.AutoSyncLimits == nil {
		return current, errors.New("empty mapping update")
	}
	if request.SupplierID != nil {
		current.SupplierID = *request.SupplierID
	}
	if request.ProductID != nil {
		current.ProductID = *request.ProductID
	}
	if request.VariantID.Set {
		current.VariantID = request.VariantID.Value
	}
	if request.ExternalProductID != nil {
		current.ExternalProductID = *request.ExternalProductID
	}
	if request.ParameterMapping.Set {
		encoded, err := service.EncodeSupplierParameterMapping(request.ParameterMapping.Value)
		if err != nil {
			return current, err
		}
		current.ParameterMapping = encoded
	}
	if request.PriceMode != nil {
		current.PriceMode = *request.PriceMode
	}
	if request.MarkupBasisPoint != nil {
		current.MarkupBasisPoint = *request.MarkupBasisPoint
	}
	if request.MarkupAmount != nil {
		current.MarkupAmount = *request.MarkupAmount
	}
	if request.FixedPrice != nil {
		current.FixedPrice = *request.FixedPrice
	}
	if request.AutoSyncPrice != nil {
		current.AutoSyncPrice = *request.AutoSyncPrice
	}
	if request.AutoSyncStock != nil {
		current.AutoSyncStock = *request.AutoSyncStock
	}
	if request.AutoSyncTitle != nil {
		current.AutoSyncTitle = *request.AutoSyncTitle
	}
	if request.AutoSyncSummary != nil {
		current.AutoSyncSummary = *request.AutoSyncSummary
	}
	if request.AutoSyncDescription != nil {
		current.AutoSyncDescription = *request.AutoSyncDescription
	}
	if request.AutoSyncMedia != nil {
		current.AutoSyncMedia = *request.AutoSyncMedia
	}
	if request.MirrorRemoteMedia != nil {
		current.MirrorRemoteMedia = *request.MirrorRemoteMedia
	}
	if request.AutoSyncCategory != nil {
		current.AutoSyncCategory = *request.AutoSyncCategory
	}
	if request.AutoSyncVariants != nil {
		current.AutoSyncVariants = *request.AutoSyncVariants
	}
	if request.AutoSyncStatus != nil {
		current.AutoSyncStatus = *request.AutoSyncStatus
	}
	if request.AutoSyncLimits != nil {
		current.AutoSyncLimits = *request.AutoSyncLimits
	}
	return current, normalizeSupplyMapping(&current)
}

func supplyMappingUpdateOverridesCategoryPolicy(request supplyMappingUpdateRequest) bool {
	return request.SupplierID != nil || request.ExternalProductID != nil ||
		request.PriceMode != nil || request.MarkupBasisPoint != nil || request.MarkupAmount != nil || request.FixedPrice != nil ||
		request.AutoSyncPrice != nil || request.AutoSyncStock != nil || request.AutoSyncTitle != nil
}

func ensureSupplyMappingRelations(tx *gorm.DB, item model.ProductMapping) error {
	var supplier model.Supplier
	if err := tx.Select("id").First(&supplier, "id = ?", item.SupplierID).Error; err != nil {
		return err
	}
	var product model.Product
	if err := tx.Select("id", "inventory_mode").First(&product, "id = ?", item.ProductID).Error; err != nil {
		return err
	}
	if product.InventoryMode != "supplier" {
		return errors.New("product is not configured for supplier inventory")
	}
	parameterMapping, err := service.DecodeSupplierParameterMapping(item.ParameterMapping)
	if err != nil {
		return err
	}
	if len(parameterMapping) > 0 {
		var fields []model.ProductInputField
		if err := tx.Select("key").Where("product_id = ? AND enabled = ? AND pass_to_supplier = ?", item.ProductID, true, true).Find(&fields).Error; err != nil {
			return err
		}
		available := make(map[string]struct{}, len(fields))
		for _, field := range fields {
			available[field.Key] = struct{}{}
		}
		for source := range parameterMapping {
			if _, exists := available[source]; !exists {
				return errSupplyParameterFieldInvalid
			}
		}
		// Include identity-mapped fields in collision detection. Otherwise a
		// configured target could overwrite a different pass-through field that
		// retained its local key.
		usedTargets := make(map[string]struct{}, len(fields))
		for _, field := range fields {
			target := field.Key
			if mapped, exists := parameterMapping[field.Key]; exists {
				target = mapped
			}
			if _, duplicate := usedTargets[target]; duplicate {
				return errSupplyParameterFieldInvalid
			}
			usedTargets[target] = struct{}{}
		}
	}
	if item.VariantID == nil {
		var activeVariants int64
		if err := tx.Model(&model.ProductVariant{}).Where("product_id = ? AND status = ?", item.ProductID, "active").Count(&activeVariants).Error; err != nil {
			return err
		}
		if activeVariants > 0 {
			return errors.New("a product variant is required")
		}
	} else {
		var count int64
		if err := tx.Model(&model.ProductVariant{}).Where("id = ? AND product_id = ? AND status = ?", *item.VariantID, item.ProductID, "active").Count(&count).Error; err != nil {
			return err
		}
		if count != 1 {
			return errors.New("variant does not belong to product")
		}
	}
	if item.AutoSyncPrice {
		query := tx.Model(&model.ProductMapping{}).Where("product_id = ? AND auto_sync_price = ?", item.ProductID, true)
		if item.ID != uuid.Nil {
			query = query.Where("id <> ?", item.ID)
		}
		if item.VariantID == nil {
			query = query.Where("variant_id IS NULL")
		} else {
			query = query.Where("variant_id = ?", *item.VariantID)
		}
		var count int64
		if err := query.Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return errSupplyPriceSourceConflict
		}
	}
	return nil
}

type adminSupplyMappingItem struct {
	ID                        uuid.UUID       `json:"id"`
	SupplierID                uuid.UUID       `json:"supplier_id"`
	SupplierName              string          `json:"supplier_name"`
	SupplierCode              string          `json:"supplier_code"`
	ProductID                 uuid.UUID       `json:"product_id"`
	ProductName               string          `json:"product_name"`
	ProductStatus             string          `json:"product_status"`
	VariantID                 *uuid.UUID      `json:"variant_id"`
	VariantName               string          `json:"variant_name"`
	VariantSKU                string          `json:"variant_sku"`
	ExternalProductID         string          `json:"external_product_id"`
	SupplierCategoryMappingID *uuid.UUID      `json:"supplier_category_mapping_id,omitempty"`
	InheritCategoryPolicy     bool            `json:"inherit_category_policy"`
	ParameterMapping          json.RawMessage `json:"parameter_mapping"`
	PriceMode                 string          `json:"price_mode"`
	MarkupBasisPoint          int             `json:"markup_basis_point"`
	MarkupAmount              int64           `json:"markup_amount"`
	MarkupCurrency            string          `json:"markup_currency"`
	FixedPrice                int64           `json:"fixed_price"`
	FixedPriceCurrency        string          `json:"fixed_price_currency"`
	AutoSyncPrice             bool            `json:"auto_sync_price"`
	AutoSyncStock             bool            `json:"auto_sync_stock"`
	AutoSyncTitle             bool            `json:"auto_sync_title"`
	AutoSyncSummary           bool            `json:"auto_sync_summary"`
	AutoSyncDescription       bool            `json:"auto_sync_description"`
	AutoSyncMedia             bool            `json:"auto_sync_media"`
	MirrorRemoteMedia         bool            `json:"mirror_remote_media"`
	AutoSyncCategory          bool            `json:"auto_sync_category"`
	AutoSyncVariants          bool            `json:"auto_sync_variants"`
	AutoSyncStatus            bool            `json:"auto_sync_status"`
	AutoSyncLimits            bool            `json:"auto_sync_limits"`
	LastSyncedAt              *time.Time      `json:"last_synced_at"`
	LastError                 string          `json:"last_error"`
	LatestExternalPrice       *int64          `json:"latest_external_price"`
	LatestExternalCurrency    string          `json:"latest_external_currency"`
	LatestExternalStock       *int64          `json:"latest_external_stock"`
	CreatedAt                 time.Time       `json:"created_at"`
	UpdatedAt                 time.Time       `json:"updated_at"`
}

func safeSupplyMappingError(value, locale string) string {
	switch strings.TrimSpace(value) {
	case "":
		return ""
	case "external product missing":
		return i18n.T(locale, "notice.supply_error_external_missing")
	case "external product invalid":
		return i18n.T(locale, "notice.supply_error_external_invalid")
	case "local product missing":
		return i18n.T(locale, "notice.supply_error_local_missing")
	case "pricing rule invalid":
		return i18n.T(locale, "notice.supply_error_pricing_invalid")
	default:
		return i18n.T(locale, "notice.supply_error_generic")
	}
}

func supplyMappingListQuery(db *gorm.DB) *gorm.DB {
	return db.Table("product_mappings pm").
		Select(`pm.id, pm.supplier_id, s.name AS supplier_name, s.code AS supplier_code,
			pm.product_id, p.name AS product_name, p.status AS product_status,
			pm.variant_id, COALESCE(v.name, '') AS variant_name, COALESCE(v.sku, '') AS variant_sku,
			pm.external_product_id, pm.supplier_category_mapping_id, pm.inherit_category_policy,
			pm.parameter_mapping, pm.price_mode, pm.markup_basis_point, pm.markup_amount, pm.markup_currency, pm.fixed_price, pm.fixed_price_currency,
			pm.auto_sync_price, pm.auto_sync_stock, pm.auto_sync_title, pm.auto_sync_summary,
			pm.auto_sync_description, pm.auto_sync_media, pm.mirror_remote_media,
			pm.auto_sync_category, pm.auto_sync_variants, pm.auto_sync_status, pm.auto_sync_limits,
			pm.last_synced_at, pm.last_error,
			sp.external_price AS latest_external_price,
			COALESCE(NULLIF(scp.currency, ''), NULLIF(pm.last_upstream_currency, ''), s.price_currency) AS latest_external_currency,
			sp.external_stock AS latest_external_stock,
			pm.created_at, pm.updated_at`).
		Joins("JOIN suppliers s ON s.id = pm.supplier_id AND s.deleted_at IS NULL").
		Joins("JOIN products p ON p.id = pm.product_id AND p.deleted_at IS NULL").
		Joins("LEFT JOIN product_variants v ON v.id = pm.variant_id AND v.deleted_at IS NULL").
		Joins("LEFT JOIN supplier_products sp ON sp.supplier_id = pm.supplier_id AND sp.product_id = pm.product_id AND sp.variant_id IS NOT DISTINCT FROM pm.variant_id AND sp.deleted_at IS NULL").
		Joins("LEFT JOIN supplier_catalog_products scp ON scp.supplier_id = pm.supplier_id AND scp.external_id = pm.external_product_id AND scp.deleted_at IS NULL").
		Where("pm.deleted_at IS NULL")
}

func (h Handler) AdminSupplyMappings(c *gin.Context) {
	page, pageSize := pagination(c)
	query := supplyMappingListQuery(h.DB)
	if supplierID := strings.TrimSpace(c.Query("supplier_id")); supplierID != "" {
		parsed, err := uuid.Parse(supplierID)
		if err != nil {
			response.Error(c, 422, 42506, "error.supplier_filter_id_invalid")
			return
		}
		query = query.Where("pm.supplier_id = ?", parsed)
	}
	if productID := strings.TrimSpace(c.Query("product_id")); productID != "" {
		parsed, err := uuid.Parse(productID)
		if err != nil {
			response.Error(c, 422, 42507, "error.product_filter_id_invalid")
			return
		}
		query = query.Where("pm.product_id = ?", parsed)
	}
	if priceMode := strings.ToLower(strings.TrimSpace(c.Query("price_mode"))); priceMode != "" {
		if priceMode != "fixed_markup" && priceMode != "fixed_amount" && priceMode != "fixed_price" {
			response.Error(c, 422, 42508, "error.pricing_mode_filter_invalid")
			return
		}
		query = query.Where("pm.price_mode = ?", priceMode)
	}
	if keyword := strings.TrimSpace(c.Query("q")); keyword != "" {
		like := "%" + keyword + "%"
		query = query.Where("pm.external_product_id ILIKE ? OR p.name ILIKE ? OR s.name ILIKE ? OR s.code ILIKE ? OR COALESCE(v.sku, '') ILIKE ?", like, like, like, like, like)
	}
	var total int64
	if err := query.Session(&gorm.Session{}).Select("COUNT(DISTINCT pm.id)").Scan(&total).Error; err != nil {
		response.Error(c, 500, 50504, "error.product_mapping_list_fetch_failed")
		return
	}
	items := make([]adminSupplyMappingItem, 0)
	if err := query.Order("pm.created_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Scan(&items).Error; err != nil {
		response.Error(c, 500, 50504, "error.product_mapping_list_fetch_failed")
		return
	}
	for index := range items {
		items[index].LastError = safeSupplyMappingError(items[index].LastError, i18n.ResolveLocale(c))
	}
	response.Page(c, items, total, page, pageSize)
}

func (h Handler) CreateSupplyMapping(c *gin.Context) {
	reason, ok := requireAdminChangeReason(c, "创建商品映射")
	if !ok {
		return
	}
	var req supplyMappingCreateRequest
	if decodeStrictJSON(c, &req) != nil {
		response.Error(c, 422, 42509, "error.product_mapping_fields_invalid")
		return
	}
	parameterMapping, mappingErr := service.EncodeSupplierParameterMapping(req.ParameterMapping.Value)
	if mappingErr != nil {
		response.Error(c, 422, 42509, "error.product_mapping_strategy_fields_invalid")
		return
	}
	item := model.ProductMapping{SupplierID: req.SupplierID, ProductID: req.ProductID, VariantID: req.VariantID, ExternalProductID: req.ExternalProductID, ParameterMapping: parameterMapping, PriceMode: req.PriceMode, MarkupBasisPoint: req.MarkupBasisPoint, MarkupAmount: req.MarkupAmount, FixedPrice: req.FixedPrice, AutoSyncPrice: req.AutoSyncPrice, AutoSyncStock: req.AutoSyncStock, AutoSyncTitle: req.AutoSyncTitle, AutoSyncSummary: req.AutoSyncSummary, AutoSyncDescription: req.AutoSyncDescription, AutoSyncMedia: req.AutoSyncMedia, MirrorRemoteMedia: req.MirrorRemoteMedia, AutoSyncCategory: req.AutoSyncCategory, AutoSyncVariants: req.AutoSyncVariants, AutoSyncStatus: req.AutoSyncStatus, AutoSyncLimits: req.AutoSyncLimits}
	if normalizeSupplyMapping(&item) != nil {
		response.Error(c, 422, 42509, "error.product_mapping_strategy_fields_invalid")
		return
	}
	err := h.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("LOCK TABLE product_mappings IN SHARE ROW EXCLUSIVE MODE").Error; err != nil {
			return err
		}
		if err := ensureSupplyMappingRelations(tx, item); err != nil {
			return err
		}
		var product model.Product
		if err := tx.Select("currency").First(&product, "id = ?", item.ProductID).Error; err != nil {
			return err
		}
		item.FixedPriceCurrency = product.Currency
		item.MarkupCurrency = product.Currency
		autoSyncPrice, autoSyncStock := item.AutoSyncPrice, item.AutoSyncStock
		flags := map[string]any{
			"auto_sync_price": autoSyncPrice, "auto_sync_stock": autoSyncStock,
			"auto_sync_title": item.AutoSyncTitle, "auto_sync_summary": item.AutoSyncSummary,
			"auto_sync_description": item.AutoSyncDescription, "auto_sync_media": item.AutoSyncMedia,
			"mirror_remote_media": item.MirrorRemoteMedia, "auto_sync_category": item.AutoSyncCategory,
			"auto_sync_variants": item.AutoSyncVariants, "auto_sync_status": item.AutoSyncStatus,
			"auto_sync_limits": item.AutoSyncLimits,
		}
		if err := tx.Create(&item).Error; err != nil {
			return err
		}
		// GORM applies model-level default:true tags to zero bools during Create.
		// Reassert both operator choices in the same locked transaction so false
		// is persisted exactly and is never externally observable as true.
		if err := tx.Model(&item).UpdateColumns(flags).Error; err != nil {
			return err
		}
		item.AutoSyncPrice, item.AutoSyncStock = autoSyncPrice, autoSyncStock
		return nil
	})
	if errors.Is(err, errSupplyPriceSourceConflict) {
		response.Error(c, 409, 40512, "error.single_auto_price_source_allowed")
		return
	}
	if errors.Is(err, errSupplyParameterFieldInvalid) {
		response.Error(c, 422, 42509, "error.product_mapping_strategy_fields_invalid")
		return
	}
	if err != nil {
		response.Error(c, 409, 40505, "error.supplier_mapping_conflict")
		return
	}
	h.audit(c, "supplier.mapping.create", "product_mapping", item.ID.String(), reason)
	response.Created(c, item)
}

func deleteSupplySnapshot(tx *gorm.DB, supplierID, productID uuid.UUID, variantID *uuid.UUID) error {
	query := tx.Where("supplier_id = ? AND product_id = ?", supplierID, productID)
	if variantID == nil {
		query = query.Where("variant_id IS NULL")
	} else {
		query = query.Where("variant_id = ?", *variantID)
	}
	return query.Delete(&model.SupplierProduct{}).Error
}

func (h Handler) UpdateSupplyMapping(c *gin.Context) {
	mappingID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, 422, 42510, "error.mapping_id_invalid")
		return
	}
	reason, ok := requireAdminChangeReason(c, "更新商品映射")
	if !ok {
		return
	}
	var req supplyMappingUpdateRequest
	if decodeStrictJSON(c, &req) != nil {
		response.Error(c, 422, 42509, "error.product_mapping_fields_invalid")
		return
	}
	var updated model.ProductMapping
	err = h.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("LOCK TABLE product_mappings IN SHARE ROW EXCLUSIVE MODE").Error; err != nil {
			return err
		}
		var current model.ProductMapping
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&current, "id = ?", mappingID).Error; err != nil {
			return err
		}
		candidate, err := applySupplyMappingUpdate(current, req)
		if err != nil {
			return err
		}
		if err := ensureSupplyMappingRelations(tx, candidate); err != nil {
			return err
		}
		if supplyMappingUpdateOverridesCategoryPolicy(req) {
			candidate.SupplierCategoryMappingID = nil
			candidate.InheritCategoryPolicy = false
		}
		var product model.Product
		if err := tx.Select("currency").First(&product, "id = ?", candidate.ProductID).Error; err != nil {
			return err
		}
		candidate.FixedPriceCurrency = product.Currency
		candidate.MarkupCurrency = product.Currency
		if err := deleteSupplySnapshot(tx, current.SupplierID, current.ProductID, current.VariantID); err != nil {
			return err
		}
		updates := map[string]any{
			"supplier_id": candidate.SupplierID, "product_id": candidate.ProductID, "variant_id": candidate.VariantID,
			"supplier_category_mapping_id": candidate.SupplierCategoryMappingID,
			"inherit_category_policy":      candidate.InheritCategoryPolicy,
			"external_product_id":          candidate.ExternalProductID, "price_mode": candidate.PriceMode,
			"parameter_mapping":  candidate.ParameterMapping,
			"markup_basis_point": candidate.MarkupBasisPoint, "markup_amount": candidate.MarkupAmount, "markup_currency": candidate.MarkupCurrency, "fixed_price": candidate.FixedPrice, "fixed_price_currency": candidate.FixedPriceCurrency,
			"auto_sync_price": candidate.AutoSyncPrice, "auto_sync_stock": candidate.AutoSyncStock,
			"auto_sync_title": candidate.AutoSyncTitle, "auto_sync_summary": candidate.AutoSyncSummary,
			"auto_sync_description": candidate.AutoSyncDescription, "auto_sync_media": candidate.AutoSyncMedia,
			"mirror_remote_media": candidate.MirrorRemoteMedia, "auto_sync_category": candidate.AutoSyncCategory,
			"auto_sync_variants": candidate.AutoSyncVariants, "auto_sync_status": candidate.AutoSyncStatus,
			"auto_sync_limits": candidate.AutoSyncLimits,
			"last_synced_at":   nil, "last_error": "",
		}
		if err := tx.Model(&current).Updates(updates).Error; err != nil {
			return err
		}
		return tx.First(&updated, "id = ?", current.ID).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		response.Error(c, 404, 40506, "error.product_mapping_not_found")
		return
	}
	if errors.Is(err, errSupplyPriceSourceConflict) {
		response.Error(c, 409, 40512, "error.single_auto_price_source_allowed")
		return
	}
	if errors.Is(err, errSupplyParameterFieldInvalid) {
		response.Error(c, 422, 42509, "error.product_mapping_strategy_fields_invalid")
		return
	}
	if err != nil {
		response.Error(c, 409, 40507, "error.mapping_invalid_or_conflict")
		return
	}
	h.audit(c, "supplier.mapping.update", "product_mapping", updated.ID.String(), reason)
	response.OK(c, updated)
}

func (h Handler) DeleteSupplyMapping(c *gin.Context) {
	mappingID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, 422, 42510, "error.mapping_id_invalid")
		return
	}
	reason, ok := requireAdminChangeReason(c, "删除商品映射")
	if !ok {
		return
	}
	var item model.ProductMapping
	err = h.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&item, "id = ?", mappingID).Error; err != nil {
			return err
		}
		if err := deleteSupplySnapshot(tx, item.SupplierID, item.ProductID, item.VariantID); err != nil {
			return err
		}
		return tx.Delete(&item).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		response.Error(c, 404, 40506, "error.product_mapping_not_found")
		return
	}
	if err != nil {
		response.Error(c, 409, 40508, "error.product_mapping_delete_failed")
		return
	}
	h.audit(c, "supplier.mapping.delete", "product_mapping", item.ID.String(), reason)
	response.OK(c, gin.H{"deleted": true})
}

type adminSupplyProcurementItem struct {
	ID                  uuid.UUID  `json:"id"`
	ProcurementNo       string     `json:"procurement_no"`
	SupplierID          uuid.UUID  `json:"supplier_id"`
	SupplierName        string     `json:"supplier_name"`
	SupplierCode        string     `json:"supplier_code"`
	OrderID             uuid.UUID  `json:"order_id"`
	OrderNo             string     `json:"order_no"`
	OrderItemID         uuid.UUID  `json:"order_item_id"`
	ProductName         string     `json:"product_name"`
	VariantName         string     `json:"variant_name"`
	ExternalOrderNo     string     `json:"external_order_no"`
	ExternalProductID   string     `json:"external_product_id"`
	Quantity            int        `json:"quantity"`
	CostAmount          int64      `json:"cost_amount"`
	CostCurrency        string     `json:"cost_currency"`
	UpstreamCurrency    string     `json:"upstream_currency"`
	Status              string     `json:"status"`
	Attempts            int        `json:"attempts"`
	NextPollAt          *time.Time `json:"next_poll_at"`
	CompletedAt         *time.Time `json:"completed_at"`
	RetryMessage        string     `json:"retry_message"`
	CallbackStatus      string     `json:"callback_status,omitempty"`
	CallbackReceivedAt  *time.Time `json:"callback_received_at,omitempty"`
	CallbackProcessedAt *time.Time `json:"callback_processed_at,omitempty"`
	CreatedAt           time.Time  `json:"created_at"`
	UpdatedAt           time.Time  `json:"updated_at"`
}

func procurementRetryMessage(status string, attempts int, next *time.Time, locale string) string {
	switch status {
	case "creating", "dispatching":
		return i18n.T(locale, "notice.procurement_dispatching")
	case "processing":
		if next != nil {
			return i18n.T(locale, "notice.procurement_processing_scheduled")
		}
		return i18n.T(locale, "notice.procurement_processing_waiting")
	case "retrying":
		if attempts >= 120 {
			return i18n.T(locale, "notice.procurement_retry_limit")
		}
		return i18n.T(locale, "notice.procurement_retry_backoff")
	case "failed":
		return i18n.T(locale, "notice.procurement_failed")
	case "cancelled":
		return i18n.T(locale, "notice.procurement_cancelled")
	case "completed":
		return i18n.T(locale, "notice.procurement_completed")
	default:
		return i18n.T(locale, "notice.procurement_unknown")
	}
}

func supplyProcurementListQuery(db *gorm.DB) *gorm.DB {
	return db.Table("procurement_orders po").
		Select(`po.id, po.procurement_no, po.supplier_id, s.name AS supplier_name, s.code AS supplier_code,
			po.order_id, o.order_no, po.order_item_id, oi.product_name, oi.variant_name,
			po.external_order_no, po.external_product_id, po.quantity, po.cost_amount, po.cost_currency, po.upstream_currency, po.status,
			po.attempts, po.next_poll_at, po.completed_at, po.created_at, po.updated_at,
			(SELECT we.status FROM webhook_events we WHERE we.procurement_order_id = po.id AND we.deleted_at IS NULL ORDER BY we.created_at DESC LIMIT 1) AS callback_status,
			(SELECT we.created_at FROM webhook_events we WHERE we.procurement_order_id = po.id AND we.deleted_at IS NULL ORDER BY we.created_at DESC LIMIT 1) AS callback_received_at,
			(SELECT we.processed_at FROM webhook_events we WHERE we.procurement_order_id = po.id AND we.deleted_at IS NULL ORDER BY we.created_at DESC LIMIT 1) AS callback_processed_at`).
		Joins("JOIN suppliers s ON s.id = po.supplier_id AND s.deleted_at IS NULL").
		Joins("JOIN orders o ON o.id = po.order_id AND o.deleted_at IS NULL").
		Joins("JOIN order_items oi ON oi.id = po.order_item_id AND oi.deleted_at IS NULL").
		Where("po.deleted_at IS NULL")
}

func applySupplyProcurementFilters(c *gin.Context, query *gorm.DB) (*gorm.DB, error) {
	if status := strings.ToLower(strings.TrimSpace(c.Query("status"))); status != "" {
		allowed := map[string]bool{"creating": true, "dispatching": true, "processing": true, "retrying": true, "completed": true, "failed": true, "cancelled": true}
		if !allowed[status] {
			return nil, errors.New("invalid procurement status")
		}
		query = query.Where("po.status = ?", status)
	}
	for parameter, column := range map[string]string{"supplier_id": "po.supplier_id", "order_id": "po.order_id"} {
		value := strings.TrimSpace(c.Query(parameter))
		if value == "" {
			continue
		}
		parsed, err := uuid.Parse(value)
		if err != nil {
			return nil, fmt.Errorf("invalid %s", parameter)
		}
		query = query.Where(column+" = ?", parsed)
	}
	if keyword := strings.TrimSpace(c.Query("q")); keyword != "" {
		like := "%" + keyword + "%"
		query = query.Where("po.procurement_no ILIKE ? OR po.external_order_no ILIKE ? OR po.external_product_id ILIKE ? OR o.order_no ILIKE ?", like, like, like, like)
	}
	return query, nil
}

func (h Handler) AdminSupplyProcurements(c *gin.Context) {
	page, pageSize := pagination(c)
	query, err := applySupplyProcurementFilters(c, supplyProcurementListQuery(h.DB))
	if err != nil {
		response.Error(c, 422, 42511, "error.purchase_status_filter_invalid")
		return
	}
	var total int64
	if err := query.Session(&gorm.Session{}).Select("COUNT(DISTINCT po.id)").Scan(&total).Error; err != nil {
		response.Error(c, 500, 50505, "error.purchase_order_list_fetch_failed")
		return
	}
	items := make([]adminSupplyProcurementItem, 0)
	if err := query.Order("po.created_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Scan(&items).Error; err != nil {
		response.Error(c, 500, 50505, "error.purchase_order_list_fetch_failed")
		return
	}
	for index := range items {
		items[index].RetryMessage = procurementRetryMessage(items[index].Status, items[index].Attempts, items[index].NextPollAt, i18n.ResolveLocale(c))
	}
	response.Page(c, items, total, page, pageSize)
}

type adminSupplyOrderSummary struct {
	ID            uuid.UUID  `json:"id"`
	OrderNo       string     `json:"order_no"`
	Status        string     `json:"status"`
	PaymentStatus string     `json:"payment_status"`
	Subtotal      int64      `json:"subtotal"`
	Discount      int64      `json:"discount"`
	Total         int64      `json:"total"`
	Currency      string     `json:"currency"`
	PaidAt        *time.Time `json:"paid_at"`
	DeliveredAt   *time.Time `json:"delivered_at"`
	CreatedAt     time.Time  `json:"created_at"`
}

type adminSupplyOrderItemSummary struct {
	ID          uuid.UUID  `json:"id"`
	ProductID   uuid.UUID  `json:"product_id"`
	VariantID   *uuid.UUID `json:"variant_id"`
	ProductName string     `json:"product_name"`
	VariantName string     `json:"variant_name"`
	UnitPrice   int64      `json:"unit_price"`
	Currency    string     `json:"currency"`
	Quantity    int        `json:"quantity"`
}

func (h Handler) AdminSupplyProcurementDetail(c *gin.Context) {
	procurementID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, 422, 42512, "error.purchase_order_id_invalid")
		return
	}
	items := make([]adminSupplyProcurementItem, 0, 1)
	if err := supplyProcurementListQuery(h.DB).Where("po.id = ?", procurementID).Limit(1).Scan(&items).Error; err != nil || len(items) == 0 {
		response.Error(c, 404, 40509, "error.purchase_order_not_found")
		return
	}
	procurement := items[0]
	procurement.RetryMessage = procurementRetryMessage(procurement.Status, procurement.Attempts, procurement.NextPollAt, i18n.ResolveLocale(c))
	var order adminSupplyOrderSummary
	if err := h.DB.Model(&model.Order{}).Select("id", "order_no", "status", "payment_status", "subtotal", "discount", "total", "currency", "paid_at", "delivered_at", "created_at").First(&order, "id = ?", procurement.OrderID).Error; err != nil {
		response.Error(c, 404, 40510, "error.related_order_not_found")
		return
	}
	var item adminSupplyOrderItemSummary
	if err := h.DB.Model(&model.OrderItem{}).Select("id", "product_id", "variant_id", "product_name", "variant_name", "unit_price", "currency", "quantity").First(&item, "id = ?", procurement.OrderItemID).Error; err != nil {
		response.Error(c, 404, 40511, "error.related_order_item_not_found")
		return
	}
	h.audit(c, "supplier.procurement.view", "procurement_order", procurement.ID.String(), "查看脱敏采购与关联订单摘要")
	c.Header("Cache-Control", "no-store")
	response.OK(c, gin.H{"procurement": procurement, "order": order, "item": item})
}

type procurementRecoveryRequest struct {
	Evidence string `json:"evidence"`
}

type procurementManualCompletionRequest struct {
	Deliveries []string `json:"deliveries"`
	CostAmount *int64   `json:"cost_amount"`
	Evidence   string   `json:"evidence"`
}

var (
	errProcurementNotRecoverable = errors.New("procurement is not recoverable")
	errProcurementItemFulfilled  = errors.New("procurement item is already fulfilled")
	errProcurementRefundActive   = errors.New("procurement order has an active refund")
)

func validProcurementEvidence(value string) (string, bool) {
	value = strings.TrimSpace(value)
	length := len([]rune(value))
	return value, length >= 4 && length <= 1000 && !strings.ContainsRune(value, '\x00')
}

func ignoreAdminSupplierCallbacks(tx *gorm.DB, procurementID uuid.UUID, reason string, at time.Time) error {
	return tx.Model(&model.WebhookEvent{}).
		Where("procurement_order_id = ? AND status IN ?", procurementID, []string{"queued", "processing"}).
		Updates(map[string]any{"status": "ignored", "response": reason, "processed_at": &at}).Error
}

func (h Handler) RetrySupplyProcurement(c *gin.Context) {
	procurementID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, 422, 42512, "error.purchase_order_id_invalid")
		return
	}
	reason, ok := requireAdminChangeReason(c, "重试失败采购")
	if !ok {
		return
	}
	var req procurementRecoveryRequest
	if decodeStrictJSON(c, &req) != nil {
		response.Error(c, 422, 42513, "error.procurement_recovery_fields_invalid")
		return
	}
	req.Evidence, ok = validProcurementEvidence(req.Evidence)
	if !ok {
		response.Error(c, 422, 42513, "error.procurement_recovery_evidence_invalid")
		return
	}
	var procurement model.ProcurementOrder
	var order model.Order
	var snapshot model.ProcurementOrder
	if err := h.DB.Select("id", "order_id", "order_item_id").First(&snapshot, "id = ?", procurementID).Error; err != nil {
		response.Error(c, 404, 40509, "error.purchase_order_not_found")
		return
	}
	now := time.Now().UTC()
	err = h.DB.Transaction(func(tx *gorm.DB) error {
		var item model.OrderItem
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&item, "id = ?", snapshot.OrderItemID).Error; err != nil {
			return err
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&procurement, "id = ?", procurementID).Error; err != nil {
			return err
		}
		if procurement.Status != "failed" && procurement.Status != "cancelled" {
			return errProcurementNotRecoverable
		}
		if procurement.OrderItemID != item.ID || procurement.OrderID != snapshot.OrderID {
			return errProcurementNotRecoverable
		}
		if len(item.CardCiphertext) > 0 || len(item.DeliveryItemsCipher) > 0 {
			return errProcurementItemFulfilled
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&order, "id = ?", procurement.OrderID).Error; err != nil {
			return err
		}
		if order.PaymentStatus != "paid" || (order.Status != "failed" && order.Status != "processing") {
			return errProcurementNotRecoverable
		}
		refundActive, err := orderHasActiveRefund(tx, order.ID)
		if err != nil {
			return err
		}
		if refundActive {
			return errProcurementRefundActive
		}
		// A failed procurement releases every outstanding supplier hold. The
		// worker retries every unfinished line in the order, so capacity for all
		// of those lines must be re-acquired atomically before re-queueing.
		if err := service.RestoreSupplierInventoryReservationsTx(tx, order.ID); err != nil {
			return err
		}
		if order.Status == "failed" {
			if err := tx.Model(&order).Update("status", "processing").Error; err != nil {
				return err
			}
			if err := tx.Create(&model.OrderEvent{OrderID: order.ID, FromStatus: "failed", ToStatus: "processing", ActorType: "admin", Reason: "supplier procurement recovery queued"}).Error; err != nil {
				return err
			}
			order.Status = "processing"
		}
		if err := tx.Model(&procurement).Updates(map[string]any{"status": "retrying", "next_poll_at": &now, "completed_at": nil}).Error; err != nil {
			return err
		}
		return tx.Create(&model.FulfillmentAttempt{
			OrderID: order.ID, OrderItemID: procurement.OrderItemID, Mode: "supplier_admin_retry",
			Attempt: procurement.Attempts + 1, Status: "queued", SupplierID: &procurement.SupplierID,
			ExternalOrder: procurement.ExternalOrderNo, ErrorMessage: req.Evidence, StartedAt: now,
		}).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		response.Error(c, 404, 40509, "error.purchase_order_not_found")
		return
	}
	if errors.Is(err, errProcurementNotRecoverable) || errors.Is(err, errProcurementItemFulfilled) || errors.Is(err, errProcurementRefundActive) {
		response.Error(c, 409, 40513, "error.procurement_state_not_recoverable")
		return
	}
	if errors.Is(err, service.ErrInsufficientStock) || errors.Is(err, service.ErrProductUnavailable) {
		response.Error(c, 409, 40514, "error.insufficient_stock")
		return
	}
	if err != nil {
		response.Error(c, 500, 50506, "error.procurement_retry_failed")
		return
	}
	queueState := "queued"
	client := queue.NewClient(h.Cfg, h.DB)
	_, enqueueErr := client.Enqueue(queue.TypeSupplierPurchase, map[string]string{"order_id": order.ID.String()}, asynq.Queue("critical"), asynq.Unique(50*time.Second))
	_ = client.Close()
	if isDuplicateSupplyTask(enqueueErr) {
		queueState = "already_queued"
	} else if enqueueErr != nil {
		queueState = "scheduler_recovery"
	}
	h.audit(c, "supplier.procurement.retry", "procurement_order", procurement.ID.String(), fmt.Sprintf("%s；evidence=%s；queue_state=%s", reason, req.Evidence, queueState))
	c.JSON(http.StatusAccepted, response.Envelope{Code: 0, Message: "accepted", Data: gin.H{"status": "retrying", "queue_state": queueState}})
}

func (h Handler) CompleteSupplyProcurementManually(c *gin.Context) {
	procurementID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, 422, 42512, "error.purchase_order_id_invalid")
		return
	}
	reason, ok := requireAdminChangeReason(c, "人工补偿采购交付")
	if !ok {
		return
	}
	var req procurementManualCompletionRequest
	if decodeStrictJSON(c, &req) != nil {
		response.Error(c, 422, 42514, "error.procurement_manual_delivery_fields_invalid")
		return
	}
	req.Evidence, ok = validProcurementEvidence(req.Evidence)
	if !ok || req.CostAmount == nil || *req.CostAmount < 0 || *req.CostAmount > 1_000_000_000_000 {
		response.Error(c, 422, 42514, "error.procurement_manual_delivery_fields_invalid")
		return
	}
	var snapshot model.ProcurementOrder
	if err := h.DB.First(&snapshot, "id = ?", procurementID).Error; err != nil {
		response.Error(c, 404, 40509, "error.purchase_order_not_found")
		return
	}
	if !supply.ValidateDeliveries(req.Deliveries, snapshot.Quantity) {
		response.Error(c, 422, 42515, "error.procurement_manual_delivery_content_invalid")
		return
	}
	var itemSnapshot model.OrderItem
	if err := h.DB.Select("id", "product_id").First(&itemSnapshot, "id = ?", snapshot.OrderItemID).Error; err != nil {
		response.Error(c, 404, 40511, "error.related_order_item_not_found")
		return
	}
	delivery := strings.Join(req.Deliveries, "\n")
	ciphertext, nonce, _, err := h.Vault.Encrypt(delivery, itemSnapshot.ProductID[:])
	if err != nil {
		response.Error(c, 500, 50507, "error.procurement_manual_delivery_encrypt_failed")
		return
	}
	deliveryItemsCipher, deliveryItemsNonce, err := service.EncryptDeliveryItems(h.Vault, itemSnapshot.ID, req.Deliveries)
	if err != nil {
		response.Error(c, 500, 50507, "error.procurement_manual_delivery_encrypt_failed")
		return
	}
	adminID, _ := uuid.Parse(c.GetString("subject"))
	completedAt := time.Now().UTC()
	var order model.Order
	delivered := false
	err = h.DB.Transaction(func(tx *gorm.DB) error {
		var item model.OrderItem
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&item, "id = ?", snapshot.OrderItemID).Error; err != nil {
			return err
		}
		var procurement model.ProcurementOrder
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&procurement, "id = ?", procurementID).Error; err != nil {
			return err
		}
		if procurement.Status != "failed" && procurement.Status != "cancelled" {
			return errProcurementNotRecoverable
		}
		if procurement.OrderItemID != item.ID || procurement.OrderID != snapshot.OrderID {
			return errProcurementNotRecoverable
		}
		if len(item.CardCiphertext) > 0 || len(item.DeliveryItemsCipher) > 0 {
			return errProcurementItemFulfilled
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&order, "id = ?", procurement.OrderID).Error; err != nil {
			return err
		}
		if order.PaymentStatus != "paid" || (order.Status != "failed" && order.Status != "processing") {
			return errProcurementNotRecoverable
		}
		refundActive, err := orderHasActiveRefund(tx, order.ID)
		if err != nil {
			return err
		}
		if refundActive {
			return errProcurementRefundActive
		}
		if order.Status == "failed" {
			if err := tx.Model(&order).Update("status", "processing").Error; err != nil {
				return err
			}
			if err := tx.Create(&model.OrderEvent{OrderID: order.ID, FromStatus: "failed", ToStatus: "processing", ActorType: "admin", ActorID: &adminID, Reason: "manual supplier compensation started"}).Error; err != nil {
				return err
			}
			order.Status = "processing"
		}
		claim := tx.Model(&item).Where("card_ciphertext IS NULL").Updates(map[string]any{
			"card_ciphertext": ciphertext, "card_nonce": nonce,
			"delivery_items_cipher": deliveryItemsCipher, "delivery_items_nonce": deliveryItemsNonce,
			"card_preview": security.SecretPreview(delivery),
		})
		if claim.Error != nil {
			return claim.Error
		}
		if claim.RowsAffected != 1 {
			return errProcurementItemFulfilled
		}
		responseSummary, _ := json.Marshal(map[string]any{"source": "manual_compensation", "evidence": req.Evidence, "delivery_count": len(req.Deliveries)})
		if err := tx.Model(&procurement).Updates(map[string]any{
			"status": "completed", "cost_amount": *req.CostAmount, "response_body": string(responseSummary),
			"completed_at": &completedAt, "next_poll_at": nil,
		}).Error; err != nil {
			return err
		}
		if err := tx.Create(&model.FulfillmentAttempt{
			OrderID: order.ID, OrderItemID: item.ID, Mode: "supplier_manual_compensation",
			Attempt: procurement.Attempts + 1, Status: "succeeded", SupplierID: &procurement.SupplierID,
			ExternalOrder: procurement.ExternalOrderNo, ErrorMessage: req.Evidence,
			StartedAt: completedAt, FinishedAt: &completedAt,
		}).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.Product{}).Where("id = ?", item.ProductID).UpdateColumn("sold_count", gorm.Expr("sold_count + ?", item.Quantity)).Error; err != nil {
			return err
		}
		if err := ignoreAdminSupplierCallbacks(tx, procurement.ID, "procurement manually compensated", completedAt); err != nil {
			return err
		}
		var outstanding int64
		if err := tx.Model(&model.OrderItem{}).Where("order_id = ? AND card_ciphertext IS NULL", order.ID).Count(&outstanding).Error; err != nil {
			return err
		}
		if outstanding > 0 {
			return nil
		}
		if err := tx.Model(&order).Updates(map[string]any{"status": "delivered", "delivered_at": &completedAt}).Error; err != nil {
			return err
		}
		if err := tx.Create(&model.OrderEvent{OrderID: order.ID, FromStatus: "processing", ToStatus: "delivered", ActorType: "admin", ActorID: &adminID, Reason: "manual supplier compensation completed"}).Error; err != nil {
			return err
		}
		order.Status, order.DeliveredAt, delivered = "delivered", &completedAt, true
		if err := service.CreateAffiliateCommissionTx(tx, order, completedAt); err != nil {
			return err
		}
		if err := service.CreditResellerMarginTx(tx, order); err != nil {
			return err
		}
		if order.UserID != nil {
			_, _, err := service.ReconcileUserMembershipTx(tx, *order.UserID, completedAt)
			return err
		}
		return nil
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		response.Error(c, 404, 40509, "error.purchase_order_not_found")
		return
	}
	if errors.Is(err, errProcurementNotRecoverable) || errors.Is(err, errProcurementItemFulfilled) || errors.Is(err, errProcurementRefundActive) {
		response.Error(c, 409, 40513, "error.procurement_state_not_recoverable")
		return
	}
	if err != nil {
		response.Error(c, 500, 50508, "error.procurement_manual_delivery_failed")
		return
	}
	deliveryDispatch := "not_ready"
	if delivered {
		deliveryDispatch = "queued"
		if err := h.dispatchOrderDelivery(order.ID); err != nil {
			deliveryDispatch = "scheduler_recovery"
		}
	}
	h.audit(c, "supplier.procurement.manual-complete", "procurement_order", procurementID.String(), fmt.Sprintf("%s；evidence=%s；delivery_count=%d；delivery_dispatch=%s", reason, req.Evidence, len(req.Deliveries), deliveryDispatch))
	response.OK(c, gin.H{"status": "completed", "order_delivered": delivered, "delivery_dispatch": deliveryDispatch})
}
