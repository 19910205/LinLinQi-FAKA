package queue

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"linlinqi/api/internal/config"
	"linlinqi/api/internal/currency"
	"linlinqi/api/internal/model"
	"linlinqi/api/internal/payment"
	"linlinqi/api/internal/security"
	"linlinqi/api/internal/service"
	"linlinqi/api/internal/supply"
)

const (
	TypeOrderExpire           = "linlinqi:order:expire"
	TypeNotificationDispatch  = "linlinqi:notification:dispatch"
	TypeWebhookDeliver        = "linlinqi:webhook:deliver"
	TypeSupplierSync          = "linlinqi:supplier:sync"
	TypeSupplierCatalogImport = "linlinqi:supplier:catalog-import"
	TypeReconciliationRun     = "linlinqi:reconciliation:run"
	TypeRefundProcess         = "linlinqi:refund:process"
	TypeRechargeRefundProcess = "linlinqi:recharge-refund:process"
	TypeSupplierPurchase      = "linlinqi:supplier:purchase"
	TypeFXRefresh             = "linlinqi:fx:refresh"
)

type Client struct {
	raw *asynq.Client
	db  *gorm.DB
}

const (
	supplierSyncTaskTimeout          = 15 * time.Minute
	supplierCatalogImportTaskTimeout = 30 * time.Minute
	supplierCatalogImportMaxAttempts = 6
)

var (
	ErrSupplierCatalogImportClaimLost     = errors.New("supplier catalog import claim lost")
	errSupplierCatalogImportAlreadyQueued = errors.New("supplier catalog import is already queued")
)

func NewClient(cfg config.Config, databases ...*gorm.DB) *Client {
	client := &Client{raw: asynq.NewClient(asynq.RedisClientOpt{Addr: cfg.RedisAddr, Password: cfg.RedisPassword, DB: cfg.RedisDB})}
	if len(databases) > 0 {
		client.db = databases[0]
	}
	return client
}
func (c *Client) Close() error { return c.raw.Close() }
func (c *Client) Enqueue(taskType string, payload any, options ...asynq.Option) (*asynq.TaskInfo, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	defaults := taskDefaultOptions(taskType)
	queueName := "default"
	taskID := uuid.NewString()
	scheduledAt := time.Now().UTC()
	for _, option := range options {
		switch option.Type() {
		case asynq.QueueOpt:
			if value, ok := option.Value().(string); ok {
				queueName = value
			}
		case asynq.TaskIDOpt:
			if value, ok := option.Value().(string); ok && strings.TrimSpace(value) != "" {
				taskID = value
			}
		case asynq.ProcessAtOpt:
			if value, ok := option.Value().(time.Time); ok {
				scheduledAt = value.UTC()
			}
		case asynq.ProcessInOpt:
			if value, ok := option.Value().(time.Duration); ok {
				scheduledAt = time.Now().UTC().Add(value)
			}
		}
	}
	job := model.JobRecord{TaskID: taskID, TaskType: taskType, Queue: queueName, Status: "queued", Payload: safeJobPayload(body), ScheduledAt: &scheduledAt}
	if c.db != nil {
		if err := c.db.Create(&job).Error; err != nil {
			return nil, fmt.Errorf("record queued task: %w", err)
		}
	}
	info, err := c.raw.Enqueue(asynq.NewTask(taskType, body), append(append(defaults, options...), asynq.TaskID(taskID))...)
	if err != nil {
		if c.db != nil {
			status, message := "failed", safeJobError(err.Error())
			if errors.Is(err, asynq.ErrDuplicateTask) {
				status, message = "cancelled", "duplicate task suppressed"
			}
			now := time.Now().UTC()
			_ = c.db.Model(&job).Updates(map[string]any{"status": status, "last_error": message, "finished_at": &now}).Error
		}
		return nil, err
	}
	if c.db != nil {
		updates := map[string]any{"queue": info.Queue, "status": "queued"}
		if !info.NextProcessAt.IsZero() {
			next := info.NextProcessAt.UTC()
			updates["scheduled_at"] = &next
		}
		_ = c.db.Model(&job).Updates(updates).Error
	}
	return info, nil
}

func taskDefaultOptions(taskType string) []asynq.Option {
	maximumRetries, timeout := taskExecutionPolicy(taskType)
	return []asynq.Option{asynq.MaxRetry(maximumRetries), asynq.Timeout(timeout), asynq.Retention(24 * time.Hour)}
}

func taskExecutionPolicy(taskType string) (maximumRetries int, timeout time.Duration) {
	maximumRetries = 12
	timeout = 45 * time.Second
	switch taskType {
	case TypeSupplierSync:
		timeout = supplierSyncTaskTimeout
	case TypeSupplierCatalogImport:
		// Domain retries rotate the fencing token through the durable job row;
		// an Asynq retry would reuse the old token and can overlap a timed-out
		// handler. Keep each queue task single-attempt.
		maximumRetries = 0
		timeout = supplierCatalogImportTaskTimeout
	}
	return maximumRetries, timeout
}

// EnqueueSupplierCatalogImport reserves a fencing token in PostgreSQL before
// Redis can expose the task to a worker. Every worker-side write verifies this
// token; recovery may replace it without allowing an older task to commit.
func (c *Client) EnqueueSupplierCatalogImport(jobID uuid.UUID) (*asynq.TaskInfo, error) {
	if jobID == uuid.Nil {
		return nil, errors.New("supplier catalog import job id is required")
	}
	if c.db == nil {
		return nil, errors.New("supplier catalog import queue requires a database")
	}
	taskID := "supplier-catalog-import-" + uuid.NewString()
	reservedUntil := time.Now().UTC().Add(5 * time.Minute)
	err := c.db.Transaction(func(tx *gorm.DB) error {
		var job model.SupplierCatalogImportJob
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&job, "id = ?", jobID).Error; err != nil {
			return err
		}
		if job.Status != "queued" && job.Status != "retrying" {
			return errSupplierCatalogImportAlreadyQueued
		}
		if strings.TrimSpace(job.TaskID) != "" {
			return errSupplierCatalogImportAlreadyQueued
		}
		return tx.Model(&job).Updates(map[string]any{
			"status": "queued", "task_id": taskID, "next_attempt_at": &reservedUntil,
			"error_summary": "",
		}).Error
	})
	if err != nil {
		return nil, err
	}
	info, err := c.Enqueue(
		TypeSupplierCatalogImport,
		map[string]string{"job_id": jobID.String(), "task_id": taskID},
		asynq.Queue("default"),
		asynq.TaskID(taskID),
	)
	if err != nil {
		next := time.Now().UTC().Add(30 * time.Second)
		_ = c.db.Model(&model.SupplierCatalogImportJob{}).
			Where("id = ? AND task_id = ? AND status NOT IN ?", jobID, taskID, []string{"succeeded", "cancelled"}).
			Updates(map[string]any{
				"status": "retrying", "task_id": "", "next_attempt_at": &next,
				"error_summary": safeJobError("queue unavailable: " + err.Error()),
			}).Error
		return nil, err
	}
	// Keep the reservation deadline until the worker successfully claims this
	// exact token. If the worker process dies before claim (or rejects a broken
	// payload), the recovery scanner can rotate the expired token.
	return info, nil
}

func safeJobPayload(payload []byte) string {
	var decoded any
	if json.Unmarshal(payload, &decoded) != nil {
		return `{}`
	}
	redactJobValue(decoded)
	encoded, err := json.Marshal(decoded)
	if err != nil || len(encoded) > 16<<10 {
		return `{}`
	}
	return string(encoded)
}

func redactJobValue(value any) {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			lower := strings.ToLower(key)
			if strings.Contains(lower, "secret") || strings.Contains(lower, "password") || strings.Contains(lower, "token") || strings.Contains(lower, "card_content") || strings.Contains(lower, "cipher") {
				typed[key] = "[REDACTED]"
				continue
			}
			redactJobValue(child)
		}
	case []any:
		for _, child := range typed {
			redactJobValue(child)
		}
	}
}

func safeJobError(message string) string {
	message = strings.Map(func(character rune) rune {
		if unicode.IsControl(character) && character != '\n' && character != '\t' {
			return -1
		}
		return character
	}, strings.TrimSpace(message))
	return truncate(message, 2000)
}

type Worker struct {
	server                         *asynq.Server
	mux                            *asynq.ServeMux
	cfg                            config.Config
	db                             *gorm.DB
	vault                          *security.Vault
	http                           *http.Client
	client                         *Client
	stop                           chan struct{}
	stopOnce                       sync.Once
	supplierCatalogImportProcessor SupplierCatalogImportProcessor
}

// SupplierCatalogImportProcessor is injected by the HTTP/domain layer so the
// queue package does not depend on Gin handlers. progress is safe to call
// after each selected product and updates the operator-visible job record.
type SupplierCatalogImportProcessor func(context.Context, uuid.UUID, string, func(imported, skipped int)) error

func NewWorker(cfg config.Config, db *gorm.DB, vault *security.Vault, processors ...SupplierCatalogImportProcessor) *Worker {
	server := asynq.NewServer(asynq.RedisClientOpt{Addr: cfg.RedisAddr, Password: cfg.RedisPassword, DB: cfg.RedisDB}, asynq.Config{
		Concurrency:    cfg.WorkerConcurrency,
		Queues:         map[string]int{"critical": 8, "default": 5, "low": 1},
		RetryDelayFunc: func(n int, _ error, _ *asynq.Task) time.Duration { return time.Duration(1<<min(n, 8)) * time.Second },
	})
	worker := &Worker{server: server, mux: asynq.NewServeMux(), cfg: cfg, db: db, vault: vault, http: security.NewOutboundHTTPClient(15*time.Second, cfg.Env != "production"), client: NewClient(cfg, db), stop: make(chan struct{})}
	if len(processors) > 0 {
		worker.supplierCatalogImportProcessor = processors[0]
	}
	worker.mux.Use(worker.trackJob)
	worker.mux.HandleFunc(TypeOrderExpire, worker.handleOrderExpire)
	worker.mux.HandleFunc(TypeNotificationDispatch, worker.handleNotification)
	worker.mux.HandleFunc(TypeWebhookDeliver, worker.handleWebhook)
	worker.mux.HandleFunc(TypeSupplierSync, worker.handleSupplierSync)
	worker.mux.HandleFunc(TypeSupplierCatalogImport, worker.handleSupplierCatalogImport)
	worker.mux.HandleFunc(TypeFXRefresh, worker.handleFXRefresh)
	worker.mux.HandleFunc(TypeReconciliationRun, worker.handleReconciliation)
	worker.mux.HandleFunc(TypeRefundProcess, worker.handleRefund)
	worker.mux.HandleFunc(TypeRechargeRefundProcess, worker.handleRechargeRefund)
	worker.mux.HandleFunc(TypeSupplierPurchase, worker.handleSupplierPurchase)
	return worker
}

func (w *Worker) trackJob(next asynq.Handler) asynq.Handler {
	return asynq.HandlerFunc(func(ctx context.Context, task *asynq.Task) error {
		taskID, ok := asynq.GetTaskID(ctx)
		if !ok || strings.TrimSpace(taskID) == "" {
			return next.ProcessTask(ctx, task)
		}
		queueName, _ := asynq.GetQueueName(ctx)
		retryCount, _ := asynq.GetRetryCount(ctx)
		maxRetry, _ := asynq.GetMaxRetry(ctx)
		job := model.JobRecord{TaskID: taskID, TaskType: task.Type(), Queue: defaultJobQueue(queueName), Status: "running", Attempts: retryCount + 1, Payload: safeJobPayload(task.Payload())}
		_ = w.db.Where("task_id = ?", taskID).Assign(map[string]any{"task_type": task.Type(), "queue": defaultJobQueue(queueName), "status": "running", "attempts": retryCount + 1}).FirstOrCreate(&job).Error
		err := next.ProcessTask(ctx, task)
		now := time.Now().UTC()
		if err == nil {
			_ = w.db.Model(&model.JobRecord{}).Where("task_id = ?", taskID).Updates(map[string]any{"status": "succeeded", "last_error": "", "finished_at": &now}).Error
			return nil
		}
		status := "retrying"
		updates := map[string]any{"status": status, "last_error": safeJobError(err.Error())}
		if errors.Is(err, asynq.SkipRetry) || retryCount >= maxRetry {
			updates["status"] = "failed"
			updates["finished_at"] = &now
		}
		_ = w.db.Model(&model.JobRecord{}).Where("task_id = ?", taskID).Updates(updates).Error
		return err
	})
}

func defaultJobQueue(queueName string) string {
	if strings.TrimSpace(queueName) == "" {
		return "default"
	}
	return queueName
}

func (w *Worker) handleOrderExpire(_ context.Context, _ *asynq.Task) error {
	timeoutMinutes := 15
	var setting model.Setting
	if w.db.First(&setting, "key = ?", "order_timeout_minutes").Error == nil {
		if parsed, parseErr := strconv.Atoi(setting.Value); parseErr == nil && parsed >= 5 && parsed <= 1440 {
			timeoutMinutes = parsed
		}
	}
	count, err := service.ExpirePendingOrders(w.db, time.Now().Add(-time.Duration(timeoutMinutes)*time.Minute))
	if err == nil && count > 0 {
		slog.Info("expired pending orders", "count", count)
	}
	return err
}

func (w *Worker) handleNotification(ctx context.Context, task *asynq.Task) error {
	id, err := payloadID(task.Payload(), "delivery_id")
	if err != nil {
		return asynq.SkipRetry
	}
	now := time.Now()
	claim := w.db.Model(&model.NotificationDelivery{}).
		Where("id = ? AND attempts < ? AND ((status IN ? AND (next_attempt_at IS NULL OR next_attempt_at <= ?)) OR (status = ? AND updated_at < ?))", id, 12, []string{"queued", "retrying"}, now, "sending", now.Add(-2*time.Minute)).
		Updates(map[string]any{"status": "sending", "updated_at": now})
	if claim.Error != nil {
		return claim.Error
	}
	if claim.RowsAffected == 0 {
		return nil
	}
	var delivery model.NotificationDelivery
	if err := w.db.First(&delivery, "id = ?", id).Error; err != nil {
		return asynq.SkipRetry
	}
	body, err := w.vault.Decrypt(delivery.BodyCipher, delivery.BodyNonce, delivery.ID[:])
	if err != nil {
		return w.notificationFailure(&delivery, "notification body decryption failed")
	}
	if handled, providerErr := w.sendViaConnector(ctx, delivery, string(body)); handled {
		if providerErr != nil {
			return w.notificationFailure(&delivery, providerErr.Error())
		}
	} else {
		if w.cfg.NotificationRelayURL == "" {
			return w.notificationFailure(&delivery, "notification connector and relay are not configured")
		}
		payload, _ := json.Marshal(map[string]any{"delivery_id": delivery.ID, "channel": delivery.Channel, "recipient": delivery.Recipient, "subject": delivery.Subject, "body": body})
		status, responseBody, relayErr := w.postSigned(ctx, w.cfg.NotificationRelayURL, w.cfg.NotificationRelaySecret, payload)
		if relayErr != nil || status < 200 || status >= 300 {
			if relayErr == nil {
				relayErr = fmt.Errorf("relay returned HTTP %d: %s", status, responseBody)
			}
			return w.notificationFailure(&delivery, relayErr.Error())
		}
	}
	sentAt := time.Now()
	return w.db.Model(&delivery).Updates(map[string]any{"status": "sent", "attempts": delivery.Attempts + 1, "sent_at": &sentAt, "last_error": "", "next_attempt_at": nil}).Error
}

func (w *Worker) notificationFailure(delivery *model.NotificationDelivery, message string) error {
	attempts := delivery.Attempts + 1
	status := "retrying"
	var nextAttemptAt *time.Time
	if attempts >= 12 {
		status = "failed"
		slog.Error("notification moved to terminal failure", "delivery_id", delivery.ID, "error", message)
	} else {
		next := time.Now().Add(time.Duration(1<<min(attempts, 8)) * time.Second)
		nextAttemptAt = &next
	}
	_ = w.db.Model(delivery).Updates(map[string]any{"status": status, "attempts": attempts, "last_error": truncate(message, 2000), "next_attempt_at": nextAttemptAt}).Error
	return fmt.Errorf("dispatch notification: %s", message)
}

func (w *Worker) handleWebhook(ctx context.Context, task *asynq.Task) error {
	id, err := payloadID(task.Payload(), "delivery_id")
	if err != nil {
		return asynq.SkipRetry
	}
	now := time.Now()
	claim := w.db.Model(&model.WebhookDelivery{}).
		Where("id = ? AND attempts < ? AND ((status IN ? AND (next_attempt_at IS NULL OR next_attempt_at <= ?)) OR (status = ? AND updated_at < ?))", id, 12, []string{"queued", "retrying"}, now, "sending", now.Add(-2*time.Minute)).
		Updates(map[string]any{"status": "sending", "updated_at": now})
	if claim.Error != nil {
		return claim.Error
	}
	if claim.RowsAffected == 0 {
		return nil
	}
	var delivery model.WebhookDelivery
	if err := w.db.First(&delivery, "id = ?", id).Error; err != nil {
		return asynq.SkipRetry
	}
	var endpoint model.WebhookEndpoint
	if err := w.db.Where("id = ? AND enabled = ?", delivery.EndpointID, true).First(&endpoint).Error; err != nil {
		w.db.Model(&delivery).Updates(map[string]any{"status": "failed", "response_body": "webhook endpoint is unavailable"})
		return asynq.SkipRetry
	}
	if _, err := security.ValidateOutboundURL(ctx, endpoint.URL, w.cfg.Env != "production"); err != nil {
		return w.webhookFailure(&delivery, 0, "", err)
	}
	secret, err := w.vault.Decrypt(endpoint.SecretCipher, endpoint.SecretNonce, endpoint.ID[:])
	if err != nil {
		return w.webhookFailure(&delivery, 0, "", err)
	}
	payload := []byte(delivery.Payload)
	if len(delivery.PayloadCipher) > 0 || len(delivery.PayloadNonce) > 0 {
		if len(delivery.PayloadCipher) == 0 || len(delivery.PayloadNonce) == 0 {
			return w.webhookFailure(&delivery, 0, "", errors.New("encrypted webhook payload is incomplete"))
		}
		plaintext, decryptErr := w.vault.Decrypt(delivery.PayloadCipher, delivery.PayloadNonce, delivery.ID[:])
		if decryptErr != nil {
			return w.webhookFailure(&delivery, 0, "", decryptErr)
		}
		payload = []byte(plaintext)
	}
	status, responseBody, err := w.postSigned(ctx, endpoint.URL, secret, payload)
	if err != nil || status < 200 || status >= 300 {
		if err == nil {
			err = fmt.Errorf("endpoint returned HTTP %d", status)
		}
		return w.webhookFailure(&delivery, status, responseBody, err)
	}
	deliveredAt := time.Now()
	if err := w.db.Model(&delivery).Updates(map[string]any{"status": "delivered", "attempts": delivery.Attempts + 1, "response_code": status, "response_body": truncate(responseBody, 4000), "delivered_at": &deliveredAt, "next_attempt_at": nil}).Error; err != nil {
		return err
	}
	return w.db.Model(&endpoint).Updates(map[string]any{"failure_count": 0}).Error
}

func (w *Worker) webhookFailure(delivery *model.WebhookDelivery, status int, responseBody string, cause error) error {
	attempts := delivery.Attempts + 1
	deliveryStatus := "retrying"
	var nextAttemptAt *time.Time
	if attempts >= 12 {
		deliveryStatus = "failed"
		slog.Error("webhook moved to terminal failure", "delivery_id", delivery.ID, "error", cause)
	} else {
		next := time.Now().Add(time.Duration(1<<min(attempts, 8)) * time.Second)
		nextAttemptAt = &next
	}
	_ = w.db.Model(delivery).Updates(map[string]any{"status": deliveryStatus, "attempts": attempts, "response_code": status, "response_body": truncate(responseBody+" "+cause.Error(), 4000), "next_attempt_at": nextAttemptAt}).Error
	_ = w.db.Model(&model.WebhookEndpoint{}).Where("id = ?", delivery.EndpointID).UpdateColumn("failure_count", gorm.Expr("failure_count + 1")).Error
	if attempts >= 12 {
		now := time.Now()
		_ = w.db.Model(&model.WebhookEndpoint{}).Where("id = ? AND failure_count >= ?", delivery.EndpointID, 12).Updates(map[string]any{"enabled": false, "disabled_at": &now}).Error
	}
	return cause
}

func (w *Worker) handleSupplierSync(ctx context.Context, task *asynq.Task) error {
	var payload struct {
		SupplierID string `json:"supplier_id"`
		Trigger    string `json:"trigger"`
	}
	if json.Unmarshal(task.Payload(), &payload) != nil {
		return asynq.SkipRetry
	}
	id, err := uuid.Parse(payload.SupplierID)
	if err != nil || id == uuid.Nil {
		return asynq.SkipRetry
	}
	trigger := strings.ToLower(strings.TrimSpace(payload.Trigger))
	if trigger == "" {
		trigger = "schedule"
	}
	if trigger != "manual" && trigger != "schedule" && trigger != "webhook" && trigger != "recovery" {
		return asynq.SkipRetry
	}
	var supplierModel model.Supplier
	if err := w.db.Where("id = ? AND status = ?", id, "active").First(&supplierModel).Error; err != nil {
		return asynq.SkipRetry
	}
	return w.syncSupplierCatalog(ctx, supplierModel, trigger)
}

func supplierCatalogImportRetryDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	return time.Duration(1<<min(attempt, 8)) * time.Second
}

func (w *Worker) handleSupplierCatalogImport(ctx context.Context, task *asynq.Task) error {
	var payload struct {
		JobID  string `json:"job_id"`
		TaskID string `json:"task_id"`
	}
	if json.Unmarshal(task.Payload(), &payload) != nil {
		return asynq.SkipRetry
	}
	jobID, err := uuid.Parse(payload.JobID)
	contextTaskID, _ := asynq.GetTaskID(ctx)
	taskID := strings.TrimSpace(payload.TaskID)
	if err != nil || jobID == uuid.Nil || taskID == "" || contextTaskID == "" || taskID != contextTaskID {
		return asynq.SkipRetry
	}
	retryCount, _ := asynq.GetRetryCount(ctx)
	now := time.Now().UTC()
	var job model.SupplierCatalogImportJob
	noWork := false
	claimErr := w.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&job, "id = ?", jobID).Error; err != nil {
			return err
		}
		if job.Status == "succeeded" || job.Status == "cancelled" || job.TaskID != taskID {
			noWork = true
			return nil
		}
		if job.Status != "queued" && job.Status != "retrying" {
			noWork = true
			return nil
		}
		attempts := max(job.Attempts+1, retryCount+1)
		job.Attempts = attempts
		result := tx.Model(&model.SupplierCatalogImportJob{}).
			Where("id = ? AND task_id = ?", jobID, taskID).
			Updates(map[string]any{
				"status": "running", "task_id": taskID, "attempts": attempts,
				"started_at":   &now,
				"completed_at": nil, "next_attempt_at": nil, "error_summary": "",
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			noWork = true
		}
		return nil
	})
	if errors.Is(claimErr, gorm.ErrRecordNotFound) {
		return asynq.SkipRetry
	}
	if claimErr != nil {
		return claimErr
	}
	if noWork {
		return nil
	}
	if w.supplierCatalogImportProcessor == nil {
		err = fmt.Errorf("%w: supplier catalog import processor is unavailable", asynq.SkipRetry)
	} else {
		progress := func(imported, skipped int) {
			if imported < 0 || skipped < 0 || imported+skipped > job.RequestedCount {
				return
			}
			_ = w.db.Model(&model.SupplierCatalogImportJob{}).
				Where("id = ? AND status = ? AND task_id = ?", jobID, "running", taskID).
				Updates(map[string]any{"imported_count": imported, "skipped_count": skipped}).Error
		}
		err = w.supplierCatalogImportProcessor(ctx, jobID, taskID, progress)
	}
	if errors.Is(err, ErrSupplierCatalogImportClaimLost) {
		return nil
	}
	if err == nil {
		// The processor commits the result and succeeded status atomically with
		// catalog changes. This guard only covers custom processors in tests.
		_ = w.db.Model(&model.SupplierCatalogImportJob{}).
			Where("id = ? AND task_id = ? AND status = ?", jobID, taskID, "running").
			Updates(map[string]any{"status": "succeeded", "completed_at": &now, "next_attempt_at": nil, "error_summary": ""}).Error
		return nil
	}
	terminal := errors.Is(err, asynq.SkipRetry) || job.Attempts >= supplierCatalogImportMaxAttempts
	status := "retrying"
	updates := map[string]any{
		"status": status, "error_summary": safeJobError(err.Error()),
		// The catalog mutation is atomic, so visible progress is reset when an
		// attempt rolls back and will be rebuilt by the next attempt.
		"imported_count": 0, "skipped_count": 0,
	}
	if terminal {
		updates["status"] = "failed"
		updates["completed_at"] = &now
		updates["next_attempt_at"] = nil
	} else {
		next := now.Add(supplierCatalogImportRetryDelay(job.Attempts))
		updates["task_id"] = ""
		updates["next_attempt_at"] = &next
	}
	_ = w.db.Model(&model.SupplierCatalogImportJob{}).Where("id = ? AND task_id = ?", jobID, taskID).Updates(updates).Error
	return err
}

func (w *Worker) handleFXRefresh(ctx context.Context, task *asynq.Task) error {
	var payload struct {
		BaseCode  string `json:"base_code"`
		QuoteCode string `json:"quote_code"`
	}
	if json.Unmarshal(task.Payload(), &payload) != nil {
		return asynq.SkipRetry
	}
	manager := currency.Manager{DB: w.db, AllowPrivate: w.cfg.Env != "production"}
	if _, err := manager.Resolve(ctx, payload.BaseCode, payload.QuoteCode); err != nil {
		return err
	}
	return nil
}

func normalizedSupplierProtocol(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" || value == "standard" {
		return "linlinqi-standard"
	}
	return value
}

func (w *Worker) supplierCredentials(supplierModel model.Supplier) (map[string]string, error) {
	protocol := normalizedSupplierProtocol(supplierModel.Protocol)
	if len(supplierModel.CredentialsCipher) > 0 && len(supplierModel.CredentialsNonce) > 0 {
		plaintext, err := w.vault.Decrypt(supplierModel.CredentialsCipher, supplierModel.CredentialsNonce, append(supplierModel.ID[:], []byte("supplier-credentials-v1")...))
		if err != nil {
			return nil, fmt.Errorf("decrypt supplier credentials: %w", err)
		}
		var credentials map[string]string
		if json.Unmarshal([]byte(plaintext), &credentials) != nil {
			return nil, errors.New("supplier credentials are invalid")
		}
		return supply.ValidateCredentials(protocol, credentials)
	}
	if protocol != "linlinqi-standard" {
		return nil, errors.New("supplier credentials are not configured")
	}
	key, err := w.vault.Decrypt(supplierModel.APIKeyCipher, supplierModel.APIKeyNonce, append(supplierModel.ID[:], []byte("api-key")...))
	if err != nil {
		return nil, err
	}
	secret, err := w.vault.Decrypt(supplierModel.APISecretCipher, supplierModel.APISecretNonce, append(supplierModel.ID[:], []byte("api-secret")...))
	if err != nil {
		return nil, err
	}
	return map[string]string{"api_key": key, "api_secret": secret}, nil
}

func (w *Worker) gatewayForSupplier(supplierModel model.Supplier) (supply.Gateway, map[string]string, error) {
	credentials, err := w.supplierCredentials(supplierModel)
	if err != nil {
		return nil, nil, err
	}
	balanceCurrency := strings.ToUpper(strings.TrimSpace(supplierModel.BalanceCurrency))
	if balanceCurrency == "" {
		balanceCurrency = strings.ToUpper(strings.TrimSpace(supplierModel.PriceCurrency))
	}
	var balanceDefinition model.CurrencyDefinition
	if err := w.db.Select("code", "minor_unit").First(&balanceDefinition, "code = ? AND enabled = ?", balanceCurrency, true).Error; err != nil {
		return nil, nil, fmt.Errorf("supplier balance currency %s is unavailable: %w", balanceCurrency, err)
	}
	money := supply.MoneySpec{
		PriceCurrency:    strings.ToUpper(strings.TrimSpace(supplierModel.PriceCurrency)),
		PriceMinorUnit:   supplierModel.PriceMinorUnit,
		BalanceCurrency:  balanceCurrency,
		BalanceMinorUnit: balanceDefinition.MinorUnit,
	}
	gateway, err := supply.NewGatewayWithMoney(normalizedSupplierProtocol(supplierModel.Protocol), supplierModel.BaseURL, credentials, w.cfg.Env != "production", money)
	return gateway, credentials, err
}

func supplierSupports(protocol, capability string) bool {
	definition, exists := supply.Protocol(normalizedSupplierProtocol(protocol))
	if !exists {
		return false
	}
	for _, item := range definition.Capabilities {
		if item == capability {
			return true
		}
	}
	return false
}

func supplierCallbackCredential(protocol string, credentials map[string]string) string {
	if !supplierSupports(protocol, "callback") {
		return ""
	}
	for _, key := range []string{"api_secret", "app_key"} {
		if value := credentials[key]; value != "" {
			return value
		}
	}
	return ""
}

func supplierMappedPrice(mapping model.ProductMapping, upstreamPrice int64) (int64, error) {
	if upstreamPrice < 0 {
		return 0, fmt.Errorf("negative upstream price")
	}
	switch mapping.PriceMode {
	case "fixed_price":
		if mapping.FixedPrice < 1 || mapping.FixedPrice > 100_000_000 {
			return 0, fmt.Errorf("invalid fixed price")
		}
		return mapping.FixedPrice, nil
	case "fixed_markup":
		if mapping.MarkupBasisPoint < 0 || mapping.MarkupBasisPoint > 100_000 {
			return 0, fmt.Errorf("invalid markup")
		}
		factor := int64(10_000 + mapping.MarkupBasisPoint)
		const maxInt64 = int64(^uint64(0) >> 1)
		if upstreamPrice > (maxInt64-9_999)/factor {
			return 0, fmt.Errorf("mapped price overflow")
		}
		return (upstreamPrice*factor + 9_999) / 10_000, nil
	case "fixed_amount":
		const maxInt64 = int64(^uint64(0) >> 1)
		if mapping.MarkupAmount < 0 || mapping.MarkupAmount > 100_000_000 || upstreamPrice > maxInt64-mapping.MarkupAmount {
			return 0, fmt.Errorf("invalid fixed amount markup")
		}
		return upstreamPrice + mapping.MarkupAmount, nil
	default:
		return 0, fmt.Errorf("unsupported price mode")
	}
}

func supplierMappedConvertedPrice(mapping model.ProductMapping, upstreamPrice int64, sourceMinorUnit, targetMinorUnit int, rate string) (salePrice, convertedCost int64, err error) {
	convertedCost, err = currency.Convert(upstreamPrice, sourceMinorUnit, targetMinorUnit, rate)
	if err != nil {
		return 0, 0, err
	}
	switch mapping.PriceMode {
	case "fixed_price":
		if mapping.FixedPrice < 1 || mapping.FixedPrice > 100_000_000 {
			return 0, 0, fmt.Errorf("invalid fixed price")
		}
		return mapping.FixedPrice, convertedCost, nil
	case "fixed_markup":
		if mapping.MarkupBasisPoint < 0 || mapping.MarkupBasisPoint > 100_000 {
			return 0, 0, fmt.Errorf("invalid markup")
		}
		salePrice, err = currency.ConvertWithMarkup(upstreamPrice, sourceMinorUnit, targetMinorUnit, rate, mapping.MarkupBasisPoint)
		return salePrice, convertedCost, err
	case "fixed_amount":
		const maxInt64 = int64(^uint64(0) >> 1)
		if mapping.MarkupAmount < 0 || mapping.MarkupAmount > 100_000_000 || convertedCost > maxInt64-mapping.MarkupAmount {
			return 0, 0, fmt.Errorf("invalid fixed amount markup")
		}
		return convertedCost + mapping.MarkupAmount, convertedCost, nil
	default:
		return 0, 0, fmt.Errorf("unsupported price mode")
	}
}

func supplierCallbackURL(cfg config.Config, supplierCode string) string {
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.SupplierCallbackURL), "/")
	if baseURL == "" {
		return ""
	}
	return baseURL + "/api/v1/supplier-callbacks/" + supplierCode
}

var errSupplierDeliverySuppressed = errors.New("supplier delivery suppressed because order is no longer deliverable")

func supplierOrderDeliverable(order model.Order) bool {
	return order.Status == "processing" && order.PaymentStatus == "paid"
}

func (w *Worker) handleSupplierPurchase(ctx context.Context, task *asynq.Task) error {
	orderID, err := payloadID(task.Payload(), "order_id")
	if err != nil {
		return asynq.SkipRetry
	}
	var order model.Order
	if err := w.db.Where("id = ? AND status = ? AND payment_status = ?", orderID, "processing", "paid").First(&order).Error; err != nil {
		return asynq.SkipRetry
	}
	var items []model.OrderItem
	if err := w.db.Table("order_items oi").Select("oi.*").Joins("JOIN products p ON p.id = oi.product_id AND p.deleted_at IS NULL").Where("oi.order_id = ? AND oi.deleted_at IS NULL AND p.inventory_mode = ? AND oi.card_ciphertext IS NULL", order.ID, "supplier").Scan(&items).Error; err != nil {
		return err
	}
	for index := range items {
		if err := w.purchaseSupplierItem(ctx, &order, &items[index]); err != nil {
			if errors.Is(err, errSupplierDeliverySuppressed) {
				return nil
			}
			return err
		}
	}
	err = w.db.Transaction(func(tx *gorm.DB) error {
		var locked model.Order
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&locked, "id = ?", order.ID).Error; err != nil {
			return err
		}
		if !supplierOrderDeliverable(locked) {
			return errSupplierDeliverySuppressed
		}
		if locked.Status == "delivered" {
			if err := service.CreateAffiliateCommissionTx(tx, locked, time.Now()); err != nil {
				return err
			}
			return service.CreditResellerMarginTx(tx, locked)
		}
		var outstanding int64
		if err := tx.Table("order_items oi").Joins("JOIN products p ON p.id = oi.product_id AND p.deleted_at IS NULL").Where("oi.order_id = ? AND oi.deleted_at IS NULL AND p.inventory_mode = ? AND oi.card_ciphertext IS NULL", order.ID, "supplier").Count(&outstanding).Error; err != nil {
			return err
		}
		if outstanding > 0 {
			return fmt.Errorf("supplier delivery is still pending")
		}
		now := time.Now()
		if err := tx.Model(&locked).Updates(map[string]any{"status": "delivered", "delivered_at": &now}).Error; err != nil {
			return err
		}
		if err := tx.Create(&model.OrderEvent{OrderID: locked.ID, FromStatus: locked.Status, ToStatus: "delivered", ActorType: "supplier", Reason: "supplier delivery completed"}).Error; err != nil {
			return err
		}
		locked.Status = "delivered"
		locked.DeliveredAt = &now
		if err := service.CreateAffiliateCommissionTx(tx, locked, now); err != nil {
			return err
		}
		if err := service.CreditResellerMarginTx(tx, locked); err != nil {
			return err
		}
		if locked.UserID != nil {
			_, _, err := service.ReconcileUserMembershipTx(tx, *locked.UserID, now)
			return err
		}
		return nil
	})
	if errors.Is(err, errSupplierDeliverySuppressed) {
		return nil
	}
	if err != nil {
		return err
	}
	outbox, err := service.CreateDeliveryOutbox(w.db, w.vault, order.ID, w.cfg.UserAppURL)
	if err != nil {
		return err
	}
	if outbox.NotificationID != nil {
		if _, err := w.client.Enqueue(TypeNotificationDispatch, map[string]string{"delivery_id": outbox.NotificationID.String()}, asynq.Queue("default"), asynq.Unique(30*time.Second)); err != nil {
			return err
		}
	}
	for _, deliveryID := range outbox.WebhookIDs {
		if _, err := w.client.Enqueue(TypeWebhookDeliver, map[string]string{"delivery_id": deliveryID.String()}, asynq.Queue("critical"), asynq.Unique(30*time.Second)); err != nil {
			return err
		}
	}
	return nil
}

func (w *Worker) claimSupplierCallback(procurementID uuid.UUID) (*model.WebhookEvent, *supply.OrderResult, error) {
	now := time.Now()
	staleBefore := now.Add(-2 * time.Minute)
	var candidate model.WebhookEvent
	err := w.db.Where("procurement_order_id = ? AND (status = ? OR (status = ? AND updated_at < ?))", procurementID, "queued", "processing", staleBefore).
		Order("created_at ASC").First(&candidate).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	claim := w.db.Model(&model.WebhookEvent{}).
		Where("id = ? AND (status = ? OR (status = ? AND updated_at < ?))", candidate.ID, "queued", "processing", staleBefore).
		Updates(map[string]any{"status": "processing", "attempts": gorm.Expr("attempts + 1"), "response": "", "updated_at": now})
	if claim.Error != nil {
		return nil, nil, claim.Error
	}
	if claim.RowsAffected == 0 {
		return nil, nil, nil
	}
	if err := w.db.First(&candidate, "id = ?", candidate.ID).Error; err != nil {
		return nil, nil, err
	}
	if len(candidate.PayloadCipher) == 0 || len(candidate.PayloadNonce) == 0 {
		err := errors.New("supplier callback payload encryption is incomplete")
		w.failSupplierCallback(&candidate, err)
		return nil, nil, err
	}
	plaintext, err := w.vault.Decrypt(candidate.PayloadCipher, candidate.PayloadNonce, candidate.ID[:])
	if err != nil {
		w.failSupplierCallback(&candidate, err)
		return nil, nil, err
	}
	var result supply.OrderResult
	if err := json.Unmarshal([]byte(plaintext), &result); err != nil {
		w.failSupplierCallback(&candidate, err)
		return nil, nil, err
	}
	return &candidate, &result, nil
}

func (w *Worker) failSupplierCallback(event *model.WebhookEvent, cause error) {
	now := time.Now()
	_ = w.db.Model(&model.WebhookEvent{}).Where("id = ?", event.ID).Updates(map[string]any{
		"status": "failed", "response": truncate(cause.Error(), 1000), "processed_at": &now,
	}).Error
}

func ignoreSupplierCallbacks(db *gorm.DB, procurementID uuid.UUID, exceptID *uuid.UUID, reason string, at time.Time) error {
	query := db.Model(&model.WebhookEvent{}).Where("procurement_order_id = ? AND status IN ?", procurementID, []string{"queued", "processing"})
	if exceptID != nil {
		query = query.Where("id <> ?", *exceptID)
	}
	return query.Updates(map[string]any{"status": "ignored", "response": reason, "processed_at": &at}).Error
}

func supplierProcurementAuditRequestBody(procurementNo, externalProductID string, quantity int, callbackEnabled bool, parameterKeys []string, parameterMapping json.RawMessage) (string, error) {
	mapping, err := service.DecodeSupplierParameterMapping(parameterMapping)
	if err != nil {
		return "", err
	}
	canonicalMapping, err := service.EncodeSupplierParameterMapping(mapping)
	if err != nil {
		return "", err
	}
	encoded, err := json.Marshal(map[string]any{
		"client_order_no": procurementNo, "external_product_id": externalProductID,
		"quantity": quantity, "payment_method": "supplier_balance",
		"callback_enabled": callbackEnabled, "parameter_keys": parameterKeys,
		"parameter_mapping": canonicalMapping,
	})
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

type supplierProcurementBinding struct {
	SupplierID        uuid.UUID
	ExternalProductID string
	ParameterMapping  json.RawMessage
}

func bindingFromSupplierProcurement(procurement model.ProcurementOrder) (supplierProcurementBinding, error) {
	externalProductID, identityErr := supply.NormalizeExternalID(procurement.ExternalProductID)
	if procurement.SupplierID == uuid.Nil || identityErr != nil {
		return supplierProcurementBinding{}, errors.New("supplier procurement binding is invalid")
	}
	var audit struct {
		ParameterMapping json.RawMessage `json:"parameter_mapping"`
	}
	if strings.TrimSpace(procurement.RequestBody) != "" {
		if err := json.Unmarshal([]byte(procurement.RequestBody), &audit); err != nil {
			return supplierProcurementBinding{}, fmt.Errorf("decode supplier procurement binding: %w", err)
		}
	}
	// Procurements created before configurable key mapping intentionally used
	// identity mapping. Never consult today's product mapping for old retries.
	if len(audit.ParameterMapping) == 0 {
		audit.ParameterMapping = json.RawMessage(`{}`)
	}
	mapping, err := service.DecodeSupplierParameterMapping(audit.ParameterMapping)
	if err != nil {
		return supplierProcurementBinding{}, fmt.Errorf("decode supplier parameter mapping snapshot: %w", err)
	}
	canonicalMapping, err := service.EncodeSupplierParameterMapping(mapping)
	if err != nil {
		return supplierProcurementBinding{}, err
	}
	return supplierProcurementBinding{
		SupplierID: procurement.SupplierID, ExternalProductID: externalProductID,
		ParameterMapping: canonicalMapping,
	}, nil
}

func (w *Worker) purchaseSupplierItem(ctx context.Context, order *model.Order, item *model.OrderItem) error {
	var currentOrder model.Order
	if err := w.db.Select("id", "status", "payment_status").First(&currentOrder, "id = ?", order.ID).Error; err != nil {
		return err
	}
	if !supplierOrderDeliverable(currentOrder) {
		return errSupplierDeliverySuppressed
	}
	var procurement model.ProcurementOrder
	procurementErr := w.db.Where("order_item_id = ?", item.ID).First(&procurement).Error
	if procurementErr != nil && procurementErr != gorm.ErrRecordNotFound {
		return procurementErr
	}
	if procurementErr == gorm.ErrRecordNotFound {
		var mapping model.ProductMapping
		if item.ProductMappingID != nil && item.SupplierID != nil && strings.TrimSpace(item.ExternalProductID) != "" {
			if err := w.db.Table("product_mappings pm").Select("pm.*").
				Joins("JOIN suppliers s ON s.id = pm.supplier_id AND s.deleted_at IS NULL AND s.status = ?", "active").
				Where("pm.id = ? AND pm.supplier_id = ? AND pm.product_id = ? AND pm.external_product_id = ? AND pm.deleted_at IS NULL", *item.ProductMappingID, *item.SupplierID, item.ProductID, item.ExternalProductID).
				Scan(&mapping).Error; err != nil || mapping.ID == uuid.Nil {
				return fmt.Errorf("supplier mapping snapshot is no longer executable for product %s", item.ProductID)
			}
		} else {
			// Compatibility for orders created before immutable supplier selection
			// was added. New orders always use the branch above.
			mappingQuery := w.db.Table("product_mappings pm").Select("pm.*").
				Joins("JOIN suppliers s ON s.id = pm.supplier_id AND s.deleted_at IS NULL AND s.status = ?", "active").
				Joins("LEFT JOIN supplier_products sp ON sp.supplier_id = pm.supplier_id AND sp.product_id = pm.product_id AND sp.variant_id IS NOT DISTINCT FROM pm.variant_id AND sp.deleted_at IS NULL").
				Where("pm.product_id = ? AND pm.deleted_at IS NULL AND COALESCE(sp.external_stock, 0) >= ?", item.ProductID, item.Quantity)
			if item.VariantID == nil {
				mappingQuery = mappingQuery.Where("pm.variant_id IS NULL")
			} else {
				mappingQuery = mappingQuery.Where("pm.variant_id = ?", *item.VariantID)
			}
			if err := mappingQuery.Order("pm.auto_sync_price DESC, COALESCE(sp.external_stock, 0) DESC, pm.updated_at DESC, pm.id ASC").Limit(1).Scan(&mapping).Error; err != nil || mapping.ID == uuid.Nil {
				return fmt.Errorf("no supplier mapping with sufficient stock for product %s", item.ProductID)
			}
		}
		var selectedSupplier model.Supplier
		if err := w.db.First(&selectedSupplier, "id = ? AND status = ?", mapping.SupplierID, "active").Error; err != nil {
			return err
		}
		_, selectedCredentials, err := w.gatewayForSupplier(selectedSupplier)
		if err != nil {
			return err
		}
		selectedSecret := supplierCallbackCredential(selectedSupplier.Protocol, selectedCredentials)
		localParameters, err := service.SupplierOrderParameters(w.db, w.vault, order.ID, item.ProductID, item.VariantID)
		if err != nil {
			return err
		}
		mappingSnapshot := mapping.ParameterMapping
		if strings.TrimSpace(item.ParameterMapping) != "" {
			mappingSnapshot = json.RawMessage(item.ParameterMapping)
		}
		parameters, err := service.ApplySupplierParameterMapping(localParameters, mappingSnapshot)
		if err != nil {
			return fmt.Errorf("apply supplier parameter mapping: %w", err)
		}
		parameterKeys := make([]string, 0, len(parameters))
		for parameterKey := range parameters {
			parameterKeys = append(parameterKeys, parameterKey)
		}
		sort.Strings(parameterKeys)
		if item.FXSnapshotID == nil || len(item.Currency) != 3 || len(item.UpstreamCurrency) != 3 || item.UpstreamUnitPrice < 0 || item.Quantity < 1 || item.UpstreamUnitPrice > (int64(^uint64(0)>>1)/int64(item.Quantity)) {
			return fmt.Errorf("supplier order item currency snapshot is invalid")
		}
		upstreamEstimate := item.UpstreamUnitPrice * int64(item.Quantity)
		costEstimate, err := convertedProcurementCost(w.db, upstreamEstimate, item.UpstreamCurrency, item.Currency, *item.FXSnapshotID)
		if err != nil {
			return err
		}
		externalProductID := mapping.ExternalProductID
		if strings.TrimSpace(item.ExternalProductID) != "" {
			externalProductID = item.ExternalProductID
		}
		candidate := model.ProcurementOrder{Base: model.Base{ID: uuid.New()}, ProcurementNo: "LQP" + strings.ReplaceAll(uuid.NewString(), "-", ""), SupplierID: selectedSupplier.ID, OrderID: order.ID, OrderItemID: item.ID, ExternalProductID: externalProductID, Quantity: item.Quantity, CostAmount: costEstimate, CostCurrency: item.Currency, UpstreamCostAmount: upstreamEstimate, UpstreamCurrency: item.UpstreamCurrency, FXSnapshotID: item.FXSnapshotID, Status: "creating"}
		callbackEnabled := selectedSecret != "" && supplierCallbackURL(w.cfg, selectedSupplier.ID.String()) != ""
		candidate.RequestBody, err = supplierProcurementAuditRequestBody(candidate.ProcurementNo, externalProductID, item.Quantity, callbackEnabled, parameterKeys, mappingSnapshot)
		if err != nil {
			return err
		}
		if callbackEnabled {
			candidate.CallbackSecretCipher, candidate.CallbackSecretNonce, err = service.EncryptProcurementCallbackSecret(w.vault, candidate.ID, selectedSecret)
			if err != nil {
				return err
			}
		}
		if err := w.db.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "order_item_id"}}, DoNothing: true}).Create(&candidate).Error; err != nil {
			return err
		}
		if err := w.db.Where("order_item_id = ?", item.ID).First(&procurement).Error; err != nil {
			return err
		}
	}
	w.createOperationalNotifications(w.db, "procurement.created", procurement.ID.String(), map[string]string{
		"occurred_at": procurement.CreatedAt.UTC().Format(time.RFC3339), "status": procurement.Status,
		"amount": fmt.Sprintf("%d", procurement.CostAmount), "currency": procurement.CostCurrency,
		"email": order.Email, "order_no": order.OrderNo, "channel": "supplier", "summary": "上游采购单已创建",
	})
	if procurement.Status == "completed" {
		_ = ignoreSupplierCallbacks(w.db, procurement.ID, nil, "procurement already completed", time.Now())
		return nil
	}
	if procurement.Status == "failed" || procurement.Status == "cancelled" {
		_ = ignoreSupplierCallbacks(w.db, procurement.ID, nil, "procurement is terminal", time.Now())
		return fmt.Errorf("procurement %s requires manual review", procurement.ProcurementNo)
	}
	if procurement.Attempts >= 120 {
		_ = w.failSupplierOrder(order.ID, "supplier procurement exceeded retry limit")
		return fmt.Errorf("procurement %s exceeded retry limit", procurement.ProcurementNo)
	}
	binding, err := bindingFromSupplierProcurement(procurement)
	if err != nil {
		return err
	}
	if procurement.Quantity != item.Quantity || procurement.Quantity < 1 {
		return fmt.Errorf("procurement %s quantity snapshot is inconsistent", procurement.ProcurementNo)
	}
	var supplierModel model.Supplier
	// A disabled supplier is excluded from new mapping selection, while an
	// already-created procurement must keep using its original supplier.
	if err := w.db.First(&supplierModel, "id = ?", binding.SupplierID).Error; err != nil {
		return err
	}
	client, credentials, err := w.gatewayForSupplier(supplierModel)
	if err != nil {
		return err
	}
	callbackURL := ""
	if supplierCallbackCredential(supplierModel.Protocol, credentials) != "" {
		callbackURL = supplierCallbackURL(w.cfg, supplierModel.ID.String())
	}
	localParameters, err := service.SupplierOrderParameters(w.db, w.vault, order.ID, item.ProductID, item.VariantID)
	if err != nil {
		return err
	}
	parameters, err := service.ApplySupplierParameterMapping(localParameters, binding.ParameterMapping)
	if err != nil {
		return fmt.Errorf("apply supplier parameter mapping snapshot: %w", err)
	}
	now := time.Now()
	leaseUntil := now.Add(2 * time.Minute)
	claim := w.db.Model(&model.ProcurementOrder{}).
		Where("id = ? AND status IN ? AND (next_poll_at IS NULL OR next_poll_at <= ?)", procurement.ID, []string{"creating", "retrying", "processing", "dispatching"}, now).
		Updates(map[string]any{"status": "dispatching", "next_poll_at": &leaseUntil, "attempts": gorm.Expr("attempts + 1")})
	if claim.Error != nil {
		return claim.Error
	}
	if claim.RowsAffected == 0 {
		return nil
	}
	if err := w.db.First(&procurement, "id = ?", procurement.ID).Error; err != nil {
		return err
	}
	callbackEvent, callbackResult, err := w.claimSupplierCallback(procurement.ID)
	if err != nil {
		w.db.Model(&procurement).Updates(map[string]any{"status": "retrying", "next_poll_at": time.Now().Add(time.Minute)})
		return err
	}
	var result supply.OrderResult
	if callbackResult != nil {
		result = *callbackResult
	} else if procurement.ExternalOrderNo == "" {
		result, err = client.CreateOrder(ctx, supply.CreateOrderRequest{ClientOrderNo: procurement.ProcurementNo, ExternalProductID: binding.ExternalProductID, Quantity: procurement.Quantity, Email: order.Email, PaymentMethod: "supplier_balance", CallbackURL: callbackURL, Parameters: parameters})
	} else {
		result, err = client.Order(ctx, procurement.ExternalOrderNo)
	}
	if err != nil {
		w.db.Model(&procurement).Updates(map[string]any{"status": "retrying", "next_poll_at": time.Now().Add(time.Minute)})
		return err
	}
	auditResult := result
	auditResult.Deliveries = nil
	result.ExternalOrderNo = strings.TrimSpace(result.ExternalOrderNo)
	if len([]rune(result.ExternalOrderNo)) > 160 || strings.IndexFunc(result.ExternalOrderNo, unicode.IsControl) >= 0 {
		w.db.Model(&procurement).Updates(map[string]any{"status": "failed", "next_poll_at": nil})
		_ = w.failSupplierOrder(order.ID, "supplier returned an invalid order number")
		return fmt.Errorf("supplier returned an invalid order number")
	}
	if result.ExternalOrderNo != "" {
		procurement.ExternalOrderNo = result.ExternalOrderNo
	}
	remoteStatus := strings.ToLower(strings.TrimSpace(result.Status))
	validRemoteStatus := remoteStatus == "pending" || remoteStatus == "processing" || remoteStatus == "delivered" || remoteStatus == "succeeded" || remoteStatus == "completed" || remoteStatus == "failed" || remoteStatus == "cancelled" || remoteStatus == "rejected"
	if procurement.ExternalOrderNo == "" || !validRemoteStatus || result.Cost < 0 || result.Cost > 1_000_000_000_000 {
		w.db.Model(&procurement).Updates(map[string]any{"status": "retrying", "next_poll_at": time.Now().Add(time.Minute)})
		return fmt.Errorf("supplier returned invalid order metadata")
	}
	if result.Cost > 0 {
		if err := normalizeSupplierCostMetadata(w.db, &result, procurement.UpstreamCurrency); err != nil {
			w.db.Model(&procurement).Updates(map[string]any{"external_order_no": procurement.ExternalOrderNo, "status": "failed", "next_poll_at": nil})
			_ = w.failSupplierOrder(order.ID, "supplier returned inconsistent cost currency metadata")
			return err
		}
	}
	convertedCost := procurement.CostAmount
	if result.Cost > 0 {
		if procurement.FXSnapshotID == nil {
			return fmt.Errorf("procurement exchange-rate snapshot is missing")
		}
		convertedCost, err = convertedProcurementCost(w.db, result.Cost, procurement.UpstreamCurrency, procurement.CostCurrency, *procurement.FXSnapshotID)
		if err != nil {
			return err
		}
	}
	auditResult.ExternalOrderNo = result.ExternalOrderNo
	auditResult.Status = remoteStatus
	auditResult.CostCurrency = result.CostCurrency
	auditResult.CostMinorUnit = result.CostMinorUnit
	responseBody, _ := json.Marshal(auditResult)
	if remoteStatus == "failed" || remoteStatus == "cancelled" || remoteStatus == "rejected" {
		updates := map[string]any{"external_order_no": procurement.ExternalOrderNo, "status": "failed", "response_body": string(responseBody), "next_poll_at": nil}
		if result.Cost > 0 {
			updates["upstream_cost_amount"], updates["cost_amount"] = result.Cost, convertedCost
		}
		w.db.Model(&procurement).Updates(updates)
		_ = w.failSupplierOrder(order.ID, "supplier rejected or failed the procurement")
		return fmt.Errorf("supplier order %s failed", procurement.ProcurementNo)
	}
	if remoteStatus != "delivered" && remoteStatus != "succeeded" && remoteStatus != "completed" {
		next := time.Now().Add(time.Minute)
		updates := map[string]any{"external_order_no": procurement.ExternalOrderNo, "status": "processing", "response_body": string(responseBody), "next_poll_at": &next}
		if result.Cost > 0 {
			updates["upstream_cost_amount"], updates["cost_amount"] = result.Cost, convertedCost
		}
		w.db.Model(&procurement).Updates(updates)
		return fmt.Errorf("supplier order %s is still processing", procurement.ProcurementNo)
	}
	if len(result.Deliveries) != item.Quantity {
		w.db.Model(&procurement).Updates(map[string]any{"status": "failed", "response_body": string(responseBody), "next_poll_at": nil})
		_ = w.failSupplierOrder(order.ID, "supplier returned an invalid delivery quantity")
		return fmt.Errorf("supplier returned %d deliveries, expected %d", len(result.Deliveries), item.Quantity)
	}
	if !supply.ValidateDeliveries(result.Deliveries, item.Quantity) {
		w.db.Model(&procurement).Updates(map[string]any{"status": "failed", "response_body": string(responseBody), "next_poll_at": nil})
		_ = w.failSupplierOrder(order.ID, "supplier returned invalid delivery content")
		return fmt.Errorf("supplier returned invalid delivery content")
	}
	delivery := strings.Join(result.Deliveries, "\n")
	ciphertext, nonce, _, err := w.vault.Encrypt(delivery, item.ProductID[:])
	if err != nil {
		return err
	}
	deliveryItemsCipher, deliveryItemsNonce, err := service.EncryptDeliveryItems(w.vault, item.ID, result.Deliveries)
	if err != nil {
		return err
	}
	completedAt := time.Now()
	deliverySuppressed := false
	err = w.db.Transaction(func(tx *gorm.DB) error {
		var lockedOrder model.Order
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Select("id", "status", "payment_status").First(&lockedOrder, "id = ?", order.ID).Error; err != nil {
			return err
		}
		if !supplierOrderDeliverable(lockedOrder) {
			deliverySuppressed = true
			if err := tx.Model(&procurement).Updates(map[string]any{
				"external_order_no": procurement.ExternalOrderNo, "status": "failed", "response_body": string(responseBody),
				"completed_at": &completedAt, "next_poll_at": nil,
			}).Error; err != nil {
				return err
			}
			if callbackEvent != nil {
				if err := tx.Model(&model.WebhookEvent{}).Where("id = ?", callbackEvent.ID).Updates(map[string]any{"status": "ignored", "response": "order is no longer deliverable", "processed_at": &completedAt}).Error; err != nil {
					return err
				}
			}
			if err := ignoreSupplierCallbacks(tx, procurement.ID, nil, "order is no longer deliverable", completedAt); err != nil {
				return err
			}
			if err := service.ReleaseSupplierInventoryReservationsTx(tx, order.ID, "order is no longer deliverable"); err != nil {
				return err
			}
			w.createOperationalNotifications(tx, "procurement.delivery_suppressed", procurement.ID.String(), map[string]string{
				"occurred_at": completedAt.UTC().Format(time.RFC3339), "status": "failed",
				"amount": fmt.Sprintf("%d", convertedCost), "currency": procurement.CostCurrency,
				"email": order.Email, "order_no": order.OrderNo, "channel": "supplier", "summary": "订单已进入退款或终态，上游交付已拦截并转人工复核",
			})
			return nil
		}
		claim := tx.Model(item).Where("card_ciphertext IS NULL").Updates(map[string]any{"card_ciphertext": ciphertext, "card_nonce": nonce, "delivery_items_cipher": deliveryItemsCipher, "delivery_items_nonce": deliveryItemsNonce, "card_preview": security.SecretPreview(delivery)})
		if claim.Error != nil {
			return claim.Error
		}
		if claim.RowsAffected == 0 {
			if callbackEvent != nil {
				if err := tx.Model(&model.WebhookEvent{}).Where("id = ?", callbackEvent.ID).Updates(map[string]any{"status": "ignored", "response": "delivery was already claimed", "processed_at": &completedAt}).Error; err != nil {
					return err
				}
			}
			return nil
		}
		procurementUpdates := map[string]any{"external_order_no": procurement.ExternalOrderNo, "status": "completed", "cost_amount": convertedCost, "response_body": string(responseBody), "completed_at": &completedAt, "next_poll_at": nil}
		if result.Cost > 0 {
			procurementUpdates["upstream_cost_amount"] = result.Cost
		}
		if err := tx.Model(&procurement).Updates(procurementUpdates).Error; err != nil {
			return err
		}
		if err := service.ConsumeSupplierInventoryReservationTx(tx, item.ID); err != nil {
			return err
		}
		attempt := model.FulfillmentAttempt{OrderID: order.ID, OrderItemID: item.ID, Mode: "supplier", Attempt: 1, Status: "succeeded", SupplierID: &supplierModel.ID, ExternalOrder: procurement.ExternalOrderNo, StartedAt: procurement.CreatedAt, FinishedAt: &completedAt}
		if err := tx.Create(&attempt).Error; err != nil {
			return err
		}
		if callbackEvent != nil {
			if err := tx.Model(&model.WebhookEvent{}).Where("id = ?", callbackEvent.ID).Updates(map[string]any{"status": "processed", "response": "delivery accepted", "processed_at": &completedAt}).Error; err != nil {
				return err
			}
			if err := ignoreSupplierCallbacks(tx, procurement.ID, &callbackEvent.ID, "duplicate terminal callback", completedAt); err != nil {
				return err
			}
		}
		w.createOperationalNotifications(tx, "procurement.succeeded", procurement.ID.String(), map[string]string{
			"occurred_at": completedAt.UTC().Format(time.RFC3339), "status": "completed",
			"amount": fmt.Sprintf("%d", convertedCost), "currency": procurement.CostCurrency,
			"email": order.Email, "order_no": order.OrderNo, "channel": "supplier", "summary": "上游采购已完成",
		})
		return tx.Model(&model.Product{}).Where("id = ?", item.ProductID).UpdateColumn("sold_count", gorm.Expr("sold_count + ?", item.Quantity)).Error
	})
	if err != nil {
		return err
	}
	if deliverySuppressed {
		return errSupplierDeliverySuppressed
	}
	return nil
}

// normalizeSupplierCostMetadata makes the monetary unit of a supplier result
// explicit before any FX conversion. Legacy adapters may omit metadata and are
// normalized to the immutable procurement snapshot; canonical OpenAPI clients
// must return matching currency and minor-unit values.
func normalizeSupplierCostMetadata(db *gorm.DB, result *supply.OrderResult, expectedCurrency string) error {
	expectedCurrency = strings.ToUpper(strings.TrimSpace(expectedCurrency))
	if len(expectedCurrency) != 3 {
		return fmt.Errorf("procurement upstream currency snapshot is invalid")
	}
	var definition model.CurrencyDefinition
	if err := db.Select("code", "minor_unit").First(&definition, "code = ?", expectedCurrency).Error; err != nil {
		return fmt.Errorf("procurement upstream currency %s is unavailable: %w", expectedCurrency, err)
	}
	returnedCurrency := strings.ToUpper(strings.TrimSpace(result.CostCurrency))
	if returnedCurrency == "" {
		result.CostCurrency = expectedCurrency
		result.CostMinorUnit = definition.MinorUnit
		return nil
	}
	if returnedCurrency != expectedCurrency {
		return fmt.Errorf("supplier cost currency %s does not match procurement snapshot %s", returnedCurrency, expectedCurrency)
	}
	if result.CostMinorUnit != definition.MinorUnit {
		return fmt.Errorf("supplier cost minor unit %d does not match %s minor unit %d", result.CostMinorUnit, expectedCurrency, definition.MinorUnit)
	}
	result.CostCurrency = returnedCurrency
	return nil
}

func convertedProcurementCost(db *gorm.DB, amount int64, sourceCode, targetCode string, snapshotID uuid.UUID) (int64, error) {
	if amount < 0 || snapshotID == uuid.Nil {
		return 0, fmt.Errorf("procurement amount snapshot is invalid")
	}
	var snapshot model.FXRateSnapshot
	if err := db.Where("id = ? AND base_code = ? AND quote_code = ?", snapshotID, sourceCode, targetCode).First(&snapshot).Error; err != nil {
		return 0, err
	}
	var source, target model.CurrencyDefinition
	if err := db.Where("code = ?", sourceCode).First(&source).Error; err != nil {
		return 0, err
	}
	if err := db.Where("code = ?", targetCode).First(&target).Error; err != nil {
		return 0, err
	}
	return currency.Convert(amount, source.MinorUnit, target.MinorUnit, snapshot.Rate)
}

func validSupplierDeliveries(values []string) bool {
	return supply.ValidateDeliveries(values, len(values))
}

func (w *Worker) failSupplierOrder(orderID uuid.UUID, reason string) error {
	return w.db.Transaction(func(tx *gorm.DB) error {
		var order model.Order
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&order, "id = ?", orderID).Error; err != nil {
			return err
		}
		if order.Status != "processing" {
			return nil
		}
		if err := tx.Model(&order).Update("status", "failed").Error; err != nil {
			return err
		}
		if err := service.ReleaseSupplierInventoryReservationsTx(tx, order.ID, "supplier procurement failed: "+reason); err != nil {
			return err
		}
		if err := tx.Create(&model.OrderEvent{OrderID: order.ID, FromStatus: "processing", ToStatus: "failed", ActorType: "supplier", Reason: truncate(reason, 500)}).Error; err != nil {
			return err
		}
		w.createOperationalNotifications(tx, "order.failed", order.ID.String(), map[string]string{"status": "failed", "amount": fmt.Sprintf("%d", order.Total), "currency": order.Currency, "email": order.Email, "order_no": order.OrderNo, "summary": "订单交付失败并进入人工复核"})
		return nil
	})
}

func (w *Worker) handleReconciliation(_ context.Context, task *asynq.Task) error {
	id, err := payloadID(task.Payload(), "batch_id")
	if err != nil {
		return asynq.SkipRetry
	}
	var batch model.ReconciliationBatch
	if err := w.db.First(&batch, "id = ?", id).Error; err != nil {
		return asynq.SkipRetry
	}
	var total, matched, resolved, mismatched int64
	w.db.Model(&model.ReconciliationItem{}).Where("batch_id = ?", batch.ID).Count(&total)
	w.db.Model(&model.ReconciliationItem{}).Where("batch_id = ? AND status = ?", batch.ID, "matched").Count(&matched)
	w.db.Model(&model.ReconciliationItem{}).Where("batch_id = ? AND status = ?", batch.ID, "resolved").Count(&resolved)
	w.db.Model(&model.ReconciliationItem{}).Where("batch_id = ? AND status NOT IN ?", batch.ID, []string{"matched", "resolved"}).Count(&mismatched)
	now := time.Now()
	status := "completed"
	if mismatched > 0 {
		status = "differences_found"
	}
	return w.db.Model(&batch).Updates(map[string]any{"total": total, "matched": matched, "resolved": resolved, "mismatched": mismatched, "status": status, "completed_at": &now}).Error
}

type workerPaymentConfig struct {
	BaseURL    string `json:"base_url"`
	MerchantID string `json:"merchant_id"`
	Secret     string `json:"secret"`
	APIToken   string `json:"api_token"`
	TradeType  string `json:"trade_type"`
	Fiat       string `json:"fiat"`
	Timeout    int    `json:"timeout"`
}

func (w *Worker) paymentDriver(channel model.PaymentChannel) (payment.Driver, error) {
	if channel.Provider == "sandbox" && w.cfg.Env != "production" {
		return payment.SandboxDriver{Secret: w.cfg.OpenAPISecret}, nil
	}
	plaintext, err := w.vault.Decrypt(channel.ConfigCipher, channel.ConfigNonce, channel.ID[:])
	if err != nil {
		return nil, errors.New("payment configuration decryption failed")
	}
	var cfg workerPaymentConfig
	if json.Unmarshal([]byte(plaintext), &cfg) != nil {
		return nil, errors.New("payment configuration is unreadable")
	}
	switch channel.Provider {
	case "signed_http":
		if cfg.BaseURL == "" || cfg.MerchantID == "" || len(cfg.Secret) < 24 {
			return nil, errors.New("payment configuration is incomplete")
		}
		return payment.NewSignedHTTPDriver(channel.Code, cfg.BaseURL, cfg.MerchantID, cfg.Secret, w.cfg.Env != "production"), nil
	case "bepusdt":
		if cfg.BaseURL == "" || cfg.APIToken == "" || !payment.ValidBepusdtTradeType(cfg.TradeType) || !payment.BepusdtFiats[strings.ToUpper(cfg.Fiat)] {
			return nil, errors.New("bepusdt payment configuration is incomplete")
		}
		var definition model.CurrencyDefinition
		if err := w.db.Where("code = ? AND enabled = ?", strings.ToUpper(cfg.Fiat), true).First(&definition).Error; err != nil || definition.MinorUnit < 0 || definition.MinorUnit > 6 {
			return nil, errors.New("bepusdt settlement currency is unavailable")
		}
		return payment.NewBepusdtDriver(payment.BepusdtConfig{
			Code: channel.Code, BaseURL: cfg.BaseURL, APIToken: cfg.APIToken,
			TradeType: cfg.TradeType, Fiat: strings.ToUpper(cfg.Fiat), MinorUnit: definition.MinorUnit,
			Timeout: cfg.Timeout, AllowPrivate: w.cfg.Env != "production",
		}), nil
	default:
		return nil, errors.New("payment provider is unsupported")
	}
}

func (w *Worker) handleRechargeRefund(ctx context.Context, task *asynq.Task) error {
	id, err := payloadID(task.Payload(), "recharge_transaction_id")
	if err != nil {
		return asynq.SkipRetry
	}
	now := time.Now()
	claim := w.db.Model(&model.RechargeTransaction{}).
		Where("id = ? AND refund_attempts < ? AND ((disposition IN ? AND (refund_next_attempt_at IS NULL OR refund_next_attempt_at <= ?)) OR (disposition = ? AND updated_at < ?))", id, 24, []string{"refund_pending", "refund_retrying"}, now, "refund_processing", now.Add(-5*time.Minute)).
		Updates(map[string]any{"disposition": "refund_processing", "updated_at": now})
	if claim.Error != nil {
		return claim.Error
	}
	if claim.RowsAffected == 0 {
		var existing model.RechargeTransaction
		if w.db.First(&existing, "id = ?", id).Error != nil {
			return asynq.SkipRetry
		}
		return nil
	}
	var transaction model.RechargeTransaction
	if err := w.db.First(&transaction, "id = ?", id).Error; err != nil {
		return asynq.SkipRetry
	}
	var recharge model.RechargeOrder
	if err := w.db.First(&recharge, "id = ?", transaction.RechargeOrderID).Error; err != nil {
		return w.rechargeRefundFailure(&transaction, "recharge order is missing")
	}
	var channel model.PaymentChannel
	if err := w.db.First(&channel, "id = ?", recharge.ChannelID).Error; err != nil {
		return w.rechargeRefundFailure(&transaction, "payment channel is missing")
	}
	driver, err := w.paymentDriver(channel)
	if err != nil {
		return w.rechargeRefundFailure(&transaction, err.Error())
	}
	if transaction.RefundNo == "" || transaction.ProviderTradeNo == "" || transaction.Amount < 1 {
		return w.rechargeRefundFailure(&transaction, "recharge refund identity is incomplete")
	}
	result, err := driver.Refund(ctx, payment.RefundRequest{
		RefundNo: transaction.RefundNo, ProviderTradeNo: transaction.ProviderTradeNo,
		Reason: transaction.MismatchReason, Amount: transaction.Amount, Currency: transaction.Currency,
	})
	if err != nil {
		if errors.Is(err, payment.ErrRefundNotSupported) {
			return w.rechargeRefundUnsupported(&transaction, err.Error())
		}
		return w.rechargeRefundFailure(&transaction, err.Error())
	}
	if result.Status != "succeeded" || strings.TrimSpace(result.ProviderRefundNo) == "" {
		return w.rechargeRefundFailure(&transaction, "provider refund is pending or missing its reference")
	}
	refundedAt := time.Now().UTC()
	return w.db.Transaction(func(tx *gorm.DB) error {
		var lockedRecharge model.RechargeOrder
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&lockedRecharge, "id = ?", recharge.ID).Error; err != nil {
			return err
		}
		var lockedTransaction model.RechargeTransaction
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&lockedTransaction, "id = ?", transaction.ID).Error; err != nil {
			return err
		}
		if lockedTransaction.Disposition == "refunded" {
			return nil
		}
		if lockedTransaction.Disposition != "refund_processing" {
			return fmt.Errorf("recharge refund is no longer processing")
		}
		if err := tx.Model(&lockedTransaction).Updates(map[string]any{
			"disposition": "refunded", "provider_refund_no": result.ProviderRefundNo,
			"refunded_at": &refundedAt, "refund_next_attempt_at": nil, "refund_last_error": "",
		}).Error; err != nil {
			return err
		}
		if lockedRecharge.Status == "succeeded" {
			return nil
		}
		var active, failed int64
		if err := tx.Model(&model.RechargeTransaction{}).
			Where("recharge_order_id = ? AND id <> ? AND disposition IN ?", lockedRecharge.ID, lockedTransaction.ID, []string{"refund_pending", "refund_processing", "refund_retrying"}).Count(&active).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.RechargeTransaction{}).
			Where("recharge_order_id = ? AND id <> ? AND disposition = ?", lockedRecharge.ID, lockedTransaction.ID, "refund_failed").Count(&failed).Error; err != nil {
			return err
		}
		status := "refunded"
		if failed > 0 {
			status = "refund_failed"
		} else if active > 0 {
			status = "requires_refund"
		}
		return tx.Model(&lockedRecharge).Update("status", status).Error
	})
}

func (w *Worker) rechargeRefundFailure(transaction *model.RechargeTransaction, message string) error {
	attempts := transaction.RefundAttempts + 1
	disposition := "refund_retrying"
	var nextAttemptAt *time.Time
	if attempts >= 24 {
		disposition = "refund_failed"
		slog.Error("recharge refund moved to terminal failure", "recharge_transaction_id", transaction.ID, "error", safeJobError(message))
	} else {
		next := time.Now().Add(time.Duration(1<<min(attempts, 8)) * time.Second)
		nextAttemptAt = &next
	}
	safeMessage := safeJobError(message)
	err := w.db.Transaction(func(tx *gorm.DB) error {
		var recharge model.RechargeOrder
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&recharge, "id = ?", transaction.RechargeOrderID).Error; err != nil {
			return err
		}
		var locked model.RechargeTransaction
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&locked, "id = ?", transaction.ID).Error; err != nil {
			return err
		}
		if locked.Disposition != "refund_processing" {
			return nil
		}
		if err := tx.Model(&locked).Updates(map[string]any{
			"disposition": disposition, "refund_attempts": attempts,
			"refund_next_attempt_at": nextAttemptAt, "refund_last_error": safeMessage,
		}).Error; err != nil {
			return err
		}
		if disposition == "refund_failed" && recharge.Status != "succeeded" {
			return tx.Model(&recharge).Update("status", "refund_failed").Error
		}
		return nil
	})
	if err != nil {
		return err
	}
	return fmt.Errorf("process recharge refund %s: %s", transaction.RefundNo, safeMessage)
}

// rechargeRefundUnsupported permanently fails a recharge refund whose provider
// has no refund API instead of exhausting the retry budget.
func (w *Worker) rechargeRefundUnsupported(transaction *model.RechargeTransaction, message string) error {
	safeMessage := safeJobError(message)
	slog.Error("recharge refund moved to terminal failure (provider unsupported)", "recharge_transaction_id", transaction.ID, "error", safeMessage)
	err := w.db.Transaction(func(tx *gorm.DB) error {
		var recharge model.RechargeOrder
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&recharge, "id = ?", transaction.RechargeOrderID).Error; err != nil {
			return err
		}
		var locked model.RechargeTransaction
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&locked, "id = ?", transaction.ID).Error; err != nil {
			return err
		}
		if locked.Disposition != "refund_processing" {
			return nil
		}
		if err := tx.Model(&locked).Updates(map[string]any{
			"disposition": "refund_failed", "refund_attempts": 24,
			"refund_next_attempt_at": nil, "refund_last_error": safeMessage,
		}).Error; err != nil {
			return err
		}
		if recharge.Status != "succeeded" {
			return tx.Model(&recharge).Update("status", "refund_failed").Error
		}
		return nil
	})
	if err != nil {
		return err
	}
	return asynq.SkipRetry
}

func (w *Worker) handleRefund(ctx context.Context, task *asynq.Task) error {
	id, err := payloadID(task.Payload(), "refund_id")
	if err != nil {
		return asynq.SkipRetry
	}
	now := time.Now()
	claimBefore := now.Add(-5 * time.Minute)
	claim := w.db.Model(&model.Refund{}).Where("id = ? AND attempts < ? AND ((status IN ? AND (next_attempt_at IS NULL OR next_attempt_at <= ?)) OR (status = ? AND updated_at < ?))", id, 24, []string{"pending", "retrying"}, now, "processing", claimBefore).Updates(map[string]any{"status": "processing", "updated_at": now})
	if claim.Error != nil {
		return claim.Error
	}
	if claim.RowsAffected == 0 {
		var existing model.Refund
		if w.db.First(&existing, "id = ?", id).Error != nil {
			return asynq.SkipRetry
		}
		return nil
	}
	var refund model.Refund
	if err := w.db.First(&refund, "id = ?", id).Error; err != nil {
		return asynq.SkipRetry
	}
	if refund.PaymentIntentID == nil {
		return w.refundFailure(&refund, "payment intent is missing")
	}
	var intent model.PaymentIntent
	if err := w.db.First(&intent, "id = ?", *refund.PaymentIntentID).Error; err != nil {
		return w.refundFailure(&refund, "payment intent is missing")
	}
	// A nil UUID is the durable provider identity for an order settled from a
	// LinLinQi wallet. Wallet refunds never load a mutable external channel or
	// leave the database transaction: the original debit proves the exact
	// account to credit and the deterministic ledger entry makes task replay
	// safe after worker restarts.
	if intent.ChannelID == uuid.Nil {
		processedAt := time.Now().UTC()
		if err := w.db.Transaction(func(tx *gorm.DB) error {
			// Preserve the project-wide monetary lock order: order -> intent ->
			// refund. This also serializes cumulative partial refunds before the
			// wallet account itself is locked by ApplyWalletOrderRefundTx.
			var order model.Order
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&order, "id = ?", refund.OrderID).Error; err != nil {
				return err
			}
			var lockedIntent model.PaymentIntent
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&lockedIntent, "id = ?", *refund.PaymentIntentID).Error; err != nil {
				return err
			}
			if lockedIntent.OrderID != order.ID || lockedIntent.ChannelID != uuid.Nil {
				return fmt.Errorf("wallet payment intent does not belong to order")
			}
			var lockedRefund model.Refund
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&lockedRefund, "id = ?", refund.ID).Error; err != nil {
				return err
			}
			if lockedRefund.Status != "processing" {
				return fmt.Errorf("wallet refund is no longer processing")
			}
			entry, err := service.ApplyWalletOrderRefundTx(tx, order, lockedRefund, processedAt)
			if err != nil {
				return err
			}
			return FinalizeSuccessfulRefundTx(tx, lockedRefund, "wallet:"+entry.ID.String(), processedAt)
		}); err != nil {
			return w.refundFailure(&refund, err.Error())
		}
		return nil
	}
	var channel model.PaymentChannel
	if err := w.db.First(&channel, "id = ?", intent.ChannelID).Error; err != nil {
		return w.refundFailure(&refund, "payment channel is missing")
	}
	driver, err := w.paymentDriver(channel)
	if err != nil {
		return w.refundFailure(&refund, err.Error())
	}
	result, err := driver.Refund(ctx, payment.RefundRequest{RefundNo: refund.RefundNo, ProviderTradeNo: intent.ProviderTradeNo, Reason: refund.Reason, Amount: refund.Amount, Currency: refund.Currency})
	if err != nil {
		if errors.Is(err, payment.ErrRefundNotSupported) {
			return w.refundUnsupported(&refund, err.Error())
		}
		return w.refundFailure(&refund, err.Error())
	}
	if result.Status != "succeeded" {
		return w.refundFailure(&refund, "provider refund is pending")
	}
	processedAt := time.Now()
	return w.db.Transaction(func(tx *gorm.DB) error {
		return FinalizeSuccessfulRefundTx(tx, refund, result.ProviderRefundNo, processedAt)
	})
}

// FinalizeSuccessfulRefundTx commits the shared order/payment state transition
// after either a verified external provider refund or a local wallet credit.
// It intentionally performs no provider call and must run in the transaction
// that already completed the corresponding financial mutation.
func FinalizeSuccessfulRefundTx(tx *gorm.DB, refund model.Refund, providerRefundNo string, processedAt time.Time) error {
	return finalizeSuccessfulRefundTx(tx, refund, providerRefundNo, processedAt)
}

func finalizeSuccessfulRefundTx(tx *gorm.DB, refund model.Refund, providerRefundNo string, processedAt time.Time) error {
	if refund.PaymentIntentID == nil {
		return errors.New("payment intent is missing")
	}
	// Keep the same global lock order as checkout and callbacks. Concurrent
	// partial refunds serialize on the order, then calculate the intent state
	// from committed successful refunds inside this transaction.
	var order model.Order
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&order, "id = ?", refund.OrderID).Error; err != nil {
		return err
	}
	var intent model.PaymentIntent
	if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&intent, "id = ?", *refund.PaymentIntentID).Error; err != nil {
		return err
	}
	if intent.OrderID != order.ID {
		return errors.New("refund payment intent does not belong to order")
	}
	switch intent.Status {
	case "succeeded", "requires_refund", "partially_refunded", "refunded":
	default:
		return fmt.Errorf("payment intent in %s cannot be refunded", intent.Status)
	}
	result := tx.Model(&model.Refund{}).
		Where("id = ? AND status = ?", refund.ID, "processing").
		Updates(map[string]any{"status": "succeeded", "provider_refund_no": providerRefundNo, "processed_at": &processedAt, "next_attempt_at": nil})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return errors.New("refund is no longer processing")
	}
	eventID := "refund:" + refund.ID.String()
	transaction := model.PaymentTransaction{PaymentIntentID: intent.ID, Direction: "refund", ProviderEventID: eventID, Amount: refund.Amount, Currency: refund.Currency, Status: "succeeded", RawPayload: "{}"}
	if err := tx.Where("provider_event_id = ?", eventID).FirstOrCreate(&transaction).Error; err != nil {
		return err
	}
	var intentRefunded int64
	if err := tx.Model(&model.Refund{}).
		Where("payment_intent_id = ? AND deleted_at IS NULL AND status = ?", intent.ID, "succeeded").
		Select("COALESCE(SUM(amount), 0)").Scan(&intentRefunded).Error; err != nil {
		return err
	}
	providerCapacity, err := service.RefundProviderCapacityTx(tx, intent, refund.RequestedBy, refund.Currency)
	if err != nil {
		return err
	}
	if intentRefunded > providerCapacity {
		return errors.New("successful refunds exceed payment intent amount")
	}
	intentStatus := "partially_refunded"
	if intentRefunded >= providerCapacity {
		intentStatus = "refunded"
	}
	if err := tx.Model(&intent).Update("status", intentStatus).Error; err != nil {
		return err
	}
	var orderRefunded int64
	if err := tx.Model(&model.Refund{}).
		Where("order_id = ? AND deleted_at IS NULL AND status = ?", refund.OrderID, "succeeded").
		Select("COALESCE(SUM(order_amount), 0)").Scan(&orderRefunded).Error; err != nil {
		return err
	}
	if orderRefunded > order.Total {
		return errors.New("successful refunds exceed order amount")
	}
	paymentStatus := "partially_refunded"
	orderStatus := order.Status
	if orderRefunded >= order.Total {
		paymentStatus = "refunded"
		orderStatus = "refunded"
		if err := service.ReleaseSupplierInventoryReservationsTx(tx, order.ID, "order fully refunded"); err != nil {
			return err
		}
	}
	if err := service.ReverseAffiliateCommissionTx(tx, order, orderRefunded); err != nil {
		return err
	}
	if err := service.ReverseResellerMarginTx(tx, order, orderRefunded); err != nil {
		return err
	}
	if err := tx.Model(&order).Updates(map[string]any{"payment_status": paymentStatus, "status": orderStatus}).Error; err != nil {
		return err
	}
	if order.UserID != nil {
		_, _, err := service.ReconcileUserMembershipTx(tx, *order.UserID, processedAt)
		return err
	}
	return nil
}

func (w *Worker) refundFailure(refund *model.Refund, message string) error {
	attempts := refund.Attempts + 1
	status := "retrying"
	var nextAttemptAt *time.Time
	if attempts >= 24 {
		status = "failed"
		slog.Error("refund moved to terminal failure", "refund_id", refund.ID, "error", message)
	} else {
		next := time.Now().Add(time.Duration(1<<min(attempts, 8)) * time.Second)
		nextAttemptAt = &next
	}
	_ = w.db.Model(refund).Updates(map[string]any{"status": status, "attempts": attempts, "next_attempt_at": nextAttemptAt}).Error
	return fmt.Errorf("process refund %s: %s", refund.RefundNo, truncate(message, 1000))
}

// refundUnsupported permanently fails a refund whose provider has no refund
// API. BEpusdt cannot reverse a confirmed on-chain payment; retrying would
// only burn attempts, so the refund moves straight to the terminal state for
// operator review.
func (w *Worker) refundUnsupported(refund *model.Refund, message string) error {
	slog.Error("refund moved to terminal failure (provider unsupported)", "refund_id", refund.ID, "error", message)
	_ = w.db.Model(refund).Updates(map[string]any{
		"status": "failed", "attempts": 24, "next_attempt_at": nil,
	}).Error
	return asynq.SkipRetry
}

func (w *Worker) postSigned(ctx context.Context, endpoint, secret string, payload []byte) (int, string, error) {
	timestamp := fmt.Sprintf("%d", time.Now().Unix())
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp + "."))
	mac.Write(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return 0, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-LinLinQi-Timestamp", timestamp)
	req.Header.Set("X-LinLinQi-Signature", hex.EncodeToString(mac.Sum(nil)))
	resp, err := w.http.Do(req)
	if err != nil {
		// URL-bearing transport errors may contain customer webhook query
		// credentials. Keep those out of persistent delivery failures and logs.
		return 0, "", errors.New("outbound request failed")
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	return resp.StatusCode, string(body), err
}

func payloadID(payload []byte, field string) (uuid.UUID, error) {
	var value map[string]string
	if err := json.Unmarshal(payload, &value); err != nil {
		return uuid.Nil, err
	}
	return uuid.Parse(value[field])
}

func truncate(value string, maximum int) string {
	value = strings.TrimSpace(value)
	runes := []rune(value)
	if len(runes) <= maximum {
		return value
	}
	return string(runes[:maximum])
}

// dueSupplierSyncQuery compares the persisted timestamp after adding its
// integer minute interval. Keeping the bind parameter on the timestamp side
// of the comparison avoids PostgreSQL having to infer the overloaded type of
// `$n - interval`, which can fail under the extended query protocol.
func dueSupplierSyncQuery(db *gorm.DB, now time.Time) *gorm.DB {
	return db.Model(&model.Supplier{}).
		Select("id").
		Where(
			"status = ? AND (last_sync_at IS NULL OR last_sync_at + (sync_interval_minutes * INTERVAL '1 minute') <= ?)",
			"active",
			now.UTC(),
		)
}

func (w *Worker) recoverSupplierCatalogImportJobs(now time.Time) {
	staleBefore := now.UTC().Add(-supplierCatalogImportTaskTimeout - time.Minute)
	_ = w.db.Model(&model.SupplierCatalogImportJob{}).
		Where("status = ? AND started_at < ? AND attempts >= ?", "running", staleBefore, supplierCatalogImportMaxAttempts).
		Updates(map[string]any{
			"status": "failed", "next_attempt_at": nil, "completed_at": now.UTC(),
			"error_summary": "stale import exhausted recovery attempts",
		}).Error
	_ = w.db.Model(&model.SupplierCatalogImportJob{}).
		Where("status = ? AND started_at < ? AND attempts < ?", "running", staleBefore, supplierCatalogImportMaxAttempts).
		Updates(map[string]any{
			"status": "retrying", "task_id": "", "next_attempt_at": now.UTC(),
			"imported_count": 0, "skipped_count": 0,
			"error_summary": "stale import attempt recovered",
		}).Error
	// A process can die after reserving a token but before a worker claims it.
	// The short reservation lease makes that state recoverable.
	_ = w.db.Model(&model.SupplierCatalogImportJob{}).
		Where("status = ? AND task_id <> ? AND next_attempt_at IS NOT NULL AND next_attempt_at <= ?", "queued", "", now.UTC()).
		Updates(map[string]any{
			"status": "retrying", "task_id": "", "next_attempt_at": now.UTC(),
			"error_summary": "orphaned queue reservation recovered",
		}).Error

	var jobs []model.SupplierCatalogImportJob
	if err := w.db.Select("id").
		Where("status IN ? AND task_id = ? AND (next_attempt_at IS NULL OR next_attempt_at <= ?)", []string{"queued", "retrying"}, "", now.UTC()).
		Order("created_at ASC").Limit(50).Find(&jobs).Error; err != nil {
		slog.Error("recover supplier catalog import jobs", "error", err)
		return
	}
	for _, job := range jobs {
		if _, err := w.client.EnqueueSupplierCatalogImport(job.ID); err != nil && !errors.Is(err, errSupplierCatalogImportAlreadyQueued) {
			slog.Error("enqueue recovered supplier catalog import", "job_id", job.ID, "error", err)
		}
	}
}

func (w *Worker) schedulerLoop() {
	expireTicker := time.NewTicker(time.Minute)
	// Poll due suppliers every minute; each supplier's persisted interval and
	// last successful catalog timestamp decide whether work is enqueued.
	supplierTicker := time.NewTicker(time.Minute)
	importRecoveryTicker := time.NewTicker(30 * time.Second)
	fxTicker := time.NewTicker(15 * time.Minute)
	defer expireTicker.Stop()
	defer supplierTicker.Stop()
	defer importRecoveryTicker.Stop()
	defer fxTicker.Stop()
	enqueueExpiry := func() {
		if _, err := w.client.Enqueue(TypeOrderExpire, map[string]any{"scheduled_at": time.Now().UTC()}, asynq.Queue("low"), asynq.Unique(50*time.Second)); err != nil {
			slog.Error("schedule order expiry", "error", err)
		}
	}
	enqueueExpiry()
	w.recoverSupplierCatalogImportJobs(time.Now().UTC())
	enqueueFX := func() {
		quoteCode := "CNY"
		var setting model.Setting
		if err := w.db.Where("key = ?", "store_currency").First(&setting).Error; err == nil && len(strings.TrimSpace(setting.Value)) == 3 {
			quoteCode = strings.ToUpper(strings.TrimSpace(setting.Value))
		}
		baseCodes := map[string]struct{}{}
		var suppliers []model.Supplier
		if err := w.db.Select("price_currency").Where("status = ?", "active").Find(&suppliers).Error; err == nil {
			for _, supplierModel := range suppliers {
				baseCodes[strings.ToUpper(strings.TrimSpace(supplierModel.PriceCurrency))] = struct{}{}
			}
		}
		var catalogCurrencies []string
		if err := w.db.Model(&model.SupplierCatalogProduct{}).Distinct("currency").Pluck("currency", &catalogCurrencies).Error; err == nil {
			for _, code := range catalogCurrencies {
				baseCodes[strings.ToUpper(strings.TrimSpace(code))] = struct{}{}
			}
		}
		for baseCode := range baseCodes {
			if len(baseCode) != 3 || baseCode == quoteCode {
				continue
			}
			payload := map[string]string{"base_code": baseCode, "quote_code": quoteCode}
			if _, err := w.client.Enqueue(TypeFXRefresh, payload, asynq.Queue("low"), asynq.Unique(14*time.Minute)); err != nil && !errors.Is(err, asynq.ErrDuplicateTask) {
				slog.Error("schedule exchange-rate refresh", "pair", baseCode+"/"+quoteCode, "error", err)
			}
		}
	}
	enqueueFX()
	for {
		select {
		case <-w.stop:
			return
		case <-expireTicker.C:
			enqueueExpiry()
			w.createOpenAPINotifications()
			w.createLifecycleRecoveryNotifications()
			var missingOutboxOrders []model.Order
			w.db.Table("orders o").Select("o.*").
				Where("o.deleted_at IS NULL AND o.payment_status = ? AND o.status IN ?", "paid", []string{"delivered", "completed"}).
				Where("NOT EXISTS (SELECT 1 FROM notification_deliveries nd WHERE nd.deleted_at IS NULL AND nd.idempotency_key = CONCAT('order-delivered:', o.id::text))").
				Order("o.created_at ASC").Limit(200).Find(&missingOutboxOrders)
			for _, order := range missingOutboxOrders {
				w.createOrderDeliveredNotifications(order)
				outbox, err := service.CreateDeliveryOutbox(w.db, w.vault, order.ID, w.cfg.UserAppURL)
				if err != nil {
					slog.Error("recover delivery outbox", "order_id", order.ID, "error", err)
					continue
				}
				if outbox.NotificationID != nil {
					_, _ = w.client.Enqueue(TypeNotificationDispatch, map[string]string{"delivery_id": outbox.NotificationID.String()}, asynq.Queue("default"), asynq.Unique(50*time.Second))
				}
				for _, deliveryID := range outbox.WebhookIDs {
					_, _ = w.client.Enqueue(TypeWebhookDeliver, map[string]string{"delivery_id": deliveryID.String()}, asynq.Queue("critical"), asynq.Unique(50*time.Second))
				}
			}
			var notifications []model.NotificationDelivery
			now := time.Now()
			w.db.Select("id").Where("attempts < ? AND ((status IN ? AND (next_attempt_at IS NULL OR next_attempt_at <= ?)) OR (status = ? AND updated_at < ?))", 12, []string{"queued", "retrying"}, now, "sending", now.Add(-2*time.Minute)).Limit(200).Find(&notifications)
			for _, delivery := range notifications {
				if _, err := w.client.Enqueue(TypeNotificationDispatch, map[string]string{"delivery_id": delivery.ID.String()}, asynq.Queue("default"), asynq.Unique(50*time.Second)); err != nil {
					slog.Error("schedule notification", "delivery_id", delivery.ID, "error", err)
				}
			}
			var webhooks []model.WebhookDelivery
			w.db.Select("id").Where("attempts < ? AND ((status IN ? AND (next_attempt_at IS NULL OR next_attempt_at <= ?)) OR (status = ? AND updated_at < ?))", 12, []string{"queued", "retrying"}, now, "sending", now.Add(-2*time.Minute)).Limit(200).Find(&webhooks)
			for _, delivery := range webhooks {
				if _, err := w.client.Enqueue(TypeWebhookDeliver, map[string]string{"delivery_id": delivery.ID.String()}, asynq.Queue("critical"), asynq.Unique(50*time.Second)); err != nil {
					slog.Error("schedule webhook", "delivery_id", delivery.ID, "error", err)
				}
			}
			var refunds []model.Refund
			w.db.Select("id").Where("attempts < ? AND ((status IN ? AND (next_attempt_at IS NULL OR next_attempt_at <= ?)) OR (status = ? AND updated_at < ?))", 24, []string{"pending", "retrying"}, time.Now(), "processing", time.Now().Add(-5*time.Minute)).Limit(200).Find(&refunds)
			for _, refund := range refunds {
				if _, err := w.client.Enqueue(TypeRefundProcess, map[string]string{"refund_id": refund.ID.String()}, asynq.Queue("critical"), asynq.Unique(50*time.Second)); err != nil {
					slog.Error("schedule refund", "refund_id", refund.ID, "error", err)
				}
			}
			var rechargeRefunds []model.RechargeTransaction
			w.db.Select("id").Where(
				"refund_attempts < ? AND ((disposition IN ? AND (refund_next_attempt_at IS NULL OR refund_next_attempt_at <= ?)) OR (disposition = ? AND updated_at < ?))",
				24, []string{"refund_pending", "refund_retrying"}, now, "refund_processing", now.Add(-5*time.Minute),
			).Limit(200).Find(&rechargeRefunds)
			for _, transaction := range rechargeRefunds {
				if _, err := w.client.Enqueue(TypeRechargeRefundProcess, map[string]string{"recharge_transaction_id": transaction.ID.String()}, asynq.Queue("critical"), asynq.Unique(50*time.Second)); err != nil {
					slog.Error("schedule recharge refund", "recharge_transaction_id", transaction.ID, "error", err)
				}
			}
			if count, err := service.SettleAffiliateCommissions(w.db, 200); err != nil {
				slog.Error("settle affiliate commissions", "error", err)
			} else if count > 0 {
				slog.Info("settled affiliate commissions", "count", count)
			}
			if count, err := service.ReconcileDueMemberships(w.db, now, 200); err != nil {
				slog.Error("reconcile due memberships", "error", err)
			} else if count > 0 {
				slog.Info("reconciled memberships", "count", count)
			}
			var reconciliations []model.ReconciliationBatch
			w.db.Select("id").Where("status IN ?", []string{"pending", "processing"}).Limit(100).Find(&reconciliations)
			for _, batch := range reconciliations {
				if _, err := w.client.Enqueue(TypeReconciliationRun, map[string]string{"batch_id": batch.ID.String()}, asynq.Queue("low"), asynq.Unique(50*time.Second)); err != nil {
					slog.Error("schedule reconciliation", "batch_id", batch.ID, "error", err)
				}
			}
			var supplierOrders []model.Order
			w.db.Select("id").Where("status = ? AND payment_status = ?", "processing", "paid").Limit(200).Find(&supplierOrders)
			for _, order := range supplierOrders {
				if _, err := w.client.Enqueue(TypeSupplierPurchase, map[string]string{"order_id": order.ID.String()}, asynq.Queue("critical"), asynq.Unique(50*time.Second)); err != nil {
					slog.Error("schedule supplier purchase", "order_id", order.ID, "error", err)
				}
			}
		case <-supplierTicker.C:
			var suppliers []model.Supplier
			if err := dueSupplierSyncQuery(w.db, time.Now().UTC()).Find(&suppliers).Error; err != nil {
				slog.Error("list suppliers for sync", "error", err)
				continue
			}
			for _, supplierModel := range suppliers {
				if _, err := w.client.Enqueue(TypeSupplierSync, map[string]string{"supplier_id": supplierModel.ID.String(), "trigger": "schedule"}, asynq.Queue("default"), asynq.Unique(50*time.Second)); err != nil && !errors.Is(err, asynq.ErrDuplicateTask) {
					slog.Error("schedule supplier sync", "supplier_id", supplierModel.ID, "error", err)
				}
			}
		case <-importRecoveryTicker.C:
			w.recoverSupplierCatalogImportJobs(time.Now().UTC())
		case <-fxTicker.C:
			enqueueFX()
		}
	}
}

func (w *Worker) Run() error {
	go w.schedulerLoop()
	if err := w.server.Run(w.mux); err != nil {
		return fmt.Errorf("run worker: %w", err)
	}
	return nil
}
func (w *Worker) Shutdown() {
	slog.Info("stopping LinLinQi worker")
	w.stopOnce.Do(func() { close(w.stop); _ = w.client.Close() })
	w.server.Shutdown()
}
