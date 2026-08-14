package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/netip"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
)

type OAuthProviderConfig struct {
	Name         string   `json:"name"`
	Issuer       string   `json:"issuer"`
	ClientID     string   `json:"client_id"`
	ClientSecret string   `json:"client_secret"`
	Scopes       []string `json:"scopes"`
}

type Config struct {
	Env                     string
	BindAddress             string
	Port                    string
	AppURL                  string
	SupplierCallbackURL     string
	DatabaseURL             string
	RedisAddr               string
	RedisPassword           string
	RedisDB                 int
	JWTSecret               string
	AdminJWTSecret          string
	DataEncryptionKey       string
	CORSOrigins             []string
	BootstrapAdmin          bool
	SeedData                bool
	OpenAPIKey              string
	OpenAPISecret           string
	WorkerConcurrency       int
	BootstrapAdminPassword  string
	NotificationRelayURL    string
	NotificationRelaySecret string
	UserAppURL              string
	SupportEmail            string
	TrustedProxies          []string
	MetricsToken            string
	OAuthProvidersJSON      string
	StorageRoot             string
	MediaPublicBaseURL      string
	MediaMaxImageBytes      int64
	MediaStorageMaxBytes    int64
	MediaMinFreeBytes       int64
}

func Load() Config {
	runtimeEnvironment := strings.ToLower(strings.TrimSpace(env("APP_ENV", "development")))
	developmentDefaults := runtimeEnvironment != "production"
	cfg := Config{
		Env:                     runtimeEnvironment,
		BindAddress:             env("APP_BIND_ADDRESS", "127.0.0.1"),
		Port:                    env("APP_PORT", "8080"),
		AppURL:                  env("APP_URL", "http://localhost:8080"),
		SupplierCallbackURL:     env("SUPPLIER_CALLBACK_URL", ""),
		DatabaseURL:             env("DATABASE_URL", "postgres://linlinqi:linlinqi@localhost:5432/linlinqi?sslmode=disable"),
		RedisAddr:               env("REDIS_ADDR", "localhost:6379"),
		RedisPassword:           env("REDIS_PASSWORD", ""),
		RedisDB:                 envInt("REDIS_DB", 0),
		JWTSecret:               env("JWT_SECRET", "dev-user-secret-change-me"),
		AdminJWTSecret:          env("ADMIN_JWT_SECRET", "dev-admin-secret-change-me"),
		DataEncryptionKey:       env("DATA_ENCRYPTION_KEY", "dev-data-encryption-key-change-me-immediately"),
		CORSOrigins:             splitNonEmpty(env("CORS_ORIGINS", "http://localhost:5173,http://localhost:5174")),
		BootstrapAdmin:          envBool("BOOTSTRAP_ADMIN", developmentDefaults),
		SeedData:                envBool("SEED_DATA", developmentDefaults),
		OpenAPIKey:              env("OPENAPI_KEY", "linlinqi_demo_key"),
		OpenAPISecret:           env("OPENAPI_SECRET", "linlinqi_demo_secret_change_me"),
		WorkerConcurrency:       envInt("WORKER_CONCURRENCY", 12),
		BootstrapAdminPassword:  env("BOOTSTRAP_ADMIN_PASSWORD", ""),
		NotificationRelayURL:    env("NOTIFICATION_RELAY_URL", ""),
		NotificationRelaySecret: env("NOTIFICATION_RELAY_SECRET", ""),
		UserAppURL:              env("USER_APP_URL", "http://localhost:5173"),
		SupportEmail:            env("SUPPORT_EMAIL", "support@linlinqi.local"),
		TrustedProxies:          splitNonEmpty(env("TRUSTED_PROXIES", "")),
		MetricsToken:            env("METRICS_TOKEN", "dev-metrics-token-change-me-123456"),
		OAuthProvidersJSON:      env("OAUTH_PROVIDERS_JSON", "{}"),
		StorageRoot:             env("STORAGE_ROOT", "./var/storage"),
		MediaPublicBaseURL:      strings.TrimRight(env("MEDIA_PUBLIC_BASE_URL", ""), "/"),
		MediaMaxImageBytes:      envInt64("MEDIA_MAX_IMAGE_BYTES", 20<<20),
		MediaStorageMaxBytes:    envInt64("MEDIA_STORAGE_MAX_BYTES", 200<<30),
		MediaMinFreeBytes:       envInt64("MEDIA_MIN_FREE_BYTES", 100<<30),
	}
	if cfg.MediaPublicBaseURL == "" {
		cfg.MediaPublicBaseURL = strings.TrimRight(cfg.AppURL, "/") + "/media"
	}
	if cfg.Env == "production" && strings.TrimSpace(cfg.SupplierCallbackURL) == "" {
		cfg.SupplierCallbackURL = cfg.AppURL
	}
	if cfg.Env != "production" && cfg.BootstrapAdminPassword == "" {
		cfg.BootstrapAdminPassword = "LinLinQi@2026"
	}
	return cfg
}

func (c Config) Validate() error {
	switch c.Env {
	case "development", "test", "production":
	default:
		return fmt.Errorf("APP_ENV must be development, test, or production")
	}
	if c.BindAddress != "" && net.ParseIP(c.BindAddress) == nil {
		return fmt.Errorf("APP_BIND_ADDRESS must be an IP address")
	}
	if err := validateTrustedProxies(c.TrustedProxies); err != nil {
		return err
	}
	if c.Port != "" {
		port, err := strconv.Atoi(c.Port)
		if err != nil || port < 1 || port > 65535 {
			return fmt.Errorf("APP_PORT must be between 1 and 65535")
		}
	}
	if c.WorkerConcurrency < 1 || c.WorkerConcurrency > 256 {
		return fmt.Errorf("WORKER_CONCURRENCY must be between 1 and 256")
	}
	mediaMaxImageBytes := c.MediaMaxImageBytes
	if mediaMaxImageBytes == 0 {
		mediaMaxImageBytes = 20 << 20
	}
	mediaStorageMaxBytes := c.MediaStorageMaxBytes
	if mediaStorageMaxBytes == 0 {
		mediaStorageMaxBytes = 200 << 30
	}
	mediaMinFreeBytes := c.MediaMinFreeBytes
	if mediaMinFreeBytes == 0 {
		mediaMinFreeBytes = 100 << 30
	}
	if mediaMaxImageBytes < 1<<20 || mediaMaxImageBytes > 100<<20 || mediaStorageMaxBytes < mediaMaxImageBytes || mediaMinFreeBytes < 1<<30 {
		return fmt.Errorf("media storage limits are invalid")
	}
	mediaPublicBaseURL := strings.TrimRight(strings.TrimSpace(c.MediaPublicBaseURL), "/")
	if mediaPublicBaseURL == "" {
		mediaPublicBaseURL = strings.TrimRight(c.AppURL, "/") + "/media"
	}
	mediaURL, err := url.Parse(mediaPublicBaseURL)
	if err != nil || mediaURL.Hostname() == "" || mediaURL.User != nil || mediaURL.RawQuery != "" || mediaURL.Fragment != "" || (mediaURL.Scheme != "https" && !(c.Env != "production" && mediaURL.Scheme == "http")) {
		return fmt.Errorf("MEDIA_PUBLIC_BASE_URL must be a valid public base URL")
	}
	if _, err := c.OAuthProviders(); err != nil {
		return err
	}
	if c.Env != "production" {
		return nil
	}
	if err := requirePublicHTTPS("MEDIA_PUBLIC_BASE_URL", mediaPublicBaseURL); err != nil {
		return err
	}
	if c.SeedData {
		return fmt.Errorf("SEED_DATA is development-only and must be false in production")
	}
	secrets := map[string]string{
		"JWT_SECRET":          c.JWTSecret,
		"ADMIN_JWT_SECRET":    c.AdminJWTSecret,
		"DATA_ENCRYPTION_KEY": c.DataEncryptionKey,
		"OPENAPI_SECRET":      c.OpenAPISecret,
		"METRICS_TOKEN":       c.MetricsToken,
	}
	for name, value := range secrets {
		lower := strings.ToLower(value)
		if len(value) < 32 || strings.Contains(lower, "replace") || strings.Contains(lower, "change-me") || strings.Contains(lower, "demo") || strings.Contains(lower, "dev-") {
			return fmt.Errorf("%s must be a non-placeholder secret of at least 32 characters", name)
		}
	}
	if c.JWTSecret == c.AdminJWTSecret {
		return fmt.Errorf("JWT_SECRET and ADMIN_JWT_SECRET must be different")
	}
	if len(c.RedisPassword) < 24 || isPlaceholder(c.RedisPassword) {
		return fmt.Errorf("REDIS_PASSWORD must be a non-placeholder secret of at least 24 characters")
	}
	if len(c.OpenAPIKey) < 16 || isPlaceholder(c.OpenAPIKey) {
		return fmt.Errorf("OPENAPI_KEY must be a non-placeholder identifier of at least 16 characters")
	}
	if err := requirePublicHTTPS("APP_URL", c.AppURL); err != nil {
		return err
	}
	supplierCallbackURL := strings.TrimSpace(c.SupplierCallbackURL)
	if supplierCallbackURL == "" {
		supplierCallbackURL = c.AppURL
	}
	if err := requirePublicHTTPS("SUPPLIER_CALLBACK_URL", supplierCallbackURL); err != nil {
		return err
	}
	if err := requirePublicHTTPS("USER_APP_URL", c.UserAppURL); err != nil {
		return err
	}
	databaseURL, err := url.Parse(c.DatabaseURL)
	if err != nil || (databaseURL.Scheme != "postgres" && databaseURL.Scheme != "postgresql") || databaseURL.Hostname() == "" || databaseURL.User == nil {
		return fmt.Errorf("DATABASE_URL must be a valid PostgreSQL URL")
	}
	databasePassword, hasPassword := databaseURL.User.Password()
	if !hasPassword || len(databasePassword) < 16 || isPlaceholder(databasePassword) {
		return fmt.Errorf("DATABASE_URL must contain a non-placeholder database password of at least 16 characters")
	}
	for _, origin := range c.CORSOrigins {
		if strings.TrimSpace(origin) == "*" {
			return fmt.Errorf("wildcard CORS origin is forbidden in production")
		}
		if err := requirePublicHTTPS("CORS_ORIGINS", origin); err != nil {
			return err
		}
	}
	if len(c.CORSOrigins) == 0 {
		return fmt.Errorf("CORS_ORIGINS must contain at least one HTTPS origin")
	}
	if c.BootstrapAdmin && len(c.BootstrapAdminPassword) < 14 {
		return fmt.Errorf("BOOTSTRAP_ADMIN_PASSWORD must contain at least 14 characters when BOOTSTRAP_ADMIN is enabled")
	}
	if c.NotificationRelayURL != "" && len(c.NotificationRelaySecret) < 32 {
		return fmt.Errorf("NOTIFICATION_RELAY_SECRET must contain at least 32 characters when a relay is configured")
	}
	if c.NotificationRelayURL != "" {
		if err := requirePublicHTTPS("NOTIFICATION_RELAY_URL", c.NotificationRelayURL); err != nil {
			return err
		}
	}
	if !strings.Contains(c.SupportEmail, "@") || strings.HasSuffix(strings.ToLower(c.SupportEmail), ".local") {
		return fmt.Errorf("SUPPORT_EMAIL must be a public support mailbox in production")
	}
	return nil
}

var oauthProviderCodePattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{1,39}$`)

// OAuthProviders decodes the provider-neutral OIDC configuration. Secrets are
// retained only in process memory and are never exposed by the public provider
// discovery endpoint.
func (c Config) OAuthProviders() (map[string]OAuthProviderConfig, error) {
	raw := strings.TrimSpace(c.OAuthProvidersJSON)
	if raw == "" {
		raw = "{}"
	}
	decoder := json.NewDecoder(bytes.NewBufferString(raw))
	decoder.DisallowUnknownFields()
	providers := map[string]OAuthProviderConfig{}
	if err := decoder.Decode(&providers); err != nil {
		return nil, fmt.Errorf("OAUTH_PROVIDERS_JSON must be a valid provider map: %w", err)
	}
	var trailing interface{}
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, fmt.Errorf("OAUTH_PROVIDERS_JSON must contain one JSON object")
	}
	if len(providers) > 10 {
		return nil, fmt.Errorf("OAUTH_PROVIDERS_JSON must contain at most 10 providers")
	}
	for code, provider := range providers {
		if strings.TrimSpace(code) != code {
			return nil, fmt.Errorf("OIDC provider codes must not contain surrounding whitespace")
		}
		provider.Name = strings.TrimSpace(provider.Name)
		provider.Issuer = strings.TrimRight(strings.TrimSpace(provider.Issuer), "/")
		provider.ClientID = strings.TrimSpace(provider.ClientID)
		if !oauthProviderCodePattern.MatchString(code) || len([]rune(provider.Name)) < 1 || len([]rune(provider.Name)) > 80 || provider.ClientID == "" || len(provider.ClientID) > 300 {
			return nil, fmt.Errorf("OIDC provider %q has an invalid code, name or client ID", code)
		}
		parsed, err := url.Parse(provider.Issuer)
		if err != nil || parsed.Hostname() == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Scheme != "https" && !(c.Env != "production" && parsed.Scheme == "http")) {
			return nil, fmt.Errorf("OIDC provider %q issuer must be an HTTPS URL", code)
		}
		if c.Env == "production" {
			if err := requirePublicHTTPS("OIDC provider "+code+" issuer", provider.Issuer); err != nil {
				return nil, err
			}
			if len(provider.ClientSecret) < 24 || isPlaceholder(provider.ClientSecret) {
				return nil, fmt.Errorf("OIDC provider %q client secret must contain at least 24 non-placeholder characters", code)
			}
		} else if len(provider.ClientSecret) < 8 {
			return nil, fmt.Errorf("OIDC provider %q client secret must contain at least 8 characters", code)
		}
		scopes := make([]string, 0, len(provider.Scopes)+2)
		seen := map[string]bool{}
		for _, required := range []string{"openid", "email"} {
			scopes = append(scopes, required)
			seen[required] = true
		}
		for _, scope := range provider.Scopes {
			scope = strings.TrimSpace(scope)
			if scope == "" || len(scope) > 80 || strings.ContainsAny(scope, "\r\n\t ") {
				return nil, fmt.Errorf("OIDC provider %q contains an invalid scope", code)
			}
			if !seen[scope] {
				scopes = append(scopes, scope)
				seen[scope] = true
			}
		}
		if len(scopes) > 12 {
			return nil, fmt.Errorf("OIDC provider %q contains too many scopes", code)
		}
		provider.Scopes = scopes
		providers[code] = provider
	}
	return providers, nil
}

func isPlaceholder(value string) bool {
	lower := strings.ToLower(value)
	return strings.Contains(lower, "replace") || strings.Contains(lower, "change-me") || strings.Contains(lower, "demo") || strings.Contains(lower, "dev-")
}

func requirePublicHTTPS(name, value string) error {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("%s must be a public HTTPS URL", name)
	}
	host := strings.ToLower(strings.TrimSuffix(parsed.Hostname(), "."))
	if host == "localhost" ||
		strings.HasSuffix(host, ".localhost") ||
		strings.HasSuffix(host, ".local") ||
		strings.HasSuffix(host, ".internal") ||
		strings.HasSuffix(host, ".lan") ||
		strings.HasSuffix(host, ".home") ||
		strings.HasSuffix(host, ".home.arpa") ||
		strings.HasSuffix(host, ".test") ||
		strings.HasSuffix(host, ".invalid") {
		return fmt.Errorf("%s must not use a local hostname in production", name)
	}
	if address := net.ParseIP(host); address != nil && forbiddenPublicConfigIP(address) {
		return fmt.Errorf("%s must not use a private or non-routable address in production", name)
	}
	return nil
}

func forbiddenPublicConfigIP(ip net.IP) bool {
	if ip == nil || ip.IsPrivate() || ip.IsLoopback() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() {
		return true
	}
	address, ok := netip.AddrFromSlice(ip)
	if !ok {
		return true
	}
	address = address.Unmap()
	for _, raw := range []string{"0.0.0.0/8", "100.64.0.0/10", "192.0.0.0/24", "192.0.2.0/24", "198.18.0.0/15", "198.51.100.0/24", "203.0.113.0/24", "240.0.0.0/4", "2001:db8::/32", "fec0::/10"} {
		if netip.MustParsePrefix(raw).Contains(address) {
			return true
		}
	}
	return false
}

func validateTrustedProxies(values []string) error {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			return fmt.Errorf("TRUSTED_PROXIES contains an empty entry")
		}
		if address, err := netip.ParseAddr(value); err == nil {
			if address.IsUnspecified() {
				return fmt.Errorf("TRUSTED_PROXIES must not trust an unspecified address")
			}
			continue
		}
		prefix, err := netip.ParsePrefix(value)
		if err != nil {
			return fmt.Errorf("TRUSTED_PROXIES contains invalid IP or CIDR %q", value)
		}
		if prefix.Bits() == 0 {
			return fmt.Errorf("TRUSTED_PROXIES must not trust the entire Internet")
		}
	}
	return nil
}

func env(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func envInt(key string, fallback int) int {
	value, err := strconv.Atoi(env(key, ""))
	if err != nil {
		return fallback
	}
	return value
}

func envInt64(key string, fallback int64) int64 {
	value, err := strconv.ParseInt(env(key, ""), 10, 64)
	if err != nil {
		return fallback
	}
	return value
}

func envBool(key string, fallback bool) bool {
	value, err := strconv.ParseBool(env(key, ""))
	if err != nil {
		return fallback
	}
	return value
}

func splitNonEmpty(value string) []string {
	var result []string
	for _, item := range strings.Split(value, ",") {
		if trimmed := strings.TrimSpace(item); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}
