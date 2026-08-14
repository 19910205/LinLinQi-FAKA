package handler

import (
	"errors"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"linlinqi/api/internal/model"
	"linlinqi/api/pkg/response"
)

type openAPIBalanceDTO struct {
	Balance          int64     `json:"balance"`
	Frozen           int64     `json:"frozen"`
	AvailableBalance int64     `json:"available_balance"`
	Currency         string    `json:"currency"`
	MinorUnit        int       `json:"minor_unit"`
	UpdatedAt        time.Time `json:"updated_at"`
}

func openAPIBalanceFromWallet(wallet model.WalletAccount) (openAPIBalanceDTO, error) {
	currency := strings.ToUpper(strings.TrimSpace(wallet.Currency))
	if len(currency) != 3 || wallet.Balance < 0 || wallet.Frozen < 0 || wallet.Frozen > wallet.Balance || wallet.UpdatedAt.IsZero() {
		return openAPIBalanceDTO{}, errors.New("wallet balance snapshot is invalid")
	}
	return openAPIBalanceDTO{
		Balance: wallet.Balance, Frozen: wallet.Frozen, AvailableBalance: wallet.Balance - wallet.Frozen,
		Currency: currency, UpdatedAt: wallet.UpdatedAt.UTC(),
	}, nil
}

// OpenAPIBalance exposes the actual billing wallet bound to the signed API
// credential. The orders:write scope is required because this is the same
// account that the credential is authorized to spend from.
func (h Handler) OpenAPIBalance(c *gin.Context) {
	credentialID, err := uuid.Parse(c.GetString("api_credential_id"))
	if err != nil {
		response.Error(c, 401, 40121, "error.invalid_api_credential")
		return
	}
	var credential model.APICredential
	if err := h.DB.Select("id", "owner_type", "owner_id").First(&credential, "id = ?", credentialID).Error; err != nil {
		response.Error(c, 401, 40121, "error.invalid_api_credential")
		return
	}
	if credential.OwnerID == nil {
		response.Error(c, 403, 40321, "error.api_credential_no_billing_account")
		return
	}
	ownerType := strings.TrimSpace(credential.OwnerType)
	if ownerType == "" {
		ownerType = "user"
	}
	requestedCurrency, specified, currencyErr := optionalCurrencyQuery(c)
	if currencyErr != nil {
		response.Error(c, 422, 42221, "error.currency_code_invalid")
		return
	}
	definition, currencyErr := resolveEnabledCurrencyDefinition(h.DB, requestedCurrency, !specified)
	if errors.Is(currencyErr, errCurrencySelectionInvalid) {
		response.Error(c, 422, 42221, "error.currency_code_invalid")
		return
	}
	if errors.Is(currencyErr, errCurrencySelectionUnavailable) {
		response.Error(c, 422, 42221, "error.currency_unavailable")
		return
	}
	if currencyErr != nil {
		response.Error(c, 500, 50022, "error.currency_definition_fetch_failed")
		return
	}
	var wallet model.WalletAccount
	err = h.DB.Where("owner_type = ? AND owner_id = ? AND currency = ?", ownerType, *credential.OwnerID, definition.Code).First(&wallet).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		wallet = model.WalletAccount{OwnerType: ownerType, OwnerID: *credential.OwnerID, Currency: definition.Code}
		wallet.UpdatedAt = time.Now().UTC()
		err = nil
	}
	if err != nil {
		response.Error(c, 500, 50022, "error.api_balance_fetch_failed")
		return
	}
	result, err := openAPIBalanceFromWallet(wallet)
	if err != nil {
		response.Error(c, 500, 50023, "error.api_balance_snapshot_invalid")
		return
	}
	result.MinorUnit = definition.MinorUnit
	c.Header("Cache-Control", "no-store")
	response.OK(c, result)
}
