package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"linlinqi/api/internal/model"
	"linlinqi/api/internal/queue"
	"linlinqi/api/internal/service"
	"linlinqi/api/pkg/response"
)

type adminJobDTO struct {
	ID          uuid.UUID  `json:"id"`
	TaskID      string     `json:"task_id"`
	TaskType    string     `json:"task_type"`
	Queue       string     `json:"queue"`
	Status      string     `json:"status"`
	Attempts    int        `json:"attempts"`
	Payload     string     `json:"payload"`
	LastError   string     `json:"last_error"`
	ScheduledAt *time.Time `json:"scheduled_at"`
	FinishedAt  *time.Time `json:"finished_at"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	Stale       bool       `json:"stale"`
	Retryable   bool       `json:"retryable"`
}

type adminJobCount struct {
	Name  string `json:"name"`
	Count int64  `json:"count"`
}

var retryableAdminJobTypes = map[string]string{
	queue.TypeOrderExpire:           "",
	queue.TypeNotificationDispatch:  "delivery_id",
	queue.TypeWebhookDeliver:        "delivery_id",
	queue.TypeSupplierSync:          "supplier_id",
	queue.TypeSupplierCatalogImport: "job_id",
	queue.TypeReconciliationRun:     "batch_id",
	queue.TypeRefundProcess:         "refund_id",
	queue.TypeRechargeRefundProcess: "recharge_transaction_id",
	queue.TypeSupplierPurchase:      "order_id",
	queue.TypeFXRefresh:             "",
}

func toAdminJobDTO(job model.JobRecord, now time.Time) adminJobDTO {
	_, supported := retryableAdminJobTypes[job.TaskType]
	// Import jobs must be retried through their domain endpoint so the durable
	// status and task fencing token are reset together.
	if job.TaskType == queue.TypeSupplierCatalogImport {
		supported = false
	}
	return adminJobDTO{
		ID: job.ID, TaskID: job.TaskID, TaskType: job.TaskType, Queue: job.Queue, Status: job.Status,
		Attempts: job.Attempts, Payload: job.Payload, LastError: job.LastError, ScheduledAt: job.ScheduledAt,
		FinishedAt: job.FinishedAt, CreatedAt: job.CreatedAt, UpdatedAt: job.UpdatedAt,
		Stale:     job.Status == "running" && job.UpdatedAt.Before(now.Add(-2*time.Minute)),
		Retryable: job.Status == "failed" && supported,
	}
}

func applyAdminJobFilters(query *gorm.DB, c *gin.Context) (*gorm.DB, error) {
	if keyword := strings.TrimSpace(c.Query("q")); keyword != "" {
		like := "%" + strings.ToLower(keyword) + "%"
		query = query.Where("LOWER(task_id) LIKE ? OR LOWER(task_type) LIKE ? OR LOWER(last_error) LIKE ?", like, like, like)
	}
	if status := strings.TrimSpace(c.Query("status")); status != "" {
		allowed := map[string]bool{"queued": true, "running": true, "retrying": true, "succeeded": true, "failed": true, "cancelled": true, "replayed": true}
		if !allowed[status] {
			return nil, errors.New("invalid job status")
		}
		query = query.Where("status = ?", status)
	}
	if queueName := strings.TrimSpace(c.Query("queue")); queueName != "" {
		if queueName != "critical" && queueName != "default" && queueName != "low" {
			return nil, errors.New("invalid queue")
		}
		query = query.Where("queue = ?", queueName)
	}
	if taskType := strings.TrimSpace(c.Query("task_type")); taskType != "" {
		if _, ok := retryableAdminJobTypes[taskType]; !ok {
			return nil, errors.New("invalid task type")
		}
		query = query.Where("task_type = ?", taskType)
	}
	for _, boundary := range []struct {
		name       string
		comparison string
	}{
		{name: "date_from", comparison: ">="},
		{name: "date_to", comparison: "<"},
	} {
		raw := strings.TrimSpace(c.Query(boundary.name))
		if raw == "" {
			continue
		}
		parsed, err := time.Parse("2006-01-02", raw)
		if err != nil {
			return nil, err
		}
		if boundary.name == "date_to" {
			parsed = parsed.AddDate(0, 0, 1)
		}
		query = query.Where("created_at "+boundary.comparison+" ?", parsed.UTC())
	}
	return query, nil
}

func (h Handler) AdminJobs(c *gin.Context) {
	page, pageSize := pagination(c)
	query, err := applyAdminJobFilters(h.DB.Model(&model.JobRecord{}), c)
	if err != nil {
		response.Error(c, 422, 42298, "error.task_filter_invalid")
		return
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		response.Error(c, 500, 50098, "error.job_count_fetch_failed")
		return
	}
	var jobs []model.JobRecord
	if err := query.Order("created_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&jobs).Error; err != nil {
		response.Error(c, 500, 50098, "error.job_list_fetch_failed")
		return
	}
	now := time.Now()
	items := make([]adminJobDTO, 0, len(jobs))
	for _, job := range jobs {
		items = append(items, toAdminJobDTO(job, now))
	}
	response.Page(c, items, total, page, pageSize)
}

func (h Handler) AdminJobsSummary(c *gin.Context) {
	var statusCounts, queueCounts, typeCounts []adminJobCount
	if err := h.DB.Model(&model.JobRecord{}).Select("status AS name, COUNT(*) AS count").Group("status").Order("status").Scan(&statusCounts).Error; err != nil {
		response.Error(c, 500, 50098, "error.job_summary_fetch_failed")
		return
	}
	if err := h.DB.Model(&model.JobRecord{}).Select("queue AS name, COUNT(*) AS count").Where("created_at >= ?", time.Now().Add(-24*time.Hour)).Group("queue").Order("queue").Scan(&queueCounts).Error; err != nil {
		response.Error(c, 500, 50098, "error.queue_summary_fetch_failed")
		return
	}
	if err := h.DB.Model(&model.JobRecord{}).Select("task_type AS name, COUNT(*) AS count").Where("created_at >= ?", time.Now().Add(-24*time.Hour)).Group("task_type").Order("count DESC").Limit(10).Scan(&typeCounts).Error; err != nil {
		response.Error(c, 500, 50098, "error.job_type_summary_fetch_failed")
		return
	}
	var last24Hours, failed24Hours, staleRunning int64
	now := time.Now()
	if err := h.DB.Model(&model.JobRecord{}).Where("created_at >= ?", now.Add(-24*time.Hour)).Count(&last24Hours).Error; err != nil {
		response.Error(c, 500, 50098, "error.job_summary_fetch_failed")
		return
	}
	if err := h.DB.Model(&model.JobRecord{}).Where("created_at >= ? AND status = ?", now.Add(-24*time.Hour), "failed").Count(&failed24Hours).Error; err != nil {
		response.Error(c, 500, 50098, "error.job_failure_summary_fetch_failed")
		return
	}
	if err := h.DB.Model(&model.JobRecord{}).Where("status = ? AND updated_at < ?", "running", now.Add(-2*time.Minute)).Count(&staleRunning).Error; err != nil {
		response.Error(c, 500, 50098, "error.stuck_job_summary_fetch_failed")
		return
	}
	response.OK(c, gin.H{
		"status_counts": statusCounts, "queue_counts_24h": queueCounts, "type_counts_24h": typeCounts,
		"last_24h": last24Hours, "failed_24h": failed24Hours, "stale_running": staleRunning,
	})
}

func decodeAdminJobPayload(job model.JobRecord) (map[string]string, error) {
	var payload map[string]string
	if err := json.Unmarshal([]byte(job.Payload), &payload); err != nil {
		return nil, err
	}
	required, ok := retryableAdminJobTypes[job.TaskType]
	if !ok {
		return nil, errors.New("unsupported task type")
	}
	if required != "" {
		value, exists := payload[required]
		if !exists {
			return nil, errors.New("required task reference is missing")
		}
		if _, err := uuid.Parse(value); err != nil {
			return nil, errors.New("task reference is invalid")
		}
	}
	return payload, nil
}

func prepareAdminJobReplay(tx *gorm.DB, taskType string, payload map[string]string, now time.Time) error {
	switch taskType {
	case queue.TypeOrderExpire, queue.TypeFXRefresh:
		return nil
	case queue.TypeSupplierSync:
		return tx.Select("id").First(&model.Supplier{}, "id = ? AND status = ?", payload["supplier_id"], "active").Error
	case queue.TypeSupplierPurchase:
		return tx.Select("id").First(&model.Order{}, "id = ? AND status = ? AND payment_status = ?", payload["order_id"], "processing", "paid").Error
	case queue.TypeReconciliationRun:
		var batch model.ReconciliationBatch
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&batch, "id = ?", payload["batch_id"]).Error; err != nil {
			return err
		}
		if batch.Status == "completed" {
			return errors.New("completed reconciliation cannot be replayed")
		}
		return tx.Model(&batch).Updates(map[string]any{"status": "pending", "completed_at": nil}).Error
	case queue.TypeNotificationDispatch:
		var delivery model.NotificationDelivery
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&delivery, "id = ?", payload["delivery_id"]).Error; err != nil {
			return err
		}
		if delivery.Status != "failed" {
			return errors.New("notification is not in terminal failure")
		}
		return tx.Model(&delivery).Updates(map[string]any{"status": "queued", "attempts": 0, "last_error": "", "next_attempt_at": &now, "sent_at": nil}).Error
	case queue.TypeWebhookDeliver:
		var delivery model.WebhookDelivery
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&delivery, "id = ?", payload["delivery_id"]).Error; err != nil {
			return err
		}
		var endpoint model.WebhookEndpoint
		if err := tx.Select("id").First(&endpoint, "id = ? AND enabled = ?", delivery.EndpointID, true).Error; err != nil {
			return errors.New("webhook endpoint is disabled")
		}
		if delivery.Status != "failed" {
			return errors.New("webhook is not in terminal failure")
		}
		return tx.Model(&delivery).Updates(map[string]any{"status": "queued", "attempts": 0, "response_code": 0, "response_body": "", "next_attempt_at": &now, "delivered_at": nil}).Error
	case queue.TypeRefundProcess:
		var snapshot model.Refund
		if err := tx.Select("id", "order_id", "payment_intent_id").First(&snapshot, "id = ?", payload["refund_id"]).Error; err != nil {
			return err
		}
		var order model.Order
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&order, "id = ?", snapshot.OrderID).Error; err != nil {
			return err
		}
		if snapshot.PaymentIntentID == nil {
			return errors.New("refund payment intent is missing")
		}
		var intent model.PaymentIntent
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&intent, "id = ?", *snapshot.PaymentIntentID).Error; err != nil {
			return err
		}
		var refund model.Refund
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&refund, "id = ?", snapshot.ID).Error; err != nil {
			return err
		}
		if refund.Status != "failed" || refund.PaymentIntentID == nil || *refund.PaymentIntentID != intent.ID || intent.OrderID != order.ID || intent.Status == "refunded" {
			return errors.New("refund is not in terminal failure")
		}
		automatic := refund.RequestedBy == "system" && intent.Status == "requires_refund"
		if automatic {
			if order.Status == "processing" || order.Status == "delivered" || order.Status == "completed" || order.PaymentStatus == "refunded" {
				return errors.New("automatic refund cannot be replayed after fulfillment")
			}
		} else if !refundableFulfillmentStatus(order.Status) || (order.PaymentStatus != "paid" && order.PaymentStatus != "partially_refunded") {
			return errors.New("refund cannot be replayed while fulfillment is active")
		}
		var committedOrder, committedPayment int64
		providerCapacity, err := service.RefundProviderCapacityTx(tx, intent, refund.RequestedBy, refund.Currency)
		if err != nil {
			return err
		}
		activeStatuses := []string{"pending", "processing", "retrying", "succeeded"}
		if err := tx.Model(&model.Refund{}).Where("order_id = ? AND id <> ? AND status IN ?", order.ID, refund.ID, activeStatuses).Select("COALESCE(SUM(order_amount), 0)").Scan(&committedOrder).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.Refund{}).Where("payment_intent_id = ? AND id <> ? AND status IN ?", intent.ID, refund.ID, activeStatuses).Select("COALESCE(SUM(amount), 0)").Scan(&committedPayment).Error; err != nil {
			return err
		}
		if refund.OrderAmount < 1 || refund.Amount < 1 || committedOrder > order.Total-refund.OrderAmount || refund.Amount > providerCapacity || committedPayment > providerCapacity-refund.Amount {
			return errors.New("refund replay would exceed the remaining paid amount")
		}
		return tx.Model(&refund).Updates(map[string]any{"status": "retrying", "attempts": 0, "next_attempt_at": &now}).Error
	case queue.TypeRechargeRefundProcess:
		var snapshot model.RechargeTransaction
		if err := tx.Select("id", "recharge_order_id").First(&snapshot, "id = ?", payload["recharge_transaction_id"]).Error; err != nil {
			return err
		}
		var recharge model.RechargeOrder
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&recharge, "id = ?", snapshot.RechargeOrderID).Error; err != nil {
			return err
		}
		var transaction model.RechargeTransaction
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&transaction, "id = ?", snapshot.ID).Error; err != nil {
			return err
		}
		if transaction.Disposition != "refund_failed" || transaction.RefundNo == "" || transaction.ProviderTradeNo == "" || transaction.Amount < 1 {
			return errors.New("recharge refund is not in terminal failure")
		}
		if recharge.Status != "succeeded" {
			if err := tx.Model(&recharge).Update("status", "requires_refund").Error; err != nil {
				return err
			}
		}
		return tx.Model(&transaction).Updates(map[string]any{
			"disposition": "refund_retrying", "refund_attempts": 0,
			"refund_next_attempt_at": &now, "refund_last_error": "",
		}).Error
	default:
		return errors.New("unsupported task type")
	}
}

func (h Handler) ReplayAdminJob(c *gin.Context) {
	jobID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, 422, 42299, "error.task_id_invalid")
		return
	}
	reason, ok := requireAdminChangeReason(c, "重试后台任务")
	if !ok {
		return
	}
	original, newTaskID, err := h.replayAdminJob(jobID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		response.Error(c, 404, 40498, "error.task_or_related_record_not_found")
		return
	}
	if err != nil {
		response.Error(c, 409, 40998, "error.task_not_retryable")
		return
	}
	h.audit(c, "job.replay", "job_record", original.ID.String(), fmt.Sprintf("%s；old_task=%s；new_task=%s", reason, original.TaskID, newTaskID))
	response.Created(c, gin.H{"task_id": newTaskID, "replayed_from": original.TaskID, "scheduled_in_seconds": 2})
}

func (h Handler) replayAdminJob(jobID uuid.UUID) (model.JobRecord, string, error) {
	var original model.JobRecord
	var newTaskID string
	now := time.Now().UTC()
	err := h.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&original, "id = ?", jobID).Error; err != nil {
			return err
		}
		if original.Status != "failed" {
			return errors.New("only terminal failed jobs can be replayed")
		}
		payload, err := decodeAdminJobPayload(original)
		if err != nil {
			return err
		}
		if err := prepareAdminJobReplay(tx, original.TaskType, payload, now); err != nil {
			return err
		}
		payload["replay_of"] = original.TaskID
		client := queue.NewClient(h.Cfg, tx)
		defer client.Close()
		info, err := client.Enqueue(original.TaskType, payload, asynq.Queue(original.Queue), asynq.ProcessIn(2*time.Second))
		if err != nil {
			return err
		}
		newTaskID = info.ID
		return tx.Model(&original).Updates(map[string]any{"status": "replayed", "last_error": "replayed as " + info.ID, "finished_at": &now}).Error
	})
	return original, newTaskID, err
}
