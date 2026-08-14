package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"linlinqi/api/internal/model"
	"linlinqi/api/pkg/response"
)

const maxAdminBatchItems = 100

type adminBatchIDsRequest struct {
	IDs []uuid.UUID `json:"ids"`
}

type adminBatchStatusRequest struct {
	IDs    []uuid.UUID `json:"ids"`
	Status string      `json:"status"`
}

func normalizedAdminBatchIDs(values []uuid.UUID, limit int) ([]uuid.UUID, bool) {
	if len(values) == 0 || len(values) > limit {
		return nil, false
	}
	seen := make(map[uuid.UUID]struct{}, len(values))
	result := make([]uuid.UUID, 0, len(values))
	for _, value := range values {
		if value == uuid.Nil {
			return nil, false
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].String() < result[j].String() })
	return result, len(result) > 0
}

func (h Handler) BatchUpdateAdminCustomerStatus(c *gin.Context) {
	var req adminBatchStatusRequest
	if decodeStrictJSON(c, &req) != nil {
		response.Error(c, 422, 42400, "error.customer_batch_request_invalid")
		return
	}
	ids, valid := normalizedAdminBatchIDs(req.IDs, maxAdminBatchItems)
	req.Status = strings.ToLower(strings.TrimSpace(req.Status))
	if !valid || (req.Status != "active" && req.Status != "disabled") {
		response.Error(c, 422, 42401, "error.customer_batch_status_invalid")
		return
	}
	reason, ok := requireAdminChangeReason(c, "批量变更客户状态")
	if !ok {
		return
	}
	adminID, _ := uuid.Parse(c.GetString("subject"))
	now := time.Now().UTC()
	changed := 0
	err := h.DB.Transaction(func(tx *gorm.DB) error {
		var users []model.User
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id IN ?", ids).Order("id").Find(&users).Error; err != nil {
			return err
		}
		if len(users) != len(ids) {
			return gorm.ErrRecordNotFound
		}
		for index := range users {
			user := &users[index]
			if user.Status == req.Status {
				continue
			}
			if err := tx.Model(user).Update("status", req.Status).Error; err != nil {
				return err
			}
			if req.Status == "disabled" {
				if err := tx.Model(&model.UserSession{}).Where("user_id = ? AND revoked_at IS NULL", user.ID).Update("revoked_at", &now).Error; err != nil {
					return err
				}
				if err := tx.Model(&model.UserSessionToken{}).Where("user_id = ? AND revoked_at IS NULL", user.ID).Update("revoked_at", &now).Error; err != nil {
					return err
				}
			}
			details, _ := json.Marshal(gin.H{"status": req.Status, "reason": reason, "batch": true})
			if err := tx.Create(&model.SecurityEvent{
				EventType: "customer.status_changed", Severity: "warning", Realm: "user", PrincipalID: &user.ID,
				Details: string(details), Resolved: true, ResolvedBy: &adminID, ResolvedAt: &now,
			}).Error; err != nil {
				return err
			}
			changed++
		}
		return nil
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		response.Error(c, 404, 40442, "error.customer_batch_contains_missing_record")
		return
	}
	if err != nil {
		response.Error(c, 500, 50042, "error.customer_batch_status_failed")
		return
	}
	h.audit(c, "customer.status.batch_update", "user", "batch", fmt.Sprintf("%s；status=%s；selected=%d；changed=%d", reason, req.Status, len(ids), changed))
	response.OK(c, gin.H{"selected": len(ids), "changed": changed, "status": req.Status, "sessions_revoked": req.Status == "disabled"})
}

func (h Handler) BatchUpdateInventoryCardStatus(c *gin.Context) {
	var req adminBatchStatusRequest
	if decodeStrictJSON(c, &req) != nil {
		response.Error(c, 422, 42410, "error.card_batch_request_invalid")
		return
	}
	ids, valid := normalizedAdminBatchIDs(req.IDs, maxAdminBatchItems)
	req.Status = strings.ToLower(strings.TrimSpace(req.Status))
	if !valid || (req.Status != "available" && req.Status != "disabled") {
		response.Error(c, 422, 42411, "error.card_batch_status_invalid")
		return
	}
	reason, ok := requireAdminChangeReason(c, "批量变更卡密状态")
	if !ok {
		return
	}
	err := h.DB.Transaction(func(tx *gorm.DB) error {
		var cards []model.Card
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id IN ?", ids).Order("id").Find(&cards).Error; err != nil {
			return err
		}
		if len(cards) != len(ids) {
			return gorm.ErrRecordNotFound
		}
		for index := range cards {
			card := &cards[index]
			if !validInventoryCardTransition(card.Status, req.Status) || card.OrderID != nil {
				return errInventoryCardState
			}
			if req.Status == "available" {
				var product model.Product
				if err := tx.Select("id", "inventory_mode").Where("id = ? AND inventory_mode = ?", card.ProductID, "local").First(&product).Error; err != nil {
					return errInventoryProduct
				}
				if card.VariantID != nil {
					var variant model.ProductVariant
					if err := tx.Select("id").Where("id = ? AND product_id = ? AND status = ?", *card.VariantID, card.ProductID, "active").First(&variant).Error; err != nil {
						return errInventoryProduct
					}
				}
			}
			if err := tx.Model(card).Update("status", req.Status).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		response.Error(c, 404, 40492, "error.card_batch_contains_missing_record")
		return
	}
	if errors.Is(err, errInventoryCardState) || errors.Is(err, errInventoryProduct) {
		response.Error(c, 409, 40998, "error.card_batch_contains_ineligible_record")
		return
	}
	if err != nil {
		response.Error(c, 500, 50096, "error.card_batch_status_failed")
		return
	}
	h.audit(c, "card.status.batch_update", "card", "batch", fmt.Sprintf("%s；status=%s；count=%d", reason, req.Status, len(ids)))
	response.OK(c, gin.H{"changed": len(ids), "status": req.Status})
}

func (h Handler) BatchReplayAdminJobs(c *gin.Context) {
	var req adminBatchIDsRequest
	if decodeStrictJSON(c, &req) != nil {
		response.Error(c, 422, 42420, "error.job_batch_request_invalid")
		return
	}
	ids, valid := normalizedAdminBatchIDs(req.IDs, 20)
	if !valid {
		response.Error(c, 422, 42421, "error.job_batch_selection_invalid")
		return
	}
	reason, ok := requireAdminChangeReason(c, "批量重试失败任务")
	if !ok {
		return
	}
	type replayFailure struct {
		ID    uuid.UUID `json:"id"`
		Error string    `json:"error"`
	}
	succeeded := make([]uuid.UUID, 0, len(ids))
	failed := make([]replayFailure, 0)
	for _, id := range ids {
		original, newTaskID, err := h.replayAdminJob(id)
		if err != nil {
			failed = append(failed, replayFailure{ID: id, Error: "not_retryable"})
			continue
		}
		succeeded = append(succeeded, id)
		h.audit(c, "job.replay", "job_record", original.ID.String(), fmt.Sprintf("%s；batch=true；old_task=%s；new_task=%s", reason, original.TaskID, newTaskID))
	}
	response.OK(c, gin.H{"selected": len(ids), "succeeded": succeeded, "failed": failed})
}
