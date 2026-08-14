package handler

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"linlinqi/api/internal/model"
	"linlinqi/api/pkg/response"
)

var errSecurityEventStateUnchanged = errors.New("security event state is unchanged")

type adminSecurityEventItem struct {
	ID             uuid.UUID      `json:"id"`
	EventType      string         `json:"event_type"`
	Severity       string         `json:"severity"`
	Realm          string         `json:"realm"`
	PrincipalID    *uuid.UUID     `json:"principal_id"`
	IP             string         `json:"ip"`
	UserAgent      string         `json:"user_agent"`
	Details        map[string]any `json:"details"`
	Resolved       bool           `json:"resolved"`
	ResolvedBy     *uuid.UUID     `json:"resolved_by"`
	ResolvedByName string         `json:"resolved_by_name"`
	ResolvedAt     *time.Time     `json:"resolved_at"`
	CreatedAt      time.Time      `json:"created_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
}

type adminSecurityEventRow struct {
	ID             uuid.UUID
	EventType      string
	Severity       string
	Realm          string
	PrincipalID    *uuid.UUID
	IP             string
	UserAgent      string
	Details        string
	Resolved       bool
	ResolvedBy     *uuid.UUID
	ResolvedByName string
	ResolvedAt     *time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

func decodedSecurityEventDetails(raw string) map[string]any {
	details := map[string]any{}
	if json.Unmarshal([]byte(raw), &details) != nil {
		return map[string]any{}
	}
	return details
}

func toAdminSecurityEvent(row adminSecurityEventRow) adminSecurityEventItem {
	return adminSecurityEventItem{
		ID: row.ID, EventType: row.EventType, Severity: row.Severity, Realm: row.Realm,
		PrincipalID: row.PrincipalID, IP: row.IP, UserAgent: row.UserAgent,
		Details: decodedSecurityEventDetails(row.Details), Resolved: row.Resolved,
		ResolvedBy: row.ResolvedBy, ResolvedByName: row.ResolvedByName,
		ResolvedAt: row.ResolvedAt, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
}

func securityEventListQuery(db *gorm.DB) *gorm.DB {
	return db.Table("security_events se").
		Select(`se.id, se.event_type, se.severity, se.realm, se.principal_id,
			se.ip, se.user_agent, se.details, se.resolved, se.resolved_by,
			COALESCE(a.name, '') AS resolved_by_name, se.resolved_at,
			se.created_at, se.updated_at`).
		Joins("LEFT JOIN admins a ON a.id = se.resolved_by AND a.deleted_at IS NULL").
		Where("se.deleted_at IS NULL")
}

func applySecurityTimeRange(c *gin.Context, query *gorm.DB, column string) (*gorm.DB, error) {
	fromRaw, toRaw := strings.TrimSpace(c.Query("from")), strings.TrimSpace(c.Query("to"))
	if fromRaw == "" && toRaw == "" {
		return query, nil
	}
	var from, to time.Time
	var err error
	if fromRaw != "" {
		from, err = parseAccessAuditTime(fromRaw, false)
		if err != nil {
			return nil, err
		}
		query = query.Where(column+" >= ?", from)
	}
	if toRaw != "" {
		to, err = parseAccessAuditTime(toRaw, true)
		if err != nil {
			return nil, err
		}
		query = query.Where(column+" < ?", to)
	}
	if !from.IsZero() && !to.IsZero() && (!to.After(from) || to.Sub(from) > 366*24*time.Hour) {
		return nil, errors.New("invalid security time range")
	}
	return query, nil
}

func (h Handler) AdminSecurityEvents(c *gin.Context) {
	page, pageSize := pagination(c)
	query := securityEventListQuery(h.DB)
	if resolvedRaw := strings.TrimSpace(c.Query("resolved")); resolvedRaw != "" {
		resolved, err := strconv.ParseBool(resolvedRaw)
		if err != nil {
			response.Error(c, 422, 42401, "error.security_event_resolved_filter_invalid")
			return
		}
		query = query.Where("se.resolved = ?", resolved)
	}
	if severity := strings.ToLower(strings.TrimSpace(c.Query("severity"))); severity != "" {
		allowed := map[string]bool{"info": true, "low": true, "medium": true, "warning": true, "high": true, "critical": true}
		if !allowed[severity] {
			response.Error(c, 422, 42402, "error.security_event_severity_filter_invalid")
			return
		}
		query = query.Where("se.severity = ?", severity)
	}
	if realm := strings.ToLower(strings.TrimSpace(c.Query("realm"))); realm != "" {
		if realm != "user" && realm != "admin" && realm != "system" && realm != "openapi" {
			response.Error(c, 422, 42403, "error.security_event_realm_filter_invalid")
			return
		}
		query = query.Where("se.realm = ?", realm)
	}
	if eventType := strings.TrimSpace(c.Query("event_type")); eventType != "" {
		if utf8.RuneCountInString(eventType) > 80 {
			response.Error(c, 422, 42404, "error.security_event_type_filter_invalid")
			return
		}
		query = query.Where("se.event_type = ?", eventType)
	}
	if keyword := strings.TrimSpace(c.Query("q")); keyword != "" {
		if utf8.RuneCountInString(keyword) > 100 {
			response.Error(c, 422, 42405, "error.security_event_search_invalid")
			return
		}
		like := "%" + keyword + "%"
		query = query.Where("se.event_type ILIKE ? OR se.ip ILIKE ? OR se.user_agent ILIKE ? OR CAST(se.principal_id AS TEXT) ILIKE ?", like, like, like, like)
	}
	var err error
	query, err = applySecurityTimeRange(c, query, "se.created_at")
	if err != nil {
		response.Error(c, 422, 42406, "error.security_event_time_range_invalid")
		return
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		response.Error(c, 500, 50401, "error.security_event_list_fetch_failed")
		return
	}
	var rows []adminSecurityEventRow
	if err := query.Order("se.created_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Scan(&rows).Error; err != nil {
		response.Error(c, 500, 50401, "error.security_event_list_fetch_failed")
		return
	}
	items := make([]adminSecurityEventItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, toAdminSecurityEvent(row))
	}
	c.Header("Cache-Control", "no-store")
	response.Page(c, items, total, page, pageSize)
}

type securityEventDispositionRequest struct {
	Resolved   *bool  `json:"resolved"`
	Conclusion string `json:"conclusion"`
	Evidence   string `json:"evidence"`
}

func safeDispositionText(value string, minimum, maximum int, multiline bool) (string, bool) {
	value = strings.TrimSpace(value)
	length := utf8.RuneCountInString(value)
	if length < minimum || length > maximum {
		return "", false
	}
	for _, character := range value {
		if character == '\x00' || (unicode.IsControl(character) && (!multiline || (character != '\n' && character != '\r' && character != '\t'))) {
			return "", false
		}
	}
	return value, true
}

func appendSecurityResolution(details map[string]any, entry map[string]any) (string, error) {
	history, _ := details["resolution_history"].([]any)
	if len(history) > 49 {
		history = append([]any(nil), history[len(history)-49:]...)
	}
	details["resolution_history"] = append(history, entry)
	encoded, err := json.Marshal(details)
	if err != nil || len(encoded) > 32<<10 {
		return "", errors.New("security event resolution history is too large")
	}
	return string(encoded), nil
}

func (h Handler) UpdateAdminSecurityEvent(c *gin.Context) {
	eventID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, 422, 42407, "error.security_event_id_invalid")
		return
	}
	reason, ok := requireAdminChangeReason(c, "处置安全事件")
	if !ok {
		return
	}
	var request securityEventDispositionRequest
	if decodeStrictJSON(c, &request) != nil || request.Resolved == nil {
		response.Error(c, 422, 42408, "error.security_event_disposition_invalid")
		return
	}
	request.Conclusion, ok = safeDispositionText(request.Conclusion, 4, 500, false)
	if !ok {
		response.Error(c, 422, 42409, "error.security_event_conclusion_invalid")
		return
	}
	request.Evidence, ok = safeDispositionText(request.Evidence, 4, 2000, true)
	if !ok {
		response.Error(c, 422, 42410, "error.security_event_evidence_invalid")
		return
	}
	adminID, err := uuid.Parse(c.GetString("subject"))
	if err != nil {
		response.Error(c, 401, 40103, "error.invalid_admin_identity")
		return
	}
	var event model.SecurityEvent
	now := time.Now().UTC()
	err = h.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&event, "id = ?", eventID).Error; err != nil {
			return err
		}
		if event.Resolved == *request.Resolved {
			return errSecurityEventStateUnchanged
		}
		details := decodedSecurityEventDetails(event.Details)
		nextDetails, err := appendSecurityResolution(details, map[string]any{
			"action":     map[bool]string{true: "resolved", false: "reopened"}[*request.Resolved],
			"conclusion": request.Conclusion, "evidence": request.Evidence,
			"change_reason": reason, "admin_id": adminID.String(), "at": now.Format(time.RFC3339Nano),
		})
		if err != nil {
			return err
		}
		updates := map[string]any{"resolved": *request.Resolved, "details": nextDetails}
		if *request.Resolved {
			updates["resolved_by"], updates["resolved_at"] = adminID, now
		} else {
			updates["resolved_by"], updates["resolved_at"] = nil, nil
		}
		return tx.Model(&event).Updates(updates).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		response.Error(c, 404, 40484, "error.security_event_not_found")
		return
	}
	if errors.Is(err, errSecurityEventStateUnchanged) {
		response.Error(c, 409, 40984, "error.security_event_state_unchanged")
		return
	}
	if err != nil {
		response.Error(c, 500, 50402, "error.security_event_disposition_failed")
		return
	}
	action := "security-event.reopen"
	if *request.Resolved {
		action = "security-event.resolve"
	}
	h.audit(c, action, "security_event", eventID.String(), "state_changed=true")
	var row adminSecurityEventRow
	load := securityEventListQuery(h.DB).Where("se.id = ?", eventID).Scan(&row)
	if load.Error != nil || load.RowsAffected != 1 {
		response.Error(c, 500, 50401, "error.security_event_fetch_failed")
		return
	}
	c.Header("Cache-Control", "no-store")
	response.OK(c, toAdminSecurityEvent(row))
}

type adminLoginEventItem struct {
	ID          uuid.UUID  `json:"id"`
	Realm       string     `json:"realm"`
	PrincipalID *uuid.UUID `json:"principal_id"`
	Account     string     `json:"account"`
	IP          string     `json:"ip"`
	Country     string     `json:"country"`
	City        string     `json:"city"`
	UserAgent   string     `json:"user_agent"`
	Succeeded   bool       `json:"succeeded"`
	Reason      string     `json:"reason"`
	CreatedAt   time.Time  `json:"created_at"`
}

func (h Handler) AdminLoginEvents(c *gin.Context) {
	page, pageSize := pagination(c)
	query := h.DB.Model(&model.LoginEvent{})
	if realm := strings.ToLower(strings.TrimSpace(c.Query("realm"))); realm != "" {
		if realm != "user" && realm != "admin" {
			response.Error(c, 422, 42411, "error.login_event_realm_filter_invalid")
			return
		}
		query = query.Where("realm = ?", realm)
	}
	if succeededRaw := strings.TrimSpace(c.Query("succeeded")); succeededRaw != "" {
		succeeded, err := strconv.ParseBool(succeededRaw)
		if err != nil {
			response.Error(c, 422, 42412, "error.login_event_result_filter_invalid")
			return
		}
		query = query.Where("succeeded = ?", succeeded)
	}
	if keyword := strings.TrimSpace(c.Query("q")); keyword != "" {
		if utf8.RuneCountInString(keyword) > 100 {
			response.Error(c, 422, 42413, "error.login_event_search_invalid")
			return
		}
		like := "%" + keyword + "%"
		query = query.Where("account ILIKE ? OR ip ILIKE ? OR user_agent ILIKE ? OR CAST(principal_id AS TEXT) ILIKE ?", like, like, like, like)
	}
	var err error
	query, err = applySecurityTimeRange(c, query, "created_at")
	if err != nil {
		response.Error(c, 422, 42414, "error.login_event_time_range_invalid")
		return
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		response.Error(c, 500, 50403, "error.login_event_list_fetch_failed")
		return
	}
	var stored []model.LoginEvent
	if err := query.Order("created_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&stored).Error; err != nil {
		response.Error(c, 500, 50403, "error.login_event_list_fetch_failed")
		return
	}
	items := make([]adminLoginEventItem, 0, len(stored))
	for _, item := range stored {
		items = append(items, adminLoginEventItem{
			ID: item.ID, Realm: item.Realm, PrincipalID: item.PrincipalID, Account: item.Account,
			IP: item.IP, Country: item.Country, City: item.City, UserAgent: item.UserAgent,
			Succeeded: item.Succeeded, Reason: item.Reason, CreatedAt: item.CreatedAt,
		})
	}
	c.Header("Cache-Control", "no-store")
	response.Page(c, items, total, page, pageSize)
}
