package handler

import (
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/hibiken/asynq"

	"linlinqi/api/internal/i18n"
	"linlinqi/api/internal/model"
)

func boolPointer(value bool) *bool { return &value }

func validAdminNotificationTemplateRequest() adminNotificationTemplateRequest {
	return adminNotificationTemplateRequest{
		Code: " Order.Paid ", Name: " 订单支付通知 ", Channel: " EMAIL ", Locale: "zh-cn",
		Subject: "订单 {{order_no}} 已支付", Body: "您的订单 {{order_no}} 已支付 {{amount}} 元。",
		Variables: []string{" amount ", "ORDER_NO"}, Enabled: boolPointer(true),
	}
}

func TestAdminNotificationTemplateValidationNormalizesStrictFields(t *testing.T) {
	request := validAdminNotificationTemplateRequest()
	if err := request.normalizeAndValidate(); err != nil {
		t.Fatalf("valid template rejected: %v", err)
	}
	if request.Code != "order.paid" || request.Name != "订单支付通知" || request.Channel != "email" || request.Locale != "zh-CN" {
		t.Fatalf("template identity was not normalized: %#v", request)
	}
	if strings.Join(request.Variables, ",") != "amount,order_no" {
		t.Fatalf("variables were not normalized and sorted: %#v", request.Variables)
	}
}

func TestAdminNotificationTemplateRejectsUnsafeOrInconsistentInput(t *testing.T) {
	for name, mutate := range map[string]func(*adminNotificationTemplateRequest){
		"unknown channel": func(request *adminNotificationTemplateRequest) { request.Channel = "sms" },
		"bad locale":      func(request *adminNotificationTemplateRequest) { request.Locale = "../../zh" },
		"server state":    func(request *adminNotificationTemplateRequest) { request.Enabled = nil },
		"bad code":        func(request *adminNotificationTemplateRequest) { request.Code = "../order" },
		"control subject": func(request *adminNotificationTemplateRequest) { request.Subject = "paid\x00" },
		"missing variable": func(request *adminNotificationTemplateRequest) {
			request.Variables = []string{"order_no"}
		},
		"unknown variable": func(request *adminNotificationTemplateRequest) {
			request.Variables = append(request.Variables, "customer")
		},
		"duplicate variable": func(request *adminNotificationTemplateRequest) {
			request.Variables = append(request.Variables, "amount")
		},
		"malformed placeholder": func(request *adminNotificationTemplateRequest) {
			request.Body = "订单 {{order-no}}"
		},
	} {
		t.Run(name, func(t *testing.T) {
			request := validAdminNotificationTemplateRequest()
			mutate(&request)
			if err := request.normalizeAndValidate(); err == nil {
				t.Fatal("unsafe or inconsistent notification template was accepted")
			}
		})
	}
}

func TestAdminNotificationTemplateDTORejectsUnknownServerOwnedFields(t *testing.T) {
	gin.SetMode(gin.TestMode)
	context, _ := gin.CreateTestContext(httptest.NewRecorder())
	context.Request = httptest.NewRequest("POST", "/admin/v1/notifications/templates", strings.NewReader(`{
		"code":"order.paid","name":"支付通知","channel":"email","locale":"zh-CN",
		"subject":"{{order_no}}","body":"{{order_no}}","variables":["order_no"],"enabled":true,"version":99
	}`))
	context.Request.Header.Set("Content-Type", "application/json")
	var request adminNotificationTemplateRequest
	if err := decodeStrictJSON(context, &request); err == nil {
		t.Fatal("server-owned template version was accepted")
	}
}

func TestAdminIntegrationDTOsHideSecretsPayloadAndRecipients(t *testing.T) {
	endpoint := model.WebhookEndpoint{
		URL:          "https://hooks.example.com/private/token?api_key=top-secret",
		Events:       `["order.paid"]`,
		SecretCipher: []byte("cipher-secret"), SecretNonce: []byte("secret-nonce"),
	}
	encoded, err := json.Marshal(toAdminWebhookEndpoint(endpoint, i18n.LocaleZH))
	if err != nil {
		t.Fatalf("marshal endpoint DTO: %v", err)
	}
	text := string(encoded)
	for _, forbidden := range []string{"private/token", "api_key", "top-secret", "cipher-secret", "secret-nonce", "secret_cipher", "secret_nonce"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("endpoint DTO exposed %q: %s", forbidden, text)
		}
	}
	if !strings.Contains(text, `"url":"https://hooks.example.com/…"`) {
		t.Fatalf("endpoint DTO did not retain a safe destination: %s", text)
	}

	webhookDelivery := adminWebhookDeliveryItem{ID: uuid.New(), Diagnostic: safeWebhookDiagnostic(500, "raw response token=top-secret", i18n.LocaleZH), StoredResponse: `{"card_secret":"raw-card"}`}
	notificationDelivery := adminNotificationDeliveryItem{ID: uuid.New(), Channel: "email", Recipient: maskIntegrationRecipient("email", "customer@example.com", i18n.LocaleZH), StoredRecipient: "customer@example.com", StoredError: "provider-secret"}
	encoded, err = json.Marshal(struct {
		Webhook      adminWebhookDeliveryItem      `json:"webhook"`
		Notification adminNotificationDeliveryItem `json:"notification"`
	}{webhookDelivery, notificationDelivery})
	if err != nil {
		t.Fatalf("marshal delivery DTOs: %v", err)
	}
	text = string(encoded)
	for _, forbidden := range []string{"raw response", "top-secret", "raw-card", "customer@example.com", "provider-secret"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("delivery DTO exposed %q: %s", forbidden, text)
		}
	}
}

func TestAdminNotificationTestRenderingAndRecipientValidation(t *testing.T) {
	variables := []string{"amount", "order_no"}
	values := map[string]string{"amount": "19.90", "order_no": "LQ-1001"}
	if err := validateNotificationTestVariables(variables, values); err != nil {
		t.Fatalf("valid test variables rejected: %v", err)
	}
	subject, body, err := renderNotificationTemplate("订单 {{order_no}}", "实付 {{amount}}", values)
	if err != nil || subject != "订单 LQ-1001" || body != "实付 19.90" {
		t.Fatalf("template render failed: %q %q %v", subject, body, err)
	}
	if _, _, err := renderNotificationTemplate("{{missing}}", "body", values); err == nil {
		t.Fatal("unresolved placeholder was accepted")
	}
	for channel, recipient := range map[string]string{
		"email": "ops@example.com", "telegram": "chat_1024", "wecom": "user-1024", "admin": "admin",
	} {
		if err := validateNotificationRecipient(channel, recipient); err != nil {
			t.Fatalf("valid %s recipient rejected: %v", channel, err)
		}
	}
	if err := validateNotificationRecipient("email", "Ops <ops@example.com>"); err == nil {
		t.Fatal("display-name email was accepted instead of an exact address")
	}
}

func TestAdminIntegrationQueueStateIsIdempotent(t *testing.T) {
	if integrationQueueState(nil) != "queued" || integrationQueueState(asynq.ErrDuplicateTask) != "already_queued" || integrationQueueState(asynq.ErrTaskIDConflict) != "already_queued" {
		t.Fatal("queue duplicate state was not classified idempotently")
	}
	if integrationQueueState(errors.New("redis unavailable")) != "scheduler_pending" {
		t.Fatal("transient enqueue failure must remain recoverable by the scheduler")
	}
}
