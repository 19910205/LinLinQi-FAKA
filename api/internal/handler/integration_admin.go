package handler

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/mail"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"linlinqi/api/internal/i18n"
	"linlinqi/api/internal/model"
	"linlinqi/api/internal/queue"
	"linlinqi/api/pkg/response"
)

var (
	notificationTemplateCodePattern = regexp.MustCompile(`^[a-z][a-z0-9_.-]{2,99}$`)
	notificationLocalePattern       = regexp.MustCompile(`^[a-z]{2,3}(?:-[A-Za-z0-9]{2,8}){0,2}$`)
	notificationVariablePattern     = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
	notificationPlaceholderPattern  = regexp.MustCompile(`{{([a-z][a-z0-9_]*)}}`)
	notificationTestKeyPattern      = regexp.MustCompile(`^[A-Za-z0-9_-]{8,64}$`)
	integrationIdentifierPattern    = regexp.MustCompile(`^[A-Za-z0-9_@.+=:/-]{1,160}$`)
	httpStatusDiagnosticPattern     = regexp.MustCompile(`(?i)HTTP\s+([1-5][0-9]{2})`)

	errIntegrationRetryState       = errors.New("delivery is not in a terminal failed state")
	errIntegrationEndpointDisabled = errors.New("webhook endpoint is disabled")
	errIntegrationEndpointUnsafe   = errors.New("webhook endpoint destination is unsafe")
	errIntegrationNoChange         = errors.New("integration state is unchanged")
	errNotificationTestRateLimit   = errors.New("notification test rate limit exceeded")
	errTemplateHasDeliveries       = errors.New("notification template has delivery history")
)

type integrationSummary struct {
	WebhookEndpointsActive   int64 `json:"webhook_endpoints_active"`
	WebhookEndpointsDisabled int64 `json:"webhook_endpoints_disabled"`
	WebhookDeliveriesPending int64 `json:"webhook_deliveries_pending"`
	WebhookDeliveriesFailed  int64 `json:"webhook_deliveries_failed"`
	WebhookDelivered24Hours  int64 `json:"webhook_delivered_24_hours"`
	NotificationTemplates    int64 `json:"notification_templates_enabled"`
	NotificationPending      int64 `json:"notification_deliveries_pending"`
	NotificationFailed       int64 `json:"notification_deliveries_failed"`
	NotificationSent24Hours  int64 `json:"notification_sent_24_hours"`
}

func (h Handler) AdminIntegrationSummary(c *gin.Context) {
	summary := integrationSummary{}
	queries := []struct {
		destination *int64
		model       any
		where       string
		arguments   []any
	}{
		{&summary.WebhookEndpointsActive, &model.WebhookEndpoint{}, "enabled = ?", []any{true}},
		{&summary.WebhookEndpointsDisabled, &model.WebhookEndpoint{}, "enabled = ?", []any{false}},
		{&summary.WebhookDeliveriesPending, &model.WebhookDelivery{}, "status IN ?", []any{[]string{"queued", "sending", "retrying"}}},
		{&summary.WebhookDeliveriesFailed, &model.WebhookDelivery{}, "status = ?", []any{"failed"}},
		{&summary.WebhookDelivered24Hours, &model.WebhookDelivery{}, "status = ? AND delivered_at >= ?", []any{"delivered", time.Now().Add(-24 * time.Hour)}},
		{&summary.NotificationTemplates, &model.NotificationTemplate{}, "enabled = ?", []any{true}},
		{&summary.NotificationPending, &model.NotificationDelivery{}, "status IN ?", []any{[]string{"queued", "sending", "retrying"}}},
		{&summary.NotificationFailed, &model.NotificationDelivery{}, "status = ?", []any{"failed"}},
		{&summary.NotificationSent24Hours, &model.NotificationDelivery{}, "status = ? AND sent_at >= ?", []any{"sent", time.Now().Add(-24 * time.Hour)}},
	}
	for _, query := range queries {
		if err := h.DB.Model(query.model).Where(query.where, query.arguments...).Count(query.destination).Error; err != nil {
			response.Error(c, 500, 50601, "error.integration_run_summary_fetch_failed")
			return
		}
	}
	response.OK(c, summary)
}

func optionalBoolQuery(c *gin.Context, key string) (*bool, error) {
	raw := strings.TrimSpace(c.Query(key))
	if raw == "" {
		return nil, nil
	}
	value, err := strconv.ParseBool(raw)
	if err != nil {
		return nil, err
	}
	return &value, nil
}

func safeWebhookDestination(raw, locale string) string {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return i18n.T(locale, "notice.webhook_destination_invalid")
	}
	destination := parsed.Scheme + "://" + parsed.Host
	if parsed.EscapedPath() != "" && parsed.EscapedPath() != "/" {
		destination += "/…"
	}
	return destination
}

func decodeWebhookEvents(raw string) []string {
	var values []string
	if json.Unmarshal([]byte(raw), &values) != nil {
		return []string{}
	}
	result := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || len([]rune(value)) > 80 {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

type adminWebhookEndpointItem struct {
	ID                    uuid.UUID  `json:"id"`
	OwnerType             string     `json:"owner_type"`
	OwnerID               uuid.UUID  `json:"owner_id"`
	URL                   string     `json:"url"`
	Events                []string   `json:"events"`
	Enabled               bool       `json:"enabled"`
	FailureCount          int        `json:"failure_count"`
	CredentialsConfigured bool       `json:"credentials_configured"`
	DisabledAt            *time.Time `json:"disabled_at"`
	CreatedAt             time.Time  `json:"created_at"`
	UpdatedAt             time.Time  `json:"updated_at"`
}

func toAdminWebhookEndpoint(item model.WebhookEndpoint, locale string) adminWebhookEndpointItem {
	return adminWebhookEndpointItem{
		ID: item.ID, OwnerType: item.OwnerType, OwnerID: item.OwnerID,
		URL: safeWebhookDestination(item.URL, locale), Events: decodeWebhookEvents(item.Events), Enabled: item.Enabled,
		FailureCount: item.FailureCount, CredentialsConfigured: len(item.SecretCipher) > 0 && len(item.SecretNonce) > 0,
		DisabledAt: item.DisabledAt, CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt,
	}
}

func (h Handler) AdminWebhookEndpoints(c *gin.Context) {
	page, pageSize := pagination(c)
	query := h.DB.Model(&model.WebhookEndpoint{})
	enabled, err := optionalBoolQuery(c, "enabled")
	if err != nil {
		response.Error(c, 422, 42601, "error.webhook_enabled_filter_invalid")
		return
	}
	if enabled != nil {
		query = query.Where("enabled = ?", *enabled)
	}
	if ownerType := strings.TrimSpace(c.Query("owner_type")); ownerType != "" {
		if ownerType != "user" && ownerType != "api_credential" {
			response.Error(c, 422, 42602, "error.webhook_owner_type_filter_invalid")
			return
		}
		query = query.Where("owner_type = ?", ownerType)
	}
	if ownerID := strings.TrimSpace(c.Query("owner_id")); ownerID != "" {
		parsed, parseErr := uuid.Parse(ownerID)
		if parseErr != nil {
			response.Error(c, 422, 42603, "error.webhook_owner_id_filter_invalid")
			return
		}
		query = query.Where("owner_id = ?", parsed)
	}
	if eventType := strings.TrimSpace(c.Query("event_type")); eventType != "" {
		if len([]rune(eventType)) > 80 {
			response.Error(c, 422, 42604, "error.webhook_event_filter_invalid")
			return
		}
		encoded, _ := json.Marshal([]string{eventType})
		query = query.Where("events @> CAST(? AS jsonb)", string(encoded))
	}
	if keyword := strings.TrimSpace(c.Query("q")); keyword != "" {
		like := "%" + keyword + "%"
		query = query.Where("url ILIKE ? OR owner_type ILIKE ? OR CAST(owner_id AS TEXT) ILIKE ?", like, like, like)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		response.Error(c, 500, 50602, "error.webhook_endpoint_list_fetch_failed")
		return
	}
	var stored []model.WebhookEndpoint
	if err := query.Order("created_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&stored).Error; err != nil {
		response.Error(c, 500, 50602, "error.webhook_endpoint_list_fetch_failed")
		return
	}
	items := make([]adminWebhookEndpointItem, 0, len(stored))
	for _, item := range stored {
		items = append(items, toAdminWebhookEndpoint(item, i18n.ResolveLocale(c)))
	}
	c.Header("Cache-Control", "no-store")
	response.Page(c, items, total, page, pageSize)
}

type adminWebhookEndpointUpdateRequest struct {
	Enabled *bool `json:"enabled"`
}

func (h Handler) UpdateAdminWebhookEndpoint(c *gin.Context) {
	endpointID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, 422, 42605, "error.webhook_endpoint_id_invalid")
		return
	}
	reason, ok := requireAdminChangeReason(c, "更新 Webhook 端点")
	if !ok {
		return
	}
	var req adminWebhookEndpointUpdateRequest
	if decodeStrictJSON(c, &req) != nil || req.Enabled == nil {
		response.Error(c, 422, 42606, "error.webhook_enabled_only_updatable")
		return
	}
	var item model.WebhookEndpoint
	err = h.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&item, "id = ?", endpointID).Error; err != nil {
			return err
		}
		if item.Enabled == *req.Enabled {
			return errIntegrationNoChange
		}
		if err := validateWebhookEndpointActivation(c.Request.Context(), item.URL, *req.Enabled); err != nil {
			return errIntegrationEndpointUnsafe
		}
		updates := map[string]any{"enabled": *req.Enabled}
		if *req.Enabled {
			updates["failure_count"], updates["disabled_at"] = 0, nil
		} else {
			now := time.Now()
			updates["disabled_at"] = &now
		}
		if err := tx.Model(&item).Updates(updates).Error; err != nil {
			return err
		}
		return tx.First(&item, "id = ?", item.ID).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		response.Error(c, 404, 40601, "error.webhook_endpoint_not_found")
		return
	}
	if errors.Is(err, errIntegrationNoChange) {
		response.Error(c, 409, 40611, "error.webhook_endpoint_already_in_target_state")
		return
	}
	if errors.Is(err, errIntegrationEndpointUnsafe) {
		response.Error(c, 422, 42263, "error.webhook_parameters_invalid")
		return
	}
	if err != nil {
		response.Error(c, 500, 50603, "error.webhook_endpoint_update_failed")
		return
	}
	h.audit(c, "webhook.endpoint.enabled.update", "webhook_endpoint", item.ID.String(), reason)
	c.Header("Cache-Control", "no-store")
	response.OK(c, toAdminWebhookEndpoint(item, i18n.ResolveLocale(c)))
}

func safeWebhookDiagnostic(statusCode int, raw, locale string) string {
	raw = strings.TrimSpace(raw)
	if strings.Contains(raw, "webhook endpoint is unavailable") {
		return i18n.T(locale, "notice.webhook_endpoint_unavailable")
	}
	if statusCode > 0 {
		if raw == "" {
			return i18n.Sprintf(i18n.T(locale, "notice.webhook_http_no_body"), map[string]interface{}{"Status": statusCode})
		}
		return i18n.Sprintf(i18n.T(locale, "notice.webhook_http_body_hidden"), map[string]interface{}{"Status": statusCode, "Bytes": len(raw)})
	}
	if raw != "" {
		return i18n.Sprintf(i18n.T(locale, "notice.webhook_conn_diag_hidden"), map[string]interface{}{"Bytes": len(raw)})
	}
	return ""
}

type adminWebhookDeliveryItem struct {
	ID              uuid.UUID  `json:"id"`
	EndpointID      uuid.UUID  `json:"endpoint_id"`
	EndpointURL     string     `json:"endpoint_url"`
	EventID         string     `json:"event_id"`
	EventType       string     `json:"event_type"`
	Status          string     `json:"status"`
	Attempts        int        `json:"attempts"`
	ResponseCode    int        `json:"response_code"`
	Diagnostic      string     `json:"diagnostic"`
	NextAttemptAt   *time.Time `json:"next_attempt_at"`
	DeliveredAt     *time.Time `json:"delivered_at"`
	EndpointEnabled bool       `json:"endpoint_enabled"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`

	StoredEndpointURL string `json:"-"`
	StoredResponse    string `json:"-"`
}

func webhookDeliveryListQuery(db *gorm.DB) *gorm.DB {
	return db.Table("webhook_deliveries wd").
		Select(`wd.id, wd.endpoint_id, we.url AS stored_endpoint_url, we.enabled AS endpoint_enabled,
			wd.event_id, wd.event_type, wd.status, wd.attempts, wd.response_code,
			wd.response_body AS stored_response, wd.next_attempt_at, wd.delivered_at,
			wd.created_at, wd.updated_at`).
		Joins("JOIN webhook_endpoints we ON we.id = wd.endpoint_id AND we.deleted_at IS NULL").
		Where("wd.deleted_at IS NULL")
}

func applyWebhookDeliveryFilters(c *gin.Context, query *gorm.DB) (*gorm.DB, error) {
	if status := strings.TrimSpace(c.Query("status")); status != "" {
		allowed := map[string]bool{"queued": true, "sending": true, "retrying": true, "delivered": true, "failed": true}
		if !allowed[status] {
			return nil, errors.New("invalid webhook status")
		}
		query = query.Where("wd.status = ?", status)
	}
	if endpointID := strings.TrimSpace(c.Query("endpoint_id")); endpointID != "" {
		parsed, err := uuid.Parse(endpointID)
		if err != nil {
			return nil, err
		}
		query = query.Where("wd.endpoint_id = ?", parsed)
	}
	if eventType := strings.TrimSpace(c.Query("event_type")); eventType != "" {
		if len([]rune(eventType)) > 80 {
			return nil, errors.New("invalid event type")
		}
		query = query.Where("wd.event_type = ?", eventType)
	}
	if keyword := strings.TrimSpace(c.Query("q")); keyword != "" {
		like := "%" + keyword + "%"
		query = query.Where("wd.event_id ILIKE ? OR wd.event_type ILIKE ?", like, like)
	}
	return query, nil
}

func (h Handler) AdminWebhookDeliveries(c *gin.Context) {
	page, pageSize := pagination(c)
	query, err := applyWebhookDeliveryFilters(c, webhookDeliveryListQuery(h.DB))
	if err != nil {
		response.Error(c, 422, 42607, "error.webhook_delivery_filter_invalid")
		return
	}
	var total int64
	if err := query.Session(&gorm.Session{}).Select("COUNT(DISTINCT wd.id)").Scan(&total).Error; err != nil {
		response.Error(c, 500, 50604, "error.webhook_delivery_list_fetch_failed")
		return
	}
	items := make([]adminWebhookDeliveryItem, 0)
	if err := query.Order("wd.created_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Scan(&items).Error; err != nil {
		response.Error(c, 500, 50604, "error.webhook_delivery_list_fetch_failed")
		return
	}
	for index := range items {
		items[index].EndpointURL = safeWebhookDestination(items[index].StoredEndpointURL, i18n.ResolveLocale(c))
		items[index].Diagnostic = safeWebhookDiagnostic(items[index].ResponseCode, items[index].StoredResponse, i18n.ResolveLocale(c))
	}
	c.Header("Cache-Control", "no-store")
	response.Page(c, items, total, page, pageSize)
}

func maskIntegrationRecipient(channel, recipient, locale string) string {
	recipient = strings.TrimSpace(recipient)
	if recipient == "" {
		return "—"
	}
	if channel == "admin" {
		return i18n.T(locale, "notice.admin_notification")
	}
	if channel == "email" {
		parts := strings.Split(recipient, "@")
		if len(parts) == 2 && parts[0] != "" && parts[1] != "" {
			local := []rune(parts[0])
			visible := string(local[0])
			if len(local) > 2 {
				visible += "***" + string(local[len(local)-1])
			} else {
				visible += "***"
			}
			return visible + "@" + parts[1]
		}
	}
	runes := []rune(recipient)
	if len(runes) <= 4 {
		return string(runes[0]) + "***"
	}
	return string(runes[:2]) + "***" + string(runes[len(runes)-2:])
}

func safeNotificationDiagnostic(raw, locale string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	lower := strings.ToLower(raw)
	switch {
	case strings.Contains(lower, "relay is not configured"):
		return i18n.T(locale, "notice.relay_not_configured")
	case strings.Contains(lower, "body decryption failed"):
		return i18n.T(locale, "notice.relay_decrypt_failed")
	case httpStatusDiagnosticPattern.MatchString(raw):
		match := httpStatusDiagnosticPattern.FindStringSubmatch(raw)
		return i18n.Sprintf(i18n.T(locale, "notice.relay_http_status"), map[string]interface{}{"Status": match[1]})
	default:
		return i18n.T(locale, "notice.delivery_failed_diag")
	}
}

type adminNotificationDeliveryItem struct {
	ID             uuid.UUID  `json:"id"`
	IdempotencyKey string     `json:"idempotency_key"`
	TemplateID     *uuid.UUID `json:"template_id"`
	TemplateCode   string     `json:"template_code"`
	TemplateName   string     `json:"template_name"`
	Channel        string     `json:"channel"`
	Recipient      string     `json:"recipient"`
	Subject        string     `json:"subject"`
	Status         string     `json:"status"`
	Attempts       int        `json:"attempts"`
	Diagnostic     string     `json:"diagnostic"`
	NextAttemptAt  *time.Time `json:"next_attempt_at"`
	SentAt         *time.Time `json:"sent_at"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`

	StoredRecipient string `json:"-"`
	StoredError     string `json:"-"`
}

func notificationDeliveryListQuery(db *gorm.DB) *gorm.DB {
	return db.Table("notification_deliveries nd").
		Select(`nd.id, nd.idempotency_key, nd.template_id, COALESCE(nt.code, '') AS template_code,
			COALESCE(nt.name, '') AS template_name, nd.channel, nd.recipient AS stored_recipient,
			nd.subject, nd.status, nd.attempts, nd.last_error AS stored_error,
			nd.next_attempt_at, nd.sent_at, nd.created_at, nd.updated_at`).
		Joins("LEFT JOIN notification_templates nt ON nt.id = nd.template_id AND nt.deleted_at IS NULL").
		Where("nd.deleted_at IS NULL")
}

func applyNotificationDeliveryFilters(c *gin.Context, query *gorm.DB) (*gorm.DB, error) {
	if status := strings.TrimSpace(c.Query("status")); status != "" {
		allowed := map[string]bool{"queued": true, "sending": true, "retrying": true, "sent": true, "failed": true}
		if !allowed[status] {
			return nil, errors.New("invalid notification status")
		}
		query = query.Where("nd.status = ?", status)
	}
	if channel := strings.TrimSpace(c.Query("channel")); channel != "" {
		if !validNotificationChannel(channel) {
			return nil, errors.New("invalid notification channel")
		}
		query = query.Where("nd.channel = ?", channel)
	}
	if templateID := strings.TrimSpace(c.Query("template_id")); templateID != "" {
		parsed, err := uuid.Parse(templateID)
		if err != nil {
			return nil, err
		}
		query = query.Where("nd.template_id = ?", parsed)
	}
	if keyword := strings.TrimSpace(c.Query("q")); keyword != "" {
		like := "%" + keyword + "%"
		query = query.Where("nd.idempotency_key ILIKE ? OR nd.subject ILIKE ? OR COALESCE(nt.code, '') ILIKE ?", like, like, like)
	}
	return query, nil
}

func (h Handler) AdminNotificationDeliveries(c *gin.Context) {
	page, pageSize := pagination(c)
	query, err := applyNotificationDeliveryFilters(c, notificationDeliveryListQuery(h.DB))
	if err != nil {
		response.Error(c, 422, 42608, "error.notification_delivery_filter_invalid")
		return
	}
	var total int64
	if err := query.Session(&gorm.Session{}).Select("COUNT(DISTINCT nd.id)").Scan(&total).Error; err != nil {
		response.Error(c, 500, 50605, "error.notification_delivery_list_fetch_failed")
		return
	}
	items := make([]adminNotificationDeliveryItem, 0)
	if err := query.Order("nd.created_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Scan(&items).Error; err != nil {
		response.Error(c, 500, 50605, "error.notification_delivery_list_fetch_failed")
		return
	}
	for index := range items {
		items[index].Recipient = maskIntegrationRecipient(items[index].Channel, items[index].StoredRecipient, i18n.ResolveLocale(c))
		items[index].Diagnostic = safeNotificationDiagnostic(items[index].StoredError, i18n.ResolveLocale(c))
	}
	c.Header("Cache-Control", "no-store")
	response.Page(c, items, total, page, pageSize)
}

type adminNotificationTemplateRequest struct {
	Code      string   `json:"code"`
	Name      string   `json:"name"`
	Audience  string   `json:"audience"`
	Channel   string   `json:"channel"`
	Locale    string   `json:"locale"`
	Subject   string   `json:"subject"`
	Body      string   `json:"body"`
	Variables []string `json:"variables"`
	Enabled   *bool    `json:"enabled"`
}

type adminNotificationTemplateItem struct {
	ID        uuid.UUID `json:"id"`
	Code      string    `json:"code"`
	Name      string    `json:"name"`
	Audience  string    `json:"audience"`
	Channel   string    `json:"channel"`
	Locale    string    `json:"locale"`
	Subject   string    `json:"subject"`
	Body      string    `json:"body"`
	Variables []string  `json:"variables"`
	Enabled   bool      `json:"enabled"`
	Version   int       `json:"version"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func validNotificationChannel(value string) bool {
	return value == "email" || value == "telegram" || value == "wecom" || value == "admin" || value == "in_app"
}

func containsUnsafeNotificationControl(value string, allowLayout bool) bool {
	for _, character := range value {
		if character == '\x00' {
			return true
		}
		if unicode.IsControl(character) && !(allowLayout && (character == '\n' || character == '\r' || character == '\t')) {
			return true
		}
	}
	return false
}

func normalizeNotificationLocale(value string) string {
	parts := strings.Split(strings.TrimSpace(value), "-")
	if len(parts) == 0 {
		return ""
	}
	parts[0] = strings.ToLower(parts[0])
	for index := 1; index < len(parts); index++ {
		if len(parts[index]) == 2 || len(parts[index]) == 3 {
			parts[index] = strings.ToUpper(parts[index])
		}
	}
	return strings.Join(parts, "-")
}

func validateNotificationTemplatePlaceholders(subject, body string, variables []string) error {
	content := subject + "\n" + body
	matches := notificationPlaceholderPattern.FindAllStringSubmatch(content, -1)
	withoutValidPlaceholders := notificationPlaceholderPattern.ReplaceAllString(content, "")
	if strings.Contains(withoutValidPlaceholders, "{{") || strings.Contains(withoutValidPlaceholders, "}}") {
		return errors.New("malformed notification placeholder")
	}
	used := make(map[string]struct{}, len(matches))
	for _, match := range matches {
		used[match[1]] = struct{}{}
	}
	declared := make(map[string]struct{}, len(variables))
	for _, variable := range variables {
		declared[variable] = struct{}{}
	}
	if len(used) != len(declared) {
		return errors.New("notification variables do not match placeholders")
	}
	for variable := range used {
		if _, exists := declared[variable]; !exists {
			return errors.New("undeclared notification placeholder")
		}
	}
	return nil
}

func (r *adminNotificationTemplateRequest) normalizeAndValidate() error {
	r.Code = strings.ToLower(strings.TrimSpace(r.Code))
	r.Name = strings.TrimSpace(r.Name)
	r.Audience = strings.ToLower(strings.TrimSpace(r.Audience))
	if r.Audience == "" {
		r.Audience = "admin"
	}
	r.Channel = strings.ToLower(strings.TrimSpace(r.Channel))
	r.Locale = normalizeNotificationLocale(r.Locale)
	r.Subject = strings.TrimSpace(r.Subject)
	r.Body = strings.TrimSpace(r.Body)
	if !notificationTemplateCodePattern.MatchString(r.Code) || len([]rune(r.Name)) < 2 || len([]rune(r.Name)) > 160 {
		return errors.New("invalid notification template identity")
	}
	if (r.Audience != "admin" && r.Audience != "user") || (r.Audience == "user" && r.Channel != "in_app" && r.Channel != "email") || (r.Audience == "admin" && r.Channel == "in_app") || !validNotificationChannel(r.Channel) || !notificationLocalePattern.MatchString(r.Locale) || len(r.Locale) > 16 {
		return errors.New("invalid notification channel or locale")
	}
	if len([]rune(r.Subject)) < 1 || len([]rune(r.Subject)) > 255 || containsUnsafeNotificationControl(r.Subject, false) {
		return errors.New("invalid notification subject")
	}
	if len([]rune(r.Body)) < 1 || len([]rune(r.Body)) > 20_000 || containsUnsafeNotificationControl(r.Body, true) {
		return errors.New("invalid notification body")
	}
	if r.Enabled == nil || len(r.Variables) > 50 {
		return errors.New("invalid notification state or variable count")
	}
	seen := make(map[string]struct{}, len(r.Variables))
	normalized := make([]string, 0, len(r.Variables))
	for _, variable := range r.Variables {
		variable = strings.ToLower(strings.TrimSpace(variable))
		if !notificationVariablePattern.MatchString(variable) {
			return errors.New("invalid notification variable")
		}
		if _, exists := seen[variable]; exists {
			return errors.New("duplicate notification variable")
		}
		seen[variable] = struct{}{}
		normalized = append(normalized, variable)
	}
	sort.Strings(normalized)
	r.Variables = normalized
	return validateNotificationTemplatePlaceholders(r.Subject, r.Body, r.Variables)
}

func decodeNotificationVariables(raw string) ([]string, error) {
	var variables []string
	if err := json.Unmarshal([]byte(raw), &variables); err != nil {
		return nil, err
	}
	if variables == nil {
		variables = []string{}
	}
	for _, variable := range variables {
		if !notificationVariablePattern.MatchString(variable) {
			return nil, errors.New("stored notification variables are invalid")
		}
	}
	return variables, nil
}

func toAdminNotificationTemplate(item model.NotificationTemplate) adminNotificationTemplateItem {
	variables, err := decodeNotificationVariables(item.Variables)
	if err != nil {
		variables = []string{}
	}
	return adminNotificationTemplateItem{
		ID: item.ID, Code: item.Code, Name: item.Name, Audience: item.Audience, Channel: item.Channel, Locale: item.Locale,
		Subject: item.Subject, Body: item.Body, Variables: variables, Enabled: item.Enabled,
		Version: item.Version, CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt,
	}
}

func notificationTemplateValues(req adminNotificationTemplateRequest) (map[string]any, error) {
	variables, err := json.Marshal(req.Variables)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"code": req.Code, "name": req.Name, "audience": req.Audience, "channel": req.Channel, "locale": req.Locale,
		"subject": req.Subject, "body": req.Body, "variables": string(variables), "enabled": *req.Enabled,
	}, nil
}

func (h Handler) AdminNotificationTemplates(c *gin.Context) {
	page, pageSize := pagination(c)
	query := h.DB.Model(&model.NotificationTemplate{})
	if channel := strings.TrimSpace(c.Query("channel")); channel != "" {
		if !validNotificationChannel(channel) {
			response.Error(c, 422, 42609, "error.notification_template_channel_filter_invalid")
			return
		}
		query = query.Where("channel = ?", channel)
	}
	if locale := strings.TrimSpace(c.Query("locale")); locale != "" {
		locale = normalizeNotificationLocale(locale)
		if !notificationLocalePattern.MatchString(locale) || len(locale) > 16 {
			response.Error(c, 422, 42610, "error.notification_template_locale_filter_invalid")
			return
		}
		query = query.Where("locale = ?", locale)
	}
	enabled, err := optionalBoolQuery(c, "enabled")
	if err != nil {
		response.Error(c, 422, 42611, "error.notification_template_enabled_filter_invalid")
		return
	}
	if enabled != nil {
		query = query.Where("enabled = ?", *enabled)
	}
	if keyword := strings.TrimSpace(c.Query("q")); keyword != "" {
		like := "%" + keyword + "%"
		query = query.Where("code ILIKE ? OR name ILIKE ? OR subject ILIKE ?", like, like, like)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		response.Error(c, 500, 50606, "error.notification_template_list_fetch_failed")
		return
	}
	var stored []model.NotificationTemplate
	if err := query.Order("updated_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&stored).Error; err != nil {
		response.Error(c, 500, 50606, "error.notification_template_list_fetch_failed")
		return
	}
	items := make([]adminNotificationTemplateItem, 0, len(stored))
	for _, item := range stored {
		items = append(items, toAdminNotificationTemplate(item))
	}
	response.Page(c, items, total, page, pageSize)
}

func (h Handler) CreateAdminNotificationTemplate(c *gin.Context) {
	reason, ok := requireAdminChangeReason(c, "创建通知模板")
	if !ok {
		return
	}
	var req adminNotificationTemplateRequest
	if decodeStrictJSON(c, &req) != nil || req.normalizeAndValidate() != nil {
		response.Error(c, 422, 42612, "error.notification_template_content_invalid")
		return
	}
	values, _ := notificationTemplateValues(req)
	variables := values["variables"].(string)
	item := model.NotificationTemplate{Code: req.Code, Name: req.Name, Audience: req.Audience, Channel: req.Channel, Locale: req.Locale, Subject: req.Subject, Body: req.Body, Variables: variables, Enabled: *req.Enabled, Version: 1}
	if err := h.DB.Transaction(func(tx *gorm.DB) error {
		return createWithExplicitColumns(tx, &item, map[string]any{"enabled": *req.Enabled})
	}); err != nil {
		response.Error(c, 409, 40602, "error.notification_template_code_conflict")
		return
	}
	item.Enabled = *req.Enabled
	h.audit(c, "notification-template.create", "notification_template", item.ID.String(), reason)
	response.Created(c, toAdminNotificationTemplate(item))
}

func (h Handler) UpdateAdminNotificationTemplate(c *gin.Context) {
	templateID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, 422, 42613, "error.notification_template_id_invalid")
		return
	}
	reason, ok := requireAdminChangeReason(c, "更新通知模板")
	if !ok {
		return
	}
	var req adminNotificationTemplateRequest
	if decodeStrictJSON(c, &req) != nil || req.normalizeAndValidate() != nil {
		response.Error(c, 422, 42612, "error.notification_template_content_invalid")
		return
	}
	values, _ := notificationTemplateValues(req)
	values["version"] = gorm.Expr("version + 1")
	var item model.NotificationTemplate
	err = h.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&item, "id = ?", templateID).Error; err != nil {
			return err
		}
		if err := tx.Model(&item).Updates(values).Error; err != nil {
			return err
		}
		return tx.First(&item, "id = ?", item.ID).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		response.Error(c, 404, 40603, "error.notification_template_not_found")
		return
	}
	if err != nil {
		response.Error(c, 409, 40604, "error.notification_template_code_conflict_on_update")
		return
	}
	h.audit(c, "notification-template.update", "notification_template", item.ID.String(), reason+"；version="+strconv.Itoa(item.Version))
	response.OK(c, toAdminNotificationTemplate(item))
}

func (h Handler) DeleteAdminNotificationTemplate(c *gin.Context) {
	templateID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, 422, 42613, "error.notification_template_id_invalid")
		return
	}
	reason, ok := requireAdminChangeReason(c, "删除通知模板")
	if !ok {
		return
	}
	var item model.NotificationTemplate
	err = h.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&item, "id = ?", templateID).Error; err != nil {
			return err
		}
		var deliveries int64
		if err := tx.Unscoped().Model(&model.NotificationDelivery{}).Where("template_id = ?", item.ID).Count(&deliveries).Error; err != nil {
			return err
		}
		if deliveries > 0 {
			return errTemplateHasDeliveries
		}
		return tx.Delete(&item).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		response.Error(c, 404, 40603, "error.notification_template_not_found")
		return
	}
	if errors.Is(err, errTemplateHasDeliveries) {
		response.Error(c, 409, 40605, "error.notification_template_has_deliveries")
		return
	}
	if err != nil {
		response.Error(c, 500, 50607, "error.notification_template_delete_failed")
		return
	}
	h.audit(c, "notification-template.delete", "notification_template", item.ID.String(), reason)
	response.OK(c, gin.H{"deleted": true})
}

type adminNotificationTestRequest struct {
	Recipient      string            `json:"recipient"`
	Variables      map[string]string `json:"variables"`
	IdempotencyKey string            `json:"idempotency_key"`
}

func validateNotificationRecipient(channel, recipient string) error {
	recipient = strings.TrimSpace(recipient)
	if len([]rune(recipient)) < 1 || len([]rune(recipient)) > 255 || containsUnsafeNotificationControl(recipient, false) {
		return errors.New("invalid notification recipient")
	}
	switch channel {
	case "email":
		address, err := mail.ParseAddress(recipient)
		if err != nil || address.Address != recipient || len(recipient) > 190 {
			return errors.New("invalid email recipient")
		}
	case "telegram", "wecom":
		if !integrationIdentifierPattern.MatchString(recipient) {
			return errors.New("invalid connector recipient")
		}
	case "admin":
		if recipient != "admin" {
			if _, err := uuid.Parse(recipient); err != nil {
				return errors.New("invalid admin recipient")
			}
		}
	case "in_app":
		if recipient != "event_user" {
			return errors.New("invalid in-app recipient")
		}
	default:
		return errors.New("invalid notification channel")
	}
	return nil
}

func validateNotificationTestVariables(declared []string, supplied map[string]string) error {
	if len(declared) != len(supplied) {
		return errors.New("notification variable set does not match template")
	}
	allowed := make(map[string]struct{}, len(declared))
	for _, variable := range declared {
		allowed[variable] = struct{}{}
	}
	for variable, value := range supplied {
		if _, exists := allowed[variable]; !exists {
			return errors.New("unknown notification variable")
		}
		if len([]rune(value)) < 1 || len([]rune(value)) > 2_000 || containsUnsafeNotificationControl(value, true) {
			return errors.New("invalid notification variable value")
		}
	}
	return nil
}

func renderNotificationTemplate(subject, body string, variables map[string]string) (string, string, error) {
	for variable, value := range variables {
		placeholder := "{{" + variable + "}}"
		subject = strings.ReplaceAll(subject, placeholder, value)
		body = strings.ReplaceAll(body, placeholder, value)
	}
	if strings.Contains(subject, "{{") || strings.Contains(subject, "}}") || strings.Contains(body, "{{") || strings.Contains(body, "}}") {
		return "", "", errors.New("unresolved notification placeholder")
	}
	if len([]rune(subject)) < 1 || len([]rune(subject)) > 255 || len([]rune(body)) < 1 || len([]rune(body)) > 50_000 {
		return "", "", errors.New("rendered notification exceeds limits")
	}
	return subject, body, nil
}

func integrationQueueState(err error) string {
	if err == nil {
		return "queued"
	}
	if errors.Is(err, asynq.ErrDuplicateTask) || errors.Is(err, asynq.ErrTaskIDConflict) {
		return "already_queued"
	}
	return "scheduler_pending"
}

func (h Handler) enqueueIntegrationDelivery(taskType, deliveryID, queueName string) string {
	client := queue.NewClient(h.Cfg, h.DB)
	defer client.Close()
	_, err := client.Enqueue(taskType, map[string]string{"delivery_id": deliveryID}, asynq.Queue(queueName), asynq.Unique(2*time.Minute))
	return integrationQueueState(err)
}

func loadAdminNotificationDelivery(db *gorm.DB, deliveryID uuid.UUID, locale string) (adminNotificationDeliveryItem, error) {
	items := make([]adminNotificationDeliveryItem, 0, 1)
	if err := notificationDeliveryListQuery(db).Where("nd.id = ?", deliveryID).Limit(1).Scan(&items).Error; err != nil {
		return adminNotificationDeliveryItem{}, err
	}
	if len(items) == 0 {
		return adminNotificationDeliveryItem{}, gorm.ErrRecordNotFound
	}
	item := items[0]
	item.Recipient = maskIntegrationRecipient(item.Channel, item.StoredRecipient, locale)
	item.Diagnostic = safeNotificationDiagnostic(item.StoredError, locale)
	return item, nil
}

func loadAdminWebhookDelivery(db *gorm.DB, deliveryID uuid.UUID, locale string) (adminWebhookDeliveryItem, error) {
	items := make([]adminWebhookDeliveryItem, 0, 1)
	if err := webhookDeliveryListQuery(db).Where("wd.id = ?", deliveryID).Limit(1).Scan(&items).Error; err != nil {
		return adminWebhookDeliveryItem{}, err
	}
	if len(items) == 0 {
		return adminWebhookDeliveryItem{}, gorm.ErrRecordNotFound
	}
	item := items[0]
	item.EndpointURL = safeWebhookDestination(item.StoredEndpointURL, locale)
	item.Diagnostic = safeWebhookDiagnostic(item.ResponseCode, item.StoredResponse, locale)
	return item, nil
}

func (h Handler) SendAdminNotificationTest(c *gin.Context) {
	templateID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, 422, 42613, "error.notification_template_id_invalid")
		return
	}
	adminID, err := uuid.Parse(c.GetString("subject"))
	if err != nil {
		response.Error(c, 401, 40103, "error.invalid_admin_identity")
		return
	}
	reason, ok := requireAdminChangeReason(c, "发送模板测试通知")
	if !ok {
		return
	}
	var req adminNotificationTestRequest
	if decodeStrictJSON(c, &req) != nil {
		response.Error(c, 422, 42614, "error.test_notification_fields_invalid")
		return
	}
	req.Recipient = strings.TrimSpace(req.Recipient)
	req.IdempotencyKey = strings.TrimSpace(req.IdempotencyKey)
	if !notificationTestKeyPattern.MatchString(req.IdempotencyKey) || len(req.Variables) > 50 {
		response.Error(c, 422, 42614, "error.test_notification_idempotency_invalid")
		return
	}
	digest := sha256.Sum256([]byte(req.IdempotencyKey))
	idempotencyKey := "admin-test:" + adminID.String() + ":" + templateID.String() + ":" + hex.EncodeToString(digest[:])
	var delivery model.NotificationDelivery
	created := false
	err = h.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("idempotency_key = ?", idempotencyKey).First(&delivery).Error; err == nil {
			return nil
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtext(?))", "notification-test:"+adminID.String()).Error; err != nil {
			return err
		}
		// The first lookup keeps the common idempotent path cheap. Re-check after
		// the per-admin lock to close the concurrent-create and rate-limit race.
		if err := tx.Where("idempotency_key = ?", idempotencyKey).First(&delivery).Error; err == nil {
			return nil
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var recent int64
		prefix := "admin-test:" + adminID.String() + ":%"
		if err := tx.Model(&model.NotificationDelivery{}).Where("idempotency_key LIKE ? AND created_at >= ?", prefix, time.Now().Add(-time.Hour)).Count(&recent).Error; err != nil {
			return err
		}
		if recent >= 20 {
			return errNotificationTestRateLimit
		}
		var template model.NotificationTemplate
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).First(&template, "id = ?", templateID).Error; err != nil {
			return err
		}
		variables, err := decodeNotificationVariables(template.Variables)
		if err != nil || validateNotificationTemplatePlaceholders(template.Subject, template.Body, variables) != nil {
			return errors.New("stored notification template is invalid")
		}
		if err := validateNotificationRecipient(template.Channel, req.Recipient); err != nil {
			return err
		}
		if err := validateNotificationTestVariables(variables, req.Variables); err != nil {
			return err
		}
		subject, body, err := renderNotificationTemplate(template.Subject, template.Body, req.Variables)
		if err != nil {
			return err
		}
		delivery = model.NotificationDelivery{Base: model.Base{ID: uuid.New()}, IdempotencyKey: idempotencyKey, TemplateID: &template.ID, Channel: template.Channel, Recipient: req.Recipient, Subject: subject, Status: "queued"}
		ciphertext, nonce, _, err := h.Vault.Encrypt(body, delivery.ID[:])
		if err != nil {
			return err
		}
		delivery.BodyCipher, delivery.BodyNonce = ciphertext, nonce
		if err := tx.Create(&delivery).Error; err != nil {
			return err
		}
		created = true
		return nil
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		response.Error(c, 404, 40603, "error.notification_template_not_found")
		return
	}
	if err != nil {
		if errors.Is(err, errNotificationTestRateLimit) {
			response.Error(c, 429, 42920, "error.test_notification_rate_limit")
		} else {
			response.Error(c, 422, 42615, "error.notification_render_invalid")
		}
		return
	}
	queueState := h.enqueueIntegrationDelivery(queue.TypeNotificationDispatch, delivery.ID.String(), "default")
	h.audit(c, "notification-template.test", "notification_delivery", delivery.ID.String(), fmt.Sprintf("%s；template_id=%s；created=%t；queue_state=%s", reason, templateID, created, queueState))
	item, loadErr := loadAdminNotificationDelivery(h.DB, delivery.ID, i18n.ResolveLocale(c))
	if loadErr != nil {
		response.Error(c, 500, 50608, "error.test_notification_created_security_summary_fetch_failed")
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(202, response.Envelope{Code: 0, Message: "accepted", Data: gin.H{"delivery": item, "deduplicated": !created, "queue_state": queueState}})
}

func (h Handler) RetryAdminNotificationDelivery(c *gin.Context) {
	deliveryID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, 422, 42616, "error.notification_delivery_id_invalid")
		return
	}
	reason, ok := requireAdminChangeReason(c, "重试通知投递")
	if !ok {
		return
	}
	var delivery model.NotificationDelivery
	err = h.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&delivery, "id = ?", deliveryID).Error; err != nil {
			return err
		}
		if delivery.Status != "failed" {
			return errIntegrationRetryState
		}
		result := tx.Model(&model.NotificationDelivery{}).Where("id = ? AND status = ?", delivery.ID, "failed").Updates(map[string]any{"status": "queued", "attempts": 0, "last_error": "", "next_attempt_at": nil, "sent_at": nil})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errIntegrationRetryState
		}
		return nil
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		response.Error(c, 404, 40606, "error.notification_delivery_not_found")
		return
	}
	if errors.Is(err, errIntegrationRetryState) {
		response.Error(c, 409, 40607, "error.notification_retry_terminal_failed_only")
		return
	}
	if err != nil {
		response.Error(c, 500, 50609, "error.notification_delivery_retry_failed")
		return
	}
	queueState := h.enqueueIntegrationDelivery(queue.TypeNotificationDispatch, delivery.ID.String(), "default")
	h.audit(c, "notification-delivery.retry", "notification_delivery", delivery.ID.String(), reason+"；queue_state="+queueState)
	item, loadErr := loadAdminNotificationDelivery(h.DB, delivery.ID, i18n.ResolveLocale(c))
	if loadErr != nil {
		response.Error(c, 500, 50609, "error.notification_requeued_security_summary_fetch_failed")
		return
	}
	c.JSON(202, response.Envelope{Code: 0, Message: "accepted", Data: gin.H{"delivery": item, "queue_state": queueState}})
}

func (h Handler) RetryAdminWebhookDelivery(c *gin.Context) {
	deliveryID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, 422, 42617, "error.webhook_delivery_id_invalid")
		return
	}
	reason, ok := requireAdminChangeReason(c, "重试 Webhook 投递")
	if !ok {
		return
	}
	var delivery model.WebhookDelivery
	err = h.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&delivery, "id = ?", deliveryID).Error; err != nil {
			return err
		}
		if delivery.Status != "failed" {
			return errIntegrationRetryState
		}
		var endpoint model.WebhookEndpoint
		if err := tx.Clauses(clause.Locking{Strength: "SHARE"}).First(&endpoint, "id = ?", delivery.EndpointID).Error; err != nil {
			return err
		}
		if !endpoint.Enabled {
			return errIntegrationEndpointDisabled
		}
		result := tx.Model(&model.WebhookDelivery{}).Where("id = ? AND status = ?", delivery.ID, "failed").Updates(map[string]any{"status": "queued", "attempts": 0, "response_code": 0, "response_body": "", "next_attempt_at": nil, "delivered_at": nil})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errIntegrationRetryState
		}
		return nil
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		response.Error(c, 404, 40608, "error.webhook_delivery_or_endpoint_not_found")
		return
	}
	if errors.Is(err, errIntegrationRetryState) {
		response.Error(c, 409, 40609, "error.webhook_retry_terminal_failed_only")
		return
	}
	if errors.Is(err, errIntegrationEndpointDisabled) {
		response.Error(c, 409, 40610, "error.webhook_endpoint_disabled")
		return
	}
	if err != nil {
		response.Error(c, 500, 50610, "error.webhook_delivery_retry_failed")
		return
	}
	queueState := h.enqueueIntegrationDelivery(queue.TypeWebhookDeliver, delivery.ID.String(), "critical")
	h.audit(c, "webhook-delivery.retry", "webhook_delivery", delivery.ID.String(), reason+"；queue_state="+queueState)
	item, loadErr := loadAdminWebhookDelivery(h.DB, delivery.ID, i18n.ResolveLocale(c))
	if loadErr != nil {
		response.Error(c, 500, 50610, "error.webhook_requeued_security_summary_fetch_failed")
		return
	}
	c.JSON(202, response.Envelope{Code: 0, Message: "accepted", Data: gin.H{"delivery": item, "queue_state": queueState}})
}
