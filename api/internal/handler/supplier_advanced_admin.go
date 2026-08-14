package handler

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"linlinqi/api/internal/model"
	"linlinqi/api/internal/supply"
	"linlinqi/api/pkg/response"
)

func (h Handler) loadAdminSupplierGateway(ctx context.Context, supplierID uuid.UUID) (model.Supplier, supply.Gateway, error) {
	var item model.Supplier
	db := h.DB.WithContext(ctx)
	if err := db.First(&item, "id = ?", supplierID).Error; err != nil {
		return item, nil, err
	}
	protocol, err := supplierProtocol(item.Protocol)
	if err != nil || item.Status != "active" {
		return item, nil, errors.New("supplier is not executable")
	}
	credentials, err := loadSupplierProbeCredentials(h.Vault, item, protocol)
	if err != nil {
		return item, nil, err
	}
	balanceCurrency := strings.ToUpper(strings.TrimSpace(item.BalanceCurrency))
	if balanceCurrency == "" {
		balanceCurrency = strings.ToUpper(strings.TrimSpace(item.PriceCurrency))
	}
	var balanceDefinition model.CurrencyDefinition
	if err := db.Select("code", "minor_unit").First(&balanceDefinition, "code = ? AND enabled = ?", balanceCurrency, true).Error; err != nil {
		return item, nil, err
	}
	money := supply.MoneySpec{
		PriceCurrency: strings.ToUpper(strings.TrimSpace(item.PriceCurrency)), PriceMinorUnit: item.PriceMinorUnit,
		BalanceCurrency: balanceCurrency, BalanceMinorUnit: balanceDefinition.MinorUnit,
	}
	gateway, err := supply.NewGatewayWithMoney(protocol, item.BaseURL, credentials, h.Cfg.Env != "production", money)
	return item, gateway, err
}

func validAdvancedSupplierParameters(parameters map[string]string) bool {
	if len(parameters) > 20 {
		return false
	}
	for key, value := range parameters {
		key = strings.TrimSpace(key)
		if key == "" || len([]rune(key)) > 100 || len(value) > 10_000 || strings.ContainsRune(value, '\x00') {
			return false
		}
		for _, character := range key {
			if character == '\x00' || unicode.IsControl(character) {
				return false
			}
		}
	}
	return true
}

type adminSupplierQuoteRequest struct {
	ExternalProductID string            `json:"external_product_id"`
	Quantity          int               `json:"quantity"`
	Parameters        map[string]string `json:"parameters"`
}

func (h Handler) AdminSupplierQuote(c *gin.Context) {
	supplierID, err := uuid.Parse(c.Param("id"))
	if err != nil || supplierID == uuid.Nil {
		response.Error(c, 422, 42504, "error.supplier_id_invalid")
		return
	}
	reason, ok := requireAdminChangeReason(c, "读取上游实时报价")
	if !ok {
		return
	}
	var req adminSupplierQuoteRequest
	if decodeStrictJSON(c, &req) != nil {
		response.Error(c, 422, 42524, "error.supplier_import_fields_invalid")
		return
	}
	req.ExternalProductID, err = supply.NormalizeExternalID(req.ExternalProductID)
	if err != nil || req.Quantity < 1 || req.Quantity > 1_000_000 || !validAdvancedSupplierParameters(req.Parameters) {
		response.Error(c, 422, 42524, "error.supplier_import_fields_invalid")
		return
	}
	item, gateway, err := h.loadAdminSupplierGateway(c.Request.Context(), supplierID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		response.Error(c, 404, 40502, "error.supplier_not_found")
		return
	}
	quoter, supported := gateway.(supply.PriceQuoter)
	if err != nil || !supported {
		response.Error(c, 409, 42507, "error.supplier_protocol_runtime_unavailable")
		return
	}
	callContext, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer cancel()
	quote, err := quoter.Quote(callContext, supply.QuoteRequest{ExternalProductID: req.ExternalProductID, Quantity: req.Quantity, Parameters: req.Parameters})
	if err != nil {
		response.Error(c, 502, 50507, "error.supplier_probe_failed")
		return
	}
	h.audit(c, "supplier.quote", "supplier", item.ID.String(), reason+"；external_product_id="+req.ExternalProductID)
	c.Header("Cache-Control", "no-store")
	response.OK(c, quote)
}

type adminSupplierDraftRequest struct {
	ExternalProductID string            `json:"external_product_id"`
	Page              int               `json:"page"`
	PageSize          int               `json:"page_size"`
	Parameters        map[string]string `json:"parameters"`
}

func (h Handler) AdminSupplierDraftCards(c *gin.Context) {
	supplierID, err := uuid.Parse(c.Param("id"))
	if err != nil || supplierID == uuid.Nil {
		response.Error(c, 422, 42504, "error.supplier_id_invalid")
		return
	}
	reason, ok := requireAdminChangeReason(c, "读取上游预选卡草稿")
	if !ok {
		return
	}
	var req adminSupplierDraftRequest
	if decodeStrictJSON(c, &req) != nil {
		response.Error(c, 422, 42524, "error.supplier_import_fields_invalid")
		return
	}
	req.ExternalProductID, err = supply.NormalizeExternalID(req.ExternalProductID)
	if req.Page == 0 {
		req.Page = 1
	}
	if req.PageSize == 0 {
		req.PageSize = 20
	}
	if err != nil || req.Page < 1 || req.Page > 1_000_000 || req.PageSize < 1 || req.PageSize > 100 || !validAdvancedSupplierParameters(req.Parameters) {
		response.Error(c, 422, 42524, "error.supplier_import_fields_invalid")
		return
	}
	item, gateway, err := h.loadAdminSupplierGateway(c.Request.Context(), supplierID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		response.Error(c, 404, 40502, "error.supplier_not_found")
		return
	}
	reader, supported := gateway.(supply.DraftCardReader)
	if err != nil || !supported {
		response.Error(c, 409, 42507, "error.supplier_protocol_runtime_unavailable")
		return
	}
	callContext, cancel := context.WithTimeout(c.Request.Context(), 20*time.Second)
	defer cancel()
	drafts, err := reader.DraftCards(callContext, supply.DraftCardRequest{ExternalProductID: req.ExternalProductID, Page: req.Page, PageSize: req.PageSize, Parameters: req.Parameters})
	if err != nil {
		response.Error(c, 502, 50507, "error.supplier_probe_failed")
		return
	}
	h.audit(c, "supplier.draft_cards", "supplier", item.ID.String(), reason+"；external_product_id="+req.ExternalProductID+"；count="+strconv.Itoa(len(drafts.Items)))
	c.Header("Cache-Control", "no-store")
	response.OK(c, drafts)
}
