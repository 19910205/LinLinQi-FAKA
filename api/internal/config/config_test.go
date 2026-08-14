package config

import "testing"

func TestConfigRejectsUnknownEnvironmentInsteadOfUsingDevelopmentPolicy(t *testing.T) {
	for _, environment := range []string{"prod", "staging", "Production", ""} {
		cfg := Config{Env: environment, WorkerConcurrency: 12}
		if err := cfg.Validate(); err == nil {
			t.Errorf("unknown APP_ENV %q was accepted", environment)
		}
	}
}

func TestProductionConfigRejectsPlaceholders(t *testing.T) {
	cfg := Config{Env: "production", WorkerConcurrency: 12, JWTSecret: "replace-user-jwt-secret", AdminJWTSecret: "another-secret", DataEncryptionKey: "data-secret", OpenAPISecret: "api-secret"}
	if err := cfg.Validate(); err == nil {
		t.Fatal("expected placeholder configuration to be rejected")
	}
}

func TestOAuthProvidersRejectUnknownFieldsAndNormalizeScopes(t *testing.T) {
	cfg := Config{Env: "development", OAuthProvidersJSON: `{"corp":{"name":"企业登录","issuer":"http://127.0.0.1:9999/issuer/","client_id":"client","client_secret":"secret-123","scopes":["profile","email"]}}`}
	providers, err := cfg.OAuthProviders()
	if err != nil {
		t.Fatalf("unexpected provider error: %v", err)
	}
	provider := providers["corp"]
	if provider.Issuer != "http://127.0.0.1:9999/issuer" || len(provider.Scopes) != 3 || provider.Scopes[0] != "openid" || provider.Scopes[1] != "email" {
		t.Fatalf("provider was not normalized safely: %#v", provider)
	}
	cfg.OAuthProvidersJSON = `{"corp":{"name":"企业登录","issuer":"https://id.example.com","client_id":"client","client_secret":"secret-123","unexpected":true}}`
	if _, err := cfg.OAuthProviders(); err == nil {
		t.Fatal("expected unknown OIDC fields to be rejected")
	}
}

func TestProductionConfigAcceptsIndependentSecrets(t *testing.T) {
	cfg := Config{
		Env: "production", WorkerConcurrency: 12,
		AppURL:            "https://api.example.com",
		UserAppURL:        "https://store.example.com",
		DatabaseURL:       "postgres://linlinqi:a-long-database-password@postgres:5432/linlinqi",
		RedisPassword:     "redis-0123456789-abcdefghijklmnop",
		JWTSecret:         "user-0123456789-abcdefghijklmnopqrstuvwxyz",
		AdminJWTSecret:    "admin-0123456789-abcdefghijklmnopqrstuvwxyz",
		DataEncryptionKey: "data-0123456789-abcdefghijklmnopqrstuvwxyz",
		OpenAPISecret:     "openapi-0123456789-abcdefghijklmnopqrstuvwxyz",
		OpenAPIKey:        "linlinqi_live_0123456789",
		MetricsToken:      "metrics-0123456789-abcdefghijklmnopqrstuvwxyz",
		SupportEmail:      "support@example.com",
		CORSOrigins:       []string{"https://store.example.com", "https://admin.example.com"},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}
}

func TestRequirePublicHTTPSRejectsPrivateAndInternalTargets(t *testing.T) {
	for _, target := range []string{
		"https://127.0.0.1",
		"https://10.0.0.8",
		"https://100.64.0.8",
		"https://198.18.0.8",
		"https://[::1]",
		"https://identity.internal",
		"https://console.home.arpa",
		"https://service.test",
	} {
		if err := requirePublicHTTPS("TEST_URL", target); err == nil {
			t.Fatalf("private production URL accepted: %s", target)
		}
	}
	if err := requirePublicHTTPS("TEST_URL", "https://identity.example.com"); err != nil {
		t.Fatalf("public HTTPS URL rejected: %v", err)
	}
}

func TestTrustedProxiesRejectsSpoofableGlobalTrust(t *testing.T) {
	for _, proxies := range [][]string{{"0.0.0.0/0"}, {"::/0"}, {"not-a-network"}, {"0.0.0.0"}} {
		if err := validateTrustedProxies(proxies); err == nil {
			t.Errorf("unsafe trusted proxy configuration accepted: %#v", proxies)
		}
	}
	if err := validateTrustedProxies([]string{"127.0.0.1", "10.0.0.0/8", "203.0.113.0/24"}); err != nil {
		t.Fatalf("scoped trusted proxies were rejected: %v", err)
	}
}
