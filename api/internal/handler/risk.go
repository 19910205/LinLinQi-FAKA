package handler

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/netip"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"linlinqi/api/internal/model"
	"linlinqi/api/internal/service"
	"linlinqi/api/pkg/response"
)

// authorizeCheckout records every evaluated attempt before inventory is
// reserved. Review/challenge decisions fail closed because LinLinQi never
// pretends a browser challenge happened when no challenge provider is wired.
func (h Handler) authorizeCheckout(c *gin.Context, userID *uuid.UUID, email string, total int64) (uuid.UUID, bool) {
	assessment, err := service.AssessCheckoutRisk(h.DB, userID, email, c.ClientIP(), total)
	if err != nil {
		response.Error(c, 503, 50391, "error.order_risk_control_unavailable")
		return uuid.Nil, false
	}
	decisionID, err := service.RecordCheckoutRisk(h.DB, nil, userID, c.ClientIP(), assessment)
	if err != nil {
		response.Error(c, 503, 50391, "error.order_risk_control_unavailable")
		return uuid.Nil, false
	}
	if assessment.Decision == "allow" {
		return decisionID, true
	}
	details, _ := json.Marshal(map[string]any{"risk_decision_id": decisionID, "decision": assessment.Decision, "score": assessment.Score})
	event := model.SecurityEvent{Base: model.Base{ID: uuid.New()}, EventType: "checkout.risk_blocked", Severity: riskSeverity(assessment.Decision), Realm: "user", PrincipalID: userID, IP: c.ClientIP(), UserAgent: truncateSecurityValue(c.Request.UserAgent(), 500), Details: string(details)}
	_ = h.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&event).Error; err != nil {
			return err
		}
		values := map[string]string{"status": assessment.Decision, "email": email, "ip": c.ClientIP(), "amount": fmt.Sprintf("%d", total), "summary": "结算请求被风控策略拒绝或转入人工复核"}
		if userID != nil {
			values["user_id"] = userID.String()
		}
		return h.createOperationalNotifications(tx, "risk.blocked", event.ID.String(), values)
	})
	response.Error(c, 403, 40391, "error.order_requires_manual_review")
	return uuid.Nil, false
}

func riskSeverity(decision string) string {
	if decision == "deny" {
		return "high"
	}
	return "medium"
}

type createIPBlockRequest struct {
	CIDR      string     `json:"cidr"`
	Scope     string     `json:"scope"`
	Reason    string     `json:"reason"`
	ExpiresAt *time.Time `json:"expires_at"`
}

func (h Handler) CreateIPBlock(c *gin.Context) {
	var req createIPBlockRequest
	if decodeStrictJSON(c, &req) != nil {
		response.Error(c, 422, 42260, "error.ip_blacklist_invalid")
		return
	}
	changeReason := strings.TrimSpace(c.GetHeader("X-Change-Reason"))
	if changeReason == "" {
		response.Error(c, 422, 42257, "error.change_reason_required")
		return
	}
	prefix, ok := normalizedPrefix(req.CIDR)
	if !ok {
		response.Error(c, 422, 42260, "error.ip_or_cidr_required")
		return
	}
	scope := strings.ToLower(strings.TrimSpace(req.Scope))
	if scope == "" {
		scope = "public"
	}
	if scope != "public" && scope != "admin" && scope != "openapi" && scope != "all" {
		response.Error(c, 422, 42261, "error.blacklist_scope_invalid")
		return
	}
	if (scope == "admin" || scope == "all") && prefixContainsIP(prefix, c.ClientIP()) {
		response.Error(c, 409, 40961, "error.policy_blocks_current_admin_create")
		return
	}
	if len(strings.TrimSpace(req.Reason)) < 3 || len([]rune(req.Reason)) > 500 {
		response.Error(c, 422, 42262, "error.ban_reason_required")
		return
	}
	if req.ExpiresAt != nil && !req.ExpiresAt.After(time.Now()) {
		response.Error(c, 422, 42263, "error.expiry_must_be_future")
		return
	}
	adminID, _ := uuid.Parse(c.GetString("subject"))
	item := model.IPBlocklist{CIDR: prefix, Scope: scope, Reason: strings.TrimSpace(req.Reason), Source: "admin", Enabled: true, ExpiresAt: req.ExpiresAt, CreatedBy: &adminID}
	if err := h.DB.Create(&item).Error; err != nil {
		response.Error(c, 409, 40960, "error.ip_cidr_exists_or_save_failed")
		return
	}
	h.audit(c, "security.ip_block.create", "ip_blocklist", item.ID.String(), changeReason)
	response.Created(c, item)
}

type updateIPBlockRequest struct {
	Enabled   *bool      `json:"enabled"`
	Reason    *string    `json:"reason"`
	ExpiresAt *time.Time `json:"expires_at"`
}

func (h Handler) UpdateIPBlock(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, 422, 42264, "error.blacklist_id_invalid")
		return
	}
	var req updateIPBlockRequest
	if decodeStrictJSON(c, &req) != nil || (req.Enabled == nil && req.Reason == nil && req.ExpiresAt == nil) {
		response.Error(c, 422, 42260, "error.ip_blacklist_update_invalid")
		return
	}
	changeReason := strings.TrimSpace(c.GetHeader("X-Change-Reason"))
	if changeReason == "" {
		response.Error(c, 422, 42257, "error.change_reason_required")
		return
	}
	var current model.IPBlocklist
	if err := h.DB.First(&current, "id = ?", id).Error; err != nil {
		response.Error(c, 404, 40460, "error.ip_blacklist_not_found")
		return
	}
	if req.Enabled != nil && *req.Enabled && (current.Scope == "admin" || current.Scope == "all") && prefixContainsIP(current.CIDR, c.ClientIP()) {
		response.Error(c, 409, 40961, "error.policy_blocks_current_admin_enable")
		return
	}
	updates := map[string]any{}
	if req.Enabled != nil {
		updates["enabled"] = *req.Enabled
	}
	if req.Reason != nil {
		reason := strings.TrimSpace(*req.Reason)
		if len([]rune(reason)) < 3 || len([]rune(reason)) > 500 {
			response.Error(c, 422, 42262, "error.ban_reason_required")
			return
		}
		updates["reason"] = reason
	}
	if req.ExpiresAt != nil {
		if !req.ExpiresAt.After(time.Now()) {
			response.Error(c, 422, 42263, "error.expiry_must_be_future")
			return
		}
		updates["expires_at"] = req.ExpiresAt
	}
	result := h.DB.Model(&model.IPBlocklist{}).Where("id = ?", id).Updates(updates)
	if result.Error != nil {
		response.Error(c, 500, 50060, "error.ip_blacklist_update_failed")
		return
	}
	if result.RowsAffected != 1 {
		response.Error(c, 404, 40460, "error.ip_blacklist_not_found")
		return
	}
	h.audit(c, "security.ip_block.update", "ip_blocklist", id.String(), changeReason)
	var item model.IPBlocklist
	if err := h.DB.First(&item, "id = ?", id).Error; err != nil && err != gorm.ErrRecordNotFound {
		slog.Error("reload updated IP blocklist", "id", id, "error", err)
	}
	response.OK(c, item)
}

func normalizedPrefix(raw string) (string, bool) {
	value := strings.TrimSpace(raw)
	if prefix, err := netip.ParsePrefix(value); err == nil {
		address := prefix.Addr().Unmap()
		bits := prefix.Bits()
		if prefix.Addr().Is6() && address.Is4() {
			bits -= 96
		}
		if bits < 0 || bits > address.BitLen() {
			return "", false
		}
		return netip.PrefixFrom(address, bits).Masked().String(), true
	}
	address, err := netip.ParseAddr(value)
	if err != nil {
		return "", false
	}
	address = address.Unmap()
	return netip.PrefixFrom(address, address.BitLen()).String(), true
}

func prefixContainsIP(rawPrefix, rawIP string) bool {
	prefix, err := netip.ParsePrefix(rawPrefix)
	if err != nil {
		return false
	}
	address, err := netip.ParseAddr(strings.TrimSpace(rawIP))
	if err != nil {
		return false
	}
	return prefix.Contains(address.Unmap())
}
