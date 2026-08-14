package supply

import (
	"errors"
	"regexp"
	"sort"
	"strings"
	"unicode"
)

type CredentialField struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Type        string `json:"type"`
	Required    bool   `json:"required"`
	Secret      bool   `json:"secret"`
	MinLength   int    `json:"min_length"`
	MaxLength   int    `json:"max_length"`
	Placeholder string `json:"placeholder,omitempty"`
	Help        string `json:"help,omitempty"`
	Managed     bool   `json:"managed,omitempty"`
}

type ProtocolDefinition struct {
	Code              string            `json:"code"`
	Name              string            `json:"name"`
	Family            string            `json:"family"`
	AuthMode          string            `json:"auth_mode"`
	Availability      string            `json:"availability"`
	Capabilities      []string          `json:"capabilities"`
	CredentialFields  []CredentialField `json:"credential_fields"`
	SupportsDiscovery bool              `json:"supports_discovery"`
	Notes             string            `json:"notes,omitempty"`
}

var credentialKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

func field(key, label string, secret bool, min, max int) CredentialField {
	fieldType := "text"
	if secret {
		fieldType = "password"
	}
	return CredentialField{Key: key, Label: label, Type: fieldType, Required: true, Secret: secret, MinLength: min, MaxLength: max}
}

var (
	usernameField  = field("username", "上游用户名", false, 1, 190)
	passwordField  = field("password", "上游密码", true, 1, 500)
	apiKeyField    = field("api_key", "API Key", true, 1, 500)
	apiSecretField = field("api_secret", "API Secret", true, 1, 500)
	tokenField     = field("token", "访问令牌", true, 1, 2000)
	appIDField     = field("app_id", "商户 ID", false, 1, 128)
	appKeyField    = field("app_key", "商户密钥", true, 1, 500)
)

func definition(code, name, family, auth, availability string, credentials []CredentialField, capabilities ...string) ProtocolDefinition {
	return ProtocolDefinition{
		Code: code, Name: name, Family: family, AuthMode: auth, Availability: availability,
		CredentialFields: credentials, Capabilities: capabilities,
		SupportsDiscovery: containsCapability(capabilities, "categories") || containsCapability(capabilities, "products") || containsCapability(capabilities, "services"),
	}
}

func containsCapability(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

var builtInProtocols = func() map[string]ProtocolDefinition {
	couponField := field("coupon", "优惠码 / 身份标识", false, 1, 190)
	couponField.Required = false
	couponField.Help = "可选；仅在 SHOPCLONE7 上游为该账号分配优惠码时填写"
	dongvanUsernameField := usernameField
	dongvanUsernameField.Required = false
	dongvanUsernameField.Help = "兼容旧连接的上游账号；DONGVANFB 请求仅发送 API Key"
	api14UsernameField := usernameField
	api14UsernameField.Required = false
	api14UsernameField.Help = "兼容旧 username/account_id；API_14 实际请求仅使用授权令牌"
	linlinqiKeyField := field("api_key", "API Key", true, 8, 255)
	linlinqiSecretField := field("api_secret", "API Secret", true, 16, 500)
	definitions := []ProtocolDefinition{
		definition("linlinqi-standard", "LinLinQi 标准 OpenAPI", "linlinqi", "hmac_sha256", "supported", []CredentialField{linlinqiKeyField, linlinqiSecretField}, "balance", "categories", "products", "product_detail", "stock", "valuation", "order", "order_query", "callback"),
		definition("cmsnt", "CMSNT / SHOPCLONE5/6", "shopclone", "query_username_password", "supported", []CredentialField{usernameField, passwordField}, "balance", "categories", "products", "order"),
		definition("shopclone7", "SHOPCLONE7", "shopclone", "query_api_key", "supported", []CredentialField{couponField, apiKeyField}, "balance", "products", "order"),
		definition("api-1", "API_1 通用账号协议", "shopclone", "api_key", "supported", []CredentialField{apiKeyField}, "balance", "categories", "products", "order", "order_query"),
		definition("api-4", "API_4 登录换令牌协议", "shopclone", "login_token", "supported", []CredentialField{usernameField, passwordField}, "balance", "categories", "products", "order"),
		definition("dongvanfb", "DONGVANFB", "shopclone", "query_api_key", "supported", []CredentialField{dongvanUsernameField, apiKeyField}, "balance", "products", "order"),
		definition("api-6", "API_6 通用账号协议", "shopclone", "query_api_key", "supported", []CredentialField{apiKeyField}, "balance", "products", "order", "order_query"),
		definition("api-7", "API_7 越南账号协议", "shopclone", "bearer", "supported", []CredentialField{tokenField}, "products", "order"),
		definition("api-8", "API_8 S3 账号协议", "shopclone", "bearer", "supported", []CredentialField{tokenField}, "balance", "products", "order"),
		definition("api-9", "API_9 通用账号协议", "shopclone", "query_api_key", "supported", []CredentialField{apiKeyField}, "balance", "products", "order"),
		definition("api-10", "API_10 临时邮箱协议", "shopclone", "query_api_key", "supported", []CredentialField{apiKeyField}, "balance", "products", "order"),
		definition("api-11", "API_11 FB 账号协议", "shopclone", "query_api_key", "supported", []CredentialField{apiKeyField}, "balance", "products", "order"),
		definition("api-12", "API_12 JSON-RPC 协议", "shopclone", "header_token_json", "supported", []CredentialField{tokenField}, "balance", "products", "order", "order_query"),
		definition("api-13", "API_13 越南服务商", "shopclone", "header_api_key", "supported", []CredentialField{usernameField, apiKeyField}, "products", "order", "order_query"),
		definition("api-14", "API_14 混合 JSON-RPC 协议", "shopclone", "header_token_mixed", "supported", []CredentialField{api14UsernameField, tokenField}, "balance", "products", "order", "order_query"),
		definition("api-15", "API_15 通用账号协议", "shopclone", "query_api_key", "supported", []CredentialField{usernameField, apiKeyField}, "balance", "products", "order"),
		definition("api-16", "API_16（原系统占位）", "shopclone", "none", "unavailable", nil),
		definition("api-17", "API_17 CMSNT 同源协议", "shopclone", "query_username_password", "supported", []CredentialField{usernameField, passwordField}, "balance", "categories", "products", "order"),
		definition("api-23", "API_23 通用账号协议", "shopclone", "json_api_key", "supported", []CredentialField{apiKeyField}, "products", "order"),
		definition("acg-faka-new", "ACG-Faka Shared（新版）", "acg-shared", "acg_form_signature", "supported", []CredentialField{appIDField, appKeyField}, "balance", "categories", "products", "product_detail", "stock", "valuation", "draft", "order", "order_query"),
		definition("acg-faka-old", "ACG-Faka Shared（旧版）", "acg-shared", "acg_form_signature", "supported", []CredentialField{appIDField, appKeyField}, "balance", "categories", "products", "product_detail", "stock", "order", "order_query"),
		definition("dujiao-next", "Dujiao-Next Upstream API", "dujiao", "hmac_sha256", "supported", []CredentialField{apiKeyField, apiSecretField}, "balance", "categories", "products", "product_detail", "stock", "order", "order_query", "order_cancel", "callback"),
		definition("5gsmm", "5GSMM API v2", "smm", "form_api_key", "limited", []CredentialField{apiKeyField}, "balance", "services"),
		definition("cmslike-autofb", "CMSLIKE / AutoFB", "smm", "header_token", "limited", []CredentialField{tokenField}, "valuation", "services", "order", "order_cancel"),
		definition("otp-thuesim-1", "OTP ThueSIM API_1", "otp", "query_token", "limited", []CredentialField{tokenField}, "services", "order", "order_query"),
		definition("otp-thuesim-2", "OTP ThueSIM API_2", "otp", "bearer", "limited", []CredentialField{tokenField}, "services", "order", "order_query"),
		definition("otp-thuesim-3", "OTP ThueSIM API_3", "otp", "query_token", "limited", []CredentialField{tokenField}, "services", "order", "order_query"),
		definition("otp-thuesim-4", "OTP ThueSIM API_4", "otp", "query_api_key", "limited", []CredentialField{apiKeyField}, "services", "order", "order_query"),
		definition("otp-thuesim-5", "OTP ThueSIM API_5", "otp", "query_api_key", "limited", []CredentialField{apiKeyField}, "services", "order", "order_query"),
		definition("vendor-card-system", "Card-System 公开购物 API", "faka-vendor", "public", "limited", nil, "products", "order", "order_query"),
		definition("vendor-kamifaka", "KamiFaka 公开购物 API", "faka-vendor", "public", "limited", nil, "products", "order", "order_query"),
		definition("vendor-mcy-shop", "Mcy-Shop 货源 API", "faka-vendor", "login_token", "limited", []CredentialField{usernameField, passwordField}, "products", "stock", "order", "order_query"),
		definition("vendor-lizhipay-faka", "Lizhipay Faka 购物 API", "faka-vendor", "public", "limited", nil, "products", "valuation", "order", "order_query"),
		definition("vendor-fakawang", "Fakawang Shared API", "acg-shared", "acg_form_signature", "supported", []CredentialField{appIDField, appKeyField}, "balance", "products", "product_detail", "stock", "order", "order_query"),
		definition("vendor-orion-key", "Orion Key 用户 API", "faka-vendor", "login_token", "limited", []CredentialField{usernameField, passwordField}, "products", "order", "order_query"),
		definition("vendor-edgekey", "EdgeKey Telefunc", "faka-vendor", "public", "limited", nil, "products", "order", "order_query"),
		definition("vendor-geekfaka", "GeekFaka 购物 API", "faka-vendor", "public", "limited", nil, "products", "order", "order_query"),
		definition("vendor-zzdylan-faka", "ZzDylan Faka 购物 API", "faka-vendor", "public", "limited", nil, "products", "order", "order_query"),
		definition("vendor-dujiaoka", "Dujiaoka 前台订单协议", "dujiao", "browser_csrf", "reference_only", nil, "products", "order", "order_query"),
		definition("vendor-yuimoi-tgfaka", "YuiMoi Telegram 发卡", "telegram", "telegram_bot", "reference_only", []CredentialField{tokenField}),
	}
	result := make(map[string]ProtocolDefinition, len(definitions))
	for _, item := range definitions {
		if item.Code == "api-16" {
			item.Notes = "协议资料明确说明原系统仅保留后台选项，没有可调用实现。"
		}
		if item.Availability == "limited" {
			item.Notes = "协议缺少完整供应商结算或自动交付能力，启用前需要逐接口连接测试。"
		}
		if item.Availability == "reference_only" {
			item.Notes = "仅作为兼容参考，不能用于自动供应采购。"
		}
		result[item.Code] = item
	}
	return result
}()

// RuntimeAvailable reports whether NewGateway has a concrete constructor for
// the protocol. It is intentionally independent from the advertised maturity
// level so registry/runtime drift can be detected in tests and admin callers.
func RuntimeAvailable(code string) bool {
	code = strings.ToLower(strings.TrimSpace(code))
	if shopcloneProtocols[code] != nil {
		return true
	}
	switch code {
	case "linlinqi-standard", "dujiao-next", "acg-faka-new", "acg-faka-old", "vendor-fakawang", "5gsmm":
		return true
	default:
		return false
	}
}

// Executable is the authoritative save/run gate: only supported protocols
// with a real runtime adapter may be persisted as executable suppliers.
func Executable(code string) bool {
	item, exists := builtInProtocols[strings.ToLower(strings.TrimSpace(code))]
	return exists && item.Availability == "supported" && RuntimeAvailable(item.Code)
}

func effectiveProtocolDefinition(item ProtocolDefinition) ProtocolDefinition {
	if !RuntimeAvailable(item.Code) && item.Availability != "unavailable" && item.Availability != "reference_only" {
		item.Availability = "unavailable"
		item.Notes = "运行时适配器尚未实现，不能保存为可执行供应商。"
	}
	item.Capabilities = append([]string(nil), item.Capabilities...)
	item.CredentialFields = append([]CredentialField(nil), item.CredentialFields...)
	return item
}

func Protocols() []ProtocolDefinition {
	result := make([]ProtocolDefinition, 0, len(builtInProtocols))
	for _, item := range builtInProtocols {
		result = append(result, effectiveProtocolDefinition(item))
	}
	sort.Slice(result, func(left, right int) bool {
		if result[left].Family == result[right].Family {
			return result[left].Name < result[right].Name
		}
		return result[left].Family < result[right].Family
	})
	return result
}

func Protocol(code string) (ProtocolDefinition, bool) {
	item, exists := builtInProtocols[strings.ToLower(strings.TrimSpace(code))]
	if !exists {
		return ProtocolDefinition{}, false
	}
	return effectiveProtocolDefinition(item), true
}

func ValidateCredentials(protocol string, credentials map[string]string) (map[string]string, error) {
	protocol = strings.ToLower(strings.TrimSpace(protocol))
	definition, exists := Protocol(protocol)
	if !exists || !Executable(protocol) {
		return nil, errors.New("supplier protocol is not executable")
	}
	if credentials == nil {
		credentials = map[string]string{}
	}
	input := make(map[string]string, len(credentials))
	for rawKey, value := range credentials {
		key := strings.ToLower(strings.TrimSpace(rawKey))
		if previous, duplicate := input[key]; duplicate && previous != value {
			return nil, errors.New("credential field is ambiguous")
		}
		input[key] = value
	}
	if protocol == "api-13" || protocol == "api-14" {
		legacy := strings.TrimSpace(input["account_id"])
		canonical := strings.TrimSpace(input["username"])
		if legacy != "" && canonical != "" && legacy != canonical {
			return nil, errors.New("legacy credential conflicts with username")
		}
		if canonical == "" && legacy != "" {
			input["username"] = legacy
		}
		delete(input, "account_id")
	}
	allowed := make(map[string]CredentialField, len(definition.CredentialFields))
	for _, item := range definition.CredentialFields {
		allowed[item.Key] = item
	}
	normalized := make(map[string]string, len(input))
	for key, raw := range input {
		field, ok := allowed[key]
		if !ok || !credentialKeyPattern.MatchString(key) {
			return nil, errors.New("credential field is not supported by protocol")
		}
		value := raw
		if !field.Secret {
			value = strings.TrimSpace(value)
		}
		if value == "" && !field.Required {
			continue
		}
		length := len([]rune(value))
		if length < field.MinLength || length > field.MaxLength || strings.IndexFunc(value, unicode.IsControl) >= 0 {
			return nil, errors.New("credential value is invalid")
		}
		normalized[key] = value
	}
	for _, item := range definition.CredentialFields {
		if item.Required && normalized[item.Key] == "" {
			return nil, errors.New("required credential is missing")
		}
	}
	return normalized, nil
}

func CredentialKeys(protocol string) []string {
	definition, exists := Protocol(protocol)
	if !exists {
		return nil
	}
	result := make([]string, 0, len(definition.CredentialFields))
	for _, item := range definition.CredentialFields {
		result = append(result, item.Key)
	}
	return result
}
