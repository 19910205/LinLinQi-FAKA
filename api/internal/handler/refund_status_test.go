package handler

import "testing"

func TestRefundableFulfillmentStatusRejectsInFlightOrders(t *testing.T) {
	for _, status := range []string{"delivered", "completed", "failed"} {
		if !refundableFulfillmentStatus(status) {
			t.Fatalf("terminal fulfillment status %q was rejected", status)
		}
	}
	for _, status := range []string{"", "pending_payment", "paid", "processing", "risk_review", "refunding", "refunded", "cancelled"} {
		if refundableFulfillmentStatus(status) {
			t.Fatalf("non-refundable fulfillment status %q was accepted", status)
		}
	}
}
