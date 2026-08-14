package queue

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"linlinqi/api/internal/model"
)

func TestDueSupplierSyncQueryPostgreSQL(t *testing.T) {
	db := isolatedSupplierWorkerDB(t, "linlinqi_supplier_due_query_test_")
	now := time.Date(2026, time.August, 11, 12, 0, 0, 0, time.UTC)
	recent := now.Add(-14 * time.Minute)
	stale := now.Add(-46 * time.Minute)
	inactiveStale := now.Add(-24 * time.Hour)
	fixtures := []model.Supplier{
		{
			Base: model.Base{ID: uuid.New()}, Name: "Never synced", Code: "due-never-synced",
			BaseURL: "https://due-never.example.test", Protocol: "linlinqi-standard", Status: "active",
			SyncIntervalMinutes: 15,
		},
		{
			Base: model.Base{ID: uuid.New()}, Name: "Recent sync", Code: "not-due-recent",
			BaseURL: "https://not-due.example.test", Protocol: "linlinqi-standard", Status: "active",
			LastSyncAt: &recent, SyncIntervalMinutes: 15,
		},
		{
			Base: model.Base{ID: uuid.New()}, Name: "Stale sync", Code: "due-stale",
			BaseURL: "https://due-stale.example.test", Protocol: "linlinqi-standard", Status: "active",
			LastSyncAt: &stale, SyncIntervalMinutes: 45,
		},
		{
			Base: model.Base{ID: uuid.New()}, Name: "Inactive stale", Code: "inactive-stale",
			BaseURL: "https://inactive-stale.example.test", Protocol: "linlinqi-standard", Status: "disabled",
			LastSyncAt: &inactiveStale, SyncIntervalMinutes: 15,
		},
	}
	if err := db.Create(&fixtures).Error; err != nil {
		t.Fatalf("create supplier schedule fixtures: %v", err)
	}

	var due []model.Supplier
	if err := dueSupplierSyncQuery(db, now).Order("code ASC").Find(&due).Error; err != nil {
		t.Fatalf("query due suppliers: %v", err)
	}
	got := make(map[uuid.UUID]bool, len(due))
	for _, item := range due {
		got[item.ID] = true
	}
	if len(got) != 2 || !got[fixtures[0].ID] || !got[fixtures[2].ID] {
		t.Fatalf("due selection mismatch: got=%v want never-synced=%s stale=%s", got, fixtures[0].ID, fixtures[2].ID)
	}
	if got[fixtures[1].ID] {
		t.Fatal("supplier synced inside its interval was scheduled")
	}
	if got[fixtures[3].ID] {
		t.Fatal("disabled stale supplier was scheduled")
	}
}
