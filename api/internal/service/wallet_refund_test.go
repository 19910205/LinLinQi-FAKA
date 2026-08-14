package service

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	"linlinqi/api/internal/model"
)

func TestWalletOrderDebitIdentityUsesDurableBillingOwner(t *testing.T) {
	orderID, userID, resellerID, credentialID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	tests := []struct {
		name      string
		order     model.Order
		ownerType string
		ownerID   uuid.UUID
		entryNo   string
		entryType string
	}{
		{
			name: "storefront user", order: model.Order{Base: model.Base{ID: orderID}, PaymentMethod: "balance", UserID: &userID},
			ownerType: "user", ownerID: userID, entryNo: "LQW-STORE-" + orderID.String(), entryType: "order_payment",
		},
		{
			name: "OpenAPI user", order: model.Order{Base: model.Base{ID: orderID}, PaymentMethod: "supplier_balance", APICredentialID: &credentialID, UserID: &userID},
			ownerType: "user", ownerID: userID, entryNo: "LQW-API-" + orderID.String(), entryType: "api_order",
		},
		{
			name: "OpenAPI reseller", order: model.Order{Base: model.Base{ID: orderID}, PaymentMethod: "supplier_balance", APICredentialID: &credentialID, ResellerID: &resellerID},
			ownerType: "reseller", ownerID: resellerID, entryNo: "LQW-API-" + orderID.String(), entryType: "api_order",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			identity, err := walletOrderDebitIdentity(test.order)
			if err != nil {
				t.Fatalf("resolve debit identity: %v", err)
			}
			if identity.OwnerType != test.ownerType || identity.OwnerID != test.ownerID || identity.EntryNo != test.entryNo || identity.EntryType != test.entryType {
				t.Fatalf("wrong debit identity: %#v", identity)
			}
		})
	}
}

func TestWalletOrderDebitIdentityFailsClosedOnAmbiguousOwner(t *testing.T) {
	orderID, userID, resellerID, credentialID := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	invalid := []model.Order{
		{Base: model.Base{ID: orderID}, PaymentMethod: "balance"},
		{Base: model.Base{ID: orderID}, PaymentMethod: "supplier_balance", UserID: &userID},
		{Base: model.Base{ID: orderID}, PaymentMethod: "supplier_balance", APICredentialID: &credentialID},
		{Base: model.Base{ID: orderID}, PaymentMethod: "supplier_balance", APICredentialID: &credentialID, UserID: &userID, ResellerID: &resellerID},
	}
	for index, order := range invalid {
		if _, err := walletOrderDebitIdentity(order); !errors.Is(err, ErrWalletSettlementOwnerInvalid) {
			t.Fatalf("case %d did not fail closed: %v", index, err)
		}
	}
	if _, err := walletOrderDebitIdentity(model.Order{PaymentMethod: "signed_http"}); !errors.Is(err, ErrWalletOrderUnsupported) {
		t.Fatalf("external payment was treated as a wallet settlement: %v", err)
	}
}

func TestValidWalletOrderCurrency(t *testing.T) {
	for _, value := range []string{"CNY", "USD", "JPY"} {
		if !validWalletOrderCurrency(value) {
			t.Fatalf("valid currency rejected: %s", value)
		}
	}
	for _, value := range []string{"", "cny", "USDT", "C1Y", " CNY"} {
		if validWalletOrderCurrency(value) {
			t.Fatalf("invalid currency accepted: %q", value)
		}
	}
}
