package service

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"

	"linlinqi/api/internal/model"
)

type CheckoutSignals struct {
	OrdersFromIP10Minutes int   `json:"orders_from_ip_10_minutes"`
	PaymentFailures1Hour  int   `json:"payment_failures_1_hour"`
	IsGuest               bool  `json:"is_guest"`
	OrderTotal            int64 `json:"order_total"`
	CountryMismatch       bool  `json:"country_mismatch"`
	ProxyDetected         bool  `json:"proxy_detected"`
	DisposableEmail       bool  `json:"disposable_email"`
}

type RiskResult struct {
	Score    int      `json:"score"`
	Decision string   `json:"decision"`
	Reasons  []string `json:"reasons"`
}

type CheckoutRiskAssessment struct {
	RiskResult
	Signals CheckoutSignals `json:"signals"`
}

func EvaluateCheckoutRisk(signals CheckoutSignals) RiskResult {
	score := 0
	reasons := make([]string, 0, 6)
	add := func(points int, reason string) { score += points; reasons = append(reasons, reason) }
	if signals.OrdersFromIP10Minutes > 12 {
		add(40, "ip_order_velocity")
	}
	if signals.PaymentFailures1Hour > 5 {
		add(35, "payment_failure_velocity")
	}
	if signals.IsGuest && signals.OrderTotal > 100000 {
		add(25, "high_value_guest")
	}
	if signals.CountryMismatch {
		add(15, "country_mismatch")
	}
	if signals.ProxyDetected {
		add(20, "proxy_detected")
	}
	if signals.DisposableEmail {
		add(20, "disposable_email")
	}
	decision := scoreDecision(score)
	return RiskResult{Score: min(score, 100), Decision: decision, Reasons: reasons}
}

var (
	ordersFromIPPattern   = regexp.MustCompile(`^orders\(ip,(\d+)m\)\s*>\s*(\d+)$`)
	emailFailuresPattern  = regexp.MustCompile(`^failures\(email,(\d+)h\)\s*>\s*(\d+)$`)
	highValueGuestPattern = regexp.MustCompile(`^guest\s*&&\s*total\s*>\s*(\d+)$`)
)

func ValidateRiskRule(rule model.RiskRule) error {
	if strings.TrimSpace(rule.Code) == "" || strings.TrimSpace(rule.Name) == "" {
		return fmt.Errorf("risk rule code and name are required")
	}
	if rule.Scope != "checkout" && rule.Scope != "payment" {
		return fmt.Errorf("unsupported risk scope")
	}
	if actionSeverity(rule.Action) == 0 || rule.Score < 0 || rule.Score > 100 {
		return fmt.Errorf("unsupported risk action or score")
	}
	expression := strings.TrimSpace(strings.ToLower(rule.Expression))
	if parts := ordersFromIPPattern.FindStringSubmatch(expression); len(parts) == 3 {
		minutes, _ := strconv.Atoi(parts[1])
		threshold, _ := strconv.Atoi(parts[2])
		if minutes == 10 && threshold >= 1 && threshold <= 10000 {
			return nil
		}
	}
	if parts := emailFailuresPattern.FindStringSubmatch(expression); len(parts) == 3 {
		hours, _ := strconv.Atoi(parts[1])
		threshold, _ := strconv.Atoi(parts[2])
		if hours == 1 && threshold >= 1 && threshold <= 10000 {
			return nil
		}
	}
	if parts := highValueGuestPattern.FindStringSubmatch(expression); len(parts) == 2 {
		threshold, _ := strconv.ParseInt(parts[1], 10, 64)
		if threshold > 0 {
			return nil
		}
	}
	return fmt.Errorf("unsupported risk expression")
}

// AssessCheckoutRisk evaluates only a small, auditable DSL. Arbitrary rule
// expressions are never executed. Unsupported expressions stay visible in the
// admin console but cannot silently run code in the API process.
func AssessCheckoutRisk(db *gorm.DB, userID *uuid.UUID, email, ip string, total int64) (CheckoutRiskAssessment, error) {
	signals := CheckoutSignals{IsGuest: userID == nil, OrderTotal: total}
	if ip != "" {
		var count int64
		if err := db.Model(&model.Order{}).Where("client_ip = ? AND created_at >= ?", ip, time.Now().Add(-10*time.Minute)).Count(&count).Error; err != nil {
			return CheckoutRiskAssessment{}, err
		}
		signals.OrdersFromIP10Minutes = int(count)
	}
	if email = strings.ToLower(strings.TrimSpace(email)); email != "" {
		var count int64
		err := db.Table("payment_intents pi").
			Joins("JOIN orders o ON o.id = pi.order_id AND o.deleted_at IS NULL").
			Where("o.email = ? AND pi.status IN ? AND pi.updated_at >= ? AND pi.deleted_at IS NULL", email, []string{"failed", "expired"}, time.Now().Add(-time.Hour)).
			Count(&count).Error
		if err != nil {
			return CheckoutRiskAssessment{}, err
		}
		signals.PaymentFailures1Hour = int(count)
	}
	var rules []model.RiskRule
	if err := db.Where("enabled = ? AND scope IN ?", true, []string{"checkout", "payment"}).Order("priority DESC, created_at ASC").Find(&rules).Error; err != nil {
		return CheckoutRiskAssessment{}, err
	}
	result := RiskResult{Decision: "allow", Reasons: make([]string, 0)}
	for _, rule := range rules {
		if ValidateRiskRule(rule) != nil {
			continue
		}
		matched := false
		expression := strings.TrimSpace(strings.ToLower(rule.Expression))
		if parts := ordersFromIPPattern.FindStringSubmatch(expression); len(parts) == 3 {
			minutes, _ := strconv.Atoi(parts[1])
			threshold, _ := strconv.Atoi(parts[2])
			if minutes == 10 && threshold >= 1 && threshold <= 10000 {
				matched = signals.OrdersFromIP10Minutes > threshold
			}
		} else if parts := emailFailuresPattern.FindStringSubmatch(expression); len(parts) == 3 {
			hours, _ := strconv.Atoi(parts[1])
			threshold, _ := strconv.Atoi(parts[2])
			if hours == 1 && threshold >= 1 && threshold <= 10000 {
				matched = signals.PaymentFailures1Hour > threshold
			}
		} else if parts := highValueGuestPattern.FindStringSubmatch(expression); len(parts) == 2 {
			threshold, _ := strconv.ParseInt(parts[1], 10, 64)
			matched = threshold > 0 && signals.IsGuest && signals.OrderTotal > threshold
		}
		if !matched {
			continue
		}
		result.Score = min(100, result.Score+max(0, min(rule.Score, 100)))
		result.Reasons = append(result.Reasons, rule.Code)
		if actionSeverity(rule.Action) > actionSeverity(result.Decision) {
			result.Decision = normalizeRiskAction(rule.Action)
		}
	}
	if result.Decision == "allow" {
		result.Decision = scoreDecision(result.Score)
	}
	return CheckoutRiskAssessment{RiskResult: result, Signals: signals}, nil
}

func RecordCheckoutRisk(db *gorm.DB, orderID, userID *uuid.UUID, ip string, assessment CheckoutRiskAssessment) (uuid.UUID, error) {
	matchedRules, err := json.Marshal(assessment.Reasons)
	if err != nil {
		return uuid.Nil, err
	}
	signals, err := json.Marshal(assessment.Signals)
	if err != nil {
		return uuid.Nil, err
	}
	decision := model.RiskDecision{OrderID: orderID, UserID: userID, IP: ip, Score: assessment.Score, Decision: assessment.Decision, MatchedRules: string(matchedRules), Signals: string(signals)}
	if err := db.Create(&decision).Error; err != nil {
		return uuid.Nil, err
	}
	return decision.ID, nil
}

func LinkCheckoutRisk(db *gorm.DB, decisionID, orderID uuid.UUID) error {
	if decisionID == uuid.Nil || orderID == uuid.Nil {
		return fmt.Errorf("risk decision and order are required")
	}
	return db.Model(&model.RiskDecision{}).Where("id = ? AND order_id IS NULL", decisionID).Update("order_id", orderID).Error
}

func scoreDecision(score int) string {
	switch {
	case score >= 80:
		return "deny"
	case score >= 40:
		return "challenge"
	case score >= 25:
		return "review"
	default:
		return "allow"
	}
}

func normalizeRiskAction(action string) string {
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "deny", "block":
		return "deny"
	case "challenge":
		return "challenge"
	case "review":
		return "review"
	default:
		return "allow"
	}
}

func actionSeverity(action string) int {
	switch normalizeRiskAction(action) {
	case "deny":
		return 3
	case "challenge":
		return 2
	case "review":
		return 1
	default:
		return 0
	}
}
