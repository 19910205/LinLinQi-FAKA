package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestNormalizeRiskRuleRequestOnlyAcceptsAuditedDSL(t *testing.T) {
	valid := []riskRuleRequest{
		{Code: "ip_velocity_20", Name: "高频下单", Scope: "checkout", Expression: "orders(ip,10m) > 20", Action: "challenge", Score: 40, Enabled: true},
		{Code: "email_failures", Name: "邮箱失败频率", Scope: "payment", Expression: "failures(email,1h) > 8", Action: "review", Score: 50, Enabled: true},
		{Code: "guest_value", Name: "游客高额", Scope: "checkout", Expression: "guest && total > 200000", Action: "deny", Score: 90, Enabled: true},
	}
	for _, request := range valid {
		if _, err := normalizeRiskRuleRequest(&request); err != nil {
			t.Fatalf("valid audited rule rejected: %#v: %v", request, err)
		}
	}
	invalid := []riskRuleRequest{
		{Code: "bad", Name: "任意执行", Scope: "checkout", Expression: "eval(process.exit())", Action: "deny", Score: 10},
		{Code: "Bad Code", Name: "非法标识", Scope: "checkout", Expression: "orders(ip,10m) > 10", Action: "deny", Score: 10},
		{Code: "bad_score", Name: "非法分数", Scope: "checkout", Expression: "orders(ip,10m) > 10", Action: "deny", Score: 101},
	}
	for _, request := range invalid {
		if _, err := normalizeRiskRuleRequest(&request); err == nil {
			t.Fatalf("unsafe risk rule accepted: %#v", request)
		}
	}
}

func TestRiskRuleRequestRejectsServerOwnedFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest(http.MethodPost, "/risk/rules", strings.NewReader(`{"code":"safe_rule","name":"规则","scope":"checkout","expression":"orders(ip,10m) > 10","action":"deny","score":10,"enabled":true,"priority":1,"id":"attacker"}`))
	var request riskRuleRequest
	if err := decodeStrictJSON(context, &request); err == nil {
		t.Fatal("risk rule request accepted server-owned id")
	}
}
