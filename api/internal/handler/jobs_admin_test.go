package handler

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"linlinqi/api/internal/model"
	"linlinqi/api/internal/queue"
)

func TestDecodeAdminJobPayloadRequiresValidReference(t *testing.T) {
	job := model.JobRecord{TaskType: queue.TypeRefundProcess, Payload: `{"refund_id":"not-a-uuid"}`}
	if _, err := decodeAdminJobPayload(job); err == nil {
		t.Fatal("expected an invalid refund reference to be rejected")
	}
	job.Payload = `{"refund_id":"` + uuid.NewString() + `"}`
	if _, err := decodeAdminJobPayload(job); err != nil {
		t.Fatalf("expected valid task payload: %v", err)
	}
}

func TestAdminJobDTOOnlyMarksFailedSupportedJobsRetryable(t *testing.T) {
	now := time.Now()
	failed := toAdminJobDTO(model.JobRecord{TaskType: queue.TypeSupplierSync, Status: "failed"}, now)
	if !failed.Retryable {
		t.Fatal("expected a failed supported task to be retryable")
	}
	running := toAdminJobDTO(model.JobRecord{TaskType: queue.TypeSupplierSync, Status: "running", Base: model.Base{UpdatedAt: now.Add(-3 * time.Minute)}}, now)
	if running.Retryable || !running.Stale {
		t.Fatal("expected stale running task to be diagnostic-only")
	}
}

func TestApplyAdminJobFiltersRejectsUnknownStatus(t *testing.T) {
	recorder := httptest.NewRecorder()
	context, _ := gin.CreateTestContext(recorder)
	context.Request = httptest.NewRequest("GET", "/jobs?status=made_up", nil)
	if _, err := applyAdminJobFilters(nil, context); err == nil {
		t.Fatal("expected unknown status to be rejected before a database query")
	}
}
