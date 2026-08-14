package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/mail"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/oauth2"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"linlinqi/api/internal/config"
	"linlinqi/api/internal/middleware"
	"linlinqi/api/internal/model"
	"linlinqi/api/internal/security"
	"linlinqi/api/pkg/response"
)

const (
	oauthStateTTL    = 10 * time.Minute
	oauthExchangeTTL = 2 * time.Minute
)

type oauthStateRecord struct {
	Provider string `json:"provider"`
	Nonce    string `json:"nonce"`
	Verifier string `json:"verifier"`
	Redirect string `json:"redirect"`
}

type oauthExchangeRecord struct {
	UserID   uuid.UUID `json:"user_id"`
	Redirect string    `json:"redirect"`
}

type oidcClaims struct {
	Subject           string `json:"sub"`
	Email             string `json:"email"`
	EmailVerified     bool   `json:"email_verified"`
	Nonce             string `json:"nonce"`
	Name              string `json:"name"`
	PreferredUsername string `json:"preferred_username"`
	Picture           string `json:"picture"`
}

type cachedOIDCProvider struct {
	Provider  *oidc.Provider
	ExpiresAt time.Time
}

var oidcProviderCache sync.Map

func oauthRedisKey(kind, token string) string {
	return "linlinqi:oauth:" + kind + ":" + refreshHash(token)
}

func safeOAuthRedirect(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "/account/profile"
	}
	if len(value) > 500 || !strings.HasPrefix(value, "/") || strings.HasPrefix(value, "//") || strings.Contains(value, "\\") {
		return "/account/profile"
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return "/account/profile"
		}
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.IsAbs() || parsed.Host != "" || parsed.User != nil || parsed.Fragment != "" {
		return "/account/profile"
	}
	return value
}

func (h Handler) oauthProviderConfig(code string) (config.OAuthProviderConfig, bool) {
	providers, err := h.Cfg.OAuthProviders()
	if err != nil {
		return config.OAuthProviderConfig{}, false
	}
	provider, ok := providers[code]
	return provider, ok
}

func (h Handler) oidcProvider(ctx context.Context, providerConfig config.OAuthProviderConfig) (*oidc.Provider, context.Context, error) {
	if h.Cfg.Env == "production" {
		if _, err := security.ValidateOutboundURL(ctx, providerConfig.Issuer, false); err != nil {
			return nil, ctx, err
		}
	}
	httpClient := security.NewOutboundHTTPClient(12*time.Second, h.Cfg.Env != "production")
	requestContext := oidc.ClientContext(ctx, httpClient)
	if cached, ok := oidcProviderCache.Load(providerConfig.Issuer); ok {
		entry := cached.(cachedOIDCProvider)
		if entry.ExpiresAt.After(time.Now()) {
			return entry.Provider, requestContext, nil
		}
		oidcProviderCache.Delete(providerConfig.Issuer)
	}
	provider, err := oidc.NewProvider(requestContext, providerConfig.Issuer)
	if err != nil {
		return nil, requestContext, err
	}
	oidcProviderCache.Store(providerConfig.Issuer, cachedOIDCProvider{Provider: provider, ExpiresAt: time.Now().Add(time.Hour)})
	return provider, requestContext, nil
}

func (h Handler) oauth2Config(code string, providerConfig config.OAuthProviderConfig, provider *oidc.Provider) oauth2.Config {
	return oauth2.Config{
		ClientID: providerConfig.ClientID, ClientSecret: providerConfig.ClientSecret,
		Endpoint: provider.Endpoint(), Scopes: providerConfig.Scopes,
		RedirectURL: strings.TrimRight(h.Cfg.AppURL, "/") + "/api/v1/auth/oauth/" + code + "/callback",
	}
}

func (h Handler) OAuthProviders(c *gin.Context) {
	providers, err := h.Cfg.OAuthProviders()
	if err != nil {
		response.Error(c, 503, 50395, "error.oauth_provider_config_fetch_failed")
		return
	}
	type publicProvider struct {
		Code string `json:"code"`
		Name string `json:"name"`
	}
	items := make([]publicProvider, 0, len(providers))
	for code, provider := range providers {
		items = append(items, publicProvider{Code: code, Name: provider.Name})
	}
	sort.Slice(items, func(left, right int) bool { return items[left].Name < items[right].Name })
	response.OK(c, items)
}

func (h Handler) StartOAuth(c *gin.Context) {
	code := strings.ToLower(strings.TrimSpace(c.Param("provider")))
	providerConfig, ok := h.oauthProviderConfig(code)
	if !ok {
		response.Error(c, 404, 40499, "error.oauth_provider_not_found_or_disabled")
		return
	}
	provider, requestContext, err := h.oidcProvider(c.Request.Context(), providerConfig)
	if err != nil {
		response.Error(c, 503, 50395, "error.oauth_idp_init_failed")
		return
	}
	state, _, err := randomRefreshToken()
	if err != nil {
		response.Error(c, 500, 50127, "error.oauth_security_params_generate_failed")
		return
	}
	nonce, _, err := randomRefreshToken()
	if err != nil {
		response.Error(c, 500, 50127, "error.oauth_security_params_generate_failed")
		return
	}
	verifier, _, err := randomRefreshToken()
	if err != nil {
		response.Error(c, 500, 50127, "error.oauth_security_params_generate_failed")
		return
	}
	record := oauthStateRecord{Provider: code, Nonce: nonce, Verifier: verifier, Redirect: safeOAuthRedirect(c.Query("redirect"))}
	payload, _ := json.Marshal(record)
	stored, err := h.Redis.SetNX(requestContext, oauthRedisKey("state", state), payload, oauthStateTTL).Result()
	if err != nil || !stored {
		response.Error(c, 503, 50395, "error.oauth_login_state_save_failed")
		return
	}
	configuration := h.oauth2Config(code, providerConfig, provider)
	authURL := configuration.AuthCodeURL(
		state,
		oauth2.SetAuthURLParam("nonce", nonce),
		oauth2.S256ChallengeOption(verifier),
	)
	response.OK(c, gin.H{"auth_url": authURL})
}

func (h Handler) oauthFrontendRedirect(c *gin.Context, values url.Values) {
	target, err := url.Parse(strings.TrimRight(h.Cfg.UserAppURL, "/") + "/auth/oauth/callback")
	if err != nil {
		response.Error(c, 500, 50127, "error.oauth_callback_url_build_failed")
		return
	}
	target.RawQuery = values.Encode()
	c.Redirect(303, target.String())
}

func (h Handler) oauthCallbackFailure(c *gin.Context, code string) {
	h.oauthFrontendRedirect(c, url.Values{"error": []string{code}})
}

func (h Handler) OAuthCallback(c *gin.Context) {
	providerCode := strings.ToLower(strings.TrimSpace(c.Param("provider")))
	state := strings.TrimSpace(c.Query("state"))
	authorizationCode := strings.TrimSpace(c.Query("code"))
	if len(state) < 32 || len(state) > 200 || authorizationCode == "" || len(authorizationCode) > 2048 || c.Query("error") != "" {
		h.oauthCallbackFailure(c, "invalid_callback")
		return
	}
	payload, err := h.Redis.GetDel(c.Request.Context(), oauthRedisKey("state", state)).Bytes()
	if err != nil {
		h.oauthCallbackFailure(c, "invalid_or_expired_state")
		return
	}
	var stateRecord oauthStateRecord
	if json.Unmarshal(payload, &stateRecord) != nil || stateRecord.Provider != providerCode || len(stateRecord.Nonce) < 32 || len(stateRecord.Verifier) < 43 {
		h.oauthCallbackFailure(c, "invalid_or_expired_state")
		return
	}
	providerConfig, ok := h.oauthProviderConfig(providerCode)
	if !ok {
		h.oauthCallbackFailure(c, "provider_unavailable")
		return
	}
	provider, requestContext, err := h.oidcProvider(c.Request.Context(), providerConfig)
	if err != nil {
		h.oauthCallbackFailure(c, "provider_unavailable")
		return
	}
	configuration := h.oauth2Config(providerCode, providerConfig, provider)
	oauthToken, err := configuration.Exchange(requestContext, authorizationCode, oauth2.VerifierOption(stateRecord.Verifier))
	if err != nil {
		h.oauthCallbackFailure(c, "code_exchange_failed")
		return
	}
	rawIDToken, ok := oauthToken.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		h.oauthCallbackFailure(c, "missing_id_token")
		return
	}
	idToken, err := provider.Verifier(&oidc.Config{ClientID: providerConfig.ClientID}).Verify(requestContext, rawIDToken)
	if err != nil {
		h.oauthCallbackFailure(c, "invalid_id_token")
		return
	}
	var claims oidcClaims
	if idToken.Claims(&claims) != nil || claims.Subject == "" || len(claims.Subject) > 190 || claims.Nonce != stateRecord.Nonce {
		h.oauthCallbackFailure(c, "invalid_identity")
		return
	}
	if claims.Email == "" || !claims.EmailVerified {
		if info, infoErr := provider.UserInfo(requestContext, oauth2.StaticTokenSource(oauthToken)); infoErr == nil {
			claims.Email = info.Email
			claims.EmailVerified = info.EmailVerified
			if claims.Subject == "" {
				claims.Subject = info.Subject
			}
		}
	}
	claims.Email = strings.ToLower(strings.TrimSpace(claims.Email))
	parsedEmail, emailErr := mail.ParseAddress(claims.Email)
	if emailErr != nil || !claims.EmailVerified || !strings.EqualFold(parsedEmail.Address, claims.Email) || len(claims.Email) > 190 {
		h.oauthCallbackFailure(c, "verified_email_required")
		return
	}
	user, err := h.linkOAuthIdentity(providerCode, claims)
	if err != nil || user.Status != "active" {
		h.recordLogin(c, "user", claims.Email, nil, false, "oauth_identity_rejected")
		h.oauthCallbackFailure(c, "account_unavailable")
		return
	}
	h.recordLogin(c, "user", user.Email, &user.ID, true, "")
	exchangeCode, _, err := randomRefreshToken()
	if err != nil {
		h.oauthCallbackFailure(c, "exchange_unavailable")
		return
	}
	exchangeRecord := oauthExchangeRecord{UserID: user.ID, Redirect: safeOAuthRedirect(stateRecord.Redirect)}
	exchangePayload, _ := json.Marshal(exchangeRecord)
	stored, err := h.Redis.SetNX(c.Request.Context(), oauthRedisKey("exchange", exchangeCode), exchangePayload, oauthExchangeTTL).Result()
	if err != nil || !stored {
		h.oauthCallbackFailure(c, "exchange_unavailable")
		return
	}
	h.oauthFrontendRedirect(c, url.Values{"code": []string{exchangeCode}})
}

var errOAuthAccountUnavailable = errors.New("oauth account unavailable")

func oauthNickname(claims oidcClaims) string {
	name := strings.TrimSpace(claims.Name)
	if name == "" {
		name = strings.TrimSpace(claims.PreferredUsername)
	}
	if name == "" {
		name = strings.Split(claims.Email, "@")[0]
	}
	runes := []rune(name)
	if len(runes) > 80 {
		name = string(runes[:80])
	}
	if !validUserNickname(name) {
		return "LinLinQi 会员"
	}
	return name
}

func (h Handler) linkOAuthIdentity(providerCode string, claims oidcClaims) (model.User, error) {
	metadata, _ := json.Marshal(map[string]string{
		"name": claims.Name, "preferred_username": claims.PreferredUsername, "picture": claims.Picture,
	})
	var user model.User
	err := h.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtext(?))", "oauth-subject:"+providerCode+":"+claims.Subject).Error; err != nil {
			return err
		}
		if err := tx.Exec("SELECT pg_advisory_xact_lock(hashtext(?))", "oauth-email:"+claims.Email).Error; err != nil {
			return err
		}
		var identity model.OAuthIdentity
		identityErr := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("provider = ? AND provider_user_id = ?", providerCode, claims.Subject).First(&identity).Error
		if identityErr == nil {
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&user, "id = ?", identity.UserID).Error; err != nil {
				return err
			}
			if user.Status != "active" {
				return errOAuthAccountUnavailable
			}
			if err := tx.Model(&identity).Updates(map[string]any{"email": claims.Email, "metadata": string(metadata)}).Error; err != nil {
				return err
			}
		} else if errors.Is(identityErr, gorm.ErrRecordNotFound) {
			userErr := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("email = ?", claims.Email).First(&user).Error
			if errors.Is(userErr, gorm.ErrRecordNotFound) {
				randomPassword, _, randomErr := randomRefreshToken()
				if randomErr != nil {
					return randomErr
				}
				hash, hashErr := bcrypt.GenerateFromPassword([]byte(randomPassword), bcrypt.DefaultCost)
				if hashErr != nil {
					return hashErr
				}
				user = model.User{Email: claims.Email, PasswordHash: string(hash), Nickname: oauthNickname(claims), Status: "active"}
				if err := tx.Create(&user).Error; err != nil {
					return err
				}
			} else if userErr != nil {
				return userErr
			} else if user.Status != "active" {
				return errOAuthAccountUnavailable
			}
			identity = model.OAuthIdentity{
				UserID: user.ID, Provider: providerCode, ProviderUserID: claims.Subject,
				Email: claims.Email, Metadata: string(metadata),
			}
			if err := tx.Create(&identity).Error; err != nil {
				return err
			}
		} else {
			return identityErr
		}
		now := time.Now()
		user.LastLoginAt = now
		return tx.Model(&user).Update("last_login_at", now).Error
	})
	return user, err
}

type oauthExchangeRequest struct {
	Code string `json:"code"`
}

func (h Handler) ExchangeOAuthCode(c *gin.Context) {
	var req oauthExchangeRequest
	if decodeStrictJSON(c, &req) != nil {
		response.Error(c, 422, 42341, "error.oauth_exchange_request_invalid")
		return
	}
	req.Code = strings.TrimSpace(req.Code)
	if len(req.Code) < 32 || len(req.Code) > 200 {
		response.Error(c, 422, 42341, "error.oauth_authorization_code_invalid")
		return
	}
	payload, err := h.Redis.GetDel(c.Request.Context(), oauthRedisKey("exchange", req.Code)).Bytes()
	if err != nil {
		response.Error(c, 401, 40195, "error.oauth_session_invalid_or_expired")
		return
	}
	var exchange oauthExchangeRecord
	if json.Unmarshal(payload, &exchange) != nil || exchange.UserID == uuid.Nil {
		response.Error(c, 401, 40195, "error.oauth_session_invalid_or_expired")
		return
	}
	var user model.User
	if err := h.DB.Where("id = ? AND status = ?", exchange.UserID, "active").First(&user).Error; err != nil {
		response.Error(c, 401, 40195, "error.oauth_session_invalid_or_expired")
		return
	}
	refreshToken, sessionID, err := h.createUserSession(c, user.ID)
	if err != nil {
		response.Error(c, 500, 50127, "error.oauth_login_session_create_failed")
		return
	}
	accessToken, err := middleware.IssueUserToken(user.ID.String(), h.Cfg.JWTSecret, sessionID.String(), user.SessionVersion, 15*time.Minute)
	if err != nil {
		response.Error(c, 500, 50127, "error.oauth_login_session_create_failed")
		return
	}
	response.OK(c, gin.H{
		"token": accessToken, "refresh_token": refreshToken, "expires_in": 900,
		"user": toUserAccountDTO(user), "redirect": safeOAuthRedirect(exchange.Redirect),
	})
}
