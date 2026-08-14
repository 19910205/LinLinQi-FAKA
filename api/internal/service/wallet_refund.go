package service

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"linlinqi/api/internal/model"
)

var (
	ErrWalletOrderUnsupported       = errors.New("order was not settled by a local wallet")
	ErrWalletSettlementInvalid      = errors.New("wallet settlement audit is invalid")
	ErrWalletSettlementOwnerInvalid = errors.New("wallet settlement owner does not match the order")
)

// WalletOrderSettlement is the verified local-wallet payment audit used by a
// refund. AccountID is intentionally derived from the immutable debit entry;
// callers never choose the wallet receiving the credit.
type WalletOrderSettlement struct {
	AccountID       uuid.UUID
	DebitEntryID    uuid.UUID
	PaymentIntentID uuid.UUID
}

type walletDebitIdentity struct {
	EntryNo   string
	EntryType string
	OwnerType string
	OwnerID   uuid.UUID
}

func walletOrderDebitIdentity(order model.Order) (walletDebitIdentity, error) {
	switch strings.ToLower(strings.TrimSpace(order.PaymentMethod)) {
	case "balance":
		if order.UserID == nil || *order.UserID == uuid.Nil {
			return walletDebitIdentity{}, ErrWalletSettlementOwnerInvalid
		}
		return walletDebitIdentity{
			EntryNo: "LQW-STORE-" + order.ID.String(), EntryType: "order_payment",
			OwnerType: "user", OwnerID: *order.UserID,
		}, nil
	case "supplier_balance":
		if order.APICredentialID == nil || *order.APICredentialID == uuid.Nil {
			return walletDebitIdentity{}, ErrWalletSettlementOwnerInvalid
		}
		hasUser := order.UserID != nil && *order.UserID != uuid.Nil
		hasReseller := order.ResellerID != nil && *order.ResellerID != uuid.Nil
		if hasUser == hasReseller { // fail closed when the billing owner is missing or ambiguous
			return walletDebitIdentity{}, ErrWalletSettlementOwnerInvalid
		}
		var ownerType string
		var ownerID uuid.UUID
		if hasUser {
			ownerType, ownerID = "user", *order.UserID
		} else {
			ownerType, ownerID = "reseller", *order.ResellerID
		}
		return walletDebitIdentity{
			EntryNo: "LQW-API-" + order.ID.String(), EntryType: "api_order",
			OwnerType: ownerType, OwnerID: ownerID,
		}, nil
	default:
		return walletDebitIdentity{}, ErrWalletOrderUnsupported
	}
}

func walletOrderDebitIdentityFromCanonicalIntent(order model.Order) (walletDebitIdentity, error) {
	// This fallback is only used after EnsureWalletOrderPaymentAuditTx has
	// proved that the order already owns one terminal zero-channel payment
	// intent. It lets legacy rows with an empty/renamed payment_method remain
	// refundable without trusting that mutable label as the refund recipient.
	if order.APICredentialID != nil && *order.APICredentialID != uuid.Nil {
		hasUser := order.UserID != nil && *order.UserID != uuid.Nil
		hasReseller := order.ResellerID != nil && *order.ResellerID != uuid.Nil
		if hasUser == hasReseller {
			return walletDebitIdentity{}, ErrWalletSettlementOwnerInvalid
		}
		ownerType := "user"
		ownerID := uuid.Nil
		if hasUser {
			ownerID = *order.UserID
		} else {
			ownerType, ownerID = "reseller", *order.ResellerID
		}
		return walletDebitIdentity{
			EntryNo: "LQW-API-" + order.ID.String(), EntryType: "api_order",
			OwnerType: ownerType, OwnerID: ownerID,
		}, nil
	}
	if order.UserID == nil || *order.UserID == uuid.Nil || (order.ResellerID != nil && *order.ResellerID != uuid.Nil) {
		return walletDebitIdentity{}, ErrWalletSettlementOwnerInvalid
	}
	return walletDebitIdentity{
		EntryNo: "LQW-STORE-" + order.ID.String(), EntryType: "order_payment",
		OwnerType: "user", OwnerID: *order.UserID,
	}, nil
}

func validWalletOrderCurrency(value string) bool {
	trimmed := strings.TrimSpace(value)
	if value != trimmed || len(value) != 3 || value != strings.ToUpper(value) {
		return false
	}
	for _, character := range value {
		if character < 'A' || character > 'Z' {
			return false
		}
	}
	return true
}

// EnsureWalletOrderPaymentAuditTx proves a local wallet payment from its exact
// deterministic debit entry, validates the snapshotted owner and creates the
// canonical PaymentIntent/PaymentTransaction audit if an older OpenAPI order
// predates those rows. The caller must already hold the order row lock.
func EnsureWalletOrderPaymentAuditTx(tx *gorm.DB, order model.Order, at time.Time) (WalletOrderSettlement, error) {
	identity, err := walletOrderDebitIdentity(order)
	if err != nil {
		if !errors.Is(err, ErrWalletOrderUnsupported) {
			return WalletOrderSettlement{}, err
		}
		// A terminal zero-channel intent is an independent durable wallet
		// settlement fact. Require exactly one before inferring an old order's
		// deterministic debit identity from its snapshotted billing owner.
		var walletIntents []model.PaymentIntent
		if queryErr := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("order_id = ? AND channel_id = ? AND status IN ?", order.ID, uuid.Nil, []string{"succeeded", "partially_refunded", "refunded"}).
			Order("created_at DESC").Limit(2).Find(&walletIntents).Error; queryErr != nil {
			return WalletOrderSettlement{}, queryErr
		}
		if len(walletIntents) != 1 {
			return WalletOrderSettlement{}, err
		}
		identity, err = walletOrderDebitIdentityFromCanonicalIntent(order)
		if err != nil {
			return WalletOrderSettlement{}, err
		}
	}
	if order.ID == uuid.Nil || order.Total < 1 || !validWalletOrderCurrency(order.Currency) {
		return WalletOrderSettlement{}, ErrWalletSettlementInvalid
	}
	var debit model.WalletEntry
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("entry_no = ?", identity.EntryNo).First(&debit).Error; err != nil {
		return WalletOrderSettlement{}, fmt.Errorf("%w: original debit is missing", ErrWalletSettlementInvalid)
	}
	if debit.Type != identity.EntryType || debit.Amount != -order.Total || debit.ReferenceType != "order" || debit.ReferenceID == nil || *debit.ReferenceID != order.ID {
		return WalletOrderSettlement{}, fmt.Errorf("%w: original debit fields do not match", ErrWalletSettlementInvalid)
	}
	var account model.WalletAccount
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&account, "id = ?", debit.AccountID).Error; err != nil {
		return WalletOrderSettlement{}, fmt.Errorf("%w: wallet account is missing", ErrWalletSettlementInvalid)
	}
	if account.OwnerType != identity.OwnerType || account.OwnerID != identity.OwnerID || account.Currency != order.Currency {
		return WalletOrderSettlement{}, ErrWalletSettlementOwnerInvalid
	}

	providerEventID := "wallet:" + order.ID.String()
	var paymentTransaction model.PaymentTransaction
	transactionErr := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("provider_event_id = ?", providerEventID).First(&paymentTransaction).Error
	if transactionErr != nil && !errors.Is(transactionErr, gorm.ErrRecordNotFound) {
		return WalletOrderSettlement{}, transactionErr
	}

	var intent model.PaymentIntent
	if transactionErr == nil {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&intent, "id = ?", paymentTransaction.PaymentIntentID).Error; err != nil {
			return WalletOrderSettlement{}, fmt.Errorf("%w: payment intent is missing", ErrWalletSettlementInvalid)
		}
	} else {
		var intents []model.PaymentIntent
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("order_id = ? AND channel_id = ?", order.ID, uuid.Nil).Order("created_at DESC").Limit(2).Find(&intents).Error; err != nil {
			return WalletOrderSettlement{}, err
		}
		if len(intents) > 1 {
			return WalletOrderSettlement{}, fmt.Errorf("%w: multiple wallet payment intents", ErrWalletSettlementInvalid)
		}
		if len(intents) == 1 {
			intent = intents[0]
		} else {
			succeededAt := at.UTC()
			if order.PaidAt != nil {
				succeededAt = order.PaidAt.UTC()
			}
			intent = model.PaymentIntent{
				Base: model.Base{ID: uuid.New()}, OrderID: order.ID,
				IntentNo: "LQWI" + strings.ReplaceAll(order.ID.String(), "-", ""), ChannelID: uuid.Nil,
				Amount: order.Total, Currency: order.Currency, OrderAmount: order.Total, OrderCurrency: order.Currency,
				Status: "succeeded", ProviderTradeNo: providerEventID, ExpiresAt: succeededAt, SucceededAt: &succeededAt,
			}
			if err := tx.Create(&intent).Error; err != nil {
				return WalletOrderSettlement{}, err
			}
		}
	}

	if intent.OrderID != order.ID || intent.ChannelID != uuid.Nil || intent.Amount != order.Total || intent.OrderAmount != order.Total || intent.Currency != order.Currency || intent.OrderCurrency != order.Currency {
		return WalletOrderSettlement{}, fmt.Errorf("%w: payment intent fields do not match", ErrWalletSettlementInvalid)
	}
	switch intent.Status {
	case "succeeded", "partially_refunded", "refunded":
	default:
		return WalletOrderSettlement{}, fmt.Errorf("%w: payment intent status is %s", ErrWalletSettlementInvalid, intent.Status)
	}
	updates := map[string]any{}
	if intent.ProviderTradeNo == "" {
		updates["provider_trade_no"] = providerEventID
		intent.ProviderTradeNo = providerEventID
	} else if intent.ProviderTradeNo != providerEventID {
		return WalletOrderSettlement{}, fmt.Errorf("%w: payment reference does not match", ErrWalletSettlementInvalid)
	}
	if intent.SucceededAt == nil {
		succeededAt := at.UTC()
		if order.PaidAt != nil {
			succeededAt = order.PaidAt.UTC()
		}
		updates["succeeded_at"] = &succeededAt
		intent.SucceededAt = &succeededAt
	}
	if len(updates) > 0 {
		if err := tx.Model(&intent).Updates(updates).Error; err != nil {
			return WalletOrderSettlement{}, err
		}
	}

	if transactionErr == nil {
		if paymentTransaction.PaymentIntentID != intent.ID || paymentTransaction.Direction != "payment" || paymentTransaction.Amount != order.Total || paymentTransaction.Currency != order.Currency || paymentTransaction.Status != "succeeded" {
			return WalletOrderSettlement{}, fmt.Errorf("%w: payment transaction fields do not match", ErrWalletSettlementInvalid)
		}
	} else {
		paymentTransaction = model.PaymentTransaction{
			Base: model.Base{ID: uuid.New()}, PaymentIntentID: intent.ID, Direction: "payment",
			ProviderEventID: providerEventID, Amount: order.Total, Currency: order.Currency,
			Status: "succeeded", RawPayload: `{"provider":"wallet"}`,
		}
		if err := tx.Create(&paymentTransaction).Error; err != nil {
			return WalletOrderSettlement{}, err
		}
	}
	return WalletOrderSettlement{AccountID: account.ID, DebitEntryID: debit.ID, PaymentIntentID: intent.ID}, nil
}

// ApplyWalletOrderRefundTx performs an idempotent credit to the exact account
// proven by the original debit. It re-runs settlement validation so a caller
// cannot substitute a wallet account between refund creation and mutation.
func ApplyWalletOrderRefundTx(tx *gorm.DB, order model.Order, refund model.Refund, at time.Time) (*model.WalletEntry, error) {
	settlement, err := EnsureWalletOrderPaymentAuditTx(tx, order, at)
	if err != nil {
		return nil, err
	}
	if refund.ID == uuid.Nil || refund.OrderID != order.ID || refund.PaymentIntentID == nil || *refund.PaymentIntentID != settlement.PaymentIntentID ||
		refund.Amount != refund.OrderAmount || refund.Amount < 1 || refund.Currency != order.Currency || refund.OrderCurrency != order.Currency {
		return nil, ErrWalletSettlementInvalid
	}
	return ApplyWalletMutation(tx, WalletMutation{
		EntryNo: "LQW-REFUND-" + refund.ID.String(), AccountID: settlement.AccountID,
		Amount: refund.OrderAmount, Type: "order_refund", ReferenceType: "refund", ReferenceID: &refund.ID,
		Description: "钱包原路退款 " + order.OrderNo,
	})
}
