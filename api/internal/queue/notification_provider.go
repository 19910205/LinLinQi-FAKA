package queue

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/mail"
	"net/smtp"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"linlinqi/api/internal/model"
	"linlinqi/api/internal/security"
)

var operationalPlaceholderPattern = regexp.MustCompile(`{{([a-z][a-z0-9_]*)}}`)

var operationalVariables = []string{"event", "occurred_at", "summary", "entity_id", "status", "amount", "currency", "email", "ip", "stock", "product_name", "order_no", "channel"}

var operationalNotificationLocales = map[string]bool{"zh-CN": true, "zh-TW": true, "en": true, "vi": true, "ru": true, "ja": true, "ko": true, "th": true}

func operationalNotificationLocale(value string) string {
	value = strings.TrimSpace(value)
	if operationalNotificationLocales[value] {
		return value
	}
	return "zh-CN"
}

func renderOperationalTemplate(template model.NotificationTemplate, values map[string]string) (string, string, bool) {
	subject, body := template.Subject, template.Body
	for _, match := range operationalPlaceholderPattern.FindAllStringSubmatch(subject+"\n"+body, -1) {
		value, ok := values[match[1]]
		if !ok {
			return "", "", false
		}
		subject = strings.ReplaceAll(subject, "{{"+match[1]+"}}", value)
		body = strings.ReplaceAll(body, "{{"+match[1]+"}}", value)
	}
	return subject, body, true
}

// createOperationalNotifications materializes one delivery per enabled rule.
// The idempotency key makes event recovery scans safe across worker restarts.
func (w *Worker) createOperationalNotifications(db *gorm.DB, eventCode, entityID string, values map[string]string) {
	if values == nil {
		values = map[string]string{}
	}
	preferredLocale := operationalNotificationLocale(values["locale"])
	if userID, err := uuid.Parse(strings.TrimSpace(values["user_id"])); err == nil && strings.TrimSpace(values["locale"]) == "" {
		var user struct{ PreferredLocale string }
		if db.Model(&model.User{}).Select("preferred_locale").First(&user, "id = ?", userID).Error == nil {
			preferredLocale = operationalNotificationLocale(user.PreferredLocale)
		}
	}
	values["locale"] = preferredLocale
	eventTime := time.Now().UTC()
	if parsed, err := time.Parse(time.RFC3339, values["occurred_at"]); err == nil {
		eventTime = parsed.UTC()
	} else {
		values["occurred_at"] = eventTime.Format(time.RFC3339)
	}
	var subscriptions []model.NotificationSubscription
	if db.Where("event_code = ? AND enabled = ? AND created_at <= ?", eventCode, true, eventTime).Find(&subscriptions).Error != nil || len(subscriptions) == 0 {
		return
	}
	for _, variable := range operationalVariables {
		if _, exists := values[variable]; !exists {
			values[variable] = ""
		}
	}
	values["event"] = eventCode
	values["entity_id"] = entityID
	for _, subscription := range subscriptions {
		if subscription.Audience == "user" && operationalNotificationLocale(subscription.Locale) != preferredLocale {
			continue
		}
		var template model.NotificationTemplate
		if db.Where("id = ? AND enabled = ?", subscription.TemplateID, true).First(&template).Error != nil {
			continue
		}
		if template.Audience != subscription.Audience || template.Channel != subscription.Channel || template.Locale != subscription.Locale {
			continue
		}
		subject, body, valid := renderOperationalTemplate(template, values)
		if !valid {
			continue
		}
		recipient := subscription.Recipient
		if subscription.Audience == "user" {
			userID, err := uuid.Parse(strings.TrimSpace(values["user_id"]))
			if err != nil {
				continue
			}
			if subscription.Channel == "in_app" {
				cipher, nonce, _, err := w.vault.Encrypt(body, userID[:])
				if err != nil {
					continue
				}
				notification := model.UserNotification{Base: model.Base{ID: uuid.New()}, UserID: userID, EventCode: eventCode, EntityID: entityID, IdempotencyKey: fmt.Sprintf("event:%s:%s:user:%s", eventCode, entityID, userID), Title: subject, BodyCipher: cipher, BodyNonce: nonce}
				_ = db.Clauses(clause.OnConflict{DoNothing: true}).Create(&notification).Error
				continue
			}
			if subscription.Channel != "email" || strings.TrimSpace(values["email"]) == "" {
				continue
			}
			recipient = values["email"]
		}
		delivery := model.NotificationDelivery{Base: model.Base{ID: uuid.New()}, IdempotencyKey: fmt.Sprintf("event:%s:%s:%s", eventCode, entityID, subscription.ID), TemplateID: &template.ID, Channel: subscription.Channel, Recipient: recipient, Subject: subject, Status: "queued"}
		cipher, nonce, _, err := w.vault.Encrypt(body, delivery.ID[:])
		if err != nil {
			continue
		}
		delivery.BodyCipher, delivery.BodyNonce = cipher, nonce
		_ = db.Clauses(clause.OnConflict{DoNothing: true}).Create(&delivery).Error
	}
}

func (w *Worker) createOrderDeliveredNotifications(order model.Order) {
	values := map[string]string{"event": "order.delivered", "occurred_at": time.Now().UTC().Format(time.RFC3339), "entity_id": order.ID.String(), "order_no": order.OrderNo, "email": order.Email, "status": order.Status, "amount": fmt.Sprintf("%d", order.Total), "currency": order.Currency, "summary": "订单已完成自动交付"}
	if order.UserID != nil {
		values["user_id"] = order.UserID.String()
	}
	w.createOperationalNotifications(w.db, "order.delivered", order.ID.String(), values)
}

func (w *Worker) createOpenAPINotifications() {
	var logs []model.APICallLog
	if w.db.Where("created_at >= ?", time.Now().Add(-2*time.Minute)).Order("created_at ASC").Limit(500).Find(&logs).Error != nil || len(logs) == 0 {
		return
	}
	for _, log := range logs {
		eventCode := "openapi.call.succeeded"
		if log.StatusCode >= 400 {
			eventCode = "openapi.call.failed"
		}
		values := map[string]string{"occurred_at": log.CreatedAt.UTC().Format(time.RFC3339), "status": fmt.Sprintf("%d", log.StatusCode), "ip": log.IP, "channel": "openapi", "summary": fmt.Sprintf("OpenAPI %s %s 返回 HTTP %d", log.Method, log.Path, log.StatusCode)}
		w.createOperationalNotifications(w.db, eventCode, log.ID.String(), values)
	}
}

// createLifecycleRecoveryNotifications closes gaps left by process crashes or
// asynchronous state changes. All writes use the same durable event key as
// direct hooks, so this scan cannot send the same rule twice.
func (w *Worker) createLifecycleRecoveryNotifications() {
	since := time.Now().UTC().Add(-24 * time.Hour)

	var runs []model.SupplierSyncRun
	if w.db.Where("updated_at >= ? AND status IN ?", since, []string{"succeeded", "failed"}).Order("updated_at DESC").Limit(300).Find(&runs).Error == nil {
		for _, run := range runs {
			eventCode := "supplier.sync.succeeded"
			if run.Status == "failed" {
				eventCode = "supplier.sync.failed"
			}
			w.createOperationalNotifications(w.db, eventCode, run.ID.String(), map[string]string{"occurred_at": run.UpdatedAt.UTC().Format(time.RFC3339), "status": run.Status, "channel": "supplier", "summary": supplierSyncSummary(run)})
		}
	}

	var procurements []model.ProcurementOrder
	if w.db.Where("updated_at >= ? AND status IN ?", since, []string{"completed", "failed"}).Order("updated_at DESC").Limit(300).Find(&procurements).Error == nil {
		for _, procurement := range procurements {
			eventCode, summary := "procurement.succeeded", "上游采购已完成"
			if procurement.Status == "failed" {
				eventCode, summary = "procurement.failed", "上游采购失败并进入人工复核"
			}
			var order model.Order
			_ = w.db.Select("id", "order_no", "email", "user_id").First(&order, "id = ?", procurement.OrderID).Error
			values := map[string]string{"occurred_at": procurement.UpdatedAt.UTC().Format(time.RFC3339), "status": procurement.Status, "amount": fmt.Sprintf("%d", procurement.CostAmount), "currency": procurement.CostCurrency, "email": order.Email, "order_no": order.OrderNo, "channel": "supplier", "summary": summary}
			if order.UserID != nil {
				values["user_id"] = order.UserID.String()
			}
			w.createOperationalNotifications(w.db, eventCode, procurement.ID.String(), values)
		}
	}

	var orders []model.Order
	if w.db.Where("updated_at >= ? AND status IN ?", since, []string{"processing", "failed", "refunded"}).Order("updated_at DESC").Limit(500).Find(&orders).Error == nil {
		for _, order := range orders {
			eventCode, summary := "order.processing", "订单已进入自动交付流程"
			switch order.Status {
			case "failed":
				eventCode, summary = "order.failed", "订单交付失败并进入人工复核"
			case "refunded":
				eventCode, summary = "order.refunded", "订单退款已完成"
			}
			values := map[string]string{"occurred_at": order.UpdatedAt.UTC().Format(time.RFC3339), "status": order.Status, "amount": fmt.Sprintf("%d", order.Total), "currency": order.Currency, "email": order.Email, "order_no": order.OrderNo, "summary": summary}
			if order.UserID != nil {
				values["user_id"] = order.UserID.String()
			}
			w.createOperationalNotifications(w.db, eventCode, order.ID.String(), values)
		}
	}

	var recharges []model.RechargeOrder
	if w.db.Where("updated_at >= ? AND status IN ?", since, []string{"failed", "expired"}).Order("updated_at DESC").Limit(300).Find(&recharges).Error == nil {
		for _, recharge := range recharges {
			values := map[string]string{"occurred_at": recharge.UpdatedAt.UTC().Format(time.RFC3339), "status": recharge.Status, "amount": fmt.Sprintf("%d", recharge.CreditAmount), "currency": recharge.CreditCurrency, "order_no": recharge.RechargeNo, "summary": "充值未完成或已失效", "user_id": recharge.UserID.String()}
			var user model.User
			if w.db.Select("email").First(&user, "id = ?", recharge.UserID).Error == nil {
				values["email"] = user.Email
			}
			w.createOperationalNotifications(w.db, "recharge.failed", recharge.ID.String(), values)
		}
	}

	var securityEvents []model.SecurityEvent
	if w.db.Where("created_at >= ? AND severity IN ?", since, []string{"high", "critical"}).Order("created_at DESC").Limit(500).Find(&securityEvents).Error == nil {
		for _, securityEvent := range securityEvents {
			w.createOperationalNotifications(w.db, "security.high_risk", securityEvent.ID.String(), map[string]string{"occurred_at": securityEvent.CreatedAt.UTC().Format(time.RFC3339), "status": securityEvent.Severity, "ip": securityEvent.IP, "channel": securityEvent.Realm, "summary": "检测到高风险安全事件：" + securityEvent.EventType})
		}
	}
}

func (w *Worker) connectorSecret(connector model.NotificationConnector) (string, error) {
	if len(connector.SecretCipher) == 0 || len(connector.SecretNonce) == 0 {
		return "", errors.New("notification connector credentials are missing")
	}
	plain, err := w.vault.Decrypt(connector.SecretCipher, connector.SecretNonce, connector.ID[:])
	if err != nil {
		return "", errors.New("notification connector credentials cannot be decrypted")
	}
	return string(plain), nil
}

func (w *Worker) notificationConnector(channel string) (model.NotificationConnector, error) {
	var connector model.NotificationConnector
	if err := w.db.Where("channel = ? AND enabled = ?", channel, true).First(&connector).Error; err != nil {
		return connector, errors.New("notification connector is not configured")
	}
	return connector, nil
}

func notificationText(delivery model.NotificationDelivery, body string) string {
	if strings.TrimSpace(delivery.Subject) == "" {
		return body
	}
	return delivery.Subject + "\n\n" + body
}

func (w *Worker) sendTelegram(ctx context.Context, connector model.NotificationConnector, delivery model.NotificationDelivery, body string) error {
	secret, err := w.connectorSecret(connector)
	if err != nil {
		return err
	}
	base := strings.TrimRight(connector.Endpoint, "/")
	if base == "" {
		base = "https://api.telegram.org"
	}
	endpoint := base + "/bot" + url.PathEscape(secret) + "/sendMessage"
	payload, _ := json.Marshal(map[string]any{"chat_id": delivery.Recipient, "text": notificationText(delivery, body), "disable_web_page_preview": true})
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return errors.New("invalid Telegram request")
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := w.http.Do(request)
	if err != nil {
		// net/http URL errors include the request URL. The Telegram bot token is
		// embedded in that URL, so never persist or log the raw transport error.
		return errors.New("Telegram transport request failed")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("telegram returned HTTP %d", response.StatusCode)
	}
	return nil
}

func (w *Worker) sendWeCom(ctx context.Context, connector model.NotificationConnector, delivery model.NotificationDelivery, body string) error {
	secret, err := w.connectorSecret(connector)
	if err != nil {
		return err
	}
	endpoint := connector.Endpoint
	if endpoint == "" {
		endpoint = "https://qyapi.weixin.qq.com/cgi-bin/webhook/send"
	}
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return err
	}
	query := parsed.Query()
	query.Set("key", secret)
	parsed.RawQuery = query.Encode()
	payload, _ := json.Marshal(map[string]any{"msgtype": "text", "text": map[string]any{"content": notificationText(delivery, body)}})
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, parsed.String(), bytes.NewReader(payload))
	if err != nil {
		return errors.New("invalid WeCom request")
	}
	request.Header.Set("Content-Type", "application/json")
	response, err := w.http.Do(request)
	if err != nil {
		// The webhook key is carried in the query string and url.Error would
		// otherwise copy it into the durable delivery failure and logs.
		return errors.New("WeCom transport request failed")
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("wecom returned HTTP %d", response.StatusCode)
	}
	return nil
}

func (w *Worker) sendEmail(ctx context.Context, connector model.NotificationConnector, delivery model.NotificationDelivery, body string) error {
	password, err := w.connectorSecret(connector)
	if err != nil {
		return err
	}
	host, _, err := net.SplitHostPort(connector.Endpoint)
	if err != nil {
		return errors.New("invalid SMTP endpoint")
	}
	sender, err := mail.ParseAddress(connector.Sender)
	if err != nil {
		return errors.New("invalid SMTP sender")
	}
	recipient, err := mail.ParseAddress(delivery.Recipient)
	if err != nil {
		return errors.New("invalid SMTP recipient")
	}
	if strings.IndexFunc(delivery.Subject, func(character rune) bool { return character < 0x20 || character == 0x7f }) >= 0 {
		return errors.New("invalid SMTP subject")
	}
	raw, err := security.DialOutboundContext(ctx, "tcp", connector.Endpoint, w.cfg.Env != "production")
	if err != nil {
		return errors.New("SMTP transport connection failed")
	}
	tlsConnection := tls.Client(raw, &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12})
	if err := tlsConnection.HandshakeContext(ctx); err != nil {
		return errors.Join(err, raw.Close())
	}
	client, err := smtp.NewClient(tlsConnection, host)
	if err != nil {
		return errors.Join(err, tlsConnection.Close())
	}
	defer client.Close()
	if connector.Username != "" {
		if err := client.Auth(smtp.PlainAuth("", connector.Username, password, host)); err != nil {
			return err
		}
	}
	if err := client.Mail(sender.Address); err != nil {
		return err
	}
	if err := client.Rcpt(recipient.Address); err != nil {
		return err
	}
	wc, err := client.Data()
	if err != nil {
		return err
	}
	message := "From: " + sender.String() + "\r\nTo: " + recipient.String() + "\r\nSubject: " + delivery.Subject + "\r\nMIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n" + body
	if _, err := wc.Write([]byte(message)); err != nil {
		return errors.Join(err, wc.Close())
	}
	if err := wc.Close(); err != nil {
		return err
	}
	return client.Quit()
}

func (w *Worker) sendViaConnector(ctx context.Context, delivery model.NotificationDelivery, body string) (bool, error) {
	if delivery.Channel == "admin" {
		return true, nil
	}
	connector, err := w.notificationConnector(delivery.Channel)
	if err != nil {
		return false, nil
	}
	switch delivery.Channel {
	case "telegram":
		err = w.sendTelegram(ctx, connector, delivery, body)
	case "wecom":
		err = w.sendWeCom(ctx, connector, delivery, body)
	case "email":
		err = w.sendEmail(ctx, connector, delivery, body)
	default:
		return false, nil
	}
	return true, err
}
