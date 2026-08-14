package handler

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"linlinqi/api/internal/i18n"
	"linlinqi/api/internal/model"
	"linlinqi/api/internal/security"
	"linlinqi/api/pkg/response"
)

var allowedAPIPermissions = map[string]struct{}{
	"orders:read":   {},
	"orders:write":  {},
	"products:read": {},
}

func (h Handler) AdminAPICredentials(c *gin.Context) {
	page, pageSize := pagination(c)
	query := h.DB.Model(&model.APICredential{})
	if status := strings.TrimSpace(c.Query("status")); status != "" {
		query = query.Where("status = ?", status)
	}
	if keyword := strings.TrimSpace(c.Query("q")); keyword != "" {
		like := "%" + keyword + "%"
		query = query.Where("name ILIKE ? OR key ILIKE ?", like, like)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		response.Error(c, 500, 50056, "error.api_credential_fetch_failed")
		return
	}
	var items []model.APICredential
	if err := query.Order("created_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&items).Error; err != nil {
		response.Error(c, 500, 50056, "error.api_credential_fetch_failed")
		return
	}
	response.Page(c, items, total, page, pageSize)
}

type apiCredentialUpdateRequest struct {
	Status      *string  `json:"status"`
	Permissions []string `json:"permissions"`
}

func (h Handler) UpdateAPICredential(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, 422, 42255, "error.api_credential_id_invalid")
		return
	}
	var req apiCredentialUpdateRequest
	if decodeStrictJSON(c, &req) != nil || (req.Status == nil && req.Permissions == nil) {
		response.Error(c, 422, 42256, "error.api_credential_no_updatable_fields")
		return
	}
	reason := strings.TrimSpace(c.GetHeader("X-Change-Reason"))
	if reason == "" {
		response.Error(c, 422, 42257, "error.change_reason_required")
		return
	}
	updates := map[string]any{}
	if req.Status != nil {
		status := strings.TrimSpace(*req.Status)
		if status != "pending" && status != "active" && status != "suspended" {
			response.Error(c, 422, 42258, "error.api_credential_status_invalid")
			return
		}
		updates["status"] = status
	}
	if req.Permissions != nil {
		unique := map[string]struct{}{}
		for _, permission := range req.Permissions {
			permission = strings.TrimSpace(permission)
			if _, ok := allowedAPIPermissions[permission]; !ok {
				response.Error(c, 422, 42259, "error.api_scope_invalid")
				return
			}
			unique[permission] = struct{}{}
		}
		if len(unique) == 0 {
			response.Error(c, 422, 42259, "error.api_scope_required")
			return
		}
		permissions := make([]string, 0, len(unique))
		for permission := range unique {
			permissions = append(permissions, permission)
		}
		sort.Strings(permissions)
		updates["permissions"] = strings.Join(permissions, ",")
	}
	var credential model.APICredential
	err = h.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&credential, "id = ?", id).Error; err != nil {
			return err
		}
		if credential.Status == "revoked" {
			return errAPICredentialRevoked
		}
		if err := tx.Model(&credential).Updates(updates).Error; err != nil {
			return err
		}
		return tx.First(&credential, "id = ?", id).Error
	})
	if err == gorm.ErrRecordNotFound {
		response.Error(c, 404, 40456, "error.api_credential_not_found")
		return
	}
	if errors.Is(err, errAPICredentialRevoked) {
		response.Error(c, 409, 40956, "error.api_credential_revoked_terminal")
		return
	}
	if err != nil {
		response.Error(c, 500, 50057, "error.api_credential_update_failed")
		return
	}
	h.audit(c, "api-credential.update", "api-credential", id.String(), credential.Status)
	response.OK(c, credential)
}

func (h Handler) AdminAPICallLogs(c *gin.Context) {
	page, pageSize := pagination(c)
	query := h.DB.Model(&model.APICallLog{})
	if credentialID := strings.TrimSpace(c.Query("credential_id")); credentialID != "" {
		parsed, err := uuid.Parse(credentialID)
		if err != nil {
			response.Error(c, 422, 42260, "error.api_credential_id_invalid")
			return
		}
		query = query.Where("credential_id = ?", parsed)
	}
	if method := strings.ToUpper(strings.TrimSpace(c.Query("method"))); method != "" {
		query = query.Where("method = ?", method)
	}
	if statusClass := strings.TrimSpace(c.Query("status_class")); len(statusClass) == 1 && statusClass[0] >= '1' && statusClass[0] <= '5' {
		base := int(statusClass[0]-'0') * 100
		query = query.Where("status_code >= ? AND status_code < ?", base, base+100)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		response.Error(c, 500, 50058, "error.api_call_log_fetch_failed")
		return
	}
	var items []model.APICallLog
	if err := query.Order("created_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&items).Error; err != nil {
		response.Error(c, 500, 50058, "error.api_call_log_fetch_failed")
		return
	}
	response.Page(c, items, total, page, pageSize)
}

func (h Handler) MyWebhooks(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	var items []model.WebhookEndpoint
	if err := h.DB.Where("owner_type = ? AND owner_id = ?", "user", userID).Order("created_at DESC").Find(&items).Error; err != nil {
		response.Error(c, 500, 50059, "error.webhook_fetch_failed")
		return
	}
	result := make([]userWebhookEndpointDTO, 0, len(items))
	for _, item := range items {
		result = append(result, toUserWebhookEndpointDTO(item))
	}
	response.OK(c, result)
}

type webhookRequest struct {
	URL    string   `json:"url" binding:"required,max=500"`
	Events []string `json:"events" binding:"required,min=1,max=10"`
}

// normalizeWebhookEndpointURL applies the production outbound policy to every
// customer-controlled webhook destination, even when the application itself
// runs in development or test mode. Unlike internal development connectors,
// webhooks are an untrusted egress boundary and must never be able to target
// local infrastructure. The worker validates and pins DNS again at dial time
// and refuses redirects, closing the TOCTOU and redirect portions of SSRF.
func normalizeWebhookEndpointURL(ctx context.Context, raw string) (string, error) {
	validationContext, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	parsed, err := security.ValidateOutboundURL(validationContext, strings.TrimSpace(raw), false)
	if err != nil {
		return "", err
	}
	return parsed.String(), nil
}

func validateWebhookEndpointActivation(ctx context.Context, raw string, enabled bool) error {
	if !enabled {
		return nil
	}
	_, err := normalizeWebhookEndpointURL(ctx, raw)
	return err
}

func (h Handler) CreateMyWebhook(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	var req webhookRequest
	if c.ShouldBindJSON(&req) != nil {
		response.Error(c, 422, 42261, "error.webhook_parameters_invalid")
		return
	}
	if len(req.Events) != 1 || strings.TrimSpace(req.Events[0]) != "order.delivered" {
		response.Error(c, 422, 42262, "error.webhook_event_unsupported")
		return
	}
	webhookURL, err := normalizeWebhookEndpointURL(c.Request.Context(), req.URL)
	if err != nil {
		response.Error(c, 422, 42263, "error.webhook_parameters_invalid")
		return
	}
	var count int64
	if err := h.DB.Model(&model.WebhookEndpoint{}).Where("owner_type = ? AND owner_id = ?", "user", userID).Count(&count).Error; err != nil {
		response.Error(c, 500, 50060, "error.webhook_create_failed")
		return
	}
	if count >= 10 {
		response.Error(c, 409, 40960, "error.webhook_limit_reached")
		return
	}
	var existing model.WebhookEndpoint
	if h.DB.Where("owner_type = ? AND owner_id = ? AND url = ?", "user", userID, webhookURL).First(&existing).Error == nil {
		response.Error(c, 409, 40961, "error.webhook_url_exists")
		return
	}
	secret, _, err := randomRefreshToken()
	if err != nil {
		response.Error(c, 500, 50060, "error.webhook_secret_generate_failed")
		return
	}
	endpoint := model.WebhookEndpoint{Base: model.Base{ID: uuid.New()}, OwnerType: "user", OwnerID: userID, URL: webhookURL, Events: `["order.delivered"]`, Enabled: true}
	ciphertext, nonce, _, err := h.Vault.Encrypt(secret, endpoint.ID[:])
	if err != nil {
		response.Error(c, 500, 50060, "error.webhook_secret_generate_failed")
		return
	}
	endpoint.SecretCipher, endpoint.SecretNonce = ciphertext, nonce
	if err := h.DB.Create(&endpoint).Error; err != nil {
		response.Error(c, 500, 50060, "error.webhook_create_failed")
		return
	}
	response.Created(c, gin.H{"endpoint": toUserWebhookEndpointDTO(endpoint), "secret": secret, "notice": i18n.Localize(c, "notice.webhook_secret_once")})
}

func (h Handler) DeleteMyWebhook(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, 422, 42264, "error.webhook_id_invalid")
		return
	}
	result := h.DB.Where("id = ? AND owner_type = ? AND owner_id = ?", id, "user", userID).Delete(&model.WebhookEndpoint{})
	if result.Error != nil {
		response.Error(c, 500, 50061, "error.webhook_delete_failed")
		return
	}
	if result.RowsAffected == 0 {
		response.Error(c, 404, 40460, "error.webhook_not_found")
		return
	}
	response.OK(c, gin.H{"deleted": true})
}
