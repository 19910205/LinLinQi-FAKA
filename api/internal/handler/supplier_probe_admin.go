package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"linlinqi/api/internal/model"
	"linlinqi/api/internal/supply"
	"linlinqi/api/pkg/response"
)

var errSupplierProbeStale = errors.New("supplier connection changed while probe was running")

// supplierProbeOperation is intentionally a small, stable DTO. Upstream
// response bodies and error strings are never returned because they may carry
// credentials, card data or provider internals.
type supplierProbeOperation struct {
	Capability string `json:"capability"`
	Status     string `json:"status"`
	DurationMS int64  `json:"duration_ms"`
	Count      int    `json:"count,omitempty"`
	Currency   string `json:"currency,omitempty"`
	Reason     string `json:"reason,omitempty"`
}

type supplierProbeResult struct {
	SupplierID       uuid.UUID                `json:"supplier_id"`
	Protocol         string                   `json:"protocol"`
	RuntimeAvailable bool                     `json:"runtime_available"`
	Executable       bool                     `json:"executable"`
	HealthStatus     string                   `json:"health_status"`
	CheckedAt        time.Time                `json:"checked_at"`
	DurationMS       int64                    `json:"duration_ms"`
	Operations       []supplierProbeOperation `json:"operations"`
	Balance          *supply.BalanceSnapshot  `json:"balance,omitempty"`
	Error            string                   `json:"error,omitempty"`
}

func loadSupplierProbeCredentials(vault interface {
	Decrypt([]byte, []byte, []byte) (string, error)
}, item model.Supplier, protocol string) (map[string]string, error) {
	protocol = strings.ToLower(strings.TrimSpace(protocol))
	if len(item.CredentialsCipher) > 0 && len(item.CredentialsNonce) > 0 {
		plaintext, err := vault.Decrypt(item.CredentialsCipher, item.CredentialsNonce, append(item.ID[:], []byte("supplier-credentials-v1")...))
		if err != nil {
			return nil, errors.New("credentials decrypt failed")
		}
		var credentials map[string]string
		if err := json.Unmarshal([]byte(plaintext), &credentials); err != nil {
			return nil, errors.New("credentials payload invalid")
		}
		return supply.ValidateCredentials(protocol, credentials)
	}
	if protocol != "linlinqi-standard" || len(item.APIKeyCipher) == 0 || len(item.APISecretCipher) == 0 {
		return nil, errors.New("credentials are not configured")
	}
	key, err := vault.Decrypt(item.APIKeyCipher, item.APIKeyNonce, append(item.ID[:], []byte("api-key")...))
	if err != nil {
		return nil, errors.New("legacy api key decrypt failed")
	}
	secret, err := vault.Decrypt(item.APISecretCipher, item.APISecretNonce, append(item.ID[:], []byte("api-secret")...))
	if err != nil {
		return nil, errors.New("legacy api secret decrypt failed")
	}
	return supply.ValidateCredentials(protocol, map[string]string{"api_key": key, "api_secret": secret})
}

func probeOperationContext(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(parent, 20*time.Second)
}

func probeReason(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, supply.ErrCapabilityUnsupported) {
		return "capability_unsupported"
	}
	return "upstream_request_failed"
}

func (h Handler) AdminSupplierProbe(c *gin.Context) {
	supplierID, err := uuid.Parse(c.Param("id"))
	if err != nil || supplierID == uuid.Nil {
		response.Error(c, 422, 42504, "error.supplier_id_invalid")
		return
	}
	reason, ok := requireAdminChangeReason(c, "测试供应商连接")
	if !ok {
		return
	}
	var item model.Supplier
	if err := h.DB.First(&item, "id = ?", supplierID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			response.Error(c, 404, 40502, "error.supplier_not_found")
		} else {
			response.Error(c, 500, 50507, "error.supplier_probe_failed")
		}
		return
	}
	protocol, protocolErr := supplierProtocol(item.Protocol)
	if protocolErr != nil {
		response.Error(c, 409, 42507, "error.supplier_protocol_runtime_unavailable")
		return
	}
	definition, exists := supply.Protocol(protocol)
	if !exists || !supply.RuntimeAvailable(protocol) || !supply.Executable(protocol) {
		response.Error(c, 409, 42507, "error.supplier_protocol_runtime_unavailable")
		return
	}
	credentials, credentialErr := loadSupplierProbeCredentials(h.Vault, item, protocol)
	if credentialErr != nil {
		response.Error(c, 422, 42506, "error.supplier_credentials_invalid_for_protocol")
		return
	}
	balanceCurrency := strings.ToUpper(strings.TrimSpace(item.BalanceCurrency))
	if balanceCurrency == "" {
		balanceCurrency = strings.ToUpper(strings.TrimSpace(item.PriceCurrency))
	}
	var balanceDefinition model.CurrencyDefinition
	if err := h.DB.Select("code", "minor_unit").First(&balanceDefinition, "code = ? AND enabled = ?", balanceCurrency, true).Error; err != nil {
		response.Error(c, 422, 42502, "error.supplier_currency_invalid")
		return
	}
	money := supply.MoneySpec{PriceCurrency: strings.ToUpper(strings.TrimSpace(item.PriceCurrency)), PriceMinorUnit: item.PriceMinorUnit, BalanceCurrency: balanceCurrency, BalanceMinorUnit: balanceDefinition.MinorUnit}
	gateway, gatewayErr := supply.NewGatewayWithMoney(protocol, item.BaseURL, credentials, h.Cfg.Env != "production", money)
	checkedAt := time.Now().UTC()
	result := supplierProbeResult{SupplierID: item.ID, Protocol: protocol, RuntimeAvailable: true, Executable: true, CheckedAt: checkedAt, Operations: []supplierProbeOperation{}}
	started := time.Now()
	if gatewayErr != nil {
		result.HealthStatus = "unreachable"
		result.Error = "gateway_initialization_failed"
		result.DurationMS = time.Since(started).Milliseconds()
		if persistErr := h.persistSupplierProbe(item, result); persistErr != nil {
			status := 500
			if errors.Is(persistErr, errSupplierProbeStale) {
				status = 409
			}
			response.Error(c, status, 50507, "error.supplier_probe_failed")
			return
		}
		h.audit(c, "supplier.probe", "supplier", item.ID.String(), reason+"；status=unreachable")
		response.OK(c, result)
		return
	}
	// Read-only operations only. Creating/cancelling an upstream order is
	// never part of a connection test.
	if containsProbeCapability(definition.Capabilities, "balance") {
		operationStarted := time.Now()
		ctx, cancel := probeOperationContext(c.Request.Context())
		balance, callErr := gateway.Balance(ctx)
		cancel()
		op := supplierProbeOperation{Capability: "balance", DurationMS: time.Since(operationStarted).Milliseconds()}
		if callErr != nil {
			op.Status, op.Reason = "failed", probeReason(callErr)
		} else if !strings.EqualFold(strings.TrimSpace(balance.Currency), balanceCurrency) || balance.Balance < 0 {
			op.Status, op.Reason = "failed", "currency_or_amount_invalid"
		} else {
			op.Status, op.Currency = "succeeded", strings.ToUpper(balance.Currency)
			result.Balance = &balance
		}
		result.Operations = append(result.Operations, op)
	}
	if containsProbeCapability(definition.Capabilities, "categories") {
		operationStarted := time.Now()
		ctx, cancel := probeOperationContext(c.Request.Context())
		categories, callErr := gateway.Categories(ctx)
		cancel()
		op := supplierProbeOperation{Capability: "categories", DurationMS: time.Since(operationStarted).Milliseconds()}
		if callErr != nil {
			op.Status, op.Reason = "failed", probeReason(callErr)
		} else {
			op.Status, op.Count = "succeeded", len(categories)
		}
		result.Operations = append(result.Operations, op)
	}
	if containsProbeCapability(definition.Capabilities, "products") {
		operationStarted := time.Now()
		ctx, cancel := probeOperationContext(c.Request.Context())
		products, callErr := gateway.Products(ctx)
		cancel()
		op := supplierProbeOperation{Capability: "products", DurationMS: time.Since(operationStarted).Milliseconds()}
		if callErr != nil {
			op.Status, op.Reason = "failed", probeReason(callErr)
		} else {
			op.Status, op.Count = "succeeded", len(products)
		}
		result.Operations = append(result.Operations, op)
	}
	result.DurationMS = time.Since(started).Milliseconds()
	succeeded, failed := 0, 0
	failedCapabilities := make([]string, 0)
	for _, operation := range result.Operations {
		switch operation.Status {
		case "succeeded":
			succeeded++
		case "failed":
			failed++
			failedCapabilities = append(failedCapabilities, operation.Capability)
		}
	}
	switch {
	case failed == 0:
		result.HealthStatus = "healthy"
	case succeeded > 0:
		result.HealthStatus = "degraded"
	default:
		result.HealthStatus = "unreachable"
	}
	if len(failedCapabilities) > 0 {
		result.Error = "failed_capabilities:" + strings.Join(failedCapabilities, ",")
	}
	if err := h.persistSupplierProbe(item, result); err != nil {
		if errors.Is(err, errSupplierProbeStale) {
			response.Error(c, 409, 50507, "error.supplier_probe_failed")
			return
		}
		response.Error(c, 500, 50507, "error.supplier_probe_failed")
		return
	}
	h.audit(c, "supplier.probe", "supplier", item.ID.String(), reason+"；status="+result.HealthStatus)
	c.Header("Cache-Control", "no-store")
	response.OK(c, result)
}

func containsProbeCapability(capabilities []string, target string) bool {
	for _, capability := range capabilities {
		if capability == target {
			return true
		}
	}
	return false
}

func sameSupplierProbeIdentity(current, probed model.Supplier) bool {
	return current.BaseURL == probed.BaseURL &&
		current.Protocol == probed.Protocol &&
		current.PriceCurrency == probed.PriceCurrency &&
		current.PriceMinorUnit == probed.PriceMinorUnit &&
		current.BalanceCurrency == probed.BalanceCurrency &&
		current.CurrencyMode == probed.CurrencyMode &&
		bytes.Equal(current.CredentialsCipher, probed.CredentialsCipher) &&
		bytes.Equal(current.CredentialsNonce, probed.CredentialsNonce) &&
		bytes.Equal(current.APIKeyCipher, probed.APIKeyCipher) &&
		bytes.Equal(current.APIKeyNonce, probed.APIKeyNonce) &&
		bytes.Equal(current.APISecretCipher, probed.APISecretCipher) &&
		bytes.Equal(current.APISecretNonce, probed.APISecretNonce)
}

func (h Handler) persistSupplierProbe(probed model.Supplier, result supplierProbeResult) error {
	updates := map[string]any{
		"health_status": result.HealthStatus, "last_probe_at": result.CheckedAt,
		"last_probe_latency_ms": result.DurationMS, "last_probe_error": result.Error,
	}
	if result.Balance != nil {
		updates["balance"] = result.Balance.Balance
		updates["balance_synced_at"] = result.Balance.UpdatedAt
	}
	return h.DB.Transaction(func(tx *gorm.DB) error {
		var supplierModel model.Supplier
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&supplierModel, "id = ?", probed.ID).Error; err != nil {
			return err
		}
		if !sameSupplierProbeIdentity(supplierModel, probed) {
			return errSupplierProbeStale
		}
		return tx.Model(&supplierModel).Updates(updates).Error
	})
}
