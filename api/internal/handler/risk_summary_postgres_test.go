package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"linlinqi/api/internal/model"
)

func TestAdminRiskSummaryDailySeriesPostgreSQL(t *testing.T) {
	db := isolatedSupplyAdminDB(t, "linlinqi_risk_summary_test_")
	rule := model.RiskRule{Code: "summary_rule", Name: "Summary rule", Scope: "checkout", Expression: "orders(ip,10m) > 10", Action: "challenge", Score: 40, Enabled: true, Priority: 100}
	if err := db.Create(&rule).Error; err != nil {
		t.Fatalf("create risk rule: %v", err)
	}
	decisions := []model.RiskDecision{
		{IP: "203.0.113.10", Score: 10, Decision: "allow", Signals: "{}", MatchedRules: "[]"},
		{IP: "203.0.113.11", Score: 90, Decision: "deny", Signals: "{}", MatchedRules: "[]"},
		{IP: "203.0.113.12", Score: 50, Decision: "review", Signals: "{}", MatchedRules: "[]"},
	}
	for _, decision := range decisions {
		if err := db.Create(&decision).Error; err != nil {
			t.Fatalf("create risk decision: %v", err)
		}
	}
	event := model.SecurityEvent{EventType: "risk.decision_created", Severity: "high", Realm: "user", IP: "203.0.113.11", Details: `{}`}
	if err := db.Create(&event).Error; err != nil {
		t.Fatalf("create security event: %v", err)
	}

	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest(http.MethodGet, "/admin/v1/risk/summary", nil)
	handler := Handler{DB: db}
	handler.AdminRiskSummary(context)
	if recorder.Code != http.StatusOK {
		t.Fatalf("summary status = %d, body=%s", recorder.Code, recorder.Body.String())
	}
	var envelope struct {
		Data struct {
			DecisionCounts []struct {
				Decision string `json:"decision"`
				Count    int64  `json:"count"`
			} `json:"decision_counts"`
			PendingReview int64 `json:"pending_review"`
			ActiveRules   int64 `json:"active_rules"`
			DailySeries   []struct {
				Date           string `json:"date"`
				Decisions      int64  `json:"decisions"`
				SecurityEvents int64  `json:"security_events"`
			} `json:"daily_series"`
		} `json:"data"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode summary: %v", err)
	}
	if len(envelope.Data.DailySeries) != 7 {
		t.Fatalf("expected 7 daily points, got %d", len(envelope.Data.DailySeries))
	}
	last := envelope.Data.DailySeries[len(envelope.Data.DailySeries)-1]
	if last.Decisions < 3 || last.SecurityEvents < 1 {
		t.Fatalf("today's point missed seeded data: %#v", last)
	}
	if envelope.Data.PendingReview != 2 || envelope.Data.ActiveRules != 1 {
		t.Fatalf("summary counters are wrong: pending=%d rules=%d", envelope.Data.PendingReview, envelope.Data.ActiveRules)
	}
	counts := map[string]int64{}
	for _, item := range envelope.Data.DecisionCounts {
		counts[item.Decision] = item.Count
	}
	if counts["allow"] != 1 || counts["deny"] != 1 || counts["review"] != 1 {
		t.Fatalf("decision counts are wrong: %#v", counts)
	}
}
