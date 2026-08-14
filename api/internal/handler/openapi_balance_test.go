package handler

import (
	"testing"
	"time"

	"linlinqi/api/internal/model"
)

func TestOpenAPIBalanceUsesRealWalletSnapshot(t *testing.T) {
	now := time.Now().UTC()
	result, err := openAPIBalanceFromWallet(model.WalletAccount{
		Currency: "cny", Balance: 12345, Frozen: 100, Base: model.Base{UpdatedAt: now},
	})
	if err != nil {
		t.Fatalf("valid wallet rejected: %v", err)
	}
	if result.Balance != 12345 || result.Currency != "CNY" || !result.UpdatedAt.Equal(now) {
		t.Fatalf("unexpected balance DTO: %#v", result)
	}
	for name, wallet := range map[string]model.WalletAccount{
		"negative balance": {Currency: "CNY", Balance: -1, Base: model.Base{UpdatedAt: now}},
		"negative frozen":  {Currency: "CNY", Balance: 1, Frozen: -1, Base: model.Base{UpdatedAt: now}},
		"frozen overflow":  {Currency: "CNY", Balance: 1, Frozen: 2, Base: model.Base{UpdatedAt: now}},
		"wrong currency":   {Currency: "USDT", Balance: 1, Base: model.Base{UpdatedAt: now}},
		"missing update":   {Currency: "CNY", Balance: 1},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := openAPIBalanceFromWallet(wallet); err == nil {
				t.Fatal("invalid wallet snapshot was exposed")
			}
		})
	}
}
