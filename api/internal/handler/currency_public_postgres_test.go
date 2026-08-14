package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"linlinqi/api/internal/model"
)

type publicCurrencyEnvelope struct {
	Data json.RawMessage `json:"data"`
}

func publicCurrencyRequest(t *testing.T, h Handler, directory bool) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodGet, "/api/v1/currencies", nil)
	if directory {
		context.Request.URL.Path = "/api/v1/currency-directory"
		h.PublicCurrencyDirectory(context)
	} else {
		h.PublicCurrencies(context)
	}
	return recorder
}

func TestPublicCurrencyDirectoryKeepsLegacyArrayContractPostgreSQL(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := isolatedSupplyAdminDB(t, "linlinqi_public_currency_test_")
	if err := db.Model(&model.CurrencyDefinition{}).Where("code = ?", "JPY").Update("enabled", false).Error; err != nil {
		t.Fatalf("disable JPY: %v", err)
	}
	setting := model.Setting{Key: "store_currency"}
	if err := db.Where("key = ?", setting.Key).Assign(model.Setting{Value: "USD", Group: "currency"}).FirstOrCreate(&setting).Error; err != nil {
		t.Fatalf("set store currency: %v", err)
	}
	h := Handler{DB: db}

	legacyResponse := publicCurrencyRequest(t, h, false)
	if legacyResponse.Code != http.StatusOK {
		t.Fatalf("legacy currencies status=%d body=%s", legacyResponse.Code, legacyResponse.Body.String())
	}
	if strings.Contains(legacyResponse.Body.String(), `"store_currency"`) || strings.Contains(legacyResponse.Body.String(), `"enabled"`) {
		t.Fatalf("legacy array contract changed: %s", legacyResponse.Body.String())
	}
	var legacyEnvelope publicCurrencyEnvelope
	if err := json.Unmarshal(legacyResponse.Body.Bytes(), &legacyEnvelope); err != nil {
		t.Fatalf("decode legacy envelope: %v", err)
	}
	var legacyItems []publicCurrencyDTO
	if err := json.Unmarshal(legacyEnvelope.Data, &legacyItems); err != nil {
		t.Fatalf("legacy data is not an array: %v body=%s", err, legacyResponse.Body.String())
	}
	for _, item := range legacyItems {
		if item.Code == "JPY" {
			t.Fatalf("disabled currency leaked through legacy endpoint: %#v", item)
		}
	}

	directoryResponse := publicCurrencyRequest(t, h, true)
	if directoryResponse.Code != http.StatusOK {
		t.Fatalf("currency directory status=%d body=%s", directoryResponse.Code, directoryResponse.Body.String())
	}
	var directoryEnvelope publicCurrencyEnvelope
	if err := json.Unmarshal(directoryResponse.Body.Bytes(), &directoryEnvelope); err != nil {
		t.Fatalf("decode directory envelope: %v", err)
	}
	var directory struct {
		Items         []publicCurrencyDTO `json:"items"`
		StoreCurrency string              `json:"store_currency"`
	}
	if err := json.Unmarshal(directoryEnvelope.Data, &directory); err != nil {
		t.Fatalf("decode directory data: %v body=%s", err, directoryResponse.Body.String())
	}
	if directory.StoreCurrency != "USD" || len(directory.Items) == 0 {
		t.Fatalf("directory metadata mismatch: %#v", directory)
	}
	for _, item := range directory.Items {
		if !item.Enabled {
			t.Fatalf("enabled public directory item omitted explicit true: %#v", item)
		}
		if item.Code == "JPY" {
			t.Fatalf("disabled currency leaked through directory endpoint: %#v", item)
		}
	}
}
