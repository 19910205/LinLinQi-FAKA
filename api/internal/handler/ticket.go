package handler

import (
	"errors"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"linlinqi/api/internal/model"
	"linlinqi/api/internal/queue"
	"linlinqi/api/pkg/response"
)

func (h Handler) createTicketNotification(tx *gorm.DB, channel, recipient, subject, body, idempotencyKey string) (model.NotificationDelivery, error) {
	delivery := model.NotificationDelivery{Base: model.Base{ID: uuid.New()}, IdempotencyKey: idempotencyKey, Channel: channel, Recipient: recipient, Subject: subject, Status: "queued"}
	ciphertext, nonce, _, err := h.Vault.Encrypt(body, delivery.ID[:])
	if err != nil {
		return model.NotificationDelivery{}, err
	}
	delivery.BodyCipher, delivery.BodyNonce = ciphertext, nonce
	if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "idempotency_key"}}, DoNothing: true}).Create(&delivery).Error; err != nil {
		return model.NotificationDelivery{}, err
	}
	if err := tx.Where("idempotency_key = ?", idempotencyKey).First(&delivery).Error; err != nil {
		return model.NotificationDelivery{}, err
	}
	return delivery, nil
}

func (h Handler) enqueueNotification(deliveryID uuid.UUID) {
	if deliveryID == uuid.Nil {
		return
	}
	client := queue.NewClient(h.Cfg, h.DB)
	defer client.Close()
	_, _ = client.Enqueue(queue.TypeNotificationDispatch, map[string]string{"delivery_id": deliveryID.String()})
}

func (h Handler) MyTicket(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	ticketID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, 422, 42246, "error.ticket_number_invalid")
		return
	}
	var ticket model.SupportTicket
	if err := h.DB.Where("id = ? AND user_id = ?", ticketID, userID).First(&ticket).Error; err != nil {
		response.Error(c, 404, 40441, "error.ticket_not_found")
		return
	}
	var messages []model.TicketMessage
	if err := h.DB.Where("ticket_id = ? AND internal = ?", ticket.ID, false).Order("created_at ASC").Find(&messages).Error; err != nil {
		response.Error(c, 500, 50041, "error.ticket_message_fetch_failed")
		return
	}
	if ticket.UserUnread > 0 {
		_ = h.DB.Model(&model.SupportTicket{}).Where("id = ? AND user_id = ?", ticket.ID, userID).Update("user_unread", 0).Error
		ticket.UserUnread = 0
	}
	response.OK(c, gin.H{"ticket": ticket, "messages": messages})
}

type ticketMessageRequest struct {
	Body string `json:"body"`
}

func (h Handler) ReplyMyTicket(c *gin.Context) {
	userID, ok := currentUserID(c)
	if !ok {
		return
	}
	ticketID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, 422, 42246, "error.ticket_number_invalid")
		return
	}
	var req ticketMessageRequest
	if decodeStrictJSON(c, &req) != nil {
		response.Error(c, 422, 42247, "error.reply_content_invalid")
		return
	}
	req.Body = strings.TrimSpace(req.Body)
	if len([]rune(req.Body)) < 1 || len([]rune(req.Body)) > 10000 {
		response.Error(c, 422, 42247, "error.reply_length_range")
		return
	}
	var message model.TicketMessage
	var deliveryID uuid.UUID
	err = h.DB.Transaction(func(tx *gorm.DB) error {
		var ticket model.SupportTicket
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ? AND user_id = ?", ticketID, userID).First(&ticket).Error; err != nil {
			return err
		}
		if ticket.Status == "closed" {
			return errTicketClosed
		}
		message = model.TicketMessage{TicketID: ticket.ID, AuthorType: "user", AuthorID: &userID, Body: req.Body}
		if err := tx.Create(&message).Error; err != nil {
			return err
		}
		now := time.Now()
		if err := tx.Model(&ticket).Updates(map[string]any{"status": "open", "last_message_at": &now, "admin_unread": gorm.Expr("admin_unread + 1"), "closed_at": nil}).Error; err != nil {
			return err
		}
		delivery, err := h.createTicketNotification(tx, "admin", "admin", "工单有新回复 "+ticket.TicketNo, "用户回复了工单："+ticket.Subject, "ticket-user-reply:"+message.ID.String())
		if err != nil {
			return err
		}
		deliveryID = delivery.ID
		return nil
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		response.Error(c, 404, 40441, "error.ticket_not_found")
		return
	}
	if errors.Is(err, errTicketClosed) {
		response.Error(c, 409, 40941, "error.ticket_closed_create_new")
		return
	}
	if err != nil {
		response.Error(c, 500, 50041, "error.ticket_reply_send_failed")
		return
	}
	h.enqueueNotification(deliveryID)
	response.Created(c, message)
}

var errTicketClosed = errors.New("ticket is closed")

func (h Handler) AdminTicket(c *gin.Context) {
	ticketID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, 422, 42246, "error.ticket_number_invalid")
		return
	}
	var ticket model.SupportTicket
	if err := h.DB.First(&ticket, "id = ?", ticketID).Error; err != nil {
		response.Error(c, 404, 40441, "error.ticket_not_found")
		return
	}
	var messages []model.TicketMessage
	if err := h.DB.Where("ticket_id = ?", ticket.ID).Order("created_at ASC").Find(&messages).Error; err != nil {
		response.Error(c, 500, 50041, "error.ticket_message_fetch_failed")
		return
	}
	if ticket.AdminUnread > 0 {
		_ = h.DB.Model(&model.SupportTicket{}).Where("id = ?", ticket.ID).Update("admin_unread", 0).Error
		ticket.AdminUnread = 0
	}
	response.OK(c, gin.H{"ticket": ticket, "messages": messages})
}

type adminTicketReplyRequest struct {
	Body     string `json:"body"`
	Internal bool   `json:"internal"`
	Status   string `json:"status"`
}

func (h Handler) ReplyAdminTicket(c *gin.Context) {
	ticketID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, 422, 42246, "error.ticket_number_invalid")
		return
	}
	var req adminTicketReplyRequest
	if decodeStrictJSON(c, &req) != nil {
		response.Error(c, 422, 42247, "error.reply_content_invalid")
		return
	}
	req.Body = strings.TrimSpace(req.Body)
	if len([]rune(req.Body)) < 1 || len([]rune(req.Body)) > 10000 {
		response.Error(c, 422, 42247, "error.reply_length_range")
		return
	}
	adminID, _ := uuid.Parse(c.GetString("subject"))
	var message model.TicketMessage
	var deliveryID uuid.UUID
	err = h.DB.Transaction(func(tx *gorm.DB) error {
		var ticket model.SupportTicket
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&ticket, "id = ?", ticketID).Error; err != nil {
			return err
		}
		message = model.TicketMessage{TicketID: ticket.ID, AuthorType: "admin", AuthorID: &adminID, Body: req.Body, Internal: req.Internal}
		if err := tx.Create(&message).Error; err != nil {
			return err
		}
		now := time.Now()
		updates := map[string]any{"last_message_at": &now}
		if !req.Internal {
			target := strings.TrimSpace(req.Status)
			if target == "" {
				target = "waiting_user"
			}
			if !validTicketTransition(ticket.Status, target) {
				return errInvalidTicketTransition
			}
			updates["status"] = target
			updates["user_unread"] = gorm.Expr("user_unread + 1")
			if target == "closed" {
				updates["closed_at"] = &now
			} else {
				updates["closed_at"] = nil
			}
		}
		if err := tx.Model(&ticket).Updates(updates).Error; err != nil {
			return err
		}
		if req.Internal {
			return nil
		}
		delivery, err := h.createTicketNotification(tx, "email", ticket.Email, "工单回复 "+ticket.TicketNo, "客服已回复您的工单："+ticket.Subject+"。请登录 LinLinQi 账户中心查看。", "ticket-admin-reply:"+message.ID.String())
		if err != nil {
			return err
		}
		deliveryID = delivery.ID
		return nil
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		response.Error(c, 404, 40441, "error.ticket_not_found")
		return
	}
	if errors.Is(err, errInvalidTicketTransition) {
		response.Error(c, 409, 40942, "error.ticket_status_transition_invalid")
		return
	}
	if err != nil {
		response.Error(c, 500, 50041, "error.ticket_reply_send_failed")
		return
	}
	h.audit(c, map[bool]string{true: "ticket.note", false: "ticket.reply"}[req.Internal], "ticket", ticketID.String(), "")
	h.enqueueNotification(deliveryID)
	response.Created(c, message)
}

type updateTicketRequest struct {
	Status     *string    `json:"status"`
	Priority   *string    `json:"priority"`
	AssignedTo *uuid.UUID `json:"assigned_to"`
}

func (h Handler) UpdateTicket(c *gin.Context) {
	ticketID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, 422, 42246, "error.ticket_number_invalid")
		return
	}
	var req updateTicketRequest
	if decodeStrictJSON(c, &req) != nil || (req.Status == nil && req.Priority == nil && req.AssignedTo == nil) {
		response.Error(c, 422, 42248, "error.ticket_update_invalid")
		return
	}
	reason := strings.TrimSpace(c.GetHeader("X-Change-Reason"))
	if reason == "" {
		response.Error(c, 422, 42257, "error.change_reason_required")
		return
	}
	err = h.DB.Transaction(func(tx *gorm.DB) error {
		var ticket model.SupportTicket
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&ticket, "id = ?", ticketID).Error; err != nil {
			return err
		}
		updates := map[string]any{}
		if req.Status != nil {
			target := strings.TrimSpace(*req.Status)
			if !validTicketTransition(ticket.Status, target) {
				return errInvalidTicketTransition
			}
			updates["status"] = target
			if target == "closed" {
				now := time.Now()
				updates["closed_at"] = &now
			} else {
				updates["closed_at"] = nil
			}
		}
		if req.Priority != nil {
			priority := strings.TrimSpace(*req.Priority)
			if priority != "low" && priority != "normal" && priority != "high" && priority != "urgent" {
				return errInvalidTicketPriority
			}
			updates["priority"] = priority
		}
		if req.AssignedTo != nil {
			var admin model.Admin
			if err := tx.Select("id").Where("id = ? AND status = ?", *req.AssignedTo, "active").First(&admin).Error; err != nil {
				return errInvalidTicketAssignee
			}
			updates["assigned_to"] = *req.AssignedTo
		}
		return tx.Model(&ticket).Updates(updates).Error
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		response.Error(c, 404, 40441, "error.ticket_not_found")
		return
	}
	if errors.Is(err, errInvalidTicketTransition) || errors.Is(err, errInvalidTicketPriority) || errors.Is(err, errInvalidTicketAssignee) {
		response.Error(c, 422, 42248, "error.ticket_assignment_invalid")
		return
	}
	if err != nil {
		response.Error(c, 500, 50041, "error.ticket_update_failed")
		return
	}
	h.audit(c, "ticket.update", "ticket", ticketID.String(), reason)
	var ticket model.SupportTicket
	h.DB.First(&ticket, "id = ?", ticketID)
	response.OK(c, ticket)
}

var (
	errInvalidTicketTransition = errors.New("invalid ticket transition")
	errInvalidTicketPriority   = errors.New("invalid ticket priority")
	errInvalidTicketAssignee   = errors.New("invalid ticket assignee")
)

func validTicketTransition(from, to string) bool {
	if from == to {
		return true
	}
	allowed := map[string]map[string]bool{
		"open":         {"in_progress": true, "waiting_user": true, "resolved": true, "closed": true},
		"in_progress":  {"open": true, "waiting_user": true, "resolved": true, "closed": true},
		"waiting_user": {"open": true, "in_progress": true, "resolved": true, "closed": true},
		"resolved":     {"open": true, "in_progress": true, "closed": true},
		"closed":       {"open": true},
	}
	return allowed[from][to]
}
