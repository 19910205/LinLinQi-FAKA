package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"linlinqi/api/internal/config"
)

func TestCreateMyWebhookRejectsPrivateDestinationsInDevelopment(t *testing.T) {
	h := Handler{Cfg: config.Config{Env: "development"}}
	for name, target := range map[string]string{
		"loopback IP":       "https://127.0.0.1/internal",
		"localhost":         "https://localhost/internal",
		"cloud metadata IP": "https://169.254.169.254/latest/meta-data",
	} {
		t.Run(name, func(t *testing.T) {
			body, err := json.Marshal(webhookRequest{URL: target, Events: []string{"order.delivered"}})
			if err != nil {
				t.Fatalf("marshal webhook request: %v", err)
			}
			context, recorder := testContext(http.MethodPost, "/api/v1/me/webhooks", string(body))
			context.Set("subject", uuid.NewString())

			h.CreateMyWebhook(context)

			if recorder.Code != http.StatusUnprocessableEntity {
				t.Fatalf("private webhook target %q returned %d: %s", target, recorder.Code, recorder.Body.String())
			}
			var envelope struct {
				Code int `json:"code"`
			}
			if err := json.Unmarshal(recorder.Body.Bytes(), &envelope); err != nil {
				t.Fatalf("decode webhook rejection: %v", err)
			}
			if envelope.Code != 42263 {
				t.Fatalf("private webhook target %q returned application code %d", target, envelope.Code)
			}
		})
	}
}

func TestWebhookDestinationPolicyPreservesPublicHTTPS(t *testing.T) {
	const destination = "https://1.1.1.1/hooks/orders?tenant=merchant"
	normalized, err := normalizeWebhookEndpointURL(context.Background(), destination)
	if err != nil {
		t.Fatalf("public HTTPS webhook was rejected: %v", err)
	}
	if normalized != destination {
		t.Fatalf("public HTTPS webhook changed during normalization: %q", normalized)
	}

	if _, err := normalizeOpenAPICallbackURL(context.Background(), "https://127.0.0.1/callback", "development"); err == nil {
		t.Fatal("OpenAPI callback creation accepted a private destination in development")
	}
	if _, err := normalizeWebhookEndpointURL(context.Background(), "http://1.1.1.1/hooks"); err == nil {
		t.Fatal("public plain-HTTP webhook was accepted")
	}
	if err := validateWebhookEndpointActivation(context.Background(), "https://127.0.0.1/callback", true); err == nil {
		t.Fatal("admin activation accepted a legacy private webhook destination")
	}
	if err := validateWebhookEndpointActivation(context.Background(), "https://127.0.0.1/callback", false); err != nil {
		t.Fatalf("disabling a legacy private webhook was blocked: %v", err)
	}
}
