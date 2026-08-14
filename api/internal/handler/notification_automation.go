package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/mail"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"linlinqi/api/internal/model"
	"linlinqi/api/internal/security"
	"linlinqi/api/pkg/response"
)

type notificationEventDefinition struct {
	Code      string   `json:"code"`
	Group     string   `json:"group"`
	Name      string   `json:"name"`
	Severity  string   `json:"severity"`
	Variables []string `json:"variables"`
}

var notificationCommonVariables = []string{"event", "occurred_at", "summary", "entity_id", "status", "amount", "currency", "email", "ip", "stock", "product_name", "order_no", "channel"}

var notificationSupportedLocales = map[string]bool{"zh-CN": true, "zh-TW": true, "en": true, "vi": true, "ru": true, "ja": true, "ko": true, "th": true}

func supportedNotificationLocale(value string) string {
	value = normalizeNotificationLocale(value)
	if notificationSupportedLocales[value] {
		return value
	}
	return "zh-CN"
}

var notificationEventCatalog = []notificationEventDefinition{
	{Code: "user.registered", Group: "account", Name: "用户注册", Severity: "info"},
	{Code: "user.login.succeeded", Group: "account", Name: "用户登录成功", Severity: "info"},
	{Code: "user.login.failed", Group: "security", Name: "用户登录失败", Severity: "warning"},
	{Code: "admin.login.failed", Group: "security", Name: "管理员登录失败", Severity: "critical"},
	{Code: "order.created", Group: "order", Name: "订单创建", Severity: "info"},
	{Code: "order.paid", Group: "order", Name: "订单支付成功", Severity: "info"},
	{Code: "order.processing", Group: "order", Name: "订单进入交付", Severity: "info"},
	{Code: "order.delivered", Group: "order", Name: "订单交付成功", Severity: "info"},
	{Code: "order.failed", Group: "order", Name: "订单交付失败", Severity: "critical"},
	{Code: "order.refunded", Group: "order", Name: "订单退款完成", Severity: "warning"},
	{Code: "recharge.created", Group: "finance", Name: "充值单创建", Severity: "info"},
	{Code: "recharge.succeeded", Group: "finance", Name: "充值到账", Severity: "info"},
	{Code: "recharge.failed", Group: "finance", Name: "充值失败", Severity: "critical"},
	{Code: "openapi.credential.created", Group: "openapi", Name: "OpenAPI 凭证创建", Severity: "warning"},
	{Code: "openapi.call.succeeded", Group: "openapi", Name: "OpenAPI 调用成功", Severity: "info"},
	{Code: "openapi.call.failed", Group: "openapi", Name: "OpenAPI 调用失败", Severity: "warning"},
	{Code: "openapi.order.created", Group: "openapi", Name: "OpenAPI 订单创建", Severity: "info"},
	{Code: "inventory.low_stock", Group: "inventory", Name: "库存低于预警线", Severity: "warning"},
	{Code: "inventory.out_of_stock", Group: "inventory", Name: "库存售罄", Severity: "critical"},
	{Code: "inventory.restocked", Group: "inventory", Name: "库存补货", Severity: "info"},
	{Code: "supplier.sync.succeeded", Group: "supplier", Name: "上游同步成功", Severity: "info"},
	{Code: "supplier.sync.failed", Group: "supplier", Name: "上游同步失败", Severity: "critical"},
	{Code: "procurement.created", Group: "supplier", Name: "采购单创建", Severity: "info"},
	{Code: "procurement.succeeded", Group: "supplier", Name: "采购成功", Severity: "info"},
	{Code: "procurement.failed", Group: "supplier", Name: "采购失败", Severity: "critical"},
	{Code: "risk.blocked", Group: "security", Name: "风控拒绝交易", Severity: "critical"},
	{Code: "security.high_risk", Group: "security", Name: "高风险安全事件", Severity: "critical"},
}

func init() {
	for index := range notificationEventCatalog {
		notificationEventCatalog[index].Variables = append([]string(nil), notificationCommonVariables...)
	}
}

func notificationEvent(code string) (notificationEventDefinition, bool) {
	for _, item := range notificationEventCatalog {
		if item.Code == code {
			return item, true
		}
	}
	return notificationEventDefinition{}, false
}

func (h Handler) AdminNotificationEvents(c *gin.Context) {
	response.OK(c, notificationEventCatalog)
}

type notificationSubscriptionRequest struct {
	Audience   string     `json:"audience"`
	EventCode  string     `json:"event_code"`
	Channel    string     `json:"channel"`
	Recipient  string     `json:"recipient"`
	TemplateID *uuid.UUID `json:"template_id"`
	Locale     string     `json:"locale"`
	Enabled    *bool      `json:"enabled"`
}

type notificationSubscriptionDTO struct {
	ID           uuid.UUID `json:"id"`
	Audience     string    `json:"audience"`
	EventCode    string    `json:"event_code"`
	Channel      string    `json:"channel"`
	Recipient    string    `json:"recipient"`
	TemplateID   uuid.UUID `json:"template_id"`
	TemplateName string    `json:"template_name"`
	Locale       string    `json:"locale"`
	Enabled      bool      `json:"enabled"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func normalizeNotificationRecipient(channel, value string) (string, error) {
	value = strings.TrimSpace(value)
	if utf8.RuneCountInString(value) < 1 || utf8.RuneCountInString(value) > 255 || containsUnsafeNotificationControl(value, false) {
		return "", errors.New("invalid recipient")
	}
	switch channel {
	case "email":
		address, err := mail.ParseAddress(value)
		if err != nil || !strings.EqualFold(address.Address, value) {
			return "", errors.New("invalid email recipient")
		}
		return strings.ToLower(address.Address), nil
	case "telegram":
		if !regexp.MustCompile(`^-?[0-9]{5,20}$|^@[A-Za-z][A-Za-z0-9_]{4,31}$`).MatchString(value) {
			return "", errors.New("invalid telegram recipient")
		}
	case "wecom":
		if !regexp.MustCompile(`^[A-Za-z0-9_@.-]{1,128}$`).MatchString(value) {
			return "", errors.New("invalid wecom recipient")
		}
	case "admin":
		if value != "all" && value != "system.manage" {
			return "", errors.New("invalid admin recipient")
		}
	case "in_app":
		if value != "event_user" {
			return "", errors.New("invalid in-app recipient")
		}
	default:
		return "", errors.New("invalid channel")
	}
	return value, nil
}

func (r *notificationSubscriptionRequest) normalizeAndValidate(db *gorm.DB) (model.NotificationTemplate, error) {
	r.EventCode = strings.ToLower(strings.TrimSpace(r.EventCode))
	r.Audience = strings.ToLower(strings.TrimSpace(r.Audience))
	if r.Audience == "" {
		r.Audience = "admin"
	}
	r.Channel = strings.ToLower(strings.TrimSpace(r.Channel))
	r.Locale = normalizeNotificationLocale(r.Locale)
	_, eventOK := notificationEvent(r.EventCode)
	if (r.Audience != "admin" && r.Audience != "user") || !validNotificationChannel(r.Channel) || (r.Audience == "user" && r.Channel != "in_app" && r.Channel != "email") || (r.Audience == "admin" && r.Channel == "in_app") || !eventOK || r.TemplateID == nil || r.Enabled == nil || !notificationLocalePattern.MatchString(r.Locale) {
		return model.NotificationTemplate{}, errors.New("invalid subscription")
	}
	if r.Audience == "user" && r.Channel == "email" && strings.TrimSpace(r.Recipient) == "event_user" {
		// The actual address is resolved from the event owner at dispatch time;
		// a fixed address here would leak one customer's notification to another.
		r.Recipient = "event_user"
	} else {
		recipient, err := normalizeNotificationRecipient(r.Channel, r.Recipient)
		if err != nil {
			return model.NotificationTemplate{}, err
		}
		r.Recipient = recipient
	}
	var template model.NotificationTemplate
	if err := db.First(&template, "id = ?", *r.TemplateID).Error; err != nil || template.Channel != r.Channel || template.Audience != r.Audience || template.Locale != r.Locale || !template.Enabled {
		return model.NotificationTemplate{}, errors.New("invalid template")
	}
	variables, err := decodeNotificationVariables(template.Variables)
	if err != nil {
		return model.NotificationTemplate{}, err
	}
	definition, _ := notificationEvent(r.EventCode)
	allowed := map[string]bool{}
	for _, variable := range definition.Variables {
		allowed[variable] = true
	}
	for _, variable := range variables {
		if !allowed[variable] {
			return model.NotificationTemplate{}, errors.New("template variable is unavailable for event")
		}
	}
	return template, nil
}

func subscriptionDTO(item model.NotificationSubscription, name string) notificationSubscriptionDTO {
	return notificationSubscriptionDTO{ID: item.ID, Audience: item.Audience, EventCode: item.EventCode, Channel: item.Channel, Recipient: maskIntegrationRecipient(item.Channel, item.Recipient, "zh-CN"), TemplateID: item.TemplateID, TemplateName: name, Locale: item.Locale, Enabled: item.Enabled, CreatedAt: item.CreatedAt, UpdatedAt: item.UpdatedAt}
}

func (h Handler) AdminNotificationSubscriptions(c *gin.Context) {
	page, pageSize := pagination(c)
	query := h.DB.Model(&model.NotificationSubscription{})
	if event := strings.TrimSpace(c.Query("event_code")); event != "" {
		query = query.Where("event_code = ?", event)
	}
	if channel := strings.TrimSpace(c.Query("channel")); channel != "" {
		query = query.Where("channel = ?", channel)
	}
	if audience := strings.TrimSpace(c.Query("audience")); audience != "" {
		if audience != "admin" && audience != "user" {
			response.Error(c, 422, 42630, "error.notification_subscription_filter_invalid")
			return
		}
		query = query.Where("audience = ?", audience)
	}
	if enabled := strings.TrimSpace(c.Query("enabled")); enabled != "" {
		value, err := strconv.ParseBool(enabled)
		if err != nil {
			response.Error(c, 422, 42630, "error.notification_subscription_filter_invalid")
			return
		}
		query = query.Where("enabled = ?", value)
	}
	var total int64
	if query.Count(&total).Error != nil {
		response.Error(c, 500, 50620, "error.notification_subscription_list_failed")
		return
	}
	var records []model.NotificationSubscription
	if query.Order("event_code, channel, created_at").Offset((page-1)*pageSize).Limit(pageSize).Find(&records).Error != nil {
		response.Error(c, 500, 50620, "error.notification_subscription_list_failed")
		return
	}
	templateIDs := make([]uuid.UUID, 0, len(records))
	for _, item := range records {
		templateIDs = append(templateIDs, item.TemplateID)
	}
	var templates []model.NotificationTemplate
	h.DB.Where("id IN ?", templateIDs).Find(&templates)
	names := map[uuid.UUID]string{}
	for _, item := range templates {
		names[item.ID] = item.Name
	}
	items := make([]notificationSubscriptionDTO, 0, len(records))
	for _, item := range records {
		items = append(items, subscriptionDTO(item, names[item.TemplateID]))
	}
	response.Page(c, items, total, page, pageSize)
}

func (h Handler) saveNotificationSubscription(c *gin.Context, id *uuid.UUID) {
	reason, ok := requireAdminChangeReason(c, "保存通知订阅规则")
	if !ok {
		return
	}
	var request notificationSubscriptionRequest
	if decodeStrictJSON(c, &request) != nil {
		response.Error(c, 422, 42631, "error.notification_subscription_fields_invalid")
		return
	}
	template, err := request.normalizeAndValidate(h.DB)
	if err != nil {
		response.Error(c, 422, 42631, "error.notification_subscription_fields_invalid")
		return
	}
	var item model.NotificationSubscription
	err = h.DB.Transaction(func(tx *gorm.DB) error {
		if id == nil {
			item = model.NotificationSubscription{Audience: request.Audience, EventCode: request.EventCode, Channel: request.Channel, Recipient: request.Recipient, TemplateID: *request.TemplateID, Locale: request.Locale, Enabled: *request.Enabled}
			return createWithExplicitColumns(tx, &item, map[string]any{"enabled": *request.Enabled})
		}
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&item, "id = ?", *id).Error; err != nil {
			return err
		}
		return tx.Model(&item).Updates(map[string]any{"audience": request.Audience, "event_code": request.EventCode, "channel": request.Channel, "recipient": request.Recipient, "template_id": *request.TemplateID, "locale": request.Locale, "enabled": *request.Enabled}).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		response.Error(c, 404, 40620, "error.notification_subscription_not_found")
		return
	}
	if err != nil {
		response.Error(c, 409, 40621, "error.notification_subscription_conflict")
		return
	}
	item.Audience, item.EventCode, item.Channel, item.Recipient, item.TemplateID, item.Locale, item.Enabled = request.Audience, request.EventCode, request.Channel, request.Recipient, *request.TemplateID, request.Locale, *request.Enabled
	action := "notification-subscription.create"
	if id != nil {
		action = "notification-subscription.update"
	}
	h.audit(c, action, "notification_subscription", item.ID.String(), reason)
	if id == nil {
		response.Created(c, subscriptionDTO(item, template.Name))
	} else {
		response.OK(c, subscriptionDTO(item, template.Name))
	}
}

func (h Handler) CreateNotificationSubscription(c *gin.Context) {
	h.saveNotificationSubscription(c, nil)
}
func (h Handler) UpdateNotificationSubscription(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, 422, 42632, "error.notification_subscription_id_invalid")
		return
	}
	h.saveNotificationSubscription(c, &id)
}
func (h Handler) DeleteNotificationSubscription(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, 422, 42632, "error.notification_subscription_id_invalid")
		return
	}
	reason, ok := requireAdminChangeReason(c, "删除通知订阅规则")
	if !ok {
		return
	}
	result := h.DB.Unscoped().Delete(&model.NotificationSubscription{}, "id = ?", id)
	if result.Error != nil || result.RowsAffected == 0 {
		response.Error(c, 404, 40620, "error.notification_subscription_not_found")
		return
	}
	h.audit(c, "notification-subscription.delete", "notification_subscription", id.String(), reason)
	response.OK(c, gin.H{"deleted": true})
}

type notificationConnectorRequest struct {
	Name     string `json:"name"`
	Channel  string `json:"channel"`
	Endpoint string `json:"endpoint"`
	Username string `json:"username"`
	Sender   string `json:"sender"`
	Secret   string `json:"secret"`
	Enabled  *bool  `json:"enabled"`
}
type notificationConnectorDTO struct {
	ID                    uuid.UUID `json:"id"`
	Name                  string    `json:"name"`
	Channel               string    `json:"channel"`
	Endpoint              string    `json:"endpoint"`
	Username              string    `json:"username"`
	Sender                string    `json:"sender"`
	CredentialsConfigured bool      `json:"credentials_configured"`
	Enabled               bool      `json:"enabled"`
	UpdatedAt             time.Time `json:"updated_at"`
}

func connectorDTO(item model.NotificationConnector) notificationConnectorDTO {
	return notificationConnectorDTO{ID: item.ID, Name: item.Name, Channel: item.Channel, Endpoint: item.Endpoint, Username: item.Username, Sender: item.Sender, CredentialsConfigured: len(item.SecretCipher) > 0 && len(item.SecretNonce) > 0, Enabled: item.Enabled, UpdatedAt: item.UpdatedAt}
}
func (r *notificationConnectorRequest) normalizeAndValidate(requireSecret bool) error {
	r.Name = strings.TrimSpace(r.Name)
	r.Channel = strings.ToLower(strings.TrimSpace(r.Channel))
	r.Endpoint = strings.TrimSpace(r.Endpoint)
	r.Username = strings.TrimSpace(r.Username)
	r.Sender = strings.TrimSpace(r.Sender)
	r.Secret = strings.TrimSpace(r.Secret)
	if r.Enabled == nil || len([]rune(r.Name)) < 2 || len([]rune(r.Name)) > 120 || (r.Channel != "email" && r.Channel != "telegram" && r.Channel != "wecom") || (requireSecret && len(r.Secret) < 8) || len(r.Secret) > 1000 {
		return errors.New("invalid connector")
	}
	if r.Channel == "email" {
		host, rawPort, endpointErr := net.SplitHostPort(r.Endpoint)
		port, portErr := strconv.Atoi(rawPort)
		if _, err := mail.ParseAddress(r.Sender); err != nil || endpointErr != nil || strings.TrimSpace(host) == "" || portErr != nil || port < 1 || port > 65535 {
			return errors.New("invalid SMTP connector")
		}
	} else if r.Endpoint != "" {
		parsed, err := url.Parse(r.Endpoint)
		if err != nil || parsed.Scheme != "https" || parsed.User != nil || parsed.Hostname() == "" {
			return errors.New("invalid connector endpoint")
		}
	}
	return nil
}
func (h Handler) AdminNotificationConnectors(c *gin.Context) {
	var records []model.NotificationConnector
	if h.DB.Order("channel").Find(&records).Error != nil {
		response.Error(c, 500, 50621, "error.notification_connector_list_failed")
		return
	}
	items := make([]notificationConnectorDTO, 0, len(records))
	for _, item := range records {
		items = append(items, connectorDTO(item))
	}
	response.OK(c, items)
}
func (h Handler) SaveNotificationConnector(c *gin.Context) {
	reason, ok := requireAdminChangeReason(c, "保存通知接收渠道")
	if !ok {
		return
	}
	var request notificationConnectorRequest
	if decodeStrictJSON(c, &request) != nil {
		response.Error(c, 422, 42633, "error.notification_connector_fields_invalid")
		return
	}
	var existing model.NotificationConnector
	found := h.DB.Where("channel = ?", strings.ToLower(strings.TrimSpace(request.Channel))).First(&existing).Error == nil
	if request.normalizeAndValidate(!found || len(existing.SecretCipher) == 0) != nil {
		response.Error(c, 422, 42633, "error.notification_connector_fields_invalid")
		return
	}
	validationContext, cancel := context.WithTimeout(c.Request.Context(), 3*time.Second)
	defer cancel()
	var endpointErr error
	if request.Channel == "email" {
		endpointErr = security.ValidateOutboundAddress(validationContext, request.Endpoint, h.Cfg.Env != "production")
	} else if request.Endpoint != "" {
		_, endpointErr = security.ValidateOutboundURL(validationContext, request.Endpoint, h.Cfg.Env != "production")
	}
	if endpointErr != nil {
		response.Error(c, 422, 42633, "error.notification_connector_fields_invalid")
		return
	}
	if !found {
		existing = model.NotificationConnector{Base: model.Base{ID: uuid.New()}, Channel: request.Channel}
	}
	values := map[string]any{"name": request.Name, "channel": request.Channel, "endpoint": request.Endpoint, "username": request.Username, "sender": request.Sender, "enabled": *request.Enabled}
	if request.Secret != "" {
		cipher, nonce, _, err := h.Vault.Encrypt(request.Secret, existing.ID[:])
		if err != nil {
			response.Error(c, 500, 50622, "error.notification_connector_save_failed")
			return
		}
		values["secret_cipher"], values["secret_nonce"] = cipher, nonce
	}
	err := h.DB.Transaction(func(tx *gorm.DB) error {
		if found {
			return tx.Model(&existing).Updates(values).Error
		}
		existing.Name = request.Name
		existing.Endpoint = request.Endpoint
		existing.Username = request.Username
		existing.Sender = request.Sender
		existing.Enabled = *request.Enabled
		existing.SecretCipher, _ = values["secret_cipher"].([]byte)
		existing.SecretNonce, _ = values["secret_nonce"].([]byte)
		return createWithExplicitColumns(tx, &existing, map[string]any{"enabled": *request.Enabled})
	})
	if err != nil {
		response.Error(c, 409, 40622, "error.notification_connector_save_failed")
		return
	}
	h.DB.First(&existing, "id = ?", existing.ID)
	h.audit(c, "notification-connector.save", "notification_connector", existing.ID.String(), reason)
	response.OK(c, connectorDTO(existing))
}

// createOperationalNotifications materializes every enabled subscription in
// the caller transaction. The existing scheduler then queues the deliveries;
// callers never perform external network I/O inside business transactions.
func (h Handler) createOperationalNotifications(tx *gorm.DB, eventCode, entityID string, values map[string]string) error {
	preferredLocale := supportedNotificationLocale(values["locale"])
	if userID, err := uuid.Parse(strings.TrimSpace(values["user_id"])); err == nil && strings.TrimSpace(values["locale"]) == "" {
		var user struct{ PreferredLocale string }
		if err := tx.Model(&model.User{}).Select("preferred_locale").First(&user, "id = ?", userID).Error; err == nil {
			preferredLocale = supportedNotificationLocale(user.PreferredLocale)
		}
	}
	values["locale"] = preferredLocale
	if _, ok := notificationEvent(eventCode); !ok {
		return nil
	}
	if values == nil {
		values = map[string]string{}
	}
	eventTime := time.Now().UTC()
	if parsed, err := time.Parse(time.RFC3339, values["occurred_at"]); err == nil {
		eventTime = parsed.UTC()
	} else {
		values["occurred_at"] = eventTime.Format(time.RFC3339)
	}
	var subscriptions []model.NotificationSubscription
	if err := tx.Where("event_code = ? AND enabled = ? AND created_at <= ?", eventCode, true, eventTime).Find(&subscriptions).Error; err != nil {
		return err
	}
	if len(subscriptions) == 0 {
		return nil
	}
	for _, variable := range notificationCommonVariables {
		if _, exists := values[variable]; !exists {
			values[variable] = ""
		}
	}
	values["event"] = eventCode
	values["entity_id"] = entityID
	for _, subscription := range subscriptions {
		// A user receives exactly the template matching the language most
		// recently selected in the storefront. Administrator recipients set
		// their language per rule, so admin rules must not inherit a customer's
		// locale or the storefront default.
		if subscription.Audience == "user" && supportedNotificationLocale(subscription.Locale) != preferredLocale {
			continue
		}
		var template model.NotificationTemplate
		if err := tx.Where("id = ? AND enabled = ?", subscription.TemplateID, true).First(&template).Error; err != nil {
			continue
		}
		if template.Audience != subscription.Audience || template.Channel != subscription.Channel || template.Locale != subscription.Locale || (subscription.Audience == "admin" && subscription.Channel == "in_app") {
			continue
		}
		recipient := subscription.Recipient
		if subscription.Audience == "user" {
			if values["user_id"] == "" || (subscription.Channel == "email" && values["email"] == "") {
				continue
			}
			if subscription.Channel == "in_app" {
				userID, err := uuid.Parse(values["user_id"])
				if err != nil {
					continue
				}
				title, body, err := renderNotificationTemplate(template.Subject, template.Body, values)
				if err != nil {
					continue
				}
				cipher, nonce, _, err := h.Vault.Encrypt(body, userID[:])
				if err != nil {
					return err
				}
				notification := model.UserNotification{Base: model.Base{ID: uuid.New()}, UserID: userID, EventCode: eventCode, EntityID: entityID, IdempotencyKey: fmt.Sprintf("event:%s:%s:user:%s", eventCode, entityID, userID), Title: title, BodyCipher: cipher, BodyNonce: nonce}
				if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&notification).Error; err != nil {
					return err
				}
				continue
			}
			recipient = values["email"]
		}
		subject, body, err := renderNotificationTemplate(template.Subject, template.Body, values)
		if err != nil {
			continue
		}
		delivery := model.NotificationDelivery{Base: model.Base{ID: uuid.New()}, IdempotencyKey: fmt.Sprintf("event:%s:%s:%s", eventCode, entityID, subscription.ID), TemplateID: &template.ID, Channel: subscription.Channel, Recipient: recipient, Subject: subject, Status: "queued"}
		cipher, nonce, _, err := h.Vault.Encrypt(body, delivery.ID[:])
		if err != nil {
			return err
		}
		delivery.BodyCipher, delivery.BodyNonce = cipher, nonce
		if err := tx.Create(&delivery).Error; err != nil && !strings.Contains(strings.ToLower(err.Error()), "duplicate") {
			return err
		}
	}
	return nil
}

func sortedNotificationEventCodes() []string {
	result := make([]string, 0, len(notificationEventCatalog))
	for _, item := range notificationEventCatalog {
		result = append(result, item.Code)
	}
	sort.Strings(result)
	return result
}
func marshalNotificationEventValues(values map[string]string) string {
	encoded, _ := json.Marshal(values)
	return string(encoded)
}
