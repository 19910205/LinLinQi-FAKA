package handler

import (
	"testing"

	"github.com/google/uuid"

	"linlinqi/api/internal/model"
)

func TestRefundNumberForIdempotencyIsScopedAndStable(t *testing.T) {
	first := refundNumberForIdempotency("admin:a", "1234567890abcdef")
	if first != refundNumberForIdempotency("admin:a", "1234567890abcdef") {
		t.Fatal("same refund request did not produce a stable number")
	}
	if first == refundNumberForIdempotency("admin:b", "1234567890abcdef") || first == refundNumberForIdempotency("admin:a", "abcdef1234567890") {
		t.Fatal("refund idempotency number was not scoped to actor and key")
	}
	if len(first) != 52 {
		t.Fatalf("unexpected refund number length: %d", len(first))
	}
}

func TestRefundIdempotentRequestMatchesAllRemainingReplay(t *testing.T) {
	orderID := uuid.New()
	refund := model.Refund{OrderID: orderID, RequestedBy: "admin:test", Reason: "duplicate request", OrderAmount: 600}
	if !refundIdempotentRequestMatches(refund, orderID, "admin:test", " duplicate request ", 0) {
		t.Fatal("all-remaining retry did not replay its durable refund")
	}
	if !refundIdempotentRequestMatches(refund, orderID, "admin:test", "duplicate request", 600) {
		t.Fatal("exact partial retry did not match")
	}
	if refundIdempotentRequestMatches(refund, orderID, "admin:test", "duplicate request", 500) ||
		refundIdempotentRequestMatches(refund, uuid.New(), "admin:test", "duplicate request", 600) ||
		refundIdempotentRequestMatches(refund, orderID, "admin:other", "duplicate request", 600) {
		t.Fatal("different refund request reused an idempotency record")
	}
}
