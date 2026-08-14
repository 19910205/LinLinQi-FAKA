package handler

import (
	"encoding/json"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"linlinqi/api/internal/model"
	"linlinqi/api/internal/service"
	"linlinqi/api/pkg/response"
)

var riskRuleCodePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{2,79}$`)

type riskRuleRequest struct {
	Code       string `json:"code"`
	Name       string `json:"name"`
	Scope      string `json:"scope"`
	Expression string `json:"expression"`
	Action     string `json:"action"`
	Score      int    `json:"score"`
	Enabled    bool   `json:"enabled"`
	Priority   int    `json:"priority"`
}

func normalizeRiskRuleRequest(req *riskRuleRequest) (model.RiskRule, error) {
	req.Code = strings.ToLower(strings.TrimSpace(req.Code))
	req.Name = strings.TrimSpace(req.Name)
	req.Scope = strings.ToLower(strings.TrimSpace(req.Scope))
	req.Expression = strings.ToLower(strings.TrimSpace(req.Expression))
	req.Action = strings.ToLower(strings.TrimSpace(req.Action))
	item := model.RiskRule{Code: req.Code, Name: req.Name, Scope: req.Scope, Expression: req.Expression, Action: req.Action, Score: req.Score, Enabled: req.Enabled, Priority: req.Priority}
	if !riskRuleCodePattern.MatchString(item.Code) || len([]rune(item.Name)) < 2 || len([]rune(item.Name)) > 160 || service.ValidateRiskRule(item) != nil {
		return model.RiskRule{}, errors.New("invalid risk rule")
	}
	return item, nil
}

func (h Handler) AdminRiskRules(c *gin.Context) {
	page, pageSize := pagination(c)
	query := h.DB.Model(&model.RiskRule{})
	if keyword := strings.TrimSpace(c.Query("q")); keyword != "" {
		like := "%" + strings.ToLower(keyword) + "%"
		query = query.Where("LOWER(code) LIKE ? OR LOWER(name) LIKE ?", like, like)
	}
	if scope := strings.TrimSpace(c.Query("scope")); scope != "" {
		query = query.Where("scope = ?", scope)
	}
	if enabled := strings.TrimSpace(c.Query("enabled")); enabled == "true" || enabled == "false" {
		query = query.Where("enabled = ?", enabled == "true")
	}
	var total int64
	query.Count(&total)
	var items []model.RiskRule
	if err := query.Order("priority DESC, created_at ASC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&items).Error; err != nil {
		response.Error(c, 500, 50090, "error.risk_rule_list_fetch_failed")
		return
	}
	response.Page(c, items, total, page, pageSize)
}

func (h Handler) CreateRiskRule(c *gin.Context) {
	var req riskRuleRequest
	if decodeStrictJSON(c, &req) != nil {
		response.Error(c, 422, 42290, "error.risk_rule_params_invalid")
		return
	}
	item, err := normalizeRiskRuleRequest(&req)
	if err != nil {
		response.Error(c, 422, 42290, "error.risk_rule_fields_invalid")
		return
	}
	reason, ok := requireAdminChangeReason(c, "创建风控规则")
	if !ok {
		return
	}
	if err := h.DB.Transaction(func(tx *gorm.DB) error {
		return createWithExplicitColumns(tx, &item, map[string]any{"enabled": req.Enabled})
	}); err != nil {
		response.Error(c, 409, 40990, "error.risk_rule_slug_exists")
		return
	}
	item.Enabled = req.Enabled
	h.audit(c, "risk.rule.create", "risk_rule", item.ID.String(), reason+"；code="+item.Code)
	response.Created(c, item)
}

func (h Handler) UpdateRiskRule(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, 422, 42291, "error.risk_rule_id_invalid")
		return
	}
	var req riskRuleRequest
	if decodeStrictJSON(c, &req) != nil {
		response.Error(c, 422, 42290, "error.risk_rule_params_invalid")
		return
	}
	item, err := normalizeRiskRuleRequest(&req)
	if err != nil {
		response.Error(c, 422, 42290, "error.risk_rule_fields_invalid")
		return
	}
	reason, ok := requireAdminChangeReason(c, "修改风控规则")
	if !ok {
		return
	}
	updates := map[string]any{"code": item.Code, "name": item.Name, "scope": item.Scope, "expression": item.Expression, "action": item.Action, "score": item.Score, "enabled": item.Enabled, "priority": item.Priority}
	result := h.DB.Model(&model.RiskRule{}).Where("id = ?", id).Updates(updates)
	if result.Error != nil {
		response.Error(c, 409, 40990, "error.risk_rule_update_failed")
		return
	}
	if result.RowsAffected != 1 {
		response.Error(c, 404, 40490, "error.risk_rule_not_found")
		return
	}
	h.audit(c, "risk.rule.update", "risk_rule", id.String(), reason+"；code="+item.Code)
	item.ID = id
	response.OK(c, item)
}

func (h Handler) AdminRiskDecisions(c *gin.Context) {
	page, pageSize := pagination(c)
	query := h.DB.Model(&model.RiskDecision{})
	if decision := strings.TrimSpace(c.Query("decision")); decision != "" {
		query = query.Where("decision = ?", decision)
	}
	if reviewed := strings.TrimSpace(c.Query("reviewed")); reviewed == "true" {
		query = query.Where("reviewed_at IS NOT NULL")
	} else if reviewed == "false" {
		query = query.Where("reviewed_at IS NULL")
	}
	if ip := strings.TrimSpace(c.Query("ip")); ip != "" {
		query = query.Where("ip = ?", ip)
	}
	if orderID, err := uuid.Parse(strings.TrimSpace(c.Query("order_id"))); err == nil {
		query = query.Where("order_id = ?", orderID)
	}
	if minimum := strings.TrimSpace(c.Query("score_min")); minimum != "" {
		parsed, err := strconv.Atoi(minimum)
		if err != nil || parsed < 0 || parsed > 100 {
			response.Error(c, 422, 42294, "error.min_risk_score_invalid")
			return
		}
		query = query.Where("score >= ?", parsed)
	}
	var total int64
	query.Count(&total)
	var items []model.RiskDecision
	if err := query.Order("created_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&items).Error; err != nil {
		response.Error(c, 500, 50091, "error.risk_decision_fetch_failed")
		return
	}
	response.Page(c, items, total, page, pageSize)
}

func (h Handler) AdminRiskSummary(c *gin.Context) {
	since := time.Now().Add(-24 * time.Hour)
	type decisionCount struct {
		Decision string `json:"decision"`
		Count    int64  `json:"count"`
	}
	var counts []decisionCount
	h.DB.Model(&model.RiskDecision{}).Select("decision, COUNT(*) AS count").Where("created_at >= ?", since).Group("decision").Scan(&counts)
	var pendingReview, activeRules int64
	h.DB.Model(&model.RiskDecision{}).Where("reviewed_at IS NULL AND decision IN ?", []string{"review", "challenge", "deny"}).Count(&pendingReview)
	h.DB.Model(&model.RiskRule{}).Where("enabled = ?", true).Count(&activeRules)
	type dailyRiskPoint struct {
		Date           string `json:"date"`
		Decisions      int64  `json:"decisions"`
		SecurityEvents int64  `json:"security_events"`
	}
	var dailySeries []dailyRiskPoint
	if err := h.DB.Raw(`
		SELECT to_char(d.date, 'YYYY-MM-DD') AS date,
			COUNT(DISTINCT rd.id) AS decisions,
			COUNT(DISTINCT se.id) AS security_events
		FROM generate_series(date_trunc('day', now()) - interval '6 days', date_trunc('day', now()), interval '1 day') AS d(date)
		LEFT JOIN risk_decisions rd ON rd.deleted_at IS NULL AND date_trunc('day', rd.created_at) = d.date
		LEFT JOIN security_events se ON se.deleted_at IS NULL AND date_trunc('day', se.created_at) = d.date
		GROUP BY d.date
		ORDER BY d.date ASC
	`).Scan(&dailySeries).Error; err != nil {
		response.Error(c, 500, 50091, "error.risk_decision_fetch_failed")
		return
	}
	response.OK(c, gin.H{"since": since, "decision_counts": counts, "pending_review": pendingReview, "active_rules": activeRules, "daily_series": dailySeries})
}

type riskDecisionReviewRequest struct {
	Outcome string `json:"outcome"`
}

var errRiskAlreadyReviewed = errors.New("risk decision already reviewed")

func (h Handler) ReviewRiskDecision(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, 422, 42292, "error.risk_decision_id_invalid")
		return
	}
	var req riskDecisionReviewRequest
	if decodeStrictJSON(c, &req) != nil {
		response.Error(c, 422, 42293, "error.review_conclusion_invalid")
		return
	}
	req.Outcome = strings.ToLower(strings.TrimSpace(req.Outcome))
	if req.Outcome != "allow" && req.Outcome != "deny" {
		response.Error(c, 422, 42293, "error.review_conclusion_allow_deny")
		return
	}
	reason, ok := requireAdminChangeReason(c, "复核风控决策")
	if !ok {
		return
	}
	adminID, _ := uuid.Parse(c.GetString("subject"))
	now := time.Now()
	var decision model.RiskDecision
	orderTransitioned := false
	err = h.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&decision, "id = ?", id).Error; err != nil {
			return err
		}
		if decision.ReviewedAt != nil {
			return errRiskAlreadyReviewed
		}
		original := decision.Decision
		if err := tx.Model(&decision).Updates(map[string]any{"decision": req.Outcome, "reviewed_by": &adminID, "reviewed_at": &now}).Error; err != nil {
			return err
		}
		if decision.OrderID != nil {
			var order model.Order
			if err := tx.Select("id", "status").First(&order, "id = ?", *decision.OrderID).Error; err == nil && order.Status == "risk_review" {
				target := "pending"
				if req.Outcome == "deny" {
					target = "cancelled"
				}
				if err := service.TransitionOrder(tx, order.ID, target, "admin", &adminID, reason); err != nil {
					return err
				}
				orderTransitioned = true
			}
		}
		details, _ := json.Marshal(gin.H{"risk_decision_id": decision.ID, "original": original, "outcome": req.Outcome, "reason": reason, "order_transitioned": orderTransitioned})
		return tx.Create(&model.SecurityEvent{EventType: "risk.decision_reviewed", Severity: "medium", Realm: "admin", PrincipalID: &adminID, Details: string(details), Resolved: true, ResolvedBy: &adminID, ResolvedAt: &now}).Error
	})
	if err != nil {
		switch {
		case errors.Is(err, gorm.ErrRecordNotFound):
			response.Error(c, 404, 40491, "error.risk_decision_not_found")
		case errors.Is(err, errRiskAlreadyReviewed):
			response.Error(c, 409, 40991, "error.risk_decision_already_reviewed")
		default:
			response.Error(c, 409, 40992, "error.risk_review_failed_order_changed")
		}
		return
	}
	h.audit(c, "risk.decision.review", "risk_decision", decision.ID.String(), reason+"；outcome="+req.Outcome)
	decision.Decision, decision.ReviewedBy, decision.ReviewedAt = req.Outcome, &adminID, &now
	response.OK(c, gin.H{"decision": decision, "order_transitioned": orderTransitioned})
}
