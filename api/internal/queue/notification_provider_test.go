package queue

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/google/uuid"

	"linlinqi/api/internal/config"
	"linlinqi/api/internal/model"
	"linlinqi/api/internal/security"
)

type notificationRoundTripFunc func(*http.Request) (*http.Response, error)

func (function notificationRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

func encryptedNotificationConnector(t *testing.T, channel, endpoint, secret string) (*security.Vault, model.NotificationConnector) {
	t.Helper()
	vault, err := security.NewVault("notification-provider-unit-test-encryption-secret")
	if err != nil {
		t.Fatalf("create vault: %v", err)
	}
	connector := model.NotificationConnector{Base: model.Base{ID: uuid.New()}, Channel: channel, Endpoint: endpoint}
	connector.SecretCipher, connector.SecretNonce, _, err = vault.Encrypt(secret, connector.ID[:])
	if err != nil {
		t.Fatalf("encrypt connector secret: %v", err)
	}
	return vault, connector
}

func TestNotificationHTTPConnectorErrorsNeverExposeURLSecrets(t *testing.T) {
	for _, test := range []struct {
		name     string
		channel  string
		endpoint string
		secret   string
		send     func(*Worker, model.NotificationConnector) error
	}{
		{
			name: "telegram", channel: "telegram", endpoint: "https://notify.example.com", secret: "telegram-bot-secret-value",
			send: func(worker *Worker, connector model.NotificationConnector) error {
				return worker.sendTelegram(context.Background(), connector, model.NotificationDelivery{Recipient: "12345", Subject: "test"}, "body")
			},
		},
		{
			name: "wecom", channel: "wecom", endpoint: "https://notify.example.com/webhook", secret: "wecom-webhook-secret-value",
			send: func(worker *Worker, connector model.NotificationConnector) error {
				return worker.sendWeCom(context.Background(), connector, model.NotificationDelivery{Recipient: "ops", Subject: "test"}, "body")
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			vault, connector := encryptedNotificationConnector(t, test.channel, test.endpoint, test.secret)
			client := &http.Client{Transport: notificationRoundTripFunc(func(request *http.Request) (*http.Response, error) {
				if !strings.Contains(request.URL.String(), test.secret) {
					t.Errorf("connector secret was not placed in provider request")
				}
				return nil, errors.New("simulated failure for " + request.URL.String())
			})}
			worker := &Worker{vault: vault, http: client}
			err := test.send(worker, connector)
			if err == nil {
				t.Fatal("expected transport failure")
			}
			if strings.Contains(err.Error(), test.secret) || strings.Contains(err.Error(), test.endpoint) {
				t.Fatalf("connector failure exposed a secret-bearing URL: %v", err)
			}
		})
	}
}

func TestSMTPConnectorRejectsHeaderInjectionAndProductionLoopback(t *testing.T) {
	vault, connector := encryptedNotificationConnector(t, "email", "127.0.0.1:465", "smtp-password-value")
	connector.Sender = "Sender <sender@example.com>"
	worker := &Worker{vault: vault, cfg: config.Config{Env: "production"}}

	err := worker.sendEmail(context.Background(), connector, model.NotificationDelivery{Recipient: "recipient@example.com", Subject: "invoice\r\nBcc: attacker@example.com"}, "body")
	if err == nil || !strings.Contains(err.Error(), "subject") {
		t.Fatalf("SMTP header injection was not rejected: %v", err)
	}
	err = worker.sendEmail(context.Background(), connector, model.NotificationDelivery{Recipient: "recipient@example.com", Subject: "invoice"}, "body")
	if err == nil || !strings.Contains(err.Error(), "transport") {
		t.Fatalf("production SMTP loopback was not rejected: %v", err)
	}
}
