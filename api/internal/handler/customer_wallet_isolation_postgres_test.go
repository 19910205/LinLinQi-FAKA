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

func assertJSONOmitsKeys(t *testing.T, body []byte, forbidden ...string) {
	t.Helper()
	var decoded any
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("decode JSON response: %v body=%s", err, body)
	}
	blocked := make(map[string]struct{}, len(forbidden))
	for _, key := range forbidden {
		blocked[key] = struct{}{}
	}
	var walk func(any)
	walk = func(value any) {
		switch typed := value.(type) {
		case map[string]any:
			for key, nested := range typed {
				if _, exists := blocked[key]; exists {
					t.Fatalf("response leaked forbidden key %q: %s", key, body)
				}
				walk(nested)
			}
		case []any:
			for _, nested := range typed {
				walk(nested)
			}
		}
	}
	walk(decoded)
}

func customerIsolationRequest(t *testing.T, h Handler, userID *uuid.UUID) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	if userID == nil {
		context.Request = httptest.NewRequest(http.MethodGet, "/admin/v1/users?q=customer-wallet-isolation", nil)
		h.AdminCustomers(context)
	} else {
		context.Request = httptest.NewRequest(http.MethodGet, "/admin/v1/users/"+userID.String(), nil)
		context.Params = gin.Params{{Key: "id", Value: userID.String()}}
		h.AdminCustomerDetail(context)
	}
	return recorder
}

func TestCustomerDTOsDoNotSerializeWalletFields(t *testing.T) {
	for name, value := range map[string]any{
		"list": adminCustomerListItem{ID: uuid.New(), Email: "customer@example.test"},
		"user": adminCustomerUserDTO{ID: uuid.New(), Email: "customer@example.test"},
	} {
		encoded, err := json.Marshal(value)
		if err != nil {
			t.Fatalf("marshal %s DTO: %v", name, err)
		}
		assertJSONOmitsKeys(t, encoded, "balance", "frozen", "wallet", "wallet_entries")
	}
}

func TestCustomerViewEndpointsAndExportsOmitWalletDataPostgreSQL(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := isolatedSupplyAdminDB(t, "linlinqi_customer_wallet_isolation_test_")
	h := Handler{DB: db}
	now := time.Now().UTC().Truncate(time.Second)
	user := model.User{
		Base:  model.Base{ID: uuid.New(), CreatedAt: now.Add(-time.Hour), UpdatedAt: now},
		Email: "customer-wallet-isolation@example.test", PasswordHash: "not-used", Nickname: "Isolation customer",
		Balance: 987_654, Status: "active", PreferredLocale: "zh-CN", LastLoginAt: now.Add(-time.Minute),
	}
	if err := db.Create(&user).Error; err != nil {
		t.Fatalf("create customer: %v", err)
	}
	wallet := model.WalletAccount{
		Base: model.Base{ID: uuid.New()}, OwnerType: "user", OwnerID: user.ID,
		Currency: "CNY", Balance: 123_456, Frozen: 789, Version: 4,
	}
	if err := db.Create(&wallet).Error; err != nil {
		t.Fatalf("create wallet: %v", err)
	}
	entry := model.WalletEntry{
		Base: model.Base{ID: uuid.New()}, AccountID: wallet.ID, EntryNo: "customer-wallet-isolation-" + uuid.NewString(),
		Type: "adjustment", Amount: 123_456, BalanceAfter: 123_456, Description: "must remain wallet scoped",
	}
	if err := db.Create(&entry).Error; err != nil {
		t.Fatalf("create wallet entry: %v", err)
	}

	for name, response := range map[string]*httptest.ResponseRecorder{
		"list":   customerIsolationRequest(t, h, nil),
		"detail": customerIsolationRequest(t, h, &user.ID),
	} {
		if response.Code != http.StatusOK {
			t.Fatalf("customer %s status=%d body=%s", name, response.Code, response.Body.String())
		}
		assertJSONOmitsKeys(t, response.Body.Bytes(), "balance", "frozen", "wallet", "wallet_entries")
	}

	customerExport := httptest.NewRecorder()
	customerContext, _ := gin.CreateTestContext(customerExport)
	customerContext.Request = httptest.NewRequest(http.MethodGet, "/admin/v1/users/export?q=customer-wallet-isolation", nil)
	customerContext.Request.Header.Set("X-Change-Reason", "customer export isolation contract")
	h.ExportAdminCustomers(customerContext)
	if customerExport.Code != http.StatusOK {
		t.Fatalf("customer export status=%d body=%s", customerExport.Code, customerExport.Body.String())
	}
	for _, forbidden := range []string{"钱包余额", "冻结金额", "1234.56", "7.89", "9876.54"} {
		if strings.Contains(customerExport.Body.String(), forbidden) {
			t.Fatalf("customer export leaked wallet value %q: %s", forbidden, customerExport.Body.String())
		}
	}

	walletExport := httptest.NewRecorder()
	walletContext, _ := gin.CreateTestContext(walletExport)
	walletContext.Request = httptest.NewRequest(http.MethodGet, "/admin/v1/wallets/users/export?q=customer-wallet-isolation", nil)
	walletContext.Request.Header.Set("X-Change-Reason", "wallet export isolation contract")
	h.ExportAdminWalletCustomers(walletContext)
	if walletExport.Code != http.StatusOK {
		t.Fatalf("wallet export status=%d body=%s", walletExport.Code, walletExport.Body.String())
	}
	for _, required := range []string{"钱包余额", "冻结金额", "1234.56", "7.89", "CNY"} {
		if !strings.Contains(walletExport.Body.String(), required) {
			t.Fatalf("wallet export omitted %q: %s", required, walletExport.Body.String())
		}
	}
	for _, forbidden := range []string{"订单数", "净消费", "最近登录", "9876.54"} {
		if strings.Contains(walletExport.Body.String(), forbidden) {
			t.Fatalf("wallet export leaked customer-operation value %q: %s", forbidden, walletExport.Body.String())
		}
	}
	var walletExportAudits int64
	if err := db.Model(&model.AuditLog{}).Where("action = ?", "wallet.export").Count(&walletExportAudits).Error; err != nil || walletExportAudits != 1 {
		t.Fatalf("wallet export audit mismatch: count=%d err=%v", walletExportAudits, err)
	}
}
