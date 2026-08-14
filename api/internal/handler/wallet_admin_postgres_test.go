package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"linlinqi/api/internal/model"
)

type walletAdminResponseEnvelope struct {
	Data json.RawMessage `json:"data"`
}

func walletAdminRequest(t *testing.T, h Handler, target string, userID *uuid.UUID) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodGet, target, nil)
	if userID != nil {
		context.Params = gin.Params{{Key: "id", Value: userID.String()}}
		h.AdminWalletCustomerDetail(context)
	} else {
		h.AdminWalletCustomers(context)
	}
	return recorder
}

func assertWalletResponseOmitsCustomerOperations(t *testing.T, body string) {
	t.Helper()
	for _, forbidden := range []string{
		"last_login_at",
		"order_count",
		"net_spend",
		"recent_orders",
		"sessions",
		"login_events",
		"membership",
		"member_level",
		"affiliate",
		"reseller",
		"statistics",
	} {
		if strings.Contains(body, `"`+forbidden+`"`) {
			t.Fatalf("wallet.view response leaked customer-operation field %q: %s", forbidden, body)
		}
	}
}

func TestAdminWalletCustomerEndpointsAreMinimalAndPaginatedPostgreSQL(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := isolatedSupplyAdminDB(t, "linlinqi_wallet_admin_test_")
	h := Handler{DB: db}
	now := time.Now().UTC().Truncate(time.Second)
	users := []model.User{
		{
			Base:         model.Base{ID: uuid.New(), CreatedAt: now.Add(-2 * time.Hour)},
			Email:        "wallet-contract-old@example.test",
			PasswordHash: "not-used",
			Nickname:     "Older wallet owner",
			Status:       "active",
			LastLoginAt:  now.Add(-time.Hour),
		},
		{
			Base:         model.Base{ID: uuid.New(), CreatedAt: now.Add(-time.Hour)},
			Email:        "wallet-contract-new@example.test",
			PasswordHash: "not-used",
			Nickname:     "Newer wallet owner",
			Status:       "disabled",
			LastLoginAt:  now.Add(-30 * time.Minute),
		},
	}
	if err := db.Create(&users).Error; err != nil {
		t.Fatalf("create wallet owners: %v", err)
	}
	wallet := model.WalletAccount{
		Base: model.Base{ID: uuid.New()}, OwnerType: "user", OwnerID: users[1].ID,
		Currency: "CNY", Balance: 12_345, Frozen: 678, Version: 2,
	}
	if err := db.Create(&wallet).Error; err != nil {
		t.Fatalf("create wallet: %v", err)
	}
	entries := []model.WalletEntry{
		{
			Base: model.Base{ID: uuid.New(), CreatedAt: now.Add(-2 * time.Minute)}, AccountID: wallet.ID,
			EntryNo: "wallet-contract-old-" + uuid.NewString(), Type: "adjustment", Amount: 345,
			BalanceAfter: 12_000, Description: "older wallet contract entry",
		},
		{
			Base: model.Base{ID: uuid.New(), CreatedAt: now.Add(-time.Minute)}, AccountID: wallet.ID,
			EntryNo: "wallet-contract-new-" + uuid.NewString(), Type: "adjustment", Amount: 345,
			BalanceAfter: 12_345, Description: "newer wallet contract entry",
		},
	}
	if err := db.Create(&entries).Error; err != nil {
		t.Fatalf("create wallet entries: %v", err)
	}

	listResponse := walletAdminRequest(t, h, "/admin/v1/wallets/users?q=wallet-contract&page=2&page_size=1", nil)
	if listResponse.Code != http.StatusOK {
		t.Fatalf("wallet list status=%d body=%s", listResponse.Code, listResponse.Body.String())
	}
	assertWalletResponseOmitsCustomerOperations(t, listResponse.Body.String())
	var listEnvelope walletAdminResponseEnvelope
	if err := json.Unmarshal(listResponse.Body.Bytes(), &listEnvelope); err != nil {
		t.Fatalf("decode wallet list envelope: %v", err)
	}
	var listPage struct {
		Items    []adminWalletCustomerListItem `json:"items"`
		Total    int64                         `json:"total"`
		Page     int                           `json:"page"`
		PageSize int                           `json:"page_size"`
	}
	if err := json.Unmarshal(listEnvelope.Data, &listPage); err != nil {
		t.Fatalf("decode wallet list page: %v", err)
	}
	if listPage.Total != 2 || listPage.Page != 2 || listPage.PageSize != 1 || len(listPage.Items) != 1 {
		t.Fatalf("wallet list pagination mismatch: %#v", listPage)
	}
	if listPage.Items[0].ID != users[0].ID || listPage.Items[0].Currency != "CNY" {
		t.Fatalf("wallet list ordering/currency mismatch: %#v", listPage.Items[0])
	}

	detailResponse := walletAdminRequest(t, h, "/admin/v1/wallets/users/"+users[1].ID.String()+"?page=2&page_size=1", &users[1].ID)
	if detailResponse.Code != http.StatusOK {
		t.Fatalf("wallet detail status=%d body=%s", detailResponse.Code, detailResponse.Body.String())
	}
	assertWalletResponseOmitsCustomerOperations(t, detailResponse.Body.String())
	var detailEnvelope walletAdminResponseEnvelope
	if err := json.Unmarshal(detailResponse.Body.Bytes(), &detailEnvelope); err != nil {
		t.Fatalf("decode wallet detail envelope: %v", err)
	}
	var detail struct {
		User          adminWalletUserSummary `json:"user"`
		Wallet        model.WalletAccount    `json:"wallet"`
		WalletEntries struct {
			Items    []model.WalletEntry `json:"items"`
			Total    int64               `json:"total"`
			Page     int                 `json:"page"`
			PageSize int                 `json:"page_size"`
		} `json:"wallet_entries"`
	}
	if err := json.Unmarshal(detailEnvelope.Data, &detail); err != nil {
		t.Fatalf("decode wallet detail: %v", err)
	}
	if detail.User.ID != users[1].ID || detail.Wallet.ID != wallet.ID || detail.Wallet.Balance != 12_345 {
		t.Fatalf("wallet detail identity/balance mismatch: %#v", detail)
	}
	if detail.WalletEntries.Total != 2 || detail.WalletEntries.Page != 2 || detail.WalletEntries.PageSize != 1 || len(detail.WalletEntries.Items) != 1 {
		t.Fatalf("wallet ledger pagination mismatch: %#v", detail.WalletEntries)
	}
	if detail.WalletEntries.Items[0].ID != entries[0].ID {
		t.Fatalf("wallet ledger ordering mismatch: %#v", detail.WalletEntries.Items[0])
	}

	missingID := uuid.New()
	missingResponse := walletAdminRequest(t, h, "/admin/v1/wallets/users/"+missingID.String(), &missingID)
	if missingResponse.Code != http.StatusNotFound {
		t.Fatalf("missing wallet owner status=%d body=%s", missingResponse.Code, missingResponse.Body.String())
	}
}
