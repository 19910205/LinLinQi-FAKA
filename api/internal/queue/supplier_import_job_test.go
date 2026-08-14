package queue

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"linlinqi/api/internal/config"
	"linlinqi/api/internal/model"
)

func TestSupplierTaskExecutionPoliciesAreWorkloadSpecific(t *testing.T) {
	retries, timeout := taskExecutionPolicy(TypeSupplierCatalogImport)
	if retries != 0 || timeout != 30*time.Minute {
		t.Fatalf("catalog import policy = retries %d timeout %s, want 0 and 30m", retries, timeout)
	}
	retries, timeout = taskExecutionPolicy(TypeSupplierSync)
	if retries != 12 || timeout != 15*time.Minute {
		t.Fatalf("supplier sync policy = retries %d timeout %s, want 12 and 15m", retries, timeout)
	}
	retries, timeout = taskExecutionPolicy(TypeNotificationDispatch)
	if retries != 12 || timeout != 45*time.Second {
		t.Fatalf("default policy = retries %d timeout %s, want 12 and 45s", retries, timeout)
	}
}

func TestSupplierCatalogImportEnqueueFailureRemainsDurablyRecoverablePostgreSQL(t *testing.T) {
	db := isolatedSupplierWorkerDB(t, "linlinqi_import_enqueue_test_")
	job := model.SupplierCatalogImportJob{
		SupplierID: uuid.New(), Status: "queued", RequestedCount: 1,
		RequestSnapshot: json.RawMessage(`{"external_product_ids":["one"]}`),
		ResultSnapshot:  json.RawMessage(`{}`),
	}
	if err := db.Create(&job).Error; err != nil {
		t.Fatalf("create import job: %v", err)
	}
	client := NewClient(config.Config{RedisAddr: "127.0.0.1:1"}, db)
	t.Cleanup(func() { _ = client.Close() })
	if _, err := client.EnqueueSupplierCatalogImport(job.ID); err == nil {
		t.Fatal("unavailable Redis unexpectedly accepted import task")
	}
	if err := db.First(&job, "id = ?", job.ID).Error; err != nil {
		t.Fatalf("reload import job: %v", err)
	}
	if job.Status != "retrying" || job.TaskID != "" || job.NextAttemptAt == nil || job.ErrorSummary == "" {
		t.Fatalf("queue failure is not recoverable: %#v", job)
	}

	job.Status, job.TaskID, job.NextAttemptAt = "queued", "existing-fencing-token", nil
	if err := db.Model(&job).Updates(map[string]any{"status": job.Status, "task_id": job.TaskID, "next_attempt_at": nil}).Error; err != nil {
		t.Fatalf("prepare reserved job: %v", err)
	}
	if _, err := client.EnqueueSupplierCatalogImport(job.ID); !errors.Is(err, errSupplierCatalogImportAlreadyQueued) {
		t.Fatalf("reserved fencing token accepted a duplicate enqueue: %v", err)
	}
}

func TestSupplierCatalogImportAcceptedButUnclaimedLeaseIsRecoveredPostgreSQL(t *testing.T) {
	db := isolatedSupplierWorkerDB(t, "linlinqi_import_lease_test_")
	now := time.Now().UTC()
	expired := now.Add(-time.Second)
	future := now.Add(5 * time.Minute)
	request := json.RawMessage(`{"external_product_ids":["one"]}`)
	expiredJob := model.SupplierCatalogImportJob{
		SupplierID: uuid.New(), TaskID: "accepted-but-never-claimed", Status: "queued",
		RequestedCount: 1, RequestSnapshot: request, ResultSnapshot: json.RawMessage(`{}`),
		NextAttemptAt: &expired,
	}
	freshJob := model.SupplierCatalogImportJob{
		SupplierID: uuid.New(), TaskID: "fresh-accepted-token", Status: "queued",
		RequestedCount: 1, RequestSnapshot: request, ResultSnapshot: json.RawMessage(`{}`),
		NextAttemptAt: &future,
	}
	if err := db.Create(&expiredJob).Error; err != nil {
		t.Fatalf("create expired reserved job: %v", err)
	}
	if err := db.Create(&freshJob).Error; err != nil {
		t.Fatalf("create fresh reserved job: %v", err)
	}
	client := NewClient(config.Config{RedisAddr: "127.0.0.1:1"}, db)
	t.Cleanup(func() { _ = client.Close() })
	worker := &Worker{db: db, client: client}
	worker.recoverSupplierCatalogImportJobs(now)

	if err := db.First(&expiredJob, "id = ?", expiredJob.ID).Error; err != nil {
		t.Fatalf("reload expired reserved job: %v", err)
	}
	if expiredJob.TaskID != "" || expiredJob.Status != "retrying" || expiredJob.NextAttemptAt == nil || !expiredJob.NextAttemptAt.After(now) {
		t.Fatalf("expired unclaimed lease was not made recoverable: %#v", expiredJob)
	}
	if err := db.First(&freshJob, "id = ?", freshJob.ID).Error; err != nil {
		t.Fatalf("reload fresh reserved job: %v", err)
	}
	if freshJob.TaskID != "fresh-accepted-token" || freshJob.Status != "queued" || freshJob.NextAttemptAt == nil || !freshJob.NextAttemptAt.Equal(future) {
		t.Fatalf("fresh unclaimed lease was recovered prematurely: %#v", freshJob)
	}
}

func TestSupplierCatalogImportRetryDelayIsBounded(t *testing.T) {
	if got := supplierCatalogImportRetryDelay(0); got != 2*time.Second {
		t.Fatalf("first retry delay = %s, want 2s", got)
	}
	if got := supplierCatalogImportRetryDelay(40); got != 256*time.Second {
		t.Fatalf("bounded retry delay = %s, want 256s", got)
	}
}
