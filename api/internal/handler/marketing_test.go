package handler

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func TestRequireAdminChangeReasonRejectsAuditControlCharacters(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest("POST", "/admin/change", nil)
	context.Request.Header.Set("X-Change-Reason", "approved\tbut forged")
	if _, ok := requireAdminChangeReason(context, "测试变更"); ok {
		t.Fatal("audit reason with a control character was accepted")
	}
}

func TestPromotionValidation(t *testing.T) {
	now := time.Now().UTC()
	valid := promotionRequest{
		Name: "会员季九折", Code: "member-90", Type: "percentage",
		Rules:    promotionRuleRequest{BasisPoints: 1000, MinAmount: 5000, MaxDiscount: 2000},
		StartsAt: now.Add(time.Hour), EndsAt: now.Add(7 * 24 * time.Hour), Status: "active",
		ProductIDs: []uuid.UUID{uuid.New()},
	}
	if err := valid.normalizeAndValidate(now); err != nil {
		t.Fatalf("valid promotion rejected: %v", err)
	}
	if valid.Code != "MEMBER-90" || valid.Type != "percentage" {
		t.Fatalf("promotion was not normalized: %#v", valid)
	}
	for name, mutate := range map[string]func(*promotionRequest){
		"unknown type":       func(r *promotionRequest) { r.Type = "discount" },
		"invalid percentage": func(r *promotionRequest) { r.Rules.BasisPoints = 10001 },
		"no products":        func(r *promotionRequest) { r.ProductIDs = nil },
		"duplicate products": func(r *promotionRequest) { r.ProductIDs = []uuid.UUID{r.ProductIDs[0], r.ProductIDs[0]} },
		"reversed dates":     func(r *promotionRequest) { r.EndsAt = r.StartsAt },
		"invalid status":     func(r *promotionRequest) { r.Status = "running" },
	} {
		t.Run(name, func(t *testing.T) {
			request := valid
			request.ProductIDs = append([]uuid.UUID(nil), valid.ProductIDs...)
			mutate(&request)
			if err := request.normalizeAndValidate(now); err == nil {
				t.Fatal("invalid promotion was accepted")
			}
		})
	}
}

func TestCouponValidation(t *testing.T) {
	now := time.Now().UTC()
	valid := couponRequest{Code: "welcome-8", Type: "fixed", Value: 800, MinAmount: 5000, UsageLimit: 1000, StartsAt: now, EndsAt: now.Add(30 * 24 * time.Hour), Enabled: true}
	if err := valid.normalizeAndValidate(now); err != nil {
		t.Fatalf("valid coupon rejected: %v", err)
	}
	if valid.Code != "WELCOME-8" {
		t.Fatalf("coupon code was not normalized: %s", valid.Code)
	}
	percentage := valid
	percentage.Type, percentage.Value = "percentage", 1000
	if err := percentage.normalizeAndValidate(now); err != nil {
		t.Fatalf("valid percentage coupon rejected: %v", err)
	}
	for name, mutate := range map[string]func(*couponRequest){
		"short code":       func(r *couponRequest) { r.Code = "A" },
		"unknown type":     func(r *couponRequest) { r.Type = "amount" },
		"negative minimum": func(r *couponRequest) { r.MinAmount = -1 },
		"bad value":        func(r *couponRequest) { r.Value = 0 },
		"bad period":       func(r *couponRequest) { r.EndsAt = r.StartsAt },
	} {
		t.Run(name, func(t *testing.T) {
			request := valid
			mutate(&request)
			if err := request.normalizeAndValidate(now); err == nil {
				t.Fatal("invalid coupon was accepted")
			}
		})
	}
}
