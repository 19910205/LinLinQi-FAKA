package handler

import (
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"linlinqi/api/internal/model"
	"linlinqi/api/pkg/response"
)

type userNotificationDTO struct {
	ID        uuid.UUID  `json:"id"`
	EventCode string     `json:"event_code"`
	EntityID  string     `json:"entity_id"`
	Title     string     `json:"title"`
	Body      string     `json:"body"`
	ReadAt    *time.Time `json:"read_at,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
}

func (h Handler) MyNotifications(c *gin.Context) {
	userID, err := uuid.Parse(c.GetString("subject"))
	if err != nil {
		response.Error(c, 401, 40101, "error.login_required")
		return
	}
	page, pageSize := pagination(c)
	query := h.DB.Model(&model.UserNotification{}).Where("user_id = ?", userID)
	if strings.EqualFold(strings.TrimSpace(c.Query("unread")), "true") {
		query = query.Where("read_at IS NULL")
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		response.Error(c, 500, 50003, "error.user_notifications_fetch_failed")
		return
	}
	var rows []model.UserNotification
	if err := query.Order("created_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&rows).Error; err != nil {
		response.Error(c, 500, 50003, "error.user_notifications_fetch_failed")
		return
	}
	items := make([]userNotificationDTO, 0, len(rows))
	for _, row := range rows {
		body, decryptErr := h.Vault.Decrypt(row.BodyCipher, row.BodyNonce, userID[:])
		if decryptErr != nil {
			body = "通知内容暂时无法解密，请联系平台客服"
		}
		items = append(items, userNotificationDTO{ID: row.ID, EventCode: row.EventCode, EntityID: row.EntityID, Title: row.Title, Body: body, ReadAt: row.ReadAt, CreatedAt: row.CreatedAt})
	}
	response.Page(c, items, total, page, pageSize)
}

func (h Handler) MarkMyNotificationRead(c *gin.Context) {
	userID, err := uuid.Parse(c.GetString("subject"))
	if err != nil {
		response.Error(c, 401, 40101, "error.login_required")
		return
	}
	notificationID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, 422, 42201, "error.user_notification_id_invalid")
		return
	}
	now := time.Now().UTC()
	result := h.DB.Model(&model.UserNotification{}).Where("id = ? AND user_id = ?", notificationID, userID).Updates(map[string]any{"read_at": &now})
	if result.Error != nil {
		response.Error(c, 500, 50003, "error.user_notification_read_failed")
		return
	}
	if result.RowsAffected == 0 {
		response.Error(c, 404, 40401, "error.user_notification_not_found")
		return
	}
	response.OK(c, gin.H{"read": true})
}
